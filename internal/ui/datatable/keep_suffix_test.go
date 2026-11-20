package datatable

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Column.KeepSuffix (DEVDESK-579, NX-12): a cell that ends with a fixed suffix
// ("nexus-production (group)") gives its variable part up when the column is too
// narrow, and the suffix stays whole. Cutting the end of such a cell would read as
// another value ("nexus-production…"), which is the defect this option exists for.

const testSuffix = " (group)" // 8 cells

func TestFitKeepingSuffix(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		head  bool
		want  string
	}{
		{"room", "nexus (group)", 20, false, "nexus (group)       "},
		{"exact", "nexus (group)", 13, false, "nexus (group)"},
		{"tail cut keeps the suffix", "nexus-production (group)", 16, false, "nexus-p…" + testSuffix},
		{"head cut keeps the suffix", "nexus-production (group)", 16, true, "…duction" + testSuffix},
		{"one cell for the alias", "nexus-production (group)", 9, false, "…" + testSuffix},
		{"width equal to the suffix width falls back to a plain cut", "nexus-production (group)", 8, false, "nexus-p…"},
		{"text without the suffix is cut as usual", "nexus-production-eu", 16, false, "nexus-productio…"},
		{"text that is only the suffix", testSuffix, 6, false, " (gro…"},
		{"wide alias", "测试测试测试测试 (group)", 14, false, "测试…" + testSuffix + " "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fitKeeping(tc.text, testSuffix, tc.width, tc.head)
			if w := ansi.StringWidth(got); w != tc.width {
				t.Errorf("fitKeeping(%q, %d) is %d cells wide, want %d: %q", tc.text, tc.width, w, tc.width, got)
			}
			if got != tc.want {
				t.Errorf("fitKeeping(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

// With an empty suffix fitKeeping is fit, byte for byte.
func TestFitKeepingAnEmptySuffixIsFit(t *testing.T) {
	for _, text := range []string{"", "abc", "nexus-production (group)", "a\tb\nc"} {
		for w := 1; w <= 30; w++ {
			for _, head := range []bool{false, true} {
				if got, want := fitKeeping(text, "", w, head), fit(text, w, head); got != want {
					t.Errorf("fitKeeping(%q, %q, %d, %v) = %q, fit gives %q", text, "", w, head, got, want)
				}
			}
		}
	}
}

// Whatever the width, a cell that has the suffix and a column wider than the
// suffix keeps the suffix whole, never cut, and spans the column exactly.
func TestFitKeepingTheSuffixAtEveryWidth(t *testing.T) {
	text := "nexus-production" + testSuffix
	for w := len(testSuffix) + 1; w <= 40; w++ {
		for _, head := range []bool{false, true} {
			got := fitKeeping(text, testSuffix, w, head)
			if !strings.HasSuffix(strings.TrimRight(got, " "), testSuffix) {
				t.Errorf("width %d head=%v: %q lost the suffix", w, head, got)
			}
			if sw := ansi.StringWidth(got); sw != w {
				t.Errorf("width %d head=%v: %q is %d cells wide", w, head, got, sw)
			}
		}
	}
}

// A column of zero, one or negative width cannot hold the suffix: nothing panics,
// zero and negative widths give an empty string, and one cell gives the marker.
func TestFitKeepingDegenerateWidths(t *testing.T) {
	text := "nexus-production" + testSuffix
	tests := []struct {
		width int
		want  string
	}{
		{0, ""},
		{-1, ""},
		{-100, ""},
		{1, "…"},
	}
	for _, tc := range tests {
		for _, head := range []bool{false, true} {
			if got := fitKeeping(text, testSuffix, tc.width, head); got != tc.want {
				t.Errorf("fitKeeping(width %d, head=%v) = %q, want %q", tc.width, head, got, tc.want)
			}
		}
	}
}

// Fitting an already fitted cell to the same width changes nothing, tail and head.
func TestFitKeepingIsIdempotent(t *testing.T) {
	for _, text := range []string{"nexus-production" + testSuffix, "nexus" + testSuffix, "hub", testSuffix, "测试测试测试测试" + testSuffix} {
		for w := 1; w <= 40; w++ {
			for _, head := range []bool{false, true} {
				once := fitKeeping(text, testSuffix, w, head)
				if twice := fitKeeping(once, testSuffix, w, head); twice != once {
					t.Errorf("fitKeeping(%q, %d, head=%v) is not idempotent: %q then %q", text, w, head, once, twice)
				}
			}
		}
	}
}

// Through the table: the cell of a squeezed column renders with its suffix whole on
// every row, selected or not, and every line spans the rendered width (Rule 116).
func TestAKeepSuffixColumnNeverCutsItsSuffix(t *testing.T) {
	names := []string{"nexus-production" + testSuffix, "hub", "nexus-staging-eu-west" + testSuffix}
	for _, head := range []bool{false, true} {
		for _, regWidth := range []int{12, 16, 20} {
			t.Run(fmt.Sprintf("head=%v/min%d", head, regWidth), func(t *testing.T) {
				cfg := Config[row]{
					SortColumn: -1,
					Columns: []Column[row]{
						{Title: "Registry", Sizing: SizingFixed, MinWidth: regWidth, TruncateHead: head,
							KeepSuffix: func(r row) string {
								if strings.HasSuffix(r.Name, testSuffix) {
									return testSuffix
								}
								return ""
							},
							Cell: func(r row) string { return r.Name }},
						{Title: "State", Sizing: SizingFixed, MinWidth: 8, Cell: func(r row) string { return r.State }},
					},
				}
				m := New(cfg)
				m.Resize(60, 12)
				items := make([]row, len(names))
				for i, n := range names {
					items[i] = row{Name: n, State: "running"}
				}
				m.SetItems(items)

				want := m.RenderedWidth()
				for cursor := range names {
					m.SetCursor(cursor)
					plain := ansi.Strip(m.View())
					for _, line := range strings.Split(m.View(), "\n") {
						if got := ansi.StringWidth(line); got != want {
							t.Errorf("cursor %d: a line is %d cells, want %d: %q", cursor, got, want, ansi.Strip(line))
						}
					}
					if got := strings.Count(plain, testSuffix); got != 2 {
						t.Errorf("cursor %d: %d rows keep the suffix, want 2:\n%s", cursor, got, plain)
					}
					if strings.Contains(plain, "(grou…") || strings.Contains(plain, "(gro…") {
						t.Errorf("cursor %d: the suffix was cut:\n%s", cursor, plain)
					}
				}
			})
		}
	}
}
