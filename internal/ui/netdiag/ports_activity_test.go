package netdiag

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-150 step 1: column 0 of the Ports table is the activity column
// (Rule 125). The only row operation is K, held in the table's busy map, which
// the datatable already reports as Running. Nothing here goes through the jobs
// registry, and a failed kill stays a footer error (D5, Q1-A, Q2-A).

func hasSpinnerFrame(s string) bool {
	for _, f := range spinner.Dot.Frames {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

func hasAnyActivityGlyph(s string) bool {
	for _, st := range indicator.States() {
		for _, on := range []bool{true, false} {
			if g := indicator.Glyph(st, on); g != "" && strings.Contains(s, g) {
				return true
			}
		}
	}
	return false
}

// portLines returns the rendered body lines (header excluded) holding sub.
func portLines(m *Model, sub string) []string {
	var out []string
	for i, l := range strings.Split(m.portsModel.table.View(), "\n") {
		if i > 0 && strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return out
}

func killingSSHD(t *testing.T) *Model {
	t.Helper()
	return feed(t, portsModel(t), testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{}) // sshd, PID 812
}

func TestPortsColumnZeroIsTheActivityColumn(t *testing.T) {
	m := portsModel(t)
	shapes := m.portsModel.table.Shapes()
	if len(shapes) != 7 {
		t.Fatalf("%d columns, want the dot plus the six of today: %+v", len(shapes), shapes)
	}
	d := shapes[0]
	if d.Title != "" || d.Sizing != datatable.SizingFixed || d.MinWidth != datatable.IconColumnWidth {
		t.Errorf("column 0 = %+v, want untitled, SizingFixed, IconColumnWidth", d)
	}
	if !d.Indicator || d.Less || d.Search {
		t.Errorf("column 0 Indicator/Less/Search = %v/%v/%v, want true/false/false", d.Indicator, d.Less, d.Search)
	}
	for i, title := range []string{"Proto", "State", "Local Address", "Peer Address", "PID", "Process"} {
		if got := shapes[i+1].Title; got != title {
			t.Errorf("column %d titled %q, want %q", i+1, got, title)
		}
		if shapes[i+1].Indicator {
			t.Errorf("column %d (%s) declares an indicator; only column 0 may", i+1, title)
		}
	}
	if portsColIndicator != 0 || portsColProto != 1 || portsColProcess != 6 {
		t.Errorf("column indexes = %d/%d/%d, want 0/1/6", portsColIndicator, portsColProto, portsColProcess)
	}
}

func TestNothingBusyLeavesEveryDotBlankAndNoBlinkWanted(t *testing.T) {
	m := portsModel(t)
	if view := m.portsModel.table.View(); hasAnyActivityGlyph(view) {
		t.Errorf("an idle table shows an activity glyph:\n%s", view)
	}
	if m.WantsBlink() {
		t.Error("WantsBlink with nothing busy: the router would frame the view for nothing")
	}
}

func TestAKillShowsTheRunningDotOnItsRowOnly(t *testing.T) {
	m := killingSSHD(t)
	if !m.portsModel.table.IsBusy("812") {
		t.Fatal("the kill was not marked on the row")
	}
	want := indicator.Glyph(indicator.Running, true)
	lines := portLines(m, "sshd")
	if len(lines) != 1 || !strings.Contains(lines[0], want) {
		t.Fatalf("the sshd row lacks %q:\n%v", want, lines)
	}
	for _, other := range []string{"nginx", "dnsmasq", "kernel"} {
		for _, l := range portLines(m, other) {
			if hasAnyActivityGlyph(l) {
				t.Errorf("the %s row shows a glyph for a kill that is not its own:\n%s", other, l)
			}
		}
	}
	if !m.WantsBlink() {
		t.Error("WantsBlink is false while a kill runs: the dot would never blink")
	}
}

func TestAKillMarksEveryRowOfThatProcess(t *testing.T) {
	shared := append(portFixtures(),
		ports.Socket{Protocol: "tcp", State: "LISTEN", LocalAddr: "0.0.0.0:2222", PID: "812", Process: "sshd"},
	)
	m := feed(t, newTestModel(t), testutil.Key("tab"))
	m = feed(t, m, portsDataMsg{ports: shared})

	m = feed(t, m, testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{})

	want := indicator.Glyph(indicator.Running, true)
	lines := portLines(m, "sshd")
	if len(lines) != 2 {
		t.Fatalf("%d sshd rows, want 2", len(lines))
	}
	for _, l := range lines {
		if !strings.Contains(l, want) {
			t.Errorf("a socket of the process lacks %q:\n%s", want, l)
		}
	}
}

func TestAFrameBlinksTheDotWithoutRebuildingRows(t *testing.T) {
	m := killingSSHD(t)
	before := reflect.ValueOf(m.portsModel.table.Items()).Pointer()
	items := append([]ports.Socket(nil), m.portsModel.table.Items()...)

	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if l := portLines(m, "sshd"); len(l) != 1 || !strings.Contains(l[0], indicator.Glyph(indicator.Running, false)) {
		t.Errorf("Blink=false: the row lacks %q:\n%v", indicator.Glyph(indicator.Running, false), l)
	}
	if strings.Contains(strings.Join(portLines(m, "sshd"), ""), indicator.Glyph(indicator.Running, true)) {
		t.Error("Blink=false still shows the on glyph")
	}
	if !m.WantsBlink() {
		t.Error("WantsBlink dropped on the off half of the blink")
	}

	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if l := portLines(m, "sshd"); len(l) != 1 || !strings.Contains(l[0], indicator.Glyph(indicator.Running, true)) {
		t.Errorf("Blink=true: the row lacks %q:\n%v", indicator.Glyph(indicator.Running, true), l)
	}
	if after := reflect.ValueOf(m.portsModel.table.Items()).Pointer(); after != before {
		t.Error("a frame rebuilt the rows; the phase is read at render")
	}
	if !reflect.DeepEqual(items, m.portsModel.table.Items()) {
		t.Error("a frame changed the items")
	}
}

// Q2-A: the State cell keeps its text and is dimmed like every busy row.
func TestABusyRowKeepsItsStateText(t *testing.T) {
	m := killingSSHD(t)
	lines := portLines(m, "sshd")
	if len(lines) != 1 {
		t.Fatalf("%d sshd rows, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "LISTEN") {
		t.Errorf("the busy row lost its State text (Q2-A keeps it, dimmed):\n%s", lines[0])
	}
	if cell := m.portsModel.table.Table().Rows()[0][portsColState]; cell != "LISTEN" {
		t.Errorf("State cell = %q, want the plain text %q", cell, "LISTEN")
	}
}

func TestNoPortsCellCarriesASpinnerFrame(t *testing.T) {
	m := killingSSHD(t)
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if view := m.portsModel.table.View(); hasSpinnerFrame(view) {
		t.Errorf("a spinner frame reached the Ports table:\n%s", view)
	}
	for _, row := range m.portsModel.table.Table().Rows() {
		for _, cell := range row {
			if hasSpinnerFrame(cell) {
				t.Errorf("cell %q carries a spinner frame", cell)
			}
		}
	}
}

func TestAKillThatLandsLeavesNoDot(t *testing.T) {
	m := feed(t, killingSSHD(t), portsKillResultMsg{pid: "812"})
	if hasAnyActivityGlyph(m.portsModel.table.View()) {
		t.Errorf("a dot survived the kill result:\n%s", m.portsModel.table.View())
	}
	if m.WantsBlink() {
		t.Error("WantsBlink after the kill landed")
	}
	if !strings.Contains(m.portsModel.footer.Text(), "812") || m.portsModel.footer.Level() != components.LevelInfo {
		t.Errorf("footer = %q (level %v), want the Info naming PID 812",
			m.portsModel.footer.Text(), m.portsModel.footer.Level())
	}
}

// Criterion 1 (D5): a refused kill is a footer Error; the row goes back to rest
// and no red dot is drawn (there is no kill kind to remember).
func TestARefusedKillIsAFooterErrorAndNeverAFailedDot(t *testing.T) {
	m := feed(t, killingSSHD(t), portsKillResultMsg{pid: "812", err: fmt.Errorf("kill: %w", os.ErrPermission)})

	if hasAnyActivityGlyph(m.portsModel.table.View()) {
		t.Errorf("a glyph is drawn after a refused kill:\n%s", m.portsModel.table.View())
	}
	for _, on := range []bool{true, false} {
		if g := indicator.Glyph(indicator.Failed, on); strings.Contains(m.portsModel.table.View(), g) {
			t.Errorf("the table shows the failed glyph %q: there is nothing to remember", g)
		}
	}
	if m.WantsBlink() {
		t.Error("WantsBlink after a refused kill")
	}
	if got := m.portsModel.footer.Text(); !strings.HasPrefix(got, "Refused by the system — ") {
		t.Errorf("footer = %q, want the refusal", got)
	}
	if m.portsModel.footer.Level() != components.LevelError {
		t.Errorf("footer level = %v, want Error", m.portsModel.footer.Level())
	}
}

// Criterion 3: the periodic refresh never blinks anything, and a busy PID
// survives it.
func TestARefreshWithNothingBusyNeverBlinks(t *testing.T) {
	m := portsModel(t)
	m = feed(t, m, NewPortsDataMsg(portFixtures()))
	if hasAnyActivityGlyph(m.portsModel.table.View()) {
		t.Errorf("a refresh drew a glyph:\n%s", m.portsModel.table.View())
	}
	if m.WantsBlink() {
		t.Error("a refresh made the view ask for blink frames")
	}
}

func TestARefreshWhileAKillRunsKeepsEveryRowOfThePIDDottedAndTheCursor(t *testing.T) {
	m := killingSSHD(t)
	m = feed(t, m, testutil.Key("down"))
	cur := m.portsModel.table.Cursor()
	fresh := append(portFixtures(),
		ports.Socket{Protocol: "tcp", State: "LISTEN", LocalAddr: "0.0.0.0:2222", PID: "812", Process: "sshd"})

	m = feed(t, m, NewPortsDataMsg(fresh))

	want := indicator.Glyph(indicator.Running, true)
	lines := portLines(m, "sshd")
	if len(lines) != 2 {
		t.Fatalf("%d sshd rows after the refresh, want 2", len(lines))
	}
	for _, l := range lines {
		if !strings.Contains(l, want) {
			t.Errorf("a refreshed socket of the busy PID lacks %q:\n%s", want, l)
		}
	}
	if got := m.portsModel.table.Cursor(); got != cur {
		t.Errorf("cursor = %d after the refresh, want %d", got, cur)
	}
	if !m.WantsBlink() {
		t.Error("the refresh dropped the busy mark")
	}
}

// Criterion 4 / Q1-A: a failed network test stays in the Verdict column and the
// other tables get no activity column.
func TestOnlyThePortsTableCarriesAnActivityColumn(t *testing.T) {
	m := resultsModel(t)
	for name, shapes := range map[string][]datatable.ColumnShape{
		"checks":     m.checksTable.Shapes(),
		"interfaces": m.interfacesModel.table.Shapes(),
	} {
		for i, s := range shapes {
			if s.Indicator {
				t.Errorf("%s column %d (%q) declares an indicator", name, i, s.Title)
			}
		}
	}
	if shapes := m.checksTable.Shapes(); len(shapes) == 0 || shapes[0].Title == "" {
		t.Errorf("the checks table no longer starts with a titled column: %+v", shapes)
	}
	tables := m.ActivityTables()
	if len(tables) != 1 || tables["netdiag/ports"] == nil {
		t.Errorf("ActivityTables = %v, want exactly netdiag/ports", tables)
	}
	// The failure is still the Verdict's: red FAIL with its reason, no dot.
	if view := m.checksTable.View(); !strings.Contains(view, "FAIL") || hasAnyActivityGlyph(view) {
		t.Errorf("the checks table must say FAIL in its Verdict and carry no glyph:\n%s", view)
	}
}

// Rule 116: the line spans the viewport at every width, and the columns drop
// rightmost first at the measured thresholds (min + 2 padding each; the dot
// costs 4 cells over the old layout: 99, 87, 78, 50 became 103, 91, 82, 54).
func TestPortsColumnsFitEveryWidthAndDropRightmostFirst(t *testing.T) {
	all := []string{"Proto", "State", "Local Address", "Peer Address", "PID", "Process"}
	for _, tc := range []struct {
		width int
		shown int // how many of `all`, from the left
	}{
		{40, 2}, {53, 2}, {54, 3}, {60, 3}, {81, 3}, {82, 4},
		{90, 4}, {91, 5}, {102, 5}, {103, 6}, {120, 6}, {200, 6},
	} {
		t.Run(fmt.Sprintf("width %d", tc.width), func(t *testing.T) {
			m := feed(t, portsModel(t), tea.WindowSizeMsg{Width: tc.width, Height: 40})
			if got, want := m.portsModel.table.RenderedWidth(), tc.width-2; got != want {
				t.Errorf("the line spans %d, want %d", got, want)
			}
			header := strings.Split(m.portsModel.table.View(), "\n")[0]
			for i, title := range all {
				if has := strings.Contains(header, title); has != (i < tc.shown) {
					t.Errorf("header has %q = %v, want %v:\n%s", title, has, i < tc.shown, header)
				}
			}
		})
	}
}

// The retired mechanisms are gone from the package's sources: a reintroduction
// names its file and line. (Test files are not scanned.)
func TestThePortsRowChainIsGone(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	retired := []string{"portsSpinnerTickMsg", "portsSpinnerCmd", "handleSpinnerTick", "StatusColumn", "AdvanceSpinner"}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		_, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		src, _ := os.ReadFile(f)
		for i, line := range strings.Split(string(src), "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "//") {
				continue
			}
			for _, name := range retired {
				if strings.Contains(line, name) {
					t.Errorf("%s:%d still uses the retired %s", f, i+1, name)
				}
			}
		}
	}
}
