package configuration

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders the active tab's fields. The tab bar itself lives in the footer
// so it stays visible when the list scrolls (Rule 123).
func (m Model) View() string {
	if m.confirmModal != nil {
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center, m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground))
	}

	lines := []string{theme.EmptyLineBg(m.width)} // Rule 131: exactly one blank line

	lines = append(lines, theme.PadWithBg(
		theme.Bg("  ")+theme.DimStyle.Render("context ")+theme.KeyStyle.Render(m.context), m.width))
	lines = append(lines, theme.EmptyLineBg(m.width))

	for i, f := range m.fields() {
		lines = append(lines, theme.PadWithBg(m.renderField(f, i == m.focusedField), m.width))
	}

	return strings.Join(lines, "\n")
}

// renderField draws one setting (Rules 120, 132).
func (m Model) renderField(f field, focused bool) string {
	indicator := "  "
	if focused {
		indicator = theme.IconCircleSmall + " "
	}

	switch f.Kind {
	case kindToggle:
		if m.isDisabled(f) {
			return theme.Bg(indicator) + theme.RenderCheckboxDisabled(f.Label)
		}
		return theme.Bg(indicator) + theme.RenderCheckbox(f.Bool(m.config), f.Label, focused)

	case kindCycle:
		label := f.Label + " " + theme.IconSelect + " "
		if focused {
			return theme.KeyStyle.Render(indicator+label+theme.IconChevronRight+" ") + theme.Bg(f.Value(m.config))
		}
		return theme.Bg(indicator+label+theme.IconChevronRight+" ") + theme.Bg(f.Value(m.config))

	default: // text and integer
		label := f.Label + " " + theme.IconChevronRight + " "
		if focused {
			return theme.KeyStyle.Render(indicator+label) + m.input.View()
		}
		return theme.Bg(indicator+label) + theme.Bg(f.Value(m.config))
	}
}

// GetFooterHeight is the tab bar, a blank line and the info line (Rule 124).
func (m Model) GetFooterHeight() int { return 3 }

// RenderFooter draws the tab bar, then the hint or message line.
func (m Model) RenderFooter(width int) string {
	tabs := make([]theme.TabItem, 0, len(m.sections))
	for _, s := range m.sections {
		tabs = append(tabs, theme.TabItem{Label: s.Title})
	}
	tabBar := theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs(tabs, m.activeTab), width)

	info := theme.EmptyLineBg(width)
	switch {
	case m.footerError != "":
		info = theme.PadWithBg(theme.StatusErrorStyle.Render(m.footerError), width)
	case m.footerInfo != "":
		info = centeredInfo(m.footerInfo, width)
	case m.current().hint != "":
		info = centeredInfo(m.current().hint, width)
	}

	return tabBar + "\n" + theme.EmptyLineBg(width) + "\n" + info
}

func centeredInfo(s string, width int) string {
	return lipgloss.NewStyle().
		Foreground(theme.ColorHighlight).
		Background(theme.ColorBackground).
		Width(width).
		Align(lipgloss.Center).
		Render(s)
}

// GetShortcuts lists only what is not self-evident (Rules 130, 137, 138).
// ↑↓ and tab are deliberately absent.
func (m Model) GetShortcuts() shortcut.Shortcuts {
	switch m.current().Kind {
	case kindCycle:
		return []shortcut.Shortcut{
			{Key: "←→", Description: "Change value"},
			{Key: "?", Description: "Open help"},
		}
	case kindToggle:
		return []shortcut.Shortcut{
			{Key: "space", Description: "Toggle"},
			{Key: "?", Description: "Open help"},
		}
	default:
		return []shortcut.Shortcut{
			{Key: "↑↓", Description: "Save and move on"},
			{Key: "?", Description: "Open help"},
		}
	}
}

// GetHelpContent implements help.Provider (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title: "Configuration",
		Description: "Every scalar setting in the current context. Changes are written to " +
			"the context's config file as you make them — there is no save step.",
		KeyBindings: []help.KeyBinding{
			{Key: "tab", Description: "Next section"},
			{Key: "↑↓", Description: "Move between settings"},
			{Key: "←→", Description: "Change a closed-list value"},
			{Key: "space", Description: "Toggle a checkbox"},
			{Key: "alt+:", Description: "Open the command line"},
		},
		Sections: []help.Section{
			{
				Title: "What is not here",
				Body: "Monitors are edited in the status view, registries in the OCI view.\n" +
					"They are lists, and they are edited where they are consulted.",
			},
			{
				Title: "Contexts",
				Body: "A configuration belongs to one context. This view edits " + m.context + ".\n" +
					"Switch with :context <name> — every view is rebuilt against the new file.",
			},
			{
				Title: "Scan tool source",
				Body: "auto    the binary when there is one, the Docker image otherwise\n" +
					"binary  the configured path, else the name on PATH — fails if absent\n" +
					"image   the Docker image, even when a binary is installed",
			},
			{
				Title: "Secret backend",
				Body: "Changing it is confirmed, and stored secrets are not migrated:\n" +
					"the GitLab token and registry passwords have to be entered again.",
			},
		},
	}
}
