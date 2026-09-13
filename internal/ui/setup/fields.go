package setup

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// bodyMaxWidth caps how wide an explanatory paragraph wraps, so a full-width
// terminal doesn't stretch a sentence edge to edge.
const bodyMaxWidth = 72

// View renders the wizard: the current question, or whichever overlay is
// active on top of it (Rule 112 — a confirmation is a centered modal, never
// the viewport).
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
		return m.renderStep()
	}
}

// renderStep shows exactly one question — a title, the context to answer it
// with, and its control — rather than every field at once, so each choice
// comes with an explanation instead of a bare label (per the user's request:
// one question at a time, not a single long form).
func (m Model) renderStep() string {
	var b strings.Builder

	b.WriteString(dimStyle.Render(fmt.Sprintf("Step %d of %d", m.step+1, stepCount)))
	b.WriteString("\n\n")

	title, body, control := m.currentQuestion()

	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")
	if body != "" {
		b.WriteString(bodyStyle(m.width).Render(body))
		b.WriteString("\n\n")
	}
	b.WriteString(control)
	b.WriteString("\n\n")

	b.WriteString(m.renderFooterLine())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(m.helpLine()))

	return b.String()
}

// currentQuestion returns the active step's title, explanatory body, and
// rendered control. One function rather than one per step: every step needs
// the same three things, and the forge vocabulary the second half of the
// wizard reads from is resolved once here.
func (m Model) currentQuestion() (title, body, control string) {
	v := forge.VocabularyFor(config.ForgeTypes()[m.forgeTypeIdx])

	switch m.step {
	case stepFontCheck:
		return m.fontCheckQuestion()

	case stepContextName:
		return "Context name",
			"A context is a complete, independent configuration — its own forge, " +
				"secrets, and settings. Give this one a short name so you can tell it " +
				"apart from others you create later (e.g. \"work\", \"client-a\"). " +
				"Lowercase letters, numbers, and hyphens only, or \"default\" for your " +
				"main context.",
			renderQuestionText("Name", m.contextNameInput.View())

	case stepSecretBackend:
		return "Where to store secrets",
			"DevDesk needs somewhere to keep your forge token. \"auto\" tries your OS " +
				"keyring first, then git's credential helper, and falls back to " +
				"memory-only — nothing survives past this session — if neither is " +
				"available. Pick \"keyring\" or \"git-credential\" to require one " +
				"specifically instead of silently falling back.",
			renderQuestionCycle("Secret backend", secretBackendOptions[m.secretBackendIdx])

	case stepContainerEngine:
		return "Container engine",
			"DevDesk drives a container engine for image scanning and container " +
				"management. \"auto\" prefers Docker when it's on PATH and falls back " +
				"to Podman. Pick one explicitly if you have both installed and want " +
				"DevDesk to always use the same one.",
			renderQuestionCycle("Container engine", config.ContainerEngines()[m.containerEngineIdx])

	case stepTheme:
		return m.themeQuestion()

	case stepForgeType:
		return "Code hosting platform",
			"Which platform hosts the repositories you work with? This decides the " +
				"words DevDesk uses (Groups vs. Organizations, Merge Requests vs. Pull " +
				"Requests) and which API it talks to.",
			renderQuestionCycle("Forge", v.Name)

	case stepForgeURL:
		return v.Name + " URL",
			"The address of your " + v.Name + " instance. Leave the public default " +
				"if that's what you use; change it for a self-hosted instance. Leave it " +
				"blank to skip forge configuration for now — you can set it up later " +
				"from the authentication view.",
			renderQuestionText(v.Name+" URL", m.forgeURLInput.View())

	case stepForgeNamespace:
		return "Default " + v.Namespace,
			"The " + v.Namespace + " new repositories are created under by default, " +
				"if you use DevDesk to create them. Leave it blank if you don't have " +
				"one yet — you can always set it per repository.",
			renderQuestionText("Default "+v.Namespace, m.forgeNamespaceInput.View())

	case stepForgeVisibility:
		return "Default visibility",
			visibilityBody(v, m.visibilityOptions()),
			renderQuestionCycle("Visibility", m.visibilityValue)

	case stepForgeCloneMethod:
		return "Clone method",
			"How DevDesk clones your repositories. \"https\" works everywhere and " +
				"may prompt for your token; \"ssh\" uses your configured SSH key and " +
				"needs no further authentication for git operations.",
			renderQuestionCycle("Clone method", cloneMethodOptions[m.cloneMethodIdx])

	case stepForgeToken:
		return v.TokenLabel,
			v.TokenHelp + " It is checked against " + v.Name + " before being saved. " +
				"Leave it blank to configure this later from the authentication view — " +
				"DevDesk will still create the context.",
			renderQuestionText(v.TokenLabel, m.forgeTokenInput.View())
	}
	return "", "", ""
}

