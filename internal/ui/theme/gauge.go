package theme

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// A load gauge: a bar that answers "is something hot" without reading a
// number (§3.71).
//
// Every cell is the same glyph — `░` — used or not. What used to tell a
// filled cell from an empty one by shape now does it by colour alone:
// LoadTextStyle on the fill, GaugeTrackStyle on the track, split by
// datatable.Column.Cut after the text is already measured (Rule 122). A
// value survives the row disappearing under the cursor only through the
// number beside the gauge — Style is not consulted on the selected row, and
// with one glyph the bar itself has nothing left to say once its colour is
// gone.

// gaugeGlyph is the one character a gauge ever renders. `░` is a Block
// Elements glyph, and the *only* one of the family that measures 1 cell in
// both the default and the East Asian `runewidth` condition — every other
// glyph in the range (`█`, `▓`, `▇`, the partial blocks `▏▎▍▌`) is *ambiguous*
// and renders double-width on some terminals, which would overflow the
// column and break Rule 116. Measured, not assumed.
const gaugeGlyph = "░"

// GaugeWidth is the width of a gauge column, in cells.
//
// Ten: with a single glyph carrying no shape of its own, resolution is one
// cell per `GaugeWidth`th of the scale — 10% here — and a wider bar reads
// better than a narrower one at the same resolution the old half-braille
// design gave for six. The number beside it still carries the exact value.
const GaugeWidth = 10

// Load thresholds, in percent: green, then orange, then red — the colour a
// *filled* cell takes; GaugeTrackStyle below is what an *empty* one always
// takes, whatever the level.
const (
	// LoadWarnPercent is where a gauge stops being ordinary: filling up, and
	// worth noticing.
	LoadWarnPercent = 75
	// LoadCriticalPercent is where it is nearly full — the memory gauge's
	// reading of a container about to meet its limit.
	LoadCriticalPercent = 90
)

// Gauge renders a bar of width cells, as plain text.
//
// Every cell is gaugeGlyph: the value lives entirely in how many of them a
// caller colours with LoadTextStyle rather than GaugeTrackStyle (see
// GaugeFillWidth), not in what Cell returns — which is why this function no
// longer takes a percentage. That split happens after measurement, through
// datatable.Column.Cut, never inside this string.
func Gauge(width int) string {
	if width <= 0 {
		return ""
	}
	return strings.Repeat(gaugeGlyph, width)
}

// GaugeFillWidth reports how many of Gauge's width cells count as filled —
// the boundary datatable.Column.Cut needs to hand LoadTextStyle the first
// cells and GaugeTrackStyle the rest.
//
// pct is clamped: anything at or above 100 fills the bar. Saturation is
// therefore indistinguishable from exactly full, which is deliberate and is
// the reason a gauge never replaces the number beside it — `docker stats`
// counts CPU against one core, so two busy cores read 200% and only the
// number tells the two apart (§3.71).
//
// Rounding is honest at the low end too: a container at 0.1% fills zero
// cells rather than one. A tenth of the bar *is* 10%, so lighting one for a
// value near zero would overstate it on every idle row — and idle is what
// most rows are.
func GaugeFillWidth(pct float64, width int) int {
	if width <= 0 {
		return 0
	}
	switch {
	case math.IsNaN(pct), pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	return int(math.Round(pct / 100 * float64(width)))
}

// LoadTextStyle is the colour a gauge's *filled* cells take at pct: green,
// then orange, then red. It follows SeverityTextStyle's shape — the view
// names a value, the theme answers with a colour — and it aliases the
// severity palette for the same reason the footer levels do (Rule 128): one
// alphabet across the application, so a full gauge reads like a CRITICAL
// finding.
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

// GaugeTrackStyle is the colour a gauge's *empty* cells take — fixed,
// whatever the level, including a gauge that is entirely empty: the cell is
// still `░`, still coloured, never blank. It aliases `ColorSeverityLow`, the
// mutest tone the severity palette already carries, rather than `DimStyle`:
// `DimStyle` is this application's word for *absent* (a `-`, a zero, a
// placeholder), and an empty gauge cell is not absent — it is measured and
// low, which a severity colour says and a grey does not.
func GaugeTrackStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(ColorBackground).Foreground(ColorSeverityLow)
}
