package security

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-145 step 2: column 0 of the :sec inventory is the activity column
// (Rule 125, "The activity column"). The blink phase is read at render.

const (
	indImage = "nexus/api:1.4"
	indRepo  = "/home/dev/workspaces/devdesk"
)

func inventoryLine(t *testing.T, m Model, name string) string {
	t.Helper()
	short := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		short = name[i+1:]
	}
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, short) {
			return l
		}
	}
	t.Fatalf("no rendered line holds %q:\n%s", short, m.View())
	return ""
}

func rowByName(t *testing.T, m Model, name string) scanTarget {
	t.Helper()
	for _, r := range m.inventory.Items() {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no row named %q", name)
	return scanTarget{}
}

func col0(t *testing.T) datatable.Column[scanTarget] {
	t.Helper()
	c := inventoryColumns(false, false)[0]
	if c.Indicator == nil {
		t.Fatal("column 0 has no Indicator")
	}
	return c
}

func TestColumnZeroIsTheActivityColumn(t *testing.T) {
	for _, tc := range []struct{ ci, cfg bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		c := inventoryColumns(tc.ci, tc.cfg)[0]
		if c.Title != "" {
			t.Errorf("ci=%v cfg=%v: column 0 titled %q, want untitled", tc.ci, tc.cfg, c.Title)
		}
		if c.Sizing != datatable.SizingFixed || c.MinWidth != datatable.IconColumnWidth {
			t.Errorf("column 0 sizing/min = %v/%d, want SizingFixed/%d", c.Sizing, c.MinWidth, datatable.IconColumnWidth)
		}
		if c.Indicator == nil {
			t.Errorf("ci=%v cfg=%v: column 0 is not built by datatable.IndicatorColumn", tc.ci, tc.cfg)
		}
		if c.Less != nil || c.Search != nil {
			t.Errorf("column 0 declares Less=%v Search=%v, want neither", c.Less != nil, c.Search != nil)
		}
	}
	m := inventoryModel(t, inventoryFixtures()...)
	if shapes := m.inventory.Shapes(); len(shapes) == 0 || !shapes[0].Indicator {
		t.Errorf("the live inventory has no activity column 0: %+v", shapes)
	}
}

func TestTheViewExposesItsTableToTheActivityGuard(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	av, ok := any(m).(interface {
		ActivityTables() map[string]datatable.ActivityTable
	})
	if !ok {
		t.Fatal("Model does not expose ActivityTables()")
	}
	tables := av.ActivityTables()
	tbl, ok := tables["sec/inventory"]
	if !ok || len(tables) != 1 {
		t.Fatalf("ActivityTables() keys = %v, want exactly [sec/inventory]", tables)
	}
	if shapes := tbl.Shapes(); len(shapes) == 0 || !shapes[0].Indicator {
		t.Errorf("the reported table has no activity column 0: %+v", shapes)
	}
}

func TestEachActivityStateRendersItsGlyphOnItsRow(t *testing.T) {
	queued := secRun(jobs.KindScan, secCtx, jobs.ItemQueued, indRepo)
	running := secRun(jobs.KindScan, secCtx, jobs.ItemRunning, indRepo)
	failure := secFailure(jobs.KindScan, secCtx, indRepo, time.Now())
	tests := []struct {
		name  string
		runs  []jobs.Run
		fails []jobs.Failure
		blink bool
		state indicator.State
	}{
		{"running, phase on", []jobs.Run{running}, nil, true, indicator.Running},
		{"running, phase off", []jobs.Run{running}, nil, false, indicator.Running},
		{"queued, phase on", []jobs.Run{queued}, nil, true, indicator.Queued},
		{"queued, phase off", []jobs.Run{queued}, nil, false, indicator.Queued},
		{"failed, phase on", nil, []jobs.Failure{failure}, true, indicator.Failed},
		{"failed, phase off", nil, []jobs.Failure{failure}, false, indicator.Failed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := inventoryModel(t, inventoryFixtures()...)
			m = feed(t, m,
				jobs.ChangedMsg{Runs: tc.runs, Failures: tc.fails, Context: secCtx},
				jobs.FrameMsg{Blink: tc.blink})
			line := inventoryLine(t, m, indRepo)
			want := indicator.Glyph(tc.state, tc.blink)
			if !strings.Contains(line, want) {
				t.Errorf("row lacks %q:\n%s", want, line)
			}
			if strings.Contains(line, theme.IconWorkspace) {
				t.Errorf("the glyph shares the row with the resting icon:\n%s", line)
			}
			if other := inventoryLine(t, m, indImage); hasAnyGlyph(other) {
				t.Errorf("an untouched row shows an activity glyph:\n%s", other)
			}
		})
	}
}

func hasAnyGlyph(line string) bool {
	for _, s := range indicator.States() {
		if g := indicator.Glyph(s, true); g != "" && strings.Contains(line, g) {
			return true
		}
	}
	return strings.Contains(line, indicator.Glyph(indicator.Running, false))
}

