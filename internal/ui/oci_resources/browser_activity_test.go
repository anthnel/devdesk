package ociresources

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-218 (A-09a2): the registry browser's tag table puts the activity
// column first, like the Images tab (DEVDESK-146) and the other OCI tables
// (DEVDESK-153). Column 0 is the dot; Registry, Tag, Updated, C, H, M, L are
// 1 to 7.

const (
	tagProd   = "registry.example.com/api:v2" // multiImageName of the prod tag "v2"
	tagProdV1 = "registry.example.com/api:v1"
)

func tagImageName(r tagRow) string {
	return multiImageName(r.tag.RegistryURL, r.tag.Repo, r.tag.Tag)
}

func tagRowNamed(t *testing.T, b *RegistryBrowser, name string) tagRow {
	t.Helper()
	for _, r := range b.tagTable.Items() {
		if tagImageName(r) == name {
			return r
		}
	}
	t.Fatalf("no tag row for %q", name)
	return tagRow{}
}

// resultsFrom is resultsModel starting from an existing model, so that the
// registry snapshot is already in when the browser opens.
func resultsFrom(t *testing.T, m Model) Model {
	t.Helper()
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	m = typeInto(t, m, "api")
	m = feed(t, m, testutil.Key("enter"))
	return feed(t, m,
		MultiRegistryTagsLoadedMsg{EntryKey: "prod", RegistryURL: "registry.example.com", Alias: "prod", Repo: "api", Tags: []string{"v1", "v2"}},
		MultiRegistryTagsLoadedMsg{EntryKey: "hub", RegistryURL: "docker.io", Alias: "hub", Repo: "api", Tags: []string{"latest"}},
	)
}

func withCounts(t *testing.T, m Model, name string) Model {
	t.Helper()
	m.registryBrowser.SetScanCache(map[string]cache.ImageScanEntry{
		name: {Critical: 3, High: 2, Medium: 1, ScannedAt: at(1)},
	})
	return m
}

func snapshot(runs []jobs.Run, fails []jobs.Failure) jobs.ChangedMsg {
	return jobs.ChangedMsg{Runs: runs, Failures: fails, Context: ociCtx}
}

// Visible columns, by title, the dot written "●"; a dropped column has width 0.
func visibleTagColumns(b *RegistryBrowser) []string {
	var out []string
	for _, c := range b.tagTable.Table().Columns() {
		if c.Width <= 0 {
			continue
		}
		title, _, _ := strings.Cut(c.Title, " ")
		if c.Title == "" {
			title = glyphDot
		}
		out = append(out, title)
	}
	return out
}

// 1. Rule 125: the first column is the untitled, fixed activity column.
func TestTagColumnZeroIsTheActivityColumn(t *testing.T) {
	checkActivityColumnZero(t, "newTagTable", newTagTable().Shapes())
	checkActivityColumnZero(t, "live browser", resultsModel(t).registryBrowser.tagTable.Shapes())
	if tagColumnTag != 2 || tagColumnUpdated != 3 {
		t.Errorf("tagColumnTag/tagColumnUpdated = %d/%d, want 2/3", tagColumnTag, tagColumnUpdated)
	}
	titles := []string{}
	for _, s := range newTagTable().Shapes() {
		titles = append(titles, s.Title)
	}
	if want := []string{"", "Registry", "Tag", "Updated", "C", "H", "M", "L"}; !slices.Equal(titles, want) {
		t.Errorf("column titles = %q, want %q", titles, want)
	}
}

// Q3: a tag at rest has an empty cell in the dot column.
func TestAnIdleTagHasAnEmptyDotCell(t *testing.T) {
	b := resultsModel(t).registryBrowser
	for _, r := range b.tagTable.Items() {
		if r.Activity.State != indicator.Idle {
			t.Errorf("%s: Activity.State = %v, want Idle", tagImageName(r), r.Activity.State)
		}
	}
	for i, row := range b.tagTable.Table().Rows() {
		if row[0] != "" {
			t.Errorf("row %d: dot cell = %q, want empty", i, row[0])
		}
	}
}

