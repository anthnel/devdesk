package configuration

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleBackendConfirmed()

	case sharedcomponents.ConfirmModalNoMsg:
		return m.handleBackendRejected()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeInput()
		return m, nil

	case saveFailedMsg:
		log.Printf("ERROR [configuration] save: %v", msg.err)
		m.footerError = "Failed to save — check logs"
		return m, clearFooterCmd()

	case clearFooterMsg:
		m.footerError = ""
		m.footerInfo = ""
		return m, nil

	case tea.KeyMsg:
		if m.confirmModal != nil {
			var cmd tea.Cmd
			m.confirmModal, cmd = m.confirmModal.Update(msg)
			return m, cmd
		}
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes a keystroke. Rule 135 assigns each key exactly one job, and
// a tabbed form is the layout that lets it: Tab switches section, ↑↓ moves
// between fields, ←→ cycles a closed set, Space toggles.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		return m.switchTab(1)
	case "shift+tab":
		return m.switchTab(-1)
	case "up":
		return m.moveField(-1)
	case "down":
		return m.moveField(1)
	case "left":
		return m.cycleField(-1)
	case "right":
		return m.cycleField(1)
	case " ":
		return m.toggleField()
	}

	// Anything else belongs to the input when a text field has focus. Nothing
	// below this point may claim a printable key: a path or a URL can contain
	// any of them.
	if m.current().takesText() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// switchTab commits the field being edited before leaving, so a value typed and
// abandoned by pressing Tab is not lost.
func (m Model) switchTab(step int) (tea.Model, tea.Cmd) {
	m, cmd, ok := m.commitFocused()
	if !ok {
		return m, cmd
	}

	m.activeTab = (m.activeTab + step + len(m.sections)) % len(m.sections)
	m.focusedField = 0
	m.bindInput()
	return m, cmd
}

// moveField changes the focused field, committing the one being left.
func (m Model) moveField(step int) (tea.Model, tea.Cmd) {
	m, cmd, ok := m.commitFocused()
	if !ok {
		return m, cmd
	}

	fields := m.fields()
	if len(fields) == 0 {
		return m, cmd
	}
	m.focusedField = (m.focusedField + step + len(fields)) % len(fields)
	m.bindInput()
	return m, cmd
}

// commitFocused writes the focused field back to the config and persists.
//
// It returns ok=false when the value was refused, which is what keeps the
// cursor on the offending field: a rejected value must not be left on screen
// while the config quietly holds something else.
func (m Model) commitFocused() (Model, tea.Cmd, bool) {
	f := m.current()

	// Leaving the secret backend with a different value asks first. Confirming
	// on the way out rather than on every ←→ is what makes the field usable.
	if f.Label == secretBackendLabel && *f.str(m.config) != m.backendOnFocus {
		m.confirmModal = sharedcomponents.NewConfirmModal(
			"Change secret backend",
			"Stored secrets are not migrated. The GitLab token and registry passwords will have to be entered again. Continue?")
		return m, nil, false
	}

	if !f.takesText() {
		return m, nil, true
	}

	// Compared by accessor, not by label: two tabs could both hold a field
	// called "URL", and pointer identity cannot be wrong about which setting is
	// in front of the cursor.
	isGitLabURL := f.str != nil && f.str(m.config) == &m.config.GitLab.URL
	before := m.config.GitLab.URL

	if err := f.Apply(m.config, m.input.Value()); err != nil {
		log.Printf("ERROR [configuration] %s: %v", f.Label, err)
		m.footerError = err.Error()
		return m, clearFooterCmd(), false
	}

	if isGitLabURL && m.config.GitLab.URL != before {
		// Said unconditionally rather than only when a session is open: this
		// view holds no session state, and "you will need to sign in again" is
		// true either way. Warning beats forbidding — the same call as for the
		// secret backend.
		m.footerInfo = "GitLab URL changed — sign in again with :gla"
		return m, tea.Batch(m.persist(saved{gitlabURL: true}), clearFooterCmd()), true
	}
	return m, m.persist(saved{}), true
}

