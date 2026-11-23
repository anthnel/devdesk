package testutil

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The cell decoder is the safety net under any change to how a frame is
// rendered (DEVDESK-281): two frames are the same when their cells are, not
// when their bytes are. These tests pin what a cell is, so a golden built on
// it does not drift with the decoder.

// glyphs returns the glyph of each cell of a row.
func glyphs(row []Cell) []string {
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = c.Glyph
	}
	return out
}

func TestCellsPlainText(t *testing.T) {
	rows := Cells("ab\ncd")
	want := [][]Cell{
		{{"a", "", ""}, {"b", "", ""}},
		{{"c", "", ""}, {"d", "", ""}},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("Cells = %#v, want %#v", rows, want)
	}
}

func TestCellsColours(t *testing.T) {
	tests := []struct {
		name           string
		in             string
		wantFG, wantBG string
	}{
		{"24-bit foreground and background", "\x1b[38;2;1;2;3m\x1b[48;2;255;16;0mx", "#010203", "#ff1000"},
		{"24-bit in one sequence", "\x1b[1;38;2;10;20;30;48;2;40;50;60mx", "#0a141e", "#28323c"},
		{"24-bit with colons", "\x1b[38:2::1:2:3;48:2::4:5:6mx", "#010203", "#040506"},
		{"256 palette above 15", "\x1b[38;5;200;48;5;17mx", "idx:200", "idx:17"},
		{"256 palette below 16 is the 16-colour one", "\x1b[38;5;1;48;5;12mx", "ansi:1", "ansi:12"},
		{"16 colours", "\x1b[31;42mx", "ansi:1", "ansi:2"},
		{"bright 16 colours", "\x1b[91;102mx", "ansi:9", "ansi:10"},
		{"39 resets only the foreground", "\x1b[31;42m\x1b[39mx", "", "ansi:2"},
		{"49 resets only the background", "\x1b[31;42m\x1b[49mx", "ansi:1", ""},
		{"bare m resets both", "\x1b[31;42m\x1b[mx", "", ""},
		{"0m resets both", "\x1b[31;42m\x1b[0mx", "", ""},
		{"empty 0 inside a list resets, then sets", "\x1b[31;0;42mx", "", "ansi:2"},
		{"a later colour wins", "\x1b[31m\x1b[32mx", "ansi:2", ""},
		{"reverse swaps", "\x1b[38;2;1;2;3;48;2;4;5;6;7mx", "#040506", "#010203"},
		{"reverse of a default foreground is not the terminal background", "\x1b[48;2;4;5;6;7mx", "#040506", "default-fg"},
		{"27 undoes reverse", "\x1b[38;2;1;2;3;48;2;4;5;6;7;27mx", "#010203", "#040506"},
		{"an unknown attribute changes nothing", "\x1b[1;4;3;9;31mx", "ansi:1", ""},
		{"a truncated 38;2 is ignored, not a panic", "\x1b[38;2;1mx", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := Cells(tt.in)
			if len(rows) != 1 || len(rows[0]) != 1 {
				t.Fatalf("Cells(%q) = %#v, want one cell", tt.in, rows)
			}
			c := rows[0][0]
			if c.Glyph != "x" || c.FG != tt.wantFG || c.BG != tt.wantBG {
				t.Fatalf("Cells(%q)[0][0] = %+v, want fg %q bg %q", tt.in, c, tt.wantFG, tt.wantBG)
			}
		})
	}
}

// A reset followed by padding is the Rule 115 trap: the padding keeps no
// background, so the terminal's own shows through.
func TestCellsResetThenPaddingIsBare(t *testing.T) {
	for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
		frame := "\x1b[48;2;1;2;3mab" + reset + "  "
		rows := Cells(frame)
		if got := glyphs(rows[0]); !reflect.DeepEqual(got, []string{"a", "b", " ", " "}) {
			t.Fatalf("%q: glyphs = %q", reset, got)
		}
		for i, c := range rows[0] {
			wantBG := ""
			if i < 2 {
				wantBG = "#010203"
			}
			if c.BG != wantBG {
				t.Errorf("%q: cell %d background = %q, want %q", reset, i, c.BG, wantBG)
			}
		}
		want := []Pos{{0, 2}, {0, 3}}
		if got := BareCells(frame); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: BareCells = %v, want %v", reset, got, want)
		}
	}
}

func TestCellsStylePersistsAcrossRows(t *testing.T) {
	rows := Cells("\x1b[48;2;1;2;3mab\ncd\x1b[m")
	for r, row := range rows {
		for c, cell := range row {
			if cell.BG != "#010203" {
				t.Errorf("cell %d,%d background = %q, want #010203", r, c, cell.BG)
			}
		}
	}
}

