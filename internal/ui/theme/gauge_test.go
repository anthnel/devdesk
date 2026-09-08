package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Gauge is now just width copies of one glyph — the value lives entirely in
// GaugeFillWidth's cut point and in which colour a caller puts on either side
// of it (§3.71, third revision). These tests are split the same way: what the
// text looks like, what the cut point says, and what colour each side takes.

// A gauge is measured by datatable before it is coloured, so its rendered
// width has to be exactly what the column declared, under an East Asian
// locale as much as under this one.
func TestAGaugeIsExactlyAsWideAsItAsksFor(t *testing.T) {
	eastAsian := &runewidth.Condition{EastAsianWidth: true}

	for _, width := range []int{1, 2, 6, 10, 20} {
		bar := Gauge(width)
		if got := runewidth.StringWidth(bar); got != width {
			t.Fatalf("Gauge(%d) measures %d cells, want %d: %q", width, got, width, bar)
		}
		if got := eastAsian.StringWidth(bar); got != width {
			t.Fatalf("Gauge(%d) measures %d cells under an East Asian locale, want %d: %q",
				width, got, width, bar)
		}
	}
}

// Gauge does not depend on a value at all: every cell is the same glyph,
// whatever the caller will go on to colour.
func TestGaugeIsUniform(t *testing.T) {
	if got, want := Gauge(6), "░░░░░░"; got != want {
		t.Errorf("Gauge(6) = %q, want %q", got, want)
	}
}

// A width of zero or less renders nothing rather than a negative repeat count.
func TestGaugeSurvivesAnEmptyWidth(t *testing.T) {
	for _, width := range []int{0, -1} {
		if got := Gauge(width); got != "" {
			t.Errorf("Gauge(%d) = %q, want the empty string", width, got)
		}
	}
}

func TestGaugeFillWidthIsProportional(t *testing.T) {
	tests := []struct {
		name string
		pct  float64
		want int
	}{
		// An idle container fills zero cells, not a token one: a tenth of a
		// ten-cell bar *is* 10%, so lighting one for 0.1% would overstate it
		// on every idle row — and idle is what most rows are.
		{"zero", 0, 0},
		{"idle", 0.4, 0},
		{"a quarter", 25, 3}, // 2.5 rounds to 3
		{"a half", 50, 5},
		{"three quarters", 75, 8}, // 7.5 rounds to 8
		{"full", 100, 10},
		// Saturation is indistinguishable from full — the number beside the
		// gauge is what tells 200% from 100%.
		{"two busy cores", 200, 10},
		// A negative or unreadable value fills nothing.
		{"negative", -10, 0},
	}

	for _, tc := range tests {
		if got := GaugeFillWidth(tc.pct, GaugeWidth); got != tc.want {
			t.Errorf("%s: GaugeFillWidth(%v, %d) = %d, want %d", tc.name, tc.pct, GaugeWidth, got, tc.want)
		}
	}
}

// A width of zero or less fills nothing, matching Gauge's own empty case.
func TestGaugeFillWidthSurvivesAnEmptyWidth(t *testing.T) {
	for _, width := range []int{0, -1} {
		if got := GaugeFillWidth(50, width); got != 0 {
			t.Errorf("GaugeFillWidth(50, %d) = %d, want 0", width, got)
		}
	}
}

// Three levels for the fill. Green under the first threshold is a declared
// exception to Rule 122's colour discipline, on Rule 128's ground (one
// severity alphabet across the application) rather than this rule's own —
// see .claude/rules/tui-tables.md.
func TestLoadTextStyleTakesItsColourFromTheLevel(t *testing.T) {
	tests := []struct {
		pct  float64
		want lipgloss.Color
	}{
		{0, ColorOK},
		{LoadWarnPercent - 0.1, ColorOK},
		{LoadWarnPercent, ColorSeverityMedium},
		{LoadCriticalPercent - 0.1, ColorSeverityMedium},
		{LoadCriticalPercent, ColorSeverityCritical},
		{400, ColorSeverityCritical},
	}

	for _, tc := range tests {
		if got := LoadTextStyle(tc.pct).GetForeground(); got != lipgloss.TerminalColor(tc.want) {
			t.Errorf("LoadTextStyle(%v) foreground = %v, want %v", tc.pct, got, tc.want)
		}
	}
}

// The track's colour is fixed and takes no argument — the whole point being
// that it never changes with the level next to it.
func TestGaugeTrackStyleIsFixed(t *testing.T) {
	if got := GaugeTrackStyle().GetForeground(); got != lipgloss.TerminalColor(ColorSeverityLow) {
		t.Errorf("GaugeTrackStyle foreground = %v, want %v", got, ColorSeverityLow)
	}
}

// The gauge glyph is pinned here rather than in a comment: swapping it for a
// block element would pass every other test in this file and overflow the
// column on a terminal nobody in this repository runs.
func TestTheGaugeGlyphIsNotAmbiguousWidth(t *testing.T) {
	for _, r := range gaugeGlyph {
		if runewidth.IsAmbiguousWidth(r) {
			t.Errorf("gauge glyph %q (U+%04X) is East Asian ambiguous: it renders double-width on some terminals and breaks Rule 116", string(r), r)
		}
	}
	if strings.ContainsAny(gaugeGlyph, "█▓▒▉▊▋▌▍▎▏▁▂▃▄▅▆▇") {
		t.Error("a different Block Elements glyph is in gaugeGlyph; every one but ░ is ambiguous-width")
	}
}
