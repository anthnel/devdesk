package dashboard

import (
	"strings"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/metrics"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Ce fichier est le seul à connaître ntcharts. La vue ne le touche jamais
// directement, et surtout ne choisit jamais entre Draw() et DrawColumnsOnly()
// sur un site d'appel.

// brailleFrom is the height at which a sparkline switches from blocks to
// braille: en dessous de trois lignes, le braille n'a pas de quoi montrer sa
// résolution verticale et rend une bouillie de points.
const brailleFrom = 3

// renderChart draws a series as a sparkline of exactly `height` lines, each
// exactly `width` cells.
//
// Le graphe ne garde **aucun** historique : il est reconstruit à chaque frame à
// partir des échantillons du modèle. C'est ce qui rend la Rule 110 vraie par
// construction — il n'y a pas de Push à placer au bon endroit — et ce qui règle
// le problème du rééchelonnage : ntcharts.Resize rééchelonne son propre ring
// buffer, donc un changement de palier tronquerait un historique qu'il
// posséderait. Ici c'est le modèle qui le possède, et le graphe n'en est qu'une
// lecture.
func renderChart(values []float64, width, height int, maxValue float64) []string {
	if width < 1 || height < 1 {
		return nil
	}
	if len(values) == 0 {
		return blankLines(width, height)
	}

	opts := []sparkline.Option{
		// Draw() habille toute la toile, donc le fond du graphe remplit aussi
		// les creux. DrawColumnsOnly() ne colore que les colonnes et laisse le
		// fond natif du terminal traverser les trous — précisément ce que la
		// Rule 115 interdit.
		sparkline.WithStyle(chartStyle()),
	}
	if maxValue > 0 {
		// Échelle fixe pour un pourcentage : sans elle, une machine au repos
		// affiche un graphe aussi haut qu'une machine saturée, parce que
		// l'échelle suit le maximum observé.
		opts = append(opts, sparkline.WithMaxValue(maxValue), sparkline.WithNoAutoMaxValue())
	}

	chart := sparkline.New(width, height, opts...)
	chart.PushAll(trimToWidth(values, width, height))
	if height >= brailleFrom {
		chart.DrawBraille()
	} else {
		chart.Draw()
	}

	lines := strings.Split(chart.View(), "\n")
	for i, line := range lines {
		lines[i] = padChart(line, width)
	}
	for len(lines) < height {
		lines = append(lines, chartBlank(width))
	}
	return lines[:height]
}

// chartStyle is the one place the chart surface is decided. Le fond est un cran
// plus clair que celui de l'application : sans lui, un graphe creux est
// indiscernable d'une boîte vide, et rien ne dit où finit la zone réservée.
func chartStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(theme.ColorPrimary).
		Background(theme.ColorChartBg)
}

// padChart pads a rendered chart line to width **with the chart's own
// background**, not the application's: theme.PadWithBg finirait la ligne sur le
// fond sombre et la zone du graphe se terminerait avant sa bordure.
func padChart(line string, width int) string {
	missing := width - lipgloss.Width(line)
	if missing <= 0 {
		return line
	}
	return line + chartBlank(missing)
}

// chartBlank is `width` cells of empty chart surface.
func chartBlank(width int) string {
	return chartStyle().Render(strings.Repeat(" ", max(width, 0)))
}

// trimToWidth keeps the newest samples the canvas can show. Un graphe braille
// tient deux échantillons par cellule, un graphe en blocs un seul.
func trimToWidth(values []float64, width, height int) []float64 {
	capacity := width
	if height >= brailleFrom {
		capacity *= 2
	}
	if len(values) > capacity {
		return values[len(values)-capacity:]
	}
	return values
}

// blankLines returns `height` lines of empty chart surface — ce qu'affiche un
// graphe sans données, plutôt qu'une ligne plate qui se lirait comme une mesure
// à zéro. La zone reste visible : elle dit qu'un graphe est attendu là.
func blankLines(width, height int) []string {
	lines := make([]string, height)
	for i := range lines {
		lines[i] = chartBlank(width)
	}
	return lines
}

// series projects one field out of the sample history.
func series(samples []metrics.HostSample, pick func(metrics.HostSample) float64) []float64 {
	out := make([]float64, 0, len(samples))
	for _, s := range samples {
		out = append(out, pick(s))
	}
	return out
}
