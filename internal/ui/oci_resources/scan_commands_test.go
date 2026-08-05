package ociresources

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The scan commands are the only ones in this package that write two things —
// the counts the table shows and the full report Enter opens — and get them
// from a subprocess. A fake trivy on PATH makes both observable: what it prints
// is a report, and where the counts end up is the disk.

// trivyReport is what the fake trivy prints. Two severities are enough to prove
// the counts are read per-severity rather than totalled.
const trivyReport = `{"Results":[{"Target":"api:v1","Vulnerabilities":[
	{"VulnerabilityID":"CVE-1","Severity":"CRITICAL","PkgName":"openssl","InstalledVersion":"3.0.1"},
	{"VulnerabilityID":"CVE-2","Severity":"HIGH","PkgName":"zlib","InstalledVersion":"1.2.11"},
	{"VulnerabilityID":"CVE-3","Severity":"HIGH","PkgName":"curl","InstalledVersion":"8.0.0"}
]}]}`

func installFakeTrivy(t *testing.T, report string) {
	t.Helper()
	installFakeTools(t, fakeScript{
		"trivy --version": {Stdout: "Version: 0.50.1"},
		"trivy image":     {Stdout: report},
	}, "trivy")
}

func vulnScan() scan.ScanOptions {
	return scan.ScanOptions{EnableVuln: true}
}

// The whole point of the cache is that a second visit costs nothing, so the
// scan has to leave both halves behind: the counts the row shows and the report
// Enter reads (Rule 126).
func TestAScanLeavesItsCountsAndItsFullReportOnDisk(t *testing.T) {
	installFakeTrivy(t, trivyReport)

	msgs := testutil.Msgs(scanOneImageCmd(
		imageScanJob{Name: "api:v1", Target: "api:v1"}, vulnScan(), make(chan struct{}, 1)))

	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want a start and a finish", len(msgs))
	}
	if start, ok := msgs[0].(ImageScanStartingMsg); !ok || start.ImageName != "api:v1" {
		t.Fatalf("msgs[0] = %#v, want the scan announcing itself first", msgs[0])
	}
	done, ok := msgs[1].(ImageScanFinishedMsg)
	if !ok {
		t.Fatalf("msgs[1] = %#v, want ImageScanFinishedMsg", msgs[1])
	}
	if done.Err != nil {
		t.Fatalf("Err = %v, want a scan that produced a report", done.Err)
	}
	if done.Entry.Critical != 1 || done.Entry.High != 2 {
		t.Errorf("Entry = %+v, want 1 critical and 2 high", done.Entry)
	}

	cached, err := cache.NewImageScanCache(config.CurrentContextName())
	if err != nil {
		t.Fatalf("reopening the scan cache: %v", err)
	}
	t.Cleanup(func() { _ = cached.Delete("api:v1") })
	entry := cached.Get("api:v1")
	if entry == nil || entry.Critical != 1 {
		t.Errorf("the cache holds %+v, want the counts that were just reported", entry)
	}

	result, err := cache.LoadImageScanResult("api:v1")
	if err != nil {
		t.Fatalf("the full report was not saved: %v", err)
	}
	if len(result.Findings) != 3 {
		t.Errorf("the saved report has %d findings, want all three", len(result.Findings))
	}
}

// An untagged image is cached under the name the table shows but scanned by ID:
// trivy would otherwise resolve "orphan" to "orphan:latest" and try to pull it
// from a registry.
func TestAnUntaggedImageIsScannedByIDAndCachedByName(t *testing.T) {
	installFakeTrivy(t, `{"Results":[]}`)
	invocations := recordInvocations(t)

	msgs := testutil.Msgs(scanOneImageCmd(
		imageScanJob{Name: "orphan", Target: "ddd4444"}, vulnScan(), make(chan struct{}, 1)))

	done := msgs[len(msgs)-1].(ImageScanFinishedMsg)
	if done.ImageName != "orphan" {
		t.Errorf("ImageName = %q, want the name the table shows", done.ImageName)
	}
	t.Cleanup(func() {
		if c, err := cache.NewImageScanCache(config.CurrentContextName()); err == nil {
			_ = c.Delete("orphan")
		}
	})

	var scanned string
	for _, line := range invocations() {
		if strings.HasPrefix(line, "trivy image") {
			scanned = line
		}
	}
	if scanned == "" {
		t.Fatalf("trivy was never asked to scan an image; invocations: %v", invocations())
	}
	if !strings.HasSuffix(scanned, " ddd4444") {
		t.Errorf("trivy was asked to scan %q, want the image ID as the target", scanned)
	}
}

