package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// A gauge is measured by datatable before it is coloured, so its rendered
// width has to be exactly what the column declared — at every value, and under
// an East Asian locale as much as under this one. That is the whole reason the
// fill glyphs are braille rather than block elements.
func TestAGaugeIsExactlyAsWideAsItAsksFor(t *testing.T) {
	eastAsian := &runewidth.Condition{EastAsianWidth: true}

	for _, width := range []int{1, 2, 6, 20} {
		for pct := -50.0; pct <= 150.0; pct += 0.5 {
			bar := Gauge(pct, width)
			if got := runewidth.StringWidth(bar); got != width {
				t.Fatalf("Gauge(%.1f, %d) measures %d cells, want %d: %q", pct, width, got, width, bar)
			}
			if got := eastAsian.StringWidth(bar); got != width {
				t.Fatalf("Gauge(%.1f, %d) measures %d cells under an East Asian locale, want %d: %q",
					pct, width, got, width, bar)
			}
		}
	}
}

func TestGaugeFillsInProportion(t *testing.T) {
	tests := []struct {
		name string
		pct  float64
		want string
	}{
		// An idle container renders an empty frame, not a token sliver: half a
		// cell out of six is 8%, and drawing one for 0.1% would overstate it on
		// every row of a table where idle is the majority state.
		{"zero", 0, "[      ]"},
		{"idle", 0.4, "[      ]"},
		{"a quarter", 25, "[⣿⡇    ]"},
		{"a half", 50, "[⣿⣿⣿   ]"},
		{"three quarters", 75, "[⣿⣿⣿⣿⡇ ]"},
		{"full", 100, "[⣿⣿⣿⣿⣿⣿]"},
		// Saturation is indistinguishable from full — the number beside the
		// gauge is what tells 200% from 100%.
		{"two busy cores", 200, "[⣿⣿⣿⣿⣿⣿]"},
		// A negative or unreadable value is not a full bar.
		{"negative", -10, "[      ]"},
	}

	for _, tc := range tests {
		if got := Gauge(tc.pct, GaugeWidth); got != tc.want {
			t.Errorf("%s: Gauge(%v, %d) = %q, want %q", tc.name, tc.pct, GaugeWidth, got, tc.want)
		}
	}
}

// A width with no room for a bar inside its frame renders blanks rather than
// panicking on a negative repeat count: a column narrow enough to have been
// dropped still calls Cell while the widths are being measured.
func TestGaugeSurvivesAWidthTooNarrowToFrame(t *testing.T) {
	for _, width := range []int{0, -1} {
		if got := Gauge(50, width); got != "" {
			t.Errorf("Gauge(50, %d) = %q, want the empty string", width, got)
		}
	}
	for width, want := range map[int]string{1: " ", 2: "  "} {
		if got := Gauge(50, width); got != want {
			t.Errorf("Gauge(50, %d) = %q, want %q — no room for a bar between the brackets", width, got, want)
		}
	}
}

// Three levels, and green is the declared exception to Rule 122's colour
// discipline: in a framed bar the fill has to be told from the frame, which is
// the one thing an uncoloured bar cannot do (see LoadTextStyle).
func TestAGaugeTakesItsColourFromItsLevel(t *testing.T) {
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

// The gauge glyphs are pinned here rather than in a comment: swapping one for a
// block element would pass every other test in this file and overflow the
// column on a terminal nobody in this repository runs.
func TestTheFillGlyphsAreNotAmbiguousWidth(t *testing.T) {
	for _, glyph := range []string{gaugeFull, gaugeHalf, gaugeEmpty} {
		for _, r := range glyph {
			if runewidth.IsAmbiguousWidth(r) {
				t.Errorf("gauge glyph %q (U+%04X) is East Asian ambiguous: it renders double-width on some terminals and breaks Rule 116", string(r), r)
			}
		}
	}
	for _, glyph := range []string{gaugeOpen, gaugeClose} {
		for _, r := range glyph {
			if r > 127 {
				t.Errorf("gauge frame glyph %q (U+%04X) is not ASCII: the box-drawing candidates are all ambiguous-width", string(r), r)
			}
		}
	}
	if strings.ContainsAny(gaugeFull+gaugeHalf+gaugeEmpty, "█▓▒▉▊▋▌▍▎▏▁▂▃▄▅▆▇") {
		t.Error("a Block Elements glyph is back in the gauge; every one of them is ambiguous-width")
	}
}
