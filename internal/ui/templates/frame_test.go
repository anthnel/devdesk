package templates

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

func TestAFrameMovesTheFooterSpinner(t *testing.T) {
	m := opened(t, entry("api", "API"))
	run := jobs.NewRun(jobs.KindScan, command.ViewTemplates, "", "API", "/dir")
	run.ID = 1
	m = send(t, m, jobs.ChangedMsg{Runs: []jobs.Run{run}})

	m = send(t, m, jobs.FrameMsg{RenderedFrame: "<R1>"})

	if footer := m.RenderFooter(120); !strings.Contains(footer, "<R1>") {
		t.Errorf("the footer does not carry the new frame:\n%s", footer)
	}
}
