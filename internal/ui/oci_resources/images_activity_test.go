package ociresources

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-146 step 2: column 0 of the Images table is the activity column
// (Rule 125, "The activity column"). The blink phase is read at render and no
// cell carries a spinner frame any more.

const (
	imgRef      = "api:v1"                 // scanned at(1), listed
	imgNewRef   = "reg.example.com/new:v1" // not local: a pull adds a placeholder row
	glyphDot    = "●"
	glyphHollow = "○"
)

func imageRowByName(t *testing.T, m Model, raw string) imageRow {
	t.Helper()
	for _, r := range m.imageTable.Items() {
		if r.RawName == raw {
			return r
		}
	}
	t.Fatalf("no image row named %q", raw)
	return imageRow{}
}

func imageColByTitle(t *testing.T, title string) datatable.Column[imageRow] {
	t.Helper()
	for _, c := range imageColumns(false) {
		if c.Title == title {
			return c
		}
	}
	t.Fatalf("no column titled %q", title)
	return datatable.Column[imageRow]{}
}

func imageLine(t *testing.T, m Model, name string) string {
	t.Helper()
	return renderedRow(t, m.View(), name)
}

func TestImagesColumnZeroIsTheActivityColumn(t *testing.T) {
	for _, cfg := range []bool{false, true} {
		cols := imageColumns(cfg)
		c := cols[0]
		if c.Title != "" {
			t.Errorf("cfg=%v: column 0 titled %q, want untitled", cfg, c.Title)
		}
		if c.Sizing != datatable.SizingFixed || c.MinWidth != datatable.IconColumnWidth {
			t.Errorf("column 0 sizing/min = %v/%d, want SizingFixed/%d", c.Sizing, c.MinWidth, datatable.IconColumnWidth)
		}
		if c.Indicator == nil {
			t.Errorf("cfg=%v: column 0 is not built by datatable.IndicatorColumn", cfg)
		}
		if c.Less != nil || c.Search != nil {
			t.Errorf("column 0 declares Less=%v Search=%v, want neither", c.Less != nil, c.Search != nil)
		}
		if cols[1].Title != "ID" || cols[2].Title != "Name" {
			t.Errorf("columns 1 and 2 are %q and %q, want ID and Name", cols[1].Title, cols[2].Title)
		}
	}
	if imageColumnName != 2 {
		t.Errorf("imageColumnName = %d, want 2", imageColumnName)
	}
	m := loadedModel(t)
	if shapes := m.imageTable.Shapes(); len(shapes) == 0 || !shapes[0].Indicator {
		t.Errorf("the live Images table has no activity column 0: %+v", shapes)
	}
}

func TestAnIdleImageRowShowsTheDockerIconNeverGreen(t *testing.T) {
	m := loadedModel(t)
	col := imageColumns(false)[0]
	r := imageRowByName(t, m, imgRef)
	if got := col.Indicator(r); got != indicator.Idle {
		t.Errorf("Indicator = %v, want Idle", got)
	}
	if got := col.Cell(r); got != theme.IconDocker {
		t.Errorf("resting cell = %q, want %q", got, theme.IconDocker)
	}
	if got, want := col.Style(r).GetForeground(), theme.IconStyle(theme.IconRoleImage).GetForeground(); got != want {
		t.Errorf("resting style = %v, want the IconRoleImage style %v", got, want)
	}
	if col.Style(r).GetForeground() == theme.StatusOKStyle.GetForeground() {
		t.Error("an idle icon is green")
	}
}

