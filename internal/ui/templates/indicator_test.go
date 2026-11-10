package templates

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/template"
	"github.com/anthnel/devdesk/internal/testutil/colorprofile"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-149 step 2: column 0 of the templates table is the activity column
// (Rule 125). A row answers for two registry targets: its slug (sync) and its
// scan directory (scan). The phase is read at render, never rebuilt per frame.

const indCtx = "ctx"

// indFixture is a catalog with one entry of each source kind. The names are
// sorted API, Img, Web.
func indFixture(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	api := entry("api", "API")
	web := template.Entry{Slug: "web", Name: "Web", Source: template.Source{Kind: template.KindLocal, Path: "/srv/web-src"}}
	img := template.Entry{Slug: "img", Name: "Img", Source: template.Source{Kind: template.KindOCI, URL: "registry.example.com/", Path: "acme/img", Ref: "1.0"}}
	m := opened(t, api, web, img)
	return send(t, m, testutil.Resize(120, 30))
}

func indRun(kind jobs.Kind, state jobs.ItemState, target string) jobs.Run {
	run := jobs.NewRun(kind, command.ViewTemplates, indCtx, "x", target)
	run.Items[0].State = state
	return run
}

func indFailure(kind jobs.Kind, target, detail string) jobs.Failure {
	return jobs.Failure{
		FailureKey: jobs.FailureKey{Kind: kind, Target: jobs.TargetKey(target), Context: indCtx},
		Display:    target, Detail: detail, At: time.Now(),
	}
}

func snapshot(t *testing.T, m Model, runs []jobs.Run, failures []jobs.Failure) Model {
	t.Helper()
	return send(t, m, jobs.ChangedMsg{Runs: runs, Failures: failures, Context: indCtx})
}

func scanDirOf(t *testing.T, slug string) string {
	t.Helper()
	dir, err := template.ScanDir(slug)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

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

func hasAnyGlyph(line string) bool {
	for _, s := range indicator.States() {
		if g := indicator.Glyph(s, true); g != "" && strings.Contains(line, g) {
			return true
		}
	}
	return strings.Contains(line, indicator.Glyph(indicator.Running, false))
}

// fgPrefix is the escape sequence that sets a style's foreground, as rendered,
// without its background (a cell carries the row's, see fgOnly).
func fgPrefix(s lipgloss.Style) string {
	out := s.Render("x")
	return fgOnly(out[:strings.Index(out, "x")])
}

// fgOnly reduces a rendered line to what a style's foreground says: the table
// composites every cell's colour with the row background (and bold on the
// selected row) into one escape sequence, so the background and bold
// parameters are dropped before the foreground sequence is compared.
func fgOnly(line string) string {
	line = regexp.MustCompile(`;48;2;\d+;\d+;\d+m`).ReplaceAllString(line, "m")
	return strings.ReplaceAll(line, "\x1b[1;", "\x1b[")
}

func TestColumnZeroIsTheActivityColumn(t *testing.T) {
	m := indFixture(t)
	shapes := m.table.Shapes()
	if len(shapes) == 0 {
		t.Fatal("no column")
	}
	c := shapes[0]
	if !c.Indicator {
		t.Error("column 0 is not built by datatable.IndicatorColumn (no Indicator)")
	}
	if c.Title != "" {
		t.Errorf("column 0 titled %q, want untitled", c.Title)
	}
	if c.Sizing != datatable.SizingFixed || c.MinWidth != datatable.IconColumnWidth {
		t.Errorf("column 0 sizing/min = %v/%d, want SizingFixed/%d", c.Sizing, c.MinWidth, datatable.IconColumnWidth)
	}
	if c.Less || c.Search {
		t.Errorf("column 0 declares Less=%v Search=%v, want neither", c.Less, c.Search)
	}
}

func TestAnIdleRowShowsItsSourceKindGlyph(t *testing.T) {
	m := indFixture(t)
	for name, kind := range map[string]template.Kind{"API": template.KindGit, "Web": template.KindLocal, "Img": template.KindOCI} {
		line := lineOf(t, m, name)
		if !strings.Contains(line, kindGlyph(kind)) {
			t.Errorf("%s: the rest icon %q is not rendered:\n%s", name, kindGlyph(kind), line)
		}
		if hasAnyGlyph(line) {
			t.Errorf("%s: an idle row shows an activity glyph:\n%s", name, line)
		}
	}
}

func TestEachActivityStateRendersItsGlyphOnItsRow(t *testing.T) {
	// The scan directory is computed under the isolated HOME of indFixture, so
	// the cases are built after it.
	tests := []struct {
		name  string
		build func(t *testing.T) ([]jobs.Run, []jobs.Failure)
		blink bool
		want  string
	}{
		{"sync queued, phase on", func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{indRun(jobs.KindSync, jobs.ItemQueued, "api")}, nil
		}, true, indicator.Glyph(indicator.Queued, true)},
		{"sync running, phase on", func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, "api")}, nil
		}, true, indicator.Glyph(indicator.Running, true)},
		{"sync running, phase off", func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, "api")}, nil
		}, false, indicator.Glyph(indicator.Running, false)},
		{"scan running on the scan directory, phase on", func(t *testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{indRun(jobs.KindScan, jobs.ItemRunning, scanDirOf(t, "api"))}, nil
		}, true, indicator.Glyph(indicator.Running, true)},
		{"scan running on the scan directory, phase off", func(t *testing.T) ([]jobs.Run, []jobs.Failure) {
			return []jobs.Run{indRun(jobs.KindScan, jobs.ItemRunning, scanDirOf(t, "api"))}, nil
		}, false, indicator.Glyph(indicator.Running, false)},
		{"sync failed, phase on", func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return nil, []jobs.Failure{indFailure(jobs.KindSync, "api", "sync failed — check logs")}
		}, true, indicator.Glyph(indicator.Failed, true)},
		{"sync failed, phase off", func(*testing.T) ([]jobs.Run, []jobs.Failure) {
			return nil, []jobs.Failure{indFailure(jobs.KindSync, "api", "sync failed — check logs")}
		}, false, indicator.Glyph(indicator.Failed, false)},
		{"scan failed, phase on", func(t *testing.T) ([]jobs.Run, []jobs.Failure) {
			return nil, []jobs.Failure{indFailure(jobs.KindScan, scanDirOf(t, "api"), "scan failed — check logs")}
		}, true, indicator.Glyph(indicator.Failed, true)},
		{"scan failed, phase off", func(t *testing.T) ([]jobs.Run, []jobs.Failure) {
			return nil, []jobs.Failure{indFailure(jobs.KindScan, scanDirOf(t, "api"), "scan failed — check logs")}
		}, false, indicator.Glyph(indicator.Failed, false)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := indFixture(t)
			runs, failures := tc.build(t)
			m = snapshot(t, m, runs, failures)
			m = send(t, m, jobs.FrameMsg{Blink: tc.blink})
			line := lineOf(t, m, "API")
			if !strings.Contains(line, tc.want) {
				t.Errorf("row lacks %q:\n%s", tc.want, line)
			}
			if strings.Contains(line, kindGlyph(template.KindGit)) {
				t.Errorf("the activity glyph shares the cell with the rest icon:\n%s", line)
			}
			for _, other := range []string{"Web", "Img"} {
				if l := lineOf(t, m, other); hasAnyGlyph(l) {
					t.Errorf("an untouched row (%s) shows an activity glyph:\n%s", other, l)
				}
			}
		})
	}
}

