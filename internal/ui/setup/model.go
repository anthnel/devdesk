// Package setup implements `dk setup`: a small standalone Bubble Tea program
// that walks a user through creating (or overwriting) a DevDesk context. It
// is not part of the app router — it runs before any context is loaded, so
// it builds a *config.Config from scratch and writes it with
// config.SaveContext, the same call the configuration view uses.
package setup

import (
	"log"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Field indices, top to bottom, matching the order rendered in View.
const (
	fieldContextName = iota
	fieldSecretBackend
	fieldContainerEngine
	fieldTheme
	fieldForgeType
	fieldForgeURL
	fieldForgeNamespace
	fieldForgeVisibility
	fieldForgeCloneMethod
	fieldForgeToken
	fieldSubmit
)

// secretBackendOptions mirrors config.AppConfig.SecretBackend's closed set.
var secretBackendOptions = []string{"auto", "keyring", "git-credential"}

// cloneMethodOptions mirrors config.ForgeConfig.CloneMethod's closed set.
var cloneMethodOptions = []string{"https", "ssh"}

// stage tracks what the wizard is doing once every field has a value.
type stage int

const (
	stageForm stage = iota
	stageConfirmOverwrite
	stageConfirmSetCurrent
	stageDone
)

// modalPurpose says what a Yes/No answer on the active confirm modal means —
// the wizard reuses one ConfirmModal for both of its confirmations rather
// than declaring two near-identical fields.
type modalPurpose int

const (
	modalNone modalPurpose = iota
	modalOverwrite
	modalSetCurrent
)

// Model is the wizard: one form, top to bottom (Rule 112), that ends by
// writing a context via config.SaveContext.
type Model struct {
	width, height int

	focusedField int
	stage        stage
	modalPurpose modalPurpose
	confirmModal *components.ConfirmModal

	contextNameInput textinput.Model

	secretBackendIdx   int
	containerEngineIdx int

	themeOptions []string
	themeIdx     int

	forgeTypeIdx int
	// forgeURLTouched stops the URL field from being overwritten once the user
	// has typed into it, so switching Forge back and forth does not clobber a
	// self-hosted URL they already entered.
	forgeURLTouched     bool
	forgeURLInput       textinput.Model
	forgeNamespaceInput textinput.Model
	visibilityValue     string
	cloneMethodIdx      int
	forgeTokenInput     textinput.Model

	// pending names the async step in flight, shown in the footer with a
	// spinner (Rule 128); empty when nothing is running.
	pending string
	footer  components.FooterMessage
	spinner spinner.Model

	savedPath             string
	currentContextWarning string
}

// New builds the wizard with every field at its default value.
func New() Model {
	nameInput := textinput.New()
	nameInput.Placeholder = "e.g. work, staging"
	nameInput.CharLimit = 64
	nameInput.Width = 40
	styleTextInput(&nameInput)
	nameInput.Focus()

	urlInput := textinput.New()
	urlInput.CharLimit = 200
	urlInput.Width = 50
	styleTextInput(&urlInput)

	nsInput := textinput.New()
	nsInput.Placeholder = "optional"
	nsInput.CharLimit = 100
	nsInput.Width = 40
	styleTextInput(&nsInput)

	tokenInput := textinput.New()
	tokenInput.Placeholder = "optional — can be set later via the auth view"
	tokenInput.CharLimit = 200
	tokenInput.Width = 50
	tokenInput.EchoMode = textinput.EchoPassword
	styleTextInput(&tokenInput)

	themes, err := theme.ListThemes()
	if err != nil || len(themes) == 0 {
		themes = []string{"default"}
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	m := Model{
		focusedField:        fieldContextName,
		contextNameInput:    nameInput,
		themeOptions:        themes,
		forgeURLInput:       urlInput,
		forgeNamespaceInput: nsInput,
		visibilityValue:     "private",
		forgeTokenInput:     tokenInput,
		spinner:             s,
	}
	m.forgeURLInput.SetValue(forge.VocabularyFor(config.ForgeTypes()[m.forgeTypeIdx]).ExampleURL)
	return m
}

// Init starts the cursor blink, the only thing the wizard needs on launch.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update is the single Elm-architecture entry point (Rule 110): every field
// mutation happens here, never inside a Cmd.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case components.ConfirmModalYesMsg:
		return m.handleConfirmYes()

	case components.ConfirmModalNoMsg:
		return m.handleConfirmNo()

	case ContextExistsMsg:
		return m.handleContextExists(msg)

	case TokenValidatedMsg:
		return m.handleTokenValidated(msg)

	case ContextSavedMsg:
		return m.handleContextSaved(msg)

	case CurrentContextSetMsg:
		return m.handleCurrentContextSet(msg)
	}

	if m.footer.Handle(msg) {
		return m, nil
	}
	return m, nil
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.stage == stageConfirmOverwrite || m.stage == stageConfirmSetCurrent {
		var cmd tea.Cmd
		m.confirmModal, cmd = m.confirmModal.Update(msg)
		return m, cmd
	}
	if m.stage == stageDone {
		if msg.String() == "enter" || msg.String() == "esc" || msg.String() == "q" {
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m, tea.Quit
	case "up":
		m.moveFocus(-1)
		return m, nil
	case "down":
		m.moveFocus(1)
		return m, nil
	case "left":
		m.cycle(-1)
		return m, nil
	case "right":
		m.cycle(1)
		return m, nil
	case "enter":
		if m.focusedField == fieldSubmit {
			return m.submit()
		}
		m.moveFocus(1)
		return m, nil
	}

	return m.updateFocusedInput(msg)
}

// moveFocus walks the field list, wrapping at both ends — the same shape
// CreationForm uses, and consistent with Rule 111 having no "past the edge"
// state to fall into.
func (m *Model) moveFocus(delta int) {
	m.focusedField = wrap(m.focusedField+delta, fieldSubmit+1)
	m.updateFocus()
}

func (m *Model) updateFocus() {
	m.contextNameInput.Blur()
	m.forgeURLInput.Blur()
	m.forgeNamespaceInput.Blur()
	m.forgeTokenInput.Blur()

	switch m.focusedField {
	case fieldContextName:
		m.contextNameInput.Focus()
	case fieldForgeURL:
		m.forgeURLInput.Focus()
	case fieldForgeNamespace:
		m.forgeNamespaceInput.Focus()
	case fieldForgeToken:
		m.forgeTokenInput.Focus()
	}
}

// cycle changes a closed-set field's value (Rule 132). On any other field —
// including a text field, where left/right have no role here (matching
// CreationForm's name/description fields) — it does nothing.
func (m *Model) cycle(delta int) {
	switch m.focusedField {
	case fieldSecretBackend:
		m.secretBackendIdx = wrap(m.secretBackendIdx+delta, len(secretBackendOptions))
	case fieldContainerEngine:
		m.containerEngineIdx = wrap(m.containerEngineIdx+delta, len(config.ContainerEngines()))
	case fieldTheme:
		m.themeIdx = wrap(m.themeIdx+delta, len(m.themeOptions))
	case fieldForgeType:
		m.forgeTypeIdx = wrap(m.forgeTypeIdx+delta, len(config.ForgeTypes()))
		m.onForgeTypeChanged()
	case fieldForgeVisibility:
		opts := m.visibilityOptions()
		idx := indexOf(opts, m.visibilityValue)
		if idx < 0 {
			idx = 0
		}
		m.visibilityValue = opts[wrap(idx+delta, len(opts))]
	case fieldForgeCloneMethod:
		m.cloneMethodIdx = wrap(m.cloneMethodIdx+delta, len(cloneMethodOptions))
	}
}

// onForgeTypeChanged keeps the URL placeholder and the visibility value
// consistent with the newly selected forge.
func (m *Model) onForgeTypeChanged() {
	forgeType := config.ForgeTypes()[m.forgeTypeIdx]
	if !m.forgeURLTouched {
		m.forgeURLInput.SetValue(forge.VocabularyFor(forgeType).ExampleURL)
	}
	// GitHub has no "internal" visibility. Coerced to the most private option
	// rather than clamped by position — clamping "internal" (index 1 of 3)
	// into a 2-option list would land on "public", the opposite of safe.
	if indexOf(m.visibilityOptions(), m.visibilityValue) < 0 {
		m.visibilityValue = "private"
	}
}

// visibilityOptions is the closed set config.ForgeConfig.DefaultVisibility
// accepts for the currently selected forge type.
func (m Model) visibilityOptions() []string {
	if config.ForgeTypes()[m.forgeTypeIdx] == config.ForgeGitHub {
		return []string{"private", "public"}
	}
	return []string{"private", "internal", "public"}
}

func (m Model) updateFocusedInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focusedField {
	case fieldContextName:
		m.contextNameInput, cmd = m.contextNameInput.Update(msg)
	case fieldForgeURL:
		m.forgeURLTouched = true
		m.forgeURLInput, cmd = m.forgeURLInput.Update(msg)
	case fieldForgeNamespace:
		m.forgeNamespaceInput, cmd = m.forgeNamespaceInput.Update(msg)
	case fieldForgeToken:
		m.forgeTokenInput, cmd = m.forgeTokenInput.Update(msg)
	}
	return m, cmd
}

