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
	Rows    []Row
	Total   int
	Query   string
	Cursor  int // -1 means no highlighted row
	Offset  int
	Notice  string
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
	return max(min(in.Offset, len(in.Rows)-in.Visible), 0)
}

// FiletLabel returns the text of the bottom rule: the notice as a warning
// when there is one, otherwise the count with what scrolled out of view.
func FiletLabel(in Input) string {
	if in.Notice != "" {
		return theme.IconWarning + " " + in.Notice
	}
	label := fmt.Sprintf("%d of %d", len(in.Rows), in.Total)
	if strings.TrimSpace(in.Query) == "" {
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
	for _, k := range []look{rowLook(), highlightLook(), filetLook(), warningLook()} {
		out = append(out, Pair{Name: k.name, Fg: k.fg, Bg: k.bg})
	}
	return out
}

// Render returns Visible list rows followed by the bottom rule, each exactly
// Width cells wide. Every line is assembled from separately styled segments;
// no already-styled text is ever cut.
func Render(in Input) []string {
	inner := max(in.Width-2, 0)
	layout := Columns(in.Width)
	border := lipgloss.NewStyle().Foreground(theme.ColorViewportBorder).Background(theme.ColorBackground)
	side := border.Render("│")

	lines := make([]string, 0, in.Visible+1)
	if len(in.Rows) == 0 {
		lines = append(lines, side+row(noMatchText(in.Query), layout, inner, false)+side)
		for i := 1; i < in.Visible; i++ {
			lines = append(lines, side+theme.EmptyLineBg(inner)+side)
		}
	} else {
		start := first(in)
		for i := 0; i < in.Visible; i++ {
			idx := start + i
			if idx >= len(in.Rows) {
				lines = append(lines, side+theme.EmptyLineBg(inner)+side)
				continue
			}
			lines = append(lines, side+row(columnsOf(in.Rows[idx], layout), layout, inner, idx == in.Cursor)+side)
		}
	}
	return append(lines, filet(in, inner, border))
}

// cells is the plain text of each column of one row.
// A message spans every column.
type cells struct{ name, alias, desc, message string }

func noMatchText(query string) cells {
	return cells{message: fmt.Sprintf("No match for %q", query)}
}

// columnsOf returns the plain text of a row for a layout. The current marker
// leads the description, or stands in for the aliases when the description
// is hidden; it is dropped when neither column exists.
func columnsOf(r Row, l Layout) cells {
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

// row assembles one list line from styled segments, filled up to inner cells.
// A highlighted row fills with its own background so it reaches the border.
func row(c cells, l Layout, inner int, highlighted bool) string {
	k, marker := rowLook(), strings.Repeat(" ", markerWidth)
	if highlighted {
		k, marker = highlightLook(), theme.IconCircleSmall+" "
	}
	st := k.style()
	body := fit(c.name, l.Name) + fit(c.alias, l.Alias) + fit(c.desc, l.Desc)
	if c.message != "" {
		body = fit(c.message, max(inner-margin-markerWidth, 0))
	}
	plain := strings.Repeat(" ", margin) + marker + body
	if w := lipgloss.Width(plain); w < inner {
		plain += strings.Repeat(" ", inner-w)
	}
	return st.Render(plain)
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
