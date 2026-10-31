// Package cmdsuggest renders the command-suggestion list shown under the
// command line (DEVDESK-89). Rendering is pure: state in, lines out.
package cmdsuggest

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// MaxVisibleRows is the ceiling on suggestion rows shown at once.
const MaxVisibleRows = 8

// Row is one suggestion; Current marks the view that is open now.
type Row struct {
	Entry   command.Entry
	Current bool
	// Arg, when non-nil, makes this an argument row: Entry is ignored.
	Arg *command.ArgSuggestion
}

// Frame selects the corners of the bottom rule.
type Frame int

const (
	// FrameJoined draws ├─┤ (the list sits above more viewport content).
	FrameJoined Frame = iota
	// FrameClosed draws └─┘.
	FrameClosed
)

// Input is everything Render needs.
type Input struct {
	Rows   []Row
	Total  int
	Query  string
	Cursor int // -1 means no highlighted row
	Offset int
	Notice string
	// Heading replaces the "N of M" count in the filet (argument stage).
	Heading string
	// Empty is the message shown when there are no rows and Loading is false.
	Empty string
	// Loading shows "Loading contexts…" in place of the rows.
	Loading bool
	Width   int
	Visible int
	Frame   Frame
}

// Layout holds column widths in cells.
type Layout struct{ Marker, Name, Alias, Desc int }

// Pair is a foreground/background combination the list paints.
type Pair struct {
	Name   string
	Fg, Bg lipgloss.Color
}

// Layout constants, in cells. The widths below are the whole grid: a line is
// a border, a one-cell margin, the columns, then fill up to the other border.
const (
	margin        = 1
	markerWidth   = 2
	minDescWidth  = 20
	minAliasWidth = 34 // narrowest terminal that still shows the alias column
	wideWidth     = 100
	aliasSep      = " · "
	currentTag    = "(current)"
	ellipsis      = "…"
	// minNoteWidth is the narrowest note column worth drawing; below it the
	// note disappears before the name or the (current) tag lose a cell.
	minNoteWidth = 8
	loadingText  = "Loading contexts…"
	// minRenderWidth is the narrowest width that holds a frame, a margin, a
	// marker and one name cell.
	minRenderWidth = 6
)

// VisibleRows returns how many list rows fit: the matches, at least one row
// (the "no match" line), capped by MaxVisibleRows and by the usable height
// minus the filet.
func VisibleRows(matches, usable int) int {
	return max(min(max(matches, 1), MaxVisibleRows, usable-1), 0)
}

// Window returns the first visible row so that cursor stays in view. The
// window never wraps and, with no highlighted row, keeps the current offset.
func Window(cursor, offset, visible int) int {
	offset = max(offset, 0)
	if cursor < 0 || visible <= 0 {
		return offset
	}
	switch {
	case cursor < offset:
		return cursor
	case cursor >= offset+visible:
		return cursor - visible + 1
	}
	return offset
}

// Columns returns the column widths for a terminal width. Columns leave from
// the right: description first, then alias; the name alone remains.
func Columns(width int) Layout {
	l := Layout{Marker: markerWidth}
	if width < minAliasWidth {
		l.Name = max(width-6, 1)
		return l
	}
	l.Name, l.Alias = 15, 14
	if width >= wideWidth {
		l.Name, l.Alias = 18, 18
	}
	if desc := width - 2 - margin - l.Marker - l.Name - l.Alias; desc >= minDescWidth {
		l.Desc = desc
	}
	return l
}

// first returns the index of the first row drawn, kept inside the list.
func first(in Input) int {
	return max(min(in.Offset, len(in.Rows)-max(in.Visible, 0)), 0)
}

// FiletLabel returns the text of the bottom rule: the notice as a warning
// when there is one, otherwise the count with what scrolled out of view.
func FiletLabel(in Input) string {
	if in.Notice != "" {
		return theme.IconWarning + " " + in.Notice
	}
	in.Visible = max(in.Visible, 0)
	label := fmt.Sprintf("%d of %d", len(in.Rows), in.Total)
	switch {
	case in.Heading != "":
		label = in.Heading
	case strings.TrimSpace(in.Query) == "":
		label = fmt.Sprintf("%d available", len(in.Rows))
	}
	start := first(in)
	if above := start; above > 0 {
		label += fmt.Sprintf(" · ▲ %d more", above)
	}
	if below := len(in.Rows) - start - in.Visible; below > 0 {
		label += fmt.Sprintf(" · ▼ %d more", below)
	}
	return label
}

