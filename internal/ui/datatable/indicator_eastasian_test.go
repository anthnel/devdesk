package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// TestRule116HoldsUnderEastAsianWidth pins Rule 116 when the terminal counts
// the ambiguous-width glyphs (● ○ ◐) as 2 cells. lipgloss always counts them as
// 1, so padding that trusts lipgloss overflows the row by one cell. It mutates
// the global runewidth condition, so it must not run in parallel.
func TestRule116HoldsUnderEastAsianWidth(t *testing.T) {
	prev := runewidth.DefaultCondition.EastAsianWidth
	runewidth.DefaultCondition.EastAsianWidth = true
	t.Cleanup(func() { runewidth.DefaultCondition.EastAsianWidth = prev })

	states := append([]indicator.State{indicator.Idle}, indicator.States()...)
	for _, width := range []int{60, 120} {
		for _, st := range states {
			for _, selected := range []bool{false, true} {
				name := st.String()
				m := New(indicatorConfig(nil))
				m.Resize(width, 10)
				m.SetItems([]row{{Name: "a", State: name}, {Name: "b", State: name}})
				if selected {
					cursorOn(t, &m, "a")
				} else {
					cursorOn(t, &m, "b")
				}
				want := m.RenderedWidth()
				// Line 0 is the header: its sort arrow (▲) is ambiguous-width
				// too, which is a separate, pre-existing matter out of scope here.
				for i, l := range strings.Split(m.View(), "\n")[1:] {
					plain := ansi.Strip(l)
					if plain == "" {
						continue
					}
					if got := runewidth.StringWidth(plain); got != want {
						t.Errorf("state=%s width=%d selected=%v line %d: East Asian width %d, want %d: %q",
							name, width, selected, i, got, want, plain)
					}
				}
			}
		}
	}
}
