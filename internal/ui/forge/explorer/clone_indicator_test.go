package explorer

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-148 (A-10b) step 2: column 0 of the clone list is the activity
// column; the Status column keeps words only; the blink phase is read at
// render (same shape as DEVDESK-147's tree tests in indicator_test.go).

func cloneLine(t *testing.T, h *cloneHarness, name string) string {
	t.Helper()
	view := h.m.clone.table.View()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, name) {
			return l
		}
	}
	t.Fatalf("no rendered line holds %q:\n%s", name, view)
	return ""
}

// cloningWide is cloning(t) at 120 columns, so the table renders its rows.
func cloningWide(t *testing.T) *cloneHarness {
	t.Helper()
	h := cloning(t)
	h.m = feed(t, h.m, testutil.Resize(120, 30))
	return h
}

func blink(h *cloneHarness, on bool) {
	h.t.Helper()
	h.m = feed(h.t, h.m, jobs.FrameMsg{Blink: on})
}

func ended(path string) CloneEventMsg {
	return CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: path}}
}

func TestCloneColumnZeroIsTheActivityColumn(t *testing.T) {
	h := cloning(t)
	shapes := h.m.clone.table.Shapes()
	if len(shapes) < 4 {
		t.Fatalf("shapes = %+v, want indicator, Repository, Detail, Status", shapes)
	}
	s := shapes[0]
	if s.Title != "" || s.Sizing != datatable.SizingFixed || s.MinWidth != datatable.IconColumnWidth ||
		!s.Indicator || s.Less || s.Search {
		t.Errorf("column 0 = %+v, want untitled SizingFixed IconColumnWidth Indicator, no Less/Search", s)
	}
	var titles []string
	for _, c := range shapes[1:] {
		titles = append(titles, c.Title)
	}
	if want := []string{"Repository", "Detail", "Status"}; !reflect.DeepEqual(titles, want) {
		t.Errorf("columns after the indicator = %v, want %v", titles, want)
	}
}

func TestEachCloneStateRendersItsGlyphOnItsRow(t *testing.T) {
	tests := []struct {
		name  string
		evs   []tea.Msg
		blink bool
		want  string
	}{
		{"queued, phase on", []tea.Msg{found("alpha/api")}, true, indicator.Glyph(indicator.Queued, true)},
		{"queued, phase off", []tea.Msg{found("alpha/api")}, false, indicator.Glyph(indicator.Queued, false)},
		{"running, phase on", []tea.Msg{found("alpha/api"), began("alpha/api")}, true, indicator.Glyph(indicator.Running, true)},
		{"running, phase off", []tea.Msg{found("alpha/api"), began("alpha/api")}, false, indicator.Glyph(indicator.Running, false)},
		{"cloned", []tea.Msg{found("alpha/api"), began("alpha/api"), ended("alpha/api")}, true, indicator.Glyph(indicator.Done, true)},
		{"already there", []tea.Msg{found("alpha/api"), began("alpha/api"),
			CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "alpha/api", skipped: true}}}, true, indicator.Glyph(indicator.Skipped, true)},
		{"failed", []tea.Msg{found("alpha/api"), began("alpha/api"),
			CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "alpha/api", err: errors.New("boom")}}}, false, indicator.Glyph(indicator.Failed, false)},
		{"walk failed", []tea.Msg{
			CloneEventMsg{event: cloneEvent{kind: cloneWalkFailed, path: "alpha/sub", err: errors.New("403")}}}, true, indicator.Glyph(indicator.Failed, true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := cloningWide(t)
			h.emit(tc.evs...)
			blink(h, tc.blink)
			name := "alpha/api"
			if strings.Contains(tc.name, "walk") {
				name = "alpha/sub"
			}
			if tc.want == "" {
				t.Fatal("test bug: empty glyph")
			}
			line := cloneLine(t, h, name)
			if !strings.Contains(line, tc.want) {
				t.Errorf("row lacks %q:\n%s", tc.want, line)
			}
		})
	}
}

func TestQueuedIsAlwaysTheFixedGlyphEvenWithThePhaseOff(t *testing.T) {
	h := cloningWide(t)
	h.emit(found("alpha/api"))
	for _, on := range []bool{true, false} {
		blink(h, on)
		if line := cloneLine(t, h, "alpha/api"); !strings.Contains(line, "◴") {
			t.Errorf("blink=%v: queued row lacks ◴:\n%s", on, line)
		}
	}
}

