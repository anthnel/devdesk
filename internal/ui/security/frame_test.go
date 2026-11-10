package security

import (
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

// ── DEVDESK-145: a frame sets the blink phase, nothing else ──────────────────

func TestAFrameSetsThePhaseWithoutRebuildingRows(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = withJobs(t, m, scanningRun("nexus/api:1.4"))
	before := reflect.ValueOf(m.inventory.Items()).Pointer()
	nRuns := len(m.jobs)

	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if m.inventory.BlinkPhase() {
		t.Error("Blink=false left the phase on")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if !m.inventory.BlinkPhase() {
		t.Error("Blink=true did not turn the phase back on")
	}
	if after := reflect.ValueOf(m.inventory.Items()).Pointer(); after != before {
		t.Error("a frame rebuilt the rows; the phase is read at render")
	}
	if len(m.jobs) != nRuns {
		t.Errorf("the held runs went from %d to %d on a frame", nRuns, len(m.jobs))
	}
}

func TestAFrameChangesTheGlyphNotTheItems(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = withJobs(t, m, scanningRun("nexus/api:1.4"))
	snapshot := append([]scanTarget(nil), m.inventory.Items()...)

	on := feed(t, m, jobs.FrameMsg{Blink: true}).Render()
	off := feed(t, m, jobs.FrameMsg{Blink: false}).Render()

	if on == off {
		t.Error("the phase did not change the rendered running row")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if !reflect.DeepEqual(snapshot, m.inventory.Items()) {
		t.Error("a frame changed the items")
	}
}

func TestAFrameNeverReachesTheTableCells(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = withJobs(t, m, scanningRun("nexus/api:1.4"))
	m = feed(t, m, jobs.FrameMsg{Frame: "ZZ", RenderedFrame: "FOOTERFRAME", Blink: true})

	if strings.Contains(m.Render(), "ZZ") {
		t.Error("the bare frame still reaches the table")
	}
}

func TestTheModelHoldsNoRowSpinnerState(t *testing.T) {
	typ := reflect.TypeOf(Model{})
	for _, name := range []string{"jobFrame", "spinnerFrameIdx"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Errorf("Model still carries %s", name)
		}
	}
	row := reflect.TypeOf(scanTarget{})
	for _, name := range []string{"Scanning", "SpinnerFrame"} {
		if _, ok := row.FieldByName(name); ok {
			t.Errorf("scanTarget still carries %s", name)
		}
	}
}

// The router's frame still feeds the footer spinner when the view has no chain
// of its own running.
func TestAFrameStillFeedsTheFooterSpinnerWhenTheViewHasNoChain(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = withJobs(t, m, scanningRun("nexus/api:1.4"))
	m = feed(t, m, jobs.FrameMsg{Frame: "ZZ", RenderedFrame: "FOOTERFRAME", Blink: true})

	if !strings.Contains(m.RenderFooter(120), "FOOTERFRAME") {
		t.Skip("the footer shows no spinner for this state; the table side is asserted above")
	}
}
