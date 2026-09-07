package dashboard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/metrics"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func rampTo(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(i % 101)
	}
	return out
}

// TestAChartLineIsExactlyTheColumnWidth — Rule 116. A single line that's too
// long shifts the whole column next to it, and one that's too short lets the
// terminal's native background show through.
func TestAChartLineIsExactlyTheColumnWidth(t *testing.T) {
	for _, width := range []int{20, 40, 78, 118} {
		for _, height := range []int{1, 3, 4} {
			lines := renderChart(rampTo(300), width, height, 100)
			if len(lines) != height {
				t.Errorf("a %dx%d chart rendered %d lines", width, height, len(lines))
				continue
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got != width {
					t.Errorf("a %dx%d chart's line %d is %d cells wide", width, height, i, got)
				}
			}
		}
	}
}

// TestEveryChartCellCarriesABackground — Rule 115, and this is the
// DrawColumnsOnly trap: it only colors the columns and lets the terminal's
// background show through the gaps. Draw() and DrawBraille() dress the whole
// canvas.
//
// The color profile is forced for the duration of the test: under `go test`
// there is no TTY, so lipgloss degrades to Ascii and strips *all* styling — a
// test looking for escape sequences without this would pass with
// DrawColumnsOnly too.
func TestEveryChartCellCarriesABackground(t *testing.T) {
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	// A sparse series: lots of zeros, so lots of gaps to fill.
	sparse := make([]float64, 40)
	for i := range sparse {
		if i%8 == 0 {
			sparse[i] = 90
		}
	}

	for _, height := range []int{1, 3, 4} {
		for i, line := range renderChart(sparse, 40, height, 100) {
			if !strings.Contains(line, "\x1b[") {
				t.Errorf("line %d of a %d-high chart carries no styling, so its cells show the terminal's own background: %q",
					i, height, line)
			}
			// What follows the last escape sequence must be empty: bare text
			// after it is the native background showing through.
			if tail := line[strings.LastIndex(line, "m")+1:]; tail != "" {
				t.Errorf("line %d of a %d-high chart ends with %q, outside any styling", i, height, tail)
			}
		}
	}
}

// And the other end of the same invariant: the choice between Draw and
// DrawColumnsOnly must not exist at any call site. It checks the *calls*, not
// the text, so that the comment explaining the trap can name it.
func TestNothingCallsDrawColumnsOnly(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", e.Name(), err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "DrawColumnsOnly" {
				t.Errorf("%s calls DrawColumnsOnly — it styles the columns only and lets the "+
					"terminal's background through the troughs (Rule 115)", e.Name())
			}
			return true
		})
	}
}

// TestAChartIsRebuiltFromTheModelRatherThanKeptByTheWidget — Rule 110 by
// construction: there is no Push to place, because the chart keeps nothing.
// Two successive renderings of the same model are identical, and a rendering
// does not modify the model.
func TestAChartIsRebuiltFromTheModelRatherThanKeptByTheWidget(t *testing.T) {
	m, _ := loadedModel(t)
	for i := range 50 {
		m = feed(t, m, HostSampleMsg{Sample: metrics.HostSample{OK: true, CPUPercent: float64(i)}})
	}

	before := len(m.samples)
	first := strings.Join(renderHostSection(m, 60, tierWide), "\n")
	second := strings.Join(renderHostSection(m, 60, tierWide), "\n")

	if first != second {
		t.Error("two renderings of one model differ — the chart is keeping state between frames")
	}
	if len(m.samples) != before {
		t.Errorf("rendering changed the history from %d to %d samples — View() must be read-only", before, len(m.samples))
	}
}

// A chart with no data does not draw a flat line: a flat line at zero reads
// as a measurement, and there was none.
func TestAnEmptySeriesDrawsNothing(t *testing.T) {
	for _, line := range renderChart(nil, 30, 3, 100) {
		if strings.TrimSpace(stripANSI(line)) != "" {
			t.Errorf("a chart with no samples drew %q", stripANSI(line))
		}
	}
}

// The tier decides the chart's height, and at `standard` it does not grow the
// box: the overview fits flush there.
func TestAChartDoesNotGrowTheBoxAtStandard(t *testing.T) {
	m, _ := loadedModel(t)

	if got := m.chartHeight(tierStandard); got != 1 {
		t.Errorf("a chart is %d lines at standard, want 1 — it takes the place of a blank line, not more", got)
	}
	standard := len(renderHostSection(m, 60, tierStandard))

	// At `wide`, the curves' height is measured by View() and stored in
	// chartLines; without it the rendering is the skeleton with no curve,
	// which is exactly what fitCharts measures.
	measured := m
	measured.chartLines = 6
	if wide := len(renderHostSection(measured, 60, tierWide)); wide <= standard {
		t.Errorf("the Host box is %d lines at wide against %d at standard — the chart gained nothing", wide, standard)
	}
}

// TestTheBareGridLeavesRoomForItsCharts — fitCharts renders the grid with no
// curve, observes what's left, and divides it up. The result must fit: that's
// the whole point of measuring rather than deriving from constants.
func TestTheBareGridLeavesRoomForItsCharts(t *testing.T) {
	m, _ := loadedModel(t)

	for _, height := range []int{20, 33, 47, 80} {
		at := feed(t, m, tea.WindowSizeMsg{Width: 250, Height: height})
		rendered := strings.Count(at.View(), "\n") + 1

		if at.chartLines != 0 {
			t.Errorf("View() left chartLines = %d on the model — it is a layout result, not state", at.chartLines)
		}
		if rendered > height && at.chartHeightAt(tierWide) > minBrailleHeight {
			t.Errorf("at %d lines the grid renders %d with charts of %d — the measurement did not fit",
				height, rendered, at.chartHeightAt(tierWide))
		}
	}
}

// The chart is drawn over the box's usable width, padding included: a chart
// as wide as the whole box would overflow by two cells.
func TestTheChartFitsInsideTheBoxPadding(t *testing.T) {
	m, _ := loadedModel(t)
	const boxWidth = 60

	for _, line := range renderHostSection(m, boxWidth, tierWide) {
		if got := lipgloss.Width(line); got > theme.BoxContentWidth(boxWidth) {
			t.Errorf("a Host line is %d cells wide, more than the %d a %d-wide box holds",
				got, theme.BoxContentWidth(boxWidth), boxWidth)
		}
	}
}
