package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// OptionConfirmModal is a confirmation carrying a checkbox: "do it, and do
// it this particular way".
//
// It used to serve deletion alone, hence its former name. §3.26 gives it a
// second use — purging the cache before a full scan — and it's the same
// need: two actions distinguished only by a modifier, with nothing in the
// form saying which one was destructive. Here the destructive variant is a
// deliberate gesture, under the eyes of whoever triggers it.
type OptionConfirmModal struct {
	title   string
	message string
	focused int // 0 = checkbox, 1 = Yes, 2 = No
	// option is the checkbox, and its meaning belongs to the caller:
	// immediate deletion here, cache purge there.
	option        bool
	optionLabel   string
	optionWarning string // shown when the box is checked; empty = nothing to say
	locked        bool   // If true, the checkbox is neither editable nor focusable
	hidden        bool   // If true, the checkbox isn't even rendered: the option has no meaning here
	width         int
	height        int
}

// immediateDeletionLabel is the deletion option, and the reason this modal
// exists.
const (
	immediateDeletionLabel   = "Immediate deletion (no grace period)"
	immediateDeletionWarning = "This action is irreversible!"
)

// NewOptionConfirmModal creates a confirmation with a named checkbox.
// No warning: an option that isn't destructive doesn't deserve one.
func NewOptionConfirmModal(title, message, optionLabel string) *OptionConfirmModal {
	return &OptionConfirmModal{
		title:       title,
		message:     message,
		focused:     2, // Defaults to "No"
		optionLabel: optionLabel,
	}
}

// NewDeleteConfirmModal creates a new deletion confirmation modal.
// offerImmediate comes from forge.Shape.PermanentDelete: on a backend that
// always deletes immediately (GitHub), there is no grace period to bypass,
// so the checkbox has nothing to signify and isn't rendered at all — not
// greyed out, absent, as that field of the Shape says.
func NewDeleteConfirmModal(title, message string, offerImmediate bool) *OptionConfirmModal {
	m := NewOptionConfirmModal(title, message, immediateDeletionLabel)
	if !offerImmediate {
		m.hidden = true
		return m
	}
	m.optionWarning = immediateDeletionWarning
	return m
}

// NewDeleteConfirmModalLocked creates a modal with the "immediate deletion" checkbox pre-checked and non-editable.
// Use it when the project is already marked for deletion — only permanent deletion is possible.
func NewDeleteConfirmModalLocked(title, message string) *OptionConfirmModal {
	m := NewDeleteConfirmModal(title, message, true)
	m.option = true
	m.locked = true
	return m
}

// minFocus returns the first element reachable by keyboard. The checkbox is
// excluded when it is locked or absent: a focusable control that ignores
// every keystroke, or that doesn't exist, is more confusing than a control
// that is simply out of reach.
func (m *OptionConfirmModal) minFocus() int {
	if m.locked || m.hidden {
		return 1
	}
	return 0
}

// OptionConfirmModalYesMsg is sent when the user confirms. Option carries
// the checkbox's state, whose meaning belongs to whoever opened the modal.
type OptionConfirmModalYesMsg struct {
	Option bool
}

// OptionConfirmModalNoMsg is sent when the user cancels
type OptionConfirmModalNoMsg struct{}

// Update updates the modal
func (m *OptionConfirmModal) Update(msg tea.Msg) (*OptionConfirmModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		// Rule 135: ↑/↓ are the only field navigation. They cycle rather than
		// clamp, so every control stays reachable in one direction — that is
		// what tab used to provide before it was removed.
		case "up":
			m.focused = m.cycleFocus(-1)
			return m, nil

		case "down":
			m.focused = m.cycleFocus(1)
			return m, nil

		case "left":
			// If on the buttons, move to Yes
			if m.focused == 2 {
				m.focused = 1
			}
			return m, nil

		case "right":
			// If on the buttons, move to No
			if m.focused == 1 {
				m.focused = 2
			}
			return m, nil

		case " ":
			// Toggle the checkbox if it has focus, otherwise confirm the selection
			if m.focused == 0 {
				m.toggleCheckbox()
				return m, nil
			}
			return m.handleConfirm()

		case "enter":
			return m.handleConfirm()

		case "y", "Y":
			// Shortcut for Yes
			return m, func() tea.Msg {
				return OptionConfirmModalYesMsg{Option: m.option}
			}

		case "n", "N", "esc":
			// Shortcut for No
			return m, func() tea.Msg {
				return OptionConfirmModalNoMsg{}
			}
		}
	}

	return m, nil
}

// cycleFocus rotates focus in the given direction, skipping the elements
// excluded by minFocus().
func (m *OptionConfirmModal) cycleFocus(step int) int {
	min := m.minFocus()
	span := 3 - min
	return min + ((m.focused-min+step)%span+span)%span
}

// toggleCheckbox flips the "immediate deletion" checkbox, unless it is
// locked or absent.
func (m *OptionConfirmModal) toggleCheckbox() {
	if m.locked || m.hidden {
		return
	}
	m.option = !m.option
}

// handleConfirm handles confirmation based on the selected element
func (m *OptionConfirmModal) handleConfirm() (*OptionConfirmModal, tea.Cmd) {
	switch m.focused {
	case 0:
		// Toggle checkbox
		m.toggleCheckbox()
		return m, nil
	case 1:
		// Yes
		return m, func() tea.Msg {
			return OptionConfirmModalYesMsg{Option: m.option}
		}
	case 2:
		// No
		return m, func() tea.Msg {
			return OptionConfirmModalNoMsg{}
		}
	}
	return m, nil
}

// View renders the modal
func (m *OptionConfirmModal) View() string {
	var b strings.Builder

	// Title
	b.WriteString(theme.TitleStyle.Render(m.title))
	b.WriteString("\n\n")

	// Message
	b.WriteString(m.message)
	b.WriteString("\n\n")

	// Checkbox for immediate deletion — absent when the option has no
	// meaning on this backend (m.hidden), not merely greyed out.
	if !m.hidden {
		checkboxStyle := lipgloss.NewStyle().Background(theme.ColorBackground)
		switch {
		case m.locked:
			// Locked: dimmed, to signal that it isn't actionable.
			checkboxStyle = checkboxStyle.Foreground(theme.ColorDim)
		case m.focused == 0:
			checkboxStyle = checkboxStyle.Bold(true).Foreground(theme.ColorHighlight)
		}

		checkbox := theme.IconCheckbox
		if m.option {
			checkbox = theme.IconChecked
		}

		indicator := "  "
		if m.focused == 0 {
			indicator = theme.IconCircleSmall + " "
		}

		checkboxLabel := " " + m.optionLabel
		checkboxLine := checkboxStyle.Render(indicator + checkbox + checkboxLabel)
		b.WriteString(checkboxLine)

		// Warning when the box is checked, if the caller has one to give.
		if m.option && m.optionWarning != "" {
			b.WriteString("\n")
			warningStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Italic(true)
			b.WriteString(warningStyle.Render("  " + theme.IconWarning + " " + m.optionWarning))
		}
		b.WriteString("\n\n")
	}

	// Buttons
	yes := theme.RenderButton("Yes", m.focused == 1, "danger")
	no := theme.RenderButton("No", m.focused == 2, "primary")

	buttons := yes + theme.Bg("  ") + no
	b.WriteString(buttons)
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
