package overlay

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	reset     = "\x1b[0m"
	baseSGR   = "\x1b[1;38;2;166;227;161;48;2;30;30;46m"
	boxSGR    = "\x1b[38;2;243;139;168m"
	underline = "\x1b[4m"
)

var widths = []int{40, 60, 80, 120}

// completeCSI matches a well-formed CSI sequence; anything else starting with
// ESC is a cut sequence.
var completeCSI = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)

// assertNoOrphan fails when s holds an escape sequence that is not complete, or
// a fragment of one left behind once the complete ones are removed.
func assertNoOrphan(t *testing.T, s string) {
	t.Helper()
	rest := completeCSI.ReplaceAllString(s, "")
	if strings.Contains(rest, "\x1b") {
		t.Errorf("incomplete escape sequence in %q", s)
	}
	plain := ansi.Strip(s)
	if strings.Contains(plain, "[38;2") || strings.Contains(plain, "[0m") || strings.Contains(plain, "\x1b") {
		t.Errorf("orphan escape fragment left after Strip: %q", plain)
	}
}

func assertWidth(t *testing.T, s string, width int) {
	t.Helper()
	if got := lipgloss.Width(s); got != width {
		t.Errorf("width = %d, want %d (line %q)", got, width, s)
	}
}

func TestSplicePlain(t *testing.T) {
	for _, width := range widths {
		tests := []struct {
			name string
			x    int
			box  string
		}{
			{"middle", 10, "[ palette ]"},
			{"left edge", 0, "[ palette ]"},
			{"right edge", width - 11, "[ palette ]"},
			{"whole line", 0, strings.Repeat("#", width)},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				line := strings.Repeat(".", width)
				got, ok := Splice(line, tc.box, tc.x, width)
				if !ok {
					t.Fatalf("Splice reported failure for a box that fits")
				}
				bw := lipgloss.Width(tc.box)
				want := line[:tc.x] + tc.box + line[tc.x+bw:]
				if ansi.Strip(got) != want {
					t.Errorf("stripped = %q, want %q", ansi.Strip(got), want)
				}
				assertWidth(t, got, width)
				assertNoOrphan(t, got)
			})
		}
	}
}

func TestSpliceStyledBase(t *testing.T) {
	tests := []struct {
		name string
		line func(width int) string
		// restored is the prefix the right part must open with after the box.
		restored string
	}{
		{
			name:     "one segment spanning the line",
			line:     func(w int) string { return baseSGR + strings.Repeat("a", w) + reset },
			restored: baseSGR,
		},
		{
			name: "underlined base",
			line: func(w int) string {
				return underline + strings.Repeat("a", w) + reset
			},
			restored: underline,
		},
		{
			name: "style changes before the cut",
			line: func(w int) string {
				return baseSGR + strings.Repeat("a", 5) + boxSGR + strings.Repeat("b", w-5) + reset
			},
			restored: boxSGR,
		},
	}
	for _, width := range widths {
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				box := boxSGR + "BOX" + reset
				got, ok := Splice(tc.line(width), box, 10, width)
				if !ok {
					t.Fatal("Splice reported failure")
				}
				assertWidth(t, got, width)
				assertNoOrphan(t, got)

				plain := ansi.Strip(got)
				if plain[10:13] != "BOX" {
					t.Errorf("box not at column 10: %q", plain)
				}
				_, after, found := strings.Cut(got, "BOX")
				if !found {
					t.Fatalf("box text missing from %q", got)
				}
				// The right part must reopen the style the base had at the cut,
				// after a reset that closes the box's own.
				if !strings.Contains(after, reset+tc.restored) {
					t.Errorf("right part does not restore %q after a reset: %q", tc.restored, after)
				}
			})
		}
	}
}

func TestSpliceCutInsideStyledRun(t *testing.T) {
	// A long SGR sequence sits exactly at the cut columns: it must not be split.
	for _, x := range []int{4, 5, 6, 9, 10, 11} {
		line := "aaaaa" + baseSGR + "bbbbbbbbbb" + reset + strings.Repeat("c", 30)
		got, ok := Splice(line, "XY", x, 45)
		if !ok {
			t.Fatalf("x=%d: failure", x)
		}
		assertWidth(t, got, 45)
		assertNoOrphan(t, got)
	}
}

func TestSpliceShortOrEmptyBase(t *testing.T) {
	for _, width := range widths {
		for _, line := range []string{"", "short", baseSGR + "short" + reset} {
			got, ok := Splice(line, "BOX", 20, width)
			if !ok {
				t.Fatalf("width %d, line %q: failure", width, line)
			}
			assertWidth(t, got, width)
			assertNoOrphan(t, got)
			if ansi.Strip(got)[20:23] != "BOX" {
				t.Errorf("box not at column 20: %q", ansi.Strip(got))
			}
		}
	}
}

