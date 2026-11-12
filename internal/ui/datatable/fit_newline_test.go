package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestFitNeverEmitsALineBreak: a cell is one line. Lip Gloss Inline(true), which
// fit used before DEVDESK-280, deleted "\r\n" and "\n" (no replacement); a
// line break left in a cell would split the row on screen.
func TestFitNeverEmitsALineBreak(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		head  bool
		want  string
	}{
		{"lf, room", "a\nb", 6, false, "ab    "},
		{"crlf, room", "a\r\nb", 6, false, "ab    "},
		{"lone cr, room", "a\rb", 6, false, "ab    "},
		{"lf, narrow", "abc\ndef", 4, false, "abc" + truncationMarker},
		{"crlf, narrow", "abc\r\ndef", 4, false, "abc" + truncationMarker},
		{"lf, narrow, head", "abc\ndef", 4, true, truncationMarker + "def"},
		{"trailing lf", "ab\n", 4, false, "ab  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fit(tc.text, tc.width, tc.head)
			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("fit(%q, %d) = %q, contains a line break", tc.text, tc.width, got)
			}
			if w := ansi.StringWidth(got); w != tc.width {
				t.Errorf("fit(%q, %d) is %d cells wide, want %d", tc.text, tc.width, w, tc.width)
			}
			if got != tc.want {
				t.Errorf("fit(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

// TestFitConvertsTabs: Lip Gloss Inline(true) turned a tab into four spaces; a
// raw tab measures 0 for the table but moves the cursor on the terminal.
func TestFitConvertsTabs(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		head  bool
		want  string
	}{
		{"room", "a\tb", 8, false, "a    b  "},
		{"narrow", "a\tb", 4, false, "a  " + truncationMarker},
		{"narrow, head", "a\tb", 4, true, truncationMarker + "  b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fit(tc.text, tc.width, tc.head)
			if got != tc.want {
				t.Errorf("fit(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

// TestFitPrependAtTheEnd: a Prepend character (Arabic number sign and the like)
// merges with the padding space into one zero-width cluster, so the cell came
// out one cell short.
func TestFitPrependAtTheEnd(t *testing.T) {
	for _, s := range []string{"\u0600", "ab\u0600", "\u06dd", "\u070f", "\u08e2"} {
		for w := 1; w <= 6; w++ {
			if got := ansi.StringWidth(fit(s, w, false)); got != w {
				t.Errorf("fit(%q, %d) is %d cells wide, want %d", s, w, got, w)
			}
		}
	}
}
