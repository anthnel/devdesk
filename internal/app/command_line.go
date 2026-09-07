package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/about"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/containers"
	"github.com/anthnel/devdesk/internal/ui/dashboard"
	"github.com/anthnel/devdesk/internal/ui/forge/auth"
	"github.com/anthnel/devdesk/internal/ui/forge/explorer"
	"github.com/anthnel/devdesk/internal/ui/jobsview"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/status"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// handleCommandMode handles command mode
func (a *App) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return a, a.closeCommandLine()

	case tea.KeyTab:
		// Cycle through the suggestions
		return a.handleCompletionCycle()

	case tea.KeyEnter:
		return a.runCommand()

	default:
		// Update the input
		var cmd tea.Cmd
		a.commandInput, cmd = a.commandInput.Update(msg)
		// Update the suggestions after each keystroke
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
			// Create/switch to a named context
			return a, a.switchContext(cmd.Args[0])
		}
		// No args -> open the interactive modal
		return a, a.listContexts()

	case command.CommandUnknown:
		// Invalid command - stay in command mode
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

// switchView changes the current view
func (a *App) switchView(view command.ViewType) tea.Cmd {
	if view != a.currentView {
		if cmd, ok := a.settleCurrentView(); !ok {
			return cmd
		}
	}

	// Lazy loading of views
	if _, exists := a.views[view]; !exists {
		a.createView(view)
	}

	a.currentView = view

	newView, ok := a.views[view]
	if !ok {
		return nil
	}
	log.Printf("Switching to view: %s, calling Init()", view)

	// Init() + a WindowSizeMsg to force the resize, plus the snapshot of
	// work in progress: the view was kept up to date for as long as it
	// existed, but a view built just now was not there for anything that had
	// already started.
	return tea.Batch(newView.Init(), a.requestResize(), a.sendJobsTo(view))
}

// settleCurrentView gives the view being left the chance to write down what it
// holds, and lets it refuse to be left.
//
// A view that does not implement LeavingView is left without ceremony, which is
// every view but one: the configuration form is the only screen that keeps a
// value in a widget rather than in the model until a key moves the cursor.
//
// Returning false keeps the current view *and* whatever it wants to say about
// why — the caller passes that Cmd on rather than the switch it did not make.
func (a *App) settleCurrentView() (tea.Cmd, bool) {
	current, exists := a.views[a.currentView]
	if !exists {
		return nil, true
	}
	leaver, implements := current.(LeavingView)
	if !implements {
		return nil, true
	}
	settled, cmd, ok := leaver.Leave()
	a.views[a.currentView] = settled
	return cmd, ok
}

// createView creates a view (lazy loading)
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
		a.views[view] = configuration.New(a.config, a.mcpFacts())
	case command.ViewJobs:
		a.views[view] = jobsview.New(a.config, a.currentContext)
	case command.ViewAbout:
		a.views[view] = about.New(a.config)
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

// updateCompletions updates the suggestions based on the current input
func (a *App) updateCompletions() {
	input := a.commandInput.Value()

	// Reset index if input changes
	if input != a.completionInput {
		a.completionIndex = 0
		a.completionInput = input
	}

	a.completionSuggestions = a.completionEngine.GetSuggestions(input)
}

// handleCompletionCycle cycles through the suggestions (repeated Tab)
func (a *App) handleCompletionCycle() (tea.Model, tea.Cmd) {
	if len(a.completionSuggestions) == 0 {
		return a, nil
	}

	// Increment with wrap-around
	a.completionIndex = (a.completionIndex + 1) % len(a.completionSuggestions)
	return a, nil
}

// resetCompletion clears the completion state
func (a *App) resetCompletion() {
	a.completionSuggestions = nil
	a.completionIndex = 0
	a.completionInput = ""
}
