package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// A table with a status column at 0 and an identity, which is what a view has
// to declare to get the busy facility at all.
func busyConfig() Config[row] {
	cfg := Config[row]{
		Key:          func(r row) string { return r.Name },
		StatusColumn: 0,
		SortColumn:   1,
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

// The whole point: the status column stops saying what the object is and says
// that something is happening to it. "Busy wins over state" is one rule, here.
func TestABusyRowShowsTheSpinnerInItsStatusColumn(t *testing.T) {
	m := busyTable(t)
	before := renderedRow(t, m, "web")
	if !strings.Contains(before, ">") {
		t.Fatalf("the status column does not show the state to begin with:\n%s", before)
	}

	m.MarkBusy("web", "Stopping web")

	line := renderedRow(t, m, "web")
	if strings.Contains(line, ">") {
		t.Errorf("the busy row still shows its state glyph:\n%s", line)
	}
	if !strings.Contains(line, m.spinnerFrame) {
		t.Errorf("the busy row shows no spinner:\n%s", line)
	}
	// And only that row.
	if other := renderedRow(t, m, "api"); !strings.Contains(other, ">") {
		t.Errorf("a row nothing is happening to lost its state glyph:\n%s", other)
	}
}

// The glyph is the primary signal, so it has to survive the cursor: a signal
// that vanishes under the highlight is lost exactly when it is being looked at.
func TestTheSpinnerShowsOnTheSelectedRowToo(t *testing.T) {
	m := busyTable(t)
	m.GotoTop() // "api", the first row by name
	selected, _ := m.Selected()
	m.MarkBusy(selected.Name, "Removing "+selected.Name)

	line := renderedRow(t, m, selected.Name)
	if !strings.Contains(line, m.spinnerFrame) {
		t.Errorf("the selected busy row shows no spinner:\n%s", line)
	}
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
	if line := renderedRow(t, m, "web"); strings.Contains(line, ">") {
		t.Errorf("the reloaded row went back to showing its state:\n%s", line)
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

// Rule 122: the spinner goes in as plain text, so the row must carry no escape
// sequence a measurement could cut through.
func TestTheSpinnerFrameCarriesNoEscapeSequence(t *testing.T) {
	m := busyTable(t)
	m.MarkBusy("web", "Stopping web")
	for range 4 {
		m.AdvanceSpinner()
	}

	if strings.Contains(m.spinnerFrame, "\x1b[") {
		t.Errorf("the spinner frame is %q, want plain text", m.spinnerFrame)
	}
}

// A table drawn in the same frame the action began has no tick behind it yet.
func TestThereIsAFrameBeforeTheFirstTick(t *testing.T) {
	m := busyTable(t)
	m.MarkBusy("web", "Stopping web")

	if m.spinnerFrame == "" {
		t.Fatal("the spinner frame is empty before the first tick")
	}
	if line := renderedRow(t, m, "web"); strings.TrimSpace(line[:6]) == "" {
		t.Errorf("the status cell is blank before the first tick:\n%s", line)
	}
}

// The row style is the second cue, after the glyph. Off the selected row it can
// be decided per cell, which is where the existing rule puts it: `Style` is not
// consulted under the cursor.
func TestABusyRowIsDimmedAndItsSpinnerIsNot(t *testing.T) {
	withTrueColor(t)
	m := busyTable(t)
	m.GotoTop() // "api" — so "web" below is busy *and* unselected
	m.MarkBusy("web", "Stopping web")

	line := renderedRow(t, m, "web")

	if !strings.Contains(line, foreground(theme.ColorDim)) {
		t.Errorf("the busy row is not dimmed: %q", line)
	}
	if !strings.Contains(line, foreground(theme.ColorHighlight)) {
		t.Errorf("the spinner does not stand out from the dimmed row: %q", line)
	}
	// The column's own colour is dropped: it describes a state the action is
	// about to change.
	if strings.Contains(line, foreground(theme.ColorOK)) {
		t.Errorf("the busy row kept a column's own colour: %q", line)
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
