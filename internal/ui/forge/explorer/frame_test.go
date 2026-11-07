package explorer

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

// The tree: a working row takes its frame from FrameMsg.
func TestAFrameMovesTheRowSpinner(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})
	m = feed(t, m, jobs.ChangedMsg{Runs: []jobs.Run{createRun("new", "new")}})

	for _, frame := range []string{"X", "Y"} {
		m = feed(t, m, jobs.FrameMsg{Frame: frame})
		row, ok := rowFor(m, "new")
		if !ok {
			t.Fatal("the placeholder row is gone")
		}
		if got := iconCell(row); got != frame {
			t.Errorf("icon cell = %q after frame %q, want the spinner", got, frame)
		}
	}
	if len(m.jobs) != 1 {
		t.Errorf("the held runs = %d after a frame, want the 1 from the snapshot", len(m.jobs))
	}
}

// The clone list: a running row takes its frame from FrameMsg.
func TestAFrameMovesTheCloneListSpinner(t *testing.T) {
	h := cloning(t)
	h.emit(found("alpha/api"), began("alpha/api"))

	for _, frame := range []string{"X", "Y"} {
		h.m = feed(t, h.m, jobs.FrameMsg{Frame: frame})
		rows := h.m.clone.table.Table().Rows()
		if len(rows) != 1 {
			t.Fatalf("rows = %v, want the one repository", rows)
		}
		if got := rows[0][colCloneStatus]; !strings.HasPrefix(got, frame+" cloning") {
			t.Errorf("status cell = %q after frame %q, want the new frame", got, frame)
		}
	}
	if got := h.item(0).State; got != jobs.ItemRunning {
		t.Errorf("state = %v, a frame must not touch the run", got)
	}
}

func TestAFrameMovesTheFooterSpinner(t *testing.T) {
	m := loadedModel(t)
	m.loading = true // the tree is reloading: the footer shows a spinner line

	m = feed(t, m, jobs.FrameMsg{RenderedFrame: "<R1>"})

	if footer := m.RenderFooter(160); !strings.Contains(footer, "<R1>") {
		t.Errorf("the footer does not carry the new frame:\n%s", footer)
	}
}
