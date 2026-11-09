package app

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// DEVDESK-145 step 5: what only the router can show about the :sec activity
// column. A scan failure lives in jobs.Registry, so it outlives the view and
// whichever view ran the scan shows up here (D5: scans only).

const (
	secImage    = "nginx:1.27"
	secRepoName = "apirepo" // no test name contains it, so a row match is unambiguous
)

// stubImageNames makes the inventory load neither call docker nor drop the
// cached image: the machine "holds" exactly the given references.
func stubImageNames(t *testing.T, refs ...string) {
	t.Helper()
	old := docker.ImageNames
	docker.ImageNames = func() (map[string]struct{}, bool) {
		out := map[string]struct{}{}
		for _, r := range refs {
			out[r] = struct{}{}
		}
		return out, true
	}
	t.Cleanup(func() { docker.ImageNames = old })
}

// seedSecCaches writes one scanned repository (context "default") and one
// scanned image into the isolated HOME and returns the repository's path.
func seedSecCaches(t *testing.T) string {
	t.Helper()
	if got := config.CurrentContextName(); got != "default" {
		t.Fatalf("config.CurrentContextName() = %q under the isolated HOME, the seed assumes \"default\"", got)
	}
	repo := filepath.Join(t.TempDir(), secRepoName)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	wc, err := cache.NewWorkspaceScanCache("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := wc.Set(repo, cache.WorkspaceScanEntry{RepoPath: repo, Critical: 1, ScannedAt: at}); err != nil {
		t.Fatal(err)
	}
	ic, err := cache.NewImageScanCache()
	if err != nil {
		t.Fatal(err)
	}
	if err := ic.Set(secImage, cache.ImageScanEntry{High: 2, ScannedAt: at}); err != nil {
		t.Fatal(err)
	}
	return repo
}

// secRouter is a router at width x 30 on an isolated HOME holding the two
// scanned targets, and the repository's path.
func secRouter(t *testing.T, width int) (*App, string) {
	t.Helper()
	testutil.FastTimers(t, &animationFastInterval)
	a := routerAt(t, &bareView{}, width, 30)
	stubImageNames(t, secImage)
	return a, seedSecCaches(t)
}

// openSec switches to :sec and feeds the router what the view's Init loaded.
func openSec(t *testing.T, a *App) {
	t.Helper()
	_ = a.switchView(command.ViewSecurity) // its batch carries timers; Init is read below
	for _, msg := range testutil.Msgs(a.views[command.ViewSecurity].Init()) {
		a.Update(msg)
	}
	a.resize(a.width, a.height)
}

func secScreen(a *App) string { return ansi.Strip(a.views[command.ViewSecurity].View()) }

func secFooter(a *App) string {
	return ansi.Strip(a.views[command.ViewSecurity].(footerRenderer).RenderFooter(a.width))
}

// secRow is the screen line of the row naming name.
func secRow(t *testing.T, a *App, name string) string {
	t.Helper()
	return rowOf(t, secScreen(a), name)
}

func wantFailed(t *testing.T, a *App, name string) {
	t.Helper()
	if row := secRow(t, a, name); !strings.Contains(row, failedGlyph()) {
		t.Errorf("row %q has no %s: %q\n%s", name, failedGlyph(), row, secScreen(a))
	}
}

func wantNotFailed(t *testing.T, a *App, name string) {
	t.Helper()
	if row := secRow(t, a, name); strings.Contains(row, failedGlyph()) {
		t.Errorf("row %q shows %s: %q", name, failedGlyph(), row)
	}
}

// reloadSec presses ctrl+r and feeds back what the load returned.
func reloadSec(t *testing.T, a *App) {
	t.Helper()
	_, cmd := a.Update(testutil.Key("ctrl+r"))
	for _, msg := range testutil.Msgs(cmd) {
		a.Update(msg)
	}
}

// Criterion 4, repository: a scan that failed from ws shows on the same path.
func TestSecShowsAScanFailedFromWs(t *testing.T) {
	a, repo := secRouter(t, 180)
	failScanOnRow(t, a, repo)
	openSec(t, a)
	wantFailed(t, a, secRepoName)
	wantNotFailed(t, a, secImage)

	// The selected row (the repository sorts first) names the state in the footer.
	if got := secFooter(a); !strings.Contains(got, failedGlyph()) || !strings.Contains(got, "last scan failed") {
		t.Errorf("the footer of the failed selected row = %q, want the glyph and \"last scan failed\"", got)
	}
}

// Criterion 4, image: a scan that failed from OCI shows on the image row.
func TestSecShowsAScanFailedFromOCI(t *testing.T) {
	a, _ := secRouter(t, 180)
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "", "oci", secImage)})
	a.Update(ociresources.ImageScanFinishedMsg{ImageName: secImage, Err: errors.New("boom")})
	openSec(t, a)
	wantFailed(t, a, secImage)
	wantNotFailed(t, a, secRepoName)
}

// The red mark survives ctrl+r, a round trip through :jobs and a rebuilt view.
func TestSecFailureSurvivesRefreshLeavingAndARebuiltView(t *testing.T) {
	a, repo := secRouter(t, 180)
	failScanOnRow(t, a, repo)
	openSec(t, a)

	reloadSec(t, a)
	wantFailed(t, a, secRepoName)

	a.switchView(command.ViewJobs)
	openSec(t, a)
	wantFailed(t, a, secRepoName)

	// A config save or a context switch rebuilds every view.
	delete(a.views, command.ViewSecurity)
	openSec(t, a)
	wantFailed(t, a, secRepoName)
}

