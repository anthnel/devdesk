package netdiag

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	pickerTitle       = "Trace from"
	pickerEllipsis    = "…"
	pickerCurrentMark = " (current)"
	pickerImageSep    = " · "
	pickerMinBox      = 20
	pickerMaxBox      = 60
	pickerSideMargin  = 4 // cells of the terminal left around the box
	pickerFrameRows   = 2 // top and bottom borders
)

// pathSourcePicker is the "Trace from" selector: a value type with no I/O.
// Every method returns a new value; the table stays the body (Rule 139) and
// the caller draws these lines over it. Loading and notes belong to the footer
// (Rule 128), so they never appear in render.
type pathSourcePicker struct {
	sources []pathSource // this machine first
	current string       // ID of the source in use ("" = this machine)
	cursor  int
	top     int
	loading bool
	note    string
}

// newPathSourcePicker returns a picker offering only this machine, still
// waiting for the container list.
func newPathSourcePicker(current pathSource) pathSourcePicker {
	return pathSourcePicker{
		sources: []pathSource{hostSource()},
		current: current.ID,
		loading: true,
	}
}

// withList returns the picker with the containers of l after this machine. The
// cursor lands on the current source, or on this machine when it is gone.
func (p pathSourcePicker) withList(l pathSourceList) pathSourcePicker {
	p.sources = append([]pathSource{hostSource()}, l.Containers...)
	p.cursor = 0
	for i, s := range p.sources {
		if s.ID == p.current {
			p.cursor = i
			break
		}
	}
	p.top = 0
	p.loading = false
	p.note = l.Note
	return p
}

// move applies a navigation key with rows visible lines and returns the new
// picker. Keys that are not navigation leave it unchanged.
func (p pathSourcePicker) move(key string, rows int) pathSourcePicker {
	rows = max(rows, 1)
	last := len(p.sources) - 1
	switch key {
	case "up":
		p.cursor--
	case "down":
		p.cursor++
	case "pgup":
		p.cursor -= rows
	case "pgdown":
		p.cursor += rows
	case "home":
		p.cursor = 0
	case "end":
		p.cursor = last
	default:
		return p
	}
	p.cursor = min(max(p.cursor, 0), max(last, 0))
	p.top = p.window(rows)
	return p
}

// window returns the first visible index for rows lines, keeping the cursor in
// view while moving as little as possible. top is also capped so a window that
// grew never leaves blank room below the last entry.
func (p pathSourcePicker) window(rows int) int {
	top := min(p.top, p.cursor)
	if p.cursor >= top+rows {
		top = p.cursor - rows + 1
	}
	return max(min(top, len(p.sources)-rows), 0)
}

// selected returns the source under the cursor.
func (p pathSourcePicker) selected() pathSource {
	if p.cursor < 0 || p.cursor >= len(p.sources) {
		return hostSource()
	}
	return p.sources[p.cursor]
}

// render returns the framed selector, one string per line, each exactly
// pickerBoxWidth(width) cells wide. height is the room available, borders
// included.
func (p pathSourcePicker) render(width, height int) []string {
	box := pickerBoxWidth(width)
	inner := box - 2
	rows := min(len(p.sources), max(height-pickerFrameRows, 1))
	top := p.window(rows)

	border := lipgloss.NewStyle().Foreground(theme.ColorPaletteBorder).Background(theme.ColorBackground)
	lines := make([]string, 0, rows+pickerFrameRows)
	lines = append(lines, pickerTopBorder(inner, border))
	for i := top; i < top+rows && i < len(p.sources); i++ {
		lines = append(lines, border.Render("│")+p.renderRow(i, inner)+border.Render("│"))
	}
	return append(lines, border.Render("└"+strings.Repeat("─", inner)+"┘"))
}

// pickerTopBorder draws ┌─ Trace from ───┐ in inner+2 cells.
func pickerTopBorder(inner int, border lipgloss.Style) string {
	dashes := max(inner-ansi.StringWidth(pickerTitle)-3, 0)
	return border.Render("┌─ " + pickerTitle + " " + strings.Repeat("─", dashes) + "┐")
}

// renderRow draws one entry padded to inner cells; the cursor row carries the
// focus mark and the highlight colour.
func (p pathSourcePicker) renderRow(i, inner int) string {
	s := p.sources[i]
	prefix, color := "  ", theme.ColorText
	if i == p.cursor {
		prefix, color = theme.IconCircleSmall+" ", theme.ColorHighlight
	}
	text := prefix + s.label()
	if s.ID == p.current {
		text += pickerCurrentMark
	}
	if !s.isHost() && s.Image != "" {
		text += pickerImageSep + s.Image
	}
	text = ansi.Truncate(text, inner, pickerEllipsis)
	text += strings.Repeat(" ", max(inner-ansi.StringWidth(text), 0))
	return lipgloss.NewStyle().Foreground(color).Background(theme.ColorBackground).Render(text)
}

// pickerBoxWidth is the frame width: the terminal width minus a margin, kept
// between pickerMinBox and pickerMaxBox.
func pickerBoxWidth(width int) int {
	return min(max(width-pickerSideMargin, pickerMinBox), pickerMaxBox)
}
