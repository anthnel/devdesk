package viewer

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: with neither the search bar nor the go-to-line prompt open,
// a paste is dropped, never read as keys (Rule 111): f, c, w, n, g, s, V in it
// open nothing and the pane does not move.
func TestPasteIsIgnoredWithoutAFocusedField(t *testing.T) {
	m := longLog(t)
	before := m.Render()

	m = feed(t, m, testutil.Paste("fcwngsV/\n120"))

	if m.gotoActive || m.bar.InEditMode() {
		t.Errorf("gotoActive = %v, searching = %v after a paste, want neither", m.gotoActive, m.bar.InEditMode())
	}
	if q := m.bar.SearchQuery(); q != "" {
		t.Errorf("search query = %q, want empty", q)
	}
	if after := m.Render(); after != before {
		t.Errorf("a paste changed the view:\n--- before\n%s\n--- after\n%s", before, after)
	}
}
