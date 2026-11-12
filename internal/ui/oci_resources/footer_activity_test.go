package ociresources

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-341 step 2f: jobs work shows the shared activity dot (phase from
// jobs.FrameMsg.Blink); a tab load does too (DEVDESK-374).

func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

func plainOciInfoLine(m Model, width int) string { return ansi.Strip(ociInfoLine(m, width)) }

// scanOnAnotherImage runs a scan with the cursor on an idle image and no load.
func scanOnAnotherImage(t *testing.T) Model {
	t.Helper()
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil)
	other := ""
	for _, r := range m.imageTable.Items() {
		if r.RawName != imgRef {
			other = r.RawName
			break
		}
	}
	if other == "" {
		t.Fatal("fixtures have a single image")
	}
	m = selectImage(t, m, other)
	m.loading = false
	return m
}

func TestJobsWorkShowsTheActivityDotNotABrailleFrame(t *testing.T) {
	m := scanOnAnotherImage(t)
	if st := m.status(); !st.Activity {
		t.Fatalf("status Activity=%v, want Activity only", st.Activity)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	got := ociInfoLine(m, 200)
	if !strings.Contains(ansi.Strip(got), "● ") {
		t.Errorf("want the dot on the info line, got %q", ansi.Strip(got))
	}
	if hasBraille(got) {
		t.Errorf("a braille frame reached the footer: %q", ansi.Strip(got))
	}
}

func TestJobsDotFollowsTheBlinkPhase(t *testing.T) {
	m := scanOnAnotherImage(t)
	on := plainOciInfoLine(feed(t, m, jobs.FrameMsg{Blink: true}), 200)
	off := plainOciInfoLine(feed(t, m, jobs.FrameMsg{Blink: false}), 200)
	if !strings.Contains(off, "○") || strings.Contains(off, "●") {
		t.Errorf("Blink=false must give the hollow dot only, got %q", off)
	}
	if strings.Index(on, "Scanning") != strings.Index(off, "Scanning") {
		t.Errorf("the text moved between phases: %q vs %q", on, off)
	}
}

// A tab load shows the shared dot and follows the router's phase; the view
// holds no spinner of its own (DEVDESK-374).
func TestATabLoadShowsTheDotAndFollowsThePhase(t *testing.T) {
	m := newTestModel(t)
	if st := m.status(); !st.Activity || st.Text != "Loading images..." {
		t.Fatalf("while loading: %+v, want Activity with the same text and no Spinner", st)
	}
	for _, phase := range []struct {
		blink      bool
		want, deny string
	}{{true, "●", "○"}, {false, "○", "●"}} {
		got := plainOciInfoLine(feed(t, m, jobs.FrameMsg{Blink: phase.blink}), 200)
		if !strings.Contains(got, "Loading images...") || strings.Count(got, phase.want) != 1 ||
			strings.Contains(got, phase.deny) || hasBraille(got) {
			t.Errorf("blink=%v: want one %s, no %s, no braille, the load text: %q", phase.blink, phase.want, phase.deny, got)
		}
	}
}

func TestNoUnfinishedRunMeansNoFooterDot(t *testing.T) {
	m := ociSnapshot(t, ociCtx, nil, nil)
	m.loading = false
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if got := plainOciInfoLine(m, 200); strings.ContainsAny(got, "●○") {
		t.Errorf("a dot without a running job: %q", got)
	}
}

func TestAFooterErrorHidesTheDotWhileAScanRuns(t *testing.T) {
	m := feed(t, scanOnAnotherImage(t), jobs.FrameMsg{Blink: true})
	m.footer.Error("it broke")
	got := plainOciInfoLine(m, 200)
	if !strings.Contains(got, "it broke") || strings.ContainsAny(got, "●○") {
		t.Errorf("want the error and no dot, got %q", got)
	}
}

// T5: the cursor on the scanned image: its glyph detail is the only dot.
func TestARunningRowDetailLeavesExactlyOneDot(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil)
	m.loading = false
	m = feed(t, selectImage(t, m, imgRef), jobs.FrameMsg{Blink: true})
	if got := plainOciInfoLine(m, 200); strings.Count(got, "●") != 1 {
		t.Errorf("want exactly one ●, got %q", got)
	}
}

func TestAFrameStartsNoTimerOfItsOwn(t *testing.T) {
	_, cmd := scanOnAnotherImage(t).Update(jobs.FrameMsg{Blink: true})
	if cmd != nil {
		t.Error("a FrameMsg returned a Cmd; the router owns the chain")
	}
}
