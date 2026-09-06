package dashboard

import (
	"strings"

	"github.com/NimbleMarkets/ntcharts/sparkline"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/metrics"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// This file is the only one that knows about ntcharts. The view never touches
// it directly, and above all never chooses between Draw() and DrawColumnsOnly()
// at a call site.

// brailleFrom is the height at which a sparkline switches from blocks to
// braille: below three lines, braille has no room to show its vertical
// resolution and renders a mush of dots.
const brailleFrom = 3

// renderChart draws a series as a sparkline of exactly `height` lines, each
// exactly `width` cells.
//
// The chart keeps **no** history at all: it is rebuilt every frame from the
// model's samples. That's what makes Rule 110 true by construction — there is
// no Push to place at the right spot — and it's what settles the rescaling
// problem: ntcharts.Resize rescales its own ring buffer, so a threshold change
// would truncate a history it owned. Here it's the model that owns it, and the
// chart is only a reading of it.
func renderChart(values []float64, width, height int, maxValue float64) []string {
	if width < 1 || height < 1 {
		return nil
	}
	if len(values) == 0 {
		return blankLines(width, height)
	}

	opts := []sparkline.Option{
		// Draw() dresses the whole canvas, so the chart background also fills
		// the gaps. DrawColumnsOnly() only colors the columns and lets the
		// terminal's native background show through the holes — exactly what
		// Rule 115 forbids.
		sparkline.WithStyle(chartStyle()),
	}
	if maxValue > 0 {
		// Fixed scale for a percentage: without it, an idle machine displays a
		// chart as tall as a saturated machine, because the scale follows the
		// observed maximum.
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

// chartStyle is the one place the chart surface is decided. The background is
// one notch lighter than the application's: without it, an empty chart is
// indistinguishable from an empty box, and nothing says where the reserved
// area ends.
func chartStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(theme.ColorPrimary).
		Background(theme.ColorChartBg)
}

// padChart pads a rendered chart line to width **with the chart's own
// background**, not the application's: theme.PadWithBg would end the line on
// the dark background and the chart area would end before its border.
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

// trimToWidth keeps the newest samples the canvas can show. A braille chart
// holds two samples per cell, a block chart only one.
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

// blankLines returns `height` lines of empty chart surface — what a chart with
// no data displays, rather than a flat line that would read as a zero
// measurement. The area stays visible: it says a chart is expected there.
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
