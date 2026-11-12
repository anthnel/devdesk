package explorer

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-147 / DEVDESK-223 step 3: column 0 of the explorer tree is the
// activity column. The row carries a state, the blink phase is read at render.

func treeLine(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, l := range strings.Split(m.table.View(), "\n") {
		if strings.Contains(l, name) {
			return l
		}
	}
	t.Fatalf("no rendered line holds %q:\n%s", name, m.table.View())
	return ""
}

func anyGlyph(line string) bool {
	if strings.Contains(line, indicator.Glyph(indicator.Running, false)) {
		return true
	}
	for _, s := range indicator.States() {
		if g := indicator.Glyph(s, true); g != "" && strings.Contains(line, g) {
			return true
		}
	}
	return false
}

// deleting returns a loaded model whose "alpha" row has a delete in flight.
func deleting(t *testing.T) Model {
	t.Helper()
	return feed(t, loadedModel(t), jobs.ChangedMsg{
		Runs: actRunning(t, jobs.KindDelete, actCtx, "alpha"), Context: actCtx,
	})
}

func TestColumnZeroIsTheActivityColumn(t *testing.T) {
	c := explorerColumns()[0]
	if c.Title != "" || c.Sizing != datatable.SizingFixed || c.MinWidth != datatable.IconColumnWidth {
		t.Errorf("column 0 = %q/%v/%d, want untitled SizingFixed IconColumnWidth", c.Title, c.Sizing, c.MinWidth)
	}
	if c.Indicator == nil {
		t.Error("column 0 is not built by datatable.IndicatorColumn")
	}
	if c.Less != nil || c.Search != nil {
		t.Errorf("column 0 declares Less=%v Search=%v, want neither", c.Less != nil, c.Search != nil)
	}
	m := loadedModel(t)
	shapes := m.table.Shapes()
	if len(shapes) == 0 || !shapes[0].Indicator {
		t.Errorf("the live tree has no activity column 0: %+v", shapes)
	}
}

func TestTheRowCarriesNoFrameAnyMore(t *testing.T) {
	if _, has := reflect.TypeOf(explorerRow{}).FieldByName("frame"); has {
		t.Error("explorerRow still has a frame field: the blink phase is read at render")
	}
}

func TestRowsAreStampedFromTheRegistrySnapshot(t *testing.T) {
	m := deleting(t)
	row, ok := rowFor(m, "alpha")
	if !ok {
		t.Fatal("no alpha row")
	}
	if row.Activity != indicator.Running {
		t.Errorf("alpha Activity = %v, want Running", row.Activity)
	}
	if other, _ := rowFor(m, "beta"); other.Activity != indicator.Idle {
		t.Errorf("beta Activity = %v, want Idle", other.Activity)
	}
	// Done: back to the icon.
	m = feed(t, m, jobs.ChangedMsg{Context: actCtx})
	if row, _ := rowFor(m, "alpha"); row.Activity != indicator.Idle {
		t.Errorf("a settled row: Activity = %v, want Idle", row.Activity)
	}
}

func TestEachActivityStateRendersItsGlyphOnItsRow(t *testing.T) {
	run := actRunning(t, jobs.KindDelete, actCtx, "alpha")
	fail := actFailure(jobs.KindDelete, actCtx, "alpha", "check logs")
	tests := []struct {
		name  string
		runs  []jobs.Run
		fails []jobs.Failure
		blink bool
		state indicator.State
	}{
		{"running, phase on", run, nil, true, indicator.Running},
		{"running, phase off", run, nil, false, indicator.Running},
		{"failed, phase on", nil, []jobs.Failure{fail}, true, indicator.Failed},
		{"failed, phase off", nil, []jobs.Failure{fail}, false, indicator.Failed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, loadedModel(t),
				jobs.ChangedMsg{Runs: tc.runs, Failures: tc.fails, Context: actCtx},
				jobs.FrameMsg{Blink: tc.blink})
			line := treeLine(t, m, "alpha")
			if want := indicator.Glyph(tc.state, tc.blink); !strings.Contains(line, want) {
				t.Errorf("row lacks %q:\n%s", want, line)
			}
			if strings.Contains(line, theme.IconNamespace) {
				t.Errorf("the glyph shares the row with the resting icon:\n%s", line)
			}
			if other := treeLine(t, m, "beta"); anyGlyph(other) {
				t.Errorf("an untouched row shows an activity glyph:\n%s", other)
			}
		})
	}
}

func TestAFailedCreateOnARealRowNeverPaintsIt(t *testing.T) {
	// H1 / Q1-C: create is read on placeholders only.
	m := feed(t, loadedModel(t), jobs.ChangedMsg{
		Failures: []jobs.Failure{actFailure(jobs.KindCreate, actCtx, "alpha", "boom")}, Context: actCtx,
	})
	if line := treeLine(t, m, "alpha"); anyGlyph(line) {
		t.Errorf("a create failure marks a healthy row:\n%s", line)
	}
}

func TestAnIdleRowKeepsItsKindIcon(t *testing.T) {
	m := feed(t, drilledModel(t), jobs.FrameMsg{Blink: true})
	if line := treeLine(t, m, "sub"); !strings.Contains(line, theme.IconNamespace) {
		t.Errorf("idle group lacks %q:\n%s", theme.IconNamespace, line)
	}
	if line := treeLine(t, m, "api"); !strings.Contains(line, theme.IconRepository) {
		t.Errorf("idle repository lacks %q:\n%s", theme.IconRepository, line)
	}
}

