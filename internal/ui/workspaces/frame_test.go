package workspaces

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

// DEVDESK-144: a frame moves the blink phase and the footer spinner, never the
// text of a cell. The row says "syncing" with no frame in it.
func TestAFrameDoesNotChangeTheRowCells(t *testing.T) {
	m := running(t, loadedModel(t), jobs.KindSync, devdeskPath)
	nRuns := len(m.jobs)

	for _, frame := range []string{"X", "Y"} {
		m = feed(t, m, jobs.FrameMsg{Frame: frame, RenderedFrame: frame})
		if got := m.table.Items()[0].GitStatus; got != "syncing" {
			t.Errorf("Git Status = %q after frame %q, want %q unchanged", got, frame, "syncing")
		}
	}
	if len(m.jobs) != nRuns {
		t.Errorf("the held runs went from %d to %d on a frame", nRuns, len(m.jobs))
	}
}

func TestAFrameMovesTheFooterSpinner(t *testing.T) {
	m := running(t, loadedModel(t), jobs.KindSync, devdeskPath)

	// An idle row keeps the spinner; the running row's own dot moves instead.
	m = selectRow(t, m, "clean-repo")
	m = feed(t, m, jobs.FrameMsg{RenderedFrame: "<R1>"})

	if footer := m.RenderFooter(160); !strings.Contains(footer, "<R1>") {
		t.Errorf("the footer does not carry the new frame:\n%s", footer)
	}
}

// A frame is not a snapshot: it must not reset what the last ChangedMsg left.
func TestAFrameKeepsTheSnapshot(t *testing.T) {
	m := running(t, loadedModel(t), jobs.KindSync, devdeskPath)

	m = feed(t, m, jobs.FrameMsg{Frame: "X", RenderedFrame: "X"})

	if !m.syncing(devdeskPath) {
		t.Error("a frame made the running sync disappear from the view")
	}
	if got := len(jobs.Unfinished(m.jobs)); got != 1 {
		t.Errorf("%d unfinished runs held after a frame, want the 1 from the snapshot", got)
	}
}
