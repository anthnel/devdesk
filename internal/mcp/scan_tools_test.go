package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/scan"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The secret itself never leaves the process. This is the one test worth failing
// loudly: it asserts on the **serialised** answer rather than on the struct,
// because the struct is not what crosses the pipe — and it looks for the string
// anywhere at all, not for a field name, since a Match copied into a Title or a
// Description by some later change would pass a field-shaped check.
func TestTheMatchedStringOfASecretNeverLeaves(t *testing.T) {
	const secret = "AKIAIOSFODNN7EXAMPLE-do-not-leak-me"

	fakeCacheHome(t)
	storeImageResult(t, "app:1.0", &scan.Result{
		Target: "app:1.0",
		Findings: []scan.Finding{{
			ID: "gl-1", Title: "AWS access key", Severity: "CRITICAL",
			Source: scan.SourceGitleaks, File: "config/prod.env", Line: 12,
			Match: secret, Fingerprint: "config/prod.env:aws-access-key:12",
		}},
	})

	cs := connect(t, testEnv(nil))
	raw := callToolRaw(t, cs, "scan_result", map[string]any{"target": "app:1.0"})

	if strings.Contains(raw, secret) {
		t.Fatalf("the matched string of a secret finding reached the client:\n%s", raw)
	}
	if strings.Contains(raw, "\"match\"") {
		t.Errorf("the answer carries a match field:\n%s", raw)
	}
	// The contrast: what identifies the finding does go out, or the tool would
	// be useless for triage.
	for _, want := range []string{"config/prod.env", "AWS access key", "aws-access-key"} {
		if !strings.Contains(raw, want) {
			t.Errorf("the answer does not carry %q, which identifies the finding without reproducing it:\n%s", want, raw)
		}
	}
}

func TestScanResultReportsTheSummaryAndTheFindings(t *testing.T) {
	fakeCacheHome(t)
	storeImageResult(t, "app:1.0", &scan.Result{
		Target:         "app:1.0",
		Counts:         scan.SeverityCounts{Critical: 1, High: 2},
		SecretCount:    1,
		SecretsScanned: true,
		Findings: []scan.Finding{
			{ID: "CVE-1", Severity: "CRITICAL", Source: scan.SourceTrivy, PkgName: "openssl"},
			{ID: "CVE-2", Severity: "HIGH", Source: scan.SourceTrivy, PkgName: "curl"},
			{ID: "CVE-3", Severity: "HIGH", Source: scan.SourceTrivy, PkgName: "zlib"},
			{ID: "gl-1", Severity: "CRITICAL", Source: scan.SourceGitleaks, Match: "x"},
		},
	})

	var out scanResultOut
	callTool(t, connect(t, testEnv(nil)), "scan_result", map[string]any{"target": "app:1.0"}, &out)

	if out.Total != 4 || out.Matched != 4 || len(out.Findings) != 4 {
		t.Errorf("total=%d matched=%d returned=%d, want 4/4/4", out.Total, out.Matched, len(out.Findings))
	}
	if out.Critical != 1 || out.High != 2 {
		t.Errorf("counts = %d critical / %d high, want 1/2", out.Critical, out.High)
	}
	if !out.SecretsScanned {
		t.Error("secrets_scanned = false for a scan whose secret stage ran")
	}
}

// A finding's family is a word an agent filters on, so it is part of the
// contract. scan.Category is an int with no String method — a conversion would
// have produced a rune.
func TestAFindingCarriesItsCategoryAsAWord(t *testing.T) {
	fakeCacheHome(t)
	storeImageResult(t, "app:1.0", &scan.Result{
		Target: "app:1.0",
		Findings: []scan.Finding{
			{ID: "CVE-1", Severity: "HIGH", Source: scan.SourceTrivy},
			{ID: "gl-1", Severity: "HIGH", Source: scan.SourceGitleaks},
			{ID: "lic-1", Severity: "LOW", Source: scan.SourceTrivyLicense},
			{ID: "mis-1", Severity: "MEDIUM", Source: scan.SourceTrivyMisconfig},
		},
	})

	var out scanResultOut
	callTool(t, connect(t, testEnv(nil)), "scan_result", map[string]any{"target": "app:1.0"}, &out)

	want := map[string]string{
		"CVE-1": "vulnerability", "gl-1": "secret",
		"lic-1": "license", "mis-1": "misconfiguration",
	}
	for _, f := range out.Findings {
		if f.Category != want[f.ID] {
			t.Errorf("finding %s has category %q, want %q", f.ID, f.Category, want[f.ID])
		}
	}
}