func TestSpliceWideRuneAtTheEdges(t *testing.T) {
	// 20 cells: ten double-width runes, each starting on an even column.
	const wide = "世世世世世世世世世世"
	t.Run("left cut straddles a wide rune", func(t *testing.T) {
		got, ok := Splice(wide, "AB", 3, 20)
		if !ok {
			t.Fatal("expected a compensated splice, got failure")
		}
		assertWidth(t, got, 20)
		assertNoOrphan(t, got)
		plain := ansi.Strip(got)
		if !strings.HasPrefix(plain, "世 AB") {
			t.Errorf("left part not compensated by one space: %q", plain)
		}
	})
	t.Run("right cut straddles a wide rune", func(t *testing.T) {
		// box covers columns 2..4, so the rune on columns 4-5 is cut at its start.
		got, ok := Splice(wide, "ABC", 2, 20)
		if !ok {
			t.Fatal("expected a compensated splice, got failure")
		}
		assertWidth(t, got, 20)
		assertNoOrphan(t, got)
		plain := ansi.Strip(got)
		if !strings.HasPrefix(plain, "世ABC ") {
			t.Errorf("right part not prefixed by one space: %q", plain)
		}
	})
	t.Run("both edges at once", func(t *testing.T) {
		got, ok := Splice(wide, "ABC", 3, 20)
		if !ok {
			t.Fatal("failure")
		}
		assertWidth(t, got, 20)
		assertNoOrphan(t, got)
	})
	t.Run("styled wide base", func(t *testing.T) {
		got, ok := Splice(baseSGR+wide+reset, "AB", 3, 20)
		if !ok {
			t.Fatal("failure")
		}
		assertWidth(t, got, 20)
		assertNoOrphan(t, got)
	})
}

// forceColors makes lipgloss emit SGR sequences even without a terminal.
func forceColors(t *testing.T) {
	t.Helper()
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })
}

// TestSpliceFillCellsCarryTheAppBackground: cells the overlay invents (wide
// rune compensation, padding of a short base) are not bare spaces after a
// reset, which would show the terminal's own background (Rule 115).
func TestSpliceFillCellsCarryTheAppBackground(t *testing.T) {
	forceColors(t)
	if theme.Bg(" ") == " " {
		t.Fatal("theme.Bg emits no SGR; the test cannot observe the background")
	}
	t.Run("wide rune cut on the left", func(t *testing.T) {
		got, ok := Splice("世世世世世世世世世世", "AB", 3, 20)
		if !ok {
			t.Fatal("failure")
		}
		if !strings.Contains(got, theme.Bg(" ")) {
			t.Errorf("compensation space has no background: %q", got)
		}
	})
	t.Run("wide rune cut on the right", func(t *testing.T) {
		got, _ := Splice("世世世世世世世世世世", "ABC", 2, 20)
		if !strings.Contains(got, theme.Bg(" ")) {
			t.Errorf("compensation space has no background: %q", got)
		}
	})
	t.Run("base shorter than the box column", func(t *testing.T) {
		got, _ := Splice("short", "BOX", 20, 40)
		if !strings.Contains(got, theme.Bg(strings.Repeat(" ", 15))) {
			t.Errorf("left padding has no background: %q", got)
		}
		if !strings.Contains(got, theme.Bg(strings.Repeat(" ", 17))) {
			t.Errorf("right padding has no background: %q", got)
		}
	})
}

func TestSpliceBoxWiderThanBase(t *testing.T) {
	tests := []struct {
		name  string
		x     int
		box   string
		width int
	}{
		{"wider than the room right of x", 30, strings.Repeat("#", 20), 40},
		{"wider than the whole line", 0, strings.Repeat("#", 50), 40},
		{"x beyond width", 45, "BOX", 40},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := strings.Repeat(".", tc.width)
			got, ok := Splice(line, tc.box, tc.x, tc.width)
			if ok {
				t.Errorf("ok = true, want false (got %q)", got)
			}
			assertNoOrphan(t, got)
		})
	}
}

func TestSpliceEmptyBox(t *testing.T) {
	line := strings.Repeat(".", 40)
	got, ok := Splice(line, "", 5, 40)
	if !ok {
		t.Fatal("empty box must not fail")
	}
	assertWidth(t, got, 40)
	if ansi.Strip(got) != line {
		t.Errorf("empty box altered the line: %q", ansi.Strip(got))
	}
}

func TestComposeBasics(t *testing.T) {
	for _, width := range widths {
		base := make([]string, 10)
		for i := range base {
			base[i] = baseSGR + strings.Repeat("a", width) + reset
		}
		box := []string{"+----+", "| hi |", "+----+"}

		got := Compose(base, box, 7, 3, width)

		if len(got) != len(base) {
			t.Fatalf("width %d: %d lines, want %d", width, len(got), len(base))
		}
		for i, l := range got {
			assertWidth(t, l, width)
			assertNoOrphan(t, l)
			plain := ansi.Strip(l)
			if i >= 3 && i < 6 {
				if got, want := plain[7:13], box[i-3]; got != want {
					t.Errorf("row %d: box = %q, want %q", i, got, want)
				}
			} else if plain != strings.Repeat("a", width) {
				t.Errorf("row %d outside the box changed: %q", i, plain)
			}
		}
	}
}

