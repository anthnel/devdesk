package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
)

// DEVDESK-211 step 2: the router maps the jobs summary to View.ProgressBar.

const progressSourceOwner = "internal/app/terminal_progress.go"

// barOf renders the bar the router asks for as "<nil>" or "{state value}".
func barOf(a *App) string {
	return barString(a.View().ProgressBar)
}

func barString(b *tea.ProgressBar) string {
	if b == nil {
		return "<nil>"
	}
	return fmt.Sprintf("{%d %d}", b.State, b.Value)
}

func wantBar(state tea.ProgressBarState, value int) string {
	return barString(&tea.ProgressBar{State: state, Value: value})
}

func wantBarFor(t *testing.T, a *App, want string) {
	t.Helper()
	if got := barOf(a); got != want {
		t.Errorf("ProgressBar = %s, want %s", got, want)
	}
}

// tick drives one harmless message through Update so the level check
// (syncTerminalProgress) runs, without a jobs broadcast.
func tick(a *App) { a.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height}) }

// failScan registers a one-item scan run under contextName and fails the item.
func failScan(a *App, contextName, target string) {
	run := scanRun(contextName, target)
	id := a.jobs.Start(run)
	a.jobs.Advance(id, target, jobs.ItemFailed, "boom")
	a.jobsChanged()
}

func TestTerminalProgressMapping(t *testing.T) {
	cases := []struct {
		name string
		in   jobs.Progress
		want string
	}{
		{"idle is nil", jobs.Progress{}, "<nil>"},
		{"indeterminate", jobs.Progress{State: jobs.ProgressIndeterminate, Running: 1}, wantBar(tea.ProgressBarIndeterminate, 0)},
		{"percent", jobs.Progress{State: jobs.ProgressPercent, Percent: 42, Measured: true, Running: 1}, wantBar(tea.ProgressBarDefault, 42)},
		{"percent 0", jobs.Progress{State: jobs.ProgressPercent, Measured: true, Running: 1}, wantBar(tea.ProgressBarDefault, 0)},
		{"warning measured", jobs.Progress{State: jobs.ProgressWarning, Percent: 30, Measured: true, Running: 1, Stopping: 1}, wantBar(tea.ProgressBarWarning, 30)},
		{"warning unmeasured", jobs.Progress{State: jobs.ProgressWarning, Percent: 100, Running: 1, Stopping: 1}, wantBar(tea.ProgressBarWarning, 100)},
		// Summarize leaves Percent as is in Error; the presentation emits 100.
		{"error is full", jobs.Progress{State: jobs.ProgressError, Percent: 37, Measured: true, Running: 1, Unread: 1}, wantBar(tea.ProgressBarError, 100)},
		{"error with nothing running", jobs.Progress{State: jobs.ProgressError, Unread: 2}, wantBar(tea.ProgressBarError, 100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := barString(terminalProgress(tc.in)); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// The renderer compares through the pointer it kept: a shared or mutated
// pointer would hide a change.
func TestTerminalProgressReturnsAFreshPointerEachCall(t *testing.T) {
	p := jobs.Progress{State: jobs.ProgressPercent, Percent: 10, Measured: true, Running: 1}
	first, second := terminalProgress(p), terminalProgress(p)
	if first == nil || second == nil {
		t.Fatalf("got %v and %v, want two bars", first, second)
	}
	if first == second {
		t.Fatal("two calls returned the same pointer")
	}
	first.Value = 99
	if second.Value != 10 {
		t.Errorf("mutating the first changed the second: %d", second.Value)
	}
}

func TestViewHandsOutAFreshProgressBarEveryCall(t *testing.T) {
	a := router(t, &fakeView{})
	a.jobs.Start(scanRun("default", "a", "b"))
	a.jobsChanged()
	first, second := a.View().ProgressBar, a.View().ProgressBar
	if first == nil || second == nil {
		t.Fatalf("got %v and %v, want bars", first, second)
	}
	if first == second {
		t.Error("View() returned the same *ProgressBar twice")
	}
}

func TestTerminalProgressIsNilWithoutJobs(t *testing.T) {
	a := router(t, &fakeView{})
	if a.View().ProgressBar != nil {
		t.Errorf("ProgressBar = %s, want nil", barOf(a))
	}
}

func TestTerminalProgressFollowsAMeasuredScan(t *testing.T) {
	a := router(t, &fakeView{})
	a.views[command.ViewSecurity] = &fakeView{}
	a.currentView = command.ViewSecurity

	a.Update(jobs.StartMsg{
		Run:  scanRun("", "a", "b", "c", "d"),
		Work: func(string) tea.Cmd { return nil },
	})
	wantBarFor(t, a, wantBar(tea.ProgressBarDefault, 0))

	id := a.jobs.Snapshot()[0].ID
	a.jobs.Advance(id, "a", jobs.ItemDone, "")
	a.jobs.Advance(id, "b", jobs.ItemDone, "")
	a.jobsChanged()
	wantBarFor(t, a, wantBar(tea.ProgressBarDefault, 50))

	a.jobs.Advance(id, "c", jobs.ItemDone, "")
	a.jobs.Advance(id, "d", jobs.ItemDone, "")
	a.jobsChanged()
	wantBarFor(t, a, "<nil>")
}

func TestTerminalProgressOfAnOpenRunIsIndeterminateUntilSealed(t *testing.T) {
	a := router(t, &fakeView{})
	a.jobs.Start(jobs.NewOpenRun(jobs.KindClone, command.ViewGitExplorer, "default", "clone"))
	a.jobsChanged()
	wantBarFor(t, a, wantBar(tea.ProgressBarIndeterminate, 0))

	a.jobs.Discover(jobs.KindClone, "a", jobs.ItemDone, "")
	a.jobs.Discover(jobs.KindClone, "b", jobs.ItemQueued, "")
	a.jobs.Seal(jobs.KindClone)
	a.jobsChanged()
	wantBarFor(t, a, wantBar(tea.ProgressBarDefault, 50))
}

func TestTerminalProgressIsRedForAnUnreadFailureOutsideJobs(t *testing.T) {
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = &fakeView{}
	a.currentView = command.ViewWorkspaces

	failScan(a, "default", "a")
	// failScan's single-target run is settled: nothing runs, the failure is what the bar tells.
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
}

// A failure inside a run that still has work left: the bar is red, then
// acknowledging it in :jobs gives back the measured progress (1 of 2 = 50),
// and it clears once the run settles.
func TestAFailureInATwoTargetRunIsAcknowledgedThenTheRunSettles(t *testing.T) {
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = &fakeView{}
	a.currentView = command.ViewWorkspaces
	id := a.jobs.Start(scanRun("default", "a", "b"))
	a.jobs.Advance(id, "a", jobs.ItemFailed, "boom")
	a.jobsChanged()
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))

	a.switchView(command.ViewJobs)
	tick(a)
	wantBarFor(t, a, wantBar(tea.ProgressBarDefault, 50))

	a.jobs.Advance(id, "b", jobs.ItemDone, "")
	a.jobsChanged()
	wantBarFor(t, a, "<nil>")
}

func TestTerminalProgressErrorIsRedEvenWithAnEmptyRegistryOfRuns(t *testing.T) {
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = &fakeView{}
	a.currentView = command.ViewWorkspaces
	run := jobs.NewImmediateRun(jobs.KindScan, command.ViewWorkspaces, "default", "x", "a")
	id := a.jobs.Start(run)
	a.jobs.Advance(id, "a", jobs.ItemFailed, "boom") // settles the only run
	a.jobsChanged()
	if got := a.jobs.Running(); got != 0 {
		t.Fatalf("setup: %d runs still running", got)
	}
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
}

func TestOpeningJobsAcknowledgesTheFailures(t *testing.T) {
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = &fakeView{}
	a.currentView = command.ViewWorkspaces
	failScan(a, "default", "a")
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))

	a.switchView(command.ViewJobs)
	tick(a)
	wantBarFor(t, a, "<nil>")

	a.switchView(command.ViewWorkspaces)
	tick(a)
	wantBarFor(t, a, "<nil>")

	failScan(a, "default", "b")
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
}