func TestCloneGlyphsAreOneAlphabetWithJobs(t *testing.T) {
	// Q1-A: ItemIndicator as is.
	want := map[jobs.ItemState]string{
		jobs.ItemQueued: "◴", jobs.ItemDone: "◉", jobs.ItemSkipped: "⊖", jobs.ItemFailed: "⊗",
	}
	for st, g := range want {
		if got := indicator.Glyph(components.ItemIndicator(st), true); got != g {
			t.Errorf("state %v glyph = %q, want %q", st, got, g)
		}
	}
}

func TestACancelledQueuedCloneReadsCancelledWithTheSkippedGlyph(t *testing.T) {
	h := cloningWide(t)
	h.emit(found("alpha/api"), found("alpha/web"))
	h.reg.CancelOpen(jobs.KindClone)
	h.broadcast()

	if it := h.item(0); it.State != jobs.ItemSkipped || it.Detail != "cancelled" {
		t.Fatalf("item = %+v, want skipped/cancelled", it)
	}
	line := cloneLine(t, h, "alpha/api")
	if !strings.Contains(line, "⊖") {
		t.Errorf("cancelled row lacks ⊖:\n%s", line)
	}
	if !strings.Contains(line, "cancelled") || strings.Contains(line, "already there") {
		t.Errorf("cancelled row must read cancelled, not already there:\n%s", line)
	}
}

// Q3-A: every running clone blinks, the queued ones stay ◴.
func TestSeveralRunningClonesBlinkTogether(t *testing.T) {
	h := cloningWide(t)
	h.emit(found("alpha/a"), found("alpha/b"), found("alpha/c"), began("alpha/a"), began("alpha/b"))

	for _, on := range []bool{true, false} {
		blink(h, on)
		for _, n := range []string{"alpha/a", "alpha/b"} {
			if line := cloneLine(t, h, n); !strings.Contains(line, indicator.Glyph(indicator.Running, on)) {
				t.Errorf("blink=%v: running %s lacks the dot:\n%s", on, n, line)
			}
		}
		if line := cloneLine(t, h, "alpha/c"); !strings.Contains(line, "◴") {
			t.Errorf("blink=%v: queued row must stay ◴:\n%s", on, line)
		}
	}
}

// H5: a frame calls SetBlinkPhase only; rows are rebuilt on ChangedMsg.
func TestAFrameBlinksTheCloneListWithoutRebuildingIt(t *testing.T) {
	h := cloningWide(t)
	h.emit(found("alpha/a"), found("alpha/b"), began("alpha/a"), began("alpha/b"))
	blink(h, true)
	on := cloneLine(t, h, "alpha/a")
	before := h.m.clone.table.Items()
	snapshot := append([]cloneRow(nil), before...)

	blink(h, false)
	off := cloneLine(t, h, "alpha/a")

	if on == off {
		t.Fatalf("the glyph did not change with the phase:\n%s", on)
	}
	if h.m.clone.table.BlinkPhase() {
		t.Error("the clone table did not take the blink phase")
	}
	after := h.m.clone.table.Items()
	if len(before) == 0 || &before[0] != &after[0] {
		t.Error("a frame reallocated the clone rows (SetItems called)")
	}
	if !reflect.DeepEqual(snapshot, after) {
		t.Error("a frame changed the clone rows")
	}
}

func TestACloneRowCarriesNoFrameAndTheModelNoJobFrame(t *testing.T) {
	if _, has := reflect.TypeOf(cloneRow{}).FieldByName("frame"); has {
		t.Error("cloneRow still has a frame field")
	}
	if _, has := reflect.TypeOf(Model{}).FieldByName("jobFrame"); has {
		t.Error("Model still has a jobFrame field")
	}
}

func TestNoCloneStatusCellCarriesAGlyphIconSpinnerOrEscape(t *testing.T) {
	h := cloningWide(t)
	h.emit(found("a/queued"), found("a/run"), found("a/done"), found("a/skip"), found("a/fail"),
		began("a/run"), began("a/done"), began("a/skip"), began("a/fail"),
		ended("a/done"),
		CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "a/skip", skipped: true}},
		CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "a/fail", err: errors.New("x")}})
	blink(h, true)

	want := map[string]string{
		"a/queued": "queued", "a/run": "cloning", "a/done": "cloned",
		"a/skip": "already there", "a/fail": "failed",
	}
	icons := []string{theme.IconOK, theme.IconError, theme.IconPending, theme.IconSkipped}
	for _, row := range h.m.clone.table.Items() {
		cell := cloneStatusLabel(row)
		if cell != want[row.item.Target] {
			t.Errorf("%s status = %q, want exactly %q (words only)", row.item.Target, cell, want[row.item.Target])
		}
		for _, f := range append(spinner.Dot.Frames, icons...) {
			if f != "" && strings.Contains(cell, f) {
				t.Errorf("%s status %q holds %q", row.item.Target, cell, f)
			}
		}
		if strings.Contains(cell, "\x1b") {
			t.Errorf("%s status carries an escape: %q", row.item.Target, cell)
		}
	}
}

