package auth

import (
	"log"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	gitlabpkg "github.com/anthnel/devdesk/internal/gitlab"
)

// GitLabAuthSuccessMsg est le message d'authentification réussie (venant de app.go)
type GitLabAuthSuccessMsg struct {
	Client *gitlabclient.Client
	User   *gitlabclient.User
}

// Update gère les mises à jour
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case AuthStartMsg:
		m.authenticating = true
		return m, m.spinner.Tick

	case AuthResultMsg:
		return m.handleAuthResult(msg)

	case spinner.TickMsg:
		if m.authenticating {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case CredentialsLoadedMsg:
		return m.handleCredentialsLoaded(msg)

	case GitLabAuthSuccessMsg:
		return m.handleGitLabAuthSuccess(msg)

	case LogoutCompleteMsg:
		return m.handleLogoutComplete()
	}

	return m, nil
}

// handleKeyMsg traite les entrées clavier
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.authenticating {
		return m, nil
	}

	switch msg.String() {
	case "down":
		m.nextField()
		return m, nil

	case "up":
		m.prevField()
		return m, nil

	case "enter":
		return m.handleEnterKey()

	case " ":
		return m.handleSpaceKey()

	case "ctrl+s":
		m.saveOption = SaveToHelper
		return m, nil

	case "ctrl+f":
		m.saveOption = SaveToConfig
		return m, nil

	default:
		return m.handleInputUpdate(msg)
	}
}

// handleEnterKey traite la touche Enter selon le champ actif
func (m *Model) handleEnterKey() (tea.Model, tea.Cmd) {
	if m.authenticated {
		return m, m.logout()
	}
	if m.currentField < 2 {
		m.nextField()
		return m, nil
	}
	if m.currentField == 2 {
		m.saveOption = SaveToHelper
		return m, nil
	}
	if m.currentField == 3 {
		m.saveOption = SaveToConfig
		return m, nil
	}
	m.authenticating = true
	return m, tea.Batch(m.spinner.Tick, m.authenticate())
}

// handleSpaceKey traite la touche espace pour les radio buttons
func (m *Model) handleSpaceKey() (tea.Model, tea.Cmd) {
	if m.currentField == 2 {
		m.saveOption = SaveToHelper
		return m, nil
	}
	if m.currentField == 3 {
		m.saveOption = SaveToConfig
		return m, nil
	}
	return m, nil
}

// handleInputUpdate transmet les messages aux inputs actifs
func (m *Model) handleInputUpdate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.currentField {
	case 0:
		m.urlInput, cmd = m.urlInput.Update(msg)
	case 1:
		m.tokenInput, cmd = m.tokenInput.Update(msg)
	}
	return m, cmd
}

// handleAuthResult traite le résultat d'authentification
func (m *Model) handleAuthResult(msg AuthResultMsg) (tea.Model, tea.Cmd) {
	log.Printf("AUTH: AuthResultMsg received (Error: %v, User: %v)", msg.Error, msg.User != nil)
	m.authenticating = false
	if msg.Error != nil {
		log.Printf("AUTH: Authentication failed: %v", msg.Error)
		m.error = msg.Error.Error()
		m.success = ""
		m.warning = ""
		return m, nil
	}

	log.Printf("AUTH: Authentication successful for user: %s", msg.User.Username)
	m.error = ""
	m.authenticated = true
	m.user = msg.User
	m.success = "✓ Authenticated as " + msg.User.Username
	m.warning = msg.SaveWarning
	m.currentField = 0
	return m, nil
}

// handleCredentialsLoaded traite les credentials chargés depuis le storage
func (m *Model) handleCredentialsLoaded(msg CredentialsLoadedMsg) (tea.Model, tea.Cmd) {
	log.Printf("AUTH: CredentialsLoadedMsg received (URL: %s, Token: %v, Source: %s)",
		msg.URL, msg.Token != "", msg.Source)
	if msg.URL != "" && msg.Token != "" {
		m.urlInput.SetValue(msg.URL)
		m.tokenInput.SetValue(msg.Token)
		log.Printf("AUTH: Starting auto-login")
		m.authenticating = true
		return m, tea.Batch(m.spinner.Tick, m.authenticate())
	}
	log.Printf("AUTH: No credentials to auto-login")
	return m, nil
}