func TestAFailureArrivingWhileJobsIsOnScreenIsNeverRed(t *testing.T) {
	a := router(t, &fakeView{})
	a.switchView(command.ViewJobs)
	tick(a)
	failScan(a, "default", "a")
	wantBarFor(t, a, "<nil>")
}

// A failure is acknowledged only while the terminal has the focus (DEVDESK-211):
// a blurred terminal means nobody saw it.
func TestAFailureArrivingWhileJobsIsOnScreenAndBlurredStaysRed(t *testing.T) {
	a := router(t, &fakeView{})
	a.switchView(command.ViewJobs)
	tick(a)
	a.Update(tea.BlurMsg{})
	failScan(a, "default", "a")
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
	tick(a)
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
}

func TestFocusBackWithJobsStillOnScreenAcknowledgesTheFailure(t *testing.T) {
	a := router(t, &fakeView{})
	a.switchView(command.ViewJobs)
	tick(a)
	a.Update(tea.BlurMsg{})
	failScan(a, "default", "a")
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))

	a.Update(tea.FocusMsg{})
	wantBarFor(t, a, "<nil>")
}

// With app.when_unfocused: run the blur returns early, yet the failure is still
// red while blurred and read when the focus comes back on :jobs.
func TestFocusBackWithRunPolicyAcknowledgesTheFailure(t *testing.T) {
	a := router(t, &fakeView{})
	_ = a.applyWhenUnfocused(policyConfig(config.WhenUnfocusedRun))
	a.switchView(command.ViewJobs)
	tick(a)
	a.Update(tea.BlurMsg{})
	failScan(a, "default", "a")
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
	tick(a)
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))

	a.Update(tea.FocusMsg{})
	wantBarFor(t, a, "<nil>")
}

