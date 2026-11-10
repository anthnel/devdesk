package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Strings whose width a terminal and a library can disagree on. The table
// measures with runewidth (render.go: fit, FillRight), the viewport and
// lipgloss with ansi.StringWidth: Rule 116 holds only while the two agree on
// what a cell holds.
var (
	flagFR    = "\U0001F1EB\U0001F1F7"                       // regional indicators F + R
	zwjFamily = "\U0001F468\u200d\U0001F469\u200d\U0001F467" // man ZWJ woman ZWJ girl
	nerdFont  = ""                                          // Nerd Font folder, Private Use Area
	folder    = "\U0001F4C1"                                 // emoji, 2 cells
)

// agreedWidths are the strings both measures must count the same way.
var agreedWidths = []struct {
	name, s string
	want    int
}{
	{"flag FR", flagFR, 2},
	{"ZWJ family", zwjFamily, 2},
	{"Nerd Font PUA glyph", nerdFont, 1},
	{"emoji", folder, 2},
	{"CJK", "日本", 4},
	{"e + combining acute", "é", 1},
	{"ambiguous bullet", "●", 1},
	{"sort arrow", "▲", 1},
	{"mixed", "a" + nerdFont + flagFR + "b", 5},
}

// TestDatatableWidthAgreesWithTheGraphemeWidth: the width the datatable pads
// and truncates by (runewidth) equals ansi.StringWidth, which is what the
// renderer and lipgloss lay the screen out with.
//
// Red proof: go-runewidth v0.0.16 (the version before the v2 migration moved
// MVS to v0.0.30) counts the flag as 1 and fails the "flag FR" and "mixed"
// cases; see the report for the exact command.
func TestDatatableWidthAgreesWithTheGraphemeWidth(t *testing.T) {
	for _, tc := range agreedWidths {
		t.Run(tc.name, func(t *testing.T) {
			rw, an := runewidth.StringWidth(tc.s), ansi.StringWidth(tc.s)
			if rw != an {
				t.Errorf("%q: runewidth %d, ansi.StringWidth %d", tc.s, rw, an)
			}
			if an != tc.want {
				t.Errorf("%q: ansi.StringWidth %d, want %d", tc.s, an, tc.want)
			}
		})
	}
}

// vs16 are emoji written as a text-presentation base plus U+FE0F. runewidth
// counts the base (1 cell), ansi.StringWidth the presentation (2 cells).
var vs16 = []struct {
	s                 string
	runewidthW, ansiW int
}{
	{"a❤️x", 3, 4},
	{"✔️", 1, 2},
}

// TestVS16EmojiIsAKnownWidthDivergence pins the one known gap (spike section 7):
// the two measures differ on VS16 emoji. It is asserted as a divergence so a
// library fix shows up as a failure here, to be turned into an agreement test.
// Whether the terminal draws 1 or 2 cells depends on mode 2027, which only a
// real terminal answers (manual check in G2).
func TestVS16EmojiIsAKnownWidthDivergence(t *testing.T) {
	for _, tc := range vs16 {
		if got := runewidth.StringWidth(tc.s); got != tc.runewidthW {
			t.Errorf("runewidth.StringWidth(%q) = %d, pinned at %d", tc.s, got, tc.runewidthW)
		}
		if got := ansi.StringWidth(tc.s); got != tc.ansiW {
			t.Errorf("ansi.StringWidth(%q) = %d, pinned at %d", tc.s, got, tc.ansiW)
		}
	}
}

// TestTheTableFillsTheViewportAtThreeWidths: at three widths, with cells that
// carry ambiguous-width icons (Nerd Font, emoji, flag, ZWJ), every line, header,
// selected and unselected rows included, spans exactly RenderedWidth(), which is
// the viewport's interior (Rule 116). VS16 emoji are left out on purpose, they
// are the pinned divergence above.
func TestTheTableFillsTheViewportAtThreeWidths(t *testing.T) {
	names := []string{nerdFont + " src", folder + " docs", flagFR + " fr", zwjFamily + " fam", "日本語", "plain"}
	for _, width := range []int{60, 100, 140} {
		for _, selectedName := range names {
			m := New(indicatorConfig(nil))
			m.Resize(width, 12)
			items := make([]row, len(names))
			for i, n := range names {
				items[i] = row{Name: n}
			}
			m.SetItems(items)
			cursorOn(t, &m, selectedName)

			want := m.RenderedWidth()
			if want != width-borderWidth {
				t.Errorf("width %d: RenderedWidth %d, want the viewport interior %d", width, want, width-borderWidth)
			}
			for i, line := range strings.Split(m.View(), "\n") {
				if got := ansi.StringWidth(line); got != want {
					t.Errorf("width %d, cursor on %q, line %d: %d cells, want %d: %q",
						width, selectedName, i, got, want, ansi.Strip(line))
				}
				if got := runewidth.StringWidth(ansi.Strip(line)); got != want {
					t.Errorf("width %d, cursor on %q, line %d: runewidth %d, want %d (the two measures disagree)",
						width, selectedName, i, got, want)
				}
			}
		}
	}
}
