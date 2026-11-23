package testutil

import (
	"fmt"
	"image/color"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Cell decoding (DEVDESK-281). A rendered frame is a string of text and SGR
// sequences; two frames can differ byte for byte (a redundant reset, a colour
// written as 24-bit or as the same palette entry) and still paint the same
// screen. A test that guards a change to how a frame is built therefore
// compares cells: what the terminal would show at each position.
//
// The decoder walks the frame with x/ansi's sequence decoder (the same
// grapheme clustering and widths Lip Gloss and the viewport measure with) and
// follows the SGR state. It tracks the foreground, the background and reverse
// video, nothing else: bold, underline and the like do not change which colour
// a cell has, which is what the Rule 115 check and the golden compare. Every
// sequence that is not an SGR (cursor moves, erases, OSC titles, hyperlinks,
// progress) is skipped and paints nothing.

// DefaultColor is the colour of a cell no sequence has coloured: the
// terminal's own foreground or background.
const DefaultColor = ""

// Colour names a Cell carries. A colour is DefaultColor, "#rrggbb" for a 24-bit
// one, "ansi:N" for palette entries 0 to 15 (SGR 30-37, 90-97, 40-47, 100-107
// and 38;5;N or 48;5;N with N below 16, which a terminal draws the same) and
// "idx:N" for entries 16 to 255. Under reverse video the two colours swap; a
// default one that lands on the other side is not the terminal's background
// (nor foreground) any more, so it is named for what it now is:
const (
	// ReversedDefaultFG is the background of a reversed cell whose foreground was
	// the terminal's default: the terminal's default foreground, as a fill.
	ReversedDefaultFG = "default-fg"
	// ReversedDefaultBG is the foreground of a reversed cell whose background was
	// the terminal's default.
	ReversedDefaultBG = "default-bg"
)

// Cell is one terminal cell: the grapheme drawn there and its colours.
// A grapheme wider than one cell occupies several: the first holds the glyph,
// the following ones have an empty Glyph and the same colours.
type Cell struct {
	Glyph  string
	FG, BG string
}

// Pos is a cell's position in a frame, zero-based.
type Pos struct{ Row, Col int }

// Cells decodes a frame into one row of cells per line. A frame without a
// newline is one row; an empty frame is one empty row. The rows are as long as
// what was written: a row shorter than the screen is not padded.
func Cells(frame string) [][]Cell {
	rows := [][]Cell{nil}
	var st sgrState
	p := ansi.NewParser()
	var state byte
	for rest := frame; len(rest) > 0; {
		var seq string
		var width, n int
		seq, width, n, state = ansi.DecodeSequence(rest, state, p)
		rest = rest[n:]
		switch {
		case seq == "\n":
			rows = append(rows, nil)
		case width > 0:
			fg, bg := st.colours()
			last := len(rows) - 1
			rows[last] = append(rows[last], Cell{Glyph: seq, FG: fg, BG: bg})
			for i := 1; i < width; i++ {
				rows[last] = append(rows[last], Cell{FG: fg, BG: bg})
			}
		case width == 0 && isZeroWidthText(seq):
			// A combining mark or joiner the decoder did not cluster with the
			// ASCII letter before it: it belongs to that cell.
			attach(rows[len(rows)-1], seq)
		case isSGR(seq, p):
			st.apply(p.Params())
		}
	}
	return rows
}

// isZeroWidthText reports whether seq is zero-width printable text (a combining
// mark, a joiner, a variation selector), as opposed to a control or an escape
// sequence.
func isZeroWidthText(seq string) bool {
	r, _ := utf8.DecodeRuneInString(seq)
	return r >= 0xA0 && r != utf8.RuneError
}

// attach appends seq to the glyph of the last glyph-bearing cell of row. In an
// empty row it is dropped: there is nothing to combine with.
func attach(row []Cell, seq string) {
	for i := len(row) - 1; i >= 0; i-- {
		if row[i].Glyph != "" {
			row[i].Glyph += seq
			return
		}
	}
}

// BareCells returns the position of every cell whose background is the
// terminal's default, in reading order. A frame that sets its own background
// everywhere returns none (Rule 115). A cell the frame never wrote, because a
// row is shorter than the screen, is not in the frame and so not in the result:
// the caller checks the row lengths against the width it expects.
func BareCells(frame string) []Pos {
	var out []Pos
	for r, row := range Cells(frame) {
		for c, cell := range row {
			if cell.BG == DefaultColor {
				out = append(out, Pos{Row: r, Col: c})
			}
		}
	}
	return out
}

// isSGR reports whether seq, just decoded into p, is a plain CSI ... m.
func isSGR(seq string, p *ansi.Parser) bool {
	if !strings.HasPrefix(seq, "\x1b[") {
		return false
	}
	cmd := ansi.Cmd(p.Command())
	return cmd.Final() == 'm' && cmd.Prefix() == 0 && cmd.Intermediate() == 0
}

// sgrState is the colour part of the graphic rendition.
type sgrState struct {
	fg, bg  string
	reverse bool
}

// colours returns the colours a cell drawn now has.
func (s *sgrState) colours() (fg, bg string) {
	if !s.reverse {
		return s.fg, s.bg
	}
	fg, bg = s.bg, s.fg
	if fg == DefaultColor {
		fg = ReversedDefaultBG
	}
	if bg == DefaultColor {
		bg = ReversedDefaultFG
	}
	return fg, bg
}

// apply follows one SGR sequence. An unreadable colour ends the sequence, as a
// terminal would not know where the next attribute starts.
func (s *sgrState) apply(params ansi.Params) {
	if len(params) == 0 {
		*s = sgrState{}
		return
	}
	for i := 0; i < len(params); {
		v := params[i].Param(0)
		if params[i].HasMore() && v != 38 && v != 48 {
			// A sub-parameter chain such as 4:3 (curly underline): none of it is
			// a colour attribute, and a 0 inside it must not reset anything.
			for i < len(params) && params[i].HasMore() {
				i++
			}
			i++
			continue
		}
		switch {
		case v == 0:
			*s = sgrState{}
		case v == 7:
			s.reverse = true
		case v == 27:
			s.reverse = false
		case v >= 30 && v <= 37:
			s.fg = paletteName(v - 30)
		case v == 39:
			s.fg = DefaultColor
		case v >= 40 && v <= 47:
			s.bg = paletteName(v - 40)
		case v == 49:
			s.bg = DefaultColor
		case v >= 90 && v <= 97:
			s.fg = paletteName(v - 90 + 8)
		case v >= 100 && v <= 107:
			s.bg = paletteName(v - 100 + 8)
		case v == 38 || v == 48:
			var c color.Color
			n := ansi.ReadStyleColor(params[i:], &c)
			if n == 0 {
				return
			}
			if v == 38 {
				s.fg = colourName(c)
			} else {
				s.bg = colourName(c)
			}
			i += n
			continue
		}
		i++
	}
}

// paletteName names palette entry n (0 to 255).
func paletteName(n int) string {
	if n < 16 {
		return fmt.Sprintf("ansi:%d", n)
	}
	return fmt.Sprintf("idx:%d", n)
}

// colourName names a decoded colour.
func colourName(c color.Color) string {
	switch c := c.(type) {
	case ansi.IndexedColor:
		return paletteName(int(c))
	case ansi.BasicColor:
		return paletteName(int(c))
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// FormatCells renders decoded cells as a stable, readable text, for a golden
// file: a legend of the distinct styles (a foreground and a background, named
// in order of first appearance), then per row a glyph line and a style line.
// Both lines sit between bars so a trailing space can be seen. The style line
// has one character per cell; the glyph line has one glyph per grapheme, so a
// wide glyph is one character there and two in the style line.
//
//	styles
//	  0: fg=- bg=#1e1e2e
//	  1: fg=#cdd6f4 bg=#1e1e2e
//	rows 2
//	00 |ab |
//	   |011|
func FormatCells(rows [][]Cell) string {
	type style struct{ fg, bg string }
	ids := map[style]rune{}
	var order []style
	idOf := func(c Cell) rune {
		k := style{c.FG, c.BG}
		id, ok := ids[k]
		if !ok {
			id = styleRune(len(order))
			ids[k] = id
			order = append(order, k)
		}
		return id
	}

	var body strings.Builder
	fmt.Fprintf(&body, "rows %d\n", len(rows))
	for r, row := range rows {
		var glyphs, styles strings.Builder
		for _, c := range row {
			glyphs.WriteString(c.Glyph)
			styles.WriteRune(idOf(c))
		}
		fmt.Fprintf(&body, "%02d |%s|\n   |%s|\n", r, glyphs.String(), styles.String())
	}

	var out strings.Builder
	out.WriteString("styles\n")
	dash := func(s string) string {
		if s == DefaultColor {
			return "-"
		}
		return s
	}
	for i, k := range order {
		fmt.Fprintf(&out, "  %c: fg=%s bg=%s\n", styleRune(i), dash(k.fg), dash(k.bg))
	}
	out.WriteString(body.String())
	return out.String()
}

// styleRune is the one-cell-wide character naming the i-th style: digits and
// letters first, then the Latin-1 and Latin Extended-A letters, all single
// width.
func styleRune(i int) rune {
	const alnum = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if i < len(alnum) {
		return rune(alnum[i])
	}
	return rune(0x100 + i - len(alnum))
}
