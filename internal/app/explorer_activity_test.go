package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/forge/explorer"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/jobsview"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-147 / DEVDESK-225 step 5: what only the router can show about the
// explorer's activity column. The real explorer is hosted with a forge client
// that is never called: every Cmd that could reach it is dropped, and the
// completion messages are fed by hand.

func explorerRoots() []*explorer.TreeNode {
	return []*explorer.TreeNode{
		{ID: "1", Name: "alpha", FullPath: "alpha", Type: explorer.NodeTypeGroup, Visibility: "private"},
		{ID: "2", Name: "beta", FullPath: "beta", Type: explorer.NodeTypeGroup, Visibility: "private"},
	}
}

// openExplorer authenticates the shared state (no network: the client is only
// built), opens the explorer and feeds it the two root groups. alpha is the
// selected row.
func openExplorer(t *testing.T, a *App) {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	testutil.FastTimers(t, &animationFastInterval)
	a.sharedState.Forge = gitlabforge.NewWithClient(nil, "https://gitlab.example.com")
	a.sharedState.IsAuthenticated = true
	_ = a.switchView(command.ViewGitExplorer) // its Init would list the groups: dropped
	a.Update(explorer.RootGroupsLoadedMsg{Nodes: explorerRoots()})
	a.resize(a.width, a.height)
}

func explorerRouter(t *testing.T) *App {
	t.Helper()
	a := routerAt(t, &bareView{}, 180, 40)
	openExplorer(t, a)
	return a
}

// reopenExplorer rebuilds the view, as a config save or a context switch does.
func reopenExplorer(t *testing.T, a *App) {
	t.Helper()
	delete(a.views, command.ViewGitExplorer)
	_ = a.switchView(command.ViewGitExplorer)
	a.Update(explorer.RootGroupsLoadedMsg{Nodes: explorerRoots()})
	a.resize(a.width, a.height)
}

func explorerScreen(a *App) string { return ansi.Strip(a.views[command.ViewGitExplorer].Render()) }

func explorerFooter(a *App) string {
	return ansi.Strip(a.views[command.ViewGitExplorer].(footerRenderer).RenderFooter(120))
}

// feedAll hands every message a command produces back to the router, the way
// the runtime does. Only for commands known not to reach a forge.
func feedAll(a *App, cmd tea.Cmd) {
	for _, msg := range testutil.Msgs(cmd) {
		a.Update(msg)
	}
}

// startFrom returns the run the explorer asked the router to register, and
// registers it, through the router and without running its work.
func startFrom(t *testing.T, a *App, cmd tea.Cmd) jobs.StartMsg {
	t.Helper()
	start, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		t.Fatalf("the explorer registered no run, got %T", testutil.Msg(cmd))
	}
	a.Update(start)
	return start
}

// startDelete deletes the selected row the way a user does: D, then confirm.
func startDelete(t *testing.T, a *App) jobs.StartMsg {
	t.Helper()
	a.Update(testutil.Key(keymap.Delete))
	_, cmd := a.Update(components.OptionConfirmModalYesMsg{})
	return startFrom(t, a, cmd)
}

// startCreate submits the creation form for a group named name.
func startCreate(t *testing.T, a *App, name string) jobs.StartMsg {
	t.Helper()
	_, cmd := a.Update(components.CreationFormSubmitMsg{
		FormType: components.FormTypeGroup, Name: name, Visibility: "private",
	})
	return startFrom(t, a, cmd)
}

func runningGlyphs() string {
	return indicator.Glyph(indicator.Running, true) + indicator.Glyph(indicator.Running, false)
}

func wantRow(t *testing.T, a *App, name string, glyphs string, present bool) {
	t.Helper()
	row := rowOf(t, explorerScreen(a), name)
	if has := strings.ContainsAny(row, glyphs); has != present {
		t.Errorf("explorer row %q: glyph(s) %q present = %v, want %v: %q", name, glyphs, has, present, row)
	}
}

// failDelete runs the whole delete of the selected row, which fails.
func failDelete(t *testing.T, a *App) {
	t.Helper()
	start := startDelete(t, a)
	_, cmd := a.Update(explorer.DeleteCompleteMsg{Target: start.Run.Items[0].Target, Error: errors.New("boom")})
	feedAll(a, cmd) // includes the expiry of the footer event
}

