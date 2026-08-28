package configuration

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"log"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ConfigSavedMsg tells the router the context's configuration changed on disk.
//
// BackendChanged is separate from "something changed" because it is the one
// setting the router cannot honour by rebuilding views: it has to resolve a new
// credentials.Selection first.
type ConfigSavedMsg struct {
	Config       *config.Config
	ThemeChanged bool
	// BackendChanged and ForgeChanged are the two settings the router must act
	// on rather than just rebuild views against: one needs a fresh
	// credentials.Selection, the other invalidates a live forge session.
	//
	// ForgeChanged is one flag for two settings — the URL and the platform —
	// because the consequence is one: the session was opened against something
	// the config no longer describes.
	BackendChanged bool
	ForgeChanged   bool
}

// Model is the configuration view: every scalar setting in the current context,
// grouped into tabs.
//
// Tabs are not decoration. Twenty-nine fields in one list is unusable, and
// under Rule 135 a tabbed form is the only layout that uses the keyboard
// correctly: Tab is reserved for tabs and ↑↓ for fields, so each key has
// exactly one job.
type Model struct {
	config   *config.Config
	context  string
	sections []section

	activeTab    int
	focusedField int

	// input is bound to the focused text or integer field. There is one, not one
	// per field: the value is read from the config on focus and written back on
	// blur, so the input is a view onto the setting rather than a second copy of
	// it.
	input textinput.Model

	// forgeOnFocus is what forge.type held when the field took focus, and
	// forgeTouched whether the user has ever moved it. The second is what stops
	// host detection overwriting an explicit choice — the difference between
	// helpful and possessive.
	forgeOnFocus string
	forgeTouched bool

	// themes and configPath are kept because the field table is rebuilt when
	// the forge changes: its title, its icon and half its options come from the
	// platform, and the router keeps this view on a save.
	themes     []string
	configPath string

	// backendOnFocus is what app.secret_backend held when the field took focus.
	// Changing it is confirmed on the way out rather than on every ←/→, and this
	// is what "No" restores.
	backendOnFocus string
	confirmModal   *sharedcomponents.ConfirmModal

	// footer is the one line of transient state below the tab bar (Rule 128).
	footer sharedcomponents.FooterMessage

	width  int
	height int
}

// New builds the view for the configuration currently loaded.
func New(cfg *config.Config) Model {
	themes, err := theme.ListThemes()
	if err != nil || len(themes) == 0 {
		// A theme directory that cannot be read still leaves the built-in one, so
		// the cycle has something to offer rather than nothing.
		log.Printf("ERROR [configuration] list themes: %v", err)
		themes = []string{"default"}
	}

	in := textinput.New()
	in.CharLimit = 512
	theme.StyleTextInput(&in)

	context := config.CurrentContextName()

	// A home directory that cannot be resolved leaves the row empty rather than
	// absent: the label still says which fact is missing.
	configPath, err := config.GetContextPath(context)
	if err != nil {
		log.Printf("ERROR [configuration] resolve context path for %s: %v", context, err)
	}

	m := Model{
		config:       cfg,
		context:      context,
		sections:     sections(themes, command.ViewNames(), configPath, context, cfg.Forge.Type, forge.VocabularyFor(cfg.Forge.Type)),
		input:        in,
		themes:       themes,
		configPath:   configPath,
		forgeOnFocus: cfg.Forge.Type,
	}
	m.bindInput()
	return m
}

// Init satisfies tea.Model. Nothing is loaded asynchronously: the configuration
// is already in memory, which is what makes this view open on its first frame.
func (m Model) Init() tea.Cmd { return nil }

// InEditMode reports whether a text field or the confirm modal has the
// keyboard, which is what stops the router claiming ":", "q" and "?".
func (m Model) InEditMode() bool {
	return m.confirmModal != nil || m.current().takesText()
}

// current returns the focused field.
func (m Model) current() field {
	fields := m.fields()
	if m.focusedField < 0 || m.focusedField >= len(fields) {
		return field{}
	}
	return fields[m.focusedField]
}

// fields returns the active tab's fields.
func (m Model) fields() []field {
	if m.activeTab < 0 || m.activeTab >= len(m.sections) {
		return nil
	}
	return m.sections[m.activeTab].Fields
}

// takesText reports whether a field is edited by typing.
func (f field) takesText() bool {
	return f.Kind == kindText || f.Kind == kindInteger
}

// focusable reports whether the cursor may stop on a field. A static row is the
// one that answers no: there is nothing to type, cycle or toggle on it, and a
// focus indicator on a row no key acts upon says the opposite.
func (f field) focusable() bool { return f.Kind != kindStatic }

// settlesOnBlur reports whether leaving a field has any effect on it, which is
// the one question esc answers here (Rule 130).
//
// A checkbox and an ordinary cycle field write as they are pressed, so there is
// nothing left to settle. The three that do not are a text field — the input
// holds the value until it is applied — and the forge and the secret backend,
// which are deliberately settled on the way out rather than on every ←→, so
// that cycling past a value does not close the session or ask three times.
func (m Model) settlesOnBlur(f field) bool {
	return f.takesText() || f.Label == forgeLabel || f.Label == secretBackendLabel
}

// isDisabled reports whether a scan option is unavailable because a Trivy
// server is configured. The protocol does not support these three, so they are
// forced off rather than silently ignored.
func (m Model) isDisabled(f field) bool {
	return m.serverMode() && serverModeFields[f.Label]
}

func (m Model) serverMode() bool {
	return m.config.Scan.TrivyServer != ""
}

// vocab is the wording of the forge this context targets, resolved from the
// config it is editing. It is what keeps a message about "the GitLab token"
// from saying GitLab in a GitHub context.
func (m Model) vocab() forge.Vocabulary {
	if m.config == nil {
		return forge.VocabularyFor("")
	}
	return forge.VocabularyFor(m.config.Forge.Type)
}
