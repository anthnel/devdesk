package app

import (
	"context"
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forge/session"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ContextSwitchCompleteMsg signale le succès du switch de contexte
type ContextSwitchCompleteMsg struct {
	ContextName string
	Config      *config.Config
	Created     bool // true si le contexte a été créé automatiquement
	Forge       forge.Forge
	GitLabUser  forge.User

	// Secrets is the store resolved for the new context, and Notices what the
	// migration off plaintext had to say. Both are carried in the message
	// rather than assigned by the Cmd that built them (Rule 110).
	Secrets credentials.Selection
	Notices []string
}

// ContextSwitchErrorMsg signale une erreur lors du switch
type ContextSwitchErrorMsg struct {
	Error error
}

// ContextListMsg contient la liste des contextes disponibles
type ContextListMsg struct {
	Contexts []string
	Current  string
}

// switchContext handles context switching with auto-creation
func (a *App) switchContext(contextName string) tea.Cmd {
	return func() tea.Msg {
		log.Printf("Context switch requested: %s", contextName)

		cfg, created, err := loadOrCreateContext(contextName)
		if err != nil {
			return ContextSwitchErrorMsg{Error: err}
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
}

// loadOrCreateContext validates the name, creates the context when it does not
// exist yet, loads its configuration and persists it as current.
func loadOrCreateContext(contextName string) (*config.Config, bool, error) {
	if err := config.ValidateContextName(contextName); err != nil {
		log.Printf("ERROR: Context validation failed: %v", err)
		return nil, false, err
	}

	exists, err := config.ContextExists(contextName)
	if err != nil {
		log.Printf("ERROR: Failed to check context existence: %v", err)
		return nil, false, err
	}

	created := false
	if !exists {
		log.Printf("Context '%s' does not exist, creating automatically", contextName)
		if err := config.CreateContext(contextName); err != nil {
			log.Printf("ERROR: Failed to create context '%s': %v", contextName, err)
			return nil, false, err
		}
		created = true
		log.Printf("Context '%s' created successfully", contextName)
	}

	cfg, err := config.LoadContext(contextName)
	if err != nil {
		log.Printf("ERROR: Failed to load context '%s': %v", contextName, err)
		return nil, created, err
	}

	if err := config.SetCurrentContext(contextName); err != nil {
		log.Printf("ERROR: Failed to persist context switch: %v", err)
		return nil, created, err
	}

	return cfg, created, nil
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
			return ContextSwitchErrorMsg{Error: err}
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
	a.config = msg.Config
	a.currentContext = msg.ContextName
	a.sharedState.Secrets = msg.Secrets
	a.sharedState.SecretNotices = msg.Notices

	// Reset GitLab auth state — the new context has its own credentials
	a.clearAuthenticated()

	// Apply auto-login result from the context switch if successful
	if msg.Forge != nil {
		a.setAuthenticated(msg.Forge, msg.GitLabUser)
	} else {
		// No credentials available — navigate to auth view for manual login
		a.currentView = command.ViewGitAuth
	}

	// Reinitialize views with new config; auth state is already populated above
	initCmd := a.reinitializeViews()

	return a, tea.Batch(a.requestResize(), initCmd)
}

// handleContextList opens the picker with the cursor on the context in use, so
// enter is a no-op rather than a switch to whatever sorts first.
func (a *App) handleContextList(msg ContextListMsg) (tea.Model, tea.Cmd) {
	a.showContextList = true
	a.contextList = msg.Contexts
	a.contextSelectedIdx = indexOf(msg.Contexts, msg.Current)
	return a, nil
}

// handleContextListKeyMsg gère les touches clavier dans l'overlay de sélection de contexte
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

// renderContextListOverlay affiche un overlay avec la liste des contextes
func (a *App) renderContextListOverlay() string {
	return renderPickerOverlay("Select Context", a.contextList, a.currentContext, a.contextSelectedIdx)
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
