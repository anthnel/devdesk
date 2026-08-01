package components

import (
	"strings"
	"testing"

	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/testutil"
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
