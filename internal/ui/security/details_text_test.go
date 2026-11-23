package security

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-750 (step 1): the pure text helpers of the finding detail. Widths are
// measured with x/ansi, the measure the viewport itself uses, never runewidth.
// No t.Parallel: HOME is process-global.

// detailWidths are the text widths of the three reference terminals (plan,
// "Measured layout"): 80, 120 and 175 columns less border and padding.
var detailWidths = []int{76, 116, 171}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixtures are POSIX paths")
	}
}

func TestShortPaths(t *testing.T) {
	skipOnWindows(t)
	const home = "/home/tester"
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	tests := []struct {
		name, text, target, want string
	}{
		{"path under the target", "/home/u/repo/k/base: no such file", "/home/u/repo", "k/base: no such file"},
		{"container mount", "/scan/overlays/prod", "/home/u/repo", "overlays/prod"},
		{"container mount without a target", "/scan/overlays/prod", "", "overlays/prod"},
		{"container mount with an image target", "open /scan/k/x.yaml", "nginx:1.25", "open k/x.yaml"},
		{"sibling directory sharing the prefix", "/a/bc/x", "/a/b", "/a/bc/x"},
		{"bare target", "build /home/u/repo failed", "/home/u/repo", "build . failed"},
		{"bare target before a colon", "/home/u/repo: no such file", "/home/u/repo", ".: no such file"},
		{"bare target at the end", "cannot read /home/u/repo", "/home/u/repo", "cannot read ."},
		{"target with a trailing slash", "/home/u/repo/k/base", "/home/u/repo/", "k/base"},
		{"image reference is not a path", "nginx:1.25/x", "nginx:1.25", "nginx:1.25/x"},
		{"relative target is ignored", "repo/k/base", "repo", "repo/k/base"},
		{"path under HOME, outside the target", "/home/tester/other/x.yaml", "/home/u/repo", "~/other/x.yaml"},
		{"HOME lookalike", "/home/testerx/a", "/home/u/repo", "/home/testerx/a"},
		{"target under HOME wins over the home fold", "/home/tester/repo/k/x.yaml", "/home/tester/repo", "k/x.yaml"},
		{"several paths in one text", "a: /home/u/repo/x and /scan/y and /home/tester/z", "/home/u/repo", "a: x and y and ~/z"},
		{"a longer name that starts like the mount", "/scanner/x", "/home/u/repo", "/scanner/x"},
		{"multi-line text keeps its lines", "l1 /home/u/repo/a\nl2 /scan/b", "/home/u/repo", "l1 a\nl2 b"},
		{"no path at all", "plain message", "/home/u/repo", "plain message"},
		{"empty text", "", "/home/u/repo", ""},
		{"empty text and target", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortPaths(tc.text, tc.target); got != tc.want {
				t.Errorf("shortPaths(%q, %q) = %q, want %q", tc.text, tc.target, got, tc.want)
			}
		})
	}
}

