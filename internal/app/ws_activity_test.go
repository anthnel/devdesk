package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// DEVDESK-144 step 5: what only the router can show about the ws activity
// column. The failure lives in jobs.Registry, so it outlives the view.

// wsRouter hosts the real ws view on a workspaces directory holding one git
// repository, "api". It returns the router and the repository's path.
func wsRouter(t *testing.T) (*App, string) {
	t.Helper()
	testutil.FastTimers(t, &animationBlinkInterval)
	a := routerAt(t, &bareView{}, 180, 40)
	dir := t.TempDir()
	repo := filepath.Join(dir, "api")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.config.App.WorkspacesDir = dir
	return a, repo
}

// openWorkspaces switches to ws and feeds the router what the view's Init
// loaded (listing and scan cache), as the runtime would.
func openWorkspaces(t *testing.T, a *App) {
	t.Helper()
	switchCmd := a.switchView(command.ViewWorkspaces)
	_ = switchCmd // its batch carries tick timers; the listing is read below
	for _, msg := range testutil.Msgs(a.views[command.ViewWorkspaces].Init()) {
		a.Update(msg)
	}
	a.resize(a.width, a.height)
}

// wsScreen is the ws view's body, ANSI stripped.
func wsScreen(a *App) string { return ansi.Strip(a.views[command.ViewWorkspaces].Render()) }

// rowOf returns the screen line naming name.
func rowOf(t *testing.T, screen, name string) string {
	t.Helper()
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("no row names %q:\n%s", name, screen)
	return ""
}

func failScanOnRow(t *testing.T, a *App, repo string) {
	t.Helper()
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewWorkspaces, "", "ws", repo)})
	a.Update(workspaces.WorkspaceScanCompleteMsg{RepoPath: repo, Error: errors.New("boom")})
}

func failedGlyph() string { return indicator.Glyph(indicator.Failed, true) }

// A-07 criterion 6: the red mark survives ctrl+r.
func TestWsFailureSurvivesRefresh(t *testing.T) {
	a, repo := wsRouter(t)
	openWorkspaces(t, a)
	failScanOnRow(t, a, repo)
	if !strings.Contains(rowOf(t, wsScreen(a), "api"), failedGlyph()) {
		t.Fatalf("the failed scan left no %s on the row:\n%s", failedGlyph(), wsScreen(a))
	}

	_, cmd := a.Update(testutil.Key("ctrl+r"))
	for _, msg := range testutil.Msgs(cmd) {
		a.Update(msg)
	}
	if !strings.Contains(rowOf(t, wsScreen(a), "api"), failedGlyph()) {
		t.Errorf("ctrl+r cleared the %s:\n%s", failedGlyph(), wsScreen(a))
	}
}

// Leaving ws for :jobs and coming back keeps it, and so does a rebuilt view.
func TestWsFailureSurvivesLeavingAndARebuiltView(t *testing.T) {
	a, repo := wsRouter(t)
	openWorkspaces(t, a)
	failScanOnRow(t, a, repo)

	a.switchView(command.ViewJobs)
	openWorkspaces(t, a)
	if !strings.Contains(rowOf(t, wsScreen(a), "api"), failedGlyph()) {
		t.Errorf("coming back from :jobs lost the %s", failedGlyph())
	}

	// A config save or a context switch rebuilds every view.
	delete(a.views, command.ViewWorkspaces)
	openWorkspaces(t, a)
	if !strings.Contains(rowOf(t, wsScreen(a), "api"), failedGlyph()) {
		t.Errorf("a rebuilt view lost the %s; the registry snapshot was not re-delivered on enter:\n%s", failedGlyph(), wsScreen(a))
	}
}

// The memory is per context: hidden in B, back in A.
func TestWsFailureIsFilteredByContext(t *testing.T) {
	a, repo := wsRouter(t)
	openWorkspaces(t, a)
	failScanOnRow(t, a, repo)

	a.currentContext = "other"
	delete(a.views, command.ViewWorkspaces)
	openWorkspaces(t, a)
	if strings.Contains(rowOf(t, wsScreen(a), "api"), failedGlyph()) {
		t.Errorf("context other shows the failure of context default")
	}

	a.currentContext = "default"
	delete(a.views, command.ViewWorkspaces)
	openWorkspaces(t, a)
	if !strings.Contains(rowOf(t, wsScreen(a), "api"), failedGlyph()) {
		t.Errorf("back in context default the failure is gone")
	}
}

// A-07 criterion 7, the :sec contract: (KindScan, TargetKey(abs path), context).
func TestWsScanFailureKeyIsTheOneSecReads(t *testing.T) {
	a, repo := wsRouter(t)
	openWorkspaces(t, a)

	// entry.Path is absolute (H3): the listing joins the expanded directory.
	var loaded *workspaces.EntriesLoadedMsg
	for _, msg := range testutil.Msgs(a.views[command.ViewWorkspaces].Init()) {
		if l, ok := msg.(workspaces.EntriesLoadedMsg); ok {
			loaded = &l
		}
	}
	if loaded == nil || len(loaded.Entries) != 1 {
		t.Fatalf("the listing returned %+v, want the one repository", loaded)
	}
	if got := loaded.Entries[0].Path; !filepath.IsAbs(got) || got != repo {
		t.Fatalf("entry.Path = %q, want the absolute %q", got, repo)
	}

	failScanOnRow(t, a, loaded.Entries[0].Path)
	f, ok := jobs.FailureFor(a.jobs.Failures(), jobs.KindScan, jobs.TargetKey(repo), "default")
	if !ok {
		t.Fatalf("no scan failure under (KindScan, %q, default): %+v", jobs.TargetKey(repo), a.jobs.Failures())
	}
	if f.Target != filepath.Clean(repo) || !filepath.IsAbs(f.Target) {
		t.Errorf("Target = %q, want the cleaned absolute path", f.Target)
	}
	if got := jobs.FailuresOn(a.jobs.Failures(), "default", repo, jobs.KindScan); len(got) != 1 {
		t.Errorf("FailuresOn(KindScan) = %d failures, want 1", len(got))
	}
}

// D5: a sync failure on the same path is not a scan failure for :sec.
func TestWsSyncFailureIsInvisibleToSec(t *testing.T) {
	a, repo := wsRouter(t)
	openWorkspaces(t, a)
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindSync, command.ViewWorkspaces, "", "ws", repo)})
	a.Update(workspaces.WorkspaceSyncCompleteMsg{RepoPath: repo, Error: errors.New("boom")})

	if _, ok := jobs.FailureFor(a.jobs.Failures(), jobs.KindSync, repo, "default"); !ok {
		t.Fatal("the sync failure was not remembered at all")
	}
	if got := jobs.FailuresOn(a.jobs.Failures(), "default", repo, jobs.KindScan); len(got) != 0 {
		t.Errorf("a sync failure is visible to :sec through KindScan: %+v", got)
	}
}

// seedWorkspacesFooter puts ws on screen with a running scan on its selected
// row, for the activity guard.
func seedWorkspacesFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "api")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.config.App.WorkspacesDir = dir
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewWorkspaces, "", "ws", repo)})
	snap := a.jobs.Snapshot()
	if len(snap) != 1 || !a.jobs.Advance(snap[0].ID, repo, jobs.ItemRunning, "") {
		t.Fatalf("the seed could not put the scan in the running state: %+v", snap)
	}
	openWorkspaces(t, a)
	return indicator.Running
}
