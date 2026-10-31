// Package overlay draws a box over already-rendered, ANSI-styled lines
// without cutting an escape sequence or shifting a wide rune.
package overlay

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// sgrReset closes every SGR attribute in effect.
const sgrReset = "\x1b[0m"

// Compose draws box over base, the top-left corner of the box landing at
// column x and row y. Rows of base that the box does not touch are returned
// as they are; base is never modified. Every row the box touches is rewritten
// to exactly width cells. A row on which the box cannot be spliced (it is
// wider than the room right of x) falls back to the box cut to the room right
// of x and spliced at x, so one failing row never spoils the others.
//
// Lines are single-row: a newline or a tab in base or box counts for no width
// (ansi.StringWidth) and is not supported.
//
// Nothing is drawn, and base is returned as is, when the box is empty, x is
// negative, or width is not positive. Rows of the box that fall above or
// below base are dropped.
func Compose(base, box []string, x, y, width int) []string {
	if len(box) == 0 || x < 0 || width <= 0 || len(base) == 0 {
		return base
	}
	out := make([]string, len(base))
	copy(out, base)
	for i, boxLine := range box {
		row := y + i
		if row < 0 || row >= len(out) {
			continue
		}
		if boxLine == "" {
			continue
		}
		spliced, ok := Splice(out[row], boxLine, x, width)
		if !ok {
			spliced = spliceCut(out[row], boxLine, x, width)
		}
		out[row] = spliced
	}
	return out
}

// spliceCut is the per-row fallback of Compose: the box is cut to the room
// left of x and spliced at x, so the row keeps its left part. A row with no
// room at all is only fitted to width.
func spliceCut(line, box string, x, width int) string {
	if x >= width {
		return fit(line, width)
	}
	cut := ansi.Truncate(box, width-x, "")
	out, ok := Splice(line, cut, x, width)
	if !ok {
		return fit(line, width)
	}
	return out
}

// Splice replaces the cells of line from column x onward with box, and returns
// a line of exactly width cells. The base style active at the cut is restored
// after the box, and a wide rune cut in two is replaced by one space. It
// reports false, returning box cut to width, when box does not fit between x
// and width. Like Compose, it does not support newlines or tabs.
func Splice(line, box string, x, width int) (string, bool) {
	bw := ansi.StringWidth(box)
	if x < 0 || width <= 0 || x+bw > width {
		return fit(box, width), false
	}
	line = fit(line, width)
	if bw == 0 {
		return line, true
	}

	left := ansi.Truncate(line, x, "")
	leftPad := bgSpaces(x - ansi.StringWidth(left))

	end := x + bw
	right := ansi.TruncateLeft(line, end, "")
	// A wide rune straddling the cut may be kept whole by TruncateLeft: drop
	// one more cell until the right part fits, and let a space stand in.
	for cut := end; ansi.StringWidth(right) > width-end; {
		cut++
		right = ansi.TruncateLeft(line, cut, "")
	}
	rightPad := width - end - ansi.StringWidth(right)

	var b strings.Builder
	b.WriteString(left)
	b.WriteString(sgrReset)
	b.WriteString(leftPad)
	b.WriteString(box)
	b.WriteString(sgrReset)
	if active := activeSGR(line, end); active != "" {
		// The compensation cell keeps the style the base had at the cut.
		b.WriteString(active)
		b.WriteString(strings.Repeat(" ", rightPad))
	} else {
		b.WriteString(bgSpaces(rightPad))
	}
	b.WriteString(right)
	return b.String(), true
}

// fit cuts s to width cells and pads it with spaces when it is shorter.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "")
	if w := ansi.StringWidth(s); w < width {
		s += sgrReset + bgSpaces(width-w)
	}
	return s
}

// bgSpaces returns n spaces painted with the app background, so that a cell the
// overlay invents never shows the terminal's own background (Rule 115).
func bgSpaces(n int) string {
	if n <= 0 {
		return ""
	}
	return theme.Bg(strings.Repeat(" ", n))
}

// activeSGR returns the SGR sequence in effect at column col of line. It only
// serves the compensation cell of a wide rune cut in two, which takes the
// base style at the cut; the right part itself gets its styling replayed by
// ansi.TruncateLeft. It is empty when nothing is active, and the last
// sequence since the latest reset wins.
func activeSGR(line string, col int) string {
	var (
		active string
		pos    int
		state  byte
		rest   = line
	)
	for len(rest) > 0 && pos < col {
		seq, width, n, newState := ansi.DecodeSequence(rest, state, nil)
		state = newState
		if width == 0 && isSGR(seq) {
			active = style(seq)
		}
		pos += width
		rest = rest[n:]
	}
	// Sequences sitting exactly at the cut still belong to the left side.
	for len(rest) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(rest, state, nil)
		if width != 0 || !isSGR(seq) {
			break
		}
		state = newState
		active = style(seq)
		rest = rest[n:]
	}
	return active
}

// isSGR reports whether seq is a Select Graphic Rendition sequence.
func isSGR(seq string) bool {
	return strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m")
}

// isReset reports whether seq clears every attribute.
func isReset(seq string) bool {
	return seq == "\x1b[m" || seq == "\x1b[0m"
}

// style returns the style in effect once seq has been applied.
func style(seq string) string {
	if isReset(seq) {
		return ""
	}
	return seq
}
