package jobsview

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-341 step 2b: the footer count carries the shared activity dot, phase
// from jobs.FrameMsg.Blink. A selected row has a glyph detail whose own dot
// already moves, so the dot only shows when no row is selected (a filter that
// matches none of the runs).

func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

// unselectedRunning has a run in flight and a filter matching no row, so the
// footer says "1 job running" with no row detail beside it.
func unselectedRunning(t *testing.T) Model {
	t.Helper()
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))
	m = feed(t, m, testutil.Key("/"))
	for _, msg := range testutil.Type("zzzz") {
		m = feed(t, m, msg)
	}
	m = feed(t, m, testutil.Key("enter"))
	if !m.status().Detail.IsZero() {
		t.Fatalf("precondition: the filter must leave no selected row, detail = %+v", m.status().Detail)
	}
	return m
}

func TestTheJobsCountShowsTheActivityDotNotABrailleFrame(t *testing.T) {
	m := unselectedRunning(t)
	if st := m.status(); !st.Activity || st.Spinner {
		t.Fatalf("status Activity=%v Spinner=%v, want Activity only", st.Activity, st.Spinner)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true, Frame: "⠹", RenderedFrame: "⠹"})
	footer := m.RenderFooter(120)
	if !strings.Contains(ansi.Strip(footer), "● 1 job running") {
		t.Errorf("want the dot before the count, got %q", ansi.Strip(footer))
	}
	if hasBraille(footer) {
		t.Errorf("a braille frame reached the footer: %q", ansi.Strip(footer))
	}
}

func TestTheJobsCountDotFollowsTheBlinkPhase(t *testing.T) {
	m := unselectedRunning(t)
	on := ansi.Strip(feed(t, m, jobs.FrameMsg{Blink: true}).RenderFooter(120))
	off := ansi.Strip(feed(t, m, jobs.FrameMsg{Blink: false}).RenderFooter(120))
	if !strings.Contains(off, "○ 1 job running") {
		t.Errorf("Blink=false must give the hollow dot, got %q", off)
	}
	if strings.Index(on, "1 job") != strings.Index(off, "1 job") {
		t.Errorf("the text moved between phases: %q vs %q", on, off)
	}
}

func TestNoRunningJobMeansNoFooterDot(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemDone))
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if got := ansi.Strip(m.RenderFooter(120)); strings.ContainsAny(got, "●○") {
		t.Errorf("a dot without a running job: %q", got)
	}
}

func TestAFooterErrorHidesTheDot(t *testing.T) {
	m := feed(t, unselectedRunning(t), jobs.FrameMsg{Blink: true})
	m.footer.Error("it broke")
	got := ansi.Strip(m.RenderFooter(120))
	if !strings.Contains(got, "it broke") || strings.ContainsAny(got, "●○") {
		t.Errorf("want the error and no dot, got %q", got)
	}
}

// T5: a selected running row says so with its own dot: exactly one ● in the footer.
func TestASelectedRunningRowLeavesExactlyOneDot(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if n := strings.Count(ansi.Strip(m.RenderFooter(120)), "●"); n != 1 {
		t.Errorf("want exactly one ●, got %d in %q", n, ansi.Strip(m.RenderFooter(120)))
	}
}

func TestAFrameStartsNoTimerOfItsOwn(t *testing.T) {
	_, cmd := step(t, unselectedRunning(t), jobs.FrameMsg{Blink: true})
	if cmd != nil {
		t.Error("a FrameMsg returned a Cmd; the router owns the chain")
	}
}
