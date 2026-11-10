package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/containers"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// seedContainersFooter opens containers with one running container whose stop
// is in flight. It drives the view's Update directly and never runs a returned
// Cmd: the stop would call the engine (PATH is emptied as a second guard).
func seedContainersFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	a.createView(command.ViewContainers)
	var m tea.Model = a.views[command.ViewContainers]
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: a.width, Height: a.height},
		containers.ContainersListMsg{Containers: []docker.Container{{
			ID: strings.Repeat("ab", 32), Name: "ghost", Image: "busybox", State: "running", Status: "Up 3 hours",
		}}},
		testutil.Key(keymap.Kill),
		components.ChoiceModalPickedMsg{Label: "Stop"},
	} {
		m, _ = m.Update(msg)
	}
	a.setView(command.ViewContainers, m)
	return indicator.Running
}
