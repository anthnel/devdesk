package app

import (
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/about"
	"github.com/anthnel/devdesk/internal/ui/agentusageview"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/containers"
	"github.com/anthnel/devdesk/internal/ui/dashboard"
	"github.com/anthnel/devdesk/internal/ui/filebrowser"
	"github.com/anthnel/devdesk/internal/ui/forge/auth"
	"github.com/anthnel/devdesk/internal/ui/forge/explorer"
	"github.com/anthnel/devdesk/internal/ui/jobsview"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/status"
	"github.com/anthnel/devdesk/internal/ui/templates"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// handleCommandMode handles a key while the command line owns the keyboard.
// Every key first clears the warning on the list's bottom rule: it lasts until
// the next keystroke, with no timer.
func (a *App) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a.sugNotice = ""

	switch msg.Type {
	case tea.KeyEsc:
		return a, a.closeCommandLine()
	case tea.KeyTab:
		a.completeHighlighted()
		return a, nil
	case tea.KeyShiftTab:
		return a, nil
	case tea.KeyUp:
		a.moveHighlight(-1)
		return a, nil
	case tea.KeyDown:
		a.moveHighlight(1)
		return a, nil
	case tea.KeyEnter:
		return a.runCommand()
	}
	return a.handleCommandInput(msg)
}

// handleCommandInput feeds an ordinary key to the input and ranks the registry
// again for the new text.
func (a *App) handleCommandInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	before := a.commandInput.Value()
	a.commandInput, cmd = a.commandInput.Update(msg)
	if a.commandInput.Value() != before {
		a.refreshSuggestions()
	}
	return a, cmd
}

// closeCommandLine leaves command mode and asks for the re-layout that gives
// the row back to the inactive prompt. The list goes with the line.
func (a *App) closeCommandLine() tea.Cmd {
	a.commandMode = false
	a.commandInput.Blur()
	a.resetSuggestions()
	return a.requestResize()
}

// runnableText is the single availability question for enter, read by the
// header (to grey it) and by runCommand (to refuse). With a list drawn it is
// the highlighted row's name. At the argument stage, or with no list drawn
// (too short a viewport), it is the text as typed: a prefix such as "co" is
// not a name and runs nothing, but an empty line has nothing to run.
func (a *App) runnableText() (string, bool) {
	if a.suggestions.Stage != command.StageArgument && a.listVisible() > 0 {
		entry, ok := a.highlightedEntry()
		return entry.Name, ok
	}
	input := a.commandInput.Value()
	return input, strings.TrimSpace(input) != ""
}

// runCommand executes the highlighted row, or at the argument stage the text
// as typed. With nothing to run it says so on the list's bottom rule and
// leaves the line open.
func (a *App) runCommand() (tea.Model, tea.Cmd) {
	input, ok := a.runnableText()
	if !ok {
		// Only a drawn list has a rule to carry the notice; without one the
		// refusal is silent, a documented exception to Rule 130.
		if a.listVisible() > 0 {
			a.sugNotice = a.nothingToRunNotice()
		}
		return a, nil
	}

	cmd := command.ParseCommand(input)

	switch cmd.Type {
	case command.CommandQuit:
		a.resetSuggestions()
		return a, tea.Quit

	case command.CommandView:
		a.commandMode = false
		a.commandInput.Blur()
		a.resetSuggestions()
		a.resetSelectionModeFor(cmd.View)
		return a, a.switchView(cmd.View)

	case command.CommandContext:
		return a.runContext(cmd.Args)
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
//
// The security and network views are dropped too, when they are showing what
// another view sent them: workspaces and images set the security view's
// OriginView, and status's H sets netdiag's. What such a view shows is that
// view's detail, not its own state. Naming the view asks for the view — its
// inventory, or a blank form — and keeping the detail answered with the same
// thing again, with esc leading back to where it came from.
//
// A view reached by name has no origin and is kept, like every other view's
// state across a switch.
func (a *App) resetSelectionModeFor(view command.ViewType) {
	switch view {
	case command.ViewWorkspaces:
		delete(a.views, view)
	case command.ViewFiles:
		// A picker left behind — the user typed a command instead of choosing —
		// answers a borrower; the name asks for the browser.
		if held, ok := a.views[view].(filebrowser.Model); ok && held.Picking() {
			delete(a.views, view)
		}
	case command.ViewSecurity:
		if held, ok := a.views[view].(security.Model); ok && held.OriginView != "" {
			delete(a.views, view)
		}
	case command.ViewNetdiag:
		// The same rule, with one precaution the security view does not need: a
		// diagnostic sent from status may still be walking its stages, and
		// dropping the view would discard that run without a word. A run in
		// flight keeps its view; esc on it still returns to status.
		if held, ok := a.views[view].(*netdiag.Model); ok && held.OriginView != "" && !held.Running() {
			delete(a.views, view)
		}
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
// every view but two: the configuration form keeps a value in a widget rather
// than in the model until a key moves the cursor, and the agent usage view has a
// live poll to stop.
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
	case command.ViewAgentUsage:
		a.views[view] = agentusageview.New(a.config)
	case command.ViewConfiguration:
		a.views[view] = configuration.New(a.config, a.mcpFacts())
	case command.ViewJobs:
		a.views[view] = jobsview.New(a.config, a.currentContext)
	case command.ViewTemplates:
		a.views[view] = templates.New(a.config, a.sharedState.Secrets.Storage)
	case command.ViewAbout:
		a.views[view] = about.New(a.config)
	case command.ViewFiles:
		a.views[view] = filebrowser.New(a.config)
	case command.ViewViewer:
		// Empty: the viewer is normally installed with its source by
		// handleViewerOpenRequest, and this case exists so the router can build
		// one like any other view — which is what the contract test does.
		a.views[view] = uiviewer.New(a.config)
	}
	// Built after the detection landed, it was not there to receive it.
	a.sendScanToolsTo(view)
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
