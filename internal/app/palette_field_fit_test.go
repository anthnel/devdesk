package app

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-788 (step E3 of DEVDESK-785): the palette field is resized with
// SetWidth, which leaves bubbles' scroll window as it was. A cursor that stays
// inside the old window then leaves a field wider than its frame, and the
// cursor cell can be pushed out of the frame. The palette re-seats the field
// after each resize (components.ReseatInput).

// paletteLongText is 80 typed runes. The command line holds 50 (CharLimit),
// so what the field really gets is the first 50: still more than the 40 column
// palette can show.
const paletteLongText = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyz"

// paletteFieldResizes is the shrink the palette goes through.
var paletteFieldResizes = [][2]int{{120, 40}, {60, 30}, {40, 16}}

var sgrReverseScan = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// reversedColumns returns the column of every cell a row draws in reverse
// video: the virtual cursor, the only reversed thing on the input row.
func reversedColumns(row string) []int {
	var cols []int
	col, reversed, rest := 0, false, row
	for len(rest) > 0 {
		loc := sgrReverseScan.FindStringSubmatchIndex(rest)
		text := rest
		if loc != nil {
			text = rest[:loc[0]]
		}
		for _, g := range strings.Split(ansi.Strip(text), "") {
			if reversed {
				cols = append(cols, col)
			}
			col += ansi.StringWidth(g)
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

// paletteInputRow returns the raw screen row that holds the prompt.
func paletteInputRow(t *testing.T, a *App) string {
	t.Helper()
	raw := strings.Split(a.Render(), "\n")
	for _, row := range raw {
		if strings.Contains(ansi.Strip(row), "❯ ") {
			return row
		}
	}
	t.Fatalf("no input row on screen:\n%s", strings.Join(screen(a), "\n"))
	return ""
}

func TestPaletteFieldFitsItsFrameAfterResize(t *testing.T) {
	tests := []struct {
		name string
		// move puts the cursor somewhere in the 50 typed runes, at the first size.
		move []string
	}{
		{"cursor at the end", nil},
		{"cursor right of the middle", repeatKey("left", 15)},
		{"cursor in the middle", repeatKey("left", 25)},
		{"cursor at the start", []string{"home"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := routerAt(t, &fakeView{body: "body"}, paletteFieldResizes[0][0], paletteFieldResizes[0][1])
			press(t, a, "ctrl+p")
			typeCommand(t, a, paletteLongText)
			press(t, a, tc.move...)
			pos := a.commandInput.Position()
			if len(a.commandInput.Value()) < 40 {
				t.Fatalf("setup: the command line holds %d runes, want at least 40", len(a.commandInput.Value()))
			}

			for _, size := range paletteFieldResizes[1:] {
				t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
					resizeTo(t, a, size[0], size[1])

					if got := a.commandInput.Position(); got != pos {
						t.Errorf("the resize moved the cursor from %d to %d", pos, got)
					}
					f := mustFrame(t, a)
					input := f.input()
					if !strings.HasPrefix(input, "│") || !strings.HasSuffix(input, "│") {
						t.Errorf("the input row is not closed by │…│: %q", input)
					}
					// The frame clips what overflows it, so a too wide field can look
					// right on screen while its cursor is cut off or its text is not
					// the window the user is editing: the field itself must fit.
					if w, room := ansi.StringWidth(a.commandInput.View()), f.Width-paletteInputChrome; w > room {
						t.Errorf("the field draws %d cells, the frame has room for %d", w, room)
					}
					row := paletteInputRow(t, a)
					if w := ansi.StringWidth(row); w > size[0] {
						t.Errorf("the input row is %d cells wide on a %d column screen: %q", w, size[0], ansi.Strip(row))
					}
					cols := reversedColumns(row)
					if len(cols) != 1 {
						t.Fatalf("%d cursor cells on the input row, want 1: %q", len(cols), ansi.Strip(row))
					}
					if cols[0] <= f.X || cols[0] >= f.X+f.Width-1 {
						t.Errorf("the cursor is in column %d, outside the frame's text [%d, %d): %q",
							cols[0], f.X+1, f.X+f.Width-1, input)
					}
				})
			}
		})
	}
}

func repeatKey(name string, n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = name
	}
	return keys
}

// A resize that leaves the width as it was must not move the view: the router
// resizes on events that do not change the size, and a long line whose cursor
// sits in the middle of its window would scroll for no reason.
func TestPaletteResizeWithoutWidthChangeKeepsTheView(t *testing.T) {
	a := routerAt(t, &fakeView{body: "body"}, 40, 16)
	press(t, a, "ctrl+p")
	typeCommand(t, a, paletteLongText)
	// Back to the start, then a few steps right: the window shows the head of
	// the line and the cursor sits inside it, not at an edge. A re-seat would
	// rebuild the window from the end of the line and scroll it.
	press(t, a, "home")
	press(t, a, repeatKey("right", 6)...)
	width, pos, before := a.commandInput.Width(), a.commandInput.Position(), a.commandInput.View()

	resizeTo(t, a, 40, 16)

	if got := a.commandInput.Width(); got != width {
		t.Fatalf("setup: the width went from %d to %d", width, got)
	}
	if got := a.commandInput.Position(); got != pos {
		t.Errorf("the resize moved the cursor from %d to %d", pos, got)
	}
	if got := a.commandInput.View(); got != before {
		t.Errorf("a resize without width change moved the view:\n got %q\nwant %q", ansi.Strip(got), ansi.Strip(before))
	}
}
