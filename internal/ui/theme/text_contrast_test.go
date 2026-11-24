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
	// key is the theme-file key that sets the colour ("" for a closed alias),
	// read by TestSuggestTones only.
	key string
	fg  func() color.Color
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

// DEVDESK-770 part A: the colours the 2026-11-24 measure left under the bar.
// ColorSecondary is the header keys, the table header, the active-tab background
// and the activity dot, the update mark and the icons (a glyph, 3:1 on the
// selected row); ColorTableHeaderFg, ColorPrimary, ColorTitleFg and
// ColorAppTitleFg are text on the background. ColorViewportBorder is the frame
// border: a non-text object, 3:1 (WCAG 1.4.11) on the background it is drawn on
// (it falls back to ColorBorder, which is never painted on its own).
func textInks() []textInk {
	allSurfaces := []surfaceNeed{onBackground(textMinContrast), onSelectedRow(textMinContrast), onRaised(textMinContrast)}
	return []textInk{
		{"ColorText", "color_text", func() color.Color { return ColorText }, allSurfaces},
		{"ColorDim", "color_dim", func() color.Color { return ColorDim }, allSurfaces},
		{"ColorSubTitleFg", "subtitle_fg", func() color.Color { return ColorSubTitleFg },
			[]surfaceNeed{onBackground(textMinContrast)}},
		{"ColorWarn", "color_warn", func() color.Color { return ColorWarn },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(textMinContrast)}},
		{"ColorFooterWarn", "severity_medium", func() color.Color { return ColorFooterWarn },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(glyphMinContrast)}},
		{"ColorFooterError", "severity_critical", func() color.Color { return ColorFooterError },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(glyphMinContrast)}},
		{"ColorSecondary", "color_secondary", func() color.Color { return ColorSecondary },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(glyphMinContrast)}},
		{"ColorTableHeaderFg", "table_header_fg", func() color.Color { return ColorTableHeaderFg },
			[]surfaceNeed{onBackground(textMinContrast)}},
		{"ColorPrimary", "color_primary", func() color.Color { return ColorPrimary },
			[]surfaceNeed{onBackground(textMinContrast)}},
		{"ColorTitleFg", "title_fg", func() color.Color { return ColorTitleFg },
			[]surfaceNeed{onBackground(textMinContrast)}},
		{"ColorAppTitleFg", "app_title_fg", func() color.Color { return ColorAppTitleFg },
			[]surfaceNeed{onBackground(textMinContrast)}},
		// DEVDESK-770 part B (D181): the HIGH and LOW badge backgrounds. As a
		// colour they are painted on the background and, under the cursor, on the
		// selected row, where a glyph is held to 3:1 (Rule 118). The badge text is
		// ColorBackground, so the 4.5:1 here is also the badge's own ratio.
		{"ColorSeverityHigh", "severity_high", func() color.Color { return ColorSeverityHigh },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(glyphMinContrast)}},
		{"ColorSeverityLow", "severity_low", func() color.Color { return ColorSeverityLow },
			[]surfaceNeed{onBackground(textMinContrast), onSelectedRow(glyphMinContrast)}},
		{"ColorViewportBorder (frame border, non-text)", "viewport_border", func() color.Color { return ColorViewportBorder },
			[]surfaceNeed{onBackground(glyphMinContrast)}},
	}
}

// textPair is a foreground painted on a background that is not one of the
// shared surfaces: both come from the same component.
type textPair struct {
	what         string
	fgKey, bgKey string
	min          float64
	get          func() (fg, bg color.Color)
}

func styleColours(focused bool, variant string) func() (color.Color, color.Color) {
	return func() (color.Color, color.Color) {
		s := ButtonStyle(focused, variant)
		return s.GetForeground(), s.GetBackground()
	}
}

