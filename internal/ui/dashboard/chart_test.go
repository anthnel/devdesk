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

// TestAChartLineIsExactlyTheColumnWidth — Rule 116. Une seule ligne trop longue
// décale toute la colonne d'à côté, et une trop courte laisse passer le fond
// natif du terminal.
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

// TestEveryChartCellCarriesABackground — Rule 115, et c'est le piège
// DrawColumnsOnly : elle ne colore que les colonnes et laisse le fond du
// terminal traverser les creux. Draw() et DrawBraille() habillent toute la
// toile.
//
// Le profil de couleur est forcé le temps du test : sous `go test` il n'y a pas
// de TTY, donc lipgloss dégrade en Ascii et supprime *tout* le style — un test
// qui chercherait des séquences sans ça passerait avec DrawColumnsOnly aussi.
func TestEveryChartCellCarriesABackground(t *testing.T) {
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	// Une série creuse : beaucoup de zéros, donc beaucoup de creux à remplir.
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
			// Ce qui suit la dernière séquence doit être vide : du texte nu
			// après elle est du fond natif qui traverse.
			if tail := line[strings.LastIndex(line, "m")+1:]; tail != "" {
				t.Errorf("line %d of a %d-high chart ends with %q, outside any styling", i, height, tail)
			}
		}
	}
}

// Et l'autre bout du même invariant : le choix entre Draw et DrawColumnsOnly ne
// doit exister sur aucun site d'appel. Il porte sur les *appels*, pas sur le
// texte, pour que le commentaire qui explique le piège puisse le nommer.
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

// TestAChartIsRebuiltFromTheModelRatherThanKeptByTheWidget — la Rule 110 par
// construction : il n'y a pas de Push à placer, parce que le graphe ne garde
// rien. Deux rendus successifs du même modèle sont identiques, et un rendu ne
// modifie pas le modèle.
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

// Un graphe sans données ne trace pas une ligne plate : une ligne plate à zéro
// se lit comme une mesure, or il n'y en a pas eu.
func TestAnEmptySeriesDrawsNothing(t *testing.T) {
	for _, line := range renderChart(nil, 30, 3, 100) {
		if strings.TrimSpace(stripANSI(line)) != "" {
			t.Errorf("a chart with no samples drew %q", stripANSI(line))
		}
	}
}

// Le palier décide de la hauteur du graphe, et à `standard` il ne fait pas
// grandir la boîte : l'overview y tient au ras.
func TestAChartDoesNotGrowTheBoxAtStandard(t *testing.T) {
	m, _ := loadedModel(t)

	if got := m.chartHeight(tierStandard); got != 1 {
		t.Errorf("a chart is %d lines at standard, want 1 — it takes the place of a blank line, not more", got)
	}
	standard := len(renderHostSection(m, 60, tierStandard))

	// À `wide`, la hauteur des courbes est mesurée par View() et rangée dans
	// chartLines ; sans elle le rendu est le squelette sans courbe, qui est
	// justement ce que fitCharts mesure.
	measured := m
	measured.chartLines = 6
	if wide := len(renderHostSection(measured, 60, tierWide)); wide <= standard {
		t.Errorf("the Host box is %d lines at wide against %d at standard — the chart gained nothing", wide, standard)
	}
}

// TestTheBareGridLeavesRoomForItsCharts — fitCharts rend la grille sans courbe,
// constate ce qui reste et le partage. Le résultat doit tenir : c'est tout
// l'intérêt de mesurer plutôt que de déduire de constantes.
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

// Le graphe est tracé sur la largeur utile de la boîte, padding compris : un
// graphe large de la boîte entière déborderait de deux cellules.
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
