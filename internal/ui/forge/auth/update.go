package auth

import (
	"context"
	"log"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forge/session"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// ForgeAuthSuccessMsg est le message d'authentification réussie (venant de app.go)
type ForgeAuthSuccessMsg struct {
	Forge forge.Forge
	User  forge.User
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

	case ForgeAuthSuccessMsg:
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

	default:
		return m.handleInputUpdate(msg)
	}
}

// handleEnterKey traite la touche Enter selon le champ actif
func (m *Model) handleEnterKey() (tea.Model, tea.Cmd) {
	if m.authenticated {
		return m, m.logout()
	}
	if m.currentField < fieldSubmit {
		m.nextField()
		return m, nil
	}
	m.authenticating = true
	return m, tea.Batch(m.spinner.Tick, m.authenticate())
}

// handleInputUpdate transmet les messages aux inputs actifs
func (m *Model) handleInputUpdate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.currentField == fieldToken {
		m.tokenInput, cmd = m.tokenInput.Update(msg)
	}
	return m, cmd
}

// handleAuthResult traite le résultat d'authentification
func (m *Model) handleAuthResult(msg AuthResultMsg) (tea.Model, tea.Cmd) {
	log.Printf("AUTH: AuthResultMsg received (Error: %v, User: %q)", msg.Error, msg.User.Username)
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
	m.currentField = fieldToken
	return m, nil
}

// handleCredentialsLoaded traite les credentials chargés depuis le storage
func (m *Model) handleCredentialsLoaded(msg CredentialsLoadedMsg) (tea.Model, tea.Cmd) {
	log.Printf("AUTH: CredentialsLoadedMsg received (URL: %s, Token: %v)", msg.URL, msg.Token != "")
	if msg.URL != "" && msg.Token != "" {
		m.tokenInput.SetValue(msg.Token)
		log.Printf("AUTH: Starting auto-login")
		m.authenticating = true
		return m, tea.Batch(m.spinner.Tick, m.authenticate())
	}
	log.Printf("AUTH: No credentials to auto-login")
	return m, nil
}

// handleGitLabAuthSuccess traite l'authentification réussie venant de app.go
func (m *Model) handleGitLabAuthSuccess(msg ForgeAuthSuccessMsg) (tea.Model, tea.Cmd) {
	m.authenticating = false
	m.authenticated = true
	m.user = msg.User
	m.error = ""
	m.success = "✓ Already authenticated as " + msg.User.Username
	m.currentField = fieldToken
	return m, nil
}

// handleLogoutComplete réinitialise l'état après logout
func (m *Model) handleLogoutComplete() (tea.Model, tea.Cmd) {
	m.authenticated = false
	m.user = forge.User{}
	m.currentField = fieldToken
	m.tokenInput.SetValue("")
	m.error = ""
	m.warning = ""
	m.success = "✓ Logged out successfully"
	m.tokenInput.Focus()
	return m, nil
}

// nextField passe au champ suivant
func (m *Model) nextField() {
	m.currentField++
	if m.currentField > lastField {
		m.currentField = lastField
	}

	m.updateFocus()
}

// prevField passe au champ précédent
func (m *Model) prevField() {
	m.currentField--
	if m.currentField < fieldToken {
		m.currentField = fieldToken
	}

	m.updateFocus()
}

// updateFocus met à jour le focus des champs
func (m *Model) updateFocus() {
	if m.currentField == fieldToken {
		m.tokenInput.Focus()
		return
	}
	m.tokenInput.Blur() // fieldSubmit is a button, not an input
}

// authenticate lance l'authentification
func (m *Model) authenticate() tea.Cmd {
	url := m.config.Forge.URL
	token := m.tokenInput.Value()

	if url == "" {
		m.error = "No " + m.vocab().Name + " URL configured — set it in :config, " + m.forgeTab() + " tab"
		return nil
	}
	if token == "" {
		m.error = "Token is required"
		return nil
	}

	// Copier les données nécessaires AVANT la goroutine (Rule 110)
	storage := m.secrets.Storage
	config := m.config
	forgeType := m.config.Forge.Type

	// Commande asynchrone
	return func() tea.Msg {
		// Créer l'auth
		auth := session.NewAuth(storage)

		// Il n'y a plus de choix : le token part vers le store, toujours.
		result, err := auth.Authenticate(context.Background(), forgeType, url, token)
		if err != nil {
			return AuthResultMsg{Error: err}
		}

		// Nothing to save: the URL came from the configuration and the token is
		// already in the store. ConfigToSave is kept so the router's handler is
		// unchanged, but it carries no edit of this view's making.
		return AuthResultMsg{
			Forge:        result.Forge,
			User:         result.User,
			Error:        nil,
			SaveWarning:  result.SaveWarning,
			ConfigToSave: config,
		}
	}
}

// logout déconnecte l'utilisateur
func (m *Model) logout() tea.Cmd {
	url := m.config.Forge.URL
	storage := m.secrets.Storage

	// Commande asynchrone
	return func() tea.Msg {
		// Créer l'auth
		auth := session.NewAuth(storage)

		// Supprimer les credentials du storage
		_ = auth.Logout(url) // Ignorer l'erreur, on déconnecte quand même

		// NE PAS modifier m.config ici (Rule 110) - le faire dans Update()
		// Retourner le message de logout complet
		return LogoutCompleteMsg{}
	}
}