func TestAFailedRowIsFixedAndFailureColoured(t *testing.T) {
	colorprofile.TrueColor(t)
	m := indFixture(t)
	// Web is not the selected row (the cursor opens on the first one, API).
	m = snapshot(t, m, nil, []jobs.Failure{indFailure(jobs.KindSync, "web", "sync failed — check logs")})
	want := fgPrefix(lipgloss.NewStyle().Foreground(theme.ColorActivityFailed)) + indicator.Glyph(indicator.Failed, true)
	for _, blink := range []bool{true, false} {
		m = send(t, m, jobs.FrameMsg{Blink: blink})
		if line := lineOf(t, m, "Web"); !strings.Contains(fgOnly(line), want) {
			t.Errorf("blink=%v: the failed glyph is not in the failure colour:\n%q", blink, line)
		}
	}
}

func TestOnlyTheRowsOwnSyncOrScanCounts(t *testing.T) {
	m := indFixture(t)
	rest := kindGlyph(template.KindGit)
	tests := []struct {
		name     string
		failures []jobs.Failure
		runs     []jobs.Run
	}{
		{"failure in another context", []jobs.Failure{{
			FailureKey: jobs.FailureKey{Kind: jobs.KindSync, Target: "api", Context: "other"}, Detail: "x", At: time.Now(),
		}}, nil},
		{"a delete failure on the slug", []jobs.Failure{indFailure(jobs.KindDelete, "api", "x")}, nil},
		{"a pull failure on the slug", []jobs.Failure{indFailure(jobs.KindPull, "api", "x")}, nil},
		{"a scan item on the slug (wrong kind for that target)", nil, []jobs.Run{indRun(jobs.KindScan, jobs.ItemRunning, "api")}},
		{"a sync item on the scan directory (wrong kind for that target)", nil, []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, scanDirOf(t, "api"))}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := lineOf(t, snapshot(t, m, tc.runs, tc.failures), "API")
			if !strings.Contains(line, rest) || hasAnyGlyph(line) {
				t.Errorf("the row must stay at rest (%q):\n%s", rest, line)
			}
		})
	}
}

