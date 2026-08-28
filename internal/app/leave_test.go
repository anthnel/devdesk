package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
)

// leavingView is a fakeView that also answers LeavingView, which fakeView
// deliberately does not: the router's fallback for a view without the interface
// is a path worth keeping under test, and every other test exercises it.
type leavingView struct {
	fakeView
	allow  bool
	cmd    tea.Cmd
	leaves int
}

func (v *leavingView) Leave() (tea.Model, tea.Cmd, bool) {
	v.leaves++
	return v, v.cmd, v.allow
}

func newAppWithLeaver(t *testing.T, allow bool) (*App, *leavingView) {
	t.Helper()
	app := newWithSize(testConfig(), 120, 40)
	leaver := &leavingView{allow: allow}
	app.views[command.ViewConfiguration] = leaver
	app.currentView = command.ViewConfiguration
	return app, leaver
}

// The defect the interface exists for: switchView told nobody, so a view
// holding an uncommitted edit lost it without a word (§1.3 D62).
func TestSwitchViewSettlesTheViewItLeaves(t *testing.T) {
	app, leaver := newAppWithLeaver(t, true)

	app.switchView(command.ViewWorkspaces)

	if leaver.leaves != 1 {
		t.Errorf("Leave() called %d times, want 1", leaver.leaves)
	}
	if app.currentView != command.ViewWorkspaces {
		t.Errorf("currentView = %q, want the switch to have happened", app.currentView)
	}
}

// A refusal keeps the screen, and carries what the view wants to say about it.
func TestARefusingViewCancelsTheSwitch(t *testing.T) {
	app, leaver := newAppWithLeaver(t, false)
	leaver.cmd = func() tea.Msg { return "refused" }

	cmd := app.switchView(command.ViewWorkspaces)

	if app.currentView != command.ViewConfiguration {
		t.Errorf("currentView = %q, want to have stayed on configuration", app.currentView)
	}
	if _, built := app.views[command.ViewWorkspaces]; built {
		t.Error("the destination view was built despite the refusal")
	}
	if cmd == nil || cmd() != "refused" {
		t.Error("the refusal's own message was dropped, so nothing says why")
	}
}

// Re-entering the view already on screen is not leaving it. Committing there
// would make :cfg from :cfg a save, which is not what the user typed.
func TestReenteringTheSameViewSettlesNothing(t *testing.T) {
	app, leaver := newAppWithLeaver(t, true)

	app.switchView(command.ViewConfiguration)

	if leaver.leaves != 0 {
		t.Errorf("Leave() called %d times when the view did not change", leaver.leaves)
	}
}

// A context switch rebuilds every view against a different file, so an
// uncommitted edit would be dropped — and dropped against the wrong config.
func TestSwitchContextSettlesTheCurrentView(t *testing.T) {
	app, leaver := newAppWithLeaver(t, true)

	app.switchContext("other")

	if leaver.leaves != 1 {
		t.Errorf("Leave() called %d times, want 1", leaver.leaves)
	}
}

// The switch is refused before the Cmd that would load the context is built:
// a refusal must not leave a half-done switch in flight.
func TestARefusingViewCancelsTheContextSwitch(t *testing.T) {
	app, leaver := newAppWithLeaver(t, false)
	leaver.cmd = func() tea.Msg { return "refused" }

	cmd := app.switchContext("other")

	if cmd == nil || cmd() != "refused" {
		t.Error("switchContext went ahead, or dropped the reason it did not")
	}
}

// Every other view is left without ceremony, and must be: the interface is
// optional and the router probes for it silently.
func TestAViewWithoutTheInterfaceIsLeftUntouched(t *testing.T) {
	app := newWithSize(testConfig(), 120, 40)
	app.views[command.ViewStatus] = &fakeView{}
	app.currentView = command.ViewStatus

	app.switchView(command.ViewWorkspaces)

	if app.currentView != command.ViewWorkspaces {
		t.Errorf("currentView = %q, want the switch to have happened", app.currentView)
	}
}

// The configuration view is the one that implements it, and the contract is
// worth pinning: this is the view the defect was found in.
func TestTheConfigurationViewIsALeavingView(t *testing.T) {
	app := newWithSize(testConfig(), 120, 40)
	app.createView(command.ViewConfiguration)

	if _, ok := app.views[command.ViewConfiguration].(LeavingView); !ok {
		t.Error("the configuration view no longer implements LeavingView; a typed value is lost again on ctrl+p")
	}
}
