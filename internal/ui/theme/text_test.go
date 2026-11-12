package theme

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestStringWidthCountsColumnsNotBytes(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int
	}{
		{"ascii", "abc", 3},
		{"empty", "", 0},
		{"accented is one column per rune", "ééé", 3},
		{"cjk is two columns per rune", "日本", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StringWidth(tt.s); got != tt.want {
				t.Errorf("StringWidth(%q) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}

func TestTruncateWidth(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxWidth int
		want     string
	}{
		{"fits untouched", "abcdef", 10, "abcdef"},
		{"exact fit is not truncated", "abcdef", 6, "abcdef"},
		{"head is kept", "abcdefghij", 8, "abcde..."},
		{"non-positive width yields empty", "abcdef", 0, ""},
		{"negative width yields empty", "abcdef", -5, ""},
		{"width below the marker emits a partial marker", "abcdef", 2, ".."},
		{"multibyte head is kept whole", "ééééééééé", 6, "ééé..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateWidth(tt.s, tt.maxWidth)
			if got != tt.want {
				t.Errorf("TruncateWidth(%q, %d) = %q, want %q", tt.s, tt.maxWidth, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("TruncateWidth(%q, %d) produced invalid UTF-8", tt.s, tt.maxWidth)
			}
			if w := StringWidth(got); w > tt.maxWidth && tt.maxWidth > 0 {
				t.Errorf("TruncateWidth(%q, %d) = %q is %d columns, over budget", tt.s, tt.maxWidth, got, w)
			}
		})
	}
}

func TestTruncateTailWidth(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		maxWidth int
		want     string
	}{
		{"fits untouched", "group/repo", 20, "group/repo"},
		{"exact fit is not truncated", "group/repo", 10, "group/repo"},
		{"tail is kept", "very/long/group/repo", 10, "...up/repo"},
		{"non-positive width yields empty", "group/repo", 0, ""},
		{"negative width yields empty", "group/repo", -5, ""},
		{"width below the marker emits a partial marker", "group/repo", 2, ".."},
		{"multibyte tail is kept whole", "ééééééééé", 6, "...ééé"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateTailWidth(tt.s, tt.maxWidth)
			if got != tt.want {
				t.Errorf("TruncateTailWidth(%q, %d) = %q, want %q", tt.s, tt.maxWidth, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("TruncateTailWidth(%q, %d) produced invalid UTF-8", tt.s, tt.maxWidth)
			}
			if w := StringWidth(got); w > tt.maxWidth && tt.maxWidth > 0 {
				t.Errorf("TruncateTailWidth(%q, %d) = %q is %d columns, over budget", tt.s, tt.maxWidth, got, w)
			}
		})
	}
}

// Double-width glyphs cannot always fill the budget exactly. The result must
// stay under it rather than splitting a glyph to reach it.
func TestTruncateTailWidthNeverExceedsBudgetOnDoubleWidthGlyphs(t *testing.T) {
	s := strings.Repeat("日", 10) // 20 columns

	for _, maxWidth := range []int{6, 7, 8, 9} {
		got := TruncateTailWidth(s, maxWidth)
		if w := StringWidth(got); w > maxWidth {
			t.Errorf("TruncateTailWidth(%q, %d) = %q (%d columns), over budget", s, maxWidth, got, w)
		}
		if !utf8.ValidString(got) {
			t.Errorf("TruncateTailWidth(%q, %d) produced invalid UTF-8", s, maxWidth)
		}
	}
}

// agreedWidths are the strings theme.StringWidth (runewidth) and
// ansi.StringWidth, which the renderer, Lip Gloss and the viewport lay the
// screen out with, must count the same way.
var agreedWidths = []struct {
	name, s string
	want    int
}{
	{"flag FR", "\U0001F1EB\U0001F1F7", 2},
	{"ZWJ family", "\U0001F468\u200d\U0001F469\u200d\U0001F467", 2},
	{"Nerd Font PUA glyph", "\uF07B", 1},
	{"Nerd Font Material glyph (supplementary PUA)", "\U000F0219", 1},
	{"emoji", "\U0001F4C1", 2},
	{"CJK", "日本", 4},
	{"e + combining acute", "é", 1},
	{"ambiguous bullet", "●", 1},
	{"sort arrow", "▲", 1},
	{"mixed", "a\uF07B\U0001F1EB\U0001F1F7b", 5},
}

// TestThemeStringWidthAgreesWithTheGraphemeWidth: StringWidth, which the footer
// and the other helpers here measure with, equals ansi.StringWidth.
//
// Red proof: go-runewidth v0.0.16 (the version before the v2 migration moved
// MVS to v0.0.30) counts the flag as 1 and fails the "flag FR" and "mixed"
// cases.
func TestThemeStringWidthAgreesWithTheGraphemeWidth(t *testing.T) {
	for _, tc := range agreedWidths {
		t.Run(tc.name, func(t *testing.T) {
			rw, an := StringWidth(tc.s), ansi.StringWidth(tc.s)
			if rw != an {
				t.Errorf("%q: StringWidth %d, ansi.StringWidth %d", tc.s, rw, an)
			}
			if an != tc.want {
				t.Errorf("%q: ansi.StringWidth %d, want %d", tc.s, an, tc.want)
			}
		})
	}
}

// TestThemeStringWidthVS16IsAKnownDivergence pins the one place the two
// measures differ: a text-presentation base plus U+FE0F. runewidth counts the
// base (1 cell), ansi the emoji presentation (2). theme.StringWidth stays on
// runewidth because the footer honours the East Asian setting. The datatable no
// longer shares this gap (DEVDESK-280). If this test fails, the divergence is
// closed: it is the cue for the follow-up story.
func TestThemeStringWidthVS16IsAKnownDivergence(t *testing.T) {
	for _, tc := range []struct {
		s              string
		theme, ansiWid int
	}{
		{"✔️", 1, 2},
		{"a❤️x", 3, 4},
	} {
		if got := StringWidth(tc.s); got != tc.theme {
			t.Errorf("theme.StringWidth(%q) = %d, want %d", tc.s, got, tc.theme)
		}
		if got := ansi.StringWidth(tc.s); got != tc.ansiWid {
			t.Errorf("ansi.StringWidth(%q) = %d, want %d", tc.s, got, tc.ansiWid)
		}
	}
}
