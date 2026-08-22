package app

import (
	"context"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
	"github.com/anthnel/devdesk/internal/ui/gitlab/auth"
)

// GitLabAutoLoginMsg signale le résultat de l'auto-login
type GitLabAutoLoginMsg struct {
	Forge forge.Forge
	User  forge.User
	Error error
}

// tryAutoLogin tente de se connecter automatiquement à GitLab avec les credentials sauvegardés
func (a *App) tryAutoLogin() tea.Cmd {
	url := a.config.Forge.URL
	storage := a.sharedState.Secrets.Storage

	// Si pas d'URL configurée, pas d'auto-login
	if url == "" {
		return nil
	}

	return func() tea.Msg {
		auth := gitlabforge.NewAuth(storage)

		// Le token vient du store et de nulle part ailleurs : il n'est plus
		// écrit en clair dans la configuration (§3.9).
		token, err := auth.LoadCredentials(url)
		if err != nil || token == "" {
			return GitLabAutoLoginMsg{}
		}

		result, err := auth.AuthenticateOnly(context.Background(), url, token)
		if err != nil {
			log.Printf("Auto-login failed: %v", err)
			return GitLabAutoLoginMsg{Error: err}
		}

		log.Printf("Auto-login successful: %s", result.User.Username)
		return GitLabAutoLoginMsg{Forge: result.Forge, User: result.User}
	}
}

// handleAutoLoginResult applies a successful auto-login. A failure is ignored:
// the user can still authenticate by hand from the auth view.
func (a *App) handleAutoLoginResult(msg GitLabAutoLoginMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil || msg.Forge == nil {
		return a, nil
	}

	log.Printf("GitLab auto-login successful for user: %s", msg.User.Username)
	a.setAuthenticated(msg.Forge, msg.User)
	return a, nil
}

// handleAuthResult intercepts a manual authentication to persist the config and
// populate the shared state before the view sees it.
func (a *App) handleAuthResult(msg auth.AuthResultMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		// L'erreur est déjà gérée par la vue, juste transmettre le message
		return a, a.forwardToActiveView(msg)
	}

	log.Printf("GitLab authentication successful for user: %s", msg.User.Username)
	a.setAuthenticated(msg.Forge, msg.User)

	if msg.ConfigToSave != nil {
		if err := config.Save(msg.ConfigToSave); err != nil {
			log.Printf("ERROR: Failed to save config after authentication: %v", err)
		} else {
			log.Printf("Config saved successfully (URL: %s)", msg.ConfigToSave.Forge.URL)
		}
	}

	// Transmettre le message à la vue pour mise à jour de l'UI
	return a, a.forwardToActiveView(msg)
}

// setAuthenticated records a live GitLab session in the shared state, which is
// what every GitLab-backed view reads to decide whether it can load anything.
func (a *App) setAuthenticated(backend forge.Forge, user forge.User) {
	a.sharedState.Forge = backend
	a.sharedState.CurrentUser = user
	a.sharedState.IsAuthenticated = true
}

// clearAuthenticated is setAuthenticated's mirror, and the cached data goes with
// the session: groups, projects and the dashboard counters were all read
// through the client that just stopped being valid.
func (a *App) clearAuthenticated() {
	a.sharedState.Forge = nil
	a.sharedState.CurrentUser = forge.User{}
	a.sharedState.IsAuthenticated = false
	a.sharedState.GitLabStats = nil
}

// handleLogoutComplete clears the session the way logging in sets it.
//
// It had no router handler at all: LogoutCompleteMsg was consumed by the auth
// view, which reset its own three fields and nothing else. So sharedState kept
// the client, the user and the caches — the explorer went on browsing projects
// and the header went on naming a signed-out user, because both read state
// nobody had cleared. Logging in updated the shared state and logging out did
// not, which is the asymmetry that made it possible.
//
// Every view but the auth view is dropped: clearing sharedState does not empty
// a table the explorer already loaded. The auth view is kept because it is on
// screen and has just written "Logged out successfully".
func (a *App) handleLogoutComplete(msg auth.LogoutCompleteMsg) (tea.Model, tea.Cmd) {
	log.Printf("GitLab logout for context %s", a.currentContext)
	a.clearAuthenticated()

	authView := a.views[command.ViewGitlabAuth]
	a.views = make(map[command.ViewType]tea.Model)
	if authView != nil {
		a.views[command.ViewGitlabAuth] = authView
	}
	a.createView(a.currentView)

	return a, tea.Batch(a.forwardToActiveView(msg), a.requestResize())
}
