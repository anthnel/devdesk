package configuration

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-258, step 6b: app.theme: auto in :config. The Theme cycle offers
// auto, Dark theme / Light theme choose what auto resolves to, and a read-only
// "Theme in use" row says what is on screen and why.
//
// Contract imposed on the implementation:
//
//	type ThemeFacts struct{ InUse, Reason string }
//	type ThemeFactsMsg struct{ InUse, Reason string } // Rule 109
//	func New(cfg *config.Config, mcp MCPFacts, theme ThemeFacts) Model
//	sections(..., mcp MCPFacts, theme ThemeFacts)
//
// Labels: "Theme" (existing), "Dark theme", "Light theme", "Theme in use", in
// that order right after Theme in the Appearance group. Dark/Light theme are
// cycle fields on app.theme_dark / app.theme_light with defaults
// config.DefaultThemeDark / config.DefaultThemeLight, and never offer auto.

const (
	darkThemeLabel  = "Dark theme"
	lightThemeLabel = "Light theme"
	themeInUseLabel = "Theme in use"
)

// themeModel is the real view (themes listed from the embedded set, HOME
// isolated), sized, with app.theme set to value.
func themeModel(t *testing.T, value string, facts ThemeFacts) Model {
	t.Helper()
	isolateMotionHome(t)
	cfg := config.Default()
	cfg.App.Theme = value
	m := New(cfg, MCPFacts{}, facts)
	return feed(t, m, testutil.Resize(120, 30))
}

func themeFieldIn(t *testing.T, m Model, label string) field {
	t.Helper()
	for _, s := range m.sections {
		for _, f := range s.Fields {
			if f.Label == label {
				return f
			}
		}
	}
	t.Fatalf("no field labelled %q", label)
	return field{}
}

func TestTheThemeCycleOffersAutoRightAfterDefault(t *testing.T) {
	m := themeModel(t, "default", ThemeFacts{})
	opts := themeFieldIn(t, m, "Theme").Options

	if len(opts) < 3 || opts[0] != "default" || opts[1] != config.ThemeAuto {
		t.Fatalf("Theme options = %v, want default, auto, then the themes", opts)
	}
	for _, want := range []string{"github-light", "github-dark", "nord"} {
		if !slices.Contains(opts, want) {
			t.Errorf("Theme options lack the embedded theme %q: %v", want, opts)
		}
	}
	if n := strings.Count(strings.Join(opts, ","), config.ThemeAuto+","); n != 1 {
		t.Errorf("auto appears %d times in %v, want once", n, opts)
	}
}

func TestDarkAndLightThemeAreCycleFieldsAfterTheme(t *testing.T) {
	m := themeModel(t, "auto", ThemeFacts{})
	var labels []string
	for _, f := range m.sections[0].Fields {
		labels = append(labels, f.Label)
	}
	i := slices.Index(labels, "Theme")
	want := []string{"Theme", darkThemeLabel, lightThemeLabel, themeInUseLabel}
	if i < 0 || len(labels) < i+len(want) || !slices.Equal(labels[i:i+len(want)], want) {
		t.Fatalf("labels after Theme = %v, want %v", labels, want)
	}

	group := themeFieldIn(t, m, "Theme").Group
	for _, label := range []string{darkThemeLabel, lightThemeLabel} {
		f := themeFieldIn(t, m, label)
		if f.Kind != kindCycle {
			t.Errorf("%s kind = %v, want kindCycle (Rule 132)", label, f.Kind)
		}
		if f.Group != group {
			t.Errorf("%s group = %q, want %q", label, f.Group, group)
		}
		if slices.Contains(f.Options, config.ThemeAuto) {
			t.Errorf("%s offers auto: it would resolve to itself: %v", label, f.Options)
		}
		for _, want := range []string{"default", "github-light", "github-dark"} {
			if !slices.Contains(f.Options, want) {
				t.Errorf("%s options lack %q: %v", label, want, f.Options)
			}
		}
		if !strings.Contains(strings.ToLower(f.hint), "auto") {
			t.Errorf("%s hint %q does not say it applies when Theme is auto", label, f.hint)
		}
	}
	if f := themeFieldIn(t, m, themeInUseLabel); f.Kind != kindStatic {
		t.Errorf("%s kind = %v, want kindStatic (read-only)", themeInUseLabel, f.Kind)
	}
}

