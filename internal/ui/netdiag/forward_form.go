package netdiag

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ForwardFormSubmitMsg carries a confirmed forward request (Rule 109).
type ForwardFormSubmitMsg struct {
	LocalPort int
	Target    string
}

// ForwardFormCancelMsg is sent when the form is dismissed.
type ForwardFormCancelMsg struct{}

// The form's fields, in the order ↑↓ walks them.
const (
	forwardFieldPort = iota
	forwardFieldTarget
	forwardFieldCount
)

// forwardLabelWidth pads the two labels to a common width so the chevrons line
// up, which is the whole of the alignment in a two-field form.
const forwardLabelWidth = 11

// ForwardForm asks for a local port and a target, and nothing else.
//
// It validates only that the port is a number, and deliberately not that it is
// above 1024, that the target parses, or that anything answers there. Every one
// of those rules lives in internal/forward, which is what actually enforces
// them; restating them here would be a second copy free to drift, and the one
// on screen would be the copy that is not enforcing anything. The registry's
// refusal reaches the user through the footer instead (Rule 128).
type ForwardForm struct {
	portInput   textinput.Model
	targetInput textinput.Model
	focused     int
	width       int
}

// NewForwardForm builds the form, focused on the port.
func NewForwardForm(width int) *ForwardForm {
	newInput := func(placeholder string, limit int) textinput.Model {
		ti := textinput.New()
		ti.CharLimit = limit
		ti.Placeholder = placeholder
		theme.StyleTextInput(&ti)
		return ti
	}

	f := &ForwardForm{
		portInput:   newInput("8080", 5),
		targetInput: newInput("host:port", 255),
		width:       width,
	}
	f.updateFocus()
	return f
}

// SetWidth keeps the form sized with the viewport (Rule 108).
func (f *ForwardForm) SetWidth(width int) { f.width = width }

// updateFocus gives the keyboard to the focused field and takes it from the
// others, which is what makes exactly one cursor visible.
func (f *ForwardForm) updateFocus() {
	inputs := []*textinput.Model{&f.portInput, &f.targetInput}
	for i, in := range inputs {
		if i == f.focused {
			in.Focus()
			continue
		}
		in.Blur()
	}
}

// Update handles the form's keys. ↑↓ move between fields, enter confirms and
// esc cancels (Rule 135); tab does nothing here, it belongs to the tabs.
func (f *ForwardForm) Update(msg tea.Msg) (*ForwardForm, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}

	switch key.String() {
	case "up":
		f.focused = max(f.focused-1, 0)
		f.updateFocus()
		return f, nil
	case "down":
		f.focused = min(f.focused+1, forwardFieldCount-1)
		f.updateFocus()
		return f, nil
	case "esc":
		return f, func() tea.Msg { return ForwardFormCancelMsg{} }
	case "enter":
		return f, f.submit()
	}

	var cmd tea.Cmd
	if f.focused == forwardFieldPort {
		f.portInput, cmd = f.portInput.Update(msg)
		return f, cmd
	}
	f.targetInput, cmd = f.targetInput.Update(msg)
	return f, cmd
}

// submit turns the two fields into a request, or nil when the port is not a
// number — the one thing that has to be decided here, because the message
// carries an int and there is nothing to put in it otherwise.
func (f *ForwardForm) submit() tea.Cmd {
	port, err := strconv.Atoi(strings.TrimSpace(f.portInput.Value()))
	if err != nil {
		return nil
	}
	target := strings.TrimSpace(f.targetInput.Value())
	return func() tea.Msg {
		return ForwardFormSubmitMsg{LocalPort: port, Target: target}
	}
}

// PortIsANumber reports whether submit would produce anything. The model asks
// before calling submit so it can say why nothing happened rather than
// swallowing the keypress (Rule 130).
func (f *ForwardForm) PortIsANumber() bool {
	_, err := strconv.Atoi(strings.TrimSpace(f.portInput.Value()))
	return err == nil
}

// View renders the form in the viewport (Rule 112), opening on exactly one
// blank line (Rule 131) and carrying no inline help (Rule 134).
func (f *ForwardForm) View() string {
	lines := []string{
		theme.EmptyLineBg(f.width),
		f.renderField("Local port", f.portInput.View(), forwardFieldPort),
		f.renderField("Target", f.targetInput.View(), forwardFieldTarget),
	}
	return strings.Join(lines, "\n")
}

// renderField draws one single-line field: indicator, padded label, chevron,
// value (Rule 120). Single-line fields are separated by one newline, which
// strings.Join supplies (Rule 113).
func (f *ForwardForm) renderField(label, value string, field int) string {
	padded := label + strings.Repeat(" ", max(forwardLabelWidth-len([]rune(label)), 0))
	if f.focused == field {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+padded+" "+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+padded+" "+theme.IconChevronRight+" ") + value
}
