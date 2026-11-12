package dashboard

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

func TestAFrameMovesTheFooterDot(t *testing.T) {
	m := feed(t, New(testConfig(), &shared.State{}), jobs.ChangedMsg{
		Runs: []jobs.Run{running(command.ViewWorkspaces, jobs.KindScan, "~/work")},
	})

	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if footer := m.RenderFooter(180); !strings.Contains(footer, on) || strings.Contains(footer, off) {
		t.Errorf("the footer does not carry the lit dot:\n%s", footer)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if footer := m.RenderFooter(180); !strings.Contains(footer, off) || strings.Contains(footer, on) {
		t.Errorf("the footer does not carry the dimmed dot:\n%s", footer)
	}
	if len(m.jobs) != 1 {
		t.Errorf("the held runs = %d after a frame, want the snapshot kept", len(m.jobs))
	}
}
