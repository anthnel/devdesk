package components

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-790 (step E5 of DEVDESK-785): ClampField cuts the rendered line of a
// one-line field to a width, walking grapheme clusters the way the renderer
// measures them (ansi.GraphemeWidth), so a field that holds emoji with a
// variation selector never draws wider than its box.
//
// This file is kept apart from field_vs16_test.go on purpose: ClampField does
// not exist yet, so the package does not compile with this file in it. The
// tests of field_vs16_test.go need nothing but what exists today.

const (
	clampRed   = "\x1b[31m"
	clampTrue  = "\x1b[38;2;1;2;3m"
	clampReset = "\x1b[0m"
)

// sgrSeq matches one complete SGR sequence.
var sgrSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// lastSGRIsReset reports whether the last style sequence of s is a reset (or
// there is none): a cut line must not leave a colour on for what follows it.
func lastSGRIsReset(s string) bool {
	all := sgrSeq.FindAllString(s, -1)
	if len(all) == 0 {
		return true
	}
	last := all[len(all)-1]
	return last == "\x1b[0m" || last == "\x1b[m"
}

// checkClamp fails unless got is a valid clamp of in to width: it spans at most
// width cells, its text is a prefix of the text of in, and no escape sequence
// is cut in half.
func checkClamp(t *testing.T, in string, width int, got string) {
	t.Helper()
	if w := ansi.StringWidth(got); w > width {
		t.Errorf("ClampField(%q, %d) spans %d cells, want at most %d", in, width, w, width)
	}
	plain, gotPlain := ansi.Strip(in), ansi.Strip(got)
	if !strings.HasPrefix(plain, gotPlain) {
		t.Errorf("ClampField(%q, %d) text %q is not a prefix of %q (a cluster or a sequence was split)",
			in, width, gotPlain, plain)
	}
	if strings.ContainsRune(gotPlain, '\x1b') {
		t.Errorf("ClampField(%q, %d) leaves a cut escape sequence in %q", in, width, got)
	}
}

func TestClampFieldNeverSplitsACluster(t *testing.T) {
	// Every cluster below is 2 cells wide under ansi.GraphemeWidth. want is the
	// text left once the escape sequences are stripped.
	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		// Variation selector: a heart is 1 cell as text, 2 with U+FE0F.
		{"VS16 heart, 4 cells", "❤️❤️❤️", 4, "❤️❤️"},
		{"VS16 heart, 3 cells", "❤️❤️❤️", 3, "❤️"},
		{"VS16 heart, 2 cells", "❤️❤️❤️", 2, "❤️"},
		{"VS16 heart, 1 cell", "❤️❤️❤️", 1, ""},
		{"VS16 heart, 0 cells", "❤️❤️❤️", 0, ""},
		{"VS16 heart after text", "ab❤️cd", 3, "ab"},
		{"VS16 heart after text, exact", "ab❤️cd", 4, "ab❤️"},
		{"text heart without the selector", "❤❤❤", 2, "❤❤"},
		// Keycap: digit, U+FE0F, U+20E3.
		{"keycap, 4 cells", "1️⃣2️⃣3️⃣", 4, "1️⃣2️⃣"},
		{"keycap, 3 cells", "1️⃣2️⃣3️⃣", 3, "1️⃣"},
		{"keycap, 1 cell", "1️⃣2️⃣", 1, ""},
		// Flag: two regional indicators.
		{"flag, 4 cells", "🇫🇷🇫🇷🇫🇷", 4, "🇫🇷🇫🇷"},
		{"flag, 3 cells", "🇫🇷🇫🇷", 3, "🇫🇷"},
		{"flag, 1 cell", "🇫🇷", 1, ""},
		// CJK.
		{"CJK, 5 cells", "日本語", 5, "日本"},
		{"CJK, 3 cells", "日本語", 3, "日"},
		{"CJK, 1 cell", "日本語", 1, ""},
		{"CJK after ASCII, odd room", "a日本", 2, "a"},
		// ZWJ sequence: three people joined, one cluster of 2 cells.
		{"ZWJ family, 3 cells", "👨‍👩‍👧👨‍👩‍👧", 3, "👨‍👩‍👧"},
		{"ZWJ family, 2 cells", "👨‍👩‍👧x", 2, "👨‍👩‍👧"},
		{"ZWJ family, 1 cell", "👨‍👩‍👧x", 1, ""},
		{"ZWJ family, room for the tail", "👨‍👩‍👧x", 3, "👨‍👩‍👧x"},
		// Decomposed accent: e and U+0301 are one cell together.
		{"combining accent", "e\u0301e\u0301e\u0301", 2, "e\u0301e\u0301"},
		// Plain ASCII.
		{"ASCII", "abcdef", 4, "abcd"},
		// Style sequences are kept and never cut.
		{"red text cut inside", clampRed + "red" + clampReset, 2, "re"},
		{"red heart cut before it", clampRed + "a❤️" + clampReset, 2, "a"},
		{"red heart kept", clampRed + "a❤️" + clampReset, 3, "a❤️"},
		{"truecolor then text", clampTrue + "abc" + clampReset, 1, "a"},
		{"style in the middle", "a" + clampRed + "b" + clampReset + "c", 2, "ab"},
		{"two styled segments, cut in the second", clampRed + "ab" + clampReset + clampTrue + "cd" + clampReset, 3, "abc"},
		{"styled CJK", clampRed + "日本" + clampReset, 3, "日"},
		{"zero room keeps no text", clampRed + "abc" + clampReset, 0, ""},
		// A string that fits is returned as it is, sequences included.
		{"fits exactly", "abcd", 4, "abcd"},
		{"shorter than the width", "ab", 10, "ab"},
		{"VS16 shorter than the width", "❤️", 10, "❤️"},
		{"styled and shorter", clampRed + "red" + clampReset, 10, "red"},
		{"empty", "", 5, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClampField(tc.in, tc.width)

			checkClamp(t, tc.in, tc.width, got)
			if plain := ansi.Strip(got); plain != tc.want {
				t.Errorf("ClampField(%q, %d) text = %q, want %q", tc.in, tc.width, plain, tc.want)
			}
			if ansi.StringWidth(tc.in) <= tc.width && got != tc.in {
				t.Errorf("ClampField(%q, %d) = %q, a string that fits must come back unchanged", tc.in, tc.width, got)
			}
			if !lastSGRIsReset(got) && lastSGRIsReset(tc.in) {
				t.Errorf("ClampField(%q, %d) = %q leaves a style on: the cut line would colour what follows it",
					tc.in, tc.width, got)
			}
		})
	}
}