// A source nobody has taught Categorize about stays visible in the family that
// gets looked at first, rather than disappearing into a name nothing filters
// for. Same rule as Categorize's own default.
func TestAnUnknownSourceStaysVisible(t *testing.T) {
	if got := categoryName(scan.Categorize(scan.Finding{Source: "some-new-scanner"})); got != "vulnerability" {
		t.Errorf("an unknown source is categorised as %q, want vulnerability", got)
	}
}

func TestSeverityAndCategoryNarrowTheFindings(t *testing.T) {
	fakeCacheHome(t)
	storeImageResult(t, "app:1.0", &scan.Result{
		Target: "app:1.0",
		Findings: []scan.Finding{
			{ID: "CVE-1", Severity: "CRITICAL", Source: scan.SourceTrivy},
			{ID: "CVE-2", Severity: "LOW", Source: scan.SourceTrivy},
			{ID: "gl-1", Severity: "CRITICAL", Source: scan.SourceGitleaks},
		},
	})
	cs := connect(t, testEnv(nil))

	var bySeverity scanResultOut
	callTool(t, cs, "scan_result", map[string]any{
		"target": "app:1.0", "severity": []any{"critical"},
	}, &bySeverity)
	if bySeverity.Matched != 2 {
		t.Errorf("severity CRITICAL matched %d findings, want 2", bySeverity.Matched)
	}
	if bySeverity.Total != 3 {
		t.Errorf("total = %d under a filter, want the scan's own 3", bySeverity.Total)
	}

	var byCategory scanResultOut
	callTool(t, cs, "scan_result", map[string]any{
		"target": "app:1.0", "category": []any{"secret"},
	}, &byCategory)
	if byCategory.Matched != 1 || byCategory.Findings[0].ID != "gl-1" {
		t.Errorf("category secret matched %d findings (%+v), want the one", byCategory.Matched, byCategory.Findings)
	}
}

func TestTheFindingsArePagedAndTheLimitIsCapped(t *testing.T) {
	fakeCacheHome(t)
	many := make([]scan.Finding, 250)
	for i := range many {
		many[i] = scan.Finding{ID: "CVE-" + itoa(i), Severity: "HIGH", Source: scan.SourceTrivy}
	}
	storeImageResult(t, "app:1.0", &scan.Result{Target: "app:1.0", Findings: many})
	cs := connect(t, testEnv(nil))

	var first scanResultOut
	callTool(t, cs, "scan_result", map[string]any{"target": "app:1.0"}, &first)
	if len(first.Findings) != defaultFindingLimit {
		t.Errorf("an unpaged call returned %d findings, want the default %d", len(first.Findings), defaultFindingLimit)
	}
	if first.Matched != 250 {
		t.Errorf("matched = %d, want every finding — paging must not change the count", first.Matched)
	}

	var second scanResultOut
	callTool(t, cs, "scan_result", map[string]any{"target": "app:1.0", "offset": 200, "limit": 500}, &second)
	if len(second.Findings) != 50 {
		t.Errorf("offset 200 returned %d findings, want the remaining 50", len(second.Findings))
	}
	if second.Findings[0].ID != "CVE-200" {
		t.Errorf("offset 200 starts at %s, want CVE-200", second.Findings[0].ID)
	}

	// The cap protects the agent's own context, so a caller cannot waive it.
	var greedy scanResultOut
	callTool(t, cs, "scan_result", map[string]any{"target": "app:1.0", "limit": 99999}, &greedy)
	if len(greedy.Findings) > maxFindingLimit {
		t.Errorf("a limit of 99999 returned %d findings, want at most %d", len(greedy.Findings), maxFindingLimit)
	}
}

// An offset past the end is an empty page, not an error and not the last page:
// paging stops rather than wrapping.
func TestAnOffsetPastTheEndIsAnEmptyPage(t *testing.T) {
	fakeCacheHome(t)
	storeImageResult(t, "app:1.0", &scan.Result{
		Target:   "app:1.0",
		Findings: []scan.Finding{{ID: "CVE-1", Severity: "HIGH", Source: scan.SourceTrivy}},
	})

	var out scanResultOut
	callTool(t, connect(t, testEnv(nil)), "scan_result", map[string]any{"target": "app:1.0", "offset": 500}, &out)

	if len(out.Findings) != 0 {
		t.Errorf("an offset past the end returned %d findings, want none", len(out.Findings))
	}
	if out.Matched != 1 {
		t.Errorf("matched = %d, want 1 — the count is of what the filters kept, not of the page", out.Matched)
	}
}

