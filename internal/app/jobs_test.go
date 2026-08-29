package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// scanRun is a run of the shape every launch site will build at poste 3.
func scanRun(contextName string, targets ...string) jobs.Run {
	return jobs.NewRun(jobs.KindScan, command.ViewWorkspaces, contextName, "~/work", targets...)
}

// jobsSeen returns every snapshot a view was handed, newest last.
func jobsSeen(v *fakeView) []jobs.ChangedMsg {
	var out []jobs.ChangedMsg
	for _, msg := range v.received {
		if typed, ok := msg.(jobs.ChangedMsg); ok {
			out = append(out, typed)
		}
	}
	return out
}

func lastJobs(t *testing.T, v *fakeView) jobs.ChangedMsg {
	t.Helper()
	seen := jobsSeen(v)
	if len(seen) == 0 {
		t.Fatal("the view was never handed a snapshot")
	}
	return seen[len(seen)-1]
}

// D1: the snapshot goes to every view the router holds, whether or not it is on
// screen. A view is told where it stands, so coming back to it shows what
// happened while it was away without it having to ask.
func TestASnapshotReachesEveryHeldView(t *testing.T) {
	dashboard := &fakeView{}
	ws := &fakeView{}
	sec := &fakeView{}
	a := router(t, dashboard)
	a.views[command.ViewWorkspaces] = ws
	a.views[command.ViewSecurity] = sec

	a.jobs.Start(scanRun("default", "a", "b"))
	a.jobsChanged()

	for name, view := range map[string]*fakeView{"dashboard": dashboard, "ws": ws, "security": sec} {
		snapshot := lastJobs(t, view)
		if got, want := len(snapshot.Runs), 1; got != want {
			t.Errorf("%s was handed %d runs, want %d", name, got, want)
		}
		if got, want := snapshot.Running(), 1; got != want {
			t.Errorf("%s sees %d running, want %d", name, got, want)
		}
	}
	if a.currentView != command.ViewDashboard {
		t.Errorf("broadcasting the snapshot switched the view to %s", a.currentView)
	}
}

// Rule 122: the frame in the message is bare, because a view puts it in a table
// cell, where it is measured before it is styled. A styled frame measures its
// escape sequence as width and is truncated inside it, which then bleeds down
// every row below.
func TestTheBroadcastFrameIsBareAndItsRenderingIsNot(t *testing.T) {
	withTrueColor(t)

	view := &fakeView{}
	a := router(t, view)
	a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()

	snapshot := lastJobs(t, view)

	if snapshot.Frame == "" {
		t.Fatal("no spinner frame was carried")
	}
	if strings.Contains(snapshot.Frame, "\x1b") {
		t.Errorf("Frame = %q carries an escape sequence; Rule 122 wants a cell measurable", snapshot.Frame)
	}
	if !strings.Contains(snapshot.RenderedFrame, "\x1b") {
		t.Errorf("RenderedFrame = %q, want the styled frame a footer takes (Rule 128)", snapshot.RenderedFrame)
	}
	if !strings.Contains(snapshot.RenderedFrame, snapshot.Frame) {
		t.Errorf("RenderedFrame = %q does not carry Frame = %q", snapshot.RenderedFrame, snapshot.Frame)
	}
}

// D5: one chain for the whole application. Asking twice while it is alive must
// not start a second, which is the failure the four hand-stamped spinners kept
// producing — two chains advancing one frame at twice the rate.
func TestOnlyOneSpinnerChainIsEverAlive(t *testing.T) {
	a := router(t, &fakeView{})
	a.jobs.Start(scanRun("default", "a"))

	first := a.ensureJobTick()
	if first == nil {
		t.Fatal("no chain was started for work that is running")
	}
	if second := a.ensureJobTick(); second != nil {
		t.Error("a second chain was started while the first was alive")
	}
	if !a.jobTicking {
		t.Error("the router does not know its chain is alive")
	}
}

// An empty registry animates nothing: a chain against no work would rebuild
// every view's rows ten times a second for a settled application.
func TestNoChainStartsWithNothingRunning(t *testing.T) {
	a := router(t, &fakeView{})

	if cmd := a.ensureJobTick(); cmd != nil {
		t.Error("a chain was started with an empty registry")
	}

	// A settled run is not running work either.
	id := a.jobs.Start(scanRun("default", "a"))
	a.jobs.Advance(id, "a", jobs.ItemDone, "")
	if cmd := a.ensureJobTick(); cmd != nil {
		t.Error("a chain was started for a run that had already settled")
	}
}

