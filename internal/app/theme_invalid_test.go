package app

import (
	"github.com/anthnel/devdesk/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// themeRouter builds a router on the default palette and restores it afterwards.
func themeRouter(t *testing.T) *App {
	t.Helper()
	a := router(t, &fakeView{})
	reset := func() {
		theme.ApplyTheme(theme.DefaultTheme())
		theme.CurrentThemeName = "default"
	}
	t.Cleanup(reset)
	reset()
	return a
}

// writeThemeFile writes ~/.devdesk/themes/<name>.json under the isolated HOME.
func writeThemeFile(t *testing.T, name, body string) {
	t.Helper()
	dir, err := theme.ThemeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// saveThemeFooter sends the router the save the configuration view emits when the
// Theme field changes, and returns the footer posts the resulting Cmd carries.
func saveThemeFooter(t *testing.T, a *App, name string) []components.PostFooterMsg {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	cfg := testConfig()
	cfg.App.Theme = name
	_, cmd := a.Update(configuration.ConfigSavedMsg{Config: cfg, ThemeChanged: true})
	var posts []components.PostFooterMsg
	for _, m := range testutil.Msgs(cmd) {
		if p, ok := m.(components.PostFooterMsg); ok {
			posts = append(posts, p)
		}
	}
	return posts
}

// wantDefaultWithWarning asserts the DEVDESK-258 contract for a theme that
// cannot be used: the default palette is applied (not the previous one kept)
// and one Warn footer message names the theme that was asked for.
func wantDefaultWithWarning(t *testing.T, posts []components.PostFooterMsg, asked string) {
	t.Helper()
	if theme.CurrentThemeName != "default" {
		t.Errorf("CurrentThemeName = %q, want default", theme.CurrentThemeName)
	}
	if got, want := theme.Hex(theme.ColorPrimary), theme.DefaultTheme().ColorPrimary; got != want {
		t.Errorf("ColorPrimary = %s, want the default palette %s", got, want)
	}
	if len(posts) != 1 {
		t.Fatalf("got %d footer posts, want exactly 1 warning: %+v", len(posts), posts)
	}
	if posts[0].Level != components.LevelWarning {
		t.Errorf("Level = %v, want LevelWarning", posts[0].Level)
	}
	if !strings.Contains(posts[0].Text, asked) {
		t.Errorf("Text = %q, want it to name %q", posts[0].Text, asked)
	}
}

// DEVDESK-258 (replaces the DEVDESK-201 H3 rule "refused theme keeps the current
// palette and posts no footer"): any load failure applies the default theme and
// warns in the footer, naming the theme that was asked for.
func TestASavedThemeWithAnInvalidHexFallsBackToDefaultWithAWarning(t *testing.T) {
	a := themeRouter(t)
	writeThemeFile(t, "broken", `{"name":"broken","color_ok":"#ggg","color_error":"#ff0000","color_warn":"#ff8800",`+
		`"color_primary":"#0000ff","color_secondary":"#8888ff","color_border":"#cccccc","color_text":"#000000",`+
		`"color_dim":"#888888","color_highlight":"#ffff00","color_white":"#ffffff","color_black":"#000000"}`)

	// Start from another palette so "kept the current one" would be caught.
	if _, cmd := a.Update(configuration.ConfigSavedMsg{Config: themedConfig("github-dark"), ThemeChanged: true}); cmd == nil {
		t.Fatal("no Cmd")
	}
	if theme.CurrentThemeName != "github-dark" {
		t.Fatalf("setup: CurrentThemeName = %q, want github-dark", theme.CurrentThemeName)
	}

	posts := saveThemeFooter(t, a, "broken")
	wantDefaultWithWarning(t, posts, "broken")
}

func TestASavedUnknownThemeFallsBackToDefaultWithAWarning(t *testing.T) {
	a := themeRouter(t)
	posts := saveThemeFooter(t, a, "nope")
	wantDefaultWithWarning(t, posts, "nope")
}

func TestASavedThemeWithUnreadableJSONFallsBackToDefaultWithAWarning(t *testing.T) {
	a := themeRouter(t)
	writeThemeFile(t, "garbled", `{"name": "garbled", "color_ok": `)
	posts := saveThemeFooter(t, a, "garbled")
	wantDefaultWithWarning(t, posts, "garbled")
}

func TestASavedValidThemeIsAppliedWithoutAWarning(t *testing.T) {
	a := themeRouter(t)
	posts := saveThemeFooter(t, a, "github-dark")
	if len(posts) != 0 {
		t.Errorf("a valid theme posted footer messages: %+v", posts)
	}
	if theme.CurrentThemeName != "github-dark" {
		t.Errorf("CurrentThemeName = %q, want github-dark", theme.CurrentThemeName)
	}
}

// The router compares by file name, so a user file whose JSON "name" differs
// still counts as the theme that was asked for.
func TestASavedThemeIsRecordedUnderItsFileName(t *testing.T) {
	a := themeRouter(t)
	writeThemeFile(t, "github-light", `{"name":"my-override","color_ok":"#00ff00","color_error":"#ff0000","color_warn":"#ff8800",`+
		`"color_primary":"#0000ff","color_secondary":"#0000cc","color_border":"#888888","color_text":"#000000",`+
		`"color_dim":"#999999","color_highlight":"#ffff00","color_white":"#ffffff","color_black":"#000000","color_background":"#fefefe"}`)
	posts := saveThemeFooter(t, a, "github-light")
	if len(posts) != 0 {
		t.Errorf("posts = %+v, want none", posts)
	}
	if theme.CurrentThemeName != "github-light" {
		t.Errorf("CurrentThemeName = %q, want github-light", theme.CurrentThemeName)
	}
}

func themedConfig(name string) *config.Config {
	c := testConfig()
	c.App.Theme = name
	return c
}