// look is one foreground/background combination, read from the theme at call
// time so that a theme applied after start-up is honored.
type look struct {
	name string
	fg   lipgloss.Color
	bg   lipgloss.Color
	bold bool
}

func (k look) style() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(k.fg).Background(k.bg).Bold(k.bold)
}

func rowLook() look {
	return look{name: "row", fg: theme.ColorText, bg: theme.ColorBackground}
}

func highlightLook() look {
	return look{name: "highlighted row", fg: theme.ColorText, bg: theme.ColorTableLineSelected, bold: true}
}

func argNoteLook() look {
	return look{name: "argument note", fg: theme.ColorText, bg: theme.ColorBackground}
}

func highlightedArgNoteLook() look {
	return look{name: "highlighted argument note", fg: theme.ColorText, bg: theme.ColorTableLineSelected}
}

func messageLook() look {
	return look{name: "message", fg: theme.ColorText, bg: theme.ColorBackground}
}

func filetLook() look {
	return look{name: "filet label", fg: theme.ColorText, bg: theme.ColorBackground}
}

func warningLook() look {
	return look{name: "filet warning", fg: theme.ColorFooterWarn, bg: theme.ColorBackground, bold: true}
}

// Palette lists every text pair the list paints, for the contrast test. The
// frame characters are decoration and carry no information, so they are not
// listed.
func Palette() []Pair {
	var out []Pair
	for _, k := range []look{rowLook(), highlightLook(), argNoteLook(), highlightedArgNoteLook(), messageLook(), filetLook(), warningLook()} {
		out = append(out, Pair{Name: k.name, Fg: k.fg, Bg: k.bg})
	}
	return out
}

// Render returns Visible list rows followed by the bottom rule, each exactly
// Width cells wide. Every line is assembled from separately styled segments;
// no already-styled text is ever cut.
func Render(in Input) []string {
	in.Visible = max(in.Visible, 0)
	if in.Width < minRenderWidth {
		return degenerate(in)
	}
	inner := max(in.Width-2, 0)
	layout := Columns(in.Width)
	border := lipgloss.NewStyle().Foreground(theme.ColorViewportBorder).Background(theme.ColorBackground)
	side := border.Render("│")

	lines := make([]string, 0, in.Visible+1)
	if len(in.Rows) == 0 {
		for i := 0; i < in.Visible; i++ {
			body := theme.EmptyLineBg(inner)
			if i == 0 {
				body = row(emptyText(in), layout, inner, false)
			}
			lines = append(lines, side+body+side)
		}
	} else {
		start := first(in)
		for i := 0; i < in.Visible; i++ {
			idx := start + i
			if idx >= len(in.Rows) {
				lines = append(lines, side+theme.EmptyLineBg(inner)+side)
				continue
			}
			lines = append(lines, side+row(columnsOf(in.Rows[idx], layout, inner), layout, inner, idx == in.Cursor)+side)
		}
	}
	return append(lines, filet(in, inner, border))
}

// degenerate renders the lines of a terminal too narrow for a frame: blank
// background lines and a bare rule, each exactly Width cells (possibly zero).
func degenerate(in Input) []string {
	w := max(in.Width, 0)
	lines := make([]string, 0, in.Visible+1)
	for i := 0; i < in.Visible; i++ {
		lines = append(lines, theme.EmptyLineBg(w))
	}
	rule := lipgloss.NewStyle().Foreground(theme.ColorViewportBorder).Background(theme.ColorBackground)
	return append(lines, rule.Render(strings.Repeat("─", w)))
}

// cells is the plain text of each column of one row.
// A message spans every column.
type cells struct {
	name, alias, desc, message string
	// arg marks an argument row: name and desc arrive already fitted.
	arg bool
}

