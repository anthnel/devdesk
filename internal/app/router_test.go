package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/gitlab/explorer"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// ── Key precedence ───────────────────────────────────────────────────────────

// An overlay owns the keyboard while it is up. A key reaching the view behind
// it would act on a table the user cannot see.
func TestAnOpenOverlaySwallowsEveryKey(t *testing.T) {
	tests := []struct {
		name string
		open func(*App)
	}{
		{"help", func(a *App) { a.showHelp = true }},
		{"context list", func(a *App) { a.showContextList = true }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := &fakeView{}
			a := router(t, view)
			tt.open(a)

			for _, key := range testutil.Keys("ctrl+d", "p", "enter", "down") {
				a.handleKeyMsg(key.(tea.KeyMsg))
			}

			if got := view.keysSeen(); len(got) != 0 {
				t.Errorf("the view received %v from behind the %s overlay", got, tt.name)
			}
		})
	}
}

// Help is checked first, so it closes first. Opening help from a view reached
// through the context list must not leave the list holding the keyboard.
func TestHelpIsCheckedBeforeTheLists(t *testing.T) {
	a := router(t, &fakeView{})
	a.showHelp = true
	a.showContextList = true

	a.handleKeyMsg(testutil.Key("esc"))

	if a.showHelp {
		t.Error("esc did not close the help overlay")
	}
	if !a.showContextList {
		t.Error("esc closed the context list as well as the help overlay")
	}
}

// ── Keys the router claims ───────────────────────────────────────────────────

// q quits, unless a field is focused — where it is the letter q.
func TestQuitDependsOnWhetherTheViewIsEditing(t *testing.T) {
	t.Run("nothing focused, it quits", func(t *testing.T) {
		a := router(t, &fakeView{})

		cmd := feedKey(t, a, testutil.Key("q"))

		if _, ok := testutil.MsgOf[tea.QuitMsg](cmd); !ok {
			t.Errorf("q produced %T, want a quit", testutil.Msg(cmd))
		}
	})

	t.Run("editing, it reaches the field", func(t *testing.T) {
		view := &fakeView{editing: true}
		a := router(t, view)

		cmd := feedKey(t, a, testutil.Key("q"))

		if _, ok := testutil.MsgOf[tea.QuitMsg](cmd); ok {
			t.Error("q quit the application while a text field was focused")
		}
		if !view.sawKey("q") {
			t.Errorf("the view received %v, want the q it needs to type", view.keysSeen())
		}
	})
}

// ctrl+c is unconditional: it is the terminal's own interrupt and no view may
// hold it hostage.
func TestCtrlCQuitsEvenWhileEditing(t *testing.T) {
	a := router(t, &fakeView{editing: true})

	cmd := feedKey(t, a, testutil.Key("ctrl+c"))

	if _, ok := testutil.MsgOf[tea.QuitMsg](cmd); !ok {
		t.Errorf("ctrl+c produced %T, want a quit", testutil.Msg(cmd))
	}
}

func TestHelpOpensOnlyWhenTheViewOffersIt(t *testing.T) {
	t.Run("a view that provides help", func(t *testing.T) {
		a := router(t, &fakeView{})

		a.handleKeyMsg(testutil.Key("?"))

		if !a.showHelp {
			t.Error("? did not open the help overlay")
		}
	})

	t.Run("a view that provides none", func(t *testing.T) {
		a := router(t, &bareView{})

		a.handleKeyMsg(testutil.Key("?"))

		if a.showHelp {
			t.Error("? opened an empty help overlay for a view that provides no content")
		}
	})

	t.Run("editing, it reaches the field", func(t *testing.T) {
		view := &fakeView{editing: true}
		a := router(t, view)

		a.handleKeyMsg(testutil.Key("?"))

		if a.showHelp {
			t.Error("? opened the help overlay while a text field was focused")
		}
		if !view.sawKey("?") {
			t.Errorf("the view received %v, want the ? it needs to type", view.keysSeen())
		}
	})
}

// Esc belongs to the view, editing or not. The router used to answer it itself
// whenever the view was not editing, which made every esc-to-go-back handler
// dead code — the explorer's drill-up among them (D15).
func TestEscAlwaysReachesTheView(t *testing.T) {
	for _, editing := range []bool{false, true} {
		view := &fakeView{editing: editing}
		a := router(t, view)

		a.handleKeyMsg(testutil.Key("esc"))

		if !view.sawKey("esc") {
			t.Errorf("with editing=%v the view received %v, want esc", editing, view.keysSeen())
		}
	}
}