func TestADoneSyncGoesBackToTheRestIcon(t *testing.T) {
	m := indFixture(t)
	m = snapshot(t, m, []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, "api")}, nil)
	m = send(t, m, jobs.FrameMsg{Blink: true})
	if !strings.Contains(lineOf(t, m, "API"), indicator.Glyph(indicator.Running, true)) {
		t.Fatal("precondition: the row does not blink while the sync runs")
	}
	m = snapshot(t, m, []jobs.Run{indRun(jobs.KindSync, jobs.ItemDone, "api")}, nil)
	line := lineOf(t, m, "API")
	if !strings.Contains(line, kindGlyph(template.KindGit)) || hasAnyGlyph(line) {
		t.Errorf("a finished sync must leave only the rest icon:\n%s", line)
	}
}

func TestANewRunOnAFailedSlugReplacesTheFailureGlyph(t *testing.T) {
	m := indFixture(t)
	m = snapshot(t, m, nil, []jobs.Failure{indFailure(jobs.KindSync, "api", "sync failed — check logs")})
	if !strings.Contains(lineOf(t, m, "API"), indicator.Glyph(indicator.Failed, true)) {
		t.Fatal("precondition: the failure is not shown")
	}
	// The registry clears the failure at admission: the snapshot has none.
	m = snapshot(t, m, []jobs.Run{indRun(jobs.KindSync, jobs.ItemQueued, "api")}, nil)
	if !strings.Contains(lineOf(t, m, "API"), indicator.Glyph(indicator.Queued, true)) {
		t.Errorf("queued after a failure:\n%s", lineOf(t, m, "API"))
	}
	m = snapshot(t, m, []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, "api")}, nil)
	m = send(t, m, jobs.FrameMsg{Blink: true})
	line := lineOf(t, m, "API")
	if !strings.Contains(line, indicator.Glyph(indicator.Running, true)) || strings.Contains(line, indicator.Glyph(indicator.Failed, true)) {
		t.Errorf("running after a failure:\n%s", line)
	}
}

func TestOnlyTheRowThatSyncsChanges(t *testing.T) {
	m := indFixture(t)
	before := lineOf(t, m, "Web")
	m = snapshot(t, m, []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, "api")}, nil)
	if after := lineOf(t, m, "Web"); after != before {
		t.Errorf("another row changed:\nbefore %q\nafter  %q", before, after)
	}
	if !hasAnyGlyph(lineOf(t, m, "API")) {
		t.Error("the syncing row shows no activity glyph")
	}
}

func TestAFrameSetsThePhaseWithoutRebuildingRows(t *testing.T) {
	m := indFixture(t)
	m = snapshot(t, m, []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, "api")}, nil)
	m = send(t, m, jobs.FrameMsg{Blink: true})
	if got := lineOf(t, m, "API"); !strings.Contains(got, indicator.Glyph(indicator.Running, true)) {
		t.Fatalf("phase on: %q", got)
	}
	before := append([]row(nil), m.table.Items()...)

	m = send(t, m, jobs.FrameMsg{Blink: false})
	if m.table.BlinkPhase() {
		t.Error("Blink=false left the phase on")
	}
	if got := lineOf(t, m, "API"); !strings.Contains(got, indicator.Glyph(indicator.Running, false)) {
		t.Errorf("phase off: %q", got)
	}
	if !reflect.DeepEqual(before, m.table.Items()) {
		t.Error("a frame changed the rows; the phase is read at render")
	}
}

func TestStartingASyncOrAScanNamesTheTemplateNotTheTarget(t *testing.T) {
	m := withScanners(indFixture(t), true, true)

	sync, ok := testutil.MsgOf[jobs.StartMsg](m.syncStart(entry("api", "API")))
	if !ok {
		t.Fatal("syncStart produced no jobs.StartMsg")
	}
	if got := sync.Run.Items[0].Display; got != "API" {
		t.Errorf("sync item Display = %q, want the template name %q", got, "API")
	}

	scanStart, _ := launched(t, m)
	if got := scanStart.Run.Items[0].Display; got != "API" {
		t.Errorf("scan item Display = %q, want the template name %q", got, "API")
	}
}

func TestTheViewExposesItsTableToTheActivityGuard(t *testing.T) {
	m := indFixture(t)
	av, ok := any(m).(interface {
		ActivityTables() map[string]datatable.ActivityTable
	})
	if !ok {
		t.Fatal("Model does not expose ActivityTables()")
	}
	tables := av.ActivityTables()
	tbl, ok := tables["templates"]
	if !ok || len(tables) != 1 {
		t.Fatalf("ActivityTables() keys = %v, want exactly [templates]", tables)
	}
	if shapes := tbl.Shapes(); len(shapes) == 0 || !shapes[0].Indicator {
		t.Errorf("the reported templates table has no activity column 0: %+v", shapes)
	}
}

