package netdiag

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ForwardFormSubmitMsg carries a confirmed forward request (Rule 109). A
// non-empty Name is a named route (§3.74) and LocalPort is then zero: the port
// is the proxy's, and comes from the configuration.
type ForwardFormSubmitMsg struct {
	LocalPort int
	Name      string
	Target    string
}

// ForwardFormCancelMsg is sent when the form is dismissed.
type ForwardFormCancelMsg struct{}

// The kinds of forward the form opens. The first is the default, since a raw
// TCP forward is the common case.
const (
	forwardKindTCP = iota
	forwardKindHTTP
)

// forwardKinds are the two values of the Type field, in the order ←→ cycles
// them (Rule 132).
var forwardKinds = [...]string{"TCP", "HTTP"}

// The form's fields. Which of them are shown depends on the type, so ↑↓ walks
// visibleFields() and not this list.
const (
	forwardFieldType = iota
	forwardFieldPort
	forwardFieldName
	forwardFieldTarget
)

// Why the form itself refuses to submit. Everything else is internal/forward's
// to refuse, and arrives at the footer (Rule 128).
const (
	reasonPortNotANumber  = "The local port has to be a number"
	reasonNameNeedsSuffix = "A named route needs a name ending in .localhost, such as api.localhost"
)

// forwardNameSuffix is what a route name has to end in. The registry validates
// the name in full; the form only decides what it cannot send at all.
const forwardNameSuffix = ".localhost"

// forwardLabelWidth pads the labels to a common width so the chevrons line up.
const forwardLabelWidth = 11

// ForwardForm opens either kind of forward: a raw TCP redirection (a local port
// and a target) or a named HTTP route (a name and a target). The type is a
// field, and only the fields that apply to it are shown.
//
// Local port and Name are mutually exclusive — a route is served on
// network.proxy_port, and a TCP forward has no name — so the type makes the
// invalid combinations impossible to type instead of refusing them afterwards.
//
// A field that is hidden keeps its value while the form is open: a name typed
// and then set aside by switching to TCP is still there when HTTP comes back.
// Only the current type is submitted.
//
// It validates only what it cannot send at all — a port that is not a number,
// a route name that does not end in .localhost — and deliberately not that the
// port is above 1024, that the target parses, that a name is free or that
// anything answers. Every one of those rules lives in internal/forward, which is
// what actually enforces them; restating them here would be a second copy free
// to drift, and the one on screen would be the copy that is not enforcing
// anything.
type ForwardForm struct {
	portInput   textinput.Model
	nameInput   textinput.Model
	targetInput textinput.Model
	kind        int
	focused     int
	width       int
}

// NewForwardForm builds the form, on TCP and focused on the type: it is the
// first field, and a form that opened below its own first field would make ↑
// the way to reach it. The cost is one ↓ to the port in the common TCP case,
// which was judged the lesser surprise.
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
		nameInput:   newInput("api"+forwardNameSuffix, 253),
		targetInput: newInput("host:port", 255),
		focused:     forwardFieldType,
	}
	f.SetWidth(width)
	f.updateFocus()
	return f
}

// forwardPortWidth fits the widest legal port; the target takes the rest.
const forwardPortWidth = 5

// forwardLabelCells is what the label, indicator and chevron take before the
// value starts: indicator (2) + padded label + " " + chevron + " ".
const forwardLabelCells = 2 + forwardLabelWidth + 1 + 1 + 1

// SetWidth keeps the form sized with the viewport (Rule 108).
//
// The inputs need an explicit Width, and a zero one is not "unbounded": a
// textinput with Width 0 renders only the first rune of its placeholder, so an
// unsized field read "8" and "h" instead of "8080" and "host:port".
func (f *ForwardForm) SetWidth(width int) {
	f.width = width
	f.portInput.Width = forwardPortWidth
	room := max(width-forwardLabelCells-2, 20)
	f.nameInput.Width = room
	f.targetInput.Width = room
}

// visibleFields are the fields the current type shows, in ↑↓ order.
func (f *ForwardForm) visibleFields() []int {
	if f.kind == forwardKindHTTP {
		return []int{forwardFieldType, forwardFieldName, forwardFieldTarget}
	}
	return []int{forwardFieldType, forwardFieldPort, forwardFieldTarget}
}

// TypeFocused reports whether ←→ has a value to cycle: on any other field they
// move the cursor, so the header greys the entry there (Rule 130).
func (f *ForwardForm) TypeFocused() bool { return f.focused == forwardFieldType }

// Named reports whether the form is on the HTTP type.
func (f *ForwardForm) Named() bool { return f.kind == forwardKindHTTP }

// updateFocus gives the keyboard to the focused field and takes it from the
// others, which is what makes exactly one cursor visible.
func (f *ForwardForm) updateFocus() {
	inputs := map[int]*textinput.Model{
		forwardFieldPort:   &f.portInput,
		forwardFieldName:   &f.nameInput,
		forwardFieldTarget: &f.targetInput,
	}
	for field, in := range inputs {
		if field == f.focused {
			in.Focus()
			continue
		}
		in.Blur()
	}
}

