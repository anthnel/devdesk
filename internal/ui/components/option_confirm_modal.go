package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// OptionConfirmModal est une confirmation portant une case à cocher : « fais-le,
// et fais-le de cette façon-là ».
//
// Elle servait la seule suppression, d'où son ancien nom. §3.26 lui donne un
// second emploi — la purge du cache avant un scan complet — et c'est le même
// besoin : deux actions que seul un modificateur distinguait, dont rien dans la
// forme ne disait laquelle était destructrice. Ici la variante destructrice est
// un geste délibéré, sous les yeux de celui qui la déclenche.
type OptionConfirmModal struct {
	title   string
	message string
	focused int // 0 = checkbox, 1 = Yes, 2 = No
	// option est la case, et son sens appartient à l'appelant : suppression
	// immédiate ici, purge du cache là.
	option        bool
	optionLabel   string
	optionWarning string // affiché quand la case est cochée ; vide = rien à dire
	locked        bool   // Si true, la checkbox n'est ni modifiable ni focusable
	hidden        bool   // Si true, la checkbox n'est même pas rendue : l'option n'a pas de sens ici
	width         int
	height        int
}

// immediateDeletionLabel est l'option de la suppression, et la raison pour
// laquelle cette modale existe.
const (
	immediateDeletionLabel   = "Immediate deletion (no grace period)"
	immediateDeletionWarning = "This action is irreversible!"
)

// NewOptionConfirmModal crée une confirmation avec une case à cocher nommée.
// Sans warning : une option qui n'est pas destructrice n'en mérite pas.
func NewOptionConfirmModal(title, message, optionLabel string) *OptionConfirmModal {
	return &OptionConfirmModal{
		title:       title,
		message:     message,
		focused:     2, // Default sur "No"
		optionLabel: optionLabel,
	}
}

// NewDeleteConfirmModal crée une nouvelle modal de confirmation de suppression.
// offerImmediate vient de forge.Shape.PermanentDelete : sur un backend qui
// supprime toujours tout de suite (GitHub), il n'y a pas de délai de grâce à
// contourner, donc la case n'a rien à signifier et n'est pas rendue du tout —
// pas grisée, absente, comme le dit ce champ du Shape.
func NewDeleteConfirmModal(title, message string, offerImmediate bool) *OptionConfirmModal {
	m := NewOptionConfirmModal(title, message, immediateDeletionLabel)
	if !offerImmediate {
		m.hidden = true
		return m
	}
	m.optionWarning = immediateDeletionWarning
	return m
}

// NewDeleteConfirmModalLocked crée une modal avec la case "immediate deletion" pré-cochée et non modifiable.
// À utiliser quand le projet est déjà marqué pour suppression — seule la suppression permanente est possible.
func NewDeleteConfirmModalLocked(title, message string) *OptionConfirmModal {
	m := NewDeleteConfirmModal(title, message, true)
	m.option = true
	m.locked = true
	return m
}

// minFocus retourne le premier élément atteignable au clavier. La checkbox est
// exclue quand elle est verrouillée ou absente : un contrôle focusable qui
// ignore toute touche, ou qui n'existe pas, est plus déroutant qu'un contrôle
// simplement hors de portée.
func (m *OptionConfirmModal) minFocus() int {
	if m.locked || m.hidden {
		return 1
	}
	return 0
}

// OptionConfirmModalYesMsg est envoyé quand l'utilisateur confirme. Option porte
// l'état de la case, dont le sens appartient à celui qui a ouvert la modale.
type OptionConfirmModalYesMsg struct {
	Option bool
}

// OptionConfirmModalNoMsg est envoyé quand l'utilisateur annule
type OptionConfirmModalNoMsg struct{}

// Update met à jour la modal
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
			// Si sur les boutons, aller vers Yes
			if m.focused == 2 {
				m.focused = 1
			}
			return m, nil

		case "right":
			// Si sur les boutons, aller vers No
			if m.focused == 1 {
				m.focused = 2
			}
			return m, nil

		case " ":
			// Toggle checkbox si focus dessus, sinon confirmer la sélection
			if m.focused == 0 {
				m.toggleCheckbox()
				return m, nil
			}
			return m.handleConfirm()

		case "enter":
			return m.handleConfirm()

		case "y", "Y":
			// Raccourci pour Yes
			return m, func() tea.Msg {
				return OptionConfirmModalYesMsg{Option: m.option}
			}

		case "n", "N", "esc":
			// Raccourci pour No
			return m, func() tea.Msg {
				return OptionConfirmModalNoMsg{}
			}
		}
	}

	return m, nil
}

// cycleFocus fait tourner le focus dans le sens donné, en sautant les éléments
// exclus par minFocus().
func (m *OptionConfirmModal) cycleFocus(step int) int {
	min := m.minFocus()
	span := 3 - min
	return min + ((m.focused-min+step)%span+span)%span
}

// toggleCheckbox inverse la case "immediate deletion", sauf si elle est
// verrouillée ou absente.
func (m *OptionConfirmModal) toggleCheckbox() {
	if m.locked || m.hidden {
		return
	}
	m.option = !m.option
}

// handleConfirm gère la confirmation selon l'élément sélectionné
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

// View affiche la modal
func (m *OptionConfirmModal) View() string {
	var b strings.Builder

	// Titre
	b.WriteString(theme.TitleStyle.Render(m.title))
	b.WriteString("\n\n")

	// Message
	b.WriteString(m.message)
	b.WriteString("\n\n")

	// Checkbox pour suppression immédiate — absente quand l'option n'a pas de
	// sens sur ce backend (m.hidden), pas seulement grisée.
	if !m.hidden {
		checkboxStyle := lipgloss.NewStyle().Background(theme.ColorBackground)
		switch {
		case m.locked:
			// Verrouillée : atténuée, pour signaler qu'elle n'est pas actionnable.
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

		// Warning quand la case est cochée, si l'appelant en a un à donner.
		if m.option && m.optionWarning != "" {
			b.WriteString("\n")
			warningStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Italic(true)
			b.WriteString(warningStyle.Render("  " + theme.IconWarning + " " + m.optionWarning))
		}
		b.WriteString("\n\n")
	}

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