func TestTheTableFillsTheViewportAt60Columns(t *testing.T) {
	m := send(t, indFixture(t), testutil.Resize(60, 30))
	if got := m.table.RenderedWidth(); got != 58 {
		t.Errorf("RenderedWidth = %d, want 58", got)
	}
}

func TestTheTableFillsTheViewportWithLongSources(t *testing.T) {
	long := template.Entry{Slug: "long", Name: "A very long template name indeed", Tags: []string{"alpha", "beta", "gamma"},
		Source: template.Source{Kind: template.KindGit, URL: "https://git.example.com/" + strings.Repeat("deep/", 12) + "repo.git", Ref: "release/2026"}}
	t.Setenv("HOME", t.TempDir())
	m := opened(t, long, entry("api", "API"))
	for _, width := range []int{60, 80, 120} {
		m = send(t, m, testutil.Resize(width, 30))
		if got := m.table.RenderedWidth(); got != width-2 {
			t.Errorf("width %d: RenderedWidth = %d, want %d", width, got, width-2)
		}
	}
}

func TestThePickerBlinksToo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := NewForSelection(nil, nil, "Pick a template")
	m.path = catalogWith(t, entry("api", "API"))
	m.cache = template.NewCacheAt(t.TempDir())
	m = send(t, m, testutil.Resize(120, 30))
	m = drive(t, m, testutil.Msgs(loadCatalogCmd(m.path))[0])

	m = snapshot(t, m, []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, "api")}, nil)
	m = send(t, m, jobs.FrameMsg{Blink: true})
	if line := lineOf(t, m, "API"); !strings.Contains(line, indicator.Glyph(indicator.Running, true)) {
		t.Errorf("the picker row does not blink:\n%s", line)
	}
}

// ── Q1-B: the Synced column says "syncing" while a sync of the row runs ──────

func TestTheSyncedColumnSaysSyncingWhileASyncRuns(t *testing.T) {
	colorprofile.TrueColor(t)
	syncedAt := time.Now().Add(-3 * 24 * time.Hour)
	setup := func(t *testing.T) Model {
		m := indFixture(t)
		m.syncedAt = map[string]time.Time{"api": syncedAt, "web": syncedAt, "img": syncedAt}
		m.rebuild()
		return m
	}
	age := theme.TimeAgo(syncedAt)

	t.Run("running sync: plain text in DimStyle", func(t *testing.T) {
		m := snapshot(t, setup(t), []jobs.Run{indRun(jobs.KindSync, jobs.ItemRunning, "api")}, nil)
		line := lineOf(t, m, "API")
		if !strings.Contains(line, "syncing") {
			t.Fatalf("the Synced cell does not say syncing:\n%s", line)
		}
		if strings.Contains(line, age) {
			t.Errorf("the stale age %q is still shown while syncing:\n%s", age, line)
		}
		if !strings.Contains(fgOnly(line), fgPrefix(theme.DimStyle)+"syncing") {
			t.Errorf("syncing is not dim:\n%q", line)
		}
		if other := lineOf(t, m, "Web"); strings.Contains(other, "syncing") || !strings.Contains(other, age) {
			t.Errorf("another row changed its Synced cell:\n%s", other)
		}
	})
	t.Run("queued sync: the age stays", func(t *testing.T) {
		m := snapshot(t, setup(t), []jobs.Run{indRun(jobs.KindSync, jobs.ItemQueued, "api")}, nil)
		line := lineOf(t, m, "API")
		if strings.Contains(line, "syncing") || !strings.Contains(line, age) {
			t.Errorf("a queued sync must leave the age:\n%s", line)
		}
	})
	t.Run("running scan: the age stays", func(t *testing.T) {
		m := setup(t)
		m = snapshot(t, m, []jobs.Run{indRun(jobs.KindScan, jobs.ItemRunning, scanDirOf(t, "api"))}, nil)
		line := lineOf(t, m, "API")
		if strings.Contains(line, "syncing") || !strings.Contains(line, age) {
			t.Errorf("a scan must leave the age:\n%s", line)
		}
	})
	t.Run("sync done: the age is back", func(t *testing.T) {
		m := snapshot(t, setup(t), []jobs.Run{indRun(jobs.KindSync, jobs.ItemDone, "api")}, nil)
		if line := lineOf(t, m, "API"); strings.Contains(line, "syncing") {
			t.Errorf("syncing outlived the sync:\n%s", line)
		}
	})
}
