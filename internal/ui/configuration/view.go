package configuration

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
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

// GetTitle is the viewport's border title. The context is not repeated here:
// the header names it, as it does for every other view, and a title saying it
// too is the same fact twice on one screen.
func (m Model) GetTitle() string {
	return theme.IconConfig + " Configuration"
}

// GetIcon is unused by the header, like every other view's.
func (m Model) GetIcon() string { return "" }

// GetHeaderInfo names the context and the section.
//
// The context is what every other view puts here, and it is what this view
// needs most: editing workspaces_dir in the wrong context is otherwise a silent
// mistake, because the fields look identical in all of them.
//
// The file path used to sit here too. It moved into the Paths group, beside the
// two paths it belongs with — the header is for what changes as the user moves,
// and the file does not.
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	section := ""
	if m.activeTab >= 0 && m.activeTab < len(m.sections) {
		section = m.sections[m.activeTab].Title
	}
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
		{Key: "Section", Value: section, Style: theme.HeaderValueStyle},
	}
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

	// A static row is never focused (Model.settleFocus walks past it) and its
	// value is dimmed, which is what says it is read here rather than edited.
	if f.Kind == kindStatic {
		return theme.Bg(prefix) + theme.DimStyle.Render(f.Value(m.config))
	}

	// A secret is masked until `space`, like the forge token and the registry
	// password are masked as they are typed. The mask is a fixed width rather
	// than one dot per character: the length of a token is not something to
	// publish either.
	if f.Kind == kindSecret {
		value := maskedSecret
		if m.revealed(f) {
			value = f.Value(m.config)
		}
		if value == "" {
			value = "—"
		}
		if focused {
			return theme.KeyStyle.Render(prefix) + theme.Bg(value)
		}
		return theme.Bg(prefix) + theme.DimStyle.Render(value)
	}

	if focused {
		if f.Kind == kindCycle {
			return theme.KeyStyle.Render(prefix) + theme.Bg(f.Value(m.config))
		}
		return theme.KeyStyle.Render(prefix) + m.input.View()
	}
	return theme.Bg(prefix) + theme.Bg(f.Value(m.config))
}

// maskedSecret is what a secret row shows until `space`. Its width says nothing
// about the value's.
const maskedSecret = "••••••••••••"

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

	// The field's hint is the derived status: it is a permanent property of
	// where the cursor is, so it has no timer and any message displaces it.
	info := m.footer.View(width, sharedcomponents.Status{Text: m.current().hint})

	return tabBar + "\n" + theme.EmptyLineBg(width) + "\n" + info
}

// GetShortcuts lists only what is not self-evident (Rules 130, 137, 138).
// ↑↓ and tab are deliberately absent.
// GetShortcuts lists the three controls a field can take, greying the two the
// focused field does not (Rule 130).
//
// It used to return one of three single-entry lists, so the column changed
// shape on every ↑↓ — in a form, where the cursor moves constantly. `←→` and
// `space` are greyed rather than refused with a message: they are controls, not
// actions, and a footer line on every stray arrow key in a form would be noise.
//
// `↑↓` is never greyed because it always moves; only its wording changes, since
// a cycle or a checkbox has already persisted by the time the cursor leaves.
//
// `esc` is announced although Rule 138 calls the key obvious, because what it
// does here is not: it saves without moving. It is greyed on exactly the fields
// where it would do nothing — the ones that have already written.
func (m Model) GetShortcuts() shortcut.Shortcuts {
	field := m.current()

	move := "Move between fields"
	if field.takesText() {
		move = "Save and move on"
	}

	return []shortcut.Shortcut{
		{Key: "↑↓", Description: move},
		{Key: "←→", Description: "Change value", Disabled: field.Kind != kindCycle},
		{Key: "space", Description: "Toggle", Disabled: field.Kind != kindToggle},
		{Key: "esc", Description: "Save this field", Disabled: !m.settlesOnBlur(field)},
		{Key: "?", Description: "Open help"},
	}
}

// GetHelpContent implements help.Provider (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title: "Configuration",
		Description: "Every scalar setting in the current context. A checkbox and a closed-list " +
			"value are written as you press them; a typed value is written when you leave the " +
			"field — with ↑↓, tab, esc, or by leaving the view. A value the file refuses keeps " +
			"the cursor where it is, and says why.",
		KeyBindings: []help.KeyBinding{
			{Key: "tab", Description: "Next section"},
			{Key: "↑↓", Description: "Move between settings"},
			{Key: "←→", Description: "Change a closed-list value"},
			{Key: "space", Description: "Toggle a checkbox, or reveal the MCP token"},
			{Key: "esc", Description: "Save the focused field without moving off it"},
			{Key: "ctrl+p", Description: "Open the command line"},
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
				Title: "MCP server",
				Body: "It runs inside dk, over HTTP, and only while dk runs. Enable it here,\n" +
					"then point your agent at http://<listen>/ with the token as a bearer:\n" +
					"space reveals it on the Token row. It is stored in the secret backend,\n" +
					"never in a file — so put it in your agent's own config, not in a\n" +
					"committed .mcp.json, where it would reach the remote on the first push.\n" +
					"\n" +
					"The address is a loopback one and should stay that way: an agent in a\n" +
					"container reaches it at host.docker.internal, and the LAN never can.\n" +
					"Switching context restarts the server, which drops any open session —\n" +
					"that is deliberate, so no agent quietly starts reading another context.",
			},
			{
				Title: "Secret backend",
				Body: "Changing it is confirmed, and stored secrets are not migrated:\n" +
					"the " + m.vocab().Name + " token and registry passwords have to be entered again.",
			},
		},
	}
}
