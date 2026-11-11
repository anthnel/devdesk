package cmdsuggest

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Every theme that ships (and so can be picked by app.theme: auto) must keep
// its text readable on its own background: the runtime check of DEVDESK-258
// (Q3) falls back to the default theme below this, which a shipped theme
// must never trigger.
func TestEveryShippedThemePassesTheAutoContrastCheck(t *testing.T) {
	themes, names := loadThemes(t)
	for _, name := range names {
		if err := theme.MeetsMinimumContrast(themes[name]); err != nil {
			t.Errorf("theme %s: %v", name, err)
		}
	}
}

// The shipped default pair of auto (default / github-light) is what a fresh
// config gets: it must resolve with no warning, from the embedded themes.
func TestAutoDefaultsResolveWithoutWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, bg := range []theme.Background{theme.BackgroundLight, theme.BackgroundDark, theme.BackgroundNoAnswer} {
		r := theme.ResolveAuto(bg, "default", "github-light")
		if r.Warning != "" {
			t.Errorf("background %v: warning %q", bg, r.Warning)
		}
	}
}
