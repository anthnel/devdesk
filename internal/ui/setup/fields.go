package setup

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders the wizard: the form, or whichever overlay is active on top
// of it (Rule 112 — a confirmation is a centered modal, never the viewport).
//
// Unlike every themed view, nothing here paints a background: the wizard
// runs standalone, before any theme is loaded, and is meant to sit on
// whatever terminal background the user already has (see styles.go).
func (m Model) View() string {
	switch m.stage {
	case stageConfirmOverwrite, stageConfirmSetCurrent:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.confirmModal.View())
	case stageDone:
		return m.renderDone()
	default:
		return m.renderForm()
	}
}

func (m Model) renderForm() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("DevDesk setup — new context"))
	b.WriteString("\n\n")

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

	b.WriteString("  " + renderSubmitButton(m.focusedField == fieldSubmit))
	b.WriteString("\n\n")

	b.WriteString(m.renderFooterLine())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  [↑↓] navigate  [←→] change  [enter] confirm  [esc] quit"))

	return b.String()
}

func (m Model) renderDone() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Context ready"))
	b.WriteString("\n\n")
	b.WriteString("Wrote " + m.savedPath)
	b.WriteString("\n")
	if m.currentContextWarning != "" {
		b.WriteString(errorStyle.Render(m.currentContextWarning))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(`Run "dk" to start.`)
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("  [enter] exit"))

	return b.String()
}

// renderFooterLine reads components.FooterMessage's state directly instead of
// calling its View — that method paints a full-width, background-filled,
// centered line (Rule 128), which is exactly what this screen opts out of.
func (m Model) renderFooterLine() string {
	if m.footer.IsSet() {
		return footerLevelStyle(m.footer.Level()).Render(m.footer.Text())
	}
	if m.pending != "" {
		return m.spinner.View() + " " + dimStyle.Render(m.pending)
	}
	return ""
}

// renderTextField renders a single-line label over a textinput's view,
// following Rule 120: the focus indicator and chevron separator, no ":".
func renderTextField(label, valueView string, focused bool) string {
	var labelStr string
	if focused {
		labelStr = focusStyle.Render(theme.IconCircleSmall + " " + label + " " + theme.IconChevronRight)
	} else {
		labelStr = "  " + label + " " + theme.IconChevronRight
	}
	return labelStr + "\n  " + valueView
}

// renderCycleField renders a closed-set field with the ←→ cycle pattern
// (Rule 132), the same layout CreationForm's resource-type field uses.
func renderCycleField(label, value string, focused bool) string {
	selectLabel := label + " " + theme.IconSelect + " "
	renderedValue := valueStyle.Render(value)
	if focused {
		return focusStyle.Render(theme.IconCircleSmall+" "+selectLabel+theme.IconChevronRight+" ") + renderedValue
	}
	return "  " + selectLabel + theme.IconChevronRight + " " + renderedValue
}

// renderSubmitButton renders the submit action as bracketed text rather than
// through theme.RenderButton, which paints a solid background box — the one
// thing this screen does not want.
func renderSubmitButton(focused bool) string {
	label := "[ Create context ]"
	if focused {
		return focusStyle.Render(label)
	}
	return dimStyle.Render(label)
}