// DEVDESK-770 part A. The four severity badges are held by
// TestEverySeverityBadgeReachesTextContrastOnEveryTheme (part B).
func textPairs() []textPair {
	return []textPair{
		{"active tab (ColorTabActiveFg on ColorTabActiveBg)", "tab_active_fg", "tab_active_bg", textMinContrast,
			func() (color.Color, color.Color) { return ColorTabActiveFg, ColorTabActiveBg }},
		{"primary button, focused", "button_fg", "button_bg", textMinContrast, styleColours(true, "primary")},
		{"primary button, unfocused", "button_inactive_fg", "button_inactive_bg", textMinContrast, styleColours(false, "primary")},
		{"secondary button, unfocused", "button_inactive_fg", "button_inactive_bg", textMinContrast, styleColours(false, "secondary")},
		{"command line, inactive (ColorCmdLineInactiveFg on ColorCmdLineBg)", "cmd_line_inactive_fg", "cmd_line_bg", textMinContrast,
			func() (color.Color, color.Color) { return ColorCmdLineInactiveFg, ColorCmdLineBg }},
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

// Each failure names the theme, the pair, both colours and the measured ratio.
func TestEveryTextPairReachesItsContrastOnEveryTheme(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		for _, p := range textPairs() {
			fg, bg := p.get()
			ratio, err := ContrastRatio(fg, bg)
			if err != nil {
				t.Errorf("theme %s: %s: %v", name, p.what, err)
				continue
			}
			if ratio < p.min {
				t.Errorf("theme %s: %s, %s on %s is %.2f:1, want >= %.1f:1",
					name, p.what, Hex(fg), Hex(bg), ratio, p.min)
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

// severityLevels are the four levels a badge exists for. INFO and UNKNOWN share
// a muted badge that is not part of this decision (D179, "Not decided here").
var severityLevels = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// DEVDESK-770 part B (D181). A severity badge is the selected row of a finding
// of that level: TableStylesForSeverity(level).Selected paints the level's
// foreground on the level's background. The ratio is read from the style the
// table really uses, so a badge cannot pass on a pair the screen never shows.
// Each failure names the theme, the level, both colours and the ratio.
func TestEverySeverityBadgeReachesTextContrastOnEveryTheme(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		for _, level := range severityLevels {
			sel := TableStylesForSeverity(level).Selected
			fg, bg := sel.GetForeground(), sel.GetBackground()
			ratio, err := ContrastRatio(fg, bg)
			if err != nil {
				t.Errorf("theme %s: %s badge: %v", name, level, err)
				continue
			}
			if ratio < textMinContrast {
				t.Errorf("theme %s: %s badge, %s on %s is %.2f:1, want >= %.1f:1",
					name, level, Hex(fg), Hex(bg), ratio, textMinContrast)
			}
		}
		// ColorFooterError and ColorFooterWarn alias the CRITICAL and MEDIUM badge
		// backgrounds, so the badge foregrounds have to read on them too.
		aliased := []struct {
			what   string
			fg, bg color.Color
		}{
			{"CRITICAL badge (ColorSeverityCriticalFg on ColorFooterError)", ColorSeverityCriticalFg, ColorFooterError},
			{"MEDIUM badge (ColorSeverityMediumFg on ColorFooterWarn)", ColorSeverityMediumFg, ColorFooterWarn},
		}
		for _, p := range aliased {
			ratio, err := ContrastRatio(p.fg, p.bg)
			if err != nil {
				t.Errorf("theme %s: %s: %v", name, p.what, err)
				continue
			}
			if ratio < textMinContrast {
				t.Errorf("theme %s: %s, %s on %s is %.2f:1, want >= %.1f:1",
					name, p.what, Hex(p.fg), Hex(p.bg), ratio, textMinContrast)
			}
		}
	})
}

// The text of a badge is the application background, on every level and every
// theme: a light badge carries dark text and a dark one light text, which is
// what CRITICAL and MEDIUM already do. It makes the foreground follow the
// theme's own surface instead of a tone nobody holds to a ratio, and it leaves
// one number to move per badge (its background). INFO is left out: its badge is
// not part of D181.
func TestSeverityBadgeTextIsTheAppBackgroundOnEveryTheme(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		badges := []struct {
			level string
			fg    color.Color
		}{
			{"CRITICAL", ColorSeverityCriticalFg},
			{"HIGH", ColorSeverityHighFg},
			{"MEDIUM", ColorSeverityMediumFg},
			{"LOW", ColorSeverityLowFg},
		}
		for _, b := range badges {
			if Hex(b.fg) != Hex(ColorBackground) {
				t.Errorf("theme %s: %s badge text is %s, want ColorBackground %s",
					name, b.level, Hex(b.fg), Hex(ColorBackground))
			}
		}
	})
}

// minSeverityGap is the smallest CIE76 distance CRITICAL and HIGH keep on a
// shipped theme. Measured on 2026-11-24 before part B: the closest pair is
// catppuccin-macchiato at 8.9 (frappe 11.3, latte 11.7, mocha and default
// 14.1); raising HIGH to 4.5:1 moves latte from 11.7 to 9.0 and leaves the
// others within 1.6 of their value. The bar is 8, a little under the tightest
// measured pair: it catches a HIGH pulled onto CRITICAL to reach a contrast,
// not the palettes' own closeness, and the word on the badge (see
// TestTheSeverityCellSpellsTheLevelOut) is the other carrier.
const minSeverityGap = 8.0

func TestSeverityHighAndCriticalStayApart(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		d, err := ColorDistance(ColorSeverityCritical, ColorSeverityHigh)
		if err != nil {
			t.Fatal(err)
		}
		if d < minSeverityGap {
			t.Errorf("theme %s: CRITICAL %s and HIGH %s are delta E %.1f apart, want >= %.1f",
				name, Hex(ColorSeverityCritical), Hex(ColorSeverityHigh), d, minSeverityGap)
		}
	})
}

