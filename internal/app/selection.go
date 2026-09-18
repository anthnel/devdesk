package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/forge/explorer"
	"github.com/anthnel/devdesk/internal/ui/templates"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// Selection mode lets one view borrow another to pick something. The router
// remembers where to come back to and delivers the answer.
//
// The explorer is the only borrower, and it borrows two views: the workspaces
// view for a clone destination, and the templates view for the template of a
// repository being created. Each borrow has its own request and its own two
// answers — chosen and cancelled — because the borrower's messages are its own
// vocabulary; what they share is where to come back to (selectionReturnView) and
// what to drop on the way out (selectionLent).

// cloneDestinationPrompt is shown while the explorer borrows the workspaces view.
const cloneDestinationPrompt = "Enter into a parent directory — the repo will be cloned inside it"

// templatePrompt is shown while the explorer borrows the templates view.
const templatePrompt = "Choose the template the new repository starts from"

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
	a.selectionLent = view
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

// handleExplorerTemplateRequest borrows the templates view to pick a template.
func (a *App) handleExplorerTemplateRequest() (tea.Model, tea.Cmd) {
	a.selectionReturnView = a.currentView
	return a, a.openBrowser(
		command.ViewTemplates,
		templates.NewForSelection(a.config, a.sharedState.Secrets.Storage, templatePrompt),
	)
}

// handleTemplateSelected returns to the explorer with the template chosen.
func (a *App) handleTemplateSelected(msg templates.TemplateSelectedMsg) (tea.Model, tea.Cmd) {
	a.leaveSelectionMode()
	return a, a.returnToOrigin(explorer.TemplateChosenMsg{Slug: msg.Slug, Name: msg.Name})
}

// handleTemplateSelectionCancelled returns to the explorer without a choice.
func (a *App) handleTemplateSelectionCancelled() (tea.Model, tea.Cmd) {
	a.leaveSelectionMode()
	return a, a.returnToOrigin(explorer.TemplateChoiceCancelledMsg{})
}

// leaveSelectionMode drops the lent view so it is rebuilt normally, and goes
// back to the borrower.
func (a *App) leaveSelectionMode() {
	a.currentView = a.selectionReturnView
	delete(a.views, a.selectionLent)
	a.selectionLent = ""
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