// Q2-A: the word is coloured like :jobs' State word, no green for done.
func TestCloneStatusWordsAreColouredLikeJobs(t *testing.T) {
	for _, st := range []jobs.ItemState{jobs.ItemQueued, jobs.ItemRunning, jobs.ItemDone, jobs.ItemSkipped, jobs.ItemFailed} {
		row := cloneRow{item: jobs.Item{State: st}}
		got := cloneStatusStyle(row).GetForeground()
		want := indicator.Style(components.ItemIndicator(st)).GetForeground()
		if got != want {
			t.Errorf("state %v foreground = %v, want %v", st, got, want)
		}
	}
	done := cloneStatusStyle(cloneRow{item: jobs.Item{State: jobs.ItemDone}}).GetForeground()
	if done == theme.StatusOKStyle.GetForeground() {
		t.Error("a cloned word is still the OK green (Rule 125)")
	}
}

func TestCloneStatusSaysCancelledForACancelledItem(t *testing.T) {
	row := cloneRow{item: jobs.Item{State: jobs.ItemSkipped, Detail: "cancelled"}}
	if got := cloneStatusLabel(row); got != "cancelled" {
		t.Errorf("label = %q, want cancelled", got)
	}
	row.item.Detail = "already on disk"
	if got := cloneStatusLabel(row); got != "already there" {
		t.Errorf("label = %q, want already there", got)
	}
}

func TestActivityTablesReportTreeAndCloneList(t *testing.T) {
	check := func(t *testing.T, m Model, wantRendered int) {
		t.Helper()
		tables := m.ActivityTables()
		if len(tables) != 2 || tables["explorer/tree"] == nil || tables["explorer/clone-list"] == nil {
			var keys []string
			for k := range tables {
				keys = append(keys, k)
			}
			t.Fatalf("ActivityTables() keys = %v, want [explorer/tree explorer/clone-list]", keys)
		}
		shapes := tables["explorer/clone-list"].Shapes()
		if len(shapes) == 0 || !shapes[0].Indicator {
			t.Errorf("clone-list has no activity column 0: %+v", shapes)
		}
		if wantRendered > 0 {
			if got := tables["explorer/clone-list"].RenderedWidth(); got != wantRendered {
				t.Errorf("clone-list RenderedWidth = %d, want %d", got, wantRendered)
			}
		}
	}
	t.Run("without a run", func(t *testing.T) {
		check(t, feed(t, loadedModel(t), testutil.Resize(60, 30)), 58)
	})
	t.Run("with a run", func(t *testing.T) {
		h := cloningWide(t)
		h.emit(found("alpha/api"))
		h.m = feed(t, h.m, testutil.Resize(60, 30))
		check(t, h.m, 58)
	})
}

// Rule 116: the table fills the width exactly; the optional Detail column is
// shown from width 75 (dot 2 + Repository 30 + Status 13, Q2-A).
func TestCloneListRule116Sweep(t *testing.T) {
	long := "alpha/a-very-long-group-name/another-long-subgroup/repository-with-a-long-name"
	for _, width := range []int{60, 74, 75, 78, 80, 120} {
		h := cloning(t)
		h.emit(found(long), found("alpha/api"), began(long))
		h.m = feed(t, h.m, testutil.Resize(width, 30))
		if got := h.m.clone.table.RenderedWidth(); got != width-2 {
			t.Errorf("width %d: RenderedWidth = %d, want %d", width, got, width-2)
		}
		header := strings.Split(h.m.clone.table.View(), "\n")[0]
		if has, want := strings.Contains(header, "Detail"), width >= 75; has != want {
			t.Errorf("width %d: Detail header present = %v, want %v:\n%s", width, has, want, header)
		}
		if !strings.Contains(header, "Status") {
			t.Errorf("width %d: Status header missing:\n%s", width, header)
		}
	}
}
