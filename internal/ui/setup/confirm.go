package setup

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// confirmPrompt is a small stand-in for components.ConfirmModal: the same
// keys and the same Yes/No messages (so model.go's handling of them is
// unchanged), but rendered without components.ConfirmModal's background and
// border-background — that shared box is what still showed the wrong
// color behind the wizard's two confirmations after every other screen had
// already stopped painting one. It is not modified in place because it is
// shared by the rest of the app, where Rule 115 requires that background.
type confirmPrompt struct {
	title   string
	message string
	yes     bool // false = No, the safe default (Rule 104)
}

func newConfirmPrompt(title, message string) *confirmPrompt {
	return &confirmPrompt{title: title, message: message}
}

// Update mirrors components.ConfirmModal's key handling exactly, so the two
// confirmations in this wizard behave like every other one in the app.
func (c *confirmPrompt) Update(msg tea.Msg) (*confirmPrompt, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	switch keyMsg.String() {
	case "left", "right":
		c.yes = !c.yes
		return c, nil
	case "enter", " ":
		return c, confirmResultCmd(c.yes)
	case "y", "Y":
		return c, confirmResultCmd(true)
	case "n", "N", "esc":
		return c, confirmResultCmd(false)
	}
	return c, nil
}

func confirmResultCmd(yes bool) tea.Cmd {
	return func() tea.Msg {
		if yes {
			return components.ConfirmModalYesMsg{}
		}
		return components.ConfirmModalNoMsg{}
	}
}

func (c *confirmPrompt) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(c.title))
	b.WriteString("\n\n")
	b.WriteString(c.message)
	b.WriteString("\n\n")
	b.WriteString(renderConfirmButton("Yes", c.yes) + "  " + renderConfirmButton("No", !c.yes))

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorHighlight).
		Padding(1, 2).
		Render(b.String())
}

func renderConfirmButton(label string, focused bool) string {
	if focused {
		return focusStyle.Render("[ " + label + " ]")
	}
	return dimStyle.Render("[ " + label + " ]")
}
