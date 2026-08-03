package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/gitlab/explorer"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// Selection mode lets one view borrow another to pick something — a scan
// target, a clone destination. The router remembers where to come back to and
// translates the answer into the message the borrower understands.

// pullDestinationPrompt is shown while the explorer borrows the workspaces view.
const pullDestinationPrompt = "Enter into a parent directory — the repo will be cloned inside it"

// handleSelectionRequest switches to workspaces or oci_resources in selection mode
func (a *App) handleSelectionRequest(msg security.SelectionRequestMsg) (tea.Model, tea.Cmd) {
	a.selectionReturnView = a.currentView

	switch msg.Type {
	case "directory":
		log.Printf("Selection request: switching to workspaces for directory selection")
		return a, a.openBrowser(command.ViewWorkspaces, workspaces.NewForSelection(a.config, msg.Message))
	case "image":
		log.Printf("Selection request: switching to OCI images for image selection")
		return a, a.openBrowser(command.ViewOCIResources, ociresources.NewForSelection(a.config, msg.Message))
	}
	return a, nil
}

// handleExplorerPullRequest borrows the workspaces view to pick a clone target.
func (a *App) handleExplorerPullRequest() (tea.Model, tea.Cmd) {
	a.selectionReturnView = a.currentView
	log.Printf("Explorer pull request: switching to workspaces for directory selection")
	return a, a.openBrowser(
		command.ViewWorkspaces,
		workspaces.NewForSelection(a.config, pullDestinationPrompt),
	)
}

// openBrowser installs a view built in selection mode and lays it out.
func (a *App) openBrowser(view command.ViewType, model tea.Model) tea.Cmd {
	a.views[view] = model
	a.currentView = view
	return tea.Batch(model.Init(), a.requestResize())
}

// handleDirectorySelected returns to the originating view with the chosen path.
func (a *App) handleDirectorySelected(msg workspaces.DirectorySelectedMsg) (tea.Model, tea.Cmd) {
	log.Printf("Directory selected: %s, returning to %s", msg.Path, a.selectionReturnView)
	a.leaveSelectionMode()

	var result tea.Msg = security.SelectionResultMsg{Path: msg.Path}
	if a.selectionReturnView == command.ViewGitlabExplorer {
		result = explorer.PullDestinationSelectedMsg{Path: msg.Path}
	}
	return a, a.returnToOrigin(result)
}

// handleImageSelected returns to the security view with the chosen image.
func (a *App) handleImageSelected(msg ociresources.ImageSelectedMsg) (tea.Model, tea.Cmd) {
	log.Printf("Image selected: %s, returning to %s", msg.ImageName, a.selectionReturnView)
	a.currentView = a.selectionReturnView
	a.resetSelectionModeFor(command.ViewOCIResources)

	return a, a.returnToOrigin(security.SelectionResultMsg{Path: msg.ImageName})
}

// handleSelectionCancelled returns to the originating view without changes.
func (a *App) handleSelectionCancelled() (tea.Model, tea.Cmd) {
	log.Printf("Selection cancelled, returning to %s", a.selectionReturnView)
	a.leaveSelectionMode()

	var cancel tea.Msg = security.SelectionCancelledMsg{}
	if a.selectionReturnView == command.ViewGitlabExplorer {
		cancel = explorer.PullSelectionCancelledMsg{}
	}
	return a, a.returnToOrigin(cancel)
}

// leaveSelectionMode takes both borrowable views out of it. Workspaces is
// dropped so it is rebuilt normally; the OCI view is reset in place because it
// holds scan results worth keeping.
func (a *App) leaveSelectionMode() {
	a.currentView = a.selectionReturnView
	delete(a.views, command.ViewWorkspaces)
	a.resetSelectionModeFor(command.ViewOCIResources)
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
