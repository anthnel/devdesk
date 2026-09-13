// Package setup implements `dk setup`: a small standalone Bubble Tea program
// that walks a user through creating (or overwriting) a DevDesk context, one
// question at a time. It is not part of the app router — it runs before any
// context is loaded, so it builds a *config.Config from scratch and writes
// it with config.SaveContext, the same call the configuration view uses.
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

// Step indices, in the order the wizard asks them.
const (
	stepFontCheck = iota
	stepContextName
	stepSecretBackend
	stepContainerEngine
	stepTheme
	stepForgeType
	stepForgeURL
	stepForgeNamespace
	stepForgeVisibility
	stepForgeCloneMethod
	stepForgeToken
	stepCount
)

// secretBackendOptions mirrors config.AppConfig.SecretBackend's closed set.
var secretBackendOptions = []string{"auto", "keyring", "git-credential"}

// cloneMethodOptions mirrors config.ForgeConfig.CloneMethod's closed set.
var cloneMethodOptions = []string{"https", "ssh"}

// fontCheckOptions is not a config value — nothing persists it — it only
// decides whether the remediation text is shown under the icon sample.
var fontCheckOptions = []string{"Looks correct", "Broken (boxes or question marks)"}

// stage tracks what the wizard is doing, on top of which question is active.
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

// themeCheckState tracks the one-shot, best-effort attempt to fetch the
// bundled themes from the repository when the wizard reaches that question.
type themeCheckState int

const (
	themeCheckNotStarted themeCheckState = iota
	themeCheckInFlight
	themeCheckDone
	themeCheckUnreachable
)

// secretBackendCheckState tracks whether the currently selected secret
// backend actually resolves to somewhere durable — re-checked every time the
// answer changes, not just once, since the whole point is to catch this
// before a token is typed rather than after (see commands.go's
// checkSecretBackendCmd).
type secretBackendCheckState int

const (
	secretBackendNotChecked secretBackendCheckState = iota
	secretBackendChecking
	secretBackendReachable
	secretBackendUnreachable
)

// engineCheckState tracks whether the currently selected container engine is
// not just installed but actually running.
type engineCheckState int

const (
	engineNotChecked engineCheckState = iota
	engineChecking
	engineReachable
	engineUnreachable
	engineMissing
)

// Model is the wizard: one question at a time (Rule 112's "form in the
// viewport" read literally per-step, rather than all fields on one screen —
// a deliberate choice for this screen alone, so every choice comes with the
// context to make it instead of a bare label).
type Model struct {
	width, height int

	step         int
	stage        stage
	modalPurpose modalPurpose
	confirmModal *confirmPrompt

	contextNameInput textinput.Model

	secretBackendIdx         int
	secretBackendCheck       secretBackendCheckState
	secretBackendCheckDetail string

	containerEngineIdx int
	engineCheck        engineCheckState
	engineCheckDetail  string

	fontCheckIdx int

	themeOptions         []string
	themeIdx             int
	themeCheckState      themeCheckState
	themeFetchAddedCount int

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
	tokenSaveWarning      string
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

	case RemoteThemesFetchedMsg:
		return m.handleRemoteThemesFetched(msg)

	case SecretBackendCheckedMsg:
		return m.handleSecretBackendChecked(msg)

	case ContainerEngineCheckedMsg:
		return m.handleContainerEngineChecked(msg)

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
		return m.stepBack()
	case "left":
		return m, m.cycle(-1)
	case "right":
		return m, m.cycle(1)
	case "enter":
		return m.advance()
	}

	return m.updateFocusedInput(msg)
}

// stepBack goes to the previous question, or quits from the first one —
// there is nothing before it to go back to (Rule 111: Esc cancels or returns
// to the parent level).
func (m Model) stepBack() (tea.Model, tea.Cmd) {
	if m.step == stepFontCheck {
		return m, tea.Quit
	}
	m.step--
	m.updateFocus()
	return m, nil
}