func wrap(i, n int) int {
	if n <= 0 {
		return 0
	}
	return ((i % n) + n) % n
}

func indexOf(opts []string, v string) int {
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return -1
}

// submit validates the context name and starts the async chain: existence
// check, then (if a token was entered) validation, then the write itself.
func (m Model) submit() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.contextNameInput.Value())
	if err := config.ValidateContextName(name); err != nil {
		return m, m.footer.Error(err.Error())
	}
	m.contextNameInput.SetValue(name)
	m.pending = "Checking context name..."
	return m, checkContextExistsCmd(name)
}

func (m Model) handleContextExists(msg ContextExistsMsg) (tea.Model, tea.Cmd) {
	m.pending = ""
	if msg.Err != nil {
		log.Printf("ERROR [setup] check context exists: %v", msg.Err)
		return m, m.footer.Error("Failed to check context: " + msg.Err.Error())
	}
	if msg.Exists {
		name := strings.TrimSpace(m.contextNameInput.Value())
		m.confirmModal = components.NewConfirmModal("Context exists",
			"Context '"+name+"' already exists and will be overwritten. Continue?")
		m.stage = stageConfirmOverwrite
		m.modalPurpose = modalOverwrite
		return m, nil
	}
	return m.proceedAfterNameConfirmed()
}

