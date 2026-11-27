package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/containers"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-04 (DEVDESK-800): the answer of the shell detection `T` starts reaches the
// containers view wherever the user is. Without it, T then enter (inspect opens
// the viewer) left the answer to the viewer, the footer on "Opening shell…" and
// every later T refused.

// seedShellPending opens containers with a running container and presses T. The
// returned Cmd is never run: PATH is emptied as a second guard against reaching
// an engine.
func seedShellPending(t *testing.T, a *App) (id string) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	id = strings.Repeat("cd", 32)
	a.createView(command.ViewContainers)
	var m tea.Model = a.views[command.ViewContainers]
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: a.width, Height: a.height},
		containers.ContainersListMsg{Containers: []docker.Container{{
			ID: id, Name: "ghost", Image: "busybox", State: "running", Status: "Up 3 hours",
		}}},
		testutil.Key(keymap.Terminal),
	} {
		m, _ = m.Update(msg)
	}
	a.setView(command.ViewContainers, m)
	if !wantsBlink(t, a) {
		t.Fatal("setup: T did not mark the shell detection pending")
	}
	return id
}

func wantsBlink(t *testing.T, a *App) bool {
	t.Helper()
	v, ok := a.views[command.ViewContainers].(interface{ WantsBlink() bool })
	if !ok {
		t.Fatalf("the router holds a %T for containers", a.views[command.ViewContainers])
	}
	return v.WantsBlink()
}

// The user left containers while the daemon answered: the view still hears it,
// its wait ends, no shell is started and the current view does not change.
func TestShellDetectionAnswerReachesContainersOffScreen(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	id := seedShellPending(t, a)
	if a.currentView != command.ViewDashboard {
		t.Fatalf("setup: the current view is %s, want the dashboard", a.currentView)
	}

	_, cmd := a.Update(containers.ShellDetectedMsg{ID: id, Shell: "/bin/bash"})

	if wantsBlink(t, a) {
		t.Error("the containers view is still waiting: the answer sent while it was off screen was dropped")
	}
	if cmd != nil {
		t.Error("a shell was started for a user who is in another view")
	}
	if a.currentView != command.ViewDashboard {
		t.Errorf("current view = %s: delivering the answer changed which view is on screen", a.currentView)
	}
}

// With containers on screen the answer starts the shell, as before.
func TestShellDetectionAnswerOnScreenStartsTheShell(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	id := seedShellPending(t, a)
	a.currentView = command.ViewContainers

	_, cmd := a.Update(containers.ShellDetectedMsg{ID: id, Shell: "/bin/sh"})

	if wantsBlink(t, a) {
		t.Error("the containers view is still waiting after the answer")
	}
	if cmd == nil {
		t.Error("the answer started nothing: the shell was not launched")
	}
}

// No containers view was ever opened: the answer finds none and creates none.
func TestShellDetectionAnswerWithNoViewIsDropped(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)

	a.Update(containers.ShellDetectedMsg{ID: "x", Shell: "/bin/sh"})

	if _, exists := a.views[command.ViewContainers]; exists {
		t.Error("an answer with no view behind it created one")
	}
}
