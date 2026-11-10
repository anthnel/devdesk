package agentusageview

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: with the filter bar closed a paste is dropped, never read as
// keys (Rule 111): the view renders exactly as before.
func TestPasteIsIgnoredWithoutAFocusedField(t *testing.T) {
	m := loaded(t, newModel(t), session("11111111-aaaa", "/srv/alpha", time.Hour))
	before := m.Render()

	m = feed(t, m, testutil.Paste("DSCNKq/\nx"))

	if q := m.table.FilterBar().SearchQuery(); q != "" {
		t.Errorf("search query = %q, want empty", q)
	}
	if after := m.Render(); after != before {
		t.Errorf("a paste changed the view:\n--- before\n%s\n--- after\n%s", before, after)
	}
}
