package datatable

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// A table with an identity, which is what a view has to declare to get the
// busy facility at all. It has no Indicator column.
func busyConfig() Config[row] {
	cfg := Config[row]{
		Key:        func(r row) string { return r.Name },
		SortColumn: 1,
		Columns: []Column[row]{
			{
				Title: "", MinWidth: 3,
				Cell: func(r row) string { return stateGlyph(r.State) },
			},
			{
				Title: "Name", MinWidth: 20, Flex: 1,
				Cell: func(r row) string { return r.Name },
				Less: func(a, b row) bool { return a.Name < b.Name },
			},
			{
				Title: "State", MinWidth: 12,
				Cell:  func(r row) string { return r.State },
				Style: func(row) lipgloss.Style { return theme.StatusOKStyle },
			},
		},
	}
	return cfg
}

// stateGlyph stands in for the view's own state iconography.
func stateGlyph(state string) string {
	if state == "running" {
		return ">"
	}
	return "="
}

func busyTable(t *testing.T) Model[row] {
	t.Helper()
	m := New(busyConfig())
	m.Resize(120, 10)
	m.SetItems(fixtures())
	return m
}

// rowFor returns the rendered line whose Name cell holds this name.
func renderedRow(t *testing.T, m Model[row], name string) string {
	t.Helper()
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("no rendered row for %q in:\n%s", name, m.View())
	return ""
}

// The highlight is applied to the joined row, so the busy colour has to be
// decided as the selection style rather than per cell — a colour inside would
// close with a reset and end the highlight mid-row.
func TestABusySelectedRowTakesTheBusySelectionStyle(t *testing.T) {
	errorStyles := func(row) table.Styles { return theme.TableStylesForState("error") }
	cfg := busyConfig()
	cfg.SelectedStyles = errorStyles
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())
	m.GotoTop()

	if got := m.styles.Selected.GetBackground(); got != theme.ColorError {
		t.Fatalf("selection background = %v, want the view's error colour", got)
	}

	selected, _ := m.Selected()
	m.MarkBusy(selected.Name, "Removing")
	m.GotoTop() // re-applies the styles for the row under the cursor

	if got := m.styles.Selected.GetBackground(); got != theme.ColorHighlight {
		t.Errorf("selection background = %v, want the busy colour to outrank the state", got)
	}
}

// Clearing has to happen on every outcome. A row that keeps spinning after a
// failed command hides the state it still has.
func TestClearBusyPutsTheStateBack(t *testing.T) {
	m := busyTable(t)
	m.MarkBusy("web", "Stopping web")

	m.ClearBusy("web")

	if m.IsBusy("web") {
		t.Error("IsBusy is still true after ClearBusy")
	}
	if line := renderedRow(t, m, "web"); !strings.Contains(line, ">") {
		t.Errorf("the row did not go back to showing its state:\n%s", line)
	}
}

// The guard on a confirm path runs before anything is rebuilt, so IsBusy has to
// answer for an object rather than for a row.
func TestIsBusyAnswersForAnObjectWithNoRow(t *testing.T) {
	m := New(busyConfig())
	m.Resize(120, 10)

	m.MarkBusy("web", "Stopping web")

	if !m.IsBusy("web") {
		t.Error("IsBusy is false for an object whose row has not been built yet")
	}
}

// The periodic refresh replaces the items mid-action. Keying on identity rather
// than a flag on the row is what survives it.
func TestABusyRowSurvivesAReload(t *testing.T) {
	m := busyTable(t)
	m.MarkBusy("web", "Stopping web")

	m.SetItems(fixtures())

	if !m.IsBusy("web") {
		t.Fatal("the busy marker was lost when the items were replaced")
	}
}

// Nothing changes for the fifteen tables that declare no Key: the facility is
// opt-in, and a status column costs cells every table cannot spare.
func TestWithoutAKeyNothingIsEverBusy(t *testing.T) {
	m := loaded(t) // testConfig() declares no Key
	before := m.View()

	m.MarkBusy("web", "Stopping web")

	if m.IsBusy("web") {
		t.Error("a table with no Key reported a busy row")
	}
	if m.View() != before {
		t.Error("a table with no Key rendered differently after MarkBusy")
	}
}

// The footer names what is running. Sorted, because a map's order changes
// between frames and a footer that reshuffles itself cannot be read.
func TestBusyLabelsAreSorted(t *testing.T) {
	m := busyTable(t)
	m.MarkBusy("web", "Stopping web")
	m.MarkBusy("api", "Removing api")
	m.MarkBusy("cache", "Restarting cache")

	got := m.BusyLabels()

	want := []string{"Removing api", "Restarting cache", "Stopping web"}
	if len(got) != len(want) {
		t.Fatalf("BusyLabels() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("BusyLabels() = %v, want %v", got, want)
			break
		}
	}
}

