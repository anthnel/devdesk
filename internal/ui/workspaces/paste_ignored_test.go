package workspaces

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: with no text field holding the keyboard a paste is dropped,
// never read as keys (Rule 111): the view renders exactly as before, so no
// mode, filter or selection changed. The text holds letters that are actions.
func TestPasteIsIgnoredWithoutAFocusedField(t *testing.T) {
	m := loadedModel(t)
	before := m.Render()
	m, cmd := step(t, m, testutil.Paste("DSCNKq/\nx"))
	if after := m.Render(); after != before {
		t.Errorf("a paste changed the view:\n--- before\n%s\n--- after\n%s", before, after)
	}
	if n := len(testutil.Msgs(cmd)); n != 0 {
		t.Errorf("a paste produced %d message(s), want none", n)
	}
}
