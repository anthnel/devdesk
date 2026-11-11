package templates

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-341 step 2d: the batch progress carries the shared activity dot (phase
// from jobs.FrameMsg.Blink), never the router's braille frame.

func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

// otherRowSyncing keeps the cursor on API while Web syncs: no detail, so the
// footer carries its own indicator.
func otherRowSyncing(t *testing.T) Model {
	t.Helper()
	m := indFixture(t)
	return snapshot(t, m, []jobs.Run{named(jobs.KindSync, jobs.ItemRunning, "web", "Web")}, nil)
}

func TestTheProgressShowsTheActivityDotNotABrailleFrame(t *testing.T) {
	m := otherRowSyncing(t)
	if st := m.jobsStatus(); !st.Activity || st.Spinner {
		t.Fatalf("status Activity=%v Spinner=%v, want Activity only", st.Activity, st.Spinner)
	}
	m = send(t, m, jobs.FrameMsg{Blink: true, Frame: "⠹", RenderedFrame: "⠹"})
	got := footerLine(m, 120)
	if !strings.Contains(ansi.Strip(got), "● ") || !strings.Contains(got, "0/1") {
		t.Errorf("want the dot and the progress, got %q", ansi.Strip(got))
	}
	if hasBraille(got) {
		t.Errorf("a braille frame reached the footer: %q", ansi.Strip(got))
	}
}

func TestTheProgressDotFollowsTheBlinkPhase(t *testing.T) {
	m := otherRowSyncing(t)
	on := ansi.Strip(footerLine(send(t, m, jobs.FrameMsg{Blink: true}), 120))
	off := ansi.Strip(footerLine(send(t, m, jobs.FrameMsg{Blink: false}), 120))
	if !strings.Contains(off, "○") || strings.Contains(off, "●") {
		t.Errorf("Blink=false must give the hollow dot only, got %q", off)
	}
	if strings.Index(on, "0/1") != strings.Index(off, "0/1") {
		t.Errorf("the progress moved between phases: %q vs %q", on, off)
	}
}

func TestNoUnfinishedRunMeansNoFooterDot(t *testing.T) {
	m := snapshot(t, indFixture(t), nil, nil)
	m = send(t, m, jobs.FrameMsg{Blink: false})
	if got := ansi.Strip(footerLine(m, 120)); strings.ContainsAny(got, "●○") {
		t.Errorf("a dot without a running job: %q", got)
	}
}

func TestAFooterErrorHidesTheDotWhileASyncRuns(t *testing.T) {
	m := send(t, otherRowSyncing(t), jobs.FrameMsg{Blink: true})
	m.footer.Error("it broke")
	got := ansi.Strip(footerLine(m, 120))
	if !strings.Contains(got, "it broke") || strings.ContainsAny(got, "●○") {
		t.Errorf("want the error and no dot, got %q", got)
	}
}

// T5: the cursor on the running row: its glyph detail is the only dot.
func TestARunningRowDetailLeavesExactlyOneDot(t *testing.T) {
	m := indFixture(t) // the cursor is on API
	m = snapshot(t, m, []jobs.Run{named(jobs.KindSync, jobs.ItemRunning, "api", "API")}, nil)
	m = send(t, m, jobs.FrameMsg{Blink: true})
	if got := ansi.Strip(footerLine(m, 120)); strings.Count(got, "●") != 1 {
		t.Errorf("want exactly one ●, got %q", got)
	}
}

func TestAFrameStartsNoTimerOfItsOwn(t *testing.T) {
	_, cmd := otherRowSyncing(t).Update(jobs.FrameMsg{Blink: true})
	if cmd != nil {
		t.Error("a FrameMsg returned a Cmd; the router owns the chain")
	}
}