// Handing esc to the view must not cost the command line its own: an open
// command line answers esc first, and the view never sees that one.
func TestAnOpenCommandLineAnswersEscItself(t *testing.T) {
	view := &fakeView{}
	a := router(t, view)
	a.enterCommandMode()

	a.handleKeyMsg(testutil.Key("esc"))

	if a.commandMode {
		t.Error("esc left the command line open")
	}
	if view.sawKey("esc") {
		t.Errorf("the view also received esc: %v", view.keysSeen())
	}
}

func TestUnclaimedKeysGoToTheActiveView(t *testing.T) {
	view := &fakeView{}
	a := router(t, view)

	for _, key := range testutil.Keys("ctrl+s", "j", "enter", "/") {
		a.handleKeyMsg(key.(tea.KeyMsg))
	}

	want := []string{"ctrl+s", "j", "enter", "/"}
	got := view.keysSeen()
	if len(got) != len(want) {
		t.Fatalf("the view received %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d was %q, want %q", i, got[i], want[i])
		}
	}
}

// ── Rule 124: the footer height contract ─────────────────────────────────────

// The viewport is sized from the view's footer height, so a view that grows a
// footer — opening its filter bar — has to be re-measured on the spot. Without
// it the last table row sits under the filter bar until the next resize.
func TestAViewThatGrowsItsFooterIsRemeasured(t *testing.T) {
	view := &fakeView{footerHeight: 2}
	view.onKey = func(v *fakeView) { v.footerHeight = 3 }
	a := router(t, view)
	before := a.viewport.Height

	a.handleKeyMsg(testutil.Key("/"))

	if a.lastFooterHeight != 3 {
		t.Errorf("lastFooterHeight = %d after the footer grew, want 3", a.lastFooterHeight)
	}
	if a.viewport.Height != before-1 {
		t.Errorf("viewport height = %d, want %d — one row given to the filter bar", a.viewport.Height, before-1)
	}
}

// The same has to hold for a footer that shrinks back, or the viewport keeps
// the row the filter bar gave up.
func TestAViewThatShrinksItsFooterIsRemeasured(t *testing.T) {
	view := &fakeView{footerHeight: 3}
	view.onKey = func(v *fakeView) { v.footerHeight = 2 }
	a := router(t, view)
	before := a.viewport.Height

	a.handleKeyMsg(testutil.Key("/"))

	if a.viewport.Height != before+1 {
		t.Errorf("viewport height = %d, want %d — the row is returned to the table", a.viewport.Height, before+1)
	}
}

// A footer height change caused by a message rather than a key — the security
// view moving from its form to its results — must be noticed too.
func TestAFooterThatChangesOnAMessageIsRemeasured(t *testing.T) {
	view := &fakeView{footerHeight: 2}
	a := router(t, view)
	before := a.viewport.Height
	view.footerHeight = 4

	a.Update(struct{ scanFinished bool }{true})

	if a.viewport.Height != before-2 {
		t.Errorf("viewport height = %d, want %d", a.viewport.Height, before-2)
	}
}

// resize() budgets the window between the header, the title border line and the
// footer; everything left is the viewport.
func TestResizeGivesTheViewportWhatIsLeft(t *testing.T) {
	view := &fakeView{footerHeight: 2}
	a := router(t, view)

	a.resize(120, 50)

	headerHeight := 9 // 7 header rows + empty line + command line
	want := 50 - headerHeight - 2 - 1
	if a.viewport.Height != want {
		t.Errorf("viewport height = %d, want %d", a.viewport.Height, want)
	}
	if a.viewport.Width != 120 {
		t.Errorf("viewport width = %d, want 120", a.viewport.Width)
	}
}

// The active view is told the height inside the viewport border, not the window
// height — it has no other way to size its table.
func TestResizePropagatesTheInnerHeightToTheView(t *testing.T) {
	view := &fakeView{footerHeight: 2}
	a := router(t, view)
	view.received = nil // drop the layout the harness already performed

	a.resize(120, 50)

	size, ok := receivedOf[tea.WindowSizeMsg](view)
	if !ok {
		t.Fatal("the view was never told the new size")
	}
	if size.Height >= 50 {
		t.Errorf("the view was told height %d, want the height inside the viewport border", size.Height)
	}
	if size.Width != 120 {
		t.Errorf("the view was told width %d, want 120", size.Width)
	}
}

