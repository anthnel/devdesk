package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/contextcmd"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forge/session"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ContextSwitchCompleteMsg reports the success of a context switch
type ContextSwitchCompleteMsg struct {
	ContextName string
	Config      *config.Config
	Created     bool // true if the switch followed a ":ctx new" creation
	Forge       forge.Forge
	GitLabUser  forge.User

	// Secrets is the store resolved for the new context, and Notices what the
	// migration off plaintext had to say. Both are carried in the message
	// rather than assigned by the Cmd that built them (Rule 110).
	Secrets credentials.Selection
	Notices []string
}

// ContextSwitchErrorMsg reports an error during the switch
type ContextSwitchErrorMsg struct {
	Name  string
	Op    contextOp
	Error error
}

// contextOp names the operation a ContextSwitchErrorMsg reports on.
type contextOp string

const (
	opSwitch contextOp = "switch"
	opCreate contextOp = "create"
	opList   contextOp = "list"
)

// ContextListMsg holds the list of available contexts
type ContextListMsg struct {
	Contexts []string
	Current  string
}

// switchContext loads an existing context and makes it current; it never
// creates one (":ctx new" does, through createContext).
//
// It settles the current view first, for the reason switchView does: a context
// switch rebuilds every view against a different file, so an uncommitted edit
// would be dropped — and dropped against the config it was *not* meant for.
func (a *App) switchContext(contextName string) tea.Cmd {
	if contextName == a.currentContext {
		return components.PostFooter(components.LevelInfo, contextcmd.AlreadyText(contextName))
	}
	if cmd, ok := a.settleCurrentView(); !ok {
		return cmd
	}
	a.switchingTo = contextName

	return func() tea.Msg {
		log.Printf("Context switch requested: %s", contextName)
		if err := config.ValidateContextName(contextName); err != nil {
			return ContextSwitchErrorMsg{Name: contextName, Op: opSwitch, Error: err}
		}
		return loadContext(contextName, opSwitch, false)
	}
}

// createContext creates a context with the default configuration, then loads
// it and makes it current.
func (a *App) createContext(contextName string) tea.Cmd {
	if cmd, ok := a.settleCurrentView(); !ok {
		return cmd
	}
	a.switchingTo = contextName

	return func() tea.Msg {
		log.Printf("Context creation requested: %s", contextName)
		if err := config.CreateContext(contextName); err != nil {
			return ContextSwitchErrorMsg{Name: contextName, Op: opCreate, Error: err}
		}
		return loadContext(contextName, opCreate, true)
	}
}

// loadContext is the path both operations share: it loads the context's
// configuration, persists it as current, resolves its secret store and tries
// its auto-login. It returns the message describing the outcome.
func loadContext(contextName string, op contextOp, created bool) tea.Msg {
	cfg, err := config.LoadContext(contextName)
	if err != nil {
		return ContextSwitchErrorMsg{Name: contextName, Op: op, Error: err}
	}
	if err := config.SetCurrentContext(contextName); err != nil {
		return ContextSwitchErrorMsg{Name: contextName, Op: op, Error: err}
	}

	// Each context gets its own store: the backend preference is per
	// context because the configuration file is, and the secrets are keyed
	// by context so two of them pointing at the same host stay separate.
	secrets := credentials.Select(contextName, cfg.App.SecretBackend)
	notices := credentials.MigrateLegacySecrets(secrets.Storage, contextName)

	backend, user := autoLoginForContext(contextName, cfg, secrets.Storage)

	log.Printf("Context switch successful: %s (created: %v, secrets: %s)", contextName, created, secrets.Backend)
	return ContextSwitchCompleteMsg{
		ContextName: contextName,
		Config:      cfg,
		Created:     created,
		Forge:       backend,
		GitLabUser:  user,
		Secrets:     secrets,
		Notices:     notices,
	}
}

// handleContextSwitchError reports a failed switch or creation. The previous
// views stay on screen; the cause goes to the log, the footer gets a sentence.
func (a *App) handleContextSwitchError(msg ContextSwitchErrorMsg) (tea.Model, tea.Cmd) {
	if msg.Op == opList {
		log.Printf("ERROR [app/context] %s: %v", msg.Op, msg.Error)
	} else {
		log.Printf("ERROR [app/context] %s %q: %v", msg.Op, msg.Name, msg.Error)
	}
	a.switchingTo = ""
	level, text := contextErrorFooter(msg)
	return a, components.PostFooter(level, text)
}

// contextErrorFooter maps the error of a context operation to a footer message.
func contextErrorFooter(msg ContextSwitchErrorMsg) (components.Level, string) {
	switch {
	case msg.Op == opList:
		return components.LevelError, "Failed to list contexts — check logs"
	case errors.Is(msg.Error, config.ErrReservedContextName):
		return components.LevelError, contextcmd.ReservedText(msg.Name)
	case errors.Is(msg.Error, config.ErrContextExists):
		return components.LevelWarning, contextcmd.ExistsText(msg.Name)
	case !contextcmd.ValidName(msg.Name):
		return components.LevelError, contextcmd.InvalidText(msg.Name)
	case errors.Is(msg.Error, config.ErrContextNotFound):
		return components.LevelError, contextcmd.NotFoundText(msg.Name)
	}
	if msg.Op == opCreate {
		return components.LevelError, fmt.Sprintf("Failed to create context %q — check logs", msg.Name)
	}
	return components.LevelError, fmt.Sprintf("Failed to switch to %q — check logs", msg.Name)
}

