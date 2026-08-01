package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// ConfirmModal est une boîte de dialogue de confirmation
type ConfirmModal struct {
	title   string
	message string
	focused bool // true = Yes, false = No
	width   int
	height  int
}

// NewConfirmModal crée une nouvelle modal de confirmation
func NewConfirmModal(title, message string) *ConfirmModal {
	return &ConfirmModal{
		title:   title,
		message: message,
		focused: false, // Default sur "No" pour éviter les suppressions accidentelles
	}
}

// ConfirmModalYesMsg est envoyé quand l'utilisateur confirme
type ConfirmModalYesMsg struct{}

// ConfirmModalNoMsg est envoyé quand l'utilisateur annule
type ConfirmModalNoMsg struct{}

// Update met à jour la modal
func (m *ConfirmModal) Update(msg tea.Msg) (*ConfirmModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "left", "right":
			// Toggle entre Yes et No (Rule 135 — Tab exclusivement pour les onglets)
			m.focused = !m.focused
			return m, nil

		case "enter", " ":
			// Confirmer la sélection
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
			// Raccourci pour Yes
			return m, func() tea.Msg {
				return ConfirmModalYesMsg{}
			}

		case "n", "N", "esc":
			// Raccourci pour No
			return m, func() tea.Msg {
				return ConfirmModalNoMsg{}
			}
		}
	}

	return m, nil
}

// View affiche la modal
func (m *ConfirmModal) View() string {
	var b strings.Builder

	// Titre
	b.WriteString(theme.TitleStyle.Render(m.title))
	b.WriteString("\n\n")

	// Message
	b.WriteString(m.message)
	b.WriteString("\n\n")

	// Boutons
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