func TestFocusBackOnAnotherViewDoesNotAcknowledgeABlurredFailure(t *testing.T) {
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = &fakeView{}
	a.switchView(command.ViewJobs)
	tick(a)
	a.Update(tea.BlurMsg{})
	failScan(a, "default", "a")

	a.switchView(command.ViewWorkspaces)
	tick(a)
	a.Update(tea.FocusMsg{})
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
}

func TestOpeningJobsWhileBlurredDoesNotAcknowledge(t *testing.T) {
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = &fakeView{}
	a.currentView = command.ViewWorkspaces
	failScan(a, "default", "a")
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))

	a.Update(tea.BlurMsg{})
	a.switchView(command.ViewJobs)
	tick(a)
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
}

func TestAnIndexFailureCountsLikeAnyOther(t *testing.T) {
	a := router(t, &fakeView{})
	run := jobs.NewImmediateRun(jobs.KindIndex, command.ViewGitExplorer, "default", "host", "host")
	id := a.jobs.Start(run)
	a.jobs.Advance(id, "host", jobs.ItemFailed, "offline")
	a.jobsChanged()
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))
}

// The scope is the current context (D8), and so is the acknowledgement.
func TestTerminalProgressIsScopedToTheCurrentContext(t *testing.T) {
	a := router(t, &fakeView{})
	ws := &fakeView{}
	a.views[command.ViewWorkspaces] = ws
	a.currentView = command.ViewWorkspaces

	failScan(a, "other", "a")
	wantBarFor(t, a, "<nil>") // the failure belongs to another context

	// Acknowledge in the current context; it must not acknowledge "other".
	failScan(a, "default", "z")
	a.switchView(command.ViewJobs)
	tick(a)
	a.switchView(command.ViewWorkspaces)
	tick(a)
	wantBarFor(t, a, "<nil>")

	// A switch of context with no broadcast: the bar follows the new context.
	a.currentContext = "other"
	tick(a)
	wantBarFor(t, a, wantBar(tea.ProgressBarError, 100))

	a.currentContext = "default"
	tick(a)
	wantBarFor(t, a, "<nil>")
}

func TestTerminalProgressIgnoresRunsOfAnotherContext(t *testing.T) {
	a := router(t, &fakeView{})
	a.jobs.Start(scanRun("other", "a", "b"))
	a.jobsChanged()
	wantBarFor(t, a, "<nil>")
	a.currentContext = "other"
	tick(a)
	wantBarFor(t, a, wantBar(tea.ProgressBarDefault, 0))
}

func TestACancelledSyncWarnsUntilItSettles(t *testing.T) {
	a := router(t, &fakeView{})
	run := jobs.NewRun(jobs.KindSync, command.ViewWorkspaces, "default", "sync", "a", "b")
	id := a.jobs.Start(run)
	a.jobs.Advance(id, "a", jobs.ItemRunning, "")
	a.jobsChanged()

	a.Update(jobs.CancelMsg{ID: id})
	got := a.View().ProgressBar
	if got == nil || got.State != tea.ProgressBarWarning {
		t.Fatalf("ProgressBar = %s, want a warning", barOf(a))
	}

	a.jobs.Advance(id, "a", jobs.ItemDone, "")
	a.jobsChanged()
	wantBarFor(t, a, "<nil>")
}

// Criterion 8: the bar touches nothing but its own field.
func TestTheProgressBarDoesNotChangeTheContent(t *testing.T) {
	a := router(t, &fakeView{body: "hello"})
	a.jobs.Start(scanRun("default", "a", "b"))
	a.jobsChanged()
	if a.View().ProgressBar == nil {
		t.Fatal("setup: no bar with a running scan")
	}
	// Running work animates the frame; compare against the same frame with the
	// summary cleared instead of against an older render.
	with := a.View().Content
	a.progress = jobs.Progress{}
	if without := a.View().Content; with != without {
		t.Error("View().Content depends on the progress summary")
	}
}

// TestOnlyTheRouterSetsTheTerminalProgress: like the background, the bar is
// terminal state owned by internal/app/view.go, and only terminal_progress.go
// builds one.
func TestOnlyTheRouterSetsTheTerminalProgress(t *testing.T) {
	assertOnlyOwnerSets(t, "internal/app/view.go", "ProgressBar")
}

func TestOnlyTerminalProgressBuildsAProgressBar(t *testing.T) {
	fset := token.NewFileSet()
	builds := false
	err := walkProductionGo("../..", func(rel, path string) error {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var pos token.Pos
			switch x := n.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewProgressBar" {
					pos = sel.Pos()
				}
			case *ast.CompositeLit:
				if sel, ok := x.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "ProgressBar" {
					pos = sel.Pos()
				}
			}
			if pos == token.NoPos {
				return true
			}
			if rel == progressSourceOwner {
				builds = true
			} else {
				t.Errorf("%s builds a tea.ProgressBar; only %s may", fset.Position(pos), progressSourceOwner)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !builds {
		t.Errorf("%s builds no tea.ProgressBar: the guard would pass vacuously", progressSourceOwner)
	}
}
