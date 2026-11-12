package datatable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Strings whose width a terminal and a library can disagree on. Rule 116 holds
// only while the table and the viewport (ansi.StringWidth) agree on what a
// cell holds.
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

// vs16Strings are emoji written as a text-presentation base plus U+FE0F (or a
// keycap sequence). x/ansi, Lip Gloss and the viewport count the presentation
// (2 cells); go-runewidth counts the base (1 cell). The datatable follows the
// former (DEVDESK-280); theme.StringWidth still reads the latter.
var vs16Strings = []struct {
	name, s string
	want    int // ansi.StringWidth
}{
	{"check mark", "✔️", 2},
	{"heart in text", "a❤️x", 4},
	{"sun", "☀️", 2},
	{"four hearts", "❤️❤️❤️❤️", 8},
	{"keycap", "1️⃣", 2},
	{"three keycaps", "1️⃣2️⃣3️⃣", 6},
}

// TestDatatableMeasuresVS16LikeAnsi: whatever the width, fit returns a cell
// that ansi.StringWidth (Lip Gloss, viewport) measures at exactly that width,
// for VS16 emoji too, cut from the tail or from the head. It fails as soon as
// the table's measure and x/ansi diverge.
func TestDatatableMeasuresVS16LikeAnsi(t *testing.T) {
	for _, tc := range vs16Strings {
		t.Run(tc.name, func(t *testing.T) {
			if got := ansi.StringWidth(tc.s); got != tc.want {
				t.Fatalf("ansi.StringWidth(%q) = %d, the fixture says %d", tc.s, got, tc.want)
			}
			if got := cellWidth(tc.s); got != ansi.StringWidth(tc.s) {
				t.Errorf("cellWidth(%q) = %d, ansi.StringWidth %d", tc.s, got, ansi.StringWidth(tc.s))
			}
			for _, head := range []bool{false, true} {
				for w := 1; w <= 10; w++ {
					out := fit(tc.s, w, head)
					if got := ansi.StringWidth(out); got != w {
						t.Errorf("fit(%q, %d, head=%t) = %q: %d cells, want %d", tc.s, w, head, out, got, w)
					}
				}
			}
		})
	}
}

// clusterBounds returns every byte offset of s that starts a grapheme cluster,
// plus len(s): the places a cut may fall.
func clusterBounds(s string) map[int]bool {
	bounds := map[int]bool{0: true}
	for i := 0; i < len(s); {
		cluster, _ := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		i += len(cluster)
		bounds[i] = true
	}
	return bounds
}

// TestAVS16CutNeverSplitsACluster: what fit keeps of an over-long value is a
// prefix (tail cut) or a suffix (head cut) that begins and ends on a grapheme
// cluster boundary of the input, and the cut always shows the marker.
func TestAVS16CutNeverSplitsACluster(t *testing.T) {
	inputs := []string{"ab✔️cd✔️ef", "x✔️", "日本語テキスト"}
	for _, tc := range vs16Strings {
		inputs = append(inputs, tc.s)
	}
	for _, s := range inputs {
		bounds := clusterBounds(s)
		for _, head := range []bool{false, true} {
			for w := 1; w <= 10; w++ {
				if ansi.StringWidth(s) <= w {
					continue // nothing is cut
				}
				out := fit(s, w, head)
				if !strings.Contains(out, truncationMarker) {
					t.Errorf("fit(%q, %d, head=%t) = %q: cut without the %q marker", s, w, head, out, truncationMarker)
					continue
				}
				if head {
					kept := strings.TrimLeft(strings.TrimPrefix(out, truncationMarker), " ")
					if !strings.HasSuffix(s, kept) || !bounds[len(s)-len(kept)] {
						t.Errorf("fit(%q, %d, head) = %q: %q is not a cluster-aligned suffix of the input", s, w, out, kept)
					}
					continue
				}
				kept := strings.TrimSuffix(strings.TrimRight(out, " "), truncationMarker)
				if !strings.HasPrefix(s, kept) || !bounds[len(kept)] {
					t.Errorf("fit(%q, %d, tail) = %q: %q is not a cluster-aligned prefix of the input", s, w, out, kept)
				}
			}
		}
	}
}

