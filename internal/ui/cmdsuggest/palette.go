package cmdsuggest

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Palette geometry, in cells and rows.
const (
	// paletteChrome is the rows around the list: title, input, separator, rule.
	paletteChrome = 4
	// reducedHeight is title, input and rule, with no list.
	reducedHeight = 3
	// narrowWidth is the width below which the frame takes the whole line.
	narrowWidth = 54
	// maxPaletteWidth caps the frame on wide terminals.
	maxPaletteWidth = 80
	// sideMargin is the cells left free on each side of a framed palette.
	sideMargin = 2
)

// Rect is where the palette sits on screen, in cells and rows. Rows is the
// number of suggestion rows; Height is the whole frame.
type Rect struct{ X, Y, Width, Height, Rows int }

// PaletteInput is everything RenderPalette needs.
type PaletteInput struct {
	Title string // "Go to" or "Contexts"
	Input string // the input line, already rendered by the caller
	List  Input  // rows, cursor, notice; Width and Visible are set from Rect
}

// PaletteRows returns the number of suggestion rows for v free lines: a
// constant of the geometry, independent of the number of matches.
func PaletteRows(v int) int {
	return max(min(MaxVisibleRows, v-paletteChrome), 0)
}

// Geometry places the palette for a width x height terminal whose header
// block is headerLines high and whose view footer is footerLines high. It
// never returns a negative field.
func Geometry(width, height, headerLines, footerLines int) Rect {
	width, height = max(width, 0), max(height, 0)
	headerLines, footerLines = max(headerLines, 0), max(footerLines, 0)

	rows := PaletteRows(height - headerLines - footerLines)
	h := reducedHeight
	if rows > 0 {
		h = rows + paletteChrome
	}
	w := width
	if width >= narrowWidth {
		w = min(width-2*sideMargin, maxPaletteWidth)
	}
	y := headerLines
	if y+h > height {
		// Pull up onto the blank line under the header; the rest is clipped.
		y = max(headerLines-1, 0)
	}
	return Rect{X: (width - w) / 2, Y: y, Width: w, Height: h, Rows: rows}
}

func titleLook() look {
	return look{name: "palette title", fg: theme.ColorText, bg: theme.ColorBackground, bold: true}
}

func inputLook() look {
	return look{name: "palette input", fg: theme.ColorText, bg: theme.ColorBackground}
}

// BorderPalette lists the non-text pairs of the frame (border and prompt
// glyph), checked at 3:1 rather than 4.5:1.
func BorderPalette() []Pair {
	return []Pair{
		{Name: "palette border", Fg: theme.ColorPaletteBorder, Bg: theme.ColorBackground},
		{Name: "palette prompt glyph", Fg: theme.ColorPaletteBorder, Bg: theme.ColorBackground},
	}
}

// RenderPalette returns r.Height lines of exactly r.Width cells, each with an
// explicit background: title border, input, then (when r.Rows > 0) a
// separator and the rows, and the bottom rule carrying the status.
func RenderPalette(p PaletteInput, r Rect) []string {
	if r.Width < minRenderWidth {
		return blankLines(r)
	}
	inner := r.Width - 2
	border := lipgloss.NewStyle().Foreground(theme.ColorPaletteBorder).Background(theme.ColorBackground)

	lines := []string{titleLine(p.Title, inner, border), inputRow(p.Input, inner, border)}
	if r.Rows > 0 {
		lines = append(lines, border.Render("├"+strings.Repeat("─", inner)+"┤"))
	}
	list := p.List
	list.Width, list.Visible = r.Width, r.Rows
	lines = append(lines, renderWith(list, theme.ColorPaletteBorder)...)
	return lines
}

// blankLines is the palette on a terminal too narrow for a frame.
func blankLines(r Rect) []string {
	lines := make([]string, r.Height)
	for i := range lines {
		lines[i] = theme.EmptyLineBg(max(r.Width, 0))
	}
	return lines
}

// titleLine builds ┌─ Title ───┐ from separately styled segments.
func titleLine(title string, inner int, border lipgloss.Style) string {
	title = ansi.Truncate(title, max(inner-3, 0), ellipsis)
	dashes := max(inner-2-lipgloss.Width(title)-1, 0)
	return border.Render("┌─ ") + titleLook().style().Render(title) +
		border.Render(" "+strings.Repeat("─", dashes)+"┐")
}

// inputRow frames the caller's rendered input with a one-cell margin and
// pads it with background up to the border.
func inputRow(input string, inner int, border lipgloss.Style) string {
	side := border.Render("│")
	room := max(inner-margin, 0)
	if lipgloss.Width(input) > room {
		input = ansi.Truncate(input, room, "")
	}
	pad := inputLook().style().Render(strings.Repeat(" ", margin))
	fill := inputLook().style().Render(strings.Repeat(" ", max(room-lipgloss.Width(input), 0)))
	return side + pad + input + fill + side
}