// 2. A scan that really runs: Running, the four count cells hold "…" in
// DimStyle without an escape sequence, and the counts return with the scan's end.
func TestARunningTagScanShowsTheDotAndDimEllipsisCounts(t *testing.T) {
	m := withCounts(t, resultsModel(t), tagProd)
	b := m.registryBrowser
	if got := rowFor(t, b, tagProd); got[4] != "3" {
		t.Fatalf("precondition: counts = %v", got[4:])
	}

	m = feed(t, m, snapshot([]jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, tagProd)}, nil))
	r := tagRowNamed(t, b, tagProd)
	if r.Activity.State != indicator.Running || !r.ScanRunning {
		t.Fatalf("Activity.State/ScanRunning = %v/%v, want Running/true", r.Activity.State, r.ScanRunning)
	}
	for col, cell := range rowFor(t, b, tagProd)[4:8] {
		if cell != "…" {
			t.Errorf("severity cell %d = %q, want …", col+4, cell)
		}
		if strings.Contains(cell, "\x1b") {
			t.Errorf("severity cell %d carries an escape sequence", col+4)
		}
	}
	for i, col := range tagColumns()[4:8] {
		if got, want := col.Style(r).GetForeground(), theme.DimStyle.GetForeground(); got != want {
			t.Errorf("column %d style = %v, want DimStyle %v", i+4, got, want)
		}
	}
	// Neighbours are untouched.
	if other := tagRowNamed(t, b, tagProdV1); other.Activity.State != indicator.Idle || other.ScanRunning {
		t.Errorf("another tag changed: %+v", other.Activity)
	}

	m = feed(t, m, snapshot(nil, nil))
	if r := tagRowNamed(t, b, tagProd); r.Activity.State != indicator.Idle || r.ScanRunning {
		t.Errorf("after the scan: Activity.State/ScanRunning = %v/%v, want Idle/false", r.Activity.State, r.ScanRunning)
	}
	if got := rowFor(t, b, tagProd); got[4] != "3" || got[5] != "2" || got[6] != "1" || got[7] != "0" {
		t.Errorf("severity cells = %v, want the counts back", got[4:])
	}
}

// 3. A queued scan keeps its counts and shows the queued dot.
func TestAQueuedTagScanKeepsItsCounts(t *testing.T) {
	m := withCounts(t, resultsModel(t), tagProd)
	b := m.registryBrowser
	feed(t, m, snapshot([]jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemQueued, tagProd)}, nil))

	r := tagRowNamed(t, b, tagProd)
	if r.Activity.State != indicator.Queued || r.ScanRunning {
		t.Errorf("Activity.State/ScanRunning = %v/%v, want Queued/false", r.Activity.State, r.ScanRunning)
	}
	if got := rowFor(t, b, tagProd); got[4] != "3" || got[5] != "2" || got[6] != "1" || got[7] != "0" {
		t.Errorf("severity cells = %v, want the counts kept while queued", got[4:])
	}
}

// A pull is shown by the dot but is not a scan: the counts stay.
func TestARunningTagPullShowsTheDotAndKeepsTheCounts(t *testing.T) {
	m := withCounts(t, resultsModel(t), tagProd)
	b := m.registryBrowser
	feed(t, m, snapshot([]jobs.Run{ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, tagProd)}, nil))

	r := tagRowNamed(t, b, tagProd)
	if r.Activity.State != indicator.Running || r.ScanRunning {
		t.Errorf("Activity.State/ScanRunning = %v/%v, want Running/false", r.Activity.State, r.ScanRunning)
	}
	if got := rowFor(t, b, tagProd); got[4] != "3" {
		t.Errorf("severity cells = %v, want the counts kept during a pull", got[4:])
	}
}

// A job of another context says nothing about this registry's tags.
func TestARunInAnotherContextLeavesTheTagIdle(t *testing.T) {
	m := resultsModel(t)
	feed(t, m, jobs.ChangedMsg{Runs: []jobs.Run{ociRun(jobs.KindScan, "other", jobs.ItemRunning, tagProd)}, Context: ociCtx})
	if r := tagRowNamed(t, m.registryBrowser, tagProd); r.Activity.State != indicator.Idle || r.ScanRunning {
		t.Errorf("a run of another context reached the row: %+v", r.Activity)
	}
}

// 4. A remembered failure of a scan or a pull makes the dot Failed (Q1).
func TestARememberedFailureMakesTheTagFailed(t *testing.T) {
	for _, kind := range []jobs.Kind{jobs.KindScan, jobs.KindPull} {
		t.Run(string(kind), func(t *testing.T) {
			m := withCounts(t, resultsModel(t), tagProd)
			b := m.registryBrowser
			feed(t, m, snapshot(nil, []jobs.Failure{ociFailure(kind, ociCtx, tagProd, at(2))}))

			r := tagRowNamed(t, b, tagProd)
			if r.Activity.State != indicator.Failed {
				t.Errorf("Activity.State = %v, want Failed", r.Activity.State)
			}
			if r.ScanRunning {
				t.Error("a failure counts as a scan running")
			}
			if got := rowFor(t, b, tagProd); got[4] != "3" {
				t.Errorf("severity cells = %v, want the counts kept after a failure", got[4:])
			}
			if other := tagRowNamed(t, b, tagProdV1); other.Activity.State != indicator.Idle {
				t.Errorf("the failure leaked to another tag: %v", other.Activity.State)
			}
		})
	}
}

