package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

const (
	paletteTextContrast   = 4.5 // WCAG AA, normal text
	paletteBorderContrast = 3.0 // WCAG non-text contrast (border, prompt glyph)
)

// shippedThemes returns the default theme plus every themes/*.json, found by
// glob so a new theme file is covered without touching this test.
func shippedThemes(t *testing.T) map[string]*Theme {
	t.Helper()
	files, err := filepath.Glob("../../../themes/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no theme found under themes/*.json")
	}
	out := map[string]*Theme{"default": DefaultTheme()}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var th Theme
		if err := json.Unmarshal(data, &th); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = &th
	}
	return out
}

// eachShippedTheme applies every shipped theme in turn, starting from the
// default as the app does, and restores the default afterwards.
// Global colour state: callers must not use t.Parallel.
func eachShippedTheme(t *testing.T, fn func(t *testing.T, name string)) {
	t.Helper()
	themes := shippedThemes(t)
	names := make([]string, 0, len(themes))
	for n := range themes {
		names = append(names, n)
	}
	sort.Strings(names)
	t.Cleanup(func() { ApplyTheme(DefaultTheme()) })
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			ApplyTheme(DefaultTheme())
			ApplyTheme(themes[n])
			fn(t, n)
		})
	}
}

// ColorPaletteBorder is a closed alias of ColorPrimary: no theme key exists
// for it, so a user theme cannot break the 3:1 border contrast unseen.
func TestPaletteBorderIsAClosedAliasOfPrimary(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		if ColorPaletteBorder != ColorPrimary {
			t.Errorf("theme %s: ColorPaletteBorder = %q, want ColorPrimary %q", name, ColorPaletteBorder, ColorPrimary)
		}
	})
}

// The alias must follow a theme change rather than keep its load-time value.
func TestPaletteBorderFollowsAThemeChange(t *testing.T) {
	th := DefaultTheme()
	th.Name = "primary-changed"
	th.ColorPrimary = "#123456"
	withTheme(t, th)
	if ColorPaletteBorder != lipgloss.Color("#123456") {
		t.Errorf("ColorPaletteBorder = %q; it did not follow ColorPrimary", ColorPaletteBorder)
	}
}

// Border and prompt glyph are decoration: 3:1 against the panel background.
func TestPaletteBorderContrastsWithBackground(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		r, err := ContrastRatio(ColorPaletteBorder, ColorBackground)
		if err != nil {
			t.Fatal(err)
		}
		if r < paletteBorderContrast {
			t.Errorf("theme %s: palette border %.2f:1 (%s on %s), want >= %.1f:1",
				name, r, ColorPaletteBorder, ColorBackground, paletteBorderContrast)
		}
	})
}

// Title and typed input are ColorText on ColorBackground.
func TestPaletteFrameTextContrastsWithBackground(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		r, err := ContrastRatio(ColorText, ColorBackground)
		if err != nil {
			t.Fatal(err)
		}
		if r < paletteTextContrast {
			t.Errorf("theme %s: title/input text %.2f:1 (%s on %s), want >= %.1f:1",
				name, r, ColorText, ColorBackground, paletteTextContrast)
		}
	})
}

// The header hint "ctrl+p Commands" must not be the dim decoration tone.
func TestShortcutHintStyleIsReadable(t *testing.T) {
	eachShippedTheme(t, func(t *testing.T, name string) {
		fg, ok := ShortcutHintStyle.GetForeground().(lipgloss.Color)
		if !ok {
			t.Fatalf("theme %s: ShortcutHintStyle foreground is %T, want lipgloss.Color", name, ShortcutHintStyle.GetForeground())
		}
		if fg == ColorDim {
			t.Errorf("theme %s: ShortcutHintStyle uses ColorDim (%s)", name, ColorDim)
		}
		if bg := ShortcutHintStyle.GetBackground(); bg != lipgloss.TerminalColor(ColorBackground) {
			t.Errorf("theme %s: ShortcutHintStyle background = %v, want app background (Rule 115)", name, bg)
		}
		r, err := ContrastRatio(fg, ColorBackground)
		if err != nil {
			t.Fatal(err)
		}
		if r < paletteTextContrast {
			t.Errorf("theme %s: ShortcutHintStyle %.2f:1 (%s on %s), want >= %.1f:1",
				name, r, fg, ColorBackground, paletteTextContrast)
		}
	})
}
