package security

import (
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/charmbracelet/x/ansi"
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
	m = feed(t, m, jobs.FrameMsg{Blink: true})

	if hasBraille(m.Render()) {
		t.Error("a spinner frame reaches the table")
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

// The router's blink phase feeds the footer dot when the view has no chain of
// its own running; the rendered frame no longer reaches the footer.
func TestAFrameStillFeedsTheFooterDotWhenTheViewHasNoChain(t *testing.T) {
	// The cursor sits on an idle row (see idleRowDuringBatch): no detail glyph
	// takes the footer dot's place, so the hollow dot must be there.
	m := idleRowDuringBatch(t)
	m = feed(t, m, jobs.FrameMsg{Blink: false})

	footer := m.RenderFooter(200)
	if !strings.Contains(footer, indicator.Glyph(indicator.Running, false)) {
		t.Errorf("the footer shows no hollow dot on an idle row during a batch: %q", ansi.Strip(footer))
	}
}
