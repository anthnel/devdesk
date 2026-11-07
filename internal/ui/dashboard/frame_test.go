package dashboard

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

func TestAFrameMovesTheFooterSpinner(t *testing.T) {
	m := feed(t, New(testConfig(), &shared.State{}), jobs.ChangedMsg{
		Runs: []jobs.Run{running(command.ViewWorkspaces, jobs.KindScan, "~/work")},
	})

	m = feed(t, m, jobs.FrameMsg{RenderedFrame: "<R1>"})

	if footer := m.RenderFooter(180); !strings.Contains(footer, "<R1>") {
		t.Errorf("the footer does not carry the new frame:\n%s", footer)
	}
	if len(m.jobs) != 1 {
		t.Errorf("the held runs = %d after a frame, want the snapshot kept", len(m.jobs))
	}
}
