package configuration

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
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
		return m, m.footer.Error("Failed to save — check logs")

	case tea.KeyMsg:
		if m.confirmModal != nil {
			var cmd tea.Cmd
			m.confirmModal, cmd = m.confirmModal.Update(msg)
			return m, cmd
		}
		return m.handleKey(msg)
	}

	m.footer.Handle(msg)
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
	case "esc":
		return m.commitField()
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
	m.focusedField = m.settleFocus(0, 1)
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
	m.focusedField = m.settleFocus((m.focusedField+step+len(fields))%len(fields), step)
	m.bindInput()
	return m, cmd
}

// settleFocus walks past the rows the cursor may not stop on, in the direction
// it was already moving — so ↓ onto a static row lands below it and ↑ above,
// rather than bouncing the cursor back where it came from.
//
// The walk is bounded by the field count: a tab of nothing but static rows
// would otherwise loop forever, and returning where it started is the honest
// answer for one.
func (m Model) settleFocus(idx, step int) int {
	fields := m.fields()
	if len(fields) == 0 {
		return 0
	}
	if step == 0 {
		step = 1
	}
	for range fields {
		if fields[idx].focusable() {
			return idx
		}
		idx = (idx + step + len(fields)) % len(fields)
	}
	return idx
}

// commitField writes the focused field without moving the cursor.
//
// esc is the key one reaches for to "close" a field, and it did nothing at all
// — neither commit, nor exit. It fell through to the input, which ignores it,
// so the only gesture a user tries to settle a value was the one gesture that
// settled nothing (§1.3 D62).
//
// It re-binds the input afterwards, so a value the field normalised on the way
// in is what stays on screen: distinguishing a written value from a merely
// typed one is the other half of what made that defect durable.
//
// On a checkbox or an ordinary cycle field this is a no-op, which is why
// GetShortcuts greys it there (Rule 130) — those have already persisted by the
// time the cursor could leave.
func (m Model) commitField() (tea.Model, tea.Cmd) {
	m, cmd, ok := m.commitFocused()
	if !ok {
		return m, cmd
	}
	m.bindInput()
	return m, cmd
}

// Leave settles the focused field before the router switches away, and refuses
// to be left when the value is not acceptable.
//
// Nothing committed a text field on the way out: commitFocused was reached only
// from switchTab and moveField, so a path typed and abandoned with ctrl+p was
// never written. Worse than lost — the router caches this view and Init() does
// nothing, so returning to :cfg showed the typed value while the file held the
// old one, with nothing on screen to tell them apart. That is what sent the
// search for the defect into the scanner (§1.3 D62).
//
// A refused value keeps the screen (ok=false), for the reason moveField already
// does: the config must not quietly hold something other than what is displayed.
// It is also the only answer that can be *said*. The footer belongs to this
// view, so a message explaining an abandoned value would leave the screen with
// the view that posted it — which is the same silence, one layer down.
func (m Model) Leave() (tea.Model, tea.Cmd, bool) {
	m, cmd, ok := m.commitFocused()
	if !ok {
		return m, cmd, false
	}
	m.bindInput()
	return m, cmd, true
}

