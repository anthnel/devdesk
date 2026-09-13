package setup

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders the wizard: the form, or whichever overlay is active on top
// of it (Rule 112 — a confirmation is a centered modal, never the viewport).
func (m Model) View() string {
	switch m.stage {
	case stageConfirmOverwrite, stageConfirmSetCurrent:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground))
	case stageDone:
		return m.renderDone()
	default:
		return m.renderForm()
	}
}

func (m Model) renderForm() string {
	var b strings.Builder

	b.WriteString(theme.TitleStyle.Render("DevDesk setup — new context"))
	b.WriteString("\n")
	b.WriteString(theme.EmptyLineBg(m.width) + "\n")

	v := forge.VocabularyFor(config.ForgeTypes()[m.forgeTypeIdx])

	fields := []string{
		renderTextField("Context name", m.contextNameInput.View(), m.focusedField == fieldContextName),
		renderCycleField("Secret backend", secretBackendOptions[m.secretBackendIdx], m.focusedField == fieldSecretBackend),
		renderCycleField("Container engine", config.ContainerEngines()[m.containerEngineIdx], m.focusedField == fieldContainerEngine),
		renderCycleField("Theme", m.themeOptions[m.themeIdx], m.focusedField == fieldTheme),
		renderCycleField("Forge", v.Name, m.focusedField == fieldForgeType),
		renderTextField(v.Name+" URL", m.forgeURLInput.View(), m.focusedField == fieldForgeURL),
		renderTextField("Default "+v.Namespace, m.forgeNamespaceInput.View(), m.focusedField == fieldForgeNamespace),
		renderCycleField("Visibility", m.visibilityValue, m.focusedField == fieldForgeVisibility),
		renderCycleField("Clone method", cloneMethodOptions[m.cloneMethodIdx], m.focusedField == fieldForgeCloneMethod),
		renderTextField(v.TokenLabel, m.forgeTokenInput.View(), m.focusedField == fieldForgeToken),
	}
	for _, f := range fields {
		b.WriteString(f)
		b.WriteString("\n\n")
	}

	b.WriteString("  " + theme.RenderButton("Create context", m.focusedField == fieldSubmit, "primary"))
	b.WriteString("\n\n")

	b.WriteString(m.footer.View(m.width, m.status()))
	b.WriteString("\n")
	b.WriteString(theme.HelpStyle.Render("  [↑↓] navigate  [←→] change  [enter] confirm  [esc] quit"))

	return b.String()
}

func (m Model) renderDone() string {
	var b strings.Builder

	b.WriteString(theme.TitleStyle.Render("Context ready"))
	b.WriteString("\n\n")
	b.WriteString(theme.Bg("Wrote " + m.savedPath))
	b.WriteString("\n")
	if m.currentContextWarning != "" {
		b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError).
			Render(m.currentContextWarning))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(theme.Bg(`Run "dk" to start.`))
	b.WriteString("\n\n")
	b.WriteString(theme.HelpStyle.Render("  [enter] exit"))

	return b.String()
}

func (m Model) status() components.Status {
	if m.pending != "" {
		return components.Status{Text: m.pending, Spinner: true}
	}
	return components.Status{}
}

// renderTextField renders a single-line label over a textinput's view,
// following Rule 120: the focus indicator and chevron separator, no ":".
func renderTextField(label, valueView string, focused bool) string {
	var labelStr string
	if focused {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall + " " + label + " " + theme.IconChevronRight)
	} else {
		labelStr = theme.Bg("  " + label + " " + theme.IconChevronRight)
	}
	return labelStr + "\n" + theme.Bg("  ") + valueView
}

// renderCycleField renders a closed-set field with the ←→ cycle pattern
// (Rule 132), the same layout CreationForm's resource-type field uses.
func renderCycleField(label, value string, focused bool) string {
	selectLabel := label + " " + theme.IconSelect + " "
	valueView := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(value)
	if focused {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+selectLabel+theme.IconChevronRight+" ") + valueView
	}
	return theme.Bg("  "+selectLabel+theme.IconChevronRight+" ") + valueView
}
