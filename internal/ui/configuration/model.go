package configuration

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"log"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
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
	// BackendChanged and GitLabURLChanged are the two settings the router must
	// act on rather than just rebuild views against: one needs a fresh
	// credentials.Selection, the other invalidates a live GitLab session.
	BackendChanged   bool
	GitLabURLChanged bool
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

	m := Model{
		config:   cfg,
		context:  config.CurrentContextName(),
		sections: sections(themes, command.ViewNames()),
		input:    in,
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

// isDisabled reports whether a scan option is unavailable because a Trivy
// server is configured. The protocol does not support these three, so they are
// forced off rather than silently ignored.
func (m Model) isDisabled(f field) bool {
	return m.serverMode() && serverModeFields[f.Label]
}

func (m Model) serverMode() bool {
	return m.config.Scan.TrivyServer != ""
}