// TestATruncatedVS16ColumnSpansExactlyItsWidth: a Name column squeezed to 3 or
// 5 cells and a head-truncated column, both holding VS16 emoji, still make
// every line of the table exactly RenderedWidth() cells wide by ansi, on every
// row, selected or not, and the cut never leaves a U+FE0F without its base.
func TestATruncatedVS16ColumnSpansExactlyItsWidth(t *testing.T) {
	names := []string{"❤️❤️❤️❤️", "ab✔️cd✔️ef", "x✔️"}
	for _, nameWidth := range []int{3, 5} {
		for _, width := range []int{60, 100} {
			t.Run(fmt.Sprintf("name%d/w%d", nameWidth, width), func(t *testing.T) {
				cfg := Config[row]{
					SortColumn: -1,
					Columns: []Column[row]{
						{Title: "Name", Sizing: SizingFixed, MinWidth: nameWidth, Cell: func(r row) string { return r.Name }},
						{Title: "Path", Sizing: SizingFixed, MinWidth: nameWidth, TruncateHead: true, Cell: func(r row) string { return r.Name }},
						{Title: "State", Sizing: SizingFixed, MinWidth: 8, Cell: func(r row) string { return r.State }},
					},
				}
				m := New(cfg)
				m.Resize(width, 12)
				items := make([]row, len(names))
				for i, n := range names {
					items[i] = row{Name: n, State: "running"}
				}
				m.SetItems(items)

				want := m.RenderedWidth()
				for cursor := range names {
					m.SetCursor(cursor)
					for i, line := range strings.Split(m.View(), "\n") {
						if got := ansi.StringWidth(line); got != want {
							t.Errorf("cursor %d, line %d: %d cells, want %d: %q", cursor, i, got, want, ansi.Strip(line))
						}
						plain := ansi.Strip(line)
						for j := strings.Index(plain, "\uFE0F"); j >= 0; {
							before := plain[:j]
							if !strings.HasSuffix(before, "❤") && !strings.HasSuffix(before, "✔") {
								t.Errorf("cursor %d, line %d: a U+FE0F without its base: %q", cursor, i, plain)
							}
							next := strings.Index(plain[j+len("\uFE0F"):], "\uFE0F")
							if next < 0 {
								break
							}
							j += len("\uFE0F") + next
						}
					}
				}
			})
		}
	}
}

// FuzzFitSpansTheColumn: fit returns exactly the column width by ansi, for any
// text without control characters, at widths 1..40, from either end. Seeds run
// under `mise run test`; explore with
// go test -fuzz=FuzzFitSpansTheColumn -fuzztime=60s ./internal/ui/datatable
func FuzzFitSpansTheColumn(f *testing.F) {
	for _, tc := range agreedWidths {
		f.Add(tc.s)
	}
	for _, tc := range vs16Strings {
		f.Add(tc.s)
	}
	for _, s := range []string{
		"日本語テキスト", "🇫🇷🇫🇷xx", zwjFamily, "1️⃣2️⃣3️⃣", "#️⃣*️⃣", "👍🏽👍🏽ok",
		"\uFE0Fabc", // a stray VS16
		"\u20E3x",   // a stray keycap mark
		"\u0600",    // a Prepend character, which merges with the padding
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip("invalid UTF-8")
		}
		for _, r := range s {
			if unicode.IsControl(r) {
				t.Skip("control characters go through lipgloss and are out of scope")
			}
		}
		for w := 1; w <= 40; w++ {
			for _, head := range []bool{false, true} {
				if got := ansi.StringWidth(fit(s, w, head)); got != w {
					t.Fatalf("fit(%q, %d, head=%t) = %q: %d cells, want %d", s, w, head, fit(s, w, head), got, w)
				}
			}
		}
	})
}

// TestDatatableMeasuresWithOneLibrary: the table cuts and pads with x/ansi, the
// width Lip Gloss and the viewport use. The one go-runewidth call left in the
// package's code is runewidth.FillRight, once, in render.go: the indicator cell,
// which follows East Asian ambiguous width and is a single-rune glyph. Any
// other use brings back the two-measure divergence of DEVDESK-280.
func TestDatatableMeasuresWithOneLibrary(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fillRight := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue // comments may name the library
			}
			for _, ref := range runewidthRef.FindAllString(line, -1) {
				if ref == "runewidth.FillRight" && file == "render.go" {
					fillRight++
					continue
				}
				t.Errorf("%s:%d: %s: the datatable measures with x/ansi; only runewidth.FillRight (indicator cell, render.go) is allowed", file, i+1, ref)
			}
		}
	}
	if fillRight != 1 {
		t.Errorf("runewidth.FillRight appears %d times in render.go, want exactly 1 (the indicator cell)", fillRight)
	}
}

// runewidthRef matches a use of the go-runewidth package in code.
var runewidthRef = regexp.MustCompile(`\brunewidth\.\w+`)

// TestTheTableFillsTheViewportAtThreeWidths: at three widths, with cells that
// carry ambiguous-width icons (Nerd Font, emoji, flag, ZWJ) and VS16 emoji, every
// line, header, selected and unselected rows included, spans exactly
// RenderedWidth() by ansi.StringWidth, which is the viewport's interior
// (Rule 116).
func TestTheTableFillsTheViewportAtThreeWidths(t *testing.T) {
	names := []string{nerdFont + " src", folder + " docs", flagFR + " fr", zwjFamily + " fam", "日本語", "plain", "a❤️x", "✔️ ok"}
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
			}
		}
	}
}