func TestComposeDoesNotMutateBase(t *testing.T) {
	base := []string{"aaaa", "bbbb"}
	orig := append([]string(nil), base...)
	_ = Compose(base, []string{"X"}, 1, 0, 4)
	if !reflect.DeepEqual(base, orig) {
		t.Errorf("base mutated: %q", base)
	}
}

func TestComposePadsShortBaseLines(t *testing.T) {
	got := Compose([]string{"", "ab"}, []string{"BOX", "BOX"}, 2, 0, 40)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	for _, l := range got {
		assertWidth(t, l, 40)
		if ansi.Strip(l)[2:5] != "BOX" {
			t.Errorf("box missing: %q", ansi.Strip(l))
		}
	}
}

func TestComposeOutOfBounds(t *testing.T) {
	base := []string{strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)}
	box := []string{"111", "222", "333"}

	tests := []struct {
		name      string
		y         int
		spliced   []int // rows expected to carry box text
		unchanged bool
	}{
		{"y beyond base", 10, nil, true},
		{"y equals len", 3, nil, true},
		{"box overruns the bottom", 2, []int{2}, false},
		{"y negative drops the top rows", -1, []int{0, 1}, false},
		{"y far negative", -10, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Compose(base, box, 5, tc.y, 40)
			if len(got) != len(base) {
				t.Fatalf("%d lines, want %d", len(got), len(base))
			}
			for i, l := range got {
				assertWidth(t, l, 40)
				assertNoOrphan(t, l)
				has := strings.Contains(ansi.Strip(l), "111") ||
					strings.Contains(ansi.Strip(l), "222") ||
					strings.Contains(ansi.Strip(l), "333")
				want := false
				for _, r := range tc.spliced {
					want = want || r == i
				}
				if has != want {
					t.Errorf("row %d: box present = %v, want %v (%q)", i, has, want, ansi.Strip(l))
				}
			}
			if tc.unchanged {
				for i := range base {
					if ansi.Strip(got[i]) != base[i] {
						t.Errorf("row %d changed: %q", i, ansi.Strip(got[i]))
					}
				}
			}
		})
	}
}

func TestComposeUnchangedInputs(t *testing.T) {
	base := []string{baseSGR + strings.Repeat("a", 40) + reset, "second"}
	tests := []struct {
		name      string
		box       []string
		x, y      int
		width     int
		emptyBase bool
		wantSame  bool
	}{
		{"empty box", nil, 2, 0, 40, false, true},
		{"box of empty strings", []string{""}, 2, 0, 40, false, false},
		{"negative x", []string{"BOX"}, -1, 0, 40, false, true},
		{"zero width", []string{"BOX"}, 2, 0, 0, false, true},
		{"negative width", []string{"BOX"}, 2, 0, -5, false, true},
		{"empty base", []string{"BOX"}, 2, 0, 40, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			if tc.emptyBase {
				in = nil
			}
			got := Compose(in, tc.box, tc.x, tc.y, tc.width)
			if len(got) != len(in) {
				t.Fatalf("%d lines, want %d", len(got), len(in))
			}
			if tc.wantSame && !reflect.DeepEqual(got, in) {
				t.Errorf("base not returned unchanged: %q", got)
			}
		})
	}
}

func TestComposeFallbackOnlyForTheFailingRow(t *testing.T) {
	const width = 40
	base := []string{strings.Repeat("a", width), strings.Repeat("b", width), strings.Repeat("c", width)}
	// Row 1 is wider than the room right of x=30; rows 0 and 2 fit.
	box := []string{"ok", strings.Repeat("#", 20), "ok"}

	got := Compose(base, box, 30, 0, width)
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	for i, l := range got {
		assertWidth(t, l, width)
		assertNoOrphan(t, l)
		if i != 1 && ansi.Strip(l)[30:32] != "ok" {
			t.Errorf("row %d should be spliced: %q", i, ansi.Strip(l))
		}
	}
	plain := ansi.Strip(got[1])
	if want := strings.Repeat("b", 30) + strings.Repeat("#", 10); plain != want {
		t.Errorf("fallback row = %q, want the box cut at width-x inserted at x: %q", plain, want)
	}
}

func TestComposeBoxWiderThanBase(t *testing.T) {
	for _, width := range widths {
		base := []string{strings.Repeat("a", width), strings.Repeat("b", width)}
		box := []string{strings.Repeat("#", width+30), "x"}
		got := Compose(base, box, 0, 0, width)
		if len(got) != 2 {
			t.Fatalf("len = %d", len(got))
		}
		for _, l := range got {
			assertWidth(t, l, width)
			assertNoOrphan(t, l)
		}
	}
}

func TestComposeStyledWideRunes(t *testing.T) {
	const width = 40
	line := baseSGR + strings.Repeat("世", width/2) + reset
	base := []string{line, line, line}
	for x := 0; x < 8; x++ {
		got := Compose(base, []string{"ABC", "DEF"}, x, 1, width)
		for i, l := range got {
			assertWidth(t, l, width)
			assertNoOrphan(t, l)
			if i == 0 && ansi.Strip(l) != ansi.Strip(line) {
				t.Errorf("x=%d: untouched row changed", x)
			}
		}
	}
}
