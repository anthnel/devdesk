package setup

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: behind the overwrite confirmation no text field holds the
// keyboard, so a paste is dropped, never read as keys (Rule 111): the stage,
// the step and every field stay as they were.
func TestPasteIsIgnoredBehindTheConfirmation(t *testing.T) {
	isolateHome(t)
	m := past(t, New(), "staging")
	m = feed(t, m, ContextExistsMsg{Exists: true})
	if m.stage != stageConfirmOverwrite {
		t.Fatalf("stage = %v, want the overwrite confirmation", m.stage)
	}
	before := m.Render()
	stage, step := m.stage, m.step

	m = feed(t, m, testutil.Paste("yNn\nstaging2"))

	if m.stage != stage || m.step != step {
		t.Errorf("stage/step = %v/%d after a paste, want %v/%d", m.stage, m.step, stage, step)
	}
	if got := m.contextNameInput.Value(); got != "staging" {
		t.Errorf("context name = %q after a paste, want it untouched", got)
	}
	if after := m.Render(); after != before {
		t.Errorf("a paste changed the view:\n--- before\n%s\n--- after\n%s", before, after)
	}
}