// advance answers the current question. Most steps just move to the next
// one; the first and last carry the async work that answering them starts.
func (m Model) advance() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepContextName:
		return m.submitContextName()
	case stepForgeToken:
		return m.finishWizard()
	}

	m.step++
	m.updateFocus()
	if m.step == stepContainerEngine {
		return m, m.recheckContainerEngineCmd()
	}
	if m.step == stepTheme && m.themeCheckState == themeCheckNotStarted {
		m.themeCheckState = themeCheckInFlight
		m.pending = "Checking for more themes..."
		return m, fetchRemoteThemesCmd()
	}
	return m, nil
}

func (m *Model) updateFocus() {
	m.contextNameInput.Blur()
	m.forgeURLInput.Blur()
	m.forgeNamespaceInput.Blur()
	m.forgeTokenInput.Blur()

	switch m.step {
	case stepContextName:
		m.contextNameInput.Focus()
	case stepForgeURL:
		m.forgeURLInput.Focus()
	case stepForgeNamespace:
		m.forgeNamespaceInput.Focus()
	case stepForgeToken:
		m.forgeTokenInput.Focus()
	}
}

// cycle changes the current question's value, when it has a closed set of
// answers (Rule 132). On a text-input question it does nothing — left/right
// have no role there, matching CreationForm's name/description fields.
//
// It returns a Cmd because two of these questions (secret backend, container
// engine) re-run a live reachability check every time the answer changes —
// the whole point is to catch a problem as early as possible, not just once
// on first arrival at the step.
func (m *Model) cycle(delta int) tea.Cmd {
	switch m.step {
	case stepFontCheck:
		m.fontCheckIdx = wrap(m.fontCheckIdx+delta, len(fontCheckOptions))
	case stepSecretBackend:
		m.secretBackendIdx = wrap(m.secretBackendIdx+delta, len(secretBackendOptions))
		return m.recheckSecretBackendCmd()
	case stepContainerEngine:
		m.containerEngineIdx = wrap(m.containerEngineIdx+delta, len(config.ContainerEngines()))
		return m.recheckContainerEngineCmd()
	case stepTheme:
		m.themeIdx = wrap(m.themeIdx+delta, len(m.themeOptions))
	case stepForgeType:
		m.forgeTypeIdx = wrap(m.forgeTypeIdx+delta, len(config.ForgeTypes()))
		m.onForgeTypeChanged()
	case stepForgeVisibility:
		opts := m.visibilityOptions()
		idx := indexOf(opts, m.visibilityValue)
		if idx < 0 {
			idx = 0
		}
		m.visibilityValue = opts[wrap(idx+delta, len(opts))]
	case stepForgeCloneMethod:
		m.cloneMethodIdx = wrap(m.cloneMethodIdx+delta, len(cloneMethodOptions))
	}
	return nil
}

// recheckSecretBackendCmd re-tests whichever secret backend is currently
// selected. Called on first arrival at the step and every time the answer
// changes.
func (m *Model) recheckSecretBackendCmd() tea.Cmd {
	m.secretBackendCheck = secretBackendChecking
	name := strings.TrimSpace(m.contextNameInput.Value())
	pref := secretBackendOptions[m.secretBackendIdx]
	return tea.Batch(m.spinner.Tick, checkSecretBackendCmd(name, pref))
}

// recheckContainerEngineCmd re-tests whichever container engine is currently
// selected. Called on first arrival at the step and every time the answer
// changes.
func (m *Model) recheckContainerEngineCmd() tea.Cmd {
	m.engineCheck = engineChecking
	pref := config.ContainerEngines()[m.containerEngineIdx]
	return tea.Batch(m.spinner.Tick, checkContainerEngineCmd(pref))
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
	switch m.step {
	case stepContextName:
		m.contextNameInput, cmd = m.contextNameInput.Update(msg)
	case stepForgeURL:
		m.forgeURLTouched = true
		m.forgeURLInput, cmd = m.forgeURLInput.Update(msg)
	case stepForgeNamespace:
		m.forgeNamespaceInput, cmd = m.forgeNamespaceInput.Update(msg)
	case stepForgeToken:
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

// submitContextName validates the name and starts the async chain's first
// link: does a context by this name already exist?
func (m Model) submitContextName() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.contextNameInput.Value())
	if err := config.ValidateContextName(name); err != nil {
		return m, m.footer.Error(err.Error())
	}
	m.contextNameInput.SetValue(name)
	m.pending = "Checking context name..."
	return m, tea.Batch(m.spinner.Tick, checkContextExistsCmd(name))
}

