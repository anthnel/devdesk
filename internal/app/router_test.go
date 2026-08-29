package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/keymap"
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

			for _, key := range testutil.Keys(keymap.Delete, "p", "enter", "down") {
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

	for _, key := range testutil.Keys(keymap.Scan, "x", "enter", "/") {
		a.handleKeyMsg(key.(tea.KeyMsg))
	}

	want := []string{keymap.Scan, "x", "enter", "/"}
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

// D67: a workspace scan reports from wherever the user has gone, and never
// takes the screen back. It used to switch to the workspaces view on every
// completed repository, so a batch of a dozen made every other view unusable
// until the last one landed — which is exactly the length of time one wants to
// spend elsewhere.
//
// The message still reaches the workspaces view: it holds the row's marker and
// writes the scan cache, so a completion dropped because the user walked away
// leaves the row spinning for the life of the view.
func TestLongRunningWorkReportsWithoutTakingTheScreen(t *testing.T) {
	messages := []struct {
		name   string
		msg    tea.Msg
		target command.ViewType
	}{
		{"scan starting", workspaces.WorkspaceScanStartingMsg{RepoPath: "/repos/devdesk"}, command.ViewWorkspaces},
		{"scan complete", workspaces.WorkspaceScanCompleteMsg{RepoPath: "/repos/devdesk"}, command.ViewWorkspaces},
		{"sync starting", workspaces.WorkspaceSyncStartingMsg{RepoPath: "/repos/devdesk"}, command.ViewWorkspaces},
		{"sync complete", workspaces.WorkspaceSyncCompleteMsg{RepoPath: "/repos/devdesk"}, command.ViewWorkspaces},
		{"delete finished", workspaces.EntryDeletedMsg{Path: "/repos/devdesk"}, command.ViewWorkspaces},
		{"image scan starting", ociresources.ImageScanStartingMsg{}, command.ViewOCIResources},
		{"image scan finished", ociresources.ImageScanFinishedMsg{}, command.ViewOCIResources},
		{"inventory scan starting", security.InventoryScanStartingMsg{}, command.ViewSecurity},
		{"inventory scan finished", security.InventoryScanFinishedMsg{}, command.ViewSecurity},
	}

	// Every view the user could be on while the work runs, the one that started
	// it included: a second scan finishing must not pull them off the security
	// view either, which was the one case the old guard covered.
	for _, onScreen := range []command.ViewType{
		command.ViewDashboard,
		command.ViewSecurity,
		command.ViewWorkspaces,
		command.ViewOCIResources,
	} {
		for _, tc := range messages {
			t.Run(tc.name+" from "+string(onScreen), func(t *testing.T) {
				owner := &fakeView{}
				a := router(t, &fakeView{})
				a.views[command.ViewWorkspaces] = &fakeView{}
				a.views[command.ViewSecurity] = &fakeView{}
				a.views[command.ViewOCIResources] = &fakeView{}
				a.views[tc.target] = owner
				if onScreen != tc.target {
					a.views[onScreen] = &fakeView{}
				} else {
					a.views[onScreen] = owner
				}
				a.currentView = onScreen

				a.Update(tc.msg)

				if a.currentView != onScreen {
					t.Errorf("the view switched to %s, want it left on %s", a.currentView, onScreen)
				}
				if len(owner.received) == 0 {
					t.Errorf("%s never reached the %s view", tc.name, tc.target)
				}
			})
		}
	}
}

// The owning view is addressed by name, so a message reaches it even when the
// user is looking at something else — and is not also handed to what is on
// screen, which would make an unrelated view act on another's progress.
func TestWorkStaysWithTheViewThatStartedIt(t *testing.T) {
	ws := &fakeView{}
	dashboard := &fakeView{}
	a := router(t, dashboard)
	a.views[command.ViewWorkspaces] = ws

	a.Update(workspaces.WorkspaceScanCompleteMsg{RepoPath: "/repos/devdesk"})

	if _, ok := receivedOf[workspaces.WorkspaceScanCompleteMsg](ws); !ok {
		t.Error("the workspaces view was not updated with its scan result")
	}
	if _, ok := receivedOf[workspaces.WorkspaceScanCompleteMsg](dashboard); ok {
		t.Error("the scan result was also delivered to the active view")
	}
}

// A view the router does not hold is not a crash: the message is dropped and
// the current view is left where it was.
func TestRoutingToAMissingViewIsSilent(t *testing.T) {
	a := router(t, &fakeView{})
	delete(a.views, command.ViewWorkspaces)

	a.Update(workspaces.WorkspaceScanCompleteMsg{RepoPath: "/repos/devdesk"})

	if a.currentView != command.ViewDashboard {
		t.Errorf("current view = %s, want the dashboard", a.currentView)
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

// A cache entry whose result file is gone must not dead-end.
//
// It used to open the security form with the target filled in. With the form
// gone the fallback stays in the list the user pressed enter in and asks it to
// rescan: that is where the target lives and where the scan that replaces the
// missing result runs.
func TestAMissingResultFileRescansInTheListItCameFrom(t *testing.T) {
	t.Run("an image", func(t *testing.T) {
		a := router(t, &fakeView{})
		oci := &fakeView{}
		a.views[command.ViewOCIResources] = oci

		a.Update(ImageScanResultLoadedMsg{ImageName: "api:v1", Err: errNotOnDisk})

		if a.currentView != command.ViewOCIResources {
			t.Fatalf("current view = %s, want the images list", a.currentView)
		}
		request, ok := receivedOf[ociresources.ScanRequestMsg](oci)
		if !ok {
			t.Fatal("the images list was not asked to rescan")
		}
		if request.ImageName != "api:v1" {
			t.Errorf("the request names %q, want api:v1", request.ImageName)
		}
	})

	t.Run("a repository", func(t *testing.T) {
		a := router(t, &fakeView{})
		ws := &fakeView{}
		a.views[command.ViewWorkspaces] = ws

		a.Update(WorkspaceScanResultLoadedMsg{RepoPath: "/repos/devdesk", Err: errNotOnDisk})

		if a.currentView != command.ViewWorkspaces {
			t.Fatalf("current view = %s, want the workspaces list", a.currentView)
		}
		request, ok := receivedOf[workspaces.ScanRequestMsg](ws)
		if !ok {
			t.Fatal("the workspaces list was not asked to rescan")
		}
		if request.TargetPath != "/repos/devdesk" {
			t.Errorf("the request names %q, want /repos/devdesk", request.TargetPath)
		}
	})
}

// Same for a rescan started from the security inventory. The row that launched
// it is marked as scanning, and a reload deliberately keeps that marker, so a
// completion delivered to the wrong view leaves it spinning for good.
func TestAnInventoryRescanReachesTheSecurityViewFromAnotherView(t *testing.T) {
	sec := &fakeView{}
	dashboard := &fakeView{}
	a := router(t, dashboard)
	a.views[command.ViewSecurity] = sec

	a.Update(security.InventoryScanFinishedMsg{Name: "nexus/api:1.4"})

	if _, ok := receivedOf[security.InventoryScanFinishedMsg](sec); !ok {
		t.Error("the rescan-finished message never reached the security view")
	}
	if a.currentView != command.ViewDashboard {
		t.Errorf("routing a background rescan switched the view to %s", a.currentView)
	}
	if _, ok := receivedOf[security.InventoryScanFinishedMsg](dashboard); ok {
		t.Error("the rescan message was also delivered to the active view")
	}
}

// The success path of the same two handlers: a stored result opens the security
// view on it, and records where esc returns to. This is what openSecurityView
// exists for, and the missing-file tests above only exercise the fallback.
func TestAStoredResultOpensTheSecurityViewOnItsOrigin(t *testing.T) {
	tests := []struct {
		name   string
		msg    tea.Msg
		origin command.ViewType
	}{
		{
			"an image",
			ImageScanResultLoadedMsg{ImageName: "api:v1", Result: &scan.Result{Target: "api:v1"}},
			command.ViewOCIResources,
		},
		{
			"a repository",
			WorkspaceScanResultLoadedMsg{RepoPath: "/repos/devdesk", Result: &scan.Result{Target: "/repos/devdesk"}},
			command.ViewWorkspaces,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := router(t, &fakeView{})

			a.Update(tt.msg)

			if a.currentView != command.ViewSecurity {
				t.Fatalf("current view = %s, want the security view", a.currentView)
			}
			view, ok := a.views[command.ViewSecurity].(security.Model)
			if !ok {
				t.Fatalf("the installed view is %T, want a security.Model", a.views[command.ViewSecurity])
			}
			if view.OriginView != tt.origin {
				t.Errorf("OriginView = %q, want %q — esc would return to the wrong list", view.OriginView, tt.origin)
			}
		})
	}
}