// fontCheckQuestion is the wizard's first screen: a sample of the Nerd Font
// glyphs every other screen (and the rest of the app) renders with, so a
// broken font is caught before the user spends the rest of setup looking at
// boxes and question marks instead of icons. It answers nothing that gets
// saved — cycling it only changes whether the remediation text below is
// shown.
func (m Model) fontCheckQuestion() (title, body, control string) {
	sample := strings.Join([]string{
		theme.IconCircleSmall, theme.IconChevronRight, theme.IconSelect,
		theme.IconOK, theme.IconWarning, theme.IconError,
	}, "  ")

	body = "DevDesk uses a Nerd Font for its icons, throughout this wizard and the " +
		"rest of the application. The line below should show a handful of small, " +
		"distinct glyphs — a dot, an arrow, a checkmark — not boxes, question " +
		"marks, or blank squares."
	if m.fontCheckIdx == 1 {
		body += "\n\nInstall a Nerd Font from https://www.nerdfonts.com/font-downloads " +
			"and set it as your terminal's font, then restart the terminal. This does " +
			"not block setup — you can continue now and fix it later."
	}

	control = valueStyle.Render(sample) + "\n\n" +
		renderQuestionCycle("Icons render correctly?", fontCheckOptions[m.fontCheckIdx])
	return "Terminal font check", body, control
}

// themeQuestion reflects the one-shot background fetch's state: in flight,
// unreachable (with a pointer to the manual-copy docs), or done.
func (m Model) themeQuestion() (title, body, control string) {
	body = "Pick the color theme DevDesk renders with."
	switch m.themeCheckState {
	case themeCheckUnreachable:
		body += " Only the built-in theme is available right now — no network " +
			"reachable to fetch more. Browse the rest at " +
			"https://github.com/anthnel/devdesk/tree/main/themes and copy one into " +
			"~/.devdesk/themes/ by hand; see README.md for the exact steps."
	case themeCheckDone:
		if m.themeFetchAddedCount > 0 {
			body += fmt.Sprintf(" %d additional theme(s) were downloaded from the "+
				"DevDesk repository and are ready to use below.", m.themeFetchAddedCount)
		}
	}
	return "Theme", body, renderQuestionCycle("Theme", m.themeOptions[m.themeIdx])
}

// visibilityBody explains the current forge's visibility options — "internal"
// only exists for the ones that have one.
func visibilityBody(v forge.Vocabulary, opts []string) string {
	body := "How visible should repositories DevDesk creates be by default? " +
		"\"private\" — only you and people you explicitly grant access."
	for _, o := range opts {
		if o == "internal" {
			body += " \"internal\" — visible to every authenticated user of this " + v.Name + " instance."
		}
	}
	body += " \"public\" — visible to anyone."
	return body
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

// helpLine lists this step's keys — only the ones that apply, since a
// cycle key on a text question would be a lie about what it does.
func (m Model) helpLine() string {
	help := "  [enter] continue"
	if isCycleStep(m.step) {
		help += "  [←→] change"
	}
	if m.step == stepFontCheck {
		help += "  [esc] quit"
	} else {
		help += "  [esc] back"
	}
	return help
}

func isCycleStep(step int) bool {
	switch step {
	case stepFontCheck, stepSecretBackend, stepContainerEngine, stepTheme,
		stepForgeType, stepForgeVisibility, stepForgeCloneMethod:
		return true
	}
	return false
}

// bodyStyle wraps an explanatory paragraph to a readable width regardless of
// how wide the terminal is.
func bodyStyle(width int) lipgloss.Style {
	w := width - 2
	if w > bodyMaxWidth {
		w = bodyMaxWidth
	}
	if w < 20 {
		w = 20
	}
	return dimStyle.Width(w)
}

// renderQuestionText renders a text question: the label above the input,
// both always in the focused style — there is only ever one control on
// screen, so it is always the focused one.
func renderQuestionText(label, valueView string) string {
	return focusStyle.Render(theme.IconCircleSmall+" "+label+" "+theme.IconChevronRight) + "\n  " + valueView
}

// renderQuestionCycle renders a closed-set question with the ←→ cycle
// pattern (Rule 132).
func renderQuestionCycle(label, value string) string {
	return focusStyle.Render(theme.IconCircleSmall+" "+label+" "+theme.IconSelect+" "+theme.IconChevronRight+" ") +
		valueStyle.Render(value)
}
