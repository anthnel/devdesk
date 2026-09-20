package app

import (
	"errors"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forward"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// The router's half of the port forwarder (§3.1). It owns the registry for the
// reason it owns the MCP server: a listener held by a view would be dropped,
// still listening, by reinitializeViews.

// handleForwardOpen answers a view's request with the I/O it implies.
//
// Registry.Open probes the target with a three-second timeout and then binds,
// so it runs in a Cmd — doing it here would freeze the interface for as long
// as an unreachable target takes to fail, which is exactly the case this is
// most likely to meet.
//
// There is no epoch guard, unlike the MCP server's start, and the difference is
// worth stating: forwards are additive and independent, so a slow open landing
// after a later one is simply a second row. The MCP server needed one because
// there is only ever one of it, and a stale start's listener stored over the
// live one would answer for a context nobody is in.
func (a *App) handleForwardOpen(msg forward.OpenMsg) (tea.Model, tea.Cmd) {
	registry := a.sharedState.Forwards
	return a, func() tea.Msg {
		f, err := registry.Open(msg.LocalPort, msg.Target, msg.Label)
		return forward.OpenedMsg{Forward: f, Err: err}
	}
}

// handleForwardOpened reports the outcome and refreshes every view.
//
// The refusals are named rather than relayed: "bind: permission denied" reads
// like something a retry would fix, and the probe's failure against a container
// address on Docker Desktop reads like nothing at all (D55).
func (a *App) handleForwardOpened(msg forward.OpenedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [app/forward] open: %v", msg.Err)
		return a, components.PostFooter(components.LevelError, forwardRefusal(msg.Err))
	}
	log.Printf("Forward %s: %s to %s", msg.Forward.ID, msg.Forward.Addr(), msg.Forward.Target)
	return a, tea.Batch(
		a.broadcastForwards(),
		components.PostFooter(components.LevelInfo,
			fmt.Sprintf("Forwarding %s to %s", msg.Forward.Addr(), msg.Forward.Target)),
	)
}

// handleForwardClose stops a forward. Close only shuts a listener, so it does
// not need a Cmd of its own.
func (a *App) handleForwardClose(msg forward.CloseMsg) (tea.Model, tea.Cmd) {
	err := a.sharedState.Forwards.Close(msg.ID)
	if err != nil {
		log.Printf("ERROR [app/forward] close %s: %v", msg.ID, err)
		return a, components.PostFooter(components.LevelError, "Failed to stop the forward — check logs")
	}
	return a, a.broadcastForwards()
}

// handleForwardRefresh answers a view's tick with the current snapshot.
func (a *App) handleForwardRefresh() (tea.Model, tea.Cmd) {
	return a, a.broadcastForwards()
}

// broadcastForwards hands the snapshot to every view the router holds, on
// screen or not — the shape of broadcastJobs, and for the same reason: a view
// coming back on screen shows what happened while it was away without having
// to ask.
func (a *App) broadcastForwards() tea.Cmd {
	msg := forward.ChangedMsg{Forwards: a.sharedState.Forwards.List()}

	cmds := make([]tea.Cmd, 0, len(a.views))
	for name, view := range a.views {
		updated, cmd := view.Update(msg)
		a.views[name] = updated
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if newFooterHeight := a.getFooterHeight(); newFooterHeight != a.lastFooterHeight {
		a.resize(a.width, a.height)
	}
	return tea.Batch(cmds...)
}

// forwardRefusal turns a refusal into the sentence that says what to do about
// it. Matching on the sentinels rather than on the message keeps the wording
// here, where Rule 129 applies, instead of in whatever the OS said.
func forwardRefusal(err error) string {
	switch {
	case errors.Is(err, forward.ErrPrivilegedPort):
		return "Ports below 1024 need privileges — pick 1024 or above"
	case errors.Is(err, forward.ErrPortInUse):
		return "That local port is already taken — pick another"
	case errors.Is(err, forward.ErrTargetUnreachable):
		// The container case is named because it is the one that looks like a
		// DevDesk fault: the address is real, and on Docker Desktop it routes
		// to nothing outside the VM (D55).
		return "The target did not answer — a container address is only reachable from the host on native Linux"
	default:
		return "Could not open the forward — check logs"
	}
}