func (m Model) handleConfirmYes() (tea.Model, tea.Cmd) {
	purpose := m.modalPurpose
	m.confirmModal = nil
	m.stage = stageForm
	m.modalPurpose = modalNone

	switch purpose {
	case modalOverwrite:
		return m.proceedAfterNameConfirmed()
	case modalSetCurrent:
		m.pending = "Setting current context..."
		return m, setCurrentContextCmd(strings.TrimSpace(m.contextNameInput.Value()))
	}
	return m, nil
}

func (m Model) handleConfirmNo() (tea.Model, tea.Cmd) {
	purpose := m.modalPurpose
	m.confirmModal = nil
	m.stage = stageForm
	m.modalPurpose = modalNone

	switch purpose {
	case modalOverwrite:
		// Back to the name field to pick a different one, rather than
		// aborting the whole wizard over one collision.
		m.focusedField = fieldContextName
		m.updateFocus()
		return m, nil
	case modalSetCurrent:
		m.stage = stageDone
		return m, nil
	}
	return m, nil
}

// proceedAfterNameConfirmed runs once the context name is settled: validate
// the forge token if one was entered, otherwise go straight to saving.
func (m Model) proceedAfterNameConfirmed() (tea.Model, tea.Cmd) {
	token := strings.TrimSpace(m.forgeTokenInput.Value())
	if token == "" {
		return m.startSave()
	}
	m.pending = "Validating token..."
	return m, validateTokenCmd(
		strings.TrimSpace(m.contextNameInput.Value()),
		secretBackendOptions[m.secretBackendIdx],
		config.ForgeTypes()[m.forgeTypeIdx],
		strings.TrimSpace(m.forgeURLInput.Value()),
		token,
	)
}

func (m Model) handleTokenValidated(msg TokenValidatedMsg) (tea.Model, tea.Cmd) {
	m.pending = ""
	if msg.Err != nil {
		log.Printf("ERROR [setup] validate token: %v", msg.Err)
		m.focusedField = fieldForgeToken
		m.updateFocus()
		return m, m.footer.Error("Token validation failed: " + msg.Err.Error())
	}
	return m.startSave()
}

func (m Model) startSave() (tea.Model, tea.Cmd) {
	cfg := buildConfig(m)
	m.pending = "Saving context..."
	return m, saveContextCmd(cfg, strings.TrimSpace(m.contextNameInput.Value()))
}

func (m Model) handleContextSaved(msg ContextSavedMsg) (tea.Model, tea.Cmd) {
	m.pending = ""
	if msg.Err != nil {
		log.Printf("ERROR [setup] save context: %v", msg.Err)
		return m, m.footer.Error("Failed to save context: " + msg.Err.Error())
	}
	name := strings.TrimSpace(m.contextNameInput.Value())
	if path, err := config.GetContextPath(name); err == nil {
		m.savedPath = path
	}
	m.confirmModal = components.NewConfirmModal("Context saved",
		"Set '"+name+"' as the active context?")
	m.stage = stageConfirmSetCurrent
	m.modalPurpose = modalSetCurrent
	return m, nil
}

func (m Model) handleCurrentContextSet(msg CurrentContextSetMsg) (tea.Model, tea.Cmd) {
	m.pending = ""
	if msg.Err != nil {
		log.Printf("ERROR [setup] set current context: %v", msg.Err)
		m.currentContextWarning = "Context saved, but it could not be made active: " + msg.Err.Error()
	}
	m.stage = stageDone
	return m, nil
}