// autoLoginForContext tries the new context's own credentials. A failure is not
// an error: the user is sent to the auth view instead.
func autoLoginForContext(contextName string, cfg *config.Config, storage credentials.Storage) (forge.Forge, forge.User) {
	if cfg.Forge.URL == "" {
		return nil, forge.User{}
	}

	auth := session.NewAuth(storage)

	token, err := auth.LoadCredentials(cfg.Forge.URL)
	if err != nil || token == "" {
		return nil, forge.User{}
	}

	result, err := auth.AuthenticateOnly(context.Background(), cfg.Forge.Type, cfg.Forge.URL, token)
	if err != nil {
		log.Printf("Auto-login failed for context '%s': %v", contextName, err)
		return nil, forge.User{}
	}
	log.Printf("Auto-login successful for context '%s': %s", contextName, result.User.Username)
	return result.Forge, result.User
}

// listContexts retrieves available contexts
func (a *App) listContexts() tea.Cmd {
	return func() tea.Msg {
		log.Printf("Listing available contexts")
		contexts, err := config.ListContexts()
		if err != nil {
			log.Printf("ERROR: Failed to list contexts: %v", err)
			return ContextSwitchErrorMsg{Op: opList, Error: err}
		}

		current, _ := config.GetCurrentContext()
		if current == "" {
			current = "default"
		}

		log.Printf("Found %d contexts, current: %s", len(contexts), current)
		return ContextListMsg{Contexts: contexts, Current: current}
	}
}

// handleContextSwitchComplete processes successful context switches
func (a *App) handleContextSwitchComplete(msg ContextSwitchCompleteMsg) (tea.Model, tea.Cmd) {
	a.switchingTo = ""
	if msg.ContextName != a.currentContext {
		a.previousContext = a.currentContext
	}
	a.config = msg.Config
	a.currentContext = msg.ContextName
	theme.SetGaugeGlyph(msg.Config.App.GaugeGlyph)
	a.sharedState.Secrets = msg.Secrets
	a.sharedState.SecretNotices = msg.Notices

	// Reset GitLab auth state — the new context has its own credentials
	a.clearAuthenticated()

	// Apply auto-login result from the context switch if successful
	var indexCmd tea.Cmd
	if msg.Forge != nil {
		indexCmd = a.setAuthenticated(msg.Forge, msg.GitLabUser)
	} else {
		// No credentials available — navigate to auth view for manual login
		a.currentView = command.ViewGitAuth
	}

	// The new context's tools may run from elsewhere: its views start from
	// "not known yet" rather than from the previous context's answer.
	a.forgetScanTools()

	// Reinitialize views with new config; auth state is already populated above
	initCmd := a.reinitializeViews()

	// The rebuilt views know nothing of the work that is still running, and it
	// did not stop: a run is stamped with the context it was started in and
	// kept for the session (D8), so a switch changes which runs are relevant,
	// never whether they exist. Telling the new views is the whole of it.
	var created tea.Cmd
	if msg.Created {
		created = components.PostFooter(components.LevelInfo, fmt.Sprintf("Context %q created", msg.ContextName))
	}
	return a, tea.Batch(a.requestResize(), initCmd, a.jobsChanged(), a.restartMCPCmd(), a.syncProxyPortCmd(),
		a.detectScanTools(), indexCmd, loadContextNamesCmd(), created)
}

// handleContextList opens the picker with the cursor on the context in use, so
// enter is a no-op rather than a switch to whatever sorts first.
func (a *App) handleContextList(msg ContextListMsg) (tea.Model, tea.Cmd) {
	a.showContextList = true
	a.contextList = msg.Contexts
	a.contextSelectedIdx = indexOf(msg.Contexts, msg.Current)
	return a, nil
}

// handleContextListKeyMsg handles key presses in the context selection overlay
func (a *App) handleContextListKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.showContextList = false
	case "up":
		a.contextSelectedIdx = max(a.contextSelectedIdx-1, 0)
	case "down":
		a.contextSelectedIdx = min(a.contextSelectedIdx+1, len(a.contextList)-1)
	case "enter":
		if len(a.contextList) > 0 {
			a.showContextList = false
			return a, a.switchContext(a.contextList[a.contextSelectedIdx])
		}
	}
	return a, nil
}

// renderContextListOverlay renders an overlay with the list of contexts
func (a *App) renderContextListOverlay() string {
	return renderPickerOverlay("Select Context", a.contextList, a.currentContext, a.contextSelectedIdx)
}

// renderContextSwitchingOverlay stands in for the interface while a switch
// loads, so the first views drawn after it are the new context's.
func (a *App) renderContextSwitchingOverlay() string {
	return theme.OverlayBoxStyle().Render(theme.Bg("Switching to context " + a.switchingTo + "..."))
}

// renderPickerOverlay draws the list overlays: a title, one entry per line, the
// entry in use marked and the highlighted one carrying the focus indicator.
func renderPickerOverlay(title string, entries []string, current string, selectedIdx int) string {
	var b strings.Builder

	b.WriteString(theme.TitleStyle.Render(title) + "\n\n")

	for i, entry := range entries {
		label := entry
		if entry == current {
			label += " (current)"
		}
		if i == selectedIdx {
			b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall + " " + label))
		} else {
			b.WriteString(theme.Bg("  " + label))
		}
		b.WriteString("\n")
	}

	return theme.OverlayBoxStyle().Render(b.String())
}

// indexOf returns the position of want, or 0 when it is absent — the cursor has
// to land on a row that exists.
func indexOf(entries []string, want string) int {
	for i, entry := range entries {
		if entry == want {
			return i
		}
	}
	return 0
}
