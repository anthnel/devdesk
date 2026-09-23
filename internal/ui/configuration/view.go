package configuration

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/scan"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// View renders the active tab's fields. The tab bar itself lives in the footer
// so it stays visible when the list scrolls (Rule 123).
//
// A tab taller than the viewport scrolls: the scan tab alone ran past seventy
// lines, and with nothing scrolling the bottom of it was simply cut off.
func (m Model) View() string {
	if m.confirmModal != nil {
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center, m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground))
	}

	lines := m.layout().lines
	if m.height > 0 && len(lines) > m.height {
		start := min(m.scroll, len(lines)-m.height)
		lines = lines[start : start+m.height]
	}
	return strings.Join(lines, "\n")
}

// layout is the active tab rendered in full: its lines, and the line each
// field sits on — which is what scrolling keeps on screen.
type layout struct {
	lines []string
	rows  []int
}

// Two columns need room for a label, a chevron and a value that reads on each
// side; below that, one column (§3.86).
const (
	columnMinWidth = 56
	columnGap      = 3
)

// columns is how many columns the active tab is laid out on at this width.
func (m Model) columns() int {
	if m.activeTab < 0 || m.activeTab >= len(m.sections) || m.sections[m.activeTab].Columns < 2 {
		return 1
	}
	if m.width < 2*columnMinWidth+columnGap {
		return 1
	}
	return 2
}

// columnWidth is the width a field is rendered in.
func (m Model) columnWidth() int {
	if m.columns() == 2 {
		return (m.width - columnGap) / 2
	}
	return m.width
}

// block is one group of fields, rendered: its heading, its rows, and the line
// each of its fields sits on, relative to the block.
type block struct {
	lines  []string
	fields map[int]int
}

func (m Model) layout() layout {
	fields := m.fields()
	width := m.columnWidth()
	blocks := m.blocks(fields, width)

	out := layout{
		lines: []string{theme.EmptyLineBg(m.width)}, // Rule 131: exactly one blank line
		rows:  make([]int, len(fields)),
	}
	if m.columns() == 1 {
		for i, b := range blocks {
			if i > 0 {
				out.lines = append(out.lines, theme.EmptyLineBg(m.width)) // breathe between groups
			}
			for idx, row := range b.fields {
				out.rows[idx] = len(out.lines) + row
			}
			out.lines = append(out.lines, b.lines...)
		}
		return out
	}

	left, right := splitBlocks(blocks)
	stack := func(bs []block, base int) []string {
		var lines []string
		for i, b := range bs {
			if i > 0 {
				lines = append(lines, theme.EmptyLineBg(width))
			}
			for idx, row := range b.fields {
				out.rows[idx] = base + len(lines) + row
			}
			lines = append(lines, b.lines...)
		}
		return lines
	}
	base := len(out.lines)
	l, r := stack(left, base), stack(right, base)
	gap := theme.Bg(strings.Repeat(" ", columnGap))
	for i := range max(len(l), len(r)) {
		cell := func(col []string) string {
			if i < len(col) {
				return col[i]
			}
			return theme.EmptyLineBg(width)
		}
		out.lines = append(out.lines, theme.PadWithBg(cell(l)+gap+cell(r), m.width))
	}
	return out
}

// splitBlocks cuts the groups, in order, where the two columns come closest to
// the same height. Order is kept so ↓ reads down the left column and carries on
// at the top of the right one: the focus order is the flat field list.
func splitBlocks(blocks []block) ([]block, []block) {
	height := func(bs []block) int {
		h := 0
		for i, b := range bs {
			if i > 0 {
				h++
			}
			h += len(b.lines)
		}
		return h
	}
	best, bestDiff := len(blocks), -1
	for cut := 1; cut < len(blocks); cut++ {
		diff := height(blocks[:cut]) - height(blocks[cut:])
		if diff < 0 {
			diff = -diff
		}
		if bestDiff < 0 || diff < bestDiff {
			best, bestDiff = cut, diff
		}
	}
	return blocks[:best], blocks[best:]
}

// blocks renders each group of the tab at a width.
func (m Model) blocks(fields []field, width int) []block {
	var out []block
	for i, f := range fields {
		if len(out) == 0 || f.Group != fields[i-1].Group {
			heading := theme.Bg("  ") + theme.SubTitleStyle.Render(f.GroupIcon+" "+f.Group)
			if f.Tool != "" {
				heading += theme.Bg("  ") + m.toolState(f.Tool)
			}
			out = append(out, block{
				lines:  []string{fit(heading, width), theme.EmptyLineBg(width)},
				fields: map[int]int{},
			})
		}
		b := &out[len(out)-1]
		b.fields[i] = len(b.lines)
		b.lines = append(b.lines, fit(m.renderField(f, i == m.focusedField), width))
	}
	return out
}

// fit cuts a line to a width and pads it to exactly that width: a row wider
// than its column would push the other column right.
func fit(line string, width int) string {
	if lipgloss.Width(line) > width {
		line = ansi.Truncate(line, width, "…")
	}
	return theme.PadWithBg(line, width)
}

