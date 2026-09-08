package theme

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// A load gauge: a bar that answers "is something hot" without reading a
// number (§3.71).
//
// It renders plain text and nothing else. Rule 122 is why: a gradient along
// the bar — green cells then orange ones inside the same cell — would need
// ANSI sequences inside what Cell returns, runewidth counts those as width,
// and the cut lands mid-sequence and bleeds over every row below. So the
// length carries the value, and LoadTextStyle gives the whole cell one colour.

// The glyphs, pinned by measurement rather than by taste.
//
// Every glyph in the Block Elements range — `█`, `▓`, `▇`, and the partial
// blocks `▏▎▍▌` — is East Asian *ambiguous*: runewidth measures it 1 here and
// 2 under an East Asian locale, so a terminal that renders it double-width
// makes the row overflow its column and breaks Rule 116. Measured, not
// assumed: `▓` and `▉` come back ambiguous, `░` does not — it is a Block
// Elements glyph too, and the one exception in the family.
//
// Braille is the one other family that measures 1 in both conditions, and it
// is already the alphabet the dashboard draws its curves in — so the gauge's
// fill is in the same visual family as the charts rather than in one of its
// own. The track is `░` rather than a blank cell: a bar with nothing behind
// the fill reads as a value that has not been measured yet (the same blank a
// dropped column or an unmeasured cell would leave), where `░` reads as a
// value that has been measured and found low.
const (
	gaugeFull  = "⣿" // eight dots: a whole cell
	gaugeHalf  = "⡇" // the left column of dots: half a cell
	gaugeEmpty = "░" // the track: measured, not assumed, to be non-ambiguous too
)

// GaugeWidth is the width of a gauge column, in cells.
//
// Six, which buys twelve steps at half-cell resolution — enough to tell a
// quarter from a third at a glance, which is all a bar is for. The number it
// sits beside carries the precision.
const GaugeWidth = 6

// Load thresholds, in percent: green, then orange, then red.
//
// Green under the first threshold is a **declared exception** to Rule 122's
// colour discipline. The rule's own criterion is "a column where the absence
// of colour is already taken" — the CI grades' reason — and here the glyph
// already tells a full cell from an empty one, so the exception is not made on
// that ground. It is made on the ground Rule 128 uses for the footer levels:
// one alphabet of severity across the application, so a nearly-full gauge
// reads like a CRITICAL finding without having to read the number beside it.
// It is the same green `ColorOK` gives the CI grades.
//
// The cost is stated rather than discovered: most containers idle near zero,
// so most rows carry a short green sliver rather than the ordinary text
// colour Rule 122 would otherwise assign the majority state.
const (
	// LoadWarnPercent is where a gauge stops being ordinary: filling up, and
	// worth noticing.
	LoadWarnPercent = 75
	// LoadCriticalPercent is where it is nearly full — the memory gauge's
	// reading of a container about to meet its limit.
	LoadCriticalPercent = 90
)

// Gauge renders pct of full as a bar of width cells, as plain text.
//
// pct is clamped: anything at or above 100 fills the bar. Saturation is
// therefore indistinguishable from exactly full, which is deliberate and is
// the reason a gauge never replaces the number beside it — `docker stats`
// counts CPU against one core, so two busy cores read 200% and only the
// number tells the two apart (§3.71).
//
// Rounding is honest in the other direction too: a container at 0.1% renders
// an empty track rather than a token sliver. Half a cell out of six *is* 8%,
// so drawing one for a value near zero would overstate it on every idle row —
// and idle is what most rows are.
func Gauge(pct float64, width int) string {
	if width <= 0 {
		return ""
	}
	switch {
	case math.IsNaN(pct), pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}

	halves := int(math.Round(pct / 100 * float64(width*2)))
	full, half := halves/2, halves%2

	var b strings.Builder
	b.WriteString(strings.Repeat(gaugeFull, full))
	if half > 0 {
		b.WriteString(gaugeHalf)
		full++
	}
	b.WriteString(strings.Repeat(gaugeEmpty, width-full))
	return b.String()
}

// LoadTextStyle is the colour a gauge takes at pct: green, then orange, then
// red. It follows SeverityTextStyle's shape — the view names a value, the theme
// answers with a colour — and it aliases the severity palette for the same
// reason the footer levels do (Rule 128): one alphabet across the application,
// so a full gauge reads like a CRITICAL finding.
//
// It is a function rather than a package-level style because the thresholds
// are the whole of it; a var per level would leave the switch in the view,
// which is where Rule 125 says a colour must not be decided.
func LoadTextStyle(pct float64) lipgloss.Style {
	base := lipgloss.NewStyle().Background(ColorBackground)
	switch {
	case pct >= LoadCriticalPercent:
		return base.Foreground(ColorSeverityCritical).Bold(true)
	case pct >= LoadWarnPercent:
		return base.Foreground(ColorSeverityMedium)
	default:
		return base.Foreground(ColorOK)
	}
}
