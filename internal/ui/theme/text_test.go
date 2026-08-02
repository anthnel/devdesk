package theme

import (
	"strings"
	"testing"
	"unicode/utf8"
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
