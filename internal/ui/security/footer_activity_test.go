package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-341 step 2e: the footer separates two things the router frame used to
// share. Both the inventory LOAD and jobs work get the shared activity dot
// (DEVDESK-372); loading_dot_test.go covers the load.

func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

func plainInfoLine(m Model, width int) string { return ansi.Strip(infoLine(m, width)) }

// idleRowDuringBatch has a batch running with the cursor on the idle row, the
// inventory loaded.
func idleRowDuringBatch(t *testing.T) Model {
	t.Helper()
	m := footerModel(t, []jobs.Run{batch()}, nil)
	m.inventoryLoading = false
	return selectTarget(t, m, ftOther)
}

func TestJobsWorkShowsTheActivityDotNotABrailleFrame(t *testing.T) {
	m := idleRowDuringBatch(t)
	if st := m.inventoryStatus(); !st.Activity || st.Spinner {
		t.Fatalf("status Activity=%v Spinner=%v, want Activity only", st.Activity, st.Spinner)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	got := infoLine(m, 200)
	if !strings.Contains(ansi.Strip(got), "● ") || !strings.Contains(ansi.Strip(got), "1/3") {
		t.Errorf("want the dot and the progress, got %q", ansi.Strip(got))
	}
	if hasBraille(got) {
		t.Errorf("a braille frame reached the footer: %q", ansi.Strip(got))
	}
}

func TestJobsDotFollowsTheBlinkPhase(t *testing.T) {
	m := idleRowDuringBatch(t)
	on := plainInfoLine(feed(t, m, jobs.FrameMsg{Blink: true}), 200)
	off := plainInfoLine(feed(t, m, jobs.FrameMsg{Blink: false}), 200)
	if !strings.Contains(off, "○") || strings.Contains(off, "●") {
		t.Errorf("Blink=false must give the hollow dot only, got %q", off)
	}
	if strings.Index(on, "Scanning") != strings.Index(off, "Scanning") {
		t.Errorf("the text moved between phases: %q vs %q", on, off)
	}
}

func TestNoUnfinishedRunMeansNoFooterDot(t *testing.T) {
	m := footerModel(t, nil, nil)
	m.inventoryLoading = false
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if got := plainInfoLine(m, 200); strings.ContainsAny(got, "●○") {
		t.Errorf("a dot without a running job: %q", got)
	}
}

func TestAFooterErrorHidesTheDotWhileAScanRuns(t *testing.T) {
	m := feed(t, idleRowDuringBatch(t), jobs.FrameMsg{Blink: true})
	m.footer.Error("it broke")
	got := plainInfoLine(m, 200)
	if !strings.Contains(got, "it broke") || strings.ContainsAny(got, "●○") {
		t.Errorf("want the error and no dot, got %q", got)
	}
}

// T5: the cursor on the running row: its glyph detail is the only dot.
func TestARunningRowDetailLeavesExactlyOneDot(t *testing.T) {
	m := footerModel(t, []jobs.Run{batch()}, nil)
	m.inventoryLoading = false
	m = feed(t, selectTarget(t, m, ftRepo), jobs.FrameMsg{Blink: true})
	if got := plainInfoLine(m, 200); strings.Count(got, "●") != 1 {
		t.Errorf("want exactly one ●, got %q", got)
	}
}

func TestAFrameStartsNoTimerOfItsOwn(t *testing.T) {
	_, cmd := idleRowDuringBatch(t).Update(jobs.FrameMsg{Blink: true})
	if cmd != nil {
		t.Error("a FrameMsg returned a Cmd; the router owns the chain")
	}
}
