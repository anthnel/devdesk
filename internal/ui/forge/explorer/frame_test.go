package explorer

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

// The tree: a working row blinks with FrameMsg.Blink, read at render. The
// glyph moves, the rows are not rebuilt (DEVDESK-147, replaces the spinner
// frame test).
func TestAFrameMovesTheRowDot(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})
	m = feed(t, m, jobs.ChangedMsg{Runs: []jobs.Run{createRun("new", "new")}})

	for _, blink := range []bool{true, false} {
		m = feed(t, m, jobs.FrameMsg{Frame: "X", Blink: blink})
		row, ok := rowFor(m, "new")
		if !ok {
			t.Fatal("the placeholder row is gone")
		}
		if row.Activity != indicator.Running {
			t.Errorf("Activity = %v, want Running", row.Activity)
		}
		if line := treeLine(t, m, "new"); !strings.Contains(line, indicator.Glyph(indicator.Running, blink)) {
			t.Errorf("blink=%v: row lacks the dot:\n%s", blink, line)
		}
		if got := restIcon(row); got == "X" {
			t.Errorf("the spinner frame leaked into the resting icon: %q", got)
		}
	}
	if len(m.jobs) != 1 {
		t.Errorf("the held runs = %d after a frame, want the 1 from the snapshot", len(m.jobs))
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
