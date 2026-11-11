package explorer

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// DEVDESK-341 step 2h, then step 7: the explorer's load lines use the shared
// activity dot. The router frame's text never replaces anything in the footer;
// only FrameMsg.Blink reaches it.

func TestARouterFrameDoesNotReplaceTheExplorersLoadLine(t *testing.T) {
	m := loadedModel(t)
	m.refreshing = 1 // "Updating from ..." is on screen

	m = feed(t, m, jobs.FrameMsg{Frame: "<R>", RenderedFrame: "<R>", Blink: true})

	footer := m.RenderFooter(160)
	if strings.Contains(footer, "<R>") {
		t.Errorf("the router frame text reached the footer:\n%s", footer)
	}
	if !strings.Contains(footer, "●") || !strings.Contains(footer, "Updating from") {
		t.Errorf("the load line lost its dot or text:\n%s", footer)
	}
}

func TestAFrameStillSetsTheBlinkPhaseAndStartsNoTimer(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})
	m = feed(t, m, jobs.ChangedMsg{Runs: []jobs.Run{createRun("new", "new")}})

	m2, cmd := step(t, m, jobs.FrameMsg{Blink: false})
	if cmd != nil {
		t.Error("a FrameMsg returned a Cmd; the router owns the chain")
	}
	if m2.table.BlinkPhase() {
		t.Error("Blink=false left the tree's phase on")
	}
}