// Criterion 4, end to end: a create or delete the explorer started is running
// in the registry, and in :jobs `K` is greyed with its reason and posts no
// CancelMsg. The run comes from the explorer itself, not from a hand-built
// constructor, so a regression in createRun/deleteRun is seen here.
func TestAnExplorerCreateOrDeleteCannotBeStoppedFromJobs(t *testing.T) {
	tests := []struct {
		kind  jobs.Kind
		label string
		start func(*testing.T, *App) jobs.StartMsg
	}{
		{jobs.KindDelete, "alpha", startDelete},
		{jobs.KindCreate, "newgroup", func(t *testing.T, a *App) jobs.StartMsg { return startCreate(t, a, "newgroup") }},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			a := explorerRouter(t)
			start := tt.start(t, a)
			if start.Run.Kind != tt.kind || start.Run.Origin != command.ViewGitExplorer {
				t.Fatalf("the explorer started %s from %s, want %s from the explorer", start.Run.Kind, start.Run.Origin, tt.kind)
			}

			snap := a.jobs.Snapshot()
			if len(snap) != 1 || snap[0].Items[0].State != jobs.ItemRunning {
				t.Fatalf("snapshot = %+v, want one run with a running item", snap)
			}
			if snap[0].Stoppable() {
				t.Errorf("a %s in flight is Stoppable()", tt.kind)
			}

			testutil.Msgs(a.switchView(command.ViewJobs))
			a.resize(a.width, a.height)
			view, ok := a.views[command.ViewJobs].(jobsview.Model)
			if !ok {
				t.Fatalf("the jobs view is a %T", a.views[command.ViewJobs])
			}

			// The selected row is this run, not some other one.
			if row := rowOf(t, ansi.Strip(view.Render()), tt.label); !strings.Contains(row, string(tt.kind)) {
				t.Errorf("the selected :jobs row %q does not name the %s", row, tt.kind)
			}
			sc := view.GetShortcuts()
			if !testutil.HasShortcut(sc, "K") {
				t.Fatal("K was dropped rather than greyed (Rule 130)")
			}
			if !testutil.ShortcutDisabled(sc, "K") {
				t.Errorf("K is offered for a %s in flight", tt.kind)
			}

			_, cmd := a.Update(testutil.Key("K"))
			// stopRefusal: the reason is the one in the run's own words. Read it
			// before the batch is drained, which also runs the footer's expiry.
			want := "A " + string(tt.kind) + " cannot be stopped once it has started"
			if footer := ansi.Strip(a.views[command.ViewJobs].(footerRenderer).RenderFooter(120)); !strings.Contains(footer, want) {
				t.Errorf("pressing K did not say why: want %q in\n%s", want, footer)
			}
			for _, msg := range testutil.Msgs(cmd) {
				switch msg.(type) {
				case jobs.CancelMsg, jobs.CancelItemMsg:
					t.Errorf("K posted %T for a %s in flight", msg, tt.kind)
				}
			}
			if got := a.jobs.Snapshot()[0].Items[0].State; got != jobs.ItemRunning {
				t.Errorf("item state after K = %q, want running", got)
			}
		})
	}
}

// Criterion 1: the row blinks from admission, not from some later transition.
func TestAnExplorerDeleteBlinksTheRowFromAdmission(t *testing.T) {
	a := explorerRouter(t)
	wantRow(t, a, "alpha", runningGlyphs(), false)
	startDelete(t, a)
	wantRow(t, a, "alpha", runningGlyphs(), true)
	wantRow(t, a, "beta", runningGlyphs(), false)
	if footer := explorerFooter(a); !strings.Contains(footer, "Deleting alpha") {
		t.Errorf("the footer does not say what the selected row is doing:\n%s", footer)
	}
}

// Criterion 3: a failed delete leaves the red mark, the 3 s event, then the
// persistent detail with the cause.
func TestAnExplorerDeleteFailureMarksTheRowAndTheFooterExplainsIt(t *testing.T) {
	a := explorerRouter(t)
	start := startDelete(t, a)
	_, cmd := a.Update(explorer.DeleteCompleteMsg{Target: start.Run.Items[0].Target, Error: errors.New("boom")})

	wantRow(t, a, "alpha", failedGlyph(), true)
	wantRow(t, a, "alpha", runningGlyphs(), false)
	if footer := explorerFooter(a); !strings.Contains(footer, "Failed to delete alpha — check logs") {
		t.Errorf("the event is not in the footer:\n%s", footer)
	}

	feedAll(a, cmd) // the event expires
	if footer := explorerFooter(a); !strings.Contains(footer, "⊗ alpha: last delete failed — check logs") {
		t.Errorf("after the event the footer lost the cause:\n%s", footer)
	}
	// Another row says nothing.
	a.Update(testutil.Key("down"))
	if footer := explorerFooter(a); strings.Contains(footer, "last delete failed") {
		t.Errorf("a healthy row carries the failure:\n%s", footer)
	}
}

