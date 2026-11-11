package components

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/charmbracelet/x/ansi"
)

// wrapInputLines is the whole reason WrappedInput exists (Rule 133 bans
// bubbles/textarea), and the cursor rendering depends on startIndex being the
// exact rune offset of each visual line.
func TestWrapInputLines(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wrapWidth int
		want      []wrappedLine
	}{
		{
			name:      "empty text produces no lines",
			text:      "",
			wrapWidth: 10,
			want:      nil,
		},
		{
			name:      "text shorter than the wrap width stays on one line",
			text:      "short",
			wrapWidth: 10,
			want:      []wrappedLine{{"short", 0}},
		},
		{
			name:      "text exactly at the wrap width stays on one line",
			text:      "0123456789",
			wrapWidth: 10,
			want:      []wrappedLine{{"0123456789", 0}},
		},
		{
			name:      "wrap happens at the last space and consumes it",
			text:      "hello world foo",
			wrapWidth: 10,
			want:      []wrappedLine{{"hello", 0}, {"world foo", 6}},
		},
		{
			name:      "a word longer than the wrap width is hard-broken",
			text:      "abcdefghij",
			wrapWidth: 5,
			want:      []wrappedLine{{"abcde", 0}, {"fghij", 5}},
		},
		{
			name:      "multiple wraps keep accurate start offsets",
			text:      "aaa bbb ccc ddd",
			wrapWidth: 7,
			want:      []wrappedLine{{"aaa bbb", 0}, {"ccc ddd", 8}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapInputLines(tt.text, tt.wrapWidth)
			if len(got) != len(tt.want) {
				t.Fatalf("wrapInputLines() = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// A non-positive wrap width used to hang: the break point collapsed onto start,
// an empty line was appended and start never advanced, so the slice grew until
// the process died. No caller does this (Rule 133 fixes the width at 60/80/100),
// but the loop must terminate regardless.
func TestWrapInputLinesRefusesNonPositiveWidth(t *testing.T) {
	const text = "hello world"

	for _, wrapWidth := range []int{0, -1, -80} {
		got := wrapInputLines(text, wrapWidth)

		if len(got) != 1 {
			t.Fatalf("wrapInputLines(%q, %d) returned %d lines, want 1", text, wrapWidth, len(got))
		}
		if got[0].text != text || got[0].startIndex != 0 {
			t.Errorf("wrapInputLines(%q, %d) = %+v, want the text unwrapped at index 0", text, wrapWidth, got[0])
		}
	}
}

// startIndex must address the original rune slice, since View() maps the cursor
// position onto a visual line with cursorPos - startIndex.
func TestWrapInputLinesStartIndexAddressesOriginalRunes(t *testing.T) {
	text := "hello world foo"
	lines := wrapInputLines(text, 10)
	runes := []rune(text)

	for _, wl := range lines {
		end := wl.startIndex + len([]rune(wl.text))
		if got := string(runes[wl.startIndex:end]); got != wl.text {
			t.Errorf("startIndex %d does not address %q in the source, got %q", wl.startIndex, wl.text, got)
		}
	}
}

func TestNewWrappedInputStartsEmptyAndUnfocused(t *testing.T) {
	w := NewWrappedInput(80, 250, "description (optional)")

	if got := w.Value(); got != "" {
		t.Errorf("Value() = %q on a new input, want empty", got)
	}
	if w.focused {
		t.Error("a new WrappedInput is focused")
	}
}

func TestWrappedInputFocusAndBlur(t *testing.T) {
	w := NewWrappedInput(80, 250, "placeholder")

	cmd := w.Focus()
	if !w.focused {
		t.Error("Focus() did not set the focused flag")
	}
	if cmd == nil {
		t.Error("Focus() returned no blink command")
	}

	w.Blur()
	if w.focused {
		t.Error("Blur() did not clear the focused flag")
	}
}

func TestWrappedInputAcceptsTypedRunes(t *testing.T) {
	w := NewWrappedInput(80, 250, "placeholder")
	w.Focus()

	for _, msg := range testutil.Type("hello") {
		w, _ = w.Update(msg)
	}

	if got := w.Value(); got != "hello" {
		t.Errorf("Value() = %q after typing, want %q", got, "hello")
	}
}

func TestWrappedInputRespectsCharLimit(t *testing.T) {
	w := NewWrappedInput(80, 3, "placeholder")
	w.Focus()

	for _, msg := range testutil.Type("abcdef") {
		w, _ = w.Update(msg)
	}

	if got := w.Value(); len(got) != 3 {
		t.Errorf("Value() = %q, want 3 characters (the char limit)", got)
	}
}

// An unfocused empty field shows the placeholder; a focused empty field shows a
// bare block cursor instead.
func TestWrappedInputViewWhenEmpty(t *testing.T) {
	w := NewWrappedInput(80, 250, "description (optional)")
	w.SetDisplayWidth(40)

	if got := w.View(); !strings.Contains(got, "description (optional)") {
		t.Error("unfocused empty View() does not show the placeholder")
	}

	w.Focus()
	if got := w.View(); strings.Contains(got, "description (optional)") {
		t.Error("focused empty View() still shows the placeholder")
	}
}

func TestWrappedInputViewWrapsAcrossLines(t *testing.T) {
	w := NewWrappedInput(10, 250, "placeholder")
	w.SetDisplayWidth(40)
	w.Focus()

	for _, msg := range testutil.Type("hello world foo") {
		w, _ = w.Update(msg)
	}

	lines := strings.Split(w.View(), "\n")
	if len(lines) != 2 {
		t.Fatalf("View() produced %d lines for wrapped text, want 2", len(lines))
	}
}

// Every rendered line is padded to displayWidth so the app background covers the
// full width (Rule 115) — a short line would otherwise show the terminal's own
// background.
func TestWrappedInputViewPadsEveryLine(t *testing.T) {
	w := NewWrappedInput(10, 250, "placeholder")
	w.SetDisplayWidth(40)

	for _, msg := range testutil.Type("hello world foo") {
		w, _ = w.Update(msg)
	}

	for i, line := range strings.Split(w.View(), "\n") {
		if line == "" {
			t.Errorf("line %d is empty instead of padded to the display width", i)
		}
	}
}

// A paste reaches a focused WrappedInput. It wraps a single-line textinput, so
// Value() has each newline turned into a space, the rest unchanged (DEVDESK-199).
func TestWrappedInputPaste(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"one line", "q? hello", "q? hello"},
		{"several lines", "l1\nl2", "l1 l2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWrappedInput(80, 250, "placeholder")
			w.Focus()

			w, _ = w.Update(testutil.Paste(tc.paste))

			if got := w.Value(); got != tc.want {
				t.Errorf("Value() = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if !w.focused {
				t.Error("the paste dropped the focus")
			}
		})
	}
}

// underlineSGR is the curly underline (4:3) StyleInvalidField emits.
const underlineSGR = "4:3"

func invalidTestInput(value string) WrappedInput {
	w := NewWrappedInput(80, 250, "Name")
	w.SetDisplayWidth(100)
	w.SetValue(value)
	return w
}

func TestAWrappedInputIsNotInvalidByDefault(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		focus       bool
	}{
		{"empty unfocused", "", false},
		{"empty focused", "", true},
		{"value unfocused", "hello", false},
		{"value focused", "hello", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := invalidTestInput(tc.value)
			if tc.focus {
				w.Focus()
			}
			want := w.View()
			if strings.Contains(want, underlineSGR) {
				t.Fatal("a default WrappedInput is underlined")
			}
			w.SetInvalid(true)
			w.SetInvalid(false)
			if got := w.View(); got != want {
				t.Errorf("SetInvalid(true) then (false) changed the render:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestAnInvalidWrappedInputUnderlinesEachLineOfText(t *testing.T) {
	w := invalidTestInput("alpha beta gamma delta")
	w.wrapWidth = 12 // two visual lines
	valid := w.View()
	w.SetInvalid(true)
	got := w.View()

	lines, validLines := strings.Split(got, "\n"), strings.Split(valid, "\n")
	if len(lines) < 2 || len(lines) != len(validLines) {
		t.Fatalf("lines = %d (valid %d), want the same count and at least 2", len(lines), len(validLines))
	}
	for i, line := range lines {
		idx := strings.Index(line, underlineSGR)
		if idx < 0 {
			t.Fatalf("line %d has no underline: %q", i, line)
		}
		if lead := ansi.Strip(line[:idx]); lead != "  " {
			t.Errorf("line %d: text before the underline = %q, want only the 2-space indent", i, lead)
		}
		if ansi.Strip(line) != ansi.Strip(validLines[i]) {
			t.Errorf("line %d text changed: %q vs %q", i, ansi.Strip(line), ansi.Strip(validLines[i]))
		}
		if ansi.StringWidth(line) != ansi.StringWidth(validLines[i]) {
			t.Errorf("line %d width %d, want %d", i, ansi.StringWidth(line), ansi.StringWidth(validLines[i]))
		}
	}
}

func TestAnInvalidFocusedWrappedInputKeepsItsCursor(t *testing.T) {
	w := invalidTestInput("hello")
	w.Focus()
	valid := w.View()
	w.SetInvalid(true)
	got := w.View()
	if !strings.Contains(got, underlineSGR) {
		t.Fatal("focused invalid field is not underlined")
	}
	if ansi.Strip(got) != ansi.Strip(valid) || ansi.StringWidth(got) != ansi.StringWidth(valid) {
		t.Errorf("focused invalid render changed text or width: %q vs %q", ansi.Strip(got), ansi.Strip(valid))
	}
}

// Decision: a required empty field refused on submit underlines its placeholder.
func TestAnInvalidEmptyWrappedInputUnderlinesItsPlaceholder(t *testing.T) {
	w := invalidTestInput("")
	valid := w.View()
	w.SetInvalid(true)
	got := w.View()

	idx := strings.Index(got, underlineSGR)
	if idx < 0 {
		t.Fatalf("placeholder is not underlined: %q", got)
	}
	if lead := ansi.Strip(got[:idx]); lead != "  " {
		t.Errorf("text before the underline = %q, want only the 2-space indent", lead)
	}
	if !strings.Contains(ansi.Strip(got), "Name") {
		t.Errorf("placeholder text lost: %q", ansi.Strip(got))
	}
	if ansi.Strip(got) != ansi.Strip(valid) || ansi.StringWidth(got) != ansi.StringWidth(valid) {
		t.Errorf("invalid placeholder changed text or width: %q vs %q", ansi.Strip(got), ansi.Strip(valid))
	}
}

// Rule 120: the focus prefix belongs to the form's label, never to the input,
// so invalidity must not add a glyph to the input's own render.
func TestInvalidWrappedInputAddsNoPrefixGlyph(t *testing.T) {
	for _, value := range []string{"", "hello"} {
		w := invalidTestInput(value)
		w.SetInvalid(true)
		if out := ansi.Strip(w.View()); strings.Contains(out, "●") || strings.Contains(out, "") {
			t.Errorf("value %q: invalid render carries a focus glyph: %q", value, out)
		}
	}
}

// A focused empty field shows a cursor, not its placeholder: nothing to
// underline, and the footer carries the message.
func TestAnInvalidEmptyFocusedWrappedInputIsUnchanged(t *testing.T) {
	w := invalidTestInput("")
	w.Focus()
	valid := w.View()
	w.SetInvalid(true)
	got := w.View()
	if strings.Contains(got, underlineSGR) {
		t.Errorf("focused empty field is underlined: %q", got)
	}
	if got != valid {
		t.Errorf("render changed:\n got %q\nwant %q", got, valid)
	}
}