// The chain advances the frame and renews itself while work runs; it stops by
// not being renewed once nothing is left, and a later run starts a fresh one.
// That last half is what each view could not do: its chain died on the first
// idle tick and nothing brought it back, so the spinner froze on the frame it
// died at.
func TestTheChainRunsWhileWorkDoesAndRestartsAfterwards(t *testing.T) {
	testutil.FastTimers(t, &jobSpinnerInterval)

	view := &fakeView{}
	a := router(t, view)
	id := a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()

	before := a.jobFrame()
	_, cmd := a.Update(jobTickMsg{seq: a.jobTickSeq})

	if a.jobFrame() == before {
		t.Error("the frame did not advance")
	}
	if _, ok := testutil.MsgOf[jobTickMsg](cmd); !ok {
		t.Error("the chain was not renewed while work was running")
	}
	if len(jobsSeen(view)) < 2 {
		t.Error("the new frame was not broadcast")
	}

	// The work settles, and the next tick is the last.
	a.jobs.Advance(id, "a", jobs.ItemDone, "")
	_, cmd = a.Update(jobTickMsg{seq: a.jobTickSeq})

	if _, ok := testutil.MsgOf[jobTickMsg](cmd); ok {
		t.Error("the chain renewed itself with nothing left to animate")
	}
	if a.jobTicking {
		t.Error("the router still believes a chain is alive")
	}

	// And a new run starts a fresh one rather than finding the door shut.
	a.jobs.Start(scanRun("default", "b"))
	if a.ensureJobTick() == nil {
		t.Error("a run started after the chain died got no chain")
	}
}

// A tick from a chain the router has replaced is dropped. It is what makes a
// second chain impossible rather than merely unlikely.
func TestATickFromAnOlderChainIsDropped(t *testing.T) {
	a := router(t, &fakeView{})
	a.jobs.Start(scanRun("default", "a"))
	a.ensureJobTick()

	before := a.jobFrame()
	_, cmd := a.Update(jobTickMsg{seq: a.jobTickSeq - 1})

	if a.jobFrame() != before {
		t.Error("a stale tick advanced the frame")
	}
	if cmd != nil {
		t.Error("a stale tick was answered with a command")
	}
}

// A view entering the screen is handed the snapshot: it was kept up to date
// while it existed, but one built lazily on the way in was there for none of it.
func TestAViewEnteringTheScreenIsHandedTheSnapshot(t *testing.T) {
	ws := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = ws
	a.jobs.Start(scanRun("default", "a", "b"))

	a.switchView(command.ViewWorkspaces)

	snapshot := lastJobs(t, ws)
	if got, want := len(snapshot.Runs), 1; got != want {
		t.Errorf("the entering view was handed %d runs, want %d", got, want)
	}
}

// A view the router does not hold is not a crash.
func TestSendingTheSnapshotToAMissingViewIsSilent(t *testing.T) {
	a := router(t, &fakeView{})
	delete(a.views, command.ViewWorkspaces)

	if cmd := a.sendJobsTo(command.ViewWorkspaces); cmd != nil {
		t.Error("a command came back for a view that does not exist")
	}
}

// D8: a context switch changes which runs are relevant, never whether they
// exist. Runs are stamped and filtered, not purged — purging would contradict
// keeping them for the session.
//
// reinitializeViews throws the view map away and rebuilds it, so the rebuilt
// views know nothing of work that did not stop. That the handler tells them is
// asserted through the spinner chain: jobsChanged is the only thing on that
// path that starts one, so a chain alive afterwards is the broadcast having
// happened.
func TestAContextSwitchTellsTheRebuiltViewsWhatIsRunning(t *testing.T) {
	testutil.FastTimers(t, &jobSpinnerInterval)

	a := router(t, &fakeView{})
	id := a.jobs.Start(scanRun("default", "a"))
	a.jobs.Advance(id, "a", jobs.ItemRunning, "")
	a.jobs.Start(scanRun("prod", "b"))

	if a.jobTicking {
		t.Fatal("a chain was already alive; the assertion below would prove nothing")
	}

	_, cmd := a.Update(ContextSwitchCompleteMsg{
		Config:      testConfig(),
		ContextName: "prod",
		Secrets:     a.sharedState.Secrets,
	})

	if got, want := a.jobs.Len(), 2; got != want {
		t.Errorf("the registry holds %d runs after the switch, want %d — runs are filtered, not purged", got, want)
	}
	if got, want := a.jobs.Running(), 2; got != want {
		t.Errorf("Running = %d, want %d — a switch does not stop what is going", got, want)
	}
	if !a.jobTicking {
		t.Error("the rebuilt views were not told what is running")
	}
	if _, ok := testutil.MsgOf[jobTickMsg](cmd); !ok {
		t.Error("the switch did not return the chain it started")
	}

	// And the run from the other context is still there to be filtered on.
	if got, want := len(jobs.FilterContext(a.jobs.Snapshot(), "default")), 1; got != want {
		t.Errorf("the previous context holds %d runs, want %d", got, want)
	}
}