func TestClampFieldDoesNotPad(t *testing.T) {
	for _, in := range []string{"", "a", "❤️", "日本", clampRed + "ab" + clampReset} {
		for _, width := range []int{0, 1, 2, 5, 40} {
			got := ClampField(in, width)
			if ansi.StringWidth(got) > ansi.StringWidth(in) {
				t.Errorf("ClampField(%q, %d) = %q is wider than its input: it must not fill", in, width, got)
			}
		}
	}
}

// FuzzClampFieldSpansAtMostWidth states the whole contract as a property: the
// result never spans more than width cells, its text is a prefix of the text of
// the input (so no cluster and no sequence is split), and an input that fits is
// returned as it is. The inputs are text and complete SGR sequences: a cut or
// stray control character is not something a rendered field ever holds.
func FuzzClampFieldSpansAtMostWidth(f *testing.F) {
	seeds := []struct {
		s string
		w int
	}{
		{"❤️❤️❤️", 4}, {"❤️❤️❤️", 3}, {"❤️❤️❤️", 1}, {"❤️❤️❤️", 0},
		{"ab❤️cd", 3}, {"1️⃣2️⃣3️⃣", 3}, {"🇫🇷🇫🇷🇫🇷", 3}, {"日本語", 3}, {"日本語", 1},
		{"👨‍👩‍👧👨‍👩‍👧", 3}, {"e\u0301e\u0301", 1},
		{clampRed + "red" + clampReset, 2}, {clampRed + "a❤️" + clampReset, 2},
		{clampTrue + "abc" + clampReset, 1}, {"ab", 10}, {"", 3},
	}
	for _, s := range seeds {
		f.Add(s.s, s.w)
	}
	f.Fuzz(func(t *testing.T, s string, w int) {
		if !utf8.ValidString(s) {
			t.Skip("not UTF-8")
		}
		for _, r := range sgrSeq.ReplaceAllString(s, "") {
			if unicode.IsControl(r) {
				t.Skip("control character outside an SGR sequence")
			}
		}
		if w < 0 {
			w = -w
		}
		w %= 200

		got := ClampField(s, w)

		checkClamp(t, s, w, got)
		if ansi.StringWidth(s) <= w && got != s {
			t.Errorf("ClampField(%q, %d) = %q, a string that fits must come back unchanged", s, w, got)
		}
	})
}
