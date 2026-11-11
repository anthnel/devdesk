package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateResolveHome points HOME at an empty dir and returns the user themes dir.
func isolateResolveHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".devdesk", "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeUserTheme(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// userThemeJSON builds a complete theme file with the given text/background.
func userThemeJSON(name, text, bg string) string {
	return `{"name":"` + name + `","color_ok":"#00ff00","color_error":"#ff0000","color_warn":"#ff8800",` +
		`"color_primary":"#0000ff","color_secondary":"#0000cc","color_border":"#888888",` +
		`"color_text":"` + text + `","color_dim":"#999999","color_highlight":"#ffff00",` +
		`"color_white":"#ffffff","color_black":"#000000","color_background":"` + bg + `"}`
}

func TestResolveNamedDefaultHasNoWarning(t *testing.T) {
	isolateResolveHome(t)
	for _, name := range []string{"default", "dark", ""} {
		r := ResolveNamed(name)
		if r.Name != "default" || r.Theme == nil || r.Warning != "" {
			t.Errorf("ResolveNamed(%q) = %+v, want default without warning", name, r)
		}
		if r.Requested != name {
			t.Errorf("ResolveNamed(%q).Requested = %q", name, r.Requested)
		}
	}
}

func TestResolveNamedUnknownFallsBackWithWarning(t *testing.T) {
	isolateResolveHome(t)
	r := ResolveNamed("nope")
	if r.Name != "default" || r.Theme == nil || r.Theme.Name != "default" {
		t.Fatalf("ResolveNamed(nope) = %+v, want default theme", r)
	}
	if !strings.Contains(r.Warning, "nope") || !strings.Contains(r.Warning, "not found") {
		t.Errorf("Warning = %q, want it to name nope and say not found", r.Warning)
	}
}

func TestResolveNamedInvalidHexFallsBackWithWarning(t *testing.T) {
	dir := isolateResolveHome(t)
	writeUserTheme(t, dir, "broken", userThemeJSON("broken", "#zzzzzz", "#ffffff"))
	r := ResolveNamed("broken")
	if r.Name != "default" {
		t.Fatalf("Name = %q, want default", r.Name)
	}
	if !strings.Contains(r.Warning, "broken") || !strings.Contains(r.Warning, "is invalid") {
		t.Errorf("Warning = %q, want it to name broken and say is invalid", r.Warning)
	}
}

func TestResolveNamedShippedThemeNeedsNoUserFile(t *testing.T) {
	isolateResolveHome(t)
	r := ResolveNamed("github-light")
	if r.Name != "github-light" || r.Warning != "" {
		t.Errorf("ResolveNamed(github-light) = %+v, want it applied from the embedded copy", r)
	}
}

func TestResolveAutoLightPicksLightTheme(t *testing.T) {
	isolateResolveHome(t)
	r := ResolveAuto(BackgroundLight, "default", "github-light")
	if r.Name != "github-light" || r.Theme == nil || r.Reason != "terminal is light" || r.Warning != "" {
		t.Errorf("got %+v", r)
	}
	if r.Requested != "auto" {
		t.Errorf("Requested = %q, want auto", r.Requested)
	}
}

func TestResolveAutoDarkAndFallbacksPickDarkTheme(t *testing.T) {
	isolateResolveHome(t)
	cases := []struct {
		bg     Background
		reason string
	}{
		{BackgroundDark, "terminal is dark"},
		{BackgroundNoAnswer, "no answer from the terminal"},
		{BackgroundUnknown, ""},
	}
	for _, c := range cases {
		r := ResolveAuto(c.bg, "nord", "github-light")
		if r.Name != "nord" || r.Warning != "" {
			t.Errorf("bg %v: got %+v, want nord without warning", c.bg, r)
		}
		if c.reason != "" && r.Reason != c.reason {
			t.Errorf("bg %v: Reason = %q, want %q", c.bg, r.Reason, c.reason)
		}
	}
}

func TestResolveAutoUnknownCandidateFallsBackWithWarning(t *testing.T) {
	isolateResolveHome(t)
	r := ResolveAuto(BackgroundLight, "default", "nope")
	if r.Name != "default" || !strings.Contains(r.Warning, "nope") {
		t.Errorf("got %+v, want default + warning naming nope", r)
	}
	// The dark candidate is not consulted when the terminal is light.
	r = ResolveAuto(BackgroundLight, "nope", "github-light")
	if r.Name != "github-light" || r.Warning != "" {
		t.Errorf("got %+v, want github-light", r)
	}
}