// keepFocusVisible scrolls so the focused field is on screen, with the line
// above it — or its group's heading, at the top of a group.
func (m *Model) keepFocusVisible() {
	l := m.layout()
	if m.height <= 0 || len(l.lines) <= m.height {
		m.scroll = 0
		return
	}
	row := 0
	if m.focusedField >= 0 && m.focusedField < len(l.rows) {
		row = l.rows[m.focusedField]
	}
	const margin = 2 // the heading and the blank line under it
	if row-margin < m.scroll {
		m.scroll = max(row-margin-1, 0)
	}
	if row+margin >= m.scroll+m.height {
		m.scroll = row + margin + 1 - m.height
	}
	m.scroll = min(max(m.scroll, 0), len(l.lines)-m.height)
}

// toolState is a tool group's heading on the tools tab: whether this context
// needs the tool, and whether it is there (Rule 121 for the icons). A tool
// nobody ticked is "not used", available or not — it has no reason to alarm.
func (m Model) toolState(id string) string {
	if m.tools == nil {
		return unknownState()
	}
	tool := scan.ToolID(id)
	if !slices.Contains(scan.Required(m.config.Scan.Categories), tool) {
		return theme.DimStyle.Render("not used")
	}
	st := m.tools.Status(tool)
	if !st.Available {
		return theme.StatusDownStyle.Render(theme.IconError) + theme.Bg(" required · missing")
	}
	parts := []string{"required", sourceWord(st.Source)}
	if v := scan.CleanVersion(st.Version); v != "" {
		parts = append(parts, v)
	}
	return theme.StatusOKStyle.Render(theme.IconOK) + theme.Bg(" "+strings.Join(parts, " · "))
}

// platformState is a platform tool's row: its version when it is there.
func (m Model) platformState(which string) string {
	if m.tools == nil {
		return unknownState()
	}
	name, ok, version := "git", m.tools.GitAvailable, m.tools.GitVersion
	if which == platformEngine {
		name, ok, version = engine.Current().Name, m.tools.EngineAvailable, m.tools.EngineVersion
	}
	if !ok {
		return theme.StatusDownStyle.Render(theme.IconError) + theme.Bg(" "+name+" not found")
	}
	text := " " + name
	if v := scan.CleanVersion(version); v != "" {
		text += " " + v
	}
	return theme.StatusOKStyle.Render(theme.IconOK) + theme.Bg(text)
}

// unknownState is what a heading says before the router's detection lands.
func unknownState() string { return theme.DimStyle.Render("…") }

// sourceWord names where a tool runs from in the configuration's own words:
// the file says "image", whichever engine runs it.
func sourceWord(s scan.ToolSource) string {
	if s == scan.ToolSourceContainer {
		return "image"
	}
	return string(s)
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
		return m.renderCheckbox(f, focused)
	}

	indicator := "  "
	if focused {
		indicator = theme.IconCircleSmall + " "
	}
	prefix := indicator + m.padHead(fieldHead(f)) + " " + theme.IconChevronRight + " "

	// A static row is never focused (Model.settleFocus walks past it) and its
	// value is dimmed, which is what says it is read here rather than edited.
	if f.Kind == kindStatic {
		if f.platform != "" {
			return theme.Bg(prefix) + m.platformState(f.platform)
		}
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

// renderCheckbox draws a checkbox at its depth, locked or not, with its note
// aligned on one column so the roles read as a list.
func (m Model) renderCheckbox(f field, focused bool) string {
	var box string
	if m.isDisabled(f) {
		box = theme.RenderCheckboxLocked(f.Bool(m.config), f.Label, focused)
	} else {
		box = theme.RenderCheckbox(f.Bool(m.config), f.Label, focused)
	}
	row := theme.Bg(depthIndent(f.Depth)) + box
	if f.Note == "" {
		return row
	}
	pad := max(m.noteColumn()-lipgloss.Width(row), 2)
	return row + theme.Bg(strings.Repeat(" ", pad)) + theme.DimStyle.Render(f.Note)
}

// depthIndent is how far a nested checkbox sits under the one it belongs to.
func depthIndent(depth int) string { return strings.Repeat("    ", depth) }

// noteColumn is where the notes of the active tab start: past the widest
// checkbox that carries one, measured unfocused and unlocked.
func (m Model) noteColumn() int {
	widest := 0
	for _, f := range m.fields() {
		if f.Kind != kindToggle || f.Note == "" {
			continue
		}
		w := lipgloss.Width(depthIndent(f.Depth) + theme.RenderCheckboxLocked(false, f.Label, false))
		widest = max(widest, w)
	}
	return widest + 3
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
	info := m.footer.View(width, sharedcomponents.Status{Text: m.hint(m.current())})

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
		{Key: "ctrl+r", Description: "Detect tools again", Disabled: !m.onToolsTab()},
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
			{Key: "ctrl+r", Description: "Detect the tools again (tools tab)"},
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
				Title: "Categories and tools",
				Body: "The scan tab says what a scan looks for: a category is on or off, and the\n" +
					"tools ticked under it run it. A tool is required when it is ticked in a\n" +
					"category that is on — helm and kustomize also need kubeconform ticked.\n" +
					"The footer says what a box does in the state it is in, and why a locked\n" +
					"one cannot move.\n" +
					"\n" +
					"The tools tab says where each tool runs from, and heads each one with its\n" +
					"state: required and there, required and missing, or not used. The tools\n" +
					"are detected at start, on a context switch, when a source, binary or image\n" +
					"is saved, and on ctrl+r there.",
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
