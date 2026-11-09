package datatable

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/anthnel/devdesk/internal/ui/indicator"
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
// A busy row shows its activity glyph in the Indicator column instead of that
// column's own cell — on the selected row too. The glyph is the primary signal,
// and a signal that disappears under the cursor is one the user loses exactly
// when they are looking at it.
func (m *Model[T]) rowLine(cols []table.Column, item T, selected bool) string {
	_, busy := m.busyLabel(item)

	var line strings.Builder
	for i, c := range m.cfg.Columns {
		if i >= len(cols) || cols[i].Width <= 0 {
			continue
		}
		state := indicator.Idle
		if c.Indicator != nil {
			state = m.indicatorState(c, item, busy)
		}
		text := c.Cell(item)
		switch {
		case state != indicator.Idle:
			text = indicator.Glyph(state, m.blinkOn)
		}
		if state != indicator.Idle {
			// Padded with runewidth, not lipgloss: lipgloss always counts an
			// ambiguous-width glyph as one cell, runewidth follows the terminal.
			fitted := runewidth.FillRight(text, cols[i].Width)
			line.WriteString(m.indicatorCellStyle(state, selected).Render(fitted))
			continue
		}
		fitted := fit(text, cols[i].Width, c.TruncateHead)
		if !busy && (!selected || m.preserveColumnColors) {
			if runs, ok := m.multiRunCell(c, item, fitted, selected); ok {
				line.WriteString(runs)
				continue
			}
		}
		line.WriteString(m.cellStyle(c, item, selected, busy).Render(fitted))
	}
	if selected {
		return m.styles.Selected.Render(m.padToContent(line.String()))
	}
	return line.String()
}

// padToContent pins the selected row to the full content width (m.width-2) so
// the highlight reaches the right border when the columns span less than the
// viewport. The gap is computed from the column widths, never by re-measuring
// the rendered line: cells are fitted by lipgloss (ansi widths) and a second
// measure with another library disagrees on emoji and flags. On the plain
// "normal" selection the gap is painted with ColorTableLineSelected itself, as
// every cell is, because it follows the last cell's reset.
func (m *Model[T]) padToContent(line string) string {
	gap := m.width - 2 - m.RenderedWidth()
	if m.width <= 2 || gap <= 0 {
		return line
	}
	pad := strings.Repeat(" ", gap)
	if m.preserveColumnColors {
		pad = lipgloss.NewStyle().Background(theme.ColorTableLineSelected).Bold(true).Render(pad)
	}
	return line + pad
}

// indicatorState is the one place the precedence is written: a busy row is
// Running whatever the view says, otherwise the column's own state.
func (m *Model[T]) indicatorState(c Column[T], item T, busy bool) indicator.State {
	if busy {
		return indicator.Running
	}
	return c.Indicator(item)
}

// indicatorCellStyle styles a non-Idle indicator cell like cellStyle styles any
// other: background forced off the selection, repainted on the plain "normal"
// selection, left to styles.Selected when a view coloured the whole row.
func (m *Model[T]) indicatorCellStyle(state indicator.State, selected bool) lipgloss.Style {
	if selected {
		if !m.preserveColumnColors {
			return m.styles.Cell
		}
		return indicator.Style(state).Padding(0, 1).Background(theme.ColorTableLineSelected).Bold(true)
	}
	return indicator.Style(state).Padding(0, 1).Background(theme.ColorBackground)
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
// on top of that would say the opposite. Every cell goes to `DimStyle` — the
// displayed state is in the process of ceasing to be true; the activity glyph
// is drawn by the Indicator column.
func (m *Model[T]) cellStyle(c Column[T], item T, selected, busy bool) lipgloss.Style {
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
		return theme.DimStyle.Padding(0, 1).Background(theme.ColorBackground)
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

// run is one coloured stretch of an already-fitted cell.
type run struct {
	text  string
	style lipgloss.Style
}

// multiRunCell renders a cell that colours more than one stretch of its text:
// Column.Spans when it answers a non-empty list, else Cut/TailStyle. ok is false
// for an ordinary one-run cell, which the caller renders through cellStyle.
func (m *Model[T]) multiRunCell(c Column[T], item T, fitted string, selected bool) (string, bool) {
	if c.Spans != nil {
		if spans := c.Spans(item); len(spans) > 0 {
			return m.renderRuns(m.spanRuns(c, item, spans, fitted, selected)), true
		}
		return "", false
	}
	if c.TailStyle == nil {
		return "", false
	}
	return m.splitCellRun(c, item, fitted, selected), true
}

// spanRuns lays spans over the fitted text from left to right: a negative width
// counts as 0, a zero-width span yields no run, what overflows the cell is cut
// and the cells left after the last span take c.Style. Widths are in runes, so
// like Cut they assume single-width glyphs.
func (m *Model[T]) spanRuns(c Column[T], item T, spans []Span, fitted string, selected bool) []run {
	runes := []rune(fitted)
	var runs []run
	pos := 0
	for _, s := range spans {
		end := min(pos+max(s.Width, 0), len(runes))
		if end > pos {
			runs = append(runs, run{string(runes[pos:end]), m.runStyle(s.Style, selected)})
			pos = end
		}
	}
	if pos < len(runes) {
		runs = append(runs, run{string(runes[pos:]), m.runStyle(m.styleOf(c.Style, item), selected)})
	}
	return runs
}

// splitCellRun renders a two-colour cell: c.Style covers the first c.Cut(item)
// cells of the already-fitted text, c.TailStyle the rest, as two runs.
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
	cut := min(max(c.Cut(item), 0), len(runes))
	return m.renderRuns([]run{
		{string(runes[:cut]), m.runStyle(m.styleOf(c.Style, item), selected)},
		{string(runes[cut:]), m.runStyle(m.styleOf(c.TailStyle, item), selected)},
	})
}

// renderRuns renders the runs of one cell side by side. The column's 1-cell
// padding goes on the outer edge of the first and last run as literal spaces
// rather than lipgloss's Padding property, so runs don't each add their own pair
// and open a gap at a boundary. Every run given is rendered, empty or not:
// callers that must not emit an empty styled run drop it beforehand.
func (m *Model[T]) renderRuns(runs []run) string {
	var b strings.Builder
	for i, r := range runs {
		text := r.text
		if i == 0 {
			text = " " + text
		}
		if i == len(runs)-1 {
			text += " "
		}
		b.WriteString(r.style.Render(text))
	}
	return b.String()
}

// styleOf evaluates an optional per-item style function; nil gives the zero style.
func (m *Model[T]) styleOf(fn func(T) lipgloss.Style, item T) lipgloss.Style {
	if fn == nil {
		return lipgloss.NewStyle()
	}
	return fn(item)
}

// runStyle applies the same foreground/background defaulting cellStyle gives
// a single-run cell, minus the Padding — renderRuns already pads with
// literal characters — so a multi-run cell is indistinguishable from a one-run
// one everywhere but the run boundaries.
//
// selected forces the shared selection background and bold weight
// unconditionally, exactly as cellStyle's selected branch does for a
// single-run cell — unlike the unselected path below, this overrides
// whatever background the run's own style already set (GaugeTrackStyle
// sets one explicitly), because the selected row's background is the
// highlight itself, not a fallback for an unset one.
func (m *Model[T]) runStyle(style lipgloss.Style, selected bool) lipgloss.Style {
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
