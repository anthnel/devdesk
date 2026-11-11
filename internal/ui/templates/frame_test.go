package templates

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// ── DEVDESK-139: the frame arrives in its own message ────────────────────────

func TestAFrameMovesTheFooterDot(t *testing.T) {
	m := opened(t, entry("api", "API"))
	run := jobs.NewRun(jobs.KindScan, command.ViewTemplates, "", "API", "/dir")
	run.ID = 1
	m = send(t, m, jobs.ChangedMsg{Runs: []jobs.Run{run}})

	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	m = send(t, m, jobs.FrameMsg{Blink: true})
	if footer := m.RenderFooter(120); !strings.Contains(footer, on) || strings.Contains(footer, off) {
		t.Errorf("the footer does not carry the lit dot:\n%s", footer)
	}
	m = send(t, m, jobs.FrameMsg{Blink: false})
	if footer := m.RenderFooter(120); !strings.Contains(footer, off) || strings.Contains(footer, on) {
		t.Errorf("the footer does not carry the dimmed dot:\n%s", footer)
	}
}