func TestAbsentDarkAndLightThemeShowTheirDefaultsAndStayAbsent(t *testing.T) {
	m := themeModel(t, "auto", ThemeFacts{})

	for label, want := range map[string]string{
		darkThemeLabel:  config.DefaultThemeDark + " (default)",
		lightThemeLabel: config.DefaultThemeLight + " (default)",
	} {
		if got := themeFieldIn(t, m, label).Value(m.config); got != want {
			t.Errorf("%s Value = %q, want %q", label, got, want)
		}
	}
	if m.config.App.ThemeDark != "" || m.config.App.ThemeLight != "" {
		t.Error("displaying the defaults wrote them")
	}
	if got := plainRows(m); !strings.Contains(got, "github-light (default)") {
		t.Errorf("the Light theme row does not read 'github-light (default)':\n%s", got)
	}
}

func TestCyclingDarkOrLightThemeWritesItsKeyAndAsksForAThemeReapply(t *testing.T) {
	for _, tt := range []struct {
		label string
		get   func(*config.Config) string
	}{
		{darkThemeLabel, func(c *config.Config) string { return c.App.ThemeDark }},
		{lightThemeLabel, func(c *config.Config) string { return c.App.ThemeLight }},
	} {
		t.Run(tt.label, func(t *testing.T) {
			m := focusOn(t, themeModel(t, "auto", ThemeFacts{}), tt.label)
			updated, cmd := m.Update(testutil.Key("right"))
			next := updated.(Model)

			got := tt.get(next.config)
			if got == "" || got == config.ThemeAuto {
				t.Fatalf("after → the value is %q, want an explicit theme name", got)
			}
			msg, ok := testutil.MsgOf[ConfigSavedMsg](cmd)
			if !ok {
				t.Fatal("no ConfigSavedMsg")
			}
			if !msg.ThemeChanged {
				t.Error("ThemeChanged = false: the router would keep the old palette until restart")
			}
			if tt.get(msg.Config) != got {
				t.Errorf("ConfigSavedMsg carries %q, want %q", tt.get(msg.Config), got)
			}
			if next.config.App.Theme != config.ThemeAuto {
				t.Errorf("cycling %s changed Theme to %q", tt.label, next.config.App.Theme)
			}
		})
	}
}

func TestNeitherDarkNorLightThemeEverLandsOnAuto(t *testing.T) {
	for _, label := range []string{darkThemeLabel, lightThemeLabel} {
		m := focusOn(t, themeModel(t, "auto", ThemeFacts{}), label)
		n := len(themeFieldIn(t, m, label).Options) + 2
		for range n {
			m = feed(t, m, testutil.Key("right"))
			if v := themeFieldIn(t, m, label).Value(m.config); strings.HasPrefix(v, config.ThemeAuto) {
				t.Fatalf("%s reached %q", label, v)
			}
		}
	}
}

func TestCyclingThemeFromDefaultReachesAutoAndSavesIt(t *testing.T) {
	m := focusOn(t, themeModel(t, "default", ThemeFacts{}), "Theme")
	updated, cmd := m.Update(testutil.Key("right"))
	next := updated.(Model)

	if next.config.App.Theme != config.ThemeAuto {
		t.Fatalf("Theme after → = %q, want auto", next.config.App.Theme)
	}
	if msg, ok := testutil.MsgOf[ConfigSavedMsg](cmd); !ok || !msg.ThemeChanged || msg.Config.App.Theme != config.ThemeAuto {
		t.Errorf("ConfigSavedMsg = %+v (ok %v), want ThemeChanged with Theme auto", msg, ok)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.App.Theme != config.ThemeAuto {
		t.Errorf("the file holds theme %q, want auto", reloaded.App.Theme)
	}
}

// Choosing a named theme replaces auto, and the file says so (criterion 6).
func TestCyclingThemeFromAutoToANamedThemeReplacesAutoAndPersists(t *testing.T) {
	m := focusOn(t, themeModel(t, config.ThemeAuto, ThemeFacts{}), "Theme")
	opts := themeFieldIn(t, m, "Theme").Options
	want := opts[slices.Index(opts, config.ThemeAuto)+1]

	updated, cmd := m.Update(testutil.Key("right"))
	next := updated.(Model)

	if next.config.App.Theme != want || want == config.ThemeAuto {
		t.Fatalf("Theme after → from auto = %q, want the next named theme %q", next.config.App.Theme, want)
	}
	msg, ok := testutil.MsgOf[ConfigSavedMsg](cmd)
	if !ok || !msg.ThemeChanged || msg.Config.App.Theme != want {
		t.Fatalf("ConfigSavedMsg = %+v (ok %v), want ThemeChanged with Theme %q", msg, ok, want)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.App.Theme != want {
		t.Errorf("the file holds theme %q, want %q (auto must be gone)", reloaded.App.Theme, want)
	}
}

func TestThemeInUseShowsTheFactsItWasBuiltWith(t *testing.T) {
	m := themeModel(t, "auto", ThemeFacts{InUse: "github-light", Reason: "auto: terminal is light"})
	row := themeInUseRow(t, m)

	for _, want := range []string{"github-light", "terminal is light"} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q lacks %q", row, want)
		}
	}
}

