package datatable

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// stateOf reads the indicator state a test row names in its State field.
func stateOf(r row) indicator.State {
	for _, s := range indicator.States() {
		if s.String() == r.State {
			return s
		}
	}
	return indicator.Idle
}

// indicatorConfig is an IndicatorColumn at 0 and two ASCII columns. cells counts
// the calls of the Size column's Cell, to prove nothing is rebuilt.
func indicatorConfig(cells *int) Config[row] {
	return Config[row]{
		Key:        func(r row) string { return r.Name },
		SortColumn: 1,
		Columns: []Column[row]{
			IndicatorColumn(stateOf, nil, nil),
			{
				Title: "Name", Sizing: SizingContent, MinWidth: 12,
				Cell: func(r row) string { return r.Name },
				Less: func(a, b row) bool { return a.Name < b.Name },
			},
			{
				Title: "Size", Sizing: SizingContent, MinWidth: 6,
				Cell: func(r row) string {
					if cells != nil {
						*cells++
					}
					return "sz"
				},
			},
		},
	}
}

func indicatorTable(t *testing.T, items ...row) Model[row] {
	t.Helper()
	m := New(indicatorConfig(nil))
	m.Resize(60, 10)
	m.SetItems(items)
	return m
}

// lineOf returns the raw rendered line holding the name.
func lineOf(t *testing.T, m *Model[row], name string) string {
	t.Helper()
	for _, l := range strings.Split(m.View(), "\n")[1:] { // [0] is the header
		if strings.Contains(ansi.Strip(l), name) {
			return l
		}
	}
	t.Fatalf("no line for %q in:\n%s", name, m.View())
	return ""
}

// cursorOn moves the cursor to the named row (rows sort by Name).
func cursorOn(t *testing.T, m *Model[row], name string) {
	t.Helper()
	for i, r := range m.Visible() {
		if r.Name == name {
			m.SetCursor(i)
			return
		}
	}
	t.Fatalf("no row %q", name)
}

func TestIndicatorColumnHasTheRule125Shape(t *testing.T) {
	c := IndicatorColumn(stateOf, nil, nil)
	if c.Title != "" || c.Sizing != SizingFixed || c.MinWidth != IconColumnWidth {
		t.Errorf("shape = title %q sizing %v min %d", c.Title, c.Sizing, c.MinWidth)
	}
	if c.Less != nil || c.Search != nil {
		t.Error("an indicator column has no Less and no Search")
	}
	if c.Indicator == nil {
		t.Error("Indicator is nil")
	}
}

func TestAnIdleRowShowsItsRestIcon(t *testing.T) {
	withTrueColor(t)
	rest := func(row) string { return ">" }
	style := func(row) lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.ColorSecondary) }
	items := []row{{Name: "a", State: "idle"}, {Name: "b", State: "idle"}}

	withRest := indicatorConfig(nil)
	withRest.Columns[0] = IndicatorColumn(stateOf, rest, style)
	plain := indicatorConfig(nil)
	plain.Columns[0] = Column[row]{Title: "", Sizing: SizingFixed, MinWidth: IconColumnWidth, Cell: rest, Style: style}

	a, b := New(withRest), New(plain)
	for _, m := range []*Model[row]{&a, &b} {
		m.Resize(60, 10)
		m.SetItems(items)
	}
	if got, want := lineOf(t, &a, "b"), lineOf(t, &b, "b"); got != want {
		t.Errorf("idle row differs from a plain column:\n got %q\nwant %q", got, want)
	}

	none := indicatorTable(t, items...)
	line := lineOf(t, &none, "b")
	if strings.Contains(line, "◌") {
		t.Error("an idle row must never show the Unknown glyph")
	}
	if !strings.HasPrefix(ansi.Strip(line), strings.Repeat(" ", IconColumnWidth+2)) {
		t.Errorf("nil rest should render %d spaces, got %q", IconColumnWidth+2, ansi.Strip(line))
	}
	if !strings.Contains(line, background(theme.ColorBackground)) {
		t.Errorf("the empty cell is not background-filled: %q", line)
	}
}

