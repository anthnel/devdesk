package theme

import (
	"image/color"
	"testing"
)

// DEVDESK-751, AC7 (decision Q1-A): the Fix block of a finding detail is written
// in ColorText alone, its labels in DetailLabelStyle (bold, ColorText), and the
// word "Warning:" carries the warning. ColorDim, ColorSubTitleFg, ColorTitleFg,
// ColorWarn and ColorFooterError fall under 4.5:1 on several of the 13 shipped
// themes (measured on 2026-11-23), so none of them is used there. ColorDim is
// left as it is: it marks a greyed key, an inactive component that WCAG 1.4.3
// exempts.

// detailMinContrast is WCAG 1.4.3 for text.
const detailMinContrast = 4.5

// Every style of the block reads on every shipped theme.
func TestTheFixBlockStylesReachTextContrastOnEveryTheme(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		for _, ink := range []struct {
			what string
			fg   color.Color
		}{
			{"ColorText", ColorText},
			{"DetailLabelStyle", DetailLabelStyle.GetForeground()},
		} {
			ratio, err := ContrastRatio(ink.fg, ColorBackground)
			if err != nil {
				t.Errorf("theme %s: %s: %v", name, ink.what, err)
				continue
			}
			if ratio < detailMinContrast {
				t.Errorf("theme %s: %s %s on the background %s is %.2f:1, want >= %.1f:1",
					name, ink.what, Hex(ink.fg), Hex(ColorBackground), ratio, detailMinContrast)
			}
		}
	})
}

// The label is bold, in the text colour, on the theme's background, and it follows
// the theme that is applied: a style declared once, at the top of styles.go, keeps
// the colours of the default theme after ApplyTheme.
func TestDetailLabelStyleIsBoldTextColourOnTheBackgroundForEveryTheme(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		if !DetailLabelStyle.GetBold() {
			t.Errorf("theme %s: DetailLabelStyle is not bold: the label would carry nothing but its colour", name)
		}
		if got := DetailLabelStyle.GetForeground(); !sameColor(got, ColorText) {
			t.Errorf("theme %s: DetailLabelStyle foreground = %s, want ColorText %s", name, Hex(got), Hex(ColorText))
		}
		if got := DetailLabelStyle.GetBackground(); !sameColor(got, ColorBackground) {
			t.Errorf("theme %s: DetailLabelStyle background = %s, want ColorBackground %s", name, Hex(got), Hex(ColorBackground))
		}
	})
}
