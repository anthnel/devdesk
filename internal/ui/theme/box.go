package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderTitledBox draws a box of exactly `width` cells per line, carrying its
// title on the top border. It returns len(lines)+2 lines: the titled top
// border, one line per content line, and the bottom border.
//
// The content is separated from the side borders by one column of space on
// each side (BoxPadding), so the usable width is BoxContentWidth(width). The
// padding is **horizontal only**: a blank line at top and bottom would cost
// four lines per row of boxes, and on a 30-line terminal the budget is worth
// exactly two.
//
// Each line carries an explicit background (Rule 115) and is exactly `width`
// cells wide (Rule 116): the content is padded, and truncated if it
// overflows — a frame whose line overflows breaks every column to its right.
func RenderTitledBox(title string, lines []string, width int) []string {
	if width < minBoxWidth {
		width = minBoxWidth
	}
	content := BoxContentWidth(width)

	borderStyle := lipgloss.NewStyle().
		Foreground(ColorViewportBorder).
		Background(ColorBackground)
	border := lipgloss.NormalBorder()
	side := borderStyle.Render(border.Left)
	pad := EmptyLineBg(BoxPadding)

	out := make([]string, 0, len(lines)+2+BoxTopPadding)
	out = append(out, RenderBorderTitle(title, width))
	for range BoxTopPadding {
		out = append(out, side+EmptyLineBg(width-2)+side)
	}
	for _, line := range lines {
		out = append(out, side+pad+PadWithBg(Truncate(line, content), content)+pad+side)
	}
	out = append(out, borderStyle.Render(
		border.BottomLeft+strings.Repeat(border.Bottom, width-2)+border.BottomRight))
	return out
}

// BoxPadding is the space between a box's side borders and its content, per
// side. BoxTopPadding is the blank line between the titled border and the first
// content line — the title sits *on* the border, so without it the first
// line would read as a continuation of the title.
//
// There is no bottom padding: the title is only at the top, and one more
// line would cost two lines per row of boxes on a budget that counts 17.
const (
	BoxPadding    = 1
	BoxTopPadding = 1
)

// BoxChrome is what a box costs in lines beyond its content: two borders and
// the top padding. Views that budget a height account for it.
const BoxChrome = 2 + BoxTopPadding

// BoxContentWidth returns the usable width inside a box of the given total
// width — borders and padding removed.
func BoxContentWidth(width int) int {
	return max(width-2-2*BoxPadding, 1)
}

// RenderTitledRule draws a titled horizontal rule with no corners, of exactly
// `width` cells. This is the title of a view without a frame: GetTitle()
// keeps a reader oriented where RenderBorderTitle would draw the top of a
// box that doesn't exist.
func RenderTitledRule(title string, width int) string {
	borderStyle := lipgloss.NewStyle().
		Foreground(ColorViewportBorder).
		Background(ColorBackground)
	titleStyle := lipgloss.NewStyle().
		Foreground(ColorTitleFg).
		Background(ColorBackground).
		Bold(true)

	rendered := titleStyle.Render(title)
	// " " + title + " " then the fill up to width.
	used := 1 + lipgloss.Width(rendered) + 1
	fill := max(width-used, 0)
	return Bg(" ") + rendered + Bg(" ") +
		borderStyle.Render(strings.Repeat(lipgloss.NormalBorder().Top, fill))
}

// minBoxWidth is the narrowest box that can still show two borders and a
// character between them.
const minBoxWidth = 3

// Truncate cuts a line to at most `width` cells. lipgloss.MaxWidth cuts while
// accounting for ANSI sequences: cutting by hand would bleed into an escape
// sequence and onto every line that follows (Rule 122). Any view that
// assembles columns must go through it rather than through a slice.
func Truncate(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}