func TestRunningBlinksWithThePhase(t *testing.T) {
	withTrueColor(t)
	m := indicatorTable(t, row{Name: "a", State: "running"}, row{Name: "z", State: "idle"})
	m.SetCursor(1)
	fg := foreground(indicator.Color(indicator.Running))

	on := lineOf(t, &m, "a")
	if !strings.Contains(on, "●") || !strings.Contains(on, fg) {
		t.Fatalf("default phase should draw ● in the running colour: %q", on)
	}
	m.SetBlinkPhase(false)
	off := lineOf(t, &m, "a")
	if !strings.Contains(off, "○") || strings.Contains(off, "●") || !strings.Contains(off, fg) {
		t.Errorf("phase off should draw ○ in the same colour: %q", off)
	}
	m.SetBlinkPhase(true)
	if got := lineOf(t, &m, "a"); got != on {
		t.Errorf("phase back on should restore the line: %q", got)
	}
	if !m.BlinkPhase() {
		t.Error("BlinkPhase should report true")
	}
}

func TestTheBlinkPhaseRebuildsNothing(t *testing.T) {
	var cells int
	m := New(indicatorConfig(&cells))
	m.Resize(60, 10)
	m.SetItems([]row{{Name: "a", State: "running"}, {Name: "b", State: "idle"}})

	rows, width, calls := len(m.Table().Rows()), m.RenderedWidth(), cells
	first := m.Table().Rows()[0][2]
	m.SetBlinkPhase(false)
	m.SetBlinkPhase(true)
	if cells != calls {
		t.Errorf("Cell of another column was called %d times by SetBlinkPhase", cells-calls)
	}
	if len(m.Table().Rows()) != rows || m.Table().Rows()[0][2] != first || m.RenderedWidth() != width {
		t.Error("SetBlinkPhase changed rows or the rendered width")
	}
}

func TestOnlyRunningFollowsThePhase(t *testing.T) {
	withTrueColor(t)
	for _, s := range indicator.States() {
		if s == indicator.Running {
			continue
		}
		m := indicatorTable(t, row{Name: "a", State: s.String()}, row{Name: "z", State: "idle"})
		m.SetCursor(1)
		on := lineOf(t, &m, "a")
		m.SetBlinkPhase(false)
		if off := lineOf(t, &m, "a"); off != on {
			t.Errorf("%v changes with the phase:\n on %q\noff %q", s, on, off)
		}
		if !strings.Contains(on, indicator.Glyph(s, true)) {
			t.Errorf("%v does not show its glyph: %q", s, on)
		}
	}
}

func TestTheIndicatorColourSurvivesThePlainSelection(t *testing.T) {
	withTrueColor(t)
	m := indicatorTable(t, row{Name: "a", State: "failed"}, row{Name: "b", State: "idle"})
	cursorOn(t, &m, "a")
	line := lineOf(t, &m, "a")
	want := foreground(indicator.Color(indicator.Failed))
	if !strings.Contains(line, want) || !strings.Contains(line, background(theme.ColorTableLineSelected)) {
		t.Errorf("selected indicator lost its colour or the selection background: %q", line)
	}
	if !strings.Contains(line, "⊗") {
		t.Errorf("glyph missing: %q", line)
	}

	// Criterion 8: under a view-coloured selection the glyph is still there.
	cfg := indicatorConfig(nil)
	cfg.SelectedStyles = func(row) table.Styles { return theme.TableStylesForState("error") }
	e := New(cfg)
	e.Resize(60, 10)
	e.SetItems([]row{{Name: "a", State: "failed"}})
	e.SetCursor(0)
	if got := lineOf(t, &e, "a"); !strings.Contains(ansi.Strip(got), "⊗") {
		t.Errorf("glyph lost under an error selection: %q", ansi.Strip(got))
	}
}