// emptyText returns the message of a list with no rows: loading wins, then the
// caller's own wording, then the command stage's "No match".
func emptyText(in Input) cells {
	switch {
	case in.Loading:
		return cells{message: loadingText}
	case in.Empty != "":
		return cells{message: in.Empty}
	}
	return cells{message: fmt.Sprintf("No match for %q", in.Query)}
}

// columnsOf returns the plain text of a row for a layout. The current marker
// leads the description, or stands in for the aliases when the description
// is hidden; it is dropped when neither column exists.
func columnsOf(r Row, l Layout, inner int) cells {
	if r.Arg != nil {
		return argCells(r, l, inner)
	}
	c := cells{
		name:  r.Entry.Label(),
		alias: strings.Join(r.Entry.Aliases, aliasSep),
		desc:  r.Entry.Description,
	}
	if r.Current {
		switch {
		case l.Desc > 0:
			c.desc = strings.TrimSpace(currentTag + " " + c.desc)
		case l.Alias > 0:
			c.alias = currentTag
		}
	}
	return c
}

// fit truncates plain text to leave one cell of air inside its column and
// pads it to the full width.
func fit(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) > width-1 {
		text = ansi.Truncate(text, max(width-1, 1), ellipsis)
	}
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}

// argCells lays out an argument row: the value, then " (current)" glued to
// it, then the note. A long value gives way first, and the note disappears
// before either. The tag is kept whole only while the row has room for it
// (about 12 cells and more); below that the final fit() cuts the line.
func argCells(r Row, l Layout, inner int) cells {
	avail := max(inner-margin-markerWidth, 0)
	text := r.Arg.Value
	if r.Current {
		tag := " " + currentTag
		if room := avail - lipgloss.Width(tag) - 1; lipgloss.Width(text) > room {
			text = ansi.Truncate(text, max(room, 1), ellipsis)
		}
		text += tag
	}
	nameCol := min(max(lipgloss.Width(text)+1, l.Name), avail)
	c := cells{name: fit(text, nameCol), arg: true}
	if noteW := avail - nameCol; r.Arg.Note != "" && noteW >= minNoteWidth {
		c.desc = fit(r.Arg.Note, noteW)
	}
	return c
}

// row assembles one list line from styled segments, filled up to inner cells.
// A highlighted row fills with its own background so it reaches the border.
func row(c cells, l Layout, inner int, highlighted bool) string {
	k, note, marker := rowLook(), argNoteLook(), strings.Repeat(" ", markerWidth)
	if highlighted {
		k, note, marker = highlightLook(), highlightedArgNoteLook(), theme.IconCircleSmall+" "
	}
	if c.message != "" {
		k = messageLook()
		marker = strings.Repeat(" ", markerWidth)
	}
	if c.arg {
		return argLine(c, inner, k, note, marker)
	}
	body := fit(c.name, l.Name) + fit(c.alias, l.Alias) + fit(c.desc, l.Desc)
	if c.message != "" {
		body = fit(c.message, max(inner-margin-markerWidth, 0))
	}
	return k.style().Render(padTo(strings.Repeat(" ", margin)+marker+body, inner))
}

// argLine renders an argument row as two segments, the name and the note, so
// the note can carry its own look.
func argLine(c cells, inner int, k, note look, marker string) string {
	head := strings.Repeat(" ", margin) + marker + c.name
	if c.desc == "" {
		return k.style().Render(padTo(head, inner))
	}
	return k.style().Render(head) + note.style().Render(padTo(c.desc, inner-lipgloss.Width(head)))
}

// padTo pads plain text with spaces up to width cells.
func padTo(plain string, width int) string {
	return plain + strings.Repeat(" ", max(width-lipgloss.Width(plain), 0))
}

// filet builds the bottom rule with its label.
func filet(in Input, inner int, border lipgloss.Style) string {
	left, right := "├", "┤"
	if in.Frame == FrameClosed {
		left, right = "└", "┘"
	}
	k := filetLook()
	if in.Notice != "" {
		k = warningLook()
	}
	label := ansi.Truncate(FiletLabel(in), max(inner-3, 0), ellipsis)
	dashes := max(inner-2-lipgloss.Width(label)-1, 0)
	return border.Render(left+"─ ") + k.style().Render(label) +
		border.Render(" "+strings.Repeat("─", dashes)+right)
}