func (m Model) handleContextExists(msg ContextExistsMsg) (tea.Model, tea.Cmd) {
	m.pending = ""
	if msg.Err != nil {
		log.Printf("ERROR [setup] check context exists: %v", msg.Err)
		return m, m.footer.Error("Failed to check context: " + msg.Err.Error())
	}
	if msg.Exists {
		name := strings.TrimSpace(m.contextNameInput.Value())
		m.confirmModal = newConfirmPrompt("Context exists",
			"Context '"+name+"' already exists and will be overwritten. Continue?")
		m.stage = stageConfirmOverwrite
		m.modalPurpose = modalOverwrite
		return m, nil
	}
	return m.advanceToNextStep()
}

// advanceToNextStep is what answering the context name question resolves
// to, once the name is free (or its overwrite is confirmed): move on to the
// next question, triggering the theme fetch if that happens to be it.
func (m Model) advanceToNextStep() (tea.Model, tea.Cmd) {
	m.step = stepSecretBackend
	m.updateFocus()
	return m, m.recheckSecretBackendCmd()
}

func (m Model) handleConfirmYes() (tea.Model, tea.Cmd) {
	purpose := m.modalPurpose
	m.confirmModal = nil
	m.stage = stageForm
	m.modalPurpose = modalNone

	switch purpose {
	case modalOverwrite:
		return m.advanceToNextStep()
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
		// Back to the name question to pick a different one, rather than
		// aborting the whole wizard over one collision.
		m.step = stepContextName
		m.updateFocus()
		return m, nil
	case modalSetCurrent:
		m.stage = stageDone
		return m, nil
	}
	return m, nil
}

func (m Model) handleRemoteThemesFetched(msg RemoteThemesFetchedMsg) (tea.Model, tea.Cmd) {
	m.pending = ""
	if msg.Unreachable {
		m.themeCheckState = themeCheckUnreachable
		return m, nil
	}
	m.themeCheckState = themeCheckDone
	m.themeFetchAddedCount = len(msg.Added)
	if len(msg.Added) > 0 {
		if themes, err := theme.ListThemes(); err == nil {
			m.themeOptions = themes
		}
	}
	return m, nil
}

// finishWizard runs once every question is answered: validate the forge
// token if one was entered, otherwise go straight to saving.
func (m Model) finishWizard() (tea.Model, tea.Cmd) {
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

func (m Model) handleSecretBackendChecked(msg SecretBackendCheckedMsg) (tea.Model, tea.Cmd) {
	if msg.Persists {
		m.secretBackendCheck = secretBackendReachable
	} else {
		m.secretBackendCheck = secretBackendUnreachable
	}
	m.secretBackendCheckDetail = msg.Detail
	return m, nil
}

func (m Model) handleContainerEngineChecked(msg ContainerEngineCheckedMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Missing:
		m.engineCheck = engineMissing
		if pref := config.ContainerEngines()[m.containerEngineIdx]; pref == config.EngineAuto {
			m.engineCheckDetail = "Neither Docker nor Podman was found on PATH."
		} else {
			m.engineCheckDetail = "No " + pref + " binary found on PATH."
		}
	case msg.Err != nil:
		m.engineCheck = engineUnreachable
		m.engineCheckDetail = msg.Name + " is installed, but its daemon did not respond: " + msg.Err.Error()
	default:
		m.engineCheck = engineReachable
		m.engineCheckDetail = msg.Name + " is running."
	}
	return m, nil
}

func (m Model) handleTokenValidated(msg TokenValidatedMsg) (tea.Model, tea.Cmd) {
	m.pending = ""
	if msg.Err != nil {
		log.Printf("ERROR [setup] validate token: %v", msg.Err)
		m.step = stepForgeToken
		m.updateFocus()
		return m, m.footer.Error("Token validation failed: " + msg.Err.Error())
	}
	if msg.SaveWarning != "" {
		log.Printf("WARN [setup] token not durably saved: %s", msg.SaveWarning)
	}
	m.tokenSaveWarning = msg.SaveWarning
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
	m.confirmModal = newConfirmPrompt("Context saved",
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
