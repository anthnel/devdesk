package app

import (
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-711 (S-02b): work netdiag started finishes in netdiag. A stop on the
// Ports tab is a chain of messages (the SIGTERM result, the probe ticks, the
// SIGKILL result) that arrive whenever the user is, so the router hands them to
// netdiag whether or not it is on screen. A message dropped because the user
// walked away leaves a row busy and, with the stop pending, K greyed for good.
//
// These tests call the views' Update directly and never run the Cmds they
// return: the PID is above the kernel's pid_max, so even a Cmd that slipped
// through could not signal a real process, and none sleeps on a footer timer.

// ghostPID is a PID no process can hold (above pid_max).
const ghostPID = "4194305"

// seedStop puts a stop in flight on the Ports tab of a netdiag view the router
// holds, off screen: seedNetdiagFooter's row, with the router left on the
// dashboard. It returns the view.
func seedStop(t *testing.T, a *App) *netdiag.Model {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no two-step stop: the kill result is covered by TestNetdiagWorkReachesItOffScreen on the other platforms")
	}
	seedNetdiagFooter(t, a)
	held, ok := a.views[command.ViewNetdiag].(*netdiag.Model)
	if !ok {
		t.Fatalf("the router holds a %T", a.views[command.ViewNetdiag])
	}
	if !held.WantsBlink() {
		t.Fatal("setup: no stop is marked on the row")
	}
	return held
}

// The result of a signal started in netdiag reaches it while another view is on
// screen: the row stops being busy and the dot goes out.
func TestNetdiagWorkReachesItOffScreen(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	held := seedStop(t, a)
	if a.currentView != command.ViewDashboard {
		t.Fatalf("setup: the current view is %s, want the dashboard", a.currentView)
	}

	a.Update(netdiag.NewPortsKillResultMsg(ghostPID))

	if held.WantsBlink() {
		t.Error("the row of the PID is still busy: the result sent while netdiag was off screen was dropped")
	}
	if got := a.views[command.ViewNetdiag]; got != held {
		t.Errorf("the router now holds %T %v, want the same view", got, got)
	}
	if a.currentView != command.ViewDashboard {
		t.Errorf("current view = %s: delivering the result changed which view is on screen", a.currentView)
	}
	row := portsRow(t, a)
	for _, on := range []bool{true, false} {
		if g := indicator.Glyph(indicator.Running, on); strings.Contains(row, g) {
			t.Errorf("the row still shows the running glyph %q: %q", g, row)
		}
	}
}

// With netdiag on screen the same message still arrives, through the active
// view as every message did before.
func TestNetdiagWorkOnScreenStillGoesThroughTheActiveView(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	held := seedStop(t, a)
	a.currentView = command.ViewNetdiag

	a.Update(netdiag.NewPortsKillResultMsg(ghostPID))

	if held.WantsBlink() {
		t.Error("the row of the PID is still busy: a result sent to the active netdiag view was dropped")
	}
	if a.currentView != command.ViewNetdiag {
		t.Errorf("current view = %s, want netdiag to stay on screen", a.currentView)
	}
}

// Nothing was started if netdiag was never opened: the message finds no view and
// creates none (routeToView's rule).
func TestNetdiagWorkWithNoViewIsDropped(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)

	a.Update(netdiag.NewPortsKillResultMsg(ghostPID))

	if _, exists := a.views[command.ViewNetdiag]; exists {
		t.Error("a result with no view behind it created one")
	}
	if a.currentView != command.ViewDashboard {
		t.Errorf("current view = %s, want the dashboard", a.currentView)
	}
}

// Only netdiag's own messages take this road: nothing the router already routes
// by another rule may implement the interface, or it would be redirected.
func TestOnlyNetdiagWorkIsBackgroundMessage(t *testing.T) {
	if _, ok := netdiag.NewPortsKillResultMsg(ghostPID).(netdiag.BackgroundMsg); !ok {
		t.Fatal("a kill result is not a netdiag.BackgroundMsg: the router cannot recognize it")
	}
	for name, msg := range map[string]tea.Msg{
		"a key":            testutil.Key("a"),
		"a resize":         tea.WindowSizeMsg{Width: 80, Height: 24},
		"a ports read":     netdiag.NewPortsDataMsg([]ports.Socket{{PID: ghostPID}}),
		"a netdiag open":   netdiag.OpenRequestMsg{},
		"a nil message":    nil,
		"an unrelated one": struct{ id int }{1},
	} {
		if _, ok := msg.(netdiag.BackgroundMsg); ok {
			t.Errorf("%s is a netdiag.BackgroundMsg: the router would reroute it", name)
		}
	}
}

// A stop in flight protects the netdiag view that was opened from another view
// (status's H) from being thrown away when the user names it again, like a run in
// flight does: dropping it would discard the wait without a word.
func TestNamingNetdiagDoesNotDropAStopInFlight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no two-step stop")
	}
	a := router(t, &fakeView{})
	held := openNetdiagFromStatus(t, a, false)
	if held.Running() {
		t.Fatal("setup: the view is running before anything started")
	}
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: a.width, Height: a.height},
		testutil.Key("tab"), // the Ports tab
		netdiag.NewPortsDataMsg([]ports.Socket{{
			Protocol: "tcp", State: "LISTEN", LocalAddr: "127.0.0.1:9", PID: ghostPID, Process: "ghost",
			Identity: ports.IdentityForTest(1),
		}}),
		testutil.Key(keymap.Kill),
		components.ConfirmModalYesMsg{},
	} {
		held.Update(msg)
	}
	if !held.Running() {
		t.Fatal("Running() is false during a stop: the router could drop the view and lose the wait")
	}

	enterNetdiagByCommand(t, a)

	if a.views[command.ViewNetdiag].(*netdiag.Model) != held {
		t.Error("a view with a stop in flight was thrown away")
	}
}