// ── Message routing ──────────────────────────────────────────────────────────

// Scan progress reaches the OCI view wherever the user has navigated to, or a
// scan started there and left behind would show a spinner that never resolves.
func TestScanProgressReachesTheOCIViewFromAnotherView(t *testing.T) {
	oci := &fakeView{}
	dashboard := &fakeView{}
	a := router(t, dashboard)
	a.views[command.ViewOCIResources] = oci

	a.Update(ociresources.ImageScanStartingMsg{})
	a.Update(ociresources.ImageScanFinishedMsg{})

	if _, ok := receivedOf[ociresources.ImageScanStartingMsg](oci); !ok {
		t.Error("the scan-starting message never reached the OCI view")
	}
	if _, ok := receivedOf[ociresources.ImageScanFinishedMsg](oci); !ok {
		t.Error("the scan-finished message never reached the OCI view")
	}
	if a.currentView != command.ViewDashboard {
		t.Errorf("routing a background scan message switched the view to %s", a.currentView)
	}
	if _, ok := receivedOf[ociresources.ImageScanStartingMsg](dashboard); ok {
		t.Error("the scan message was also delivered to the active view")
	}
}

// Documented race: a background workspace scan finishing must not drag the user
// out of the security view, or the security scan's own completion is routed to
// the wrong view and its spinner never stops.
func TestAWorkspaceScanDoesNotStealTheSecurityView(t *testing.T) {
	ws := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = ws
	a.views[command.ViewSecurity] = &fakeView{}
	a.currentView = command.ViewSecurity

	a.Update(workspaces.WorkspaceScanCompleteMsg{})

	if a.currentView != command.ViewSecurity {
		t.Errorf("the view switched to %s while a security scan was on screen", a.currentView)
	}
	if _, ok := receivedOf[workspaces.WorkspaceScanCompleteMsg](ws); !ok {
		t.Error("the workspaces view was not updated with its scan result")
	}
}

// From anywhere else, the same message does bring the results up.
func TestAWorkspaceScanSwitchesToTheWorkspacesView(t *testing.T) {
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = &fakeView{}

	a.Update(workspaces.WorkspaceScanCompleteMsg{})

	if a.currentView != command.ViewWorkspaces {
		t.Errorf("current view = %s, want the workspaces view showing the result", a.currentView)
	}
}

func TestUnknownMessagesGoToTheActiveView(t *testing.T) {
	view := &fakeView{}
	a := router(t, view)

	type customMsg struct{ n int }
	a.Update(customMsg{7})

	if _, ok := receivedOf[customMsg](view); !ok {
		t.Error("an unrecognised message was not forwarded to the active view")
	}
}

// ── Selection mode ───────────────────────────────────────────────────────────

// The security view browses for a scan target through another view; the router
// remembers where to come back to.
func TestSelectionRequestOpensTheBrowserAndRemembersTheOrigin(t *testing.T) {
	tests := []struct {
		name string
		kind string
		want command.ViewType
	}{
		{"a directory is picked in workspaces", "directory", command.ViewWorkspaces},
		{"an image is picked in oci-resources", "image", command.ViewOCIResources},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := router(t, &fakeView{})
			a.currentView = command.ViewSecurity
			a.views[command.ViewSecurity] = &fakeView{}

			a.Update(security.SelectionRequestMsg{Type: tt.kind, Message: "pick one"})

			if a.currentView != tt.want {
				t.Errorf("current view = %s, want %s", a.currentView, tt.want)
			}
			if a.selectionReturnView != command.ViewSecurity {
				t.Errorf("return view = %s, want the security view that asked", a.selectionReturnView)
			}
		})
	}
}

