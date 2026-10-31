package cmdsuggest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	minContrast       = 4.5 // WCAG AA, normal text
	minBorderContrast = 3.0 // WCAG 1.4.11, graphical objects
)

// themeFiles discovers the shipped themes by glob, so a new file is covered
// without touching this test.
func themeFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../../themes/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no theme found under themes/*.json")
	}
	return files
}

// loadThemes returns the default theme and every shipped theme, by name, with
// the names sorted.
func loadThemes(t *testing.T) (map[string]*theme.Theme, []string) {
	t.Helper()
	themes := map[string]*theme.Theme{"default": theme.DefaultTheme()}
	for _, f := range themeFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var th theme.Theme
		if err := json.Unmarshal(data, &th); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		themes[strings.TrimSuffix(filepath.Base(f), ".json")] = &th
	}
	names := make([]string, 0, len(themes))
	for name := range themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return themes, names
}

// checkPairs fails, naming theme, pair and both colors, for each pair below min.
func checkPairs(t *testing.T, themeName string, pairs []Pair, min float64) {
	t.Helper()
	for _, p := range pairs {
		ratio, err := theme.ContrastRatio(p.Fg, p.Bg)
		if err != nil {
			t.Errorf("theme %s: pair %q: %v", themeName, p.Name, err)
			continue
		}
		if ratio < min {
			t.Errorf("theme %s: pair %q: contrast %.2f:1 (fg %s on bg %s), want >= %.1f:1",
				themeName, p.Name, ratio, p.Fg, p.Bg, min)
		}
	}
}

// Global color state: no t.Parallel in this file.
func TestPaletteContrastOnEveryTheme(t *testing.T) {
	t.Cleanup(func() { theme.ApplyTheme(theme.DefaultTheme()) })
	themes, names := loadThemes(t)
	for _, name := range names {
		th := themes[name]
		t.Run(name, func(t *testing.T) {
			// Start from the default theme, as the app does, so a theme
			// missing a key does not inherit the previous theme's value.
			theme.ApplyTheme(theme.DefaultTheme())
			theme.ApplyTheme(th)
			checkPairs(t, name, Palette(), minContrast)
		})
	}
}

// The palette frame adds a title and an input line: both are text and are
// part of Palette(), so the 4.5:1 loop above covers them.
func TestPaletteListsTitleAndInputPairs(t *testing.T) {
	t.Cleanup(func() { theme.ApplyTheme(theme.DefaultTheme()) })
	theme.ApplyTheme(theme.DefaultTheme())
	for _, want := range []string{"title", "input"} {
		found := false
		for _, p := range Palette() {
			if strings.Contains(strings.ToLower(p.Name), want) {
				found = true
			}
		}
		if !found {
			t.Errorf("Palette() has no %s pair", want)
		}
	}
}

// Border and prompt glyph are non-text graphics: WCAG 1.4.11 asks for 3:1.
func TestPaletteBorderContrastOnEveryTheme(t *testing.T) {
	t.Cleanup(func() { theme.ApplyTheme(theme.DefaultTheme()) })
	themes, names := loadThemes(t)
	for _, name := range names {
		th := themes[name]
		t.Run(name, func(t *testing.T) {
			theme.ApplyTheme(theme.DefaultTheme())
			theme.ApplyTheme(th)
			pairs := BorderPalette()
			if len(pairs) == 0 {
				t.Fatalf("theme %s: BorderPalette() is empty", name)
			}
			for _, p := range pairs {
				if p.Fg != theme.ColorPaletteBorder {
					t.Errorf("theme %s: pair %q uses %s, want ColorPaletteBorder %s", name, p.Name, p.Fg, theme.ColorPaletteBorder)
				}
				if p.Bg != theme.ColorBackground {
					t.Errorf("theme %s: pair %q sits on %s, want ColorBackground %s", name, p.Name, p.Bg, theme.ColorBackground)
				}
			}
			checkPairs(t, name, pairs, minBorderContrast)
		})
	}
}

// ColorDim is a decoration tone, not a text tone: it is not required to reach
// 4.5:1 anywhere, so no text pair may use it.
func TestPaletteNeverUsesDimAsTextColor(t *testing.T) {
	t.Cleanup(func() { theme.ApplyTheme(theme.DefaultTheme()) })
	theme.ApplyTheme(theme.DefaultTheme())
	for _, p := range Palette() {
		if p.Fg == theme.ColorDim {
			t.Errorf("pair %q uses ColorDim (%s) as its text color", p.Name, p.Fg)
		}
	}
}
