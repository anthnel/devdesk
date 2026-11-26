package app

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-790 (step E5 of DEVDESK-785): bubbles' textinput builds its scroll
// window with runewidth, where "❤️" (U+2764 U+FE0F) is 1 cell, while the
// renderer counts it for 2. A command line full of such emoji draws up to twice
// what the palette's input row holds. paletteInputView clamps the field
// (components.ClampField).

const paletteHeart = "❤️"

var paletteSGR = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// reversedClusterColumns returns the column of every cell a row draws in
// reverse video, counting by grapheme cluster. reversedColumns splits by rune,
// which counts "❤️" for 1 cell and puts every column after it too far left.
func reversedClusterColumns(row string) []int {
	var cols []int
	col, reversed, rest := 0, false, row
	for len(rest) > 0 {
		loc := paletteSGR.FindStringSubmatchIndex(rest)
		text := rest
		if loc != nil {
			text = rest[:loc[0]]
		}
		for text = ansi.Strip(text); text != ""; {
			cluster, w := ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
			if cluster == "" {
				break
			}
			if reversed {
				cols = append(cols, col)
			}
			col += w
			text = text[len(cluster):]
		}
		if loc == nil {
			break
		}
		for _, p := range strings.Split(rest[loc[2]:loc[3]], ";") {
			switch p {
			case "", "0", "27":
				reversed = false
			case "7":
				reversed = true
			}
		}
		rest = rest[loc[1]:]
	}
	return cols
}

func TestPaletteFieldWithVS16StaysInItsFrame(t *testing.T) {
	// The command line holds 50 runes (CharLimit): 25 hearts at most. The 40
	// column palette has room for 36 cells of text, so 25 hearts (50 cells) do
	// not fit, and neither do 20 at 40 columns once the cursor cell is counted.
	tests := []struct {
		name   string
		hearts int
	}{
		{"3 hearts", 3},
		{"12 hearts", 12},
		{"20 hearts", 20},
		{"25 hearts, the whole line", 25},
	}
	for _, width := range []int{40, 60} {
		for _, tc := range tests {
			t.Run(fmt.Sprintf("%s, %d columns", tc.name, width), func(t *testing.T) {
				a := routerAt(t, &fakeView{body: "body"}, width, 24)
				press(t, a, "ctrl+p")
				typeCommand(t, a, strings.Repeat(paletteHeart, tc.hearts))
				if got := a.commandInput.Value(); got != strings.Repeat(paletteHeart, tc.hearts) {
					t.Fatalf("setup: the command line holds %q", got)
				}

				f := mustFrame(t, a)
				// The frame clips what overflows it, so a too wide field can look
				// right on screen while its text is cut at the wrong place: the
				// field itself must fit, under the measure the renderer uses.
				field := paletteInputView(a.commandInput)
				if w, room := ansi.StringWidth(field), f.Width-paletteInputChrome; w > room {
					t.Errorf("the field draws %d cells, the frame has room for %d: %q", w, room, ansi.Strip(field))
				}
				if w, room := ansi.StringWidthWc(field), f.Width-paletteInputChrome; w > room {
					t.Errorf("the field draws %d cells under wcwidth, the frame has room for %d: %q", w, room, ansi.Strip(field))
				}

				input := f.input()
				if !strings.HasPrefix(input, "│") || !strings.HasSuffix(input, "│") {
					t.Errorf("the input row is not closed by │…│: %q", input)
				}
				row := paletteInputRow(t, a)
				if w := ansi.StringWidth(row); w > width {
					t.Errorf("the input row is %d cells wide on a %d column screen: %q", w, width, ansi.Strip(row))
				}
				if w := ansi.StringWidthWc(row); w > width {
					t.Errorf("the input row is %d cells wide under wcwidth on a %d column screen: %q", w, width, ansi.Strip(row))
				}
				cols := reversedClusterColumns(row)
				if len(cols) > 1 {
					t.Errorf("%d cursor cells on the input row, want at most 1: %q", len(cols), ansi.Strip(row))
				}
				for _, col := range cols {
					if col <= f.X || col >= f.X+f.Width-1 {
						t.Errorf("the cursor is in column %d, outside the frame's text [%d, %d): %q",
							col, f.X+1, f.X+f.Width-1, input)
					}
				}
			})
		}
	}
}