func TestBusyWinsOverTheViewState(t *testing.T) {
	withTrueColor(t)
	m := indicatorTable(t, row{Name: "a", State: "failed"}, row{Name: "z", State: "idle"})
	m.SetCursor(1)
	check := func(label string, wantGlyph string) {
		t.Helper()
		got := ansi.Strip(lineOf(t, &m, "a"))
		if !strings.Contains(got, wantGlyph) {
			t.Errorf("%s: want %s in %q", label, wantGlyph, got)
		}
		for _, f := range []string{"⊗", "⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"} {
			if wantGlyph != f && strings.Contains(got, f) {
				t.Errorf("%s: unexpected %s in %q", label, f, got)
			}
		}
	}
	m.MarkBusy("a", "Working")
	check("busy", "●")
	m.SetBlinkPhase(false)
	check("busy off-phase", "○")
	m.SetBlinkPhase(true)
	if !strings.Contains(lineOf(t, &m, "a"), foreground(indicator.Color(indicator.Running))) {
		t.Error("busy indicator should take the running colour")
	}
	m.ClearBusy("a")
	check("cleared", "⊗")

	// Same on the selected row.
	cursorOn(t, &m, "a")
	m.MarkBusy("a", "Working")
	m.GotoTop()
	cursorOn(t, &m, "a")
	check("busy selected", "●")
	m.ClearBusy("a")
	check("cleared selected", "⊗")
}

func TestTwoSourcesRenderTheSame(t *testing.T) {
	withTrueColor(t)
	mk := func(s indicator.State) string {
		cfg := indicatorConfig(nil)
		cfg.Columns[0] = IndicatorColumn(func(row) indicator.State { return s }, nil, nil)
		m := New(cfg)
		m.Resize(60, 10)
		m.SetItems([]row{{Name: "a"}, {Name: "z"}})
		m.SetCursor(1)
		return lineOf(t, &m, "a")
	}
	a := mk(indicator.Resolve(indicator.Activity{Failed: true}))
	b := mk(indicator.Resolve(indicator.Activity{Observed: indicator.Failed}))
	if a != b || !strings.Contains(a, "⊗") {
		t.Errorf("sources differ or glyph missing:\n%q\n%q", a, b)
	}
}

func TestAnIndicatorCellIsPlainText(t *testing.T) {
	rest := func(row) string { return ">" }
	for _, c := range []Column[row]{IndicatorColumn(stateOf, nil, nil), IndicatorColumn(stateOf, rest, nil)} {
		for _, s := range append(indicator.States(), indicator.Idle) {
			if got := c.Cell(row{State: s.String()}); strings.Contains(got, "\x1b") {
				t.Errorf("Cell for %v contains an escape: %q", s, got)
			}
		}
	}
	withTrueColor(t)
	m := indicatorTable(t, row{Name: "a", State: "done"}, row{Name: "z", State: "idle"})
	m.SetCursor(1)
	if got := ansi.Strip(lineOf(t, &m, "a")); !strings.HasPrefix(got, " ◉ ") {
		t.Errorf("glyph should sit alone in its cell: %q", got)
	}
	for _, r := range m.Table().Rows() {
		if strings.Contains(r[0], "\x1b") {
			t.Errorf("row text handed to rendering has an escape: %q", r[0])
		}
	}
}

func TestRule116HoldsWithAnIndicatorColumnAt60(t *testing.T) {
	m := indicatorTable(t, row{Name: "a", State: "running"}, row{Name: "b", State: "failed"})
	if got := m.RenderedWidth(); got != 58 {
		t.Errorf("RenderedWidth = %d, want 58", got)
	}
}

// Only IndicatorColumn may set Indicator: a view writing the field in a column
// literal would bypass the Rule 125 shape.
func TestOnlyIndicatorColumnSetsIndicator(t *testing.T) {
	var offenders []string
	err := filepath.WalkDir(sizingScannedTree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if rel, _ := filepath.Rel(sizingScannedTree, path); strings.HasPrefix(rel, "datatable"+string(filepath.Separator)) {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok {
				for _, column := range columnLiterals(lit) {
					if fieldValue(column, "Indicator") != nil {
						offenders = append(offenders, fset.Position(column.Pos()).String())
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("Indicator set in a column literal outside datatable; use IndicatorColumn:\n  %s", strings.Join(offenders, "\n  "))
	}
}
