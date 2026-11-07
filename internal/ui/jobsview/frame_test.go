package jobsview

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

func TestAFrameMovesTheRowSpinner(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))

	for _, frame := range []string{"X", "Y"} {
		m = feed(t, m, jobs.FrameMsg{Frame: frame})
		row, ok := m.runTable.Selected()
		if !ok {
			t.Fatal("no row is selected")
		}
		if got := runStateGlyph(row); got != frame {
			t.Errorf("the state cell = %q after frame %q", got, frame)
		}
	}
	if len(m.runs) != 1 {
		t.Errorf("the snapshot holds %d runs after a frame, want it untouched", len(m.runs))
	}
}

func TestAFrameMovesTheFooterSpinner(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))

	m = feed(t, m, jobs.FrameMsg{RenderedFrame: "<R1>"})

	if footer := m.RenderFooter(120); !strings.Contains(footer, "<R1>") {
		t.Errorf("the footer does not carry the new frame:\n%s", footer)
	}
}
