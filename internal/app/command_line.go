package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/containers"
	"github.com/anthnel/devdesk/internal/ui/dashboard"
	"github.com/anthnel/devdesk/internal/ui/forge/auth"
	"github.com/anthnel/devdesk/internal/ui/forge/explorer"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/status"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// handleCommandMode gère le mode commande
func (a *App) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return a, a.closeCommandLine()

	case tea.KeyTab:
		// Cycle à travers les suggestions
		return a.handleCompletionCycle()

	case tea.KeyEnter:
		return a.runCommand()

	default:
		// Mettre à jour l'input
		var cmd tea.Cmd
		a.commandInput, cmd = a.commandInput.Update(msg)
		// Mettre à jour les suggestions après chaque frappe
		a.updateCompletions()
		return a, cmd
	}
}

// closeCommandLine leaves command mode and asks for the re-layout that gives
// the row back to the inactive prompt.
func (a *App) closeCommandLine() tea.Cmd {
	a.commandMode = false
	a.commandInput.Blur()
	a.resetCompletion()
	return a.requestResize()
}

// runCommand executes what the command line holds — or the highlighted
// suggestion, which is what makes tab-then-enter work.
func (a *App) runCommand() (tea.Model, tea.Cmd) {
	input := a.commandInput.Value()
	if len(a.completionSuggestions) > 0 {
		input = a.completionSuggestions[a.completionIndex].Text
	}

	cmd := command.ParseCommand(input)

	switch cmd.Type {
	case command.CommandQuit:
		a.resetCompletion()
		return a, tea.Quit

	case command.CommandView:
		a.commandMode = false
		a.commandInput.Blur()
		a.resetCompletion()
		a.resetSelectionModeFor(cmd.View)
		return a, a.switchView(cmd.View)

	case command.CommandContext:
		a.commandMode = false
		a.commandInput.Blur()
		a.resetCompletion()
		if len(cmd.Args) > 0 {
			// Créer/switch vers un contexte nommé
			return a, a.switchContext(cmd.Args[0])
		}
		// Pas d'args → ouvrir la modale interactive
		return a, a.listContexts()

	case command.CommandUnknown:
		// Commande invalide - rester en mode commande
		return a, nil
	}

	return a, nil
}

// resetSelectionModeFor takes a view out of selection mode before switching to
// it, by dropping it so it is rebuilt normally.
//
// Only the workspaces view is lent now. The images view was the other one — the
// security form borrowed it to pick a scan target — and it could not simply be
// dropped, because it holds scan results worth keeping; it took a
// ResetSelectionMsg instead. That whole path went with the form (phase 3).
func (a *App) resetSelectionModeFor(view command.ViewType) {
	if view == command.ViewWorkspaces {
		delete(a.views, view)
	}
}

// switchView change la vue courante
func (a *App) switchView(view command.ViewType) tea.Cmd {
	// Lazy loading des vues
	if _, exists := a.views[view]; !exists {
		a.createView(view)
	}

	a.currentView = view

	newView, ok := a.views[view]
	if !ok {
		return nil
	}
	log.Printf("Switching to view: %s, calling Init()", view)

	// Init() + un WindowSizeMsg pour forcer le redimensionnement
	return tea.Batch(newView.Init(), a.requestResize())
}

// createView crée une vue (lazy loading)
func (a *App) createView(view command.ViewType) {
	if _, exists := a.views[view]; exists && view != command.ViewGitAuth {
		return
	}

	switch view {
	case command.ViewDashboard:
		a.views[view] = dashboard.New(a.config, a.sharedState)
	case command.ViewStatus:
		a.views[view] = status.New(a.config)
	case command.ViewGitAuth:
		a.views[view] = a.newAuthView()
	case command.ViewGitExplorer:
		a.views[view] = explorer.New(a.config, a.sharedState)
	case command.ViewWorkspaces:
		a.views[view] = workspaces.New(a.config, a.sharedState.Secrets.Storage)
	case command.ViewSecurity:
		a.views[view] = security.New(a.config, a.sharedState.Secrets.Storage)
	case command.ViewContainers:
		a.views[view] = containers.New(a.config)
	case command.ViewOCIResources:
		a.views[view] = ociresources.New(a.config)
	case command.ViewNetdiag:
		a.views[view] = netdiag.New(a.config)
	case command.ViewConfiguration:
		a.views[view] = configuration.New(a.config)
	case command.ViewViewer:
		// Empty: the viewer is normally installed with its source by
		// handleViewerOpenRequest, and this case exists so the router can build
		// one like any other view — which is what the contract test does.
		a.views[view] = uiviewer.New(a.config)
	}
}

// newAuthView builds the GitLab auth view against this context's credentials.
// It is rebuilt on every switch rather than cached, so the form reflects the
// session that is current now.
func (a *App) newAuthView() tea.Model {
	log.Printf("Creating GitLab auth view with context: %s", a.currentContext)

	authView := auth.New(a.config, a.sharedState.Secrets, a.sharedState.SecretNotices)
	if a.sharedState.IsAuthenticated {
		authView.SetAuth(a.sharedState.Forge, a.sharedState.CurrentUser)
	}
	return authView
}

// updateCompletions met à jour les suggestions basées sur l'input actuel
func (a *App) updateCompletions() {
	input := a.commandInput.Value()

	// Reset index si input change
	if input != a.completionInput {
		a.completionIndex = 0
		a.completionInput = input
	}

	a.completionSuggestions = a.completionEngine.GetSuggestions(input)
}

// handleCompletionCycle cycle à travers les suggestions (Tab répété)
func (a *App) handleCompletionCycle() (tea.Model, tea.Cmd) {
	if len(a.completionSuggestions) == 0 {
		return a, nil
	}

	// Increment avec wrap-around
	a.completionIndex = (a.completionIndex + 1) % len(a.completionSuggestions)
	return a, nil
}

// resetCompletion nettoie l'état de complétion
func (a *App) resetCompletion() {
	a.completionSuggestions = nil
	a.completionIndex = 0
	a.completionInput = ""
}