// 5. Opening the browser during a scan: the row is Running as soon as the
// results arrive, which needs openMultiRegistryBrowser to pass the snapshot.
func TestOpeningTheBrowserDuringAScanShowsTheRowRunning(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, tagProd)}, nil)
	m = resultsFrom(t, m)
	b := m.registryBrowser

	r := tagRowNamed(t, b, tagProd)
	if r.Activity.State != indicator.Running || !r.ScanRunning {
		t.Fatalf("Activity.State/ScanRunning = %v/%v, want Running/true on arrival", r.Activity.State, r.ScanRunning)
	}
	if got := rowFor(t, b, tagProd); got[4] != "…" {
		t.Errorf("severity cells = %v, want …", got[4:])
	}
	// And a failure remembered before the browser opened.
	m = ociSnapshot(t, ociCtx, nil, []jobs.Failure{ociFailure(jobs.KindPull, ociCtx, tagProd, at(2))})
	m = resultsFrom(t, m)
	if r := tagRowNamed(t, m.registryBrowser, tagProd); r.Activity.State != indicator.Failed {
		t.Errorf("a remembered failure is not shown on arrival: %v", r.Activity.State)
	}
}

// 6. The router's blink phase reaches the open browser; the rendered dot follows.
func TestTheBlinkPhaseReachesTheBrowserTable(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, tagProd)}, nil)
	m = resultsFrom(t, m)
	b := m.registryBrowser

	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if !b.tagTable.BlinkPhase() {
		t.Fatal("BlinkPhase() = false after FrameMsg{Blink: true}")
	}
	if line := renderedRow(t, m.View(), "v2"); !strings.Contains(line, glyphDot) {
		t.Errorf("the running row lacks %q:\n%s", glyphDot, line)
	}

	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if b.tagTable.BlinkPhase() {
		t.Error("BlinkPhase() = true after FrameMsg{Blink: false}")
	}
	line := renderedRow(t, m.View(), "v2")
	if !strings.Contains(line, glyphHollow) || strings.Contains(line, glyphDot) {
		t.Errorf("the off phase should render %q only:\n%s", glyphHollow, line)
	}
}

// 7. No row chain any more: a spinner tick animates nothing on the rows. Only a
// search keeps the browser's own spinner (the footer's loading state).
func TestASpinnerTickDoesNotTouchTheRows(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	b.SetJobs([]jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, tagProd)}, nil, ociCtx)
	if b.IsSearching() {
		t.Fatal("precondition: the browser is searching")
	}
	rowsBefore := b.tagTable.Items()
	cellsBefore := b.tagTable.Table().Rows()

	next, cmd := b.Update(spinner.TickMsg{})
	if cmd != nil {
		t.Error("a tick with a scan running and no search returned a Cmd: the chain is still alive")
	}
	if !reflect.DeepEqual(next.tagTable.Items(), rowsBefore) || !reflect.DeepEqual(next.tagTable.Table().Rows(), cellsBefore) {
		t.Error("a spinner tick changed the rows")
	}

	b.pendingSearches = 1
	if _, cmd := b.Update(spinner.TickMsg{}); cmd == nil {
		t.Error("a tick during a search returned no Cmd: the search spinner stopped")
	}
}

// The search spinner feeds the footer frame, and the router's frame does not
// overwrite it while a search runs.
func TestTheSearchSpinnerFeedsTheFooterAndTheRouterFrameDoesNotOverwriteIt(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	b.pendingSearches = 1

	m = feed(t, m, spinner.TickMsg{})
	info := ansi.Strip(ociInfoLine(m, 200))
	if !strings.Contains(info, "Searching registries...") {
		t.Fatalf("the footer lacks the search label:\n%s", info)
	}
	frame := strings.TrimSpace(ansi.Strip(b.spinner.View()))
	if frame == "" || !strings.Contains(info, frame) {
		t.Errorf("the footer lacks the search spinner frame %q:\n%s", frame, info)
	}

	m = feed(t, m, jobs.FrameMsg{Frame: "ZZ", RenderedFrame: "ZZ", Blink: true})
	if info := ansi.Strip(ociInfoLine(m, 200)); strings.Contains(info, "ZZ") || !strings.Contains(info, frame) {
		t.Errorf("the router's frame overwrote the search spinner:\n%s", info)
	}
}