// A target the caller invented gets an error naming what to call instead. It
// must be a *tool* error rather than a protocol one, or the agent sees a
// transport failure for a question that was merely wrong.
func TestAnUnknownTargetIsReportedAsAToolError(t *testing.T) {
	fakeCacheHome(t)
	cs := connect(t, testEnv(nil))

	res := callToolExpectingError(t, cs, "scan_result", map[string]any{"target": "never:scanned"})

	if !strings.Contains(res, "scan_inventory") {
		t.Errorf("the error does not say where to find a valid target: %s", res)
	}
}

// ── scan_inventory ──────────────────────────────────────────────────────────

func TestTheInventoryListsBothCachesSortedByCritical(t *testing.T) {
	fakeCacheHome(t)
	storeImageEntry(t, "low:1", cache.ImageScanEntry{Critical: 1})
	storeImageEntry(t, "high:1", cache.ImageScanEntry{Critical: 9})
	repo := existingDir(t)
	storeWorkspaceEntry(t, "work", repo, cache.WorkspaceScanEntry{Critical: 5})
	stubImages(t, "low:1", "high:1")

	var out scanInventoryOut
	callTool(t, connect(t, testEnv(nil)), "scan_inventory", nil, &out)

	if len(out.Targets) != 3 {
		t.Fatalf("the inventory holds %d targets (%+v), want 3", len(out.Targets), out.Targets)
	}
	if out.Targets[0].Name != "high:1" || out.Targets[1].Name != repo {
		t.Errorf("the inventory is not sorted by CRITICAL descending: %+v", out.Targets)
	}
	if out.Context != "work" {
		t.Errorf("context = %q, want the served one", out.Context)
	}
}

// §3.37: an image the daemon no longer has is dropped.
func TestAnImageThatIsGoneIsNotListed(t *testing.T) {
	fakeCacheHome(t)
	storeImageEntry(t, "kept:1", cache.ImageScanEntry{Critical: 1})
	storeImageEntry(t, "removed:1", cache.ImageScanEntry{Critical: 2})
	stubImages(t, "kept:1")

	var out scanInventoryOut
	callTool(t, connect(t, testEnv(nil)), "scan_inventory", nil, &out)

	if len(out.Targets) != 1 || out.Targets[0].Name != "kept:1" {
		t.Errorf("the inventory holds %+v, want only kept:1", out.Targets)
	}
}

// The other half of §3.37's rule, and the one that matters more: an enumeration
// that *failed* keeps every image. "Docker is not running" and "the image is
// gone" are the same silence, and reading the first as the second empties the
// inventory the moment the daemon stops.
func TestADaemonThatCannotBeReachedKeepsEveryImage(t *testing.T) {
	fakeCacheHome(t)
	storeImageEntry(t, "one:1", cache.ImageScanEntry{Critical: 1})
	storeImageEntry(t, "two:1", cache.ImageScanEntry{Critical: 2})
	stubImagesUnavailable(t)

	var out scanInventoryOut
	callTool(t, connect(t, testEnv(nil)), "scan_inventory", nil, &out)

	if len(out.Targets) != 2 {
		t.Errorf("a failed image list left %d targets (%+v), want both kept", len(out.Targets), out.Targets)
	}
}

func TestARepositoryThatIsGoneIsNotListed(t *testing.T) {
	fakeCacheHome(t)
	stubImages(t)
	kept := existingDir(t)
	storeWorkspaceEntry(t, "work", kept, cache.WorkspaceScanEntry{Critical: 1})
	storeWorkspaceEntry(t, "work", filepath.Join(t.TempDir(), "deleted"), cache.WorkspaceScanEntry{Critical: 2})

	var out scanInventoryOut
	callTool(t, connect(t, testEnv(nil)), "scan_inventory", nil, &out)

	if len(out.Targets) != 1 || out.Targets[0].Name != kept {
		t.Errorf("the inventory holds %+v, want only the directory that is still there", out.Targets)
	}
}

