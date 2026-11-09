package ociresources

import (
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-146 step 4: the Images footer names the SELECTED row's activity
// (rowActivity(...).Detail), composed with the action line or the batch
// progress. The load stays first. Timers are never run.

func selectImage(t *testing.T, m Model, raw string) Model {
	t.Helper()
	m = feed(t, m, testutil.Key("home"))
	for range len(m.imageTable.Items()) + 1 {
		if r, ok := m.imageTable.Selected(); ok && r.RawName == raw {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("could not select %q", raw)
	return m
}

func ociInfoLine(m Model, width int) string {
	lines := strings.Split(m.RenderFooter(width), "\n")
	return lines[len(lines)-1]
}

func TestFooterOfAFailedImageNamesItsCauseDuringACtrlR(t *testing.T) {
	f := ociFailure(jobs.KindScan, ociCtx, imgRef, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	m := selectImage(t, ociSnapshot(t, ociCtx, nil, []jobs.Failure{f}), imgRef)

	for _, loading := range []bool{false, true} {
		m.loading = loading // a refresh with rows on screen is not the priority load
		got := ociInfoLine(m, 200)
		for _, w := range []string{imgRef, "check logs"} {
			if !strings.Contains(got, w) {
				t.Errorf("loading=%v: failed row footer lacks %q:\n%s", loading, w, got)
			}
		}
	}
}

func TestFooterOfAFailedImageFollowsTheCursor(t *testing.T) {
	f := ociFailure(jobs.KindScan, ociCtx, imgRef, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	m := ociSnapshot(t, ociCtx, nil, []jobs.Failure{f})
	other := ""
	for _, r := range m.imageTable.Items() {
		if r.RawName != imgRef {
			other = r.RawName
			break
		}
	}
	if other == "" {
		t.Skip("fixtures have a single image")
	}
	if got := ociInfoLine(selectImage(t, m, other), 200); strings.Contains(got, "check logs") {
		t.Errorf("another row shows the failure: %q", got)
	}
}

func TestFooterOfARunningScanNamesTheRowAndTheBatch(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil)
	m = selectImage(t, m, imgRef)
	got := ociInfoLine(m, 200)
	for _, w := range []string{"Scanning", imgRef, glyphDot} {
		if !strings.Contains(got, w) {
			t.Errorf("running row footer lacks %q:\n%s", w, got)
		}
	}
}

func TestFooterOfABusyRemovalReadsRemoving(t *testing.T) {
	m := selectImage(t, loadedModel(t), imgRef)
	m = removeSelected(t, m)
	got := ociInfoLine(m, 200)
	for _, w := range []string{"Removing", imgRef, glyphDot} {
		if !strings.Contains(got, w) {
			t.Errorf("busy row footer lacks %q:\n%s", w, got)
		}
	}
}

func TestFooterOfAnIdleImageAddsNothing(t *testing.T) {
	m := selectImage(t, ociSnapshot(t, ociCtx, nil, nil), imgRef)
	if got := strings.TrimSpace(ociInfoLine(m, 120)); got != "" {
		t.Errorf("idle row, no work: info line = %q, want empty", got)
	}
}

func TestTheLoadStaysAheadOfTheRowDetail(t *testing.T) {
	f := ociFailure(jobs.KindScan, ociCtx, imgRef, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	m := selectImage(t, ociSnapshot(t, ociCtx, nil, []jobs.Failure{f}), imgRef)
	m.loading = true
	m.images = nil // the priority load: nothing listed yet
	got := ociInfoLine(m, 200)
	if !strings.Contains(got, "Loading images...") {
		t.Errorf("load text missing:\n%s", got)
	}
	if strings.Contains(got, "check logs") {
		t.Errorf("a row detail pre-empts the priority load:\n%s", got)
	}
}

func TestFooterStaysOneLineWithARowDetail(t *testing.T) {
	f := ociFailure(jobs.KindScan, ociCtx, imgRef, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	m := selectImage(t, ociSnapshot(t, ociCtx, nil, []jobs.Failure{f}), imgRef)
	if n := strings.Count(m.RenderFooter(40), "\n") + 1; n != m.GetFooterHeight() {
		t.Errorf("footer renders %d lines, GetFooterHeight says %d", n, m.GetFooterHeight())
	}
}

// A scan with no load still shows the footer spinner on an idle row; the router's
// frames must move it, or "Scanning — 1/3" carries a frozen frame (Rule 128).
func TestAFrameMovesTheFooterSpinnerDuringAScanWithNoLoad(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil)
	other := ""
	for _, r := range m.imageTable.Items() {
		if r.RawName != imgRef {
			other = r.RawName
			break
		}
	}
	if other == "" {
		t.Skip("fixtures have a single image")
	}
	m = selectImage(t, m, other)
	m.loading = false

	for _, frame := range []string{"<F1>", "<F2>"} {
		m = feed(t, m, jobs.FrameMsg{Frame: frame, RenderedFrame: frame})
		if got := ociInfoLine(m, 200); !strings.Contains(got, frame) {
			t.Errorf("footer spinner did not move to %q during a scan:\n%s", frame, got)
		}
	}
}
