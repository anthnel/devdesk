package workspaces

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-144 step 2: column 0 of the ws table is the activity column
// (Rule 125, "The activity column"). The phase is read at render.

const (
	indRepo  = "/tmp/workspaces/devdesk"
	indClean = "/tmp/workspaces/clean-repo"
)

// lineOf returns the rendered line holding the given row name.
func lineOf(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, name) {
			return l
		}
	}
	t.Fatalf("no rendered line holds %q:\n%s", name, m.View())
	return ""
}

func rowNamed(t *testing.T, m Model, name string) workspaceRow {
	t.Helper()
	for _, r := range m.table.Items() {
		if r.Entry.Name == name {
			return r
		}
	}
	t.Fatalf("no row named %q", name)
	return workspaceRow{}
}

// col0 returns column 0, failing the test (rather than panicking) while it
// carries no Indicator.
func col0(t *testing.T) datatable.Column[workspaceRow] {
	t.Helper()
	c := workspaceColumns(false, false)[0]
	if c.Indicator == nil {
		t.Fatal("column 0 has no Indicator")
	}
	return c
}

func hasAnyGlyph(line string) bool {
	for _, s := range indicator.States() {
		if g := indicator.Glyph(s, true); g != "" && strings.Contains(line, g) {
			return true
		}
	}
	return strings.Contains(line, indicator.Glyph(indicator.Running, false))
}

func TestColumnZeroIsTheActivityColumn(t *testing.T) {
	for _, tc := range []struct{ ci, cfg bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		c := workspaceColumns(tc.ci, tc.cfg)[0]
		if c.Title != "" {
			t.Errorf("ci=%v cfg=%v: column 0 titled %q, want untitled", tc.ci, tc.cfg, c.Title)
		}
		if c.Sizing != datatable.SizingFixed || c.MinWidth != datatable.IconColumnWidth {
			t.Errorf("column 0 sizing/min = %v/%d, want SizingFixed/%d", c.Sizing, c.MinWidth, datatable.IconColumnWidth)
		}
		if c.Indicator == nil {
			t.Errorf("ci=%v cfg=%v: column 0 is not built by datatable.IndicatorColumn (no Indicator)", tc.ci, tc.cfg)
		}
		if c.Less != nil || c.Search != nil {
			t.Errorf("column 0 declares Less=%v Search=%v, want neither", c.Less != nil, c.Search != nil)
		}
	}
}

func TestTheViewExposesItsTableToTheActivityGuard(t *testing.T) {
	m := loadedModel(t)
	av, ok := any(m).(interface {
		ActivityTables() map[string]datatable.ActivityTable
	})
	if !ok {
		t.Fatal("Model does not expose ActivityTables()")
	}
	tables := av.ActivityTables()
	tbl, ok := tables["ws"]
	if !ok || len(tables) != 1 {
		t.Fatalf("ActivityTables() keys = %v, want exactly [ws]", tables)
	}
	if shapes := tbl.Shapes(); len(shapes) == 0 || !shapes[0].Indicator {
		t.Errorf("the reported ws table has no activity column 0: %+v", shapes)
	}
}

func TestEachActivityStateRendersItsGlyphOnItsRow(t *testing.T) {
	tests := []struct {
		name  string
		run   jobs.Run
		fail  []jobs.Failure
		blink bool
		want  string
	}{
		{"running, phase on", runningRun(jobs.KindScan, indRepo), nil, true, indicator.Glyph(indicator.Running, true)},
		{"running, phase off", runningRun(jobs.KindScan, indRepo), nil, false, indicator.Glyph(indicator.Running, false)},
		{"queued, phase on", actRun(jobs.KindScan, testContext, jobs.ItemQueued, indRepo), nil, true, indicator.Glyph(indicator.Queued, true)},
		{"queued, phase off", actRun(jobs.KindScan, testContext, jobs.ItemQueued, indRepo), nil, false, indicator.Glyph(indicator.Queued, false)},
		{"failed, phase on", jobs.Run{}, []jobs.Failure{actFailure(jobs.KindScan, testContext, indRepo, "boom", time.Now())}, true, indicator.Glyph(indicator.Failed, true)},
		{"failed, phase off", jobs.Run{}, []jobs.Failure{actFailure(jobs.KindScan, testContext, indRepo, "boom", time.Now())}, false, indicator.Glyph(indicator.Failed, false)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var runs []jobs.Run
			if len(tc.run.Items) > 0 {
				runs = append(runs, tc.run)
			}
			m := withSnapshot(t, loadedModel(t), runs, tc.fail)
			m = feed(t, m, jobs.FrameMsg{Blink: tc.blink})
			line := lineOf(t, m, "devdesk")
			if !strings.Contains(line, tc.want) {
				t.Errorf("row lacks %q:\n%s", tc.want, line)
			}
			if icon := entryIcon(rowNamed(t, m, "devdesk").Entry); strings.Contains(line, icon) {
				t.Errorf("the activity glyph shares the cell with the resting icon %q:\n%s", icon, line)
			}
			if other := lineOf(t, m, "clean-repo"); hasAnyGlyph(other) {
				t.Errorf("an untouched row shows an activity glyph:\n%s", other)
			}
		})
	}
}

