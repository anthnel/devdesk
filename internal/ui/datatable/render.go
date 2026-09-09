package datatable

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Why this package renders its own rows rather than calling table.Model.View().
//
// bubbles/table builds a cell as:
//
//	m.styles.Cell.Render(style.Render(runewidth.Truncate(value, width, "…")))
//
// — it measures the value *before* styling it, with runewidth, which counts an
// escape sequence's bytes as width. A seven-cell string carrying a colour
// measures 28, so it is truncated in a column twice wide enough, and the cut
// lands inside the escape: "\x1b[38;2;166;2…". The unterminated sequence then
// bleeds over every row below. That is Rule 122, and no configuration of
// bubbles avoids it — bubbles v1.0.0 has the same line.
//
// Here the order is inverted: Cell returns plain text, which is what gets
// measured and truncated, and Style is applied to the finished cell. Nothing
// styled is ever measured, so the failure is unexpressible rather than
// forbidden by review.
//
// The other half of the reason is the selected row. bubbles hands the whole
// joined row to styles.Selected, and a colour inside it closes with a reset
// that takes the selection background with it for the rest of the line — the
// highlight ends mid-row. A row a view has coloured whole (error, busy, a CVE
// severity) still drops per-cell colours for exactly that reason (see
// cellStyle). The plain "normal" selection instead gives every cell the
// *same* background itself, which sidesteps the defect rather than reproduces
// it — a reset between two cells never uncovers anything but that shared
// background again.

// truncationMarker ends a cell too narrow for its content.
const truncationMarker = "…"

