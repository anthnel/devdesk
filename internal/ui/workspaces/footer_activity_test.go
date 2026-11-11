package workspaces

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-341 step 2c: the batch progress line carries the shared activity dot
// (phase from jobs.FrameMsg.Blink), never the router's braille frame.

func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

// infoLine is the footer's last line, stripped: the one the dot lives on.
func infoLine(m Model, width int) string {
	lines := strings.Split(ansi.Strip(m.RenderFooter(width)), "\n")
	return lines[len(lines)-1]
}

// syncOnIdleRow runs a sync with the cursor on a row the sync does not touch,
// so the row detail has no moving dot and the footer must carry its own.
func syncOnIdleRow(t *testing.T) Model {
	t.Helper()
	m := running(t, loadedModel(t), jobs.KindSync, devdeskPath)
	return selectRow(t, m, "clean-repo")
}

func TestTheProgressLineShowsTheActivityDotNotABrailleFrame(t *testing.T) {
	m := syncOnIdleRow(t)
	if st := m.status(); !st.Activity || st.Spinner {
		t.Fatalf("status Activity=%v Spinner=%v, want Activity only", st.Activity, st.Spinner)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true, Frame: "⠹", RenderedFrame: "⠹"})
	if got := infoLine(m, 160); !strings.Contains(got, "●") {
		t.Errorf("want the dot on the info line, got %q", got)
	}
	if hasBraille(m.RenderFooter(160)) {
		t.Errorf("a braille frame reached the footer: %q", ansi.Strip(m.RenderFooter(160)))
	}
}

func TestTheProgressLineDotFollowsTheBlinkPhase(t *testing.T) {
	m := syncOnIdleRow(t)
	on := infoLine(feed(t, m, jobs.FrameMsg{Blink: true}), 160)
	off := infoLine(feed(t, m, jobs.FrameMsg{Blink: false}), 160)
	if !strings.Contains(off, "○") || strings.Contains(off, "●") {
		t.Errorf("Blink=false must give the hollow dot only, got %q", off)
	}
	col := func(s string) int {
		i := strings.Index(s, "Syncing")
		if i < 0 {
			return -1
		}
		return ansi.StringWidth(s[:i])
	}
	if col(on) < 0 || col(on) != col(off) {
		t.Errorf("the text moved between phases: %q vs %q", on, off)
	}
}

func TestNoUnfinishedRunMeansNoFooterDot(t *testing.T) {
	m := feed(t, loadedModel(t), jobs.ChangedMsg{}, jobs.FrameMsg{Blink: false})
	if got := infoLine(m, 160); strings.ContainsAny(got, "●○") {
		t.Errorf("a dot without a running job: %q", got)
	}
}

func TestAFooterErrorHidesTheDotWhileASyncRuns(t *testing.T) {
	m := feed(t, syncOnIdleRow(t), jobs.FrameMsg{Blink: true})
	m.footer.Error("it broke")
	got := infoLine(m, 160)
	if !strings.Contains(got, "it broke") || strings.ContainsAny(got, "●○") {
		t.Errorf("want the error and no dot, got %q", got)
	}
}

// T5: the cursor on the running row: its glyph detail is the only dot.
func TestARunningRowDetailLeavesExactlyOneDot(t *testing.T) {
	m := running(t, loadedModel(t), jobs.KindSync, devdeskPath)
	m = selectRow(t, m, "devdesk")
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if got := infoLine(m, 160); strings.Count(got, "●") != 1 {
		t.Errorf("want exactly one ●, got %q", got)
	}
}

func TestAFrameStartsNoTimerOfItsOwn(t *testing.T) {
	_, cmd := step(t, syncOnIdleRow(t), jobs.FrameMsg{Blink: true})
	if cmd != nil {
		t.Error("a FrameMsg returned a Cmd; the router owns the chain")
	}
}
