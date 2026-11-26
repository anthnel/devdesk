package components

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-788 (step E3 of DEVDESK-785): bubbles scrolls a textinput through two
// internal offsets that only SetCursor, SetValue and Update refresh. After a
// SetWidth that shrinks the field, a cursor that still sits inside the old
// window leaves the old, too wide window in place: the view overflows its box
// and the cursor cell can land beyond it. ReseatInput rebuilds the window for
// the current width without touching what the user typed.

// reseatPrompt is the prompt of the fields under test, two cells wide.
const reseatPrompt = "❯ "

// reseatText is 80 runes, each one cell wide and each one different (Latin
// Extended-A and -B), so the glyph under the cursor tells where the window is.
func reseatText() string {
	runes := make([]rune, 80)
	for i := range runes {
		runes[i] = rune(0x100 + i)
	}
	return string(runes)
}

// reseatInput builds a focused field at width, holding reseatText with the
// cursor at pos.
func reseatInput(width, pos int) textinput.Model {
	in := textinput.New()
	in.Prompt = reseatPrompt
	in.CharLimit = 0
	in.Focus()
	in.SetWidth(width)
	in.SetValue(reseatText())
	in.SetCursor(pos)
	return in
}

// reseatShrinkCases are the cursor positions of the 80-rune text the shrink is
// checked at. "right of middle" is the one a stale window hides from the frame.
var reseatShrinkCases = []struct {
	name string
	pos  int
}{
	{"cursor at the end", 80},
	{"cursor on the last rune", 79},
	{"cursor right of the middle", 70},
	{"cursor in the middle", 40},
	{"cursor at the start", 0},
}

// reseatWidths is the shrink the field goes through: it holds the whole text at
// 120, then has to scroll at 60 and again at 40.
var reseatWidths = []int{120, 60, 40}

