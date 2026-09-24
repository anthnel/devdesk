package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
)

// handleOCIFocusImage switches to the OCI view and hands it the request, which
// it holds until its first listing if it was built just now.
//
// Switching first is what lets a view that refuses to be left keep the user:
// the request is then dropped rather than served to a view nobody is looking at.
func (a *App) handleOCIFocusImage(msg ociresources.FocusImageRequestMsg) (tea.Model, tea.Cmd) {
	switchCmd := a.switchView(command.ViewOCIResources)
	if a.currentView != command.ViewOCIResources {
		return a, switchCmd
	}
	_, focusCmd := a.routeToView(command.ViewOCIResources, msg)
	return a, tea.Batch(switchCmd, focusCmd)
}
