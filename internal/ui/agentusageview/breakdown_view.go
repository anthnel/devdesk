package agentusageview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	breakdownMargin   = 1 // blank cells before every line
	breakdownGap      = 2 // blank cells before each numeric column
	breakdownShareW   = 6 // "100.0%"
	breakdownMinToken = 6 // "Tokens"
	breakdownMinLabel = 8 // below this, a numeric column is dropped
	swatchWidth       = 2 // glyph + space
)

// segment is a run of plain text and the style applied once it is measured
// (Rule 122).
type segment struct {
	text  string
	style lipgloss.Style
}

// columns is which numeric columns fit and how wide Tokens is.
type columns struct {
	share, tokens bool
	tokenW        int
}

// renderBreakdown renders the panel as lines exactly width cells wide. Text is
// measured as plain and styled afterwards (Rule 122). A block is a header
// line, its lines and a Total line; blocks are separated by a blank line. A
// session with no token shows "-" for every share, Total included.
func renderBreakdown(b breakdown, width int) []string {
	cols := fitColumns(b, width)
	var out []string
	for i, blk := range b.blocks {
		if i > 0 {
			out = append(out, theme.EmptyLineBg(width))
		}
		out = append(out, renderBlock(blk, cols, width)...)
	}
	return out
}

// fitColumns drops Tokens, then Share, until every block keeps at least
// breakdownMinLabel cells for its label. The widest case (a category line,
// which carries a swatch) decides, so all blocks show the same columns.
func fitColumns(b breakdown, width int) columns {
	c := columns{share: true, tokens: true, tokenW: tokenWidth(b)}
	if labelWidth(width, swatchWidth, c) < breakdownMinLabel {
		c.tokens = false
	}
	if labelWidth(width, swatchWidth, c) < breakdownMinLabel {
		c.share = false
	}
	return c
}

// tokenWidth is the Tokens column width: the longest figure of any block.
func tokenWidth(b breakdown) int {
	w := breakdownMinToken
	for _, blk := range b.blocks {
		w = max(w, len(formatTokens(blk.total)))
		for _, l := range blk.lines {
			w = max(w, len(formatTokens(l.tokens)))
		}
	}
	return w
}

// labelWidth is what is left for the label after the margin, the swatch and
// the numeric columns.
func labelWidth(width, swatch int, c columns) int {
	w := width - breakdownMargin - swatch
	if c.share {
		w -= breakdownGap + breakdownShareW
	}
	if c.tokens {
		w -= breakdownGap + c.tokenW
	}
	return max(w, 0)
}

// renderBlock is the header, the lines and the Total line of one block.
func renderBlock(blk breakdownBlock, c columns, width int) []string {
	hasSwatch := len(blk.lines) > 0 && blk.lines[0].role != ""
	swatch := 0
	if hasSwatch {
		swatch = swatchWidth
	}
	lw := labelWidth(width, swatch, c)
	head := theme.HeaderStyle.Bold(true).Foreground(theme.ColorSecondary)

	out := []string{renderSegments(headerSegments(blk.title, head, hasSwatch, lw, c), width)}
	for _, l := range blk.lines {
		out = append(out, renderSegments(lineSegments(l, lw, c), width))
	}
	total := breakdownLine{label: "Total", tokens: blk.total, tenths: 1000}
	if blk.total == 0 {
		total.tenths = noShare
	}
	if hasSwatch {
		out = append(out, renderSegments(append([]segment{{text: strings.Repeat(" ", swatchWidth)}}, lineSegments(total, lw, c)...), width))
	} else {
		out = append(out, renderSegments(lineSegments(total, lw, c), width))
	}
	return out
}

// headerSegments is the block title with the Share and Tokens column titles.
func headerSegments(title string, st lipgloss.Style, swatch bool, lw int, c columns) []segment {
	segs := []segment{{text: strings.Repeat(" ", breakdownMargin)}}
	if swatch {
		segs = append(segs, segment{text: strings.Repeat(" ", swatchWidth)})
	}
	segs = append(segs, segment{text: fit(title, lw), style: st})
	if c.share {
		segs = append(segs, segment{text: strings.Repeat(" ", breakdownGap)},
			segment{text: fmt.Sprintf("%*s", breakdownShareW, "Share"), style: st})
	}
	if c.tokens {
		segs = append(segs, segment{text: strings.Repeat(" ", breakdownGap)},
			segment{text: fmt.Sprintf("%*s", c.tokenW, "Tokens"), style: st})
	}
	return segs
}

// lineSegments is one entry: margin, swatch (category only), label, columns.
func lineSegments(l breakdownLine, lw int, c columns) []segment {
	segs := []segment{{text: strings.Repeat(" ", breakdownMargin)}}
	if l.role != "" {
		segs = append(segs, segment{text: theme.GaugeFill(1), style: theme.TokenStyle(l.role)},
			segment{text: " "})
	}
	segs = append(segs, segment{text: fit(l.label, lw)})
	if c.share {
		share := formatShare(l.tenths)
		segs = append(segs, segment{text: strings.Repeat(" ", breakdownGap)},
			segment{text: fmt.Sprintf("%*s", breakdownShareW, share), style: dimIf(l.tenths <= 0)})
	}
	if c.tokens {
		segs = append(segs, segment{text: strings.Repeat(" ", breakdownGap)},
			segment{text: fmt.Sprintf("%*s", c.tokenW, formatTokens(l.tokens)), style: dimIf(l.tokens == 0)})
	}
	return segs
}

// dimIf is DimStyle for zeros and placeholders, the plain style otherwise.
func dimIf(dim bool) lipgloss.Style {
	if dim {
		return theme.DimStyle
	}
	return lipgloss.NewStyle()
}

// fit truncates s to w cells with an ellipsis, then pads it to exactly w.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// renderSegments styles each measured run on the app background and pads the
// line to exactly width cells, cutting it if the runs overflow.
func renderSegments(segs []segment, width int) string {
	var sb strings.Builder
	used := 0
	for _, s := range segs {
		text := ansi.Truncate(s.text, max(width-used, 0), "")
		if text == "" {
			continue
		}
		used += ansi.StringWidth(text)
		sb.WriteString(s.style.Background(theme.ColorBackground).Render(text))
	}
	return theme.PadWithBg(sb.String(), width)
}

// clampTop keeps a scroll offset within [0, max(0, lines-height)].
func clampTop(top, lines, height int) int {
	return min(max(top, 0), max(lines-height, 0))
}