// commitFocused writes the focused field back to the config and persists.
//
// It returns ok=false when the value was refused, which is what keeps the
// cursor on the offending field: a rejected value must not be left on screen
// while the config quietly holds something else.
func (m Model) commitFocused() (Model, tea.Cmd, bool) {
	f := m.current()

	// Leaving the forge with a different platform closes the session and says
	// so. Settled here rather than on every ←→ for the reason the backend below
	// is: cycling through three values would otherwise close the session three
	// times, including on the way back to where it started.
	if f.Label == forgeLabel {
		m, cmd := m.commitForge()
		return m, cmd, true
	}

	// Leaving the secret backend with a different value asks first. Confirming
	// on the way out rather than on every ←→ is what makes the field usable.
	if f.Label == secretBackendLabel && *f.str(m.config) != m.backendOnFocus {
		m.confirmModal = sharedcomponents.NewConfirmModal(
			"Change secret backend",
			"Stored secrets are not migrated. The "+m.vocab().Name+" token and registry passwords will have to be entered again. Continue?")
		return m, nil, false
	}

	if !f.takesText() {
		return m, nil, true
	}

	// Compared by accessor, not by label: two tabs could both hold a field
	// called "URL", and pointer identity cannot be wrong about which setting is
	// in front of the cursor.
	isForgeURL := f.str != nil && f.str(m.config) == &m.config.Forge.URL
	before := m.config.Forge.URL

	if err := f.Apply(m.config, m.input.Value()); err != nil {
		log.Printf("ERROR [configuration] %s: %v", f.Label, err)
		return m, m.footer.Error(err.Error()), false
	}

	if isForgeURL && m.config.Forge.URL != before {
		m = m.detectForgeFromURL()

		// Said unconditionally rather than only when a session is open: this
		// view holds no session state, and "you will need to sign in again" is
		// true either way. Warning beats forbidding — the same call as for the
		// secret backend.
		return m, tea.Batch(
			m.persist(saved{forgeChanged: true}),
			m.footer.Info(m.vocab().Name+" URL changed — sign in again with :"+string(command.ViewGitAuth)),
		), true
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
	// The forge is settled on blur too, and for a related reason: cycling
	// through it would otherwise close the session once per keypress, including
	// on the way back to where it started.
	if f.Label == forgeLabel {
		// Touched: from here on, host detection leaves the field alone.
		m.forgeTouched = true
		return m, nil
	}
	return m, m.persist(saved{})
}

// toggleField flips a checkbox. Space is the only key that may (Rule 135).
func (m Model) toggleField() (tea.Model, tea.Cmd) {
	f := m.current()

	// A secret row is shown or hidden, and nothing is written: `space` is the
	// only key that toggles anything in a form (Rule 135), and revealing is a
	// toggle like any other.
	if f.Kind == kindSecret {
		if f.Value(m.config) == "" {
			return m, m.footer.Warn("There is no token — the MCP server is not running for this context")
		}
		m.shown[f.Label] = !m.shown[f.Label]
		return m, nil
	}

	if f.Kind != kindToggle {
		return m, nil
	}
	if m.isDisabled(f) {
		// A warning: nothing failed, the setting simply has no meaning here.
		return m, m.footer.Warn("Not supported in Trivy client-server mode")
	}
	f.Toggle(m.config)
	return m, m.persist(saved{})
}

// saved says which settings this write touched that the router has to act on
// rather than merely rebuild views against.
type saved struct {
	theme bool
	// forgeChanged says the session this context holds was opened against
	// something the config no longer describes — a different host, or a
	// different platform. One flag for both, because the consequence is one:
	// the session is stale and the user has to sign in again.
	forgeChanged bool
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
		return ConfigSavedMsg{Config: cfg, ThemeChanged: what.theme, ForgeChanged: what.forgeChanged}
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
}

type saveFailedMsg struct{ err error }

// handleBackendConfirmed commits the new secret backend and asks the router to
// resolve a fresh credentials.Selection for the context.
func (m Model) handleBackendConfirmed() (tea.Model, tea.Cmd) {
	m.confirmModal = nil
	m.backendOnFocus = m.config.App.SecretBackend

	if err := config.Save(m.config); err != nil {
		log.Printf("ERROR [configuration] save context %s: %v", m.context, err)
		return m, m.footer.Error("Failed to save — check logs")
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
	if f.Label == forgeLabel {
		m.forgeOnFocus = m.config.Forge.Type
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
	forgeLabel         = "Forge"
)

// resizeInput fits the input between the value column and the right border.
func (m *Model) resizeInput() {
	const focusIndicator, borders = 2, 4
	m.input.Width = max(m.width-m.chevronColumn()-4-focusIndicator-borders, 20)
}
