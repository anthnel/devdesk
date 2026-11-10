package containers

import (
	"testing"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: behind a modal a paste is dropped, never read as keys
// (Rule 111): the pasted `y` does not answer the confirmation.
func TestPasteIsIgnoredBehindAModal(t *testing.T) {
	m := loadedModel(t)
	m.confirmModal = sharedcomponents.NewConfirmModal("Remove", "Remove the container?")
	before := m.Render()

	m, cmd := step(t, m, testutil.Paste("yY\nn"))

	if m.confirmModal == nil {
		t.Error("a paste closed the modal")
	}
	if n := len(testutil.Msgs(cmd)); n != 0 {
		t.Errorf("a paste produced %d message(s), want none", n)
	}
	if after := m.Render(); after != before {
		t.Errorf("a paste changed the view:\n--- before\n%s\n--- after\n%s", before, after)
	}
}
