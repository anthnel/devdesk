package config

import (
	"strings"
	"testing"
)

// DEVDESK-258 step 1: app.theme "auto", app.theme_dark and app.theme_light.

func TestThemeConstants(t *testing.T) {
	if ThemeAuto != "auto" || ThemeDefault != "default" ||
		DefaultThemeDark != "default" || DefaultThemeLight != "github-light" {
		t.Fatalf("constants: %q %q %q %q", ThemeAuto, ThemeDefault, DefaultThemeDark, DefaultThemeLight)
	}
}

func TestThemeTable(t *testing.T) {
	tests := []struct {
		name string
		in   string
		kept string
		auto bool
	}{
		{"absent is normalised", "", "default", false},
		{"dark is normalised", "dark", "default", false},
		{"default", "default", "default", false},
		{"auto is kept", "auto", "auto", true},
		{"Auto is an unknown name", "Auto", "Auto", false},
		{"named theme", "github-light", "github-light", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateMotionHome(t)
			cfg := Default()
			cfg.App.Theme = tc.in
			if err := applyDefaults(cfg); err != nil {
				t.Fatalf("applyDefaults: %v", err)
			}
			if cfg.App.Theme != tc.kept {
				t.Errorf("Theme = %q, want %q", cfg.App.Theme, tc.kept)
			}
			if got := cfg.App.IsAutoTheme(); got != tc.auto {
				t.Errorf("IsAutoTheme = %v, want %v", got, tc.auto)
			}
		})
	}
}

func TestThemeAutoSurvivesLoadAndSave(t *testing.T) {
	loaded, reloaded, saved := roundTrip(t, "app:\n  theme: auto\n")
	if loaded.App.Theme != "auto" || reloaded.App.Theme != "auto" {
		t.Errorf("Theme = %q / %q, want auto", loaded.App.Theme, reloaded.App.Theme)
	}
	if !loaded.App.IsAutoTheme() || !reloaded.App.IsAutoTheme() {
		t.Error("IsAutoTheme must be true after load and reload")
	}
	if !strings.Contains(saved, "theme: auto") {
		t.Errorf("saved file lost theme: auto:\n%s", saved)
	}
}

func TestThemeAbsentKeysDefaultAndStayAbsentOnSave(t *testing.T) {
	loaded, reloaded, saved := roundTrip(t, "app:\n  theme: auto\n")
	for _, cfg := range []*Config{loaded, reloaded} {
		if got := cfg.App.EffectiveThemeDark(); got != "default" {
			t.Errorf("EffectiveThemeDark = %q, want default", got)
		}
		if got := cfg.App.EffectiveThemeLight(); got != "github-light" {
			t.Errorf("EffectiveThemeLight = %q, want github-light", got)
		}
		if cfg.App.ThemeDark != "" || cfg.App.ThemeLight != "" {
			t.Errorf("ThemeDark/Light = %q / %q, want empty (not filled by defaults)", cfg.App.ThemeDark, cfg.App.ThemeLight)
		}
	}
	if strings.Contains(saved, "theme_dark") || strings.Contains(saved, "theme_light") {
		t.Errorf("saved file gained theme_dark/theme_light keys:\n%s", saved)
	}
}

func TestThemeDarkAndLightRoundTrip(t *testing.T) {
	loaded, reloaded, saved := roundTrip(t, "app:\n  theme: auto\n  theme_dark: nord\n  theme_light: solarized-light\n")
	for _, cfg := range []*Config{loaded, reloaded} {
		if cfg.App.ThemeDark != "nord" || cfg.App.ThemeLight != "solarized-light" {
			t.Errorf("ThemeDark/Light = %q / %q", cfg.App.ThemeDark, cfg.App.ThemeLight)
		}
		if got := cfg.App.EffectiveThemeDark(); got != "nord" {
			t.Errorf("EffectiveThemeDark = %q, want nord", got)
		}
		if got := cfg.App.EffectiveThemeLight(); got != "solarized-light" {
			t.Errorf("EffectiveThemeLight = %q, want solarized-light", got)
		}
	}
	if !strings.Contains(saved, "theme_dark: nord") || !strings.Contains(saved, "theme_light: solarized-light") {
		t.Errorf("saved file lost the keys:\n%s", saved)
	}
}

func TestThemeEmptyAppConfigEffectiveDefaults(t *testing.T) {
	var a AppConfig
	if a.IsAutoTheme() {
		t.Error("zero AppConfig must not be auto")
	}
	if a.EffectiveThemeDark() != DefaultThemeDark || a.EffectiveThemeLight() != DefaultThemeLight {
		t.Errorf("effective = %q / %q", a.EffectiveThemeDark(), a.EffectiveThemeLight())
	}
}
