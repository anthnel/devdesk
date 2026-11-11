package auth

import (
	"context"
	"log"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forge/session"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// ForgeAuthSuccessMsg is the successful authentication message (coming from app.go)
type ForgeAuthSuccessMsg struct {
	Forge forge.Forge
	User  forge.User
}

// Update handles updates
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyPressMsg:
		return m.handleKeyMsg(msg)

	case tea.PasteMsg:
		return m.handlePaste(msg)

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

	m.footer.Handle(msg)
	return m, nil
}

// handleKeyMsg handles keyboard input
func (m *Model) handleKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

// handlePaste gives pasted text to the token field while it has the keyboard.
// It is dropped during a login, and never read as keys (Rule 111).
func (m *Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.authenticating {
		return m, nil
	}
	return m.handleInputUpdate(msg)
}

// handleEnterKey handles the Enter key based on the active field
func (m *Model) handleEnterKey() (tea.Model, tea.Cmd) {
	if m.authenticated {
		return m, m.logout()
	}
	if m.currentField < fieldSubmit {
		m.nextField()
		return m, nil
	}
	// Validate before the spinner: a refused submit starts nothing.
	if !m.validation.Submit(m.tokenErrors()) {
		m.restyleToken()
		return m, nil
	}
	m.restyleToken()
	if m.config.Forge.URL == "" {
		m.error = m.missingURLMessage()
		return m, nil
	}
	m.authenticating = true
	return m, tea.Batch(m.spinner.Tick, m.authenticate())
}

// tokenErrors lists the field errors of the form: the token is required once
// trimmed. The URL is configuration, not a field of this form.
func (m *Model) tokenErrors() []components.FieldError {
	if strings.TrimSpace(m.tokenInput.Value()) == "" {
		return []components.FieldError{{Key: keyToken, Text: "Token is required"}}
	}
	return nil
}

// restyleToken applies the invalid mark to the token field. It runs from
// Update() only, never from a Cmd or View() (Rule 110).
func (m *Model) restyleToken() {
	m.marks.Sync(keyToken, &m.tokenInput, m.validation.Invalid(keyToken))
}

// missingURLMessage is the body error for an unconfigured forge URL.
func (m *Model) missingURLMessage() string {
	return "No " + m.vocab().Name + " URL configured — set it in :config, " + m.forgeTab() + " tab"
}

// handleInputUpdate forwards messages to the active inputs
func (m *Model) handleInputUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.currentField == fieldToken {
		m.tokenInput, cmd = m.tokenInput.Update(msg)
		m.validation.Revalidate(m.tokenErrors())
		m.restyleToken()
	}
	return m, cmd
}

// handleAuthResult handles the authentication result
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

// handleCredentialsLoaded handles the credentials loaded from storage
func (m *Model) handleCredentialsLoaded(msg CredentialsLoadedMsg) (tea.Model, tea.Cmd) {
	log.Printf("AUTH: CredentialsLoadedMsg received (URL: %s, Token: %v)", msg.URL, msg.Token != "")
	if msg.URL != "" && strings.TrimSpace(msg.Token) != "" {
		m.tokenInput.SetValue(msg.Token)
		log.Printf("AUTH: Starting auto-login")
		m.authenticating = true
		return m, tea.Batch(m.spinner.Tick, m.authenticate())
	}
	log.Printf("AUTH: No credentials to auto-login")
	return m, nil
}

// handleGitLabAuthSuccess handles the successful authentication coming from app.go
func (m *Model) handleGitLabAuthSuccess(msg ForgeAuthSuccessMsg) (tea.Model, tea.Cmd) {
	m.authenticating = false
	m.authenticated = true
	m.user = msg.User
	m.error = ""
	m.success = "✓ Already authenticated as " + msg.User.Username
	m.currentField = fieldToken
	return m, nil
}

// handleLogoutComplete resets the state after logout
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

// nextField moves to the next field
func (m *Model) nextField() {
	m.currentField++
	if m.currentField > lastField {
		m.currentField = lastField
	}

	m.updateFocus()
}

// prevField moves to the previous field
func (m *Model) prevField() {
	m.currentField--
	if m.currentField < fieldToken {
		m.currentField = fieldToken
	}

	m.updateFocus()
}

// updateFocus updates the fields' focus
func (m *Model) updateFocus() {
	if m.currentField == fieldToken {
		m.tokenInput.Focus()
		return
	}
	m.tokenInput.Blur() // fieldSubmit is a button, not an input
}

// authenticate starts authentication
func (m *Model) authenticate() tea.Cmd {
	url := m.config.Forge.URL
	token := strings.TrimSpace(m.tokenInput.Value())

	// Defence in depth: the auto-login path comes here without handleEnterKey,
	// and a stored token of blanks must not start an anonymous call.
	if token == "" {
		return nil
	}
	if url == "" {
		m.error = m.missingURLMessage()
		return nil
	}

	// Copy the necessary data BEFORE the goroutine (Rule 110)
	storage := m.secrets.Storage
	config := m.config
	forgeType := m.config.Forge.Type

	// Asynchronous command
	return func() tea.Msg {
		// Create the auth
		auth := session.NewAuth(storage)

		// There is no longer a choice: the token always goes to the store.
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

// logout signs the user out
func (m *Model) logout() tea.Cmd {
	url := m.config.Forge.URL
	storage := m.secrets.Storage

	// Asynchronous command
	return func() tea.Msg {
		// Create the auth
		auth := session.NewAuth(storage)

		// Remove the credentials from storage
		_ = auth.Logout(url) // Ignore the error, we log out anyway

		// Do NOT modify m.config here (Rule 110) - do it in Update()
		// Return the logout-complete message
		return LogoutCompleteMsg{}
	}
}
