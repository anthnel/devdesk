package filebrowser

import (
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

// This view shows no jobs line of its own, so the footer component is read
// directly: a frame carries the blink phase only and leaves the footer's
// spinner line exactly as it was.
func TestAFrameLeavesTheFooterSpinnerAlone(t *testing.T) {
	m := openAt(t, tree(t))
	status := sharedcomponents.Status{Text: "Deleting"}
	before := m.footer.View(80, status)

	m, _ = step(t, m, jobs.FrameMsg{Blink: false})

	if got := m.footer.View(80, status); got != before {
		t.Errorf("a frame changed the footer line:\nbefore %q\nafter  %q", before, got)
	}
}
