package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-201, H3: LoadTheme refuses a theme with an invalid hex, and the
// router keeps the current palette. No footer message is added (a follow-up if
// the user asks for one): the failure goes to the log only.
func TestASavedThemeWithAnInvalidHexKeepsTheCurrentPaletteAndPostsNoFooter(t *testing.T) {
	a := router(t, &fakeView{})
	t.Cleanup(func() {
		theme.ApplyTheme(theme.DefaultTheme())
		theme.CurrentThemeName = "default"
	})
	theme.ApplyTheme(theme.DefaultTheme())
	theme.CurrentThemeName = "default"

	dir, err := theme.ThemeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `{"name":"broken","color_ok":"#ggg","color_error":"#ff0000","color_warn":"#ff8800",` +
		`"color_primary":"#0000ff","color_secondary":"#8888ff","color_border":"#cccccc","color_text":"#000000",` +
		`"color_dim":"#888888","color_highlight":"#ffff00","color_white":"#ffffff","color_black":"#000000"}`
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	before := theme.Hex(theme.ColorPrimary)
	cfg := testConfig()
	cfg.App.Theme = "broken"
	_, cmd := a.Update(configuration.ConfigSavedMsg{Config: cfg, ThemeChanged: true})

	if got := theme.Hex(theme.ColorPrimary); got != before {
		t.Errorf("ColorPrimary = %s after a refused theme, want %s kept", got, before)
	}
	if theme.CurrentThemeName != "default" {
		t.Errorf("CurrentThemeName = %q, want %q kept", theme.CurrentThemeName, "default")
	}
	for _, m := range testutil.Msgs(cmd) {
		if p, ok := m.(components.PostFooterMsg); ok {
			t.Errorf("a refused theme posted a footer message: %+v", p)
		}
	}
}