func TestOneLine(t *testing.T) {
	long := strings.Repeat("abcdefghij", 40)
	for _, w := range detailWidths {
		t.Run(fmt.Sprintf("long ASCII at %d", w), func(t *testing.T) {
			got := oneLine(long, w)
			if ansi.StringWidth(got) != w {
				t.Errorf("width = %d, want exactly %d: %q", ansi.StringWidth(got), w, got)
			}
			if !strings.HasSuffix(got, theme.Ellipsis) {
				t.Errorf("a cut line must end with %q: %q", theme.Ellipsis, got)
			}
			if !strings.HasPrefix(long, strings.TrimSuffix(got, theme.Ellipsis)) {
				t.Errorf("the kept part is not a prefix of the text: %q", got)
			}
		})
		t.Run(fmt.Sprintf("multi-line at %d", w), func(t *testing.T) {
			got := oneLine("first line\nsecond line\nthird", w)
			if got != "first line" {
				t.Errorf("oneLine = %q, want the first line only", got)
			}
		})
	}

	tests := []struct {
		name, text string
		width      int
		want       string
	}{
		{"fits exactly, no ellipsis", "abcde", 5, "abcde"},
		{"one over", "abcdef", 5, "ab" + theme.Ellipsis},
		{"short", "abc", 76, "abc"},
		{"CRLF", "abc\r\ndef", 76, "abc"},
		{"tab is four cells", "a\tb", 76, "a    b"},
		{"tabs are cut at the width", "a\tb\tc\td", 8, "a    " + theme.Ellipsis},
		{"lone CR", "a\rb", 76, "ab"},
		{"width zero", "abc", 0, ""},
		{"negative width", "abc", -3, ""},
		{"empty", "", 76, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := oneLine(tc.text, tc.width); got != tc.want {
				t.Errorf("oneLine(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

// A wide rune or a ZWJ sequence is never split, whatever the width.
func TestOneLineNeverSplitsAWideCharacter(t *testing.T) {
	texts := map[string]string{
		"CJK":       strings.Repeat("日本語のテキスト", 20),
		"emoji":     strings.Repeat("\U0001F4C1", 80),
		"ZWJ":       strings.Repeat("\U0001F468\u200d\U0001F469\u200d\U0001F467", 40),
		"VS16":      strings.Repeat("✔️", 80),
		"mixed":     strings.Repeat("a日\U0001F4C1b", 40),
		"combining": strings.Repeat("é", 200),
	}
	for name, text := range texts {
		for _, w := range []int{1, 2, 3, 4, 5, 7, 10, 76, 116, 171} {
			t.Run(fmt.Sprintf("%s/%d", name, w), func(t *testing.T) {
				got := oneLine(text, w)
				if !utf8.ValidString(got) {
					t.Fatalf("invalid UTF-8: %q", got)
				}
				if sw := ansi.StringWidth(got); sw > w {
					t.Errorf("width %d > %d: %q", sw, w, got)
				}
				kept := strings.TrimSuffix(got, theme.Ellipsis)
				if !strings.HasPrefix(text, kept) {
					t.Errorf("the kept part is not a prefix of the text: %q", got)
				}
				if strings.HasSuffix(kept, "\u200d") {
					t.Errorf("a dangling ZWJ before the ellipsis: %q", got)
				}
			})
		}
	}
}

func numberedLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(lines, "\n")
}

func maxLineWidth(lines []string) int {
	widest := 0
	for _, l := range lines {
		widest = max(widest, ansi.StringWidth(l))
	}
	return widest
}

func TestCappedLines(t *testing.T) {
	token300 := strings.Repeat("x", 300)
	prose := strings.TrimSpace(strings.Repeat("alpha beta gamma delta ", 40))

	t.Run("fewer lines than the cap", func(t *testing.T) {
		lines, hidden := cappedLines("one\ntwo\nthree", 76, 8)
		if len(lines) != 3 || hidden != 0 {
			t.Errorf("got %d lines, hidden %d; want 3, 0: %q", len(lines), hidden, lines)
		}
	})
	t.Run("a trailing newline adds no line", func(t *testing.T) {
		lines, hidden := cappedLines("one\ntwo\n", 76, 8)
		if len(lines) != 2 || hidden != 0 {
			t.Errorf("got %d lines, hidden %d; want 2, 0: %q", len(lines), hidden, lines)
		}
	})
	t.Run("twenty lines keep eight", func(t *testing.T) {
		lines, hidden := cappedLines(numberedLines(20), 76, maxMessageLines)
		if len(lines) != 8 || hidden != 12 {
			t.Fatalf("got %d lines, hidden %d; want 8, 12", len(lines), hidden)
		}
		if lines[0] != "line 1" || lines[7] != "line 8" {
			t.Errorf("the first lines must be kept, in order: %q", lines)
		}
	})
	t.Run("exactly the cap hides nothing", func(t *testing.T) {
		lines, hidden := cappedLines(numberedLines(8), 76, 8)
		if len(lines) != 8 || hidden != 0 {
			t.Errorf("got %d lines, hidden %d; want 8, 0", len(lines), hidden)
		}
	})
	t.Run("hidden counts wrapped lines, not source lines", func(t *testing.T) {
		// Ten source lines of a 152-char unbreakable token: two wrapped lines each.
		text := strings.TrimSuffix(strings.Repeat(strings.Repeat("y", 152)+"\n", 10), "\n")
		lines, hidden := cappedLines(text, 76, 8)
		if len(lines) != 8 || hidden != 12 {
			t.Errorf("got %d lines, hidden %d; want 8, 12", len(lines), hidden)
		}
	})
	t.Run("a long unbreakable token is hard-broken at 76 without loss", func(t *testing.T) {
		lines, hidden := cappedLines(token300, 76, 100)
		if hidden != 0 {
			t.Errorf("hidden = %d, want 0", hidden)
		}
		if widest := maxLineWidth(lines); widest != 76 {
			t.Errorf("widest line = %d, want 76: %q", widest, lines)
		}
		if got := strings.Join(lines, ""); got != token300 {
			t.Errorf("the hard break lost or added characters: %q", got)
		}
	})
	t.Run("the same token under the cap", func(t *testing.T) {
		lines, hidden := cappedLines(token300, 76, 3)
		if len(lines) != 3 || hidden != 1 {
			t.Errorf("got %d lines, hidden %d; want 3, 1 (300 cells at 76 = 4 lines)", len(lines), hidden)
		}
	})
	t.Run("width 175 is capped at 110", func(t *testing.T) {
		lines, _ := cappedLines(token300, 175, 100)
		if widest := maxLineWidth(lines); widest != maxMessageWidth {
			t.Errorf("token: widest line = %d, want %d", widest, maxMessageWidth)
		}
		lines, _ = cappedLines(prose, 175, 100)
		if widest := maxLineWidth(lines); widest > maxMessageWidth || widest <= 100 {
			t.Errorf("prose: widest line = %d, want in (100, %d]", widest, maxMessageWidth)
		}
	})
	t.Run("never wider than the width", func(t *testing.T) {
		for _, w := range []int{20, 40, 76, 116} {
			for _, text := range []string{token300, prose, strings.Repeat("日本語", 80)} {
				lines, _ := cappedLines(text, w, 100)
				if widest := maxLineWidth(lines); widest > min(w, maxMessageWidth) {
					t.Errorf("width %d: widest line = %d", w, widest)
				}
			}
		}
	})
	t.Run("blank input", func(t *testing.T) {
		for _, text := range []string{"", "  \n", "\n\n", "   "} {
			lines, hidden := cappedLines(text, 76, 8)
			if lines != nil || hidden != 0 {
				t.Errorf("cappedLines(%q) = %q, %d; want nil, 0", text, lines, hidden)
			}
		}
	})
}

func TestMoreLines(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "… 1 more line"},
		{2, "… 2 more lines"},
		{5, "… 5 more lines"},
		{12, "… 12 more lines"},
		{0, ""},
		{-1, ""},
	}
	for _, tc := range tests {
		if got := moreLines(tc.n); got != tc.want {
			t.Errorf("moreLines(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestScrollPosition(t *testing.T) {
	tests := []struct {
		name                   string
		offset, visible, total int
		want                   string
	}{
		{"top", 0, 12, 40, "Lines 1-12 of 40"},
		{"one down", 1, 12, 40, "Lines 2-13 of 40"},
		{"bottom", 28, 12, 40, "Lines 29-40 of 40"},
		{"past the bottom is clamped", 35, 12, 40, "Lines 36-40 of 40"},
		{"one line over", 0, 12, 13, "Lines 1-12 of 13"},
		{"everything visible", 0, 12, 12, ""},
		{"fewer lines than the viewport", 0, 12, 5, ""},
		{"empty", 0, 12, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scrollPosition(tc.offset, tc.visible, tc.total); got != tc.want {
				t.Errorf("scrollPosition(%d, %d, %d) = %q, want %q", tc.offset, tc.visible, tc.total, got, tc.want)
			}
		})
	}
}
