package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ChoiceModal demande « laquelle de ces actions, ou aucune ».
//
// ConfirmModal pose une question fermée et OptionConfirmModal une question
// fermée assortie d'une variante ; ni l'une ni l'autre ne sait proposer deux
// actions distinctes. §3.26 en a besoin pour `K` : arrêter et redémarrer sont
// deux gestes, pas un geste et son modificateur — un redémarrage est un arrêt
// suivi d'un démarrage, et présenter le second comme une case à cocher du
// premier ferait d'un choix exclusif une bascule (Rule 132).
//
// Annuler est le choix par défaut, comme « No » ailleurs (Rule 104) : ces deux
// actions coupent les connexions en cours.
type ChoiceModal struct {
	title   string
	message string
	choices []string
	// focused indexe choices, et vaut len(choices) sur Cancel — qui est donc
	// toujours le dernier bouton, sans avoir à être dans la liste.
	focused int
	width   int
	height  int
}

// NewChoiceModal crée une modale à N actions plus Cancel, focus sur Cancel.
func NewChoiceModal(title, message string, choices ...string) *ChoiceModal {
	return &ChoiceModal{
		title:   title,
		message: message,
		choices: choices,
		focused: len(choices), // Cancel
	}
}

// ChoiceModalPickedMsg est envoyé quand l'utilisateur choisit une action.
// Index et Label désignent la même chose : l'index pour router, le libellé pour
// journaliser sans avoir à retraduire.
type ChoiceModalPickedMsg struct {
	Index int
	Label string
}

// ChoiceModalCancelledMsg est envoyé quand l'utilisateur renonce.
type ChoiceModalCancelledMsg struct{}

// Update met à jour la modal.
func (m *ChoiceModal) Update(msg tea.Msg) (*ChoiceModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		// ←/→ entre boutons (Rule 135 — Tab est réservé aux onglets). Le
		// parcours est borné plutôt que cyclique : Cancel doit rester le bout
		// de la course, sinon une pression de trop ramène sur une action.
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

// pick rend la commande correspondant au bouton focusé.
func (m *ChoiceModal) pick() tea.Cmd {
	if m.focused >= len(m.choices) {
		return cancelChoice
	}
	picked := ChoiceModalPickedMsg{Index: m.focused, Label: m.choices[m.focused]}
	return func() tea.Msg { return picked }
}

func cancelChoice() tea.Msg { return ChoiceModalCancelledMsg{} }

// View affiche la modal.
func (m *ChoiceModal) View() string {
	var b strings.Builder

	b.WriteString(theme.TitleStyle.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(m.message)
	b.WriteString("\n\n")

	// Les actions sont en "danger" et Cancel en "primary", la même répartition
	// que Yes/No : c'est le bouton sûr qui porte la couleur calme.
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
