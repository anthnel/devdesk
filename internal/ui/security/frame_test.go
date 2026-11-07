package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

func TestAFrameMovesTheRowSpinner(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = withJobs(t, m, scanningRun("nexus/api:1.4"))
	nRuns := len(m.jobs)

	for _, frame := range []string{"X", "Y"} {
		m = feed(t, m, jobs.FrameMsg{Frame: frame})
		if cell := scanningCell(t, m); !strings.Contains(cell, frame) {
			t.Errorf("the scanning cell = %q after frame %q, want the new frame", cell, frame)
		}
	}
	if len(m.jobs) != nRuns {
		t.Errorf("the held runs went from %d to %d on a frame", nRuns, len(m.jobs))
	}
}