// D20 through a protocol: nil means nobody looked, and a server that rendered it
// false would report a clean target from a scan that examined nothing.
func TestASecretVerdictNobodyReachedStaysAbsent(t *testing.T) {
	fakeCacheHome(t)
	yes := true
	storeImageEntry(t, "looked:1", cache.ImageScanEntry{Sensitive: &yes})
	storeImageEntry(t, "unlooked:1", cache.ImageScanEntry{})
	stubImages(t, "looked:1", "unlooked:1")

	var out scanInventoryOut
	callTool(t, connect(t, testEnv(nil)), "scan_inventory", nil, &out)

	for _, target := range out.Targets {
		switch target.Name {
		case "looked:1":
			if target.Sensitive == nil || !*target.Sensitive {
				t.Errorf("looked:1 reports %v, want true", target.Sensitive)
			}
		case "unlooked:1":
			if target.Sensitive != nil {
				t.Errorf("unlooked:1 reports %v, want absent — nobody looked", *target.Sensitive)
			}
		}
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

// fakeCacheHome points os.UserHomeDir at a temp directory, so no test reads or
// writes the developer's own ~/.devdesk — which every worktree shares.
func fakeCacheHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	return home
}

// The fixtures go in through the real writers, so the read-only paths are
// checked against what actually gets written rather than against a hand-built
// file that could be wrong in the same way twice.

func storeImageEntry(t *testing.T, name string, entry cache.ImageScanEntry) {
	t.Helper()
	if entry.ScannedAt.IsZero() {
		entry.ScannedAt = time.Now().Add(-time.Hour)
	}
	c, err := cache.NewImageScanCache()
	if err != nil {
		t.Fatalf("open image cache: %v", err)
	}
	if err := c.Set(name, entry); err != nil {
		t.Fatalf("store image entry: %v", err)
	}
}

func storeWorkspaceEntry(t *testing.T, contextName, path string, entry cache.WorkspaceScanEntry) {
	t.Helper()
	if entry.ScannedAt.IsZero() {
		entry.ScannedAt = time.Now().Add(-time.Hour)
	}
	c, err := cache.NewWorkspaceScanCache(contextName)
	if err != nil {
		t.Fatalf("open workspace cache: %v", err)
	}
	if err := c.Set(path, entry); err != nil {
		t.Fatalf("store workspace entry: %v", err)
	}
}

func storeImageResult(t *testing.T, name string, result *scan.Result) {
	t.Helper()
	if err := cache.SaveImageScanResult(name, result); err != nil {
		t.Fatalf("store image result: %v", err)
	}
}

func existingDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	return dir
}

// stubImages replaces the docker seam with an enumeration that succeeds, naming
// exactly these images. stubImagesUnavailable replaces it with one that fails.
// Les deux coutures, parce que les deux outils ne demandent pas la même chose :
// scan_inventory réconcilie sur un ensemble de noms (docker.ImageNames),
// images_list projette l'Image entière — taille, âge, conteneurs (listImages).
// Les laisser diverger ferait passer un test pour la mauvaise raison.
func stubImages(t *testing.T, names ...string) {
	t.Helper()
	present := make(map[string]struct{}, len(names))
	images := make([]docker.Image, 0, len(names))
	for _, name := range names {
		present[name] = struct{}{}
		repo, tag, _ := strings.Cut(name, ":")
		images = append(images, docker.Image{Repository: repo, Tag: tag})
	}
	swapImageNames(t, func() (map[string]struct{}, bool) { return present, true })
	swapListImages(t, func() ([]docker.Image, error) { return images, nil })
}

func stubImagesUnavailable(t *testing.T) {
	t.Helper()
	swapImageNames(t, func() (map[string]struct{}, bool) { return nil, false })
	swapListImages(t, func() ([]docker.Image, error) { return nil, os.ErrNotExist })
}

func swapImageNames(t *testing.T, fn func() (map[string]struct{}, bool)) {
	t.Helper()
	previous := docker.ImageNames
	docker.ImageNames = fn
	t.Cleanup(func() { docker.ImageNames = previous })
}

func swapListImages(t *testing.T, fn func() ([]docker.Image, error)) {
	t.Helper()
	previous := listImages
	listImages = fn
	t.Cleanup(func() { listImages = previous })
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// callToolRaw returns the answer as it is serialised, which is what actually
// crosses the pipe — a struct round trip would hide a field the projection
// forgot to drop.
func callToolRaw(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool %s reported a tool error: %+v", name, res.Content)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	return string(data)
}

func callToolExpectingError(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s failed at the protocol level, want a tool error: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("CallTool %s succeeded, want a tool error", name)
	}
	data, err := json.Marshal(res.Content)
	if err != nil {
		t.Fatalf("marshal error content: %v", err)
	}
	return string(data)
}