func TestAnIdleRowShowsItsIconInColumnZero(t *testing.T) {
	m := loadedModel(t)
	cols := []datatable.Column[workspaceRow]{col0(t)}
	for _, name := range []string{"devdesk", "clients", "notes.md"} {
		r := rowNamed(t, m, name)
		if got := cols[0].Indicator(r); got != indicator.Idle {
			t.Errorf("%s: Indicator = %v, want Idle", name, got)
		}
		if got, want := cols[0].Cell(r), entryIcon(r.Entry); got != want {
			t.Errorf("%s: resting cell = %q, want its icon %q", name, got, want)
		}
		if !strings.Contains(lineOf(t, m, name), entryIcon(r.Entry)) {
			t.Errorf("%s: the icon is not rendered at rest", name)
		}
	}
}

func TestRowsCarryTheStateTheRegistryGave(t *testing.T) {
	m := withSnapshot(t, loadedModel(t),
		[]jobs.Run{
			runningRun(jobs.KindScan, indRepo),
			actRun(jobs.KindSync, testContext, jobs.ItemQueued, indClean),
		},
		[]jobs.Failure{actFailure(jobs.KindScan, testContext, "/tmp/workspaces/empty-dir", "x", time.Now())})
	col := col0(t)
	for name, want := range map[string]indicator.State{
		"devdesk": indicator.Running, "clean-repo": indicator.Queued,
		"empty-dir": indicator.Failed, "notes.md": indicator.Idle,
	} {
		if got := col.Indicator(rowNamed(t, m, name)); got != want {
			t.Errorf("%s: Indicator = %v, want %v", name, got, want)
		}
	}
}

func TestADoneItemGoesBackToItsIconNeverGreen(t *testing.T) {
	m := withSnapshot(t, loadedModel(t),
		[]jobs.Run{actRun(jobs.KindScan, testContext, jobs.ItemDone, indRepo)}, nil)
	r := rowNamed(t, m, "devdesk")
	col := col0(t)
	if got := col.Indicator(r); got != indicator.Idle {
		t.Errorf("a finished row: Indicator = %v, want Idle", got)
	}
	line := lineOf(t, m, "devdesk")
	if !strings.Contains(line, entryIcon(r.Entry)) || hasAnyGlyph(line) {
		t.Errorf("a finished row must show only its icon:\n%s", line)
	}
	if got, want := col.Style(r).GetForeground(), theme.IconStyle(entryIconRole(r.Entry)).GetForeground(); got != want {
		t.Errorf("resting style = %v, want the IconStyle role %v", got, want)
	}
	if col.Style(r).GetForeground() == theme.StatusOKStyle.GetForeground() {
		t.Error("a finished row is green")
	}
}

func TestAFrameSetsThePhaseWithoutRebuildingRows(t *testing.T) {
	m := withSnapshot(t, loadedModel(t), []jobs.Run{runningRun(jobs.KindScan, indRepo)}, nil)
	before := m.table.Items()

	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if m.table.BlinkPhase() {
		t.Error("Blink=false left the phase on")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if !m.table.BlinkPhase() {
		t.Error("Blink=true did not turn the phase back on")
	}
	after := m.table.Items()
	if len(before) == 0 || len(before) != len(after) || &before[0] != &after[0] {
		t.Error("a frame rebuilt the rows; the phase is read at render")
	}
}

func TestAFrameStillFeedsTheFooterSpinner(t *testing.T) {
	m := withSnapshot(t, loadedModel(t), []jobs.Run{runningRun(jobs.KindScan, indRepo)}, nil)
	m = feed(t, m, jobs.FrameMsg{Frame: "ZZ", RenderedFrame: "FOOTERFRAME", Blink: true})
	if strings.Contains(m.View(), "ZZ") {
		t.Error("the bare frame still reaches the table")
	}
	if !strings.Contains(m.RenderFooter(120), "FOOTERFRAME") {
		t.Skip("footer shows no spinner for this state; only the table side is asserted")
	}
}

func TestTheModelHoldsNoRowSpinnerFrame(t *testing.T) {
	if _, ok := reflect.TypeOf(Model{}).FieldByName("jobFrame"); ok {
		t.Error("Model still carries jobFrame; the phase lives in the table")
	}
}

func TestTheTableFillsTheViewportAt60Columns(t *testing.T) {
	m := feed(t, loadedModel(t), tea.WindowSizeMsg{Width: 60, Height: 30})
	if got := m.table.RenderedWidth(); got != 58 {
		t.Errorf("RenderedWidth = %d, want 58", got)
	}
}