// move steps the focus through the visible fields and stops at the ends.
func (f *ForwardForm) move(delta int) {
	fields := f.visibleFields()
	at := 0
	for i, field := range fields {
		if field == f.focused {
			at = i
		}
	}
	f.focused = fields[min(max(at+delta, 0), len(fields)-1)]
	f.updateFocus()
}

// cycle changes the type. The focus is on the Type field when this runs, which
// is on every visible list, so it never has to be replaced.
func (f *ForwardForm) cycle(delta int) {
	f.kind = (f.kind + delta + len(forwardKinds)) % len(forwardKinds)
}

// Update handles the form's keys. ↑↓ move between fields, ←→ change the type
// while it is focused, enter confirms and esc cancels (Rule 135); tab does
// nothing here, it belongs to the tabs.
func (f *ForwardForm) Update(msg tea.Msg) (*ForwardForm, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}

	switch key.String() {
	case "up":
		f.move(-1)
		return f, nil
	case "down":
		f.move(1)
		return f, nil
	case "esc":
		return f, func() tea.Msg { return ForwardFormCancelMsg{} }
	case "enter":
		return f, f.submit()
	}

	switch f.focused {
	case forwardFieldType:
		switch key.String() {
		case "left":
			f.cycle(-1)
		case "right":
			f.cycle(1)
		}
		return f, nil
	case forwardFieldPort:
		var cmd tea.Cmd
		f.portInput, cmd = f.portInput.Update(msg)
		return f, cmd
	case forwardFieldName:
		var cmd tea.Cmd
		f.nameInput, cmd = f.nameInput.Update(msg)
		return f, cmd
	}
	var cmd tea.Cmd
	f.targetInput, cmd = f.targetInput.Update(msg)
	return f, cmd
}

// Problem is why submit would produce nothing, or "" when it would. The model
// asks before calling submit so it can say why nothing happened rather than
// swallowing the keypress (Rule 130).
func (f *ForwardForm) Problem() string {
	if f.kind == forwardKindHTTP {
		name := strings.ToLower(strings.TrimSpace(f.nameInput.Value()))
		if !strings.HasSuffix(name, forwardNameSuffix) || len(name) == len(forwardNameSuffix) {
			return reasonNameNeedsSuffix
		}
		return ""
	}
	if _, err := strconv.Atoi(strings.TrimSpace(f.portInput.Value())); err != nil {
		return reasonPortNotANumber
	}
	return ""
}

// submit turns the visible fields into a request, or nil when Problem says
// there is nothing to send.
func (f *ForwardForm) submit() tea.Cmd {
	if f.Problem() != "" {
		return nil
	}
	target := strings.TrimSpace(f.targetInput.Value())

	if f.kind == forwardKindHTTP {
		name := strings.TrimSpace(f.nameInput.Value())
		return func() tea.Msg { return ForwardFormSubmitMsg{Name: name, Target: target} }
	}
	port, _ := strconv.Atoi(strings.TrimSpace(f.portInput.Value()))
	return func() tea.Msg { return ForwardFormSubmitMsg{LocalPort: port, Target: target} }
}

// View renders the form in the viewport (Rule 112), opening on exactly one
// blank line (Rule 131) and carrying no inline help (Rule 134).
func (f *ForwardForm) View() string {
	lines := []string{theme.EmptyLineBg(f.width)}
	for i, field := range f.visibleFields() {
		if i > 0 {
			lines = append(lines, theme.EmptyLineBg(f.width))
		}
		lines = append(lines, f.renderVisible(field))
	}
	return strings.Join(lines, "\n")
}

func (f *ForwardForm) renderVisible(field int) string {
	switch field {
	case forwardFieldType:
		return f.renderCycleField("Type", forwardKinds[f.kind], field)
	case forwardFieldPort:
		return f.renderField("Local port", f.portInput.View(), field)
	case forwardFieldName:
		return f.renderField("Name", f.nameInput.View(), field)
	}
	return f.renderField("Target", f.targetInput.View(), field)
}

// renderField draws one single-line field: indicator, padded label, chevron,
// value (Rule 120). A blank line goes between fields, as the diagnostics form
// has: two bare rows read as one block.
func (f *ForwardForm) renderField(label, value string, field int) string {
	padded := label + strings.Repeat(" ", max(forwardLabelWidth-len([]rune(label)), 0))
	if f.focused == field {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+padded+" "+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+padded+" "+theme.IconChevronRight+" ") + value
}

// renderCycleField draws the closed-list field (Rule 132). The selection icon
// takes the place of two of the label's padding cells, so the chevron lands in
// the column every other field's does.
func (f *ForwardForm) renderCycleField(label, value string, field int) string {
	padded := label + strings.Repeat(" ", max(forwardLabelWidth-2-len([]rune(label)), 0))
	head := padded + " " + theme.IconSelect + " " + theme.IconChevronRight + " "
	if f.focused == field {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+head) + theme.Bg(value)
	}
	return theme.Bg("  "+head) + theme.Bg(value)
}
