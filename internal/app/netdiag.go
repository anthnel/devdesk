package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
)

// handleNetdiagOpenRequest opens netdiag prefilled with a target named by
// whoever asked — today, a monitor selected in status (§3.66, keymap.Diagnose).
//
// It mirrors handleViewerOpenRequest: the producer names only what it knows
// (the target, and whether the port is certain enough to run immediately),
// the router builds the view and remembers where esc should return to.
func (a *App) handleNetdiagOpenRequest(msg netdiag.OpenRequestMsg) (tea.Model, tea.Cmd) {
	view := netdiag.NewWithTarget(a.config, msg.Target)
	view.OriginView = a.currentView

	a.views[command.ViewNetdiag] = view
	a.currentView = command.ViewNetdiag

	cmds := []tea.Cmd{view.Init(), a.requestResize()}
	if msg.AutoRun {
		var cmd tea.Cmd
		_, cmd = view.StartRun()
		cmds = append(cmds, cmd)
	}
	return a, tea.Batch(cmds...)
}