var sgrParams = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// reverseCells returns the column and the glyph of every cell the view draws in
// reverse video: the virtual cursor, the only reversed thing a plain field has.
func reverseCells(view string) (cols []int, glyphs []string) {
	col, reversed, rest := 0, false, view
	for len(rest) > 0 {
		loc := sgrParams.FindStringSubmatchIndex(rest)
		text := rest
		if loc != nil {
			text = rest[:loc[0]]
		}
		if text != "" {
			for _, g := range strings.Split(ansi.Strip(text), "") {
				if reversed {
					cols = append(cols, col)
					glyphs = append(glyphs, g)
				}
				col += ansi.StringWidth(g)
			}
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
	return cols, glyphs
}

// checkFieldFits fails unless the view fits its box and shows the cursor cell,
// on the glyph the cursor really is on (a space after the last rune).
func checkFieldFits(t *testing.T, in textinput.Model) {
	t.Helper()
	view := in.View()
	box := ansi.StringWidth(reseatPrompt) + in.Width() + 1 // prompt, text, cursor cell

	if got := ansi.StringWidth(view); got > box {
		t.Errorf("width %d: the field is %d cells wide, its box holds %d: %q",
			in.Width(), got, box, ansi.Strip(view))
	}
	cols, glyphs := reverseCells(view)
	if len(cols) != 1 {
		t.Fatalf("width %d: %d cursor cells in the view, want 1: %q", in.Width(), len(cols), ansi.Strip(view))
	}
	if cols[0] >= box {
		t.Errorf("width %d: the cursor is in column %d, past the box of %d cells", in.Width(), cols[0], box)
	}
	want := " "
	if runes := []rune(in.Value()); in.Position() < len(runes) {
		want = string(runes[in.Position()])
	}
	if glyphs[0] != want {
		t.Errorf("width %d: the cursor sits on %q, the rune at position %d is %q",
			in.Width(), glyphs[0], in.Position(), want)
	}
}

func TestReseatKeepsTheCursorVisibleAfterAShrink(t *testing.T) {
	for _, tc := range reseatShrinkCases {
		t.Run(tc.name, func(t *testing.T) {
			in := reseatInput(reseatWidths[0], tc.pos)
			for _, width := range reseatWidths[1:] {
				in.SetWidth(width)
				ReseatInput(&in)
				checkFieldFits(t, in)
			}
		})
	}
}

// The defect, pinned without ReseatInput: SetWidth followed by SetCursor at the
// same position (what sizePaletteInput did before DEVDESK-788) keeps the old
// window whenever the cursor is still inside it. This passes today and goes
// red on the day bubbles refreshes its window on a width change (E0 moves
// bubbles to v2.2.2): the signal to retire ReseatInput, not a test to relax.
func TestShrinkWithoutReseatLeavesTheCursorOutside(t *testing.T) {
	tests := []struct {
		name           string
		pos            int
		cursorOutOfBox bool // the cursor cell is beyond the box, not only the text
	}{
		{"cursor right of the middle", 70, true},
		{"cursor in the middle", 40, false},
		{"cursor at the start", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := reseatInput(120, tc.pos)
			in.SetWidth(40)
			in.SetCursor(in.Position())

			view := in.View()
			box := ansi.StringWidth(reseatPrompt) + in.Width() + 1
			if got := ansi.StringWidth(view); got <= box {
				t.Fatalf("the field is %d cells wide, within its box of %d: bubbles refreshes its window now", got, box)
			}
			if !tc.cursorOutOfBox {
				return
			}
			cols, _ := reverseCells(view)
			if len(cols) != 1 || cols[0] < box {
				t.Errorf("cursor columns %v, want one at or past the box of %d cells", cols, box)
			}
		})
	}
}

func TestReseatKeepsValuePositionAndErr(t *testing.T) {
	failed := errors.New("failed by the caller, not by a validator")
	tests := []struct {
		name  string
		build func() textinput.Model
	}{
		{"cursor in the middle", func() textinput.Model { return reseatInput(120, 40) }},
		{"cursor at the end", func() textinput.Model { return reseatInput(120, 80) }},
		{"cursor at the start", func() textinput.Model { return reseatInput(120, 0) }},
		{"error set by the caller", func() textinput.Model {
			in := reseatInput(120, 40)
			in.Err = failed
			return in
		}},
		{"error from the validator", func() textinput.Model {
			in := reseatInput(120, 40)
			in.Validate = func(string) error { return failed }
			in.SetValue(in.Value())
			in.SetCursor(40)
			return in
		}},
		{"empty field", func() textinput.Model {
			in := reseatInput(120, 0)
			in.SetValue("")
			return in
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.build()
			value, pos, err := in.Value(), in.Position(), in.Err
			focused, limit := in.Focused(), in.CharLimit

			in.SetWidth(40)
			ReseatInput(&in)

			if in.Value() != value {
				t.Errorf("value %q, want %q", in.Value(), value)
			}
			if in.Position() != pos {
				t.Errorf("position %d, want %d", in.Position(), pos)
			}
			if !errors.Is(in.Err, err) || (err == nil) != (in.Err == nil) {
				t.Errorf("Err %v, want %v", in.Err, err)
			}
			if in.Focused() != focused || in.CharLimit != limit {
				t.Errorf("focus %v and limit %d, want %v and %d", in.Focused(), in.CharLimit, focused, limit)
			}
		})
	}
}

func TestReseatKeepsTheSuggestions(t *testing.T) {
	in := textinput.New()
	in.Prompt = reseatPrompt
	in.ShowSuggestions = true
	in.Focus()
	in.SetWidth(60)
	in.SetSuggestions([]string{"deploy-staging", "deploy-production", "destroy", "logs"})
	// Typed, not set: bubbles recomputes its matches when the user types.
	for _, msg := range testutil.Type("de") {
		in, _ = in.Update(msg)
	}
	in, _ = in.Update(testutil.Key("ctrl+n")) // the second match is now current

	available := slices.Clone(in.AvailableSuggestions())
	matched := slices.Clone(in.MatchedSuggestions())
	index, current := in.CurrentSuggestionIndex(), in.CurrentSuggestion()
	if len(matched) < 2 || index == 0 {
		t.Fatalf("setup: matched %v, current index %d, want several matches and the second one current", matched, index)
	}

	in.SetWidth(10)
	ReseatInput(&in)

	if got := in.AvailableSuggestions(); !slices.Equal(got, available) {
		t.Errorf("available suggestions %v, want %v", got, available)
	}
	if got := in.MatchedSuggestions(); !slices.Equal(got, matched) {
		t.Errorf("matched suggestions %v, want %v", got, matched)
	}
	if in.CurrentSuggestionIndex() != index || in.CurrentSuggestion() != current {
		t.Errorf("current suggestion %d %q, want %d %q",
			in.CurrentSuggestionIndex(), in.CurrentSuggestion(), index, current)
	}
}

// A field that already fits is left looking exactly as it did.
func TestReseatLeavesAFittingFieldAlone(t *testing.T) {
	for _, tc := range reseatShrinkCases {
		t.Run(tc.name, func(t *testing.T) {
			in := reseatInput(120, tc.pos)
			before := in.View()
			ReseatInput(&in)
			if got := in.View(); got != before {
				t.Errorf("reseating a field that fits changed it:\n got %q\nwant %q", got, before)
			}
		})
	}
}
