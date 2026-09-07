package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ChoiceModal asks "which of these actions, or none".
//
// ConfirmModal poses a closed question and OptionConfirmModal a closed
// question paired with a variant; neither one can offer two distinct
// actions. §3.26 needs this for `K`: stop and restart are two gestures, not
// one gesture and its modifier — a restart is a stop followed by a start,
// and presenting the second as a checkbox of the first would turn an
// exclusive choice into a toggle (Rule 132).
//
// Cancel is the default choice, like "No" elsewhere (Rule 104): both of
// these actions cut ongoing connections.
type ChoiceModal struct {
	title   string
	message string
	choices []string
	// focused indexes choices, and equals len(choices) on Cancel — which is
	// therefore always the last button, without needing to be in the list.
	focused int
	width   int
	height  int
}

// NewChoiceModal creates a modal with N actions plus Cancel, focused on Cancel.
func NewChoiceModal(title, message string, choices ...string) *ChoiceModal {
	return &ChoiceModal{
		title:   title,
		message: message,
		choices: choices,
		focused: len(choices), // Cancel
	}
}

// ChoiceModalPickedMsg is sent when the user picks an action.
// Index and Label refer to the same thing: the index for routing, the label
// for logging without having to re-translate it.
type ChoiceModalPickedMsg struct {
	Index int
	Label string
}

// ChoiceModalCancelledMsg is sent when the user gives up.
type ChoiceModalCancelledMsg struct{}

// Update updates the modal.
func (m *ChoiceModal) Update(msg tea.Msg) (*ChoiceModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		// ←/→ between buttons (Rule 135 — Tab is reserved for tabs). The
		// traversal is bounded rather than cyclic: Cancel must stay the end
		// of the run, otherwise one extra press would loop back onto an action.
		case "left":
			m.focused = max(m.focused-1, 0)
			return m, nil

		case "right":
			m.focused = min(m.focused+1, len(m.choices))
			return m, nil

		case "enter", " ":
			return m, m.pick()

		case "esc":
			return m, cancelChoice
		}
	}

	return m, nil
}

// pick returns the command matching the focused button.
func (m *ChoiceModal) pick() tea.Cmd {
	if m.focused >= len(m.choices) {
		return cancelChoice
	}
	picked := ChoiceModalPickedMsg{Index: m.focused, Label: m.choices[m.focused]}
	return func() tea.Msg { return picked }
}

func cancelChoice() tea.Msg { return ChoiceModalCancelledMsg{} }

// View renders the modal.
func (m *ChoiceModal) View() string {
	var b strings.Builder

	b.WriteString(theme.TitleStyle.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(m.message)
	b.WriteString("\n\n")

	// The actions are "danger" and Cancel is "primary", the same split as
	// Yes/No: it's the safe button that carries the calm color.
	buttons := make([]string, 0, len(m.choices)+1)
	for i, choice := range m.choices {
		buttons = append(buttons, theme.RenderButton(choice, m.focused == i, "danger"))
	}
	buttons = append(buttons, theme.RenderButton("Cancel", m.focused == len(m.choices), "primary"))

	b.WriteString(strings.Join(buttons, theme.Bg("  ")))
	b.WriteString("\n\n")

	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorText).
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorError).
		BorderBackground(theme.ColorBackground).
		Padding(1, 2).
		Render(b.String())
}