// A new attempt (S) clears the mark: queued, running, then the icon again.
func TestSecRetryClearsTheFailure(t *testing.T) {
	a, repo := secRouter(t, 180)
	failScanOnRow(t, a, repo)
	openSec(t, a)
	wantFailed(t, a, secRepoName)

	_, cmd := a.Update(testutil.Key("S"))
	// Only the start message is fed: running its work would start trivy.
	var start *jobs.StartMsg
	for _, msg := range testutil.Msgs(cmd) {
		if s, ok := msg.(jobs.StartMsg); ok {
			start = &s
		}
	}
	if start == nil {
		t.Fatal("S on the failed row started nothing")
	}
	a.Update(*start)
	row := secRow(t, a, secRepoName)
	if strings.Contains(row, failedGlyph()) {
		t.Errorf("a retry in flight still shows %s: %q", failedGlyph(), row)
	}
	if !strings.Contains(row, indicator.Glyph(indicator.Queued, true)) {
		t.Errorf("a retry waiting to start has no %s: %q", indicator.Glyph(indicator.Queued, true), row)
	}

	a.Update(security.InventoryScanStartingMsg{Name: repo})
	// The running dot blinks: either phase is the running state.
	if row := secRow(t, a, secRepoName); !strings.Contains(row, indicator.Glyph(indicator.Running, true)) &&
		!strings.Contains(row, indicator.Glyph(indicator.Running, false)) {
		t.Errorf("a running retry shows neither phase of the running dot: %q", row)
	}

	a.Update(security.InventoryScanFinishedMsg{Name: repo, ScannedAt: time.Now()})
	wantNotFailed(t, a, secRepoName)
}

// D5: a sync that failed on the same path is not a scan failure.
func TestSecIgnoresASyncFailure(t *testing.T) {
	a, repo := secRouter(t, 180)
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindSync, command.ViewWorkspaces, "", "ws", repo)})
	a.Update(workspaces.WorkspaceSyncCompleteMsg{RepoPath: repo, Error: errors.New("boom")})
	openSec(t, a)
	wantNotFailed(t, a, secRepoName)
}

// The memory is per context: hidden in "other", back in "default".
func TestSecFailureIsFilteredByContext(t *testing.T) {
	a, repo := secRouter(t, 180)
	failScanOnRow(t, a, repo)

	a.currentContext = "other"
	delete(a.views, command.ViewSecurity)
	openSec(t, a)
	// The workspace cache is bound to the current context name, which the
	// loader reads from disk, so the row is still listed under "default".
	wantNotFailed(t, a, secRepoName)

	a.currentContext = "default"
	delete(a.views, command.ViewSecurity)
	openSec(t, a)
	wantFailed(t, a, secRepoName)
}

// dirHash digests every file under dir (path and bytes), and lists them.
func dirHash(t *testing.T, dir string) (digest [32]byte, files []string) {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files = append(files, rel)
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(b)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	sort.Strings(files)
	copy(digest[:], h.Sum(nil))
	return digest, files
}

// Criterion 5 (Rule 126): showing, reloading and animating the failure reads
// the scan caches and never writes them.
func TestSecDisplayNeverWritesTheScanCaches(t *testing.T) {
	a, repo := secRouter(t, 180)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(home, ".devdesk", "cache")
	before, filesBefore := dirHash(t, cacheDir)
	if len(filesBefore) == 0 {
		t.Fatalf("the seed left nothing under %s", cacheDir)
	}

	failScanOnRow(t, a, repo)
	openSec(t, a)
	wantFailed(t, a, secRepoName)
	a.Update(testutil.Key("down"))
	a.Update(testutil.Key("up"))
	for range 3 {
		a.sendFrameTo(command.ViewSecurity)
	}
	reloadSec(t, a)
	_ = a.views[command.ViewSecurity].View()
	_ = secFooter(a)

	after, filesAfter := dirHash(t, cacheDir)
	if before != after || strings.Join(filesBefore, ",") != strings.Join(filesAfter, ",") {
		t.Errorf("the scan caches changed while :sec displayed a failure (Rule 126):\nbefore %v\nafter  %v", filesBefore, filesAfter)
	}
}

// Rule 116 on the whole view at 60 columns, failed row included.
func TestSecFullViewFitsSixtyColumns(t *testing.T) {
	a, repo := secRouter(t, 60)
	failScanOnRow(t, a, repo)
	openSec(t, a)
	wantFailed(t, a, secRepoName)

	for i, line := range strings.Split(a.View(), "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Errorf("line %d is %d cells wide at 60 columns: %q", i, w, ansi.Strip(line))
		}
	}
	tables := a.views[command.ViewSecurity].(activityView).ActivityTables()
	tb, ok := tables["sec/inventory"]
	if !ok {
		t.Fatalf("no sec/inventory table reported: %v", sortedKeys(tables))
	}
	if got, want := tb.RenderedWidth(), 60-2; got != want {
		t.Errorf("RenderedWidth() = %d at 60 columns, want %d (Rule 116)", got, want)
	}
}

// seedSecurityFooter puts :sec on screen with a running scan on its selected
// row, for the activity guard.
func seedSecurityFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	stubImageNames(t, secImage)
	repo := seedSecCaches(t)
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewSecurity, "", "inventory", repo)})
	snap := a.jobs.Snapshot()
	if len(snap) != 1 || !a.jobs.Advance(snap[0].ID, repo, jobs.ItemRunning, "") {
		t.Fatalf("the seed could not put the scan in the running state: %+v", snap)
	}
	openSec(t, a)
	return indicator.Running
}
