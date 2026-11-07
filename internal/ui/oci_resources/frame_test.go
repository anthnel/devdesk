package ociresources

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

func TestAFrameMovesTheRowSpinner(t *testing.T) {
	m := loadedModel(t)
	m = scanning(t, m, m.images[0].Name())
	nRuns := len(m.jobs)

	for _, frame := range []string{"X", "Y"} {
		next, _ := m.Update(jobs.FrameMsg{Frame: frame})
		m = next.(Model)
		if cell := scanningImageCell(t, m); !strings.Contains(cell, frame) {
			t.Errorf("the scanning cell = %q after frame %q, want the new frame", cell, frame)
		}
	}
	if len(m.jobs) != nRuns {
		t.Errorf("the held runs went from %d to %d on a frame", nRuns, len(m.jobs))
	}
}

// scanningImageCell returns the cell of the image being scanned that reads as such.
func scanningImageCell(t *testing.T, m Model) string {
	t.Helper()
	for _, row := range m.imageTable.Table().Rows() {
		for _, cell := range row {
			if strings.Contains(cell, "scanning") {
				return cell
			}
		}
	}
	t.Fatal("no row reads as being scanned")
	return ""
}