// The whole point of the registry is that one bookkeeping answers for work
// started anywhere: a scan launched from the security view is visible to the
// workspaces view, which is what its busy() guard could never see.
func TestWorkStartedInOneViewIsVisibleToAnother(t *testing.T) {
	ws := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = ws

	fromSecurity := jobs.NewRun(jobs.KindScan, command.ViewSecurity, "default", "images", "/repos/devdesk")
	id := a.jobs.Start(fromSecurity)
	a.jobs.Advance(id, "/repos/devdesk", jobs.ItemRunning, "")
	a.jobsChanged()

	snapshot := lastJobs(t, ws)
	if got, want := len(snapshot.Runs), 1; got != want {
		t.Fatalf("the workspaces view sees %d runs, want %d", got, want)
	}
	run := snapshot.Runs[0]
	if run.Origin != command.ViewSecurity {
		t.Errorf("Origin = %s, want the view that started it", run.Origin)
	}
	if run.Items[0].State != jobs.ItemRunning {
		t.Errorf("the repository reads %q in a view that did not start the scan", run.Items[0].State)
	}
}

// The router owns the registry; a snapshot cannot write back into it. Views get
// data, and every mutation goes through the one owner (D1).
func TestAViewCannotWriteThroughItsSnapshot(t *testing.T) {
	view := &fakeView{}
	a := router(t, view)
	a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()

	snapshot := lastJobs(t, view)
	snapshot.Runs[0].Items[0].State = jobs.ItemDone

	if got, want := a.jobs.Running(), 1; got != want {
		t.Errorf("Running = %d, want %d — a view wrote through its snapshot", got, want)
	}
}

// The full round trip, which is what nothing tested before the registry: a view
// asks for a run, the router admits it, and the view's own progress messages
// move its items — without either side holding a JobID.
func TestAViewsWorkIsRegisteredAndAdvancedByItsOwnMessages(t *testing.T) {
	ws := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = ws
	a.currentContext = "prod"

	testutil.FastTimers(t, &jobSpinnerInterval)

	worked := false
	_, cmd := a.Update(jobs.StartMsg{
		Run:  jobs.NewRun(jobs.KindScan, command.ViewWorkspaces, "", "~/work", "/repos/a", "/repos/b"),
		Work: func() tea.Msg { worked = true; return nil },
	})

	// The work rides on the message rather than being batched beside it, so it
	// cannot outrun the registration — and a test can read what was launched
	// without launching it, which a tea.Sequence at the launch site would deny.
	testutil.Msgs(cmd)
	if !worked {
		t.Error("the run was registered but its work never went out")
	}

	snapshot := a.jobs.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("the registry holds %d runs, want 1", len(snapshot))
	}
	if got, want := snapshot[0].Context, "prod"; got != want {
		t.Errorf("Context = %q, want %q — the router stamps it, not the view", got, want)
	}
	if got := snapshot[0].State(); got != jobs.RunQueued {
		t.Errorf("state = %q, want every target queued at registration (D6)", got)
	}

	// One repository starts, and the row it belongs to is running while the
	// other is still queued.
	a.Update(workspaces.WorkspaceScanStartingMsg{RepoPath: "/repos/a"})
	run := a.jobs.Snapshot()[0]
	if got := run.Items[0].State; got != jobs.ItemRunning {
		t.Errorf("the started repository is %q, want running", got)
	}
	if got := run.Items[1].State; got != jobs.ItemQueued {
		t.Errorf("the waiting repository is %q, want it still queued", got)
	}

	// It finishes; the other fails. The run settles as failed, and the reason
	// is the view's own wording, not a raw error.
	a.Update(workspaces.WorkspaceScanCompleteMsg{RepoPath: "/repos/a"})
	a.Update(workspaces.WorkspaceScanCompleteMsg{RepoPath: "/repos/b", Error: errNotOnDisk})

	run = a.jobs.Snapshot()[0]
	if got := run.State(); got != jobs.RunFailed {
		t.Errorf("state = %q, want %q", got, jobs.RunFailed)
	}
	if got := run.Items[1].Detail; got == "" || strings.Contains(got, "no such file") {
		t.Errorf("Detail = %q, want the view's short wording rather than the raw error", got)
	}
	if got := a.jobs.Running(); got != 0 {
		t.Errorf("Running = %d, want 0", got)
	}

	// And the view saw every one of them: the registry does not swallow what it
	// applies.
	if _, ok := receivedOf[workspaces.WorkspaceScanStartingMsg](ws); !ok {
		t.Error("the starting message never reached the view")
	}
	if _, ok := receivedOf[workspaces.WorkspaceScanCompleteMsg](ws); !ok {
		t.Error("the completion never reached the view")
	}
}

// A progress message for work nobody registered is routed all the same. The
// registry declines it rather than inventing a run, and the view still gets to
// act on it.
func TestAMessageForUnregisteredWorkIsStillRouted(t *testing.T) {
	ws := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = ws

	a.Update(workspaces.WorkspaceScanCompleteMsg{RepoPath: "/repos/never-registered"})

	if got := a.jobs.Len(); got != 0 {
		t.Errorf("the registry invented %d runs for work it was never told about", got)
	}
	if _, ok := receivedOf[workspaces.WorkspaceScanCompleteMsg](ws); !ok {
		t.Error("the message was dropped because the registry did not recognise it")
	}
}