func TestEachImageActivityStateRendersItsGlyphOnItsRow(t *testing.T) {
	t0 := time.Now()
	for _, tc := range []struct {
		name  string
		runs  []jobs.Run
		fails []jobs.Failure
		blink bool
		state indicator.State
	}{
		{"running scan, on", []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil, true, indicator.Running},
		{"running scan, off", []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil, false, indicator.Running},
		{"running pull, on", []jobs.Run{ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, imgRef)}, nil, true, indicator.Running},
		{"queued scan", []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemQueued, imgRef)}, nil, true, indicator.Queued},
		{"scan failure", nil, []jobs.Failure{ociFailure(jobs.KindScan, ociCtx, imgRef, t0)}, true, indicator.Failed},
		{"pull failure", nil, []jobs.Failure{ociFailure(jobs.KindPull, ociCtx, imgRef, t0)}, false, indicator.Failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := ociSnapshot(t, ociCtx, tc.runs, tc.fails)
			m = feed(t, m, jobs.FrameMsg{Blink: tc.blink})
			line := imageLine(t, m, imgRef)
			if want := indicator.Glyph(tc.state, tc.blink); !strings.Contains(line, want) {
				t.Errorf("row lacks %q:\n%s", want, line)
			}
			if strings.Contains(line, theme.IconDocker) {
				t.Errorf("the glyph shares the row with the resting icon:\n%s", line)
			}
			if other := imageLine(t, m, "cache:v2"); hasAnyActivityGlyph(other) {
				t.Errorf("an untouched row shows an activity glyph:\n%s", other)
			}
		})
	}
}

func hasAnyActivityGlyph(line string) bool {
	for _, s := range indicator.States() {
		for _, on := range []bool{true, false} {
			if g := indicator.Glyph(s, on); g != "" && strings.Contains(line, g) {
				return true
			}
		}
	}
	return false
}

func TestNoImageCellCarriesASpinnerFrame(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{
		ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef),
		ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, "web:v3"),
		ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, imgNewRef),
	}, nil)
	m = feed(t, m, jobs.FrameMsg{Frame: "ZZ", RenderedFrame: "ZZ", Blink: true})
	m.imageTable.MarkBusy(imageRowByName(t, m, "cache:v2").Image.ID, "Removing cache:v2")
	if view := m.imageTable.View(); hasSpinnerFrame(view) || strings.Contains(view, "ZZ") {
		t.Errorf("a spinner frame reached the Images table:\n%s", view)
	}
	for _, row := range m.imageTable.Table().Rows() {
		for _, cell := range row {
			if hasSpinnerFrame(cell) {
				t.Errorf("cell %q carries a spinner frame", cell)
			}
		}
	}
}

func TestARunningScanHoldsTheScannedCellDim(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil)
	r := imageRowByName(t, m, imgRef)
	if !r.ScanRunning || r.PullRunning {
		t.Fatalf("ScanRunning/PullRunning = %v/%v, want true/false", r.ScanRunning, r.PullRunning)
	}
	col := imageColByTitle(t, "Scanned")
	if got := col.Cell(r); got != "scanning" {
		t.Errorf("Scanned cell = %q, want the static %q", got, "scanning")
	}
	if got, want := col.Style(r).GetForeground(), theme.DimStyle.GetForeground(); got != want {
		t.Errorf("Scanned style = %v, want DimStyle %v", got, want)
	}
	if id := imageColByTitle(t, "ID").Cell(r); id != shortID(r.Image.ID) {
		t.Errorf("ID cell = %q on a scanning row, want %q", id, shortID(r.Image.ID))
	}
}

func TestARunningPullHoldsTheIDCellDim(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, "web:v3")}, nil)
	r := imageRowByName(t, m, "web:v3")
	if !r.PullRunning || r.ScanRunning {
		t.Fatalf("PullRunning/ScanRunning = %v/%v, want true/false", r.PullRunning, r.ScanRunning)
	}
	col := imageColByTitle(t, "ID")
	if got := col.Cell(r); got != "pulling" {
		t.Errorf("ID cell = %q, want the static %q", got, "pulling")
	}
	if got, want := col.Style(r).GetForeground(), theme.DimStyle.GetForeground(); got != want {
		t.Errorf("ID style = %v, want DimStyle %v", got, want)
	}
}

// A queued scan is not running: the row keeps its date and shows no "scanning".
func TestAQueuedScanKeepsItsPreviousScannedCell(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemQueued, imgRef)}, nil)
	r := imageRowByName(t, m, imgRef)
	if r.ScanRunning {
		t.Error("a queued scan holds the Scanned cell")
	}
	if got, want := imageColByTitle(t, "Scanned").Cell(r), timeAgo(r.Entry.ScannedAt); got != want || got == "scanning" {
		t.Errorf("queued Scanned cell = %q, want the previous %q", got, want)
	}
	if got := imageColByTitle(t, "ID").Cell(r); got == "pulling" {
		t.Error("a scan turned the ID cell into pulling")
	}
}

