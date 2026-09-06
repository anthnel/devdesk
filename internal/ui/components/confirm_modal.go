package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ConfirmModal is a confirmation dialog box
type ConfirmModal struct {
	title   string
	message string
	focused bool // true = Yes, false = No
	width   int
	height  int
}

// NewConfirmModal creates a new confirmation modal
func NewConfirmModal(title, message string) *ConfirmModal {
	return &ConfirmModal{
		title:   title,
		message: message,
		focused: false, // Defaults to "No" to avoid accidental deletions
	}
}

// ConfirmModalYesMsg is sent when the user confirms
type ConfirmModalYesMsg struct{}

// ConfirmModalNoMsg is sent when the user cancels
type ConfirmModalNoMsg struct{}

// Update updates the modal
func (m *ConfirmModal) Update(msg tea.Msg) (*ConfirmModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "left", "right":
			// Toggle between Yes and No (Rule 135 — Tab exclusively for tabs)
			m.focused = !m.focused
			return m, nil

		case "enter", " ":
			// Confirm the selection
			if m.focused {
				return m, func() tea.Msg {
					return ConfirmModalYesMsg{}
				}
			} else {
				return m, func() tea.Msg {
					return ConfirmModalNoMsg{}
				}
			}

		case "y", "Y":
			// Shortcut for Yes
			return m, func() tea.Msg {
				return ConfirmModalYesMsg{}
			}

		case "n", "N", "esc":
			// Shortcut for No
			return m, func() tea.Msg {
				return ConfirmModalNoMsg{}
			}
		}
	}

	return m, nil
}

// View renders the modal
func (m *ConfirmModal) View() string {
	var b strings.Builder

	// Title
	b.WriteString(theme.TitleStyle.Render(m.title))
	b.WriteString("\n\n")

	// Message
	b.WriteString(m.message)
	b.WriteString("\n\n")

	// Buttons
	yes := theme.RenderButton("Yes", m.focused, "danger")
	no := theme.RenderButton("No", !m.focused, "primary")

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
