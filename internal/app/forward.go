package app

import (
	"errors"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
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
		f, err := registry.Open(msg.LocalPort, msg.Target)
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
		a.saveForwardsCmd(),
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
	return a, tea.Batch(a.broadcastForwards(), a.saveForwardsCmd())
}

// handleForwardToggle pauses or resumes a forward. Resuming dials the target and
// binds the port, so it runs in a Cmd, like an open.
func (a *App) handleForwardToggle(msg forward.ToggleMsg) (tea.Model, tea.Cmd) {
	registry := a.sharedState.Forwards
	return a, func() tea.Msg {
		f, err := registry.Toggle(msg.ID)
		return forward.ToggledMsg{Forward: f, Err: err}
	}
}

// handleForwardToggled reports a resume that could not bind — the row already
// shows it unbound with the reason, the footer says what to do about it — and
// keeps the file in step with what the user just decided.
func (a *App) handleForwardToggled(msg forward.ToggledMsg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{a.broadcastForwards()}

	switch {
	case errors.Is(msg.Err, forward.ErrToggleBusy):
		// Nothing changed and nothing needs saving.
		cmds = append(cmds, components.PostFooter(components.LevelWarning, "That forward is already being switched"))
		return a, tea.Batch(cmds...)
	case errors.Is(msg.Err, forward.ErrNoSuchForward):
		// Closed while it was being resumed: the Close already saved.
		return a, tea.Batch(cmds...)
	case msg.Err != nil:
		log.Printf("WARN [app/forward] resume %s: %v", msg.Forward.ID, msg.Err)
		cmds = append(cmds, components.PostFooter(components.LevelWarning, forwardRefusal(msg.Err)))
	}
	return a, tea.Batch(append(cmds, a.saveForwardsCmd())...)
}

// useForwardStore points the router at ~/.devdesk/forwards.yaml.
//
// It is called by New and not by newWithSize, for the reason useSecrets is: the
// constructor tests build must not reach for the machine, and a router that
// wrote the developer's real forwards file from a test would be the worst way
// to find that out.
func (a *App) useForwardStore() {
	dir, err := config.ConfigDir()
	if err != nil {
		log.Printf("ERROR [app/forward] no configuration directory, forwards will not be saved: %v", err)
		return
	}
	a.forwardStore = forward.NewStore(forward.StorePath(dir))
}

// restoreForwardsCmd reopens the saved forwards. It reads a file and binds
// ports, so it is a Cmd; it is a no-op without a store.
func (a *App) restoreForwardsCmd() tea.Cmd {
	store, registry := a.forwardStore, a.sharedState.Forwards
	if store == nil {
		return nil
	}
	return func() tea.Msg {
		entries, err := store.Load()
		if err != nil {
			return forward.RestoredMsg{Err: err}
		}
		return forward.RestoredMsg{Summary: registry.Restore(entries)}
	}
}

// handleForwardRestored says what came back. An unreadable file is an error
// and is left exactly as it was; the first change afterwards moves it aside.
func (a *App) handleForwardRestored(msg forward.RestoredMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [app/forward] restore: %v", msg.Err)
		return a, components.PostFooter(components.LevelError,
			"The saved forwards could not be read — check logs")
	}
	sum := msg.Summary
	log.Printf("Forwards restored: %d live, %d paused, %d unbound", sum.Live, sum.Paused, sum.Unbound)

	cmds := []tea.Cmd{a.broadcastForwards()}
	switch {
	case sum.Unbound > 0:
		cmds = append(cmds, components.PostFooter(components.LevelWarning, fmt.Sprintf(
			"%d of %d forwards could not be bound — see the Forward tab", sum.Unbound, sum.Total())))
	case sum.Live > 0:
		cmds = append(cmds, components.PostFooter(components.LevelInfo,
			fmt.Sprintf("Restored %d %s", sum.Live, plural(sum.Live, "forward", "forwards"))))
	}
	return a, tea.Batch(cmds...)
}

// saveForwardsCmd writes the forwards file.
//
// The list is copied here, in Update, and the Cmd works on the copy: a Cmd that
// read the registry itself would be racing the goroutines that keep it running
// (Rule 110). It returns nothing when the write works — that is what every
// change expects — and a SavedMsg when it does not, because losing a write in
// silence is the one thing a persisted list must not do.
func (a *App) saveForwardsCmd() tea.Cmd {
	store := a.forwardStore
	if store == nil {
		return nil
	}
	entries := a.sharedState.Forwards.Entries()
	return func() tea.Msg {
		if err := store.Save(entries); err != nil {
			return forward.SavedMsg{Err: err}
		}
		return nil
	}
}

// handleForwardSaved reports a failed write.
func (a *App) handleForwardSaved(msg forward.SavedMsg) (tea.Model, tea.Cmd) {
	if msg.Err == nil {
		return a, nil
	}
	log.Printf("ERROR [app/forward] save: %v", msg.Err)
	return a, components.PostFooter(components.LevelError, "Failed to save the forwards — check logs")
}

// plural picks the noun for a count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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
