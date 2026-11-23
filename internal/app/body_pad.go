package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// styleBody paints a view's content on the viewport's background and pads every
// line to width with it (Rule 115). It is the result of
//
//	lipgloss.NewStyle().Background(bg).Foreground(fg).Width(width).Render(content)
//
// byte for byte, which is what the cell goldens hold, without that call's word
// wrap and its pen tracking when the content cannot need them (DEVDESK-281,
// task 281.4). A line wider than width is wrapped by lipgloss, so any such
// content takes the lipgloss call itself.
func styleBody(content string, width int) string {
	style := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
	if out, ok := padBody(content, width, style); ok {
		return out
	}
	return style.Width(width).Render(content)
}

// padBody is styleBody's fast path. It reports false, and the caller falls back
// to lipgloss, unless every line is one lipgloss would leave as it is: no wider
// than width, only printable characters and CSI sequences, and ending with the
// pen at its default (a style left open on one line is re-opened on the next by
// lipgloss's wrap writer, which this path does not do).
func padBody(content string, width int, style lipgloss.Style) (string, bool) {
	if width <= 0 {
		return "", false
	}
	open, shut, ok := strings.Cut(style.Render("x"), "x")
	if !ok || strings.Contains(shut, "x") {
		return "", false
	}
	padOpen, padShut, ok := strings.Cut(lipgloss.NewStyle().Background(theme.ColorBackground).Render(" "), " ")
	if !ok || padShut != shut {
		return "", false
	}

	var b strings.Builder
	b.Grow(len(content) + (strings.Count(content, "\n")+1)*(len(open)+len(padOpen)+2*len(shut)+width))
	for rest, first := content, true; ; first = false {
		line, after, more := strings.Cut(rest, "\n")
		w, plain := scanBodyLine(line)
		if !plain || w > width {
			return "", false
		}
		if !first {
			b.WriteByte('\n')
		}
		b.WriteString(open)
		b.WriteString(line)
		b.WriteString(shut)
		if pad := width - w; pad > 0 {
			b.WriteString(padOpen)
			writeSpaces(&b, pad)
			b.WriteString(shut)
		}
		if !more {
			return b.String(), true
		}
		rest = after
	}
}

// scanBodyLine returns the width of line in cells and whether it is a line
// padBody may pass through: printable characters and complete CSI sequences
// only (no control character, no OSC, no tab, no CR), and the pen at its default
// at the end, that is the last SGR sequence, if any, a reset. The test is
// deliberately narrow; whatever it refuses lipgloss handles.
func scanBodyLine(line string) (width int, plain bool) {
	nonASCII, pen := false, true // pen: still at its default
	for i := 0; i < len(line); {
		c := line[i]
		switch {
		case c == 0x1b:
			if i+1 >= len(line) || line[i+1] != '[' {
				return 0, false
			}
			j := i + 2
			for j < len(line) && line[j] >= 0x20 && line[j] <= 0x3f { // parameters and intermediates
				j++
			}
			if j >= len(line) || line[j] < 0x40 || line[j] > 0x7e {
				return 0, false
			}
			if line[j] == 'm' {
				params := line[i+2 : j]
				pen = params == "" || params == "0"
			}
			i = j + 1
		case c < 0x20 || c == 0x7f:
			return 0, false
		case c >= 0x80:
			nonASCII = true
			i++
		default:
			width++
			i++
		}
	}
	if !pen {
		return 0, false
	}
	if nonASCII {
		width = ansi.StringWidth(line)
	}
	return width, true
}

const spaces = "                                                                "

// writeSpaces writes n spaces.
func writeSpaces(b *strings.Builder, n int) {
	for ; n > len(spaces); n -= len(spaces) {
		b.WriteString(spaces)
	}
	b.WriteString(spaces[:n])
}