func TestAnIdleRowShowsItsKindIconNeverGreen(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	col := col0(t)
	for name, icon := range map[string]string{indRepo: theme.IconWorkspace, indImage: theme.IconDocker} {
		r := rowByName(t, m, name)
		if got := col.Indicator(r); got != indicator.Idle {
			t.Errorf("%s: Indicator = %v, want Idle", name, got)
		}
		if got := col.Cell(r); got != icon {
			t.Errorf("%s: resting cell = %q, want %q", name, got, icon)
		}
		if got, want := col.Style(r).GetForeground(), theme.IconStyle(r.kindIconRole()).GetForeground(); got != want {
			t.Errorf("%s: resting style = %v, want the IconStyle role %v", name, got, want)
		}
		if col.Style(r).GetForeground() == theme.StatusOKStyle.GetForeground() {
			t.Errorf("%s: an idle icon is green", name)
		}
	}
}

func TestADoneItemGoesBackToItsIcon(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = withJobs(t, m, secRun(jobs.KindScan, secCtx, jobs.ItemDone, indRepo))
	if got := col0(t).Indicator(rowByName(t, m, indRepo)); got != indicator.Idle {
		t.Errorf("a finished row: Indicator = %v, want Idle", got)
	}
	if line := inventoryLine(t, m, indRepo); hasAnyGlyph(line) {
		t.Errorf("a finished row shows an activity glyph:\n%s", line)
	}
}

func TestAQueuedRowBlinksNotAndKeepsItsPreviousScannedCell(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = feed(t, m,
		jobs.ChangedMsg{Runs: []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemQueued, indRepo)}, Context: secCtx},
		jobs.FrameMsg{Blink: true})
	r := rowByName(t, m, indRepo)
	if r.ScanRunning {
		t.Error("a queued scan holds the Scanned cell")
	}
	if got, want := inventoryScannedCell(r), theme.TimeAgo(r.ScannedAt); got != want {
		t.Errorf("queued Scanned cell = %q, want the previous %q", got, want)
	}
}

func TestARunningScanHoldsTheScannedCellDim(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = feed(t, m, jobs.ChangedMsg{
		Runs: []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemRunning, indRepo)}, Context: secCtx})
	r := rowByName(t, m, indRepo)
	if got := inventoryScannedCell(r); got != scanningText {
		t.Errorf("Scanned cell = %q, want %q", got, scanningText)
	}
	if got, want := inventoryScannedStyle(r).GetForeground(), theme.DimStyle.GetForeground(); got != want {
		t.Errorf("Scanned style = %v, want DimStyle %v", got, want)
	}
	if strings.Contains(inventoryScannedCell(r), "ago") {
		t.Error("a running scan still shows its previous age")
	}
}

// A reload mid-scan keeps the dot and the text: the state is derived.
func TestAReloadKeepsTheDotAndTheScanningText(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = feed(t, m,
		jobs.ChangedMsg{Runs: []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemRunning, indRepo)}, Context: secCtx},
		jobs.FrameMsg{Blink: true},
		InventoryLoadedMsg{Targets: inventoryFixtures()})
	line := inventoryLine(t, m, indRepo)
	if !strings.Contains(line, indicator.Glyph(indicator.Running, true)) || !strings.Contains(line, scanningText) {
		t.Errorf("a reload lost the in-flight marker:\n%s", line)
	}
}

func TestTheInventoryFillsTheViewportAt60Columns(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = feed(t, m, tea.WindowSizeMsg{Width: 60, Height: 30})
	if got := m.inventory.RenderedWidth(); got != 58 {
		t.Errorf("RenderedWidth = %d, want 58 (Rule 116)", got)
	}
}

func TestTheSelectedRunningRowStartsWithTheDot(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = feed(t, m, tea.WindowSizeMsg{Width: 60, Height: 30})
	// Critical-descending: the image (3 critical) is the first row.
	m = feed(t, m,
		jobs.ChangedMsg{Runs: []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemRunning, indImage)}, Context: secCtx},
		jobs.FrameMsg{Blink: true})
	dot := indicator.Glyph(indicator.Running, true)
	for _, l := range strings.Split(m.View(), "\n") {
		if strings.Contains(l, "api:1.4") {
			if !strings.Contains(l, dot) || strings.Index(l, dot) > strings.Index(l, "api") {
				t.Errorf("the dot is not the first visible cell:\n%s", l)
			}
			return
		}
	}
	t.Fatal("selected row not found")
}

func TestARowIsRestampedOnlyBySnapshotsNeverByFrames(t *testing.T) {
	m := inventoryModel(t, inventoryFixtures()...)
	m = withJobs(t, m, scanningRun(indRepo))
	before := reflect.ValueOf(m.inventory.Items()).Pointer()
	m = feed(t, m, jobs.FrameMsg{Blink: false}, jobs.FrameMsg{Blink: true})
	if reflect.ValueOf(m.inventory.Items()).Pointer() != before {
		t.Error("a frame restamped the rows")
	}
}