func TestAPullPlaceholderRowShowsTheRunningDot(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, imgNewRef)}, nil)
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	r := imageRowByName(t, m, imgNewRef)
	if r.Activity.State != indicator.Running || !r.PullRunning {
		t.Fatalf("placeholder Activity/PullRunning = %v/%v, want Running/true", r.Activity.State, r.PullRunning)
	}
	if line := imageLine(t, m, "new:v1"); !strings.Contains(line, glyphDot) {
		t.Errorf("the placeholder row has no running dot:\n%s", line)
	}
}

func TestARemovalBlinksTheDotWithThePhase(t *testing.T) {
	m := loadedModel(t)
	r := imageRowByName(t, m, imgRef)
	m.imageTable.MarkBusy(r.Image.ID, "Removing "+imgRef)
	m.updateImageTable()
	for _, tc := range []struct {
		blink bool
		want  string
	}{{true, glyphDot}, {false, glyphHollow}} {
		next := feed(t, m, jobs.FrameMsg{Blink: tc.blink})
		line := imageLine(t, next, imgRef)
		if !strings.Contains(line, tc.want) {
			t.Errorf("blink=%v: row lacks %q:\n%s", tc.blink, tc.want, line)
		}
		if !strings.Contains(line, shortID(r.Image.ID)) {
			t.Errorf("blink=%v: the removing row lost its ID:\n%s", tc.blink, line)
		}
		if hasSpinnerFrame(line) {
			t.Errorf("blink=%v: spinner frame on the removing row:\n%s", tc.blink, line)
		}
	}
}

func TestAFrameStampsNothingOnTheRows(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil)
	a := imageRowByName(t, feed(t, m, jobs.FrameMsg{Blink: true}), imgRef)
	b := imageRowByName(t, feed(t, m, jobs.FrameMsg{Blink: false}), imgRef)
	if a.Activity != b.Activity {
		t.Errorf("the phase leaked into the row: %+v vs %+v", a.Activity, b.Activity)
	}
}

// BlinkingView: the router asks the view on screen whether a row blinks.
func TestWantsBlinkFollowsTheRowsThatBlink(t *testing.T) {
	var _ interface{ WantsBlink() bool } = Model{}
	t0 := time.Now()
	idle := loadedModel(t)
	if idle.WantsBlink() {
		t.Error("an idle table wants the blink")
	}
	if !ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil).WantsBlink() {
		t.Error("a running scan does not ask for the blink")
	}
	if !ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, imgNewRef)}, nil).WantsBlink() {
		t.Error("a running pull placeholder does not ask for the blink")
	}
	if ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemQueued, imgRef)}, nil).WantsBlink() {
		t.Error("a queued scan blinks")
	}
	if ociSnapshot(t, ociCtx, nil, []jobs.Failure{ociFailure(jobs.KindScan, ociCtx, imgRef, t0)}).WantsBlink() {
		t.Error("a remembered failure blinks")
	}
	busy := loadedModel(t)
	id := busy.imageTable.Items()[0].Image.ID
	busy.imageTable.MarkBusy(id, "Removing")
	busy.updateImageTable()
	if !busy.WantsBlink() {
		t.Error("a busy image row does not ask for the blink")
	}
	busy.imageTable.ClearBusy(id)
	busy.updateImageTable()
	if busy.WantsBlink() {
		t.Error("the blink outlived the removal")
	}
}

// Visible columns, by title, indicator written as "●". A dropped column has
// width 0 (Rule 116).
func visibleImageColumns(m Model) []string {
	var out []string
	for _, c := range m.imageTable.Table().Columns() {
		if c.Width <= 0 {
			continue
		}
		title, _, _ := strings.Cut(c.Title, " ") // "Name ▲" -> "Name"
		if c.Title == "" {
			title = glyphDot
		}
		out = append(out, title)
	}
	return out
}

