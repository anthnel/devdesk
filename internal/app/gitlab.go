package app

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/gitlab"
	"github.com/anthnel/devdesk/internal/ui/gitlab/auth"
)

// GitLabAutoLoginMsg signale le résultat de l'auto-login
type GitLabAutoLoginMsg struct {
	Client *gitlabclient.Client
	User   *gitlabclient.User
	Error  error
}

// tryAutoLogin tente de se connecter automatiquement à GitLab avec les credentials sauvegardés
func (a *App) tryAutoLogin() tea.Cmd {
	url := a.config.GitLab.URL
	storage := a.sharedState.Secrets.Storage

	// Si pas d'URL configurée, pas d'auto-login
	if url == "" {
		return nil
	}

	return func() tea.Msg {
		gitlabAuth := gitlab.NewAuth(storage)

		// Le token vient du store et de nulle part ailleurs : il n'est plus
		// écrit en clair dans la configuration (§3.9).
		token, err := gitlabAuth.LoadCredentials(url)
		if err != nil || token == "" {
			return GitLabAutoLoginMsg{}
		}

		result, err := gitlabAuth.AuthenticateOnly(url, token)
		if err != nil {
			log.Printf("Auto-login failed: %v", err)
			return GitLabAutoLoginMsg{Error: err}
		}

		log.Printf("Auto-login successful: %s", result.User.Username)
		return GitLabAutoLoginMsg{Client: result.Client, User: result.User}
	}
}

// handleAutoLoginResult applies a successful auto-login. A failure is ignored:
// the user can still authenticate by hand from the auth view.
func (a *App) handleAutoLoginResult(msg GitLabAutoLoginMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil || msg.Client == nil {
		return a, nil
	}

	log.Printf("GitLab auto-login successful for user: %s", msg.User.Username)
	a.setAuthenticated(msg.Client, msg.User)
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
	a.setAuthenticated(msg.Client, msg.User)

	if msg.ConfigToSave != nil {
		if err := config.Save(msg.ConfigToSave); err != nil {
			log.Printf("ERROR: Failed to save config after authentication: %v", err)
		} else {
			log.Printf("Config saved successfully (URL: %s)", msg.ConfigToSave.GitLab.URL)
		}
	}

	// Transmettre le message à la vue pour mise à jour de l'UI
	return a, a.forwardToActiveView(msg)
}

// setAuthenticated records a live GitLab session in the shared state, which is
// what every GitLab-backed view reads to decide whether it can load anything.
func (a *App) setAuthenticated(client *gitlabclient.Client, user *gitlabclient.User) {
	a.sharedState.GitLabClient = client
	a.sharedState.CurrentUser = user
	a.sharedState.IsAuthenticated = true
}