func TestThemeFactsMsgUpdatesTheRowAndKeepsTheFocus(t *testing.T) {
	m := focusOn(t, themeModel(t, "auto", ThemeFacts{InUse: "default"}), darkThemeLabel)
	tab, idx := m.activeTab, m.focusedField

	updated, cmd := m.Update(ThemeFactsMsg{InUse: "github-light", Reason: "auto: terminal is light"})
	next := updated.(Model)

	row := themeInUseRow(t, next)
	for _, want := range []string{"github-light", "terminal is light"} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q lacks %q after ThemeFactsMsg", row, want)
		}
	}
	if next.activeTab != tab || next.focusedField != idx {
		t.Errorf("focus moved from tab %d field %d to tab %d field %d", tab, idx, next.activeTab, next.focusedField)
	}
	if _, ok := testutil.MsgOf[ConfigSavedMsg](cmd); ok {
		t.Error("a ThemeFactsMsg saved the configuration")
	}
	if next.config.App.Theme != config.ThemeAuto {
		t.Error("a ThemeFactsMsg rewrote app.theme")
	}
}

func TestThemeFactsMsgForANamedThemeShowsItsName(t *testing.T) {
	m := themeModel(t, "nord", ThemeFacts{InUse: "default"})
	m = feed(t, m, ThemeFactsMsg{InUse: "nord"})

	if row := themeInUseRow(t, m); !strings.Contains(row, "nord") || strings.Contains(row, "default") {
		t.Errorf("row %q, want nord and not the stale default", row)
	}
}

// A static fact is not focusable (focusable()), so the cursor walks past it:
// nothing can be typed or cycled on it.
func TestThemeInUseIsReadOnlyAndTheCursorSkipsIt(t *testing.T) {
	m := themeModel(t, "auto", ThemeFacts{InUse: "default"})
	if themeFieldIn(t, m, themeInUseLabel).focusable() {
		t.Fatal("Theme in use is focusable")
	}

	m = focusOn(t, m, lightThemeLabel)
	m = feed(t, m, testutil.Key("down"))
	if got := m.current().Label; got == themeInUseLabel || got == lightThemeLabel {
		t.Errorf("↓ from Light theme landed on %q, want the next setting", got)
	}
	m = feed(t, m, testutil.Key("up"))
	if got := m.current().Label; got != lightThemeLabel {
		t.Errorf("↑ landed on %q, want Light theme (Theme in use is skipped)", got)
	}
}

func TestThemeRowsPlainTextHoldsNoEscapeInTheFactsRow(t *testing.T) {
	m := themeModel(t, "auto", ThemeFacts{InUse: "github-light", Reason: "auto: terminal is light"})
	for _, s := range m.sections {
		for _, f := range s.Fields {
			if f.Label == themeInUseLabel && ansi.Strip(f.fact) != f.fact {
				t.Errorf("fact %q carries an escape sequence (Rule 122)", f.fact)
			}
		}
	}
}

// themeInUseRow is the rendered line holding the Theme in use row.
func themeInUseRow(t *testing.T, m Model) string {
	t.Helper()
	for _, line := range strings.Split(plainRows(m), "\n") {
		if strings.Contains(line, themeInUseLabel) {
			return line
		}
	}
	t.Fatalf("no %q row in:\n%s", themeInUseLabel, plainRows(m))
	return ""
}
