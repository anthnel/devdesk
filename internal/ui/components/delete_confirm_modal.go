package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DeleteConfirmModal est une boîte de dialogue de confirmation pour suppression
// avec option de suppression immédiate
type DeleteConfirmModal struct {
	title             string
	message           string
	focused           int  // 0 = checkbox, 1 = Yes, 2 = No
	permanentlyRemove bool // Si true, suppression immédiate sans période de grâce
	width             int
	height            int
}

// NewDeleteConfirmModal crée une nouvelle modal de confirmation de suppression
func NewDeleteConfirmModal(title, message string) *DeleteConfirmModal {
	return &DeleteConfirmModal{
		title:             title,
		message:           message,
		focused:           2, // Default sur "No" pour éviter les suppressions accidentelles
		permanentlyRemove: false,
	}
}

// NewDeleteConfirmModalPermanent crée une modal avec la case "immediate deletion" pré-cochée et non modifiable.
// À utiliser quand le projet est déjà marqué pour suppression — seule la suppression permanente est possible.
func NewDeleteConfirmModalPermanent(title, message string) *DeleteConfirmModal {
	return &DeleteConfirmModal{
		title:             title,
		message:           message,
		focused:           2, // Default sur "No"
		permanentlyRemove: true,
	}
}

// DeleteConfirmModalYesMsg est envoyé quand l'utilisateur confirme la suppression
type DeleteConfirmModalYesMsg struct {
	PermanentlyRemove bool
}

// DeleteConfirmModalNoMsg est envoyé quand l'utilisateur annule
type DeleteConfirmModalNoMsg struct{}

// Update met à jour la modal
func (m *DeleteConfirmModal) Update(msg tea.Msg) (*DeleteConfirmModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			// Navigate up
			if m.focused > 0 {
				m.focused--
			}
			return m, nil

		case "down", "j":
			// Navigate down
			if m.focused < 2 {
				m.focused++
			}
			return m, nil

		case "left", "h":
			// Si sur les boutons, aller vers Yes
			if m.focused == 2 {
				m.focused = 1
			}
			return m, nil

		case "right", "l":
			// Si sur les boutons, aller vers No
			if m.focused == 1 {
				m.focused = 2
			}
			return m, nil

		case "tab":
			// Cycle through: checkbox -> Yes -> No -> checkbox
			m.focused = (m.focused + 1) % 3
			return m, nil

		case "shift+tab":
			// Cycle backwards
			m.focused = (m.focused + 2) % 3
			return m, nil

		case " ":
			// Toggle checkbox si focus dessus, sinon confirmer la sélection
			if m.focused == 0 {
				m.permanentlyRemove = !m.permanentlyRemove
				return m, nil
			}
			return m.handleConfirm()

		case "enter":
			return m.handleConfirm()

		case "y", "Y":
			// Raccourci pour Yes
			return m, func() tea.Msg {
				return DeleteConfirmModalYesMsg{PermanentlyRemove: m.permanentlyRemove}
			}

		case "n", "N", "esc":
			// Raccourci pour No
			return m, func() tea.Msg {
				return DeleteConfirmModalNoMsg{}
			}
		}
	}

	return m, nil
}

// handleConfirm gère la confirmation selon l'élément sélectionné
func (m *DeleteConfirmModal) handleConfirm() (*DeleteConfirmModal, tea.Cmd) {
	switch m.focused {
	case 0:
		// Toggle checkbox
		m.permanentlyRemove = !m.permanentlyRemove
		return m, nil
	case 1:
		// Yes
		return m, func() tea.Msg {
			return DeleteConfirmModalYesMsg{PermanentlyRemove: m.permanentlyRemove}
		}
	case 2:
		// No
		return m, func() tea.Msg {
			return DeleteConfirmModalNoMsg{}
		}
	}
	return m, nil
}

// View affiche la modal
func (m *DeleteConfirmModal) View() string {
	var b strings.Builder

	// Titre
	b.WriteString(theme.TitleStyle.Render(m.title))
	b.WriteString("\n\n")

	// Message
	b.WriteString(m.message)
	b.WriteString("\n\n")

	// Checkbox pour suppression immédiate
	checkboxStyle := lipgloss.NewStyle().Background(theme.ColorBackground)
	if m.focused == 0 {
		checkboxStyle = checkboxStyle.Bold(true).Foreground(theme.ColorHighlight)
	}

	checkbox := theme.IconCheckbox
	if m.permanentlyRemove {
		checkbox = theme.IconChecked
	}

	indicator := "  "
	if m.focused == 0 {
		indicator = theme.IconCircleSmall + " "
	}

	checkboxLabel := " Immediate deletion (no grace period)"
	checkboxLine := checkboxStyle.Render(indicator + checkbox + checkboxLabel)
	b.WriteString(checkboxLine)

	// Warning si suppression immédiate activée
	if m.permanentlyRemove {
		b.WriteString("\n")
		warningStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Italic(true)
		b.WriteString(warningStyle.Render("  " + theme.IconWarning + " This action is irreversible!"))
	}
	b.WriteString("\n\n")

	// Boutons
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
