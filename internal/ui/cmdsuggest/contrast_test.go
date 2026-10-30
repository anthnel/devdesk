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

const minContrast = 4.5 // WCAG AA, normal text

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

// Global color state: no t.Parallel in this file.
func TestPaletteContrastOnEveryTheme(t *testing.T) {
	t.Cleanup(func() { theme.ApplyTheme(theme.DefaultTheme()) })

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

	for _, name := range names {
		th := themes[name]
		t.Run(name, func(t *testing.T) {
			// Start from the default theme, as the app does, so a theme
			// missing a key does not inherit the previous theme's value.
			theme.ApplyTheme(theme.DefaultTheme())
			theme.ApplyTheme(th)
			for _, p := range Palette() {
				ratio, err := theme.ContrastRatio(p.Fg, p.Bg)
				if err != nil {
					t.Errorf("pair %q: %v", p.Name, err)
					continue
				}
				if ratio < minContrast {
					t.Errorf("pair %q: contrast %.2f:1 (fg %s on bg %s), want >= %.1f:1",
						p.Name, ratio, p.Fg, p.Bg, minContrast)
				}
			}
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