// handleGitLabAuthSuccess traite l'authentification réussie venant de app.go
func (m *Model) handleGitLabAuthSuccess(msg GitLabAuthSuccessMsg) (tea.Model, tea.Cmd) {
	m.authenticating = false
	m.authenticated = true
	m.user = msg.User
	m.error = ""
	m.success = "✓ Already authenticated as " + msg.User.Username
	m.currentField = 0
	return m, nil
}

// handleLogoutComplete réinitialise l'état après logout
func (m *Model) handleLogoutComplete() (tea.Model, tea.Cmd) {
	m.authenticated = false
	m.user = nil
	m.currentField = 0
	m.tokenInput.SetValue("")
	m.config.GitLab.Token = ""
	m.error = ""
	m.warning = ""
	m.success = "✓ Logged out successfully"
	m.urlInput.Focus()
	return m, nil
}

// nextField passe au champ suivant
func (m *Model) nextField() {
	m.currentField++
	if m.currentField > 4 {
		m.currentField = 4
	}

	m.updateFocus()
}

// prevField passe au champ précédent
func (m *Model) prevField() {
	m.currentField--
	if m.currentField < 0 {
		m.currentField = 0
	}

	m.updateFocus()
}

// updateFocus met à jour le focus des champs
func (m *Model) updateFocus() {
	switch m.currentField {
	case 0:
		m.urlInput.Focus()
		m.tokenInput.Blur()
	case 1:
		m.urlInput.Blur()
		m.tokenInput.Focus()
	default:
		// Champs 2, 3, 4 (checkboxes et button) - pas de textinput
		m.urlInput.Blur()
		m.tokenInput.Blur()
	}
}

// authenticate lance l'authentification
func (m *Model) authenticate() tea.Cmd {
	url := m.urlInput.Value()
	token := m.tokenInput.Value()

	if url == "" || token == "" {
		m.error = "URL and token are required"
		return nil
	}

	// Copier les données nécessaires AVANT la goroutine (Rule 110)
	saveToHelper := m.saveOption == SaveToHelper
	saveToConfig := m.saveOption == SaveToConfig
	config := m.config

	// Commande asynchrone
	return func() tea.Msg {
		// Créer l'auth
		auth := gitlabpkg.NewAuth(m.storage)

		// Authentifier avec saveToHelper
		result, err := auth.Authenticate(url, token, saveToHelper)
		if err != nil {
			return AuthResultMsg{
				Client: nil,
				User:   nil,
				Error:  err,
			}
		}

		// Préparer la config à sauvegarder (copie, pas modification du modèle!)
		config.GitLab.URL = url
		if saveToConfig {
			config.GitLab.Token = token
		}

		// Retourner le message avec les données
		return AuthResultMsg{
			Client:       result.Client,
			User:         result.User,
			Error:        nil,
			SaveWarning:  result.SaveWarning,
			SaveToConfig: saveToConfig,
			ConfigToSave: config,
		}
	}
}

// logout déconnecte l'utilisateur
func (m *Model) logout() tea.Cmd {
	url := m.urlInput.Value()
	storage := m.storage

	// Commande asynchrone
	return func() tea.Msg {
		// Créer l'auth
		auth := gitlabpkg.NewAuth(storage)

		// Supprimer les credentials du storage
		_ = auth.Logout(url) // Ignorer l'erreur, on déconnecte quand même

		// NE PAS modifier m.config ici (Rule 110) - le faire dans Update()
		// Retourner le message de logout complet
		return LogoutCompleteMsg{}
	}
}
