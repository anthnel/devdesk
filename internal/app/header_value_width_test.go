package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// DEVDESK-305 (IS-13): shortcut.HeaderValueWidth is the room a value really has
// in the column the router draws, not a second formula that can drift.
func TestAValueOfHeaderValueWidthIsNeverClipped(t *testing.T) {
	keys := []string{"Images", "Refused"}
	for _, w := range []int{60, 80, 120, 200} {
		n := shortcut.HeaderValueWidth(w, keys)
		if n < 2 {
			t.Fatalf("width %d: HeaderValueWidth = %d, the test needs a usable width", w, n)
		}
		render := func(cells int) string {
			info := []shortcut.HeaderInfo{
				{Key: "Images", Value: "3"},
				{Key: "Refused", Value: strings.Repeat("Q", cells)},
			}
			return ansi.Strip(renderHeaderContent(info, nil, w))
		}
		if got := render(n); !strings.Contains(got, strings.Repeat("Q", n)) {
			t.Errorf("width %d: a value of %d cells (HeaderValueWidth) was clipped:\n%s", w, n, got)
		}
		if got := render(n + 1); strings.Contains(got, strings.Repeat("Q", n+1)) {
			t.Errorf("width %d: a value of %d cells was not clipped, so HeaderValueWidth is too small", w, n+1)
		}
	}
}
