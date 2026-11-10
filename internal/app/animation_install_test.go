package app

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/filebrowser"
	"github.com/anthnel/devdesk/internal/ui/security"
)

// ── DEVDESK-139: a view installed without switchView must not wait ───────────
//
// switchView hands a view entering the screen the snapshot and the frame. The
// paths below install or re-show a view some other way. Before the shared tick
// the 100 ms spinner tick refreshed them; now nothing does until the next
// registry transition, so each must do what switchView does.

// runningRouter returns a router with a scan in flight and a frame that is not
// the zero value, so "never received" and "received the zero frame" differ.
func runningRouter(t *testing.T, current Screen) *App {
	t.Helper()
	a := router(t, current)
	a.jobs.Start(scanRun("default", "a"))
	a.anim.frameIdx = 3
	a.anim.blinkOn = true
	return a
}

// unexported reads a field of a view built in another package. Len and String
// are the two reads reflect allows on an unexported field, which is all a
// snapshot and a frame need.
func unexported(t *testing.T, model tea.Model, path ...string) reflect.Value {
	t.Helper()
	v := reflect.ValueOf(model)
	for _, name := range path {
		v = v.FieldByName(name)
		if !v.IsValid() {
			t.Fatalf("%T has no field %q", model, name)
		}
	}
	return v
}

func wantSnapshotThenFrame(t *testing.T, v *fakeView) {
	t.Helper()
	snaps, frames := jobsSeen(v), framesSeen(v)
	if len(snaps) == 0 {
		t.Error("the installed view was never handed a ChangedMsg")
	} else if len(snaps[len(snaps)-1].Runs) == 0 {
		t.Error("the ChangedMsg it was handed carries no run")
	}
	if len(frames) == 0 {
		t.Fatal("the installed view was never handed a FrameMsg")
	}
	if !frames[len(frames)-1].Blink {
		t.Error("the FrameMsg does not carry the router's blink phase")
	}
}

func TestOpenBrowserHandsTheSnapshotAndTheFrame(t *testing.T) {
	lent := &fakeView{}
	a := runningRouter(t, &fakeView{})

	a.openBrowser(command.ViewWorkspaces, lent)

	wantSnapshotThenFrame(t, lent)
}

func TestReturnToOriginHandsTheFrameBack(t *testing.T) {
	origin := &fakeView{}
	a := runningRouter(t, origin)

	a.returnToOrigin(filebrowser.PickCancelledMsg{})

	if len(framesSeen(origin)) == 0 {
		t.Error("the view returned to was never handed a FrameMsg; it missed every tick while the borrowed view was on screen")
	}
}

func TestAPickerAnswerHandsTheFrameBackToTheBorrower(t *testing.T) {
	borrower := &fakeView{}
	a := runningRouter(t, borrower)
	a.handlePickRequest(filebrowser.PickRequestMsg{})
	before := len(framesSeen(borrower))

	a.handlePickAnswer(filebrowser.PickCancelledMsg{})

	if a.currentView != command.ViewDashboard {
		t.Fatalf("current view = %s, want the borrower back", a.currentView)
	}
	if len(framesSeen(borrower)) <= before {
		t.Error("the borrower was never handed a FrameMsg on coming back")
	}
}

func TestHandlePickRequestHandsThePickerTheSnapshotAndTheFrame(t *testing.T) {
	a := runningRouter(t, &fakeView{})

	a.handlePickRequest(filebrowser.PickRequestMsg{})

	picker := a.views[command.ViewFiles]
	if n := unexported(t, picker, "jobs").Len(); n == 0 {
		t.Error("the picker holds no run: it was installed without the snapshot")
	}
	if got := unexported(t, picker, "footer", "spinnerFrame").String(); got == "" {
		t.Error("the picker's footer has no spinner frame: it was installed without the frame")
	}
}

func TestOpenSecurityViewHandsItTheSnapshotAndTheFrame(t *testing.T) {
	a := runningRouter(t, &fakeView{})
	a.anim.blinkOn = false

	a.openSecurityView(security.New(a.config, nil), command.ViewWorkspaces)

	view := a.views[command.ViewSecurity]
	if n := unexported(t, view, "jobs").Len(); n == 0 {
		t.Error("the :sec view holds no run: it was installed without the snapshot")
	}
	// DEVDESK-145: the frame is no longer stamped on the rows. The inventory
	// holds the router's blink phase.
	if got := unexported(t, view, "inventory", "blinkOn").Bool(); got {
		t.Error("the :sec inventory blink phase is on, want the router's off phase")
	}
	if v := reflect.ValueOf(view).FieldByName("jobFrame"); v.IsValid() {
		t.Error("the :sec view still carries jobFrame")
	}
}