func TestResolveAutoLowContrastCandidateFallsBackWithWarning(t *testing.T) {
	dir := isolateResolveHome(t)
	writeUserTheme(t, dir, "flat", userThemeJSON("flat", "#ffffff", "#ffffff"))
	r := ResolveAuto(BackgroundLight, "default", "flat")
	if r.Name != "default" {
		t.Fatalf("Name = %q, want default", r.Name)
	}
	if !strings.Contains(r.Warning, "flat") || !strings.Contains(r.Warning, "fails the contrast check") {
		t.Errorf("Warning = %q", r.Warning)
	}
}

func TestResolveAutoReadableUserThemeIsAccepted(t *testing.T) {
	dir := isolateResolveHome(t)
	writeUserTheme(t, dir, "paper", userThemeJSON("paper", "#000000", "#ffffff"))
	r := ResolveAuto(BackgroundLight, "default", "paper")
	if r.Name != "paper" || r.Warning != "" {
		t.Errorf("got %+v", r)
	}
}

func TestMinimumContrast(t *testing.T) {
	if err := MeetsMinimumContrast(DefaultTheme()); err != nil {
		t.Errorf("default theme: %v", err)
	}
	flat := DefaultTheme()
	flat.ColorText = flat.ColorBackground
	if err := MeetsMinimumContrast(flat); err == nil {
		t.Error("text equal to background must fail the 4.5:1 check")
	}
	// 4.5 is the floor: #767676 on white is 4.54:1, #777777 is 4.48:1.
	ok := DefaultTheme()
	ok.ColorText, ok.ColorBackground = "#767676", "#ffffff"
	if err := MeetsMinimumContrast(ok); err != nil {
		t.Errorf("4.54:1 must pass: %v", err)
	}
	ok.ColorText = "#777777"
	if err := MeetsMinimumContrast(ok); err == nil {
		t.Error("4.48:1 must fail")
	}
}

func TestEmbeddedThemesLoadWithoutUserDir(t *testing.T) {
	isolateResolveHome(t)
	th, err := LoadTheme("github-light")
	if err != nil || th.Name == "" {
		t.Fatalf("LoadTheme(github-light) with empty HOME: %v", err)
	}
	names, err := ListThemes()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"default", "catppuccin-frappe", "catppuccin-latte", "catppuccin-macchiato",
		"catppuccin-mocha", "dark-plus", "github-dark", "github-light", "nord", "one-dark",
		"solarized-dark", "solarized-light", "tokyo-night"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("ListThemes() lacks %q: %v", want, names)
		}
	}
	if names[0] != "default" {
		t.Errorf("default must come first: %v", names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate %q in %v", n, names)
		}
		seen[n] = true
	}
}

func TestUserThemeShadowsEmbeddedOne(t *testing.T) {
	dir := isolateResolveHome(t)
	writeUserTheme(t, dir, "github-light", userThemeJSON("my-override", "#000000", "#fefefe"))
	th, err := LoadTheme("github-light")
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "my-override" || th.ColorBackground != "#fefefe" {
		t.Errorf("got %q / %s, want the user's file to win", th.Name, th.ColorBackground)
	}
	names, _ := ListThemes()
	n := 0
	for _, name := range names {
		if name == "github-light" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("github-light listed %d times, want 1", n)
	}
}

func TestListThemesIncludesUserOnlyTheme(t *testing.T) {
	dir := isolateResolveHome(t)
	writeUserTheme(t, dir, "mine", userThemeJSON("mine", "#000000", "#ffffff"))
	names, _ := ListThemes()
	found := false
	for _, n := range names {
		found = found || n == "mine"
	}
	if !found {
		t.Errorf("user theme missing from %v", names)
	}
}

// The router compares the applied theme by file name, so Resolution.Name is the
// name that was asked for, never the "name" field inside the JSON.
func TestResolveNamedKeepsTheRequestedFileNameNotTheJSONField(t *testing.T) {
	dir := isolateResolveHome(t)
	writeUserTheme(t, dir, "github-light", userThemeJSON("my-override", "#000000", "#fefefe"))
	r := ResolveNamed("github-light")
	if r.Warning != "" {
		t.Fatalf("Warning = %q, want none", r.Warning)
	}
	if r.Name != "github-light" {
		t.Errorf("Name = %q, want %q (file name, not the JSON name field)", r.Name, "github-light")
	}
	if r.Theme == nil || r.Theme.ColorBackground != "#fefefe" {
		t.Errorf("Theme = %+v, want the user's file loaded", r.Theme)
	}
}
