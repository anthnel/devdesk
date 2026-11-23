package app

import (
	"fmt"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// TestTheDiagnosticsTableFillsTheViewport measures, in the real router, the
// Diagnostics table against the viewport that holds it: the table must be as
// tall as the viewport's inside, or its last rows hide below a blank half
// (DEVDESK-744). It also covers a resize made while another tab is on screen,
// then the way back to Diagnostics.
func TestTheDiagnosticsTableFillsTheViewport(t *testing.T) {
	sizes := [][2]int{{80, 24}, {100, 30}, {120, 30}, {175, 38}, {200, 60}}
	for _, size := range sizes {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			isolateHome(t)
			a := newWithSize(testConfig(), w, h)
			a.Update(netdiag.OpenRequestMsg{Target: netcheck.Target{Host: "example.com", Port: 443}, AutoRun: true})
			if a.currentView != command.ViewNetdiag {
				t.Fatalf("currentView = %q, want netdiag", a.currentView)
			}
			// What the requestResize Cmd the open request returned leads to.
			a.resize(w, h)
			requireFilled(t, a, "after the run started")

			// Resize while the Ports tab is on screen, then come back.
			feedKey(t, a, testutil.Key("tab"))
			a.resize(w, h+6)
			feedKey(t, a, testutil.Key("shift+tab"))
			requireFilled(t, a, "after a resize on another tab")
		})
	}
}

func requireFilled(t *testing.T, a *App, when string) {
	t.Helper()
	inner := a.viewport.Height() - 1 // the bottom border
	got := lipgloss.Height(a.views[command.ViewNetdiag].(*netdiag.Model).Render())
	if got != inner {
		t.Errorf("%s: the Diagnostics table is %d lines tall in a viewport %d lines inside", when, got, inner)
	}
}
