package theme

import (
	"image/color"
	"testing"
)

// DEVDESK-764: every text colour of the palette reaches WCAG 1.4.3 (4.5:1) on
// the surface it is really painted on, on each of the 13 shipped themes. The
// measure of 2026-11-23 (docs/measurements/2026-11-23-sec-detail-contrast.md)
// found ColorDim, ColorSubTitleFg, ColorWarn and ColorFooterError below it.
//
// Surfaces, and why each colour is measured on the ones it has:
//
//   - background: ColorBackground, under everything (Rule 115).
//   - selected row: ColorTableLineSelected. A plain "normal" selection keeps
//     every cell's own foreground and repaints this background under it
//     (Rule 118, Rule 122), so any colour a table cell can take is read on it.
//   - raised surface: ColorCmdLineBg, which the chart, the inactive tab and the
//     disabled button sit on. It is the same hex as the selected row on every
//     shipped theme, but it is its own key, so it is measured on its own.
//
// ColorSubTitleFg only heads a block of text on the background. The footer
// colours are text on the background (the footer line); under the cursor they
// are the activity dot and the severity icon, a glyph held to 3:1 (WCAG 1.4.11).
const (
	textMinContrast  = 4.5 // WCAG 1.4.3, text
	glyphMinContrast = 3.0 // WCAG 1.4.11, graphical object
)

type textInk struct {
	what string
	fg   func() color.Color
	// on lists the surfaces the colour is painted on and the ratio each needs.
	on []surfaceNeed
}

type surfaceNeed struct {
	surface string
	bg      func() color.Color
	min     float64
}

var (
	onBackground = func(min float64) surfaceNeed {
		return surfaceNeed{"ColorBackground", func() color.Color { return ColorBackground }, min}
	}
	onSelectedRow = func(min float64) surfaceNeed {
		return surfaceNeed{"ColorTableLineSelected", func() color.Color { return ColorTableLineSelected }, min}
	}
	onRaised = func(min float64) surfaceNeed {
		return surfaceNeed{"ColorCmdLineBg", func() color.Color { return ColorCmdLineBg }, min}
	}
)

func textInks() []textInk {
	return []textInk{
		{"ColorText", func() color.Color { return ColorText },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(textMinContrast), onRaised(textMinContrast)}},
		{"ColorDim", func() color.Color { return ColorDim },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(textMinContrast), onRaised(textMinContrast)}},
		{"ColorSubTitleFg", func() color.Color { return ColorSubTitleFg },
			[]surfaceNeed{onBackground(textMinContrast)}},
		{"ColorWarn", func() color.Color { return ColorWarn },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(textMinContrast)}},
		{"ColorFooterWarn", func() color.Color { return ColorFooterWarn },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(glyphMinContrast)}},
		{"ColorFooterError", func() color.Color { return ColorFooterError },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(glyphMinContrast)}},
	}
}

// Each failure names the theme, the colour, the surface and the measured ratio.
func TestEveryTextColourReachesItsContrastOnEveryTheme(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		for _, ink := range textInks() {
			for _, need := range ink.on {
				fg, bg := ink.fg(), need.bg()
				ratio, err := ContrastRatio(fg, bg)
				if err != nil {
					t.Errorf("theme %s: %s on %s: %v", name, ink.what, need.surface, err)
					continue
				}
				if ratio < need.min {
					t.Errorf("theme %s: %s %s on %s %s is %.2f:1, want >= %.1f:1",
						name, ink.what, Hex(fg), need.surface, Hex(bg), ratio, need.min)
				}
			}
		}
	})
}

// A greyed control still has to be read: raising ColorDim is not enough when the
// label sits on a background of the same hue. The disabled button used to paint
// ColorDim on ColorButtonInactiveBg, which is the same hex on nine themes (1:1).
func TestTheDisabledButtonLabelReachesTextContrastOnEveryTheme(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		for _, focused := range []bool{false, true} {
			s := ButtonStyle(focused, "disabled")
			fg, bg := s.GetForeground(), s.GetBackground()
			ratio, err := ContrastRatio(fg, bg)
			if err != nil {
				t.Errorf("theme %s: disabled button (focused=%v): %v", name, focused, err)
				continue
			}
			if ratio < textMinContrast {
				t.Errorf("theme %s: disabled button (focused=%v) label %s on %s is %.2f:1, want >= %.1f:1",
					name, focused, Hex(fg), Hex(bg), ratio, textMinContrast)
			}
		}
	})
}

// Grey says "not now", and colour alone must not be the only thing that says it
// (WCAG 1.4.1): a disabled button is italic, an enabled one is not, and the
// focus mark of a disabled button is an inversion rather than a colour only.
func TestTheDisabledButtonDiffersFromAnEnabledOneByMoreThanColour(t *testing.T) {
	withTheme(t, DefaultTheme())
	for _, focused := range []bool{false, true} {
		dis, en := ButtonStyle(focused, "disabled"), ButtonStyle(focused, "primary")
		if !dis.GetItalic() {
			t.Errorf("focused=%v: the disabled button is not italic", focused)
		}
		if en.GetItalic() {
			t.Errorf("focused=%v: the enabled button is italic: the signal would carry nothing", focused)
		}
	}
	if on, off := ButtonStyle(true, "disabled"), ButtonStyle(false, "disabled"); sameColor(on.GetBackground(), off.GetBackground()) {
		t.Error("a focused disabled button keeps the unfocused background: the focus would be invisible")
	}
}

// Rule 118: an inactive tab is ColorDim on the tab's own background. The theme
// files used to set tab_inactive_fg on its own, to a tone that was never held to
// 4.5:1 (1.4:1 on catppuccin-latte).
func TestTheInactiveTabLabelReachesTextContrastOnEveryTheme(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		ratio, err := ContrastRatio(ColorTabInactiveFg, ColorTabInactiveBg)
		if err != nil {
			t.Fatal(err)
		}
		if ratio < textMinContrast {
			t.Errorf("theme %s: ColorTabInactiveFg %s on ColorTabInactiveBg %s is %.2f:1, want >= %.1f:1",
				name, Hex(ColorTabInactiveFg), Hex(ColorTabInactiveBg), ratio, textMinContrast)
		}
	})
}
