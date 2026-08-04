package security

import (
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The cache commands write to ~/.devdesk, which TestMain has redirected at a
// temporary directory, so they are executed rather than asserted on identity.
// Only the scan itself — which runs Trivy and Gitleaks in containers — is left
// alone.

func TestTargetTypeMapsTheFormValue(t *testing.T) {
	m := newTestModel(t)

	if got := m.getTargetType(); got != scan.TargetDirectory {
		t.Errorf("getTargetType() = %v for a directory", got)
	}

	m.targetType = "image"
	if got := m.getTargetType(); got != scan.TargetImage {
		t.Errorf("getTargetType() = %v for an image", got)
	}

	// Anything unrecognised falls back to a directory rather than scanning
	// nothing.
	m.targetType = "nonsense"
	if got := m.getTargetType(); got != scan.TargetDirectory {
		t.Errorf("getTargetType() = %v for an unknown type", got)
	}
}

// A stale entry must not survive a re-scan: if the new scan is interrupted, the
// row would otherwise still show the old counts as if they were current
// (Rule 126).
func TestPurgingTheCacheBeforeAScan(t *testing.T) {
	t.Run("workspace", func(t *testing.T) {
		c, err := cache.NewWorkspaceScanCache()
		if err != nil {
			t.Fatalf("opening the cache: %v", err)
		}
		if err := c.Set("/tmp/purge-me", cache.WorkspaceScanEntry{
			RepoPath: "/tmp/purge-me", Critical: 3, ScannedAt: time.Now(),
		}); err != nil {
			t.Fatalf("seeding the cache: %v", err)
		}

		testutil.Msg(purgeScanCacheCmd("/tmp/purge-me", scan.TargetDirectory))

		fresh, _ := cache.NewWorkspaceScanCache()
		if fresh.Get("/tmp/purge-me") != nil {
			t.Error("the workspace cache entry survived the purge")
		}
	})

	t.Run("image", func(t *testing.T) {
		c, err := cache.NewImageScanCache()
		if err != nil {
			t.Fatalf("opening the cache: %v", err)
		}
		if err := c.Set("nginx:purge", cache.ImageScanEntry{Critical: 2, ScannedAt: time.Now()}); err != nil {
			t.Fatalf("seeding the cache: %v", err)
		}

		testutil.Msg(purgeScanCacheCmd("nginx:purge", scan.TargetImage))

		fresh, _ := cache.NewImageScanCache()
		if fresh.Get("nginx:purge") != nil {
			t.Error("the image cache entry survived the purge")
		}
	})
}

// An image scan writes its severity counts back so the OCI images list can show
// them without re-scanning (Rule 126).
func TestImageScanCountsAreCached(t *testing.T) {
	result := resultFixture()
	result.Counts = scan.SeverityCounts{Critical: 4, High: 3, Medium: 2, Low: 1}

	saveImageScanToCache("nginx:cached", result)

	c, err := cache.NewImageScanCache()
	if err != nil {
		t.Fatalf("opening the cache: %v", err)
	}
	entry := c.Get("nginx:cached")
	if entry == nil {
		t.Fatal("the scan was not cached")
	}
	if entry.Critical != 4 || entry.High != 3 || entry.Medium != 2 || entry.Low != 1 {
		t.Errorf("cached counts = %+v, want the result's", entry)
	}
}

// waitForProgressCmd is the read side of the progress channel. A closed channel
// means the scan finished, and it has to stop rather than block forever.
func TestWaitingForProgress(t *testing.T) {
	t.Run("no channel", func(t *testing.T) {
		if cmd := waitForProgressCmd(nil); cmd != nil {
			t.Error("waitForProgressCmd(nil) returned a command")
		}
	})

	t.Run("an update arrives", func(t *testing.T) {
		ch := make(chan scan.ProgressUpdate, 1)
		ch <- scan.ProgressUpdate{Stage: "vuln", Label: "Vulnerabilities"}

		msg, ok := testutil.MsgOf[ScanProgressMsg](waitForProgressCmd(ch))

		if !ok {
			t.Fatal("no progress message was produced")
		}
		if msg.Update.Stage != "vuln" {
			t.Errorf("Stage = %q", msg.Update.Stage)
		}
	})

	t.Run("the channel closes", func(t *testing.T) {
		ch := make(chan scan.ProgressUpdate)
		close(ch)

		if msg := testutil.Msg(waitForProgressCmd(ch)); msg != nil {
			t.Errorf("a closed channel produced %T, want nothing", msg)
		}
	})
}

// Every progress update schedules the next read, or the second stage never
// arrives.
func TestProgressSchedulesTheNextRead(t *testing.T) {
	m := newTestModel(t)
	m.progressCh = make(chan scan.ProgressUpdate, 1)

	_, cmd := step(t, m, ScanProgressMsg{Update: scan.ProgressUpdate{Stage: "vuln"}})

	if cmd == nil {
		t.Error("a progress update scheduled no further read")
	}
}

// Cancelling has to invalidate the generation as well as flip the state, or the
// goroutine's result would land on the form the user went back to.
func TestCancellingInvalidatesTheGeneration(t *testing.T) {
	m := newTestModel(t)
	m.state = StateScanning
	before := m.scanGen

	m = feed(t, m, testutil.Key("esc"))

	if m.scanGen == before {
		t.Error("cancelling did not invalidate the pending result")
	}
	if m.state != StateInput {
		t.Errorf("state = %v after cancelling", m.state)
	}
}

// ctrl+c cancels too, since a scan can run for minutes and the router's own
// ctrl+c would otherwise quit the application mid-scan.
func TestCtrlCCancelsTheScan(t *testing.T) {
	m := newTestModel(t)
	m.state = StateScanning

	if got := feed(t, m, testutil.Key("ctrl+c")).state; got != StateInput {
		t.Errorf("state = %v after ctrl+c, want the form", got)
	}
}

// A keystroke that is neither esc nor ctrl+c must not disturb a running scan.
func TestOtherKeysAreIgnoredWhileScanning(t *testing.T) {
	m := newTestModel(t)
	m.state = StateScanning

	next := feed(t, m, testutil.Key("enter"), testutil.Key("tab"), testutil.Key(" "))

	if next.state != StateScanning {
		t.Errorf("state = %v after stray keystrokes", next.state)
	}
}

// The detail column is one line in a fixed-width row, so a tool that writes a
// long progress line must not push it past the edge — and truncation counts
// columns, not bytes.
func TestScanDetailTruncationHandlesMultibyte(t *testing.T) {
	multibyte := strings.Repeat("é", 400)

	got := truncateScanDetail(multibyte)

	if !strings.ContainsRune(got, 'é') {
		t.Errorf("truncateScanDetail() = %q, want it to keep the runes", got)
	}
	if strings.Contains(got, "�") {
		t.Errorf("truncateScanDetail() cut a rune in half: %q", got)
	}
}

// ── The Trivy server address ─────────────────────────────────────────────────

// This is how a ":" gets into the field, and it is worth pinning because the
// alternative is not a defect: §3.7 records that the Trivy server placeholder
// is "https://trivy-server:4954", so the field has to accept two colons to hold
// a valid value. Forwarding ":" to the input is correct — which is exactly why
// `alt+:` exists and why the value has to be validated rather than trusted.
func TestAColonTypedIntoTheTrivyServerFieldIsACharacter(t *testing.T) {
	m := newTestModel(t)
	m.trivyServerInput.SetValue("")
	m.focusedField = 7 // the Trivy server field
	m.focusTextField(7)

	next := feed(t, m, testutil.Key(":"))

	if next.trivyServerInput.Value() != ":" {
		t.Fatalf("the field holds %q, want the colon typed into it",
			next.trivyServerInput.Value())
	}
}

// ...and once it is there, the scan has to refuse rather than hand it to Trivy,
// which fails the whole run with a message naming neither DevDesk nor the
// setting the value came from.
func TestAScanIsRefusedWhileTheTrivyServerCannotBeUsed(t *testing.T) {
	m := newTestModel(t)
	m.targetPath = "/repos/devdesk"
	m.trivyServerInput.SetValue(":")

	next, cmd := m.startScan()
	after := next.(Model)

	if after.err == nil {
		t.Fatal("a scan was started with an address Trivy cannot parse")
	}
	if !strings.Contains(after.err.Error(), "scan.trivy_server") {
		t.Errorf("err = %v, want it to name the setting to fix", after.err)
	}
	if after.state == StateScanning {
		t.Error("the view moved to its scanning state anyway")
	}
	if cmd != nil {
		t.Error("a scan command was issued for an unusable server address")
	}
}

func TestAUsableTrivyServerDoesNotBlockAScan(t *testing.T) {
	m := newTestModel(t)
	m.targetPath = "/repos/devdesk"
	m.trivyServerInput.SetValue("https://trivy:4954")

	next, _ := m.startScan()

	if err := next.(Model).err; err != nil {
		t.Errorf("a valid address was refused: %v", err)
	}
}

// An empty field is client-server mode being off, not a broken address.
func TestAnEmptyTrivyServerDoesNotBlockAScan(t *testing.T) {
	m := newTestModel(t)
	m.targetPath = "/repos/devdesk"
	m.trivyServerInput.SetValue("")

	next, _ := m.startScan()

	if err := next.(Model).err; err != nil {
		t.Errorf("an empty address was refused: %v", err)
	}
}

// The field is persisted on every option toggle, so an all-space value would
// otherwise be saved and become a failure on some later scan from another view.
func TestTheSavedOptionsAreTrimmed(t *testing.T) {
	m := newTestModel(t)
	m.trivyServerInput.SetValue("  https://trivy:4954  ")
	m.gitleaksConfigInput.SetValue("  /repos/.gitleaks.toml  ")

	m.saveOptionsToConfig()

	if got := m.config.Scan.TrivyServer; got != "https://trivy:4954" {
		t.Errorf("trivy_server = %q, want it trimmed", got)
	}
	if got := m.config.Scan.GitleaksConfig; got != "/repos/.gitleaks.toml" {
		t.Errorf("gitleaks_config = %q, want it trimmed", got)
	}

	m.trivyServerInput.SetValue("   ")
	m.saveOptionsToConfig()
	if got := m.config.Scan.TrivyServer; got != "" {
		t.Errorf("trivy_server = %q, want an all-space value to persist as unset", got)
	}
}
