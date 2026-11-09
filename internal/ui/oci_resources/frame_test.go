package ociresources

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/anthnel/devdesk/internal/jobs"
)

// ── DEVDESK-146: a frame sets the blink phase, nothing else ──────────────────

// hasSpinnerFrame reports whether s carries one of the Dot spinner's frames.
func hasSpinnerFrame(s string) bool {
	for _, f := range spinner.Dot.Frames {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

func TestAFrameSetsThePhaseWithoutRebuildingRows(t *testing.T) {
	m := loadedModel(t)
	m = scanning(t, m, m.images[0].Name())
	before := reflect.ValueOf(m.imageTable.Items()).Pointer()
	nRuns := len(m.jobs)

	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if m.imageTable.BlinkPhase() {
		t.Error("Blink=false left the phase on")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if !m.imageTable.BlinkPhase() {
		t.Error("Blink=true did not turn the phase back on")
	}
	if after := reflect.ValueOf(m.imageTable.Items()).Pointer(); after != before {
		t.Error("a frame rebuilt the rows; the phase is read at render")
	}
	if len(m.jobs) != nRuns {
		t.Errorf("the held runs went from %d to %d on a frame", nRuns, len(m.jobs))
	}
}

func TestAFrameNeverReachesTheTableCells(t *testing.T) {
	m := loadedModel(t)
	m = scanning(t, m, m.images[0].Name())
	m = feed(t, m, jobs.FrameMsg{Frame: "ZZ", RenderedFrame: "FOOTERFRAME", Blink: true})
	if strings.Contains(m.View(), "ZZ") {
		t.Error("the bare frame still reaches the table")
	}
}

func TestTheModelHoldsNoRowSpinnerState(t *testing.T) {
	if _, ok := reflect.TypeOf(Model{}).FieldByName("jobFrame"); ok {
		t.Error("Model still carries jobFrame")
	}
	row := reflect.TypeOf(imageRow{})
	for _, name := range []string{"Scanning", "Pulling", "SpinnerFrame"} {
		if _, ok := row.FieldByName(name); ok {
			t.Errorf("imageRow still carries %s", name)
		}
	}
	for _, name := range []string{"Activity", "ScanRunning", "PullRunning"} {
		if _, ok := row.FieldByName(name); !ok {
			t.Errorf("imageRow lacks %s", name)
		}
	}
}