// View renders the header and the window of rows around the cursor.
func (m *Model[T]) View() string {
	cols := m.table.Columns()
	// At least one line under the header. A terminal too short to hold a single
	// row leaves Height() at 0, and a table that renders literally nothing
	// closes the viewport border onto itself — bubbles' viewport always emitted
	// that line, and two views check for it at 20x1.
	rows := max(m.table.Height(), 1)

	lines := make([]string, 0, rows+1)
	lines = append(lines, m.headerLine(cols))

	cursor := m.table.Cursor()
	for i := m.offset; i < len(m.visible) && len(lines) <= rows; i++ {
		lines = append(lines, m.rowLine(cols, m.visible[i], i == cursor))
	}
	// Pad to the full height so the viewport border does not close in on a
	// short list. bubbles' viewport did this with unstyled lines; filling them
	// with the app background is Rule 115 applied to the one part of the table
	// that had escaped it.
	blank := theme.EmptyLineBg(contentWidth(cols))
	for len(lines) <= rows {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

// RenderedWidth is what the table's lines actually span. It equals the viewport
// interior — the width passed to Resize, less its two borders — whenever
// Rule 116 holds, and it is what a view's layout test should assert on.
//
// Summing the declared columns' widths and adding two per column is the
// tempting version, and it is what every one of those tests did. It is wrong as
// soon as a column is dropped for want of room: the dropped column renders
// nothing and its padding goes back into the budget, so the formula asks for
// less than the line spans and fails on a layout that is correct. That
// disagreement was D61 seen from the other side.
func (m *Model[T]) RenderedWidth() int { return contentWidth(m.table.Columns()) }

// contentWidth is what the rendered lines span: every column plus the padding
// bubbles adds around each. It equals the viewport width less its two borders
// whenever Rule 116 holds, but it is measured rather than assumed so that a
// table that has never been resized still pads to its own content.
func contentWidth(cols []table.Column) int {
	width := 0
	for _, col := range cols {
		if col.Width > 0 {
			width += col.Width + cellPadding
		}
	}
	return width
}

// headerLine renders the column titles, sort arrow included.
func (m *Model[T]) headerLine(cols []table.Column) string {
	var line strings.Builder
	for _, col := range cols {
		if col.Width <= 0 {
			continue
		}
		// A header is cut at its end whatever the column asks for its values: a
		// title is recognised by how it starts, and TruncateHead is a statement
		// about the data.
		line.WriteString(m.styles.Header.Render(fit(col.Title, col.Width, false)))
	}
	return line.String()
}

// rowLine renders one row.
//
// A busy row shows the spinner in its status column instead of that column's
// own cell — on the selected row too. The glyph is the primary signal, and a
// signal that disappears under the cursor is one the user loses exactly when
// they are looking at it.
func (m *Model[T]) rowLine(cols []table.Column, item T, selected bool) string {
	_, busy := m.busyLabel(item)

	var line strings.Builder
	for i, c := range m.cfg.Columns {
		if i >= len(cols) || cols[i].Width <= 0 {
			continue
		}
		text := c.Cell(item)
		if busy && i == m.cfg.StatusColumn {
			text = m.spinnerFrame
		}
		fitted := fit(text, cols[i].Width, c.TruncateHead)
		if !busy && c.TailStyle != nil && (!selected || m.preserveColumnColors) {
			line.WriteString(m.splitCellRun(c, item, fitted, selected))
			continue
		}
		line.WriteString(m.cellStyle(c, item, selected, busy, i).Render(fitted))
	}
	if selected {
		return m.styles.Selected.Render(line.String())
	}
	return line.String()
}

// fit truncates the text to the column and pads it out, emitting no escape
// sequence of its own — the caller styles what comes back. Doing it in that
// order is the whole point of this file: runewidth counts an escape sequence's
// bytes as width, so text has to be measured while it is still plain.
func fit(text string, width int, head bool) string {
	cut := runewidth.Truncate(text, width, truncationMarker)
	if head {
		cut = truncateHead(text, width)
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Inline(true).Render(cut)
}

// truncateHead keeps the *end* of the text, for the columns that declare it
// (Column.TruncateHead).
//
// A column of URLs, image references or paths shares its prefix on every row,
// so cutting the tail renders three identical cells that identify nothing —
// and it does so precisely when the column is squeezed to its floor, which is
// when it is hardest to read. Keeping the end is what tells the rows apart.
//
// runewidth rather than runes throughout: a cell is measured in terminal cells,
// and a CJK name counts two per rune.
func truncateHead(text string, width int) string {
	total := runewidth.StringWidth(text)
	if total <= width || width <= 0 {
		return text
	}
	marker := runewidth.StringWidth(truncationMarker)
	if width <= marker {
		// No room for the marker and anything after it. Saying "there is more"
		// with no idea what beats a single arbitrary character.
		return runewidth.Truncate(truncationMarker, width, "")
	}
	return runewidth.TruncateLeft(text, total-(width-marker), truncationMarker)
}

// cellStyle is the style one cell is rendered with.
//
// A row a view has coloured whole — error, busy, a CVE severity, via
// SelectedStyles or the busy override — drops every column's own colour and
// is handed to styles.Selected instead: a colour inside it would close with a
// reset that takes that solid background with it for the rest of the line.
// The highlight there answers "what state is this row in", and no per-cell
// colour is worth losing it to.
//
// **The plain "normal" selection is the opposite case** (§3.72 in the
// backlog, m.preserveColumnColors): each cell keeps its own colour and
// additionally repaints ColorTableLineSelected and bold **itself**, rather than
// once on an outer wrap. That is what makes it safe rather than a return of
// the defect above — every cell's own reset only ever uncovers the *same*
// background the next cell immediately repaints, so nothing but that
// background is ever exposed between two cells. styles.Selected still wraps
// the joined line afterward (rowLine), painting the same background again;
// by then it is redundant colour, not load-bearing, and it is what still
// pads the row out to the full content width.
//
// Off the selected row every cell carries an explicit foreground **and**
// background, whether or not the column asked for either. Both halves are
// forced for the same reason and each fixes its own defect:
//
//   - Background: lipgloss does not inherit one (Rule 115), and the app's
//     viewport style only reaches the cells that emit nothing of their own. One
//     coloured cell would otherwise end its line with a reset and strip the
//     background from everything to its right.
//   - Foreground: neither theme.DefaultTableStyles() nor bubbles' own sets one
//     on Cell, so a column that declares no Style rendered in whatever
//     foreground the terminal happens to use — the theme had no say. Four views
//     had written `Foreground(theme.ColorText)` into a Style of their own to get
//     it back, which is the shape a missing default takes.
//
// The busy row, for its part, does not consult `Style` either: what it says
// is that an operation is in progress, and a color by severity or by state
// on top of that would say the opposite. The spinner's glyph keeps
// `ColorHighlight`, the rest goes to `DimStyle` — the displayed state is in
// the process of ceasing to be true.
func (m *Model[T]) cellStyle(c Column[T], item T, selected, busy bool, at int) lipgloss.Style {
	if selected {
		if !m.preserveColumnColors {
			// Neither background nor text here: they would hide those of styles.Selected.
			return m.styles.Cell
		}
		style := m.styles.Cell
		if c.Style != nil {
			style = c.Style(item).Padding(0, 1)
		}
		if _, unset := style.GetForeground().(lipgloss.NoColor); unset {
			style = style.Foreground(theme.ColorText)
		}
		return style.Background(theme.ColorTableLineSelected).Bold(true)
	}
	if busy {
		style := theme.DimStyle
		if at == m.cfg.StatusColumn {
			style = lipgloss.NewStyle().Foreground(theme.ColorHighlight)
		}
		return style.Padding(0, 1).Background(theme.ColorBackground)
	}
	if c.Style == nil {
		return m.styles.Cell.Foreground(theme.ColorText).Background(theme.ColorBackground)
	}
	style := c.Style(item).Padding(0, 1)
	if _, unset := style.GetForeground().(lipgloss.NoColor); unset {
		style = style.Foreground(theme.ColorText)
	}
	if _, unset := style.GetBackground().(lipgloss.NoColor); unset {
		style = style.Background(theme.ColorBackground)
	}
	return style
}

// splitCellRun renders a two-colour cell: c.Style covers the first c.Cut(item)
// cells of the already-fitted text, c.TailStyle the rest. Both runs get the
// column's usual 1-cell padding on their own outer edge — literal padding
// characters rather than lipgloss's Padding property, so the two renders don't
// each add their own pair and open a two-cell gap at the run boundary.
//
// fitted is exactly cols[i].Width runes (fit() guarantees it), so slicing by
// rune index is safe: every glyph a caller uses here — braille, ASCII, the
// truncation marker — is single-width, which callers are expected to keep
// true rather than this function verifying it.
//
// selected mirrors cellStyle's own selected branch: on the plain "normal"
// selection (m.preserveColumnColors), both runs repaint ColorTableLineSelected and
// bold themselves instead of each keeping their unselected background — the
// same per-cell repaint that makes a single-run selected cell safe applies
// unchanged to two runs, since a reset between them only ever uncovers that
// same shared background, immediately repainted by the run that follows.
// This is what lets a load gauge (Cut/TailStyle) keep its fill/track split
// under the cursor instead of collapsing to Style's fill colour alone.
func (m *Model[T]) splitCellRun(c Column[T], item T, fitted string, selected bool) string {
	runes := []rune(fitted)
	cut := c.Cut(item)
	switch {
	case cut < 0:
		cut = 0
	case cut > len(runes):
		cut = len(runes)
	}
	head := " " + string(runes[:cut])
	tail := string(runes[cut:]) + " "
	return m.runStyle(c.Style, item, selected).Render(head) + m.runStyle(c.TailStyle, item, selected).Render(tail)
}

// runStyle applies the same foreground/background defaulting cellStyle gives
// a single-run cell, minus the Padding — splitCellRun already pads with
// literal characters — so a two-run cell is indistinguishable from a one-run
// one everywhere but the run boundary.
//
// selected forces the shared selection background and bold weight
// unconditionally, exactly as cellStyle's selected branch does for a
// single-run cell — unlike the unselected path below, this overrides
// whatever background the column's own Style already set (GaugeTrackStyle
// sets one explicitly), because the selected row's background is the
// highlight itself, not a fallback for an unset one.
func (m *Model[T]) runStyle(styleFn func(T) lipgloss.Style, item T, selected bool) lipgloss.Style {
	var style lipgloss.Style
	if styleFn != nil {
		style = styleFn(item)
	}
	if _, unset := style.GetForeground().(lipgloss.NoColor); unset {
		style = style.Foreground(theme.ColorText)
	}
	if selected {
		return style.Background(theme.ColorTableLineSelected).Bold(true)
	}
	if _, unset := style.GetBackground().(lipgloss.NoColor); unset {
		style = style.Background(theme.ColorBackground)
	}
	return style
}

// clampOffset moves the scroll window the least it can to keep the cursor in
// it. bubbles kept this in an unexported viewport; owning the rendering means
// owning the offset too.
//
// The window is only moved when the cursor has left it, so a reload that keeps
// the cursor on screen keeps the scroll position — the property SetItems was
// written to protect.
func (m *Model[T]) clampOffset() {
	height := max(m.table.Height(), 1)
	last := max(len(m.visible)-height, 0)

	m.offset = min(max(m.offset, 0), last)

	switch cursor := m.table.Cursor(); {
	case cursor < 0: // nothing selected: an empty table scrolls nowhere
		m.offset = 0
	case cursor < m.offset:
		m.offset = cursor
	case cursor >= m.offset+height:
		m.offset = cursor - height + 1
	}
}

// Offset reports the first visible row, for tests that check the window
// follows the cursor without going through the rendered string.
func (m *Model[T]) Offset() int { return m.offset }