// Without an Indicator column a busy row is only greyed: no glyph is added and
// no spinner frame replaces a cell (DEVDESK-143).
func TestABusyRowIsDimmed(t *testing.T) {
	withTrueColor(t)
	m := busyTable(t)
	m.GotoTop() // "api" — so "web" below is busy *and* unselected
	before := ansi.Strip(renderedRow(t, m, "web"))
	m.MarkBusy("web", "Stopping web")

	line := renderedRow(t, m, "web")

	if !strings.Contains(line, foreground(theme.ColorDim)) {
		t.Errorf("the busy row is not dimmed: %q", line)
	}
	// Every cell is dim: nothing keeps a column's colour or a highlight.
	for _, c := range []lipgloss.Color{theme.ColorOK, theme.ColorHighlight} {
		if strings.Contains(line, foreground(c)) {
			t.Errorf("the busy row kept a colour that is not dim: %q", line)
		}
	}
	if after := ansi.Strip(line); after != before {
		t.Errorf("busy changed the row's text:\nbefore %q\nafter  %q", before, after)
	}
}

func TestABusyRowWithoutAnIndicatorShowsNoSpinnerFrame(t *testing.T) {
	m := busyTable(t)
	m.MarkBusy("web", "Stopping web")
	line := ansi.Strip(renderedRow(t, m, "web"))
	for _, frame := range spinner.Dot.Frames {
		if strings.Contains(line, frame) {
			t.Errorf("the busy row shows the spinner frame %q: %q", frame, line)
		}
	}
	for _, g := range []string{"●", "○"} {
		if strings.Contains(line, g) {
			t.Errorf("a table with no Indicator column shows the glyph %q: %q", g, line)
		}
	}
}

// With an Indicator column a busy row is Running: ● then ○ with the blink phase.
func TestABusyRowOnAnIndicatorColumnBlinks(t *testing.T) {
	m := indicatorTable(t, row{Name: "web", State: "idle"}, row{Name: "api", State: "idle"})
	m.MarkBusy("web", "Stopping web")
	if got := ansi.Strip(lineOf(t, &m, "web")); !strings.Contains(got, "●") {
		t.Errorf("busy row, phase on: want ●: %q", got)
	}
	m.SetBlinkPhase(false)
	got := ansi.Strip(lineOf(t, &m, "web"))
	if !strings.Contains(got, "○") || strings.Contains(got, "●") {
		t.Errorf("busy row, phase off: want ○ only: %q", got)
	}
	if other := ansi.Strip(lineOf(t, &m, "api")); strings.ContainsAny(other, "●○") {
		t.Errorf("a row nothing happens to shows a glyph: %q", other)
	}
}

// Reflection: the row spinner mechanism is gone from the type.
func TestDatatableHasNoRowSpinner(t *testing.T) {
	if _, ok := reflect.TypeOf(Config[row]{}).FieldByName("StatusColumn"); ok {
		t.Error("Config still has a StatusColumn field")
	}
	mt := reflect.TypeOf(Model[row]{})
	for _, name := range []string{"spinnerFrame", "spinnerIdx"} {
		if _, ok := mt.FieldByName(name); ok {
			t.Errorf("Model still has a %s field", name)
		}
	}
	if _, ok := reflect.TypeOf(&Model[row]{}).MethodByName("AdvanceSpinner"); ok {
		t.Error("Model still has an AdvanceSpinner method")
	}
}

// Rule 115: lipgloss inherits no background, so a busy row must not open a hole
// in the table's own.
func TestABusyRowKeepsTheAppBackground(t *testing.T) {
	withTrueColor(t)
	m := busyTable(t)
	m.GotoTop()
	m.MarkBusy("web", "Stopping web")

	line := renderedRow(t, m, "web")

	for i, segment := range strings.Split(line, "\x1b[0m") {
		if visible(segment) == "" {
			continue
		}
		if !strings.Contains(segment, background(theme.ColorBackground)) {
			t.Errorf("run %d (%q) of the busy row carries no background", i, visible(segment))
		}
	}
}

// BusyLabel answers for one key, where BusyLabels lists them all.
func TestBusyLabelByKey(t *testing.T) {
	m := busyTable(t)
	if label, ok := m.BusyLabel("web"); ok || label != "" {
		t.Fatalf("BusyLabel before MarkBusy = (%q, %v), want (\"\", false)", label, ok)
	}
	m.MarkBusy("web", "Removing web")
	if label, ok := m.BusyLabel("web"); !ok || label != "Removing web" {
		t.Errorf("BusyLabel(web) = (%q, %v), want (Removing web, true)", label, ok)
	}
	if _, ok := m.BusyLabel("api"); ok {
		t.Error("BusyLabel(api) reports a row nothing is happening to")
	}
	m.ClearBusy("web")
	if _, ok := m.BusyLabel("web"); ok {
		t.Error("BusyLabel(web) still true after ClearBusy")
	}
}