// The answer goes back as the message the originating view understands, which
// differs between the explorer's pull destination and a scan target.
func TestTheSelectedPathGoesBackAsTheOriginExpects(t *testing.T) {
	t.Run("the security view gets a selection result", func(t *testing.T) {
		origin := &fakeView{}
		a := router(t, &fakeView{})
		a.views[command.ViewSecurity] = origin
		a.selectionReturnView = command.ViewSecurity

		a.Update(workspaces.DirectorySelectedMsg{Path: "/repos/devdesk"})

		got, ok := receivedOf[security.SelectionResultMsg](origin)
		if !ok {
			t.Fatal("the security view never received the selected path")
		}
		if got.Path != "/repos/devdesk" {
			t.Errorf("path = %q, want /repos/devdesk", got.Path)
		}
	})

	t.Run("the explorer gets a pull destination", func(t *testing.T) {
		origin := &fakeView{}
		a := router(t, &fakeView{})
		a.views[command.ViewGitlabExplorer] = origin
		a.selectionReturnView = command.ViewGitlabExplorer

		a.Update(workspaces.DirectorySelectedMsg{Path: "/repos"})

		if _, ok := receivedOf[explorer.PullDestinationSelectedMsg](origin); !ok {
			t.Errorf("the explorer received %d messages, none of them a pull destination", len(origin.received))
		}
	})
}

// Cancelling returns to the origin and tells it so, rather than leaving the
// user in a browser they did not ask for.
func TestCancellingSelectionReturnsToTheOrigin(t *testing.T) {
	origin := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewSecurity] = origin
	a.selectionReturnView = command.ViewSecurity
	a.currentView = command.ViewWorkspaces

	a.Update(workspaces.SelectionCancelledMsg{})

	if a.currentView != command.ViewSecurity {
		t.Errorf("current view = %s, want the security view back", a.currentView)
	}
	if _, ok := receivedOf[security.SelectionCancelledMsg](origin); !ok {
		t.Error("the security view was not told the selection was cancelled")
	}
}

// The workspaces view is dropped so it is rebuilt out of selection mode; the
// OCI view is kept and reset, because it holds scan state worth preserving.
func TestLeavingSelectionResetsTheBrowserWithoutLosingScanState(t *testing.T) {
	oci := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = &fakeView{}
	a.views[command.ViewOCIResources] = oci
	a.views[command.ViewSecurity] = &fakeView{}
	a.selectionReturnView = command.ViewSecurity

	a.Update(workspaces.SelectionCancelledMsg{})

	if _, kept := a.views[command.ViewWorkspaces]; kept {
		t.Error("the workspaces view survived selection mode and will reopen in it")
	}
	if _, ok := a.views[command.ViewOCIResources]; !ok {
		t.Fatal("the OCI view was dropped, losing its scan results")
	}
	if _, ok := receivedOf[ociresources.ResetSelectionMsg](oci); !ok {
		t.Error("the OCI view was not taken out of selection mode")
	}
}

// ── Cached scan results ──────────────────────────────────────────────────────

// Rule 126: Enter on a scanned image reads the cache, it does not rescan.
func TestScanDetailsAsksTheCacheForTheResult(t *testing.T) {
	a := router(t, &fakeView{})

	_, cmd := a.Update(ociresources.ScanDetailsRequestMsg{ImageName: "api:v1"})

	loaded, ok := testutil.MsgOf[ImageScanResultLoadedMsg](cmd)
	if !ok {
		t.Fatalf("the request produced %T, want a cache load", testutil.Msg(cmd))
	}
	if loaded.ImageName != "api:v1" {
		t.Errorf("the cache was asked for %q, want api:v1", loaded.ImageName)
	}
}

// A cache entry whose result file is gone must not dead-end. The image falls
// back to a fresh scan, the workspace to the form with its target filled in.
func TestAMissingResultFileFallsBackInsteadOfFailingSilently(t *testing.T) {
	t.Run("an image falls back to scanning", func(t *testing.T) {
		a := router(t, &fakeView{})

		_, cmd := a.Update(ImageScanResultLoadedMsg{ImageName: "api:v1", Err: errNotOnDisk})

		if a.currentView != command.ViewSecurity {
			t.Fatalf("current view = %s, want the security view", a.currentView)
		}
		if _, ok := testutil.MsgOf[security.StartScanMsg](cmd); !ok {
			t.Error("no scan was started for an image whose cached result is gone")
		}
	})

	t.Run("a workspace falls back to the form", func(t *testing.T) {
		a := router(t, &fakeView{})

		_, cmd := a.Update(WorkspaceScanResultLoadedMsg{RepoPath: "/repos/devdesk", Err: errNotOnDisk})

		if a.currentView != command.ViewSecurity {
			t.Fatalf("current view = %s, want the security view", a.currentView)
		}
		if _, ok := testutil.MsgOf[security.StartScanMsg](cmd); ok {
			t.Error("a workspace scan started on its own; the user must confirm the target first")
		}
	})
}