// cycleField advances a closed-set field (Rule 132).
func (m Model) cycleField(step int) (tea.Model, tea.Cmd) {
	f := m.current()
	if f.Kind != kindCycle {
		return m, nil
	}
	f.Cycle(m.config, step)

	// The theme is the one setting whose effect is the screen itself, so it is
	// applied as it is cycled rather than on blur — otherwise the user is
	// choosing blind.
	if f.Label == themeLabel {
		return m, m.persist(saved{theme: true})
	}
	// The backend is confirmed on blur, so nothing is written here.
	if f.Label == secretBackendLabel {
		return m, nil
	}
	return m, m.persist(saved{})
}

// toggleField flips a checkbox. Space is the only key that may (Rule 135).
func (m Model) toggleField() (tea.Model, tea.Cmd) {
	f := m.current()
	if f.Kind != kindToggle {
		return m, nil
	}
	if m.isDisabled(f) {
		m.footerInfo = "Not supported in Trivy client-server mode"
		return m, clearFooterCmd()
	}
	f.Toggle(m.config)
	return m, m.persist(saved{})
}

// saved says which settings this write touched that the router has to act on
// rather than merely rebuild views against.
type saved struct {
	theme     bool
	gitlabURL bool
}

// persist applies the cross-field constraints, saves, and tells the router.
func (m Model) persist(what saved) tea.Cmd {
	m.applyServerModeConstraints()

	if err := config.Save(m.config); err != nil {
		log.Printf("ERROR [configuration] save context %s: %v", m.context, err)
		// Reported through the message rather than set here: Update() owns the
		// model, and this is called from it, but the caller decides the footer.
		return func() tea.Msg { return saveFailedMsg{err} }
	}

	cfg := m.config
	return func() tea.Msg {
		return ConfigSavedMsg{Config: cfg, ThemeChanged: what.theme, GitLabURLChanged: what.gitlabURL}
	}
}

// applyServerModeConstraints forces off the three scan options the Trivy
// client-server protocol does not support. A genuine constraint, carried over
// from the security form this view replaces.
func (m *Model) applyServerModeConstraints() {
	if !m.serverMode() {
		return
	}
	m.config.Scan.EnableMisconfig = false
	m.config.Scan.EnableLicense = false
	m.config.Scan.GenerateSBOM = false
}

type saveFailedMsg struct{ err error }

// handleBackendConfirmed commits the new secret backend and asks the router to
// resolve a fresh credentials.Selection for the context.
func (m Model) handleBackendConfirmed() (tea.Model, tea.Cmd) {
	m.confirmModal = nil
	m.backendOnFocus = m.config.App.SecretBackend

	if err := config.Save(m.config); err != nil {
		log.Printf("ERROR [configuration] save context %s: %v", m.context, err)
		m.footerError = "Failed to save — check logs"
		return m, clearFooterCmd()
	}
	cfg := m.config
	return m, func() tea.Msg {
		return ConfigSavedMsg{Config: cfg, BackendChanged: true}
	}
}

// handleBackendRejected puts the setting back. The value was already cycled
// into the config, so declining has to undo it.
func (m Model) handleBackendRejected() (tea.Model, tea.Cmd) {
	m.confirmModal = nil
	m.config.App.SecretBackend = m.backendOnFocus
	return m, nil
}

// bindInput points the shared input at the focused field, or parks it.
func (m *Model) bindInput() {
	f := m.current()

	if f.Label == secretBackendLabel {
		m.backendOnFocus = m.config.App.SecretBackend
	}

	if !f.takesText() {
		m.input.Blur()
		m.input.SetValue("")
		return
	}
	m.resizeInput()
	m.input.SetValue(f.Value(m.config))
	m.input.CursorEnd()
	m.input.Focus()
}

// Labels the view has to recognise. Comparing on the label keeps the field
// table declarative; these two are the only settings that need special
// handling, and naming them here is what makes that visible.
const (
	themeLabel         = "Theme"
	secretBackendLabel = "Secret backend"
)

// resizeInput fits the input between the value column and the right border.
func (m *Model) resizeInput() {
	const focusIndicator, borders = 2, 4
	m.input.Width = max(m.width-m.chevronColumn()-4-focusIndicator-borders, 20)
}
