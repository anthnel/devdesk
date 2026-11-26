package components

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-790 (step E5 of DEVDESK-785): bubbles' textinput measures its scroll
// window with runewidth, where "❤️" (U+2764 U+FE0F) is 1 cell, while the
// renderer (Lip Gloss, mode 2027, ansi.GraphemeWidth) counts it for 2. A field
// that holds such emoji draws up to twice what its box holds. ClampField cuts
// the rendered field to its box (field_clamp_test.go).
//
// Nothing here needs ClampField: these tests compile today, and the ones about
// the bar fail today because the bar overflows its frame.

const hearts = "❤️"

// TestTextInputWindowIsMeasuredWithRunewidth pins the behavior of bubbles that
// makes ClampField necessary. It passes today. When it fails, bubbles measures
// the window the way the renderer does: ClampField (field_clamp.go) and its
// two call sites can be removed.
//
// bubbles v2.2.1 decides whether the value scrolls with uniseg (a heart is 2
// cells), then builds the window with runewidth per rune (a heart is 1 cell,
// its U+FE0F 0): the window holds as many hearts as the field has cells, which
// the renderer draws twice as wide. The window can also start on a lone U+FE0F.
// The box of a field of width w is w cells of text plus the cursor cell.
func TestTextInputWindowIsMeasuredWithRunewidth(t *testing.T) {
	const width = 4
	tests := []struct {
		name   string
		hearts int
		home   bool // the cursor goes to the start of the line
	}{
		// 4 hearts are 8 cells, the field holds 4: bubbles shows all four.
		{"cursor at the end", 4, false},
		{"cursor at the start", 4, true},
		{"longer line, cursor at the end", 8, false},
		{"longer line, cursor at the start", 8, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := textinput.New()
			in.Prompt = ""
			in.SetWidth(width)
			in.SetValue(strings.Repeat(hearts, tc.hearts))
			in.Focus()
			if tc.home {
				in.CursorStart()
			}

			view := in.View()
			box := width + 1 // text, cursor cell

			if got := ansi.StringWidth(view); got <= box {
				t.Errorf("a field of width %d draws %d cells, its box holds %d: %q.\n"+
					"bubbles now measures emoji with a variation selector the way the renderer does (2 cells each). "+
					"ClampField can be removed: drop field_clamp.go, its tests and its two call sites "+
					"(paletteInputView, FilterBar), then update docs/architecture/ui-components.md.",
					width, got, box, ansi.Strip(view))
			}
		})
	}
}

// reverseClusterCols returns the column of every cell the view draws in reverse
// video, counting by grapheme cluster: reverseCells splits by rune, which makes
// "❤️" one cell wide and every column after it too small.
func reverseClusterCols(view string) []int {
	var cols []int
	col, reversed, rest := 0, false, view
	for len(rest) > 0 {
		loc := sgrParams.FindStringSubmatchIndex(rest)
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

func TestFilterBarFieldWithVS16StaysInTheBar(t *testing.T) {
	// The bar holds 100 runes at most (CharLimit), so 40 hearts (80 runes) can
	// be typed whole. The field has 34 cells at width 40 and 14 at width 20;
	// bubbles counts 1 cell per heart in its window, so it shows up to that many
	// hearts, which draw twice as much. Short queries (3 hearts) fit and pass today.
	tests := []struct {
		name   string
		hearts int
		paste  bool
	}{
		{"typed, 3 hearts", 3, false},
		{"typed, 8 hearts", 8, false},
		{"typed, 12 hearts", 12, false},
		{"typed, 20 hearts", 20, false},
		{"typed, 40 hearts", 40, false},
		{"pasted, 12 hearts", 12, true},
		{"pasted, 20 hearts", 20, true},
		{"pasted, 40 hearts", 40, true},
	}
	for _, width := range []int{40, 20} {
		for _, tc := range tests {
			t.Run(fmt.Sprintf("%s, width %d", tc.name, width), func(t *testing.T) {
				fb := searchingBar(width)
				text := strings.Repeat(hearts, tc.hearts)
				if tc.paste {
					feedFilterBar(&fb, testutil.Paste(text))
				} else {
					feedFilterBar(&fb, testutil.Type(text)...)
				}
				if got := fb.SearchQuery(); got != text {
					t.Fatalf("setup: the bar holds %q, want %q", got, text)
				}

				checkBarFits(t, fb.View(), width)
			})
		}
	}

	t.Run("a query that fits keeps every heart", func(t *testing.T) {
		// 2 hearts are 4 cells: the field has room for them at width 40, and
		// the clamp must not eat into a field that fits.
		fb := searchingBar(40)
		feedFilterBar(&fb, testutil.Type(strings.Repeat(hearts, 2))...)

		view := fb.View()
		checkBarFits(t, view, 40)
		if got := strings.Count(ansi.Strip(view), hearts); got != 2 {
			t.Errorf("%d hearts on screen, want 2: %q", got, ansi.Strip(view))
		}
	})

	t.Run("with active tokens", func(t *testing.T) {
		fb := searchingBar(40, "tcp", "LISTEN")
		feedFilterBar(&fb, testutil.Type(strings.Repeat(hearts, 40))...)

		checkBarFits(t, fb.View(), 40)
	})

	t.Run("shrunk after typing", func(t *testing.T) {
		fb := searchingBar(120)
		feedFilterBar(&fb, testutil.Type(strings.Repeat(hearts, 40))...)
		for _, width := range []int{80, 40, 20} {
			fb.Resize(width)
			checkBarFits(t, fb.View(), width)
		}
	})

	// A confirmed query is cut by cutQuery, which already walks clusters: this
	// passes today and guards it.
	t.Run("confirmed query", func(t *testing.T) {
		fb := searchingBar(20)
		feedFilterBar(&fb, testutil.Type(strings.Repeat(hearts, 40))...)
		fb.Update(testutil.Key("enter"))

		checkBarFits(t, fb.View(), 20)
	})
}

// checkBarFits fails unless both lines of the bar fit width under the
// grapheme measure the renderer uses and under the wcwidth measure, and the
// cursor, if the bar draws one, is inside the frame.
func checkBarFits(t *testing.T, view string, width int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != 2 {
		t.Fatalf("width %d: View() produced %d lines, want 2", width, len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Errorf("width %d: line %d is %d cells wide (graphemes): %q", width, i, got, ansi.Strip(line))
		}
		if got := ansi.StringWidthWc(line); got > width {
			t.Errorf("width %d: line %d is %d cells wide (wcwidth): %q", width, i, got, ansi.Strip(line))
		}
	}
	cols := reverseClusterCols(lines[0])
	if len(cols) > 1 {
		t.Errorf("width %d: %d cursor cells in the bar, want at most 1: %q", width, len(cols), ansi.Strip(lines[0]))
	}
	for _, col := range cols {
		if col < 1 || col > width-2 {
			t.Errorf("width %d: the cursor is in column %d, outside the frame (columns 1 to %d): %q",
				width, col, width-2, ansi.Strip(lines[0]))
		}
	}
}