func TestImageColumnsDropInTheNewOrder(t *testing.T) {
	tests := []struct {
		width int
		want  []string
	}{
		{60, []string{"●", "ID", "Name", "Update"}},
		{70, []string{"●", "ID", "Name", "Update", "Secrets", "C"}},
		{80, []string{"●", "ID", "Name", "Update", "Secrets", "C", "H", "M"}},
		{90, []string{"●", "ID", "Name", "Update", "Sig", "Secrets", "C", "H", "M", "L"}},
		{100, []string{"●", "ID", "Name", "Update", "Sig", "Secrets", "C", "H", "M", "L"}},
		{103, []string{"●", "ID", "Name", "Update", "Sig", "Secrets", "C", "H", "M", "L"}},
		{104, []string{"●", "ID", "Name", "Update", "Sig", "Disk", "Secrets", "C", "H", "M", "L"}},
		{119, []string{"●", "ID", "Name", "Update", "Sig", "Disk", "Secrets", "C", "H", "M", "L"}},
		{120, []string{"●", "ID", "Name", "Update", "Sig", "Disk", "Content", "Secrets", "C", "H", "M", "L"}},
		{135, []string{"●", "ID", "Name", "Update", "Sig", "Disk", "Content", "Secrets", "C", "H", "M", "L"}},
		{136, []string{"●", "ID", "Name", "Update", "Sig", "Disk", "Content", "Secrets", "C", "H", "M", "L", "Scanned"}},
	}
	for _, tt := range tests {
		m := feed(t, loadedModel(t), testutil.Resize(tt.width, 30))
		if got := visibleImageColumns(m); !slices.Equal(got, tt.want) {
			t.Errorf("width %d: visible columns = %v, want %v", tt.width, got, tt.want)
		}
		if got, want := m.imageTable.RenderedWidth(), tt.width-2; got != want {
			t.Errorf("width %d: the line spans %d, want %d (Rule 116)", tt.width, got, want)
		}
	}
}

func TestImageColumnsFitTheWidthWithTheCFGColumn(t *testing.T) {
	cfg := testConfig()
	cfg.Scan.Categories.Misconfig.Enabled = true
	for _, width := range []int{60, 70, 80, 90, 110, 140, 200} {
		m := feed(t, New(cfg), tea.WindowSizeMsg{Width: width, Height: 30}, ImagesListMsg{Images: imageFixtures()})
		if got, want := m.imageTable.RenderedWidth(), width-2; got != want {
			t.Errorf("width %d with CFG: the line spans %d, want %d", width, got, want)
		}
		if got := visibleImageColumns(m); got[0] != glyphDot || got[1] != "ID" {
			t.Errorf("width %d with CFG: columns start %v, want [● ID …]", width, got)
		}
	}
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 200, Height: 30}, ImagesListMsg{Images: imageFixtures()})
	got := visibleImageColumns(m)
	if i := slices.Index(got, "CFG"); i < 0 || got[i-1] != "L" || got[i+1] != "Scanned" {
		t.Errorf("at 200 the CFG column is misplaced: %v", got)
	}
}

// The real removal flow marks the row busy in Update() without rebuilding the
// rows; WantsBlink must still follow it (no manual updateImageTable here).
func TestWantsBlinkStartsWhenARemovalStartsAndStopsWhenItFails(t *testing.T) {
	m := removeSelected(t, loadedModel(t))
	img := m.imageTable.Items()[0]
	if !m.imageTable.IsBusy(img.Image.ID) {
		t.Fatal("precondition: the removal was not marked busy")
	}
	if !m.WantsBlink() {
		t.Error("WantsBlink is false right after the removal started")
	}
	m = feed(t, m, ImageActionMsg{Action: "remove", ID: img.Image.ID, Name: img.RawName, Err: errors.New("image is in use")})
	if m.imageTable.IsBusy(img.Image.ID) {
		t.Fatal("precondition: the busy marker survived the failure")
	}
	if m.WantsBlink() {
		t.Error("WantsBlink stays true after a failed rmi (blink stuck)")
	}
}

// Failure half, isolated: the rows were rebuilt while busy (setup only), then
// the real ImageActionMsg error path runs with no manual rebuild.
func TestWantsBlinkStopsAfterAFailedRmi(t *testing.T) {
	m := removeSelected(t, loadedModel(t))
	img := m.imageTable.Items()[0]
	m.updateImageTable() // setup: put the blinking state on the rows
	if !m.WantsBlink() {
		t.Fatal("setup: a busy row does not ask for the blink")
	}
	m = feed(t, m, ImageActionMsg{Action: "remove", ID: img.Image.ID, Name: img.RawName, Err: errors.New("image is in use")})
	if m.WantsBlink() {
		t.Error("WantsBlink stays true after a failed rmi (blink stuck)")
	}
}