func TestCellsWideGraphemeSpansCells(t *testing.T) {
	rows := Cells("\x1b[48;2;1;2;3m\x1b[38;2;9;9;9m世a")
	want := []Cell{
		{"世", "#090909", "#010203"},
		{"", "#090909", "#010203"}, // the second cell of the glyph
		{"a", "#090909", "#010203"},
	}
	if !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("Cells = %#v, want %#v", rows[0], want)
	}
}

func TestCellsEmojiVS16(t *testing.T) {
	// U+2764 U+FE0F: a text-presentation heart made an emoji by the variation
	// selector. ansi.StringWidth counts it as two cells, so the decoder must
	// too, or every cell after it in the row would be off by one.
	heart := "❤️"
	if w := ansi.StringWidth(heart); w != 2 {
		t.Fatalf("ansi.StringWidth(%q) = %d; the premise of this test changed", heart, w)
	}
	rows := Cells("\x1b[48;2;1;2;3m" + heart + "z")
	if len(rows[0]) != 3 {
		t.Fatalf("cells = %#v, want 3", rows[0])
	}
	if rows[0][0].Glyph != heart || rows[0][1].Glyph != "" || rows[0][2].Glyph != "z" {
		t.Fatalf("glyphs = %q", glyphs(rows[0]))
	}
	if rows[0][1].BG != "#010203" {
		t.Errorf("the continuation cell lost its background: %+v", rows[0][1])
	}
}

func TestCellsCombiningMarkStaysInItsCell(t *testing.T) {
	rows := Cells("éx")
	if got := glyphs(rows[0]); !reflect.DeepEqual(got, []string{"é", "x"}) {
		t.Fatalf("glyphs = %q", got)
	}
}

func TestCellsIgnoreNonSGRSequences(t *testing.T) {
	seqs := []string{
		"\x1b[2K",                           // erase line
		"\x1b[3;4H",                         // cursor position
		"\x1b[?25l",                         // private mode
		"\x1b[5C",                           // cursor forward
		"\x1b]0;a window title\x07",         // OSC terminated by BEL
		"\x1b]8;;https://example.com\x1b\\", // hyperlink open, ST terminated
		"\x1b]8;;\x1b\\",                    // hyperlink close
		"\x1b]9;4;1;50\x07",                 // progress bar
		"\x1b7",                             // save cursor
		"\x1b[>4;2m",                        // private-prefixed "m": not an SGR
		"\x1b[31 m",                         // intermediate byte: not an SGR
	}
	for _, seq := range seqs {
		t.Run(strings.NewReplacer("\x1b", `\e`, "\x07", `\a`).Replace(seq), func(t *testing.T) {
			rows := Cells("\x1b[38;2;1;2;3ma" + seq + "b")
			want := []Cell{{"a", "#010203", ""}, {"b", "#010203", ""}}
			if len(rows) != 1 || !reflect.DeepEqual(rows[0], want) {
				t.Fatalf("Cells = %#v, want %#v", rows, want)
			}
		})
	}
}

func TestCellsEmptyFrame(t *testing.T) {
	if got := Cells(""); len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("Cells(\"\") = %#v, want one empty row", got)
	}
	if got := BareCells(""); len(got) != 0 {
		t.Fatalf("BareCells(\"\") = %v", got)
	}
}

func TestBareCellsAreOnlyDefaultBackgrounds(t *testing.T) {
	frame := "\x1b[48;5;4m a\x1b[49m b\x1b[41m c\n\x1b[mxy"
	want := []Pos{{0, 2}, {0, 3}, {1, 0}, {1, 1}}
	if got := BareCells(frame); !reflect.DeepEqual(got, want) {
		t.Fatalf("BareCells = %v, want %v", got, want)
	}
}

func TestFormatCells(t *testing.T) {
	frame := "\x1b[48;2;1;2;3mab\x1b[m \n\x1b[48;2;1;2;3m世\x1b[38;5;9mz"
	// One glyph line and one style line per row, both between bars so a
	// trailing space is visible. The wide glyph's second cell is in the style
	// line (one character per cell) but not in the glyph line, where the glyph
	// already stands for both.
	want := strings.Join([]string{
		"styles",
		"  0: fg=- bg=#010203",
		"  1: fg=- bg=-",
		"  2: fg=ansi:9 bg=#010203",
		"rows 2",
		"00 |ab |",
		"   |001|",
		"01 |世z|",
		"   |002|",
		"",
	}, "\n")
	if got := FormatCells(Cells(frame)); got != want {
		t.Fatalf("FormatCells =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatCellsIsStable(t *testing.T) {
	frame := "\x1b[48;2;1;2;3mab\x1b[m \n\x1b[31mcd"
	if a, b := FormatCells(Cells(frame)), FormatCells(Cells(frame)); a != b {
		t.Fatalf("two formats of one frame differ:\n%s\n%s", a, b)
	}
}