// 8. Rule 116 and the column drop order, computed with datatable/widths.go.
func TestTagColumnsDropInTheNewOrder(t *testing.T) {
	tests := []struct {
		width int
		want  []string
	}{
		{60, []string{"●", "Registry", "Tag"}},
		{65, []string{"●", "Registry", "Tag"}},
		{66, []string{"●", "Registry", "Tag", "C"}},
		{71, []string{"●", "Registry", "Tag", "C"}},
		{72, []string{"●", "Registry", "Tag", "C", "H"}},
		{78, []string{"●", "Registry", "Tag", "C", "H", "M"}},
		{83, []string{"●", "Registry", "Tag", "C", "H", "M"}},
		{84, []string{"●", "Registry", "Tag", "C", "H", "M", "L"}},
		{101, []string{"●", "Registry", "Tag", "C", "H", "M", "L"}},
		{102, []string{"●", "Registry", "Tag", "Updated", "C", "H", "M", "L"}},
		{200, []string{"●", "Registry", "Tag", "Updated", "C", "H", "M", "L"}},
	}
	for _, tt := range tests {
		m := feed(t, resultsModel(t), testutil.Resize(tt.width, 40))
		b := m.registryBrowser
		if got := visibleTagColumns(b); !slices.Equal(got, tt.want) {
			t.Errorf("width %d: visible columns = %v, want %v", tt.width, got, tt.want)
		}
		if got, want := b.tagTable.RenderedWidth(), tt.width-2; got != want {
			t.Errorf("width %d: the line spans %d, want %d (Rule 116)", tt.width, got, want)
		}
	}
}

// 9. The guard reaches the browser's table, open or not.
func TestActivityTablesListsTheRegistryBrowser(t *testing.T) {
	closed := feed(t, loadedModel(t), testutil.Resize(60, 30))
	tbl := closed.ActivityTables()["oci/registry-browser"]
	if tbl == nil {
		t.Fatal("ActivityTables lacks oci/registry-browser with the browser closed")
	}
	checkActivityColumnZero(t, "closed browser", tbl.Shapes())
	if got, want := tbl.RenderedWidth(), 58; got != want {
		t.Errorf("closed: the line spans %d at 60 columns, want %d", got, want)
	}

	open := feed(t, closed, testutil.Key(keymap.Browser))
	if open.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	tbl = open.ActivityTables()["oci/registry-browser"]
	if tbl == nil {
		t.Fatal("ActivityTables lacks oci/registry-browser with the browser open")
	}
	if got, want := tbl.RenderedWidth(), 58; got != want {
		t.Errorf("open: the line spans %d at 60 columns, want %d", got, want)
	}
}

// 10. The footer says what the tag under the cursor is doing, unless a search
// runs: the loading label comes first and carries no row detail.
func TestTheBrowserFooterDescribesTheSelectedTag(t *testing.T) {
	m := resultsModel(t)
	b := m.registryBrowser
	row, ok := b.tagTable.Selected()
	if !ok {
		t.Fatal("no tag is selected")
	}
	name := tagImageName(row)
	m = feed(t, m, snapshot([]jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, name)}, nil))

	info := ansi.Strip(ociInfoLine(m, 200))
	if !strings.Contains(info, glyphDot) || !strings.Contains(info, name) {
		t.Errorf("the footer lacks %q and the target %q:\n%s", glyphDot, name, info)
	}

	b.pendingSearches = 1
	info = ansi.Strip(ociInfoLine(m, 200))
	if !strings.Contains(info, "Searching registries...") {
		t.Errorf("the search label is not first:\n%s", info)
	}
	if strings.Contains(info, name) {
		t.Errorf("the row detail sits beside the search label:\n%s", info)
	}
}

// 11. Rule 114: the help carries the legend and says what the tag dot is.
func TestBrowserHelpCarriesTheLegendAndTheTagDot(t *testing.T) {
	content := resultsModel(t).GetHelpContent()
	legend := indicator.Legend()
	var browser string
	haveLegend := false
	for _, s := range content.Sections {
		switch s.Title {
		case legend.Title:
			haveLegend = s.Body == legend.Body
		case "Registry Browser":
			browser = s.Body
		}
	}
	if !haveLegend {
		t.Errorf("the help lacks the %q section, verbatim", legend.Title)
	}
	lower := strings.ToLower(browser)
	if !strings.Contains(lower, "dot") || !strings.Contains(lower, "first column") {
		t.Errorf("the Registry Browser help does not explain the dot in the first column of the tags:\n%s", browser)
	}
}