// D20, fixed. A stage whose tool is missing is skipped rather than failed, so
// the result used to carry no errors and no findings — and scanOneImageCmd
// reports a failure only when there are errors *and* no findings. The image was
// recorded as clean, cached with a fresh timestamp, and Rule 126 kept it that
// way until an explicit rescan.
//
// Scan() now records the missing tool as an error, which is what turns this
// into the failure it always was. Nothing in this package changed: the view has
// no dependency check of its own, and with the error present it no longer needs
// one to avoid lying.
func TestAScanWithNoScannerInstalledIsReportedAsAFailure(t *testing.T) {
	noDocker(t) // an empty PATH: no trivy, no gitleaks, no docker either

	msgs := testutil.Msgs(scanOneImageCmd(
		imageScanJob{Name: "no-scanner:v1", Target: "no-scanner:v1"}, vulnScan(), make(chan struct{}, 1)))

	done := msgs[len(msgs)-1].(ImageScanFinishedMsg)

	if done.Err == nil {
		t.Fatal("a scan that ran no scanner was reported as a clean image")
	}
	if !strings.Contains(done.Err.Error(), "trivy") {
		t.Errorf("Err = %v, want it to name the tool that is missing", done.Err)
	}
	if done.ImageName != "no-scanner:v1" {
		t.Errorf("ImageName = %q, want it named even on failure", done.ImageName)
	}

	// The half that did the damage: an empty result must not reach the disk, or
	// the row keeps claiming the image is clean after a restart.
	cached, err := cache.NewImageScanCache(config.CurrentContextName())
	if err != nil {
		t.Fatalf("reopening the scan cache: %v", err)
	}
	if entry := cached.Get("no-scanner:v1"); entry != nil {
		t.Errorf("the cache holds %+v for an image nothing scanned", entry)
	}
}

// The batch is what ctrl+a and the "scan all" path emit. Every image has to be
// announced and answered for, or a row keeps its spinner forever.
func TestABatchAnnouncesAndAnswersForEveryImage(t *testing.T) {
	installFakeTrivy(t, `{"Results":[]}`)

	jobs := []imageScanJob{
		{Name: "api:v1", Target: "api:v1"},
		{Name: "cache:v2", Target: "cache:v2"},
	}
	msgs := testutil.Msgs(batchScanCmd(jobs, vulnScan()))
	t.Cleanup(func() {
		if c, err := cache.NewImageScanCache(config.CurrentContextName()); err == nil {
			for _, job := range jobs {
				_ = c.Delete(job.Name)
			}
		}
	})

	started, finished := map[string]bool{}, map[string]bool{}
	for _, msg := range msgs {
		switch m := msg.(type) {
		case ImageScanStartingMsg:
			started[m.ImageName] = true
		case ImageScanFinishedMsg:
			finished[m.ImageName] = true
		}
	}
	for _, job := range jobs {
		if !started[job.Name] {
			t.Errorf("%s was scanned without announcing itself", job.Name)
		}
		if !finished[job.Name] {
			t.Errorf("%s never reported a result", job.Name)
		}
	}
}

func TestAnEmptyBatchDoesNothing(t *testing.T) {
	if msgs := testutil.Msgs(batchScanCmd(nil, vulnScan())); len(msgs) != 0 {
		t.Errorf("an empty batch produced %v", msgs)
	}
}

// ── The periodic refresh ─────────────────────────────────────────────────────

// tickCmd is the only command in the package that is not worth executing — it
// blocks for its whole interval — so what is checked is that one exists at all.
func TestTheRefreshTickIsScheduled(t *testing.T) {
	cmd := tickCmd()
	if cmd == nil {
		t.Fatal("tickCmd returned no command, so nothing would ever refresh")
	}
	if refreshInterval <= 0 {
		t.Errorf("refreshInterval = %v, want a positive interval", refreshInterval)
	}
}