// A-07 criterion 6, explorer side: the red mark survives ctrl+r, leaving for
// :jobs and a rebuilt view, because it lives in the registry.
func TestAnExplorerFailureSurvivesRefreshLeavingAndARebuiltView(t *testing.T) {
	a := explorerRouter(t)
	failDelete(t, a)
	wantRow(t, a, "alpha", failedGlyph(), true)

	// ctrl+r: its reload is dropped (no forge); the answer is fed by hand.
	a.Update(testutil.Key("ctrl+r"))
	a.Update(explorer.RootGroupsLoadedMsg{Nodes: explorerRoots()})
	wantRow(t, a, "alpha", failedGlyph(), true)

	_ = a.switchView(command.ViewJobs)
	_ = a.switchView(command.ViewGitExplorer)
	wantRow(t, a, "alpha", failedGlyph(), true)

	reopenExplorer(t, a)
	wantRow(t, a, "alpha", failedGlyph(), true)
}

// The memory is per context: hidden in "other", back in "default".
func TestAnExplorerFailureIsFilteredByContext(t *testing.T) {
	a := explorerRouter(t)
	failDelete(t, a)

	a.currentContext = "other"
	reopenExplorer(t, a)
	wantRow(t, a, "alpha", failedGlyph(), false)

	a.currentContext = "default"
	reopenExplorer(t, a)
	wantRow(t, a, "alpha", failedGlyph(), true)
}

// Retry from the explorer: D on the failed row clears the mark at once (the row
// blinks), and a delete that goes through removes the row and the memory.
func TestRetryingAFailedDeleteClearsTheMark(t *testing.T) {
	a := explorerRouter(t)
	failDelete(t, a)
	wantRow(t, a, "alpha", failedGlyph(), true)

	start := startDelete(t, a)
	wantRow(t, a, "alpha", runningGlyphs(), true)
	wantRow(t, a, "alpha", failedGlyph(), false)

	a.Update(explorer.DeleteCompleteMsg{
		Target:      start.Run.Items[0].Target,
		DeletedNode: &explorer.TreeNode{ID: "1", Name: "alpha", FullPath: "alpha", Type: explorer.NodeTypeGroup},
	})
	if strings.Contains(explorerScreen(a), "alpha") {
		t.Errorf("the deleted row is still on screen:\n%s", explorerScreen(a))
	}
	if got := jobs.FailuresOn(a.jobs.Failures(), "default", "alpha", jobs.KindDelete); len(got) != 0 {
		t.Errorf("the registry still remembers the failure of a delete that succeeded: %+v", got)
	}
}

// Q1=C: a failed create is an event and a failed run in :jobs, never a mark on
// a row (the placeholder is dropped, and a healthy row of the same name stays
// healthy).
func TestAFailedExplorerCreateNeverMarksARow(t *testing.T) {
	a := explorerRouter(t)
	start := startCreate(t, a, "newgroup")
	wantRow(t, a, "newgroup", runningGlyphs(), true)

	_, cmd := a.Update(explorer.GroupCreatedMsg{Target: start.Run.Items[0].Target, Error: errors.New("boom")})
	if strings.Contains(explorerScreen(a), "newgroup") {
		t.Errorf("the placeholder survived the failure:\n%s", explorerScreen(a))
	}
	if footer := explorerFooter(a); !strings.Contains(footer, "Failed to create") {
		t.Errorf("the failure is not announced:\n%s", footer)
	}
	feedAll(a, cmd)
	if strings.ContainsAny(explorerScreen(a), failedGlyph()) {
		t.Errorf("a failed create left a %s on the tree:\n%s", failedGlyph(), explorerScreen(a))
	}
}

// Criterion 2: the forge index walk is a KindIndex run, global, in the footer;
// it never paints a tree row, even one whose path equals its target.
func TestAnIndexWalkNeverPaintsATreeRow(t *testing.T) {
	a := explorerRouter(t)
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindIndex, command.ViewGitExplorer, "", "alpha", "alpha")})
	a.Update(explorer.RootGroupsLoadedMsg{Nodes: explorerRoots()})
	for _, name := range []string{"alpha", "beta"} {
		wantRow(t, a, name, runningGlyphs()+failedGlyph()+indicator.Glyph(indicator.Queued, false), false)
	}
}

// seedExplorerFooter puts the explorer on screen with a delete in flight on its
// selected row (alpha), for the activity guard.
func seedExplorerFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	openExplorer(t, a)
	startDelete(t, a)
	return indicator.Running
}