// A theme written before severity_low existed, or by hand, may leave both LOW
// keys out. The fallback pair has to stay readable. Since D181 the fallbacks are
// ColorDim for the background and ColorBackground for the text, the same pair a
// shipped dark theme sets: ColorDim is held to 4.5:1 on the background, so the
// pair reads on every theme. (D177 had ColorCmdLineBg and ColorText, because
// ColorDim was then a light text tone under a light ColorText, about 2:1.)
func TestAThemeWithoutSeverityLowKeepsAReadableLowBadge(t *testing.T) {
	for name, th := range shippedThemes(t) {
		t.Run(name, func(t *testing.T) {
			bare := *th
			bare.SeverityLow, bare.SeverityLowFg = "", ""
			withTheme(t, &bare)
			ratio, err := ContrastRatio(ColorSeverityLowFg, ColorSeverityLow)
			if err != nil {
				t.Fatal(err)
			}
			if ratio < textMinContrast {
				t.Errorf("theme %s without severity_low: %s on %s is %.2f:1, want >= %.1f:1",
					name, Hex(ColorSeverityLowFg), Hex(ColorSeverityLow), ratio, textMinContrast)
			}
		})
	}
}

// The fallbacks themselves: a theme that omits the two LOW keys gets ColorDim
// under ColorBackground, not the pre-D181 ColorCmdLineBg under ColorText.
func TestAThemeWithoutSeverityLowFallsBackOnDimAndBackground(t *testing.T) {
	for name, th := range shippedThemes(t) {
		t.Run(name, func(t *testing.T) {
			bare := *th
			bare.SeverityLow, bare.SeverityLowFg = "", ""
			withTheme(t, &bare)
			if Hex(ColorSeverityLow) != Hex(ColorDim) {
				t.Errorf("theme %s without severity_low: LOW badge is %s, want ColorDim %s",
					name, Hex(ColorSeverityLow), Hex(ColorDim))
			}
			if Hex(ColorSeverityLowFg) != Hex(ColorBackground) {
				t.Errorf("theme %s without severity_low_fg: LOW badge text is %s, want ColorBackground %s",
					name, Hex(ColorSeverityLowFg), Hex(ColorBackground))
			}
		})
	}
}
