package configuration

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
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

	group := ""
	for i, f := range m.fields() {
		if f.Group != group {
			if group != "" {
				lines = append(lines, theme.EmptyLineBg(m.width)) // breathe between groups
			}
			group = f.Group
			lines = append(lines,
				theme.PadWithBg(theme.Bg("  ")+theme.SubTitleStyle.Render(f.GroupIcon+" "+f.Group), m.width),
				theme.EmptyLineBg(m.width))
		}
		lines = append(lines, theme.PadWithBg(m.renderField(f, i == m.focusedField), m.width))
	}

	return strings.Join(lines, "\n")
}

// GetTitle is the viewport's border title. It carries the context because a
// configuration belongs to one, and editing workspaces_dir in the wrong context
// is otherwise a silent mistake — the fields look identical in all of them.
func (m Model) GetTitle() string {
	return theme.IconConfig + " Configuration · " + m.context
}

// GetIcon is unused by the header, like every other view's.
func (m Model) GetIcon() string { return "" }

// GetHeaderInfo names the section and where its settings are written. The file
// path is the point: this view is the one place a user needs to know which file
// their keystrokes are landing in.
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	section := ""
	if m.activeTab >= 0 && m.activeTab < len(m.sections) {
		section = m.sections[m.activeTab].Title
	}
	info := []shortcut.HeaderInfo{
		{Key: "Section", Value: section, Style: theme.HeaderValueStyle},
	}
	if path, err := config.GetContextPath(m.context); err == nil {
		info = append(info, shortcut.HeaderInfo{Key: "File", Value: path, Style: theme.HeaderValueStyle})
	}
	return info
}

// renderField draws one setting (Rules 120, 132).
//
// Values are aligned on one column: twenty-nine settings whose values each start
// wherever their label happened to end reads as noise, and a cycle field's
// select icon makes its prefix two cells wider than a text field's, so the
// padding has to be measured on the whole prefix rather than on the label.
func (m Model) renderField(f field, focused bool) string {
	// A checkbox brings its own focus indicator and needs no value column.
	// Both helpers already emit the two-cell indent, so the view must not add
	// one — a locked checkbox would otherwise sit two cells right of the rest.
	if f.Kind == kindToggle {
		if m.isDisabled(f) {
			return theme.RenderCheckboxDisabled(f.Label)
		}
		return theme.RenderCheckbox(f.Bool(m.config), f.Label, focused)
	}

	indicator := "  "
	if focused {
		indicator = theme.IconCircleSmall + " "
	}
	prefix := indicator + m.padHead(fieldHead(f)) + " " + theme.IconChevronRight + " "

	if focused {
		if f.Kind == kindCycle {
			return theme.KeyStyle.Render(prefix) + theme.Bg(f.Value(m.config))
		}
		return theme.KeyStyle.Render(prefix) + m.input.View()
	}
	return theme.Bg(prefix) + theme.Bg(f.Value(m.config))
}

// fieldHead is everything before the chevron: the label, plus the select icon a
// closed-list field carries (Rule 132's ordering).
func fieldHead(f field) string {
	if f.Kind == kindCycle {
		return f.Label + " " + theme.IconSelect
	}
	return f.Label
}

// padHead widens a head to the active tab's chevron column.
//
// Padding here rather than after the chevron aligns both: the chevrons form one
// column and the values another. Padding the label alone would leave a cycle
// field's chevron two cells right of every other, because its select icon sits
// between the two.
func (m Model) padHead(head string) string {
	if w := m.chevronColumn() - lipgloss.Width(head); w > 0 {
		return head + strings.Repeat(" ", w)
	}
	return head
}

// chevronColumn is the widest head in the active tab. Checkboxes are excluded:
// they have no chevron and no value, so a long checkbox label pushing every
// value right would be padding for nothing.
func (m Model) chevronColumn() int {
	widest := 0
	for _, f := range m.fields() {
		if f.Kind == kindToggle {
			continue
		}
		widest = max(widest, lipgloss.Width(fieldHead(f)))
	}
	return widest
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
