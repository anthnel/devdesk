package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
)

// DEVDESK-150 step 3: what only the router can show about the Ports activity
// column — the router's frame reaches the netdiag view and moves the blink.

func portsRow(t *testing.T, a *App) string {
	t.Helper()
	view := ansi.Strip(a.views[command.ViewNetdiag].Render())
	return rowOf(t, view, "ghost")
}

func TestNetdiagReceivesTheRouterFrameAndBlinksTheKillDot(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedNetdiagFooter(t, a)
	held, ok := a.views[command.ViewNetdiag].(*netdiag.Model)
	if !ok {
		t.Fatalf("the router holds a %T", a.views[command.ViewNetdiag])
	}
	if !held.WantsBlink() {
		t.Fatal("a kill is running but the view does not ask for the blink")
	}

	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	var m tea.Model = a.views[command.ViewNetdiag]
	m, _ = m.Update(jobs.FrameMsg{Blink: true})
	a.setView(command.ViewNetdiag, m)
	if row := portsRow(t, a); !strings.Contains(row, on) {
		t.Errorf("blink on: the row has no %q: %q", on, row)
	}
	m, _ = m.Update(jobs.FrameMsg{Blink: false})
	a.setView(command.ViewNetdiag, m)
	if row := portsRow(t, a); strings.Contains(row, on) || (off != "" && !strings.Contains(row, off)) {
		t.Errorf("blink off: expected the off phase %q only: %q", off, row)
	}
}