func TestAPlaceholderBlinksWhileItsCreateRuns(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})
	m = feed(t, m,
		jobs.ChangedMsg{Runs: actRunning(t, jobs.KindCreate, actCtx, "new"), Context: actCtx},
		jobs.FrameMsg{Blink: true})
	row, ok := rowFor(m, "new")
	if !ok || row.Activity != indicator.Running {
		t.Fatalf("placeholder = %+v (found %v), want Running", row.Activity, ok)
	}
	if line := treeLine(t, m, "new"); !strings.Contains(line, indicator.Glyph(indicator.Running, true)) {
		t.Errorf("placeholder lacks the running dot:\n%s", line)
	}
}

// A frame moves the glyph and rebuilds nothing (H5): the rows are the same
// values before and after.
func TestAFrameBlinksWithoutRebuildingTheRows(t *testing.T) {
	m := feed(t, deleting(t), jobs.FrameMsg{Blink: true})
	on := treeLine(t, m, "alpha")
	before := append([]explorerRow(nil), m.table.Items()...)

	m = feed(t, m, jobs.FrameMsg{Blink: false})
	off := treeLine(t, m, "alpha")

	if on == off {
		t.Fatalf("the glyph did not change with the phase:\n%s", on)
	}
	if !strings.Contains(on, indicator.Glyph(indicator.Running, true)) ||
		!strings.Contains(off, indicator.Glyph(indicator.Running, false)) {
		t.Errorf("phase glyphs wrong:\non : %s\noff: %s", on, off)
	}
	if m.table.BlinkPhase() {
		t.Error("the table did not take the blink phase")
	}
	if !reflect.DeepEqual(before, m.table.Items()) {
		t.Error("a frame rebuilt the tree rows")
	}
}

// SetItems resets nothing observable, so detect a rebuild by identity: a frame
// must not reallocate the backing slice.
func TestAFrameDoesNotCallSetItems(t *testing.T) {
	m := deleting(t)
	before := m.table.Items()
	m = feed(t, m, jobs.FrameMsg{Blink: true}, jobs.FrameMsg{Blink: false})
	after := m.table.Items()
	if len(before) == 0 || &before[0] != &after[0] {
		t.Error("the tree rows were rebuilt by a frame")
	}
}

func TestTheTreeAndTheCloneListBothTakeThePhase(t *testing.T) {
	h := cloning(t)
	h.emit(found("alpha/api"), began("alpha/api"))
	h.m = feed(t, h.m, jobs.FrameMsg{Blink: true})
	if !h.m.table.BlinkPhase() || !h.m.clone.table.BlinkPhase() {
		t.Errorf("phase: tree=%v clone=%v, want both on", h.m.table.BlinkPhase(), h.m.clone.table.BlinkPhase())
	}
}

// ChangedMsg during a clone returns early; the tree rows must catch up when
// the list closes.
func TestTheRowsAreRebuiltWhenTheCloneListCloses(t *testing.T) {
	h := cloning(t)
	h.m = feed(t, h.m, jobs.ChangedMsg{
		Runs: actRunning(t, jobs.KindDelete, actCtx, "alpha/api"), Context: actCtx,
	})
	next, _ := h.m.handleCloneClose()
	m := next.(Model)
	row, ok := rowFor(m, "alpha/api")
	if !ok || row.Activity != indicator.Running {
		t.Errorf("after the close, alpha/api = %v (found %v), want Running", row.Activity, ok)
	}
}

func TestInSelectionTheRestIsTheCheckboxAndFailedShowsItToo(t *testing.T) {
	m := feed(t, drilledModel(t), jobs.ChangedMsg{
		Runs:     actRunning(t, jobs.KindDelete, actCtx, "alpha/sub"),
		Failures: []jobs.Failure{actFailure(jobs.KindDelete, actCtx, "alpha/api", "x")},
		Context:  actCtx,
	}, testutil.Key(keymap.Clone))
	if m.mode != ModeSelecting {
		t.Fatalf("mode = %v, want ModeSelecting", m.mode)
	}
	sub, _ := rowFor(m, "alpha/sub")
	api, _ := rowFor(m, "alpha/api")
	if sub.Activity != indicator.Running {
		t.Errorf("running row in selection: Activity = %v, want Running (keeps its glyph)", sub.Activity)
	}
	if api.Activity != indicator.Idle {
		t.Errorf("failed row in selection: Activity = %v, want Idle so the checkbox shows (H2)", api.Activity)
	}
	if got := restIcon(api); got != theme.IconCheckbox {
		t.Errorf("failed row rest = %q, want the checkbox", got)
	}
	if line := treeLine(t, m, "api"); !strings.Contains(line, theme.IconCheckbox) || anyGlyph(line) {
		t.Errorf("failed row in selection should show its checkbox only:\n%s", line)
	}
}

func TestNoTreeCellCarriesASpinnerRuneOrEscape(t *testing.T) {
	m := feed(t, deleting(t), jobs.FrameMsg{Blink: true})
	for _, row := range m.table.Items() {
		for i, col := range explorerColumns() {
			cell := col.Cell(row)
			if strings.Contains(cell, "\x1b") {
				t.Errorf("%s col %d carries an escape: %q", row.node.FullPath, i, cell)
			}
			for _, f := range spinner.Dot.Frames {
				if strings.Contains(cell, f) {
					t.Errorf("%s col %d holds a spinner rune: %q", row.node.FullPath, i, cell)
				}
			}
		}
	}
}

func TestRule116At60Columns(t *testing.T) {
	m := feed(t, deleting(t), testutil.Resize(60, 30))
	if got := m.table.RenderedWidth(); got != 58 {
		t.Errorf("RenderedWidth = %d at 60 columns, want 58", got)
	}
}
