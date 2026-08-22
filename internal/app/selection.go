package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/forge/explorer"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// Selection mode lets one view borrow another to pick something. The router
// remembers where to come back to and delivers the answer.
//
// There were two borrowers: the security form, which borrowed the workspaces
// view for a directory and the images view for an image, and the explorer, which
// borrows the workspaces view for a clone destination. The form is gone
// (phase 3) — a scan target is a row of the security inventory now — so the
// explorer is the only borrower left, and only the workspaces view is ever lent.
// That is why nothing here is parameterised any more.

// cloneDestinationPrompt is shown while the explorer borrows the workspaces view.
const cloneDestinationPrompt = "Enter into a parent directory — the repo will be cloned inside it"

// handleExplorerCloneRequest borrows the workspaces view to pick a clone target.
func (a *App) handleExplorerCloneRequest() (tea.Model, tea.Cmd) {
	a.selectionReturnView = a.currentView
	log.Printf("Explorer clone request: switching to workspaces for directory selection")
	return a, a.openBrowser(
		command.ViewWorkspaces,
		workspaces.NewForSelection(a.config, cloneDestinationPrompt),
	)
}

// openBrowser installs a view built in selection mode and lays it out.
func (a *App) openBrowser(view command.ViewType, model tea.Model) tea.Cmd {
	a.views[view] = model
	a.currentView = view
	return tea.Batch(model.Init(), a.requestResize())
}

// handleDirectorySelected returns to the explorer with the chosen path.
func (a *App) handleDirectorySelected(msg workspaces.DirectorySelectedMsg) (tea.Model, tea.Cmd) {
	log.Printf("Directory selected: %s, returning to %s", msg.Path, a.selectionReturnView)
	a.leaveSelectionMode()
	return a, a.returnToOrigin(explorer.CloneDestinationSelectedMsg{Path: msg.Path})
}

// handleSelectionCancelled returns to the explorer without changes.
func (a *App) handleSelectionCancelled() (tea.Model, tea.Cmd) {
	log.Printf("Selection cancelled, returning to %s", a.selectionReturnView)
	a.leaveSelectionMode()
	return a, a.returnToOrigin(explorer.CloneSelectionCancelledMsg{})
}

// leaveSelectionMode drops the workspaces view so it is rebuilt normally.
func (a *App) leaveSelectionMode() {
	a.currentView = a.selectionReturnView
	delete(a.views, command.ViewWorkspaces)
}

// returnToOrigin delivers the outcome to the view that asked and lays it out.
func (a *App) returnToOrigin(msg tea.Msg) tea.Cmd {
	view, ok := a.views[a.currentView]
	if !ok {
		return nil
	}
	updatedView, cmd := view.Update(msg)
	a.views[a.currentView] = updatedView
	a.viewport.GotoTop()
	return tea.Batch(cmd, a.requestResize())
}
