package app

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-258, review round: three defects of settleTheme and its neighbours.
//
// Behavior these tests impose on the implementation:
//   - settleTheme rebuilds the views (as handleConfigSaved does: drop every view
//     but :config, createView(currentView), requestResize) when the effective
//     theme changes, and only then;
//   - at startup, themeInitCmd posts one Warn per invalid auto candidate
//     (theme_dark and theme_light), the requested name in the text, even for
//     the candidate the terminal does not select;
//   - a candidate already warned about at startup is never warned about again
//     when the terminal's answer arrives: one Warn per invalid candidate in total.

// autoRouterWith is autoRouter with a hook that runs after HOME is isolated and
// before the router is built, so a theme file exists when the startup choice
// loads it.
func autoRouterWith(t *testing.T, cfg *config.Config, setup func(t *testing.T)) *App {
	t.Helper()
	isolateHome(t)
	reset := func() {
		theme.ApplyTheme(theme.DefaultTheme())
		theme.CurrentThemeName = "default"
	}
	t.Cleanup(reset)
	reset()
	if setup != nil {
		setup(t)
	}
	a := newWithSize(cfg, 120, 40)
	sendTrueColor(t, a)
	return a
}

// ── Defect 1: the views are rebuilt when the palette changes ─────────────────

func TestAnAnswerThatChangesThePaletteRebuildsTheViews(t *testing.T) {
	a := autoRouterWith(t, autoConfig("auto", "", ""), nil)
	stale := &fakeView{}
	a.views[a.currentView] = stale

	cmds := answer(t, a, color.White) // light -> github-light
	if theme.CurrentThemeName != "github-light" {
		t.Fatalf("CurrentThemeName = %q, want github-light", theme.CurrentThemeName)
	}
	got, ok := a.views[a.currentView]
	if !ok {
		t.Fatal("the current view was dropped and not recreated")
	}
	if got == Screen(stale) {
		t.Error("the view built before the answer survived: its styles were captured with the dark palette")
	}
	sawResize := false
	for _, m := range cmds {
		if _, ok := m.(tea.WindowSizeMsg); ok {
			sawResize = true
		}
	}
	if !sawResize {
		t.Error("no resize requested after rebuilding the views (the new view would stay unsized)")
	}
}

func TestAnAnswerThatChangesThePaletteKeepsTheConfigurationView(t *testing.T) {
	a := autoRouterWith(t, autoConfig("auto", "", ""), nil)
	// :config exists while another view is on screen.
	orig := a.currentView
	openConfigView(a)
	a.currentView = orig
	answer(t, a, color.White)
	if _, ok := a.views[command.ViewConfiguration]; !ok {
		t.Fatal("the :config view was dropped; like handleConfigSaved, settleTheme must keep it")
	}
	wantThemeRow(t, a, "github-light")
}

func TestNoRebuildWhenTheEffectiveThemeIsUnchanged(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, a *App)
	}{
		{"dark answer", func(t *testing.T, a *App) { answer(t, a, color.Black) }},
		{"no colour in the answer", func(t *testing.T, a *App) { answer(t, a, nil) }},
		{"timeout", func(t *testing.T, a *App) {
			timeout, ok := firstOf[themeDetectTimeoutMsg](initMsgs(t, a))
			if !ok {
				t.Fatal("Init armed no timeout")
			}
			a.Update(timeout)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := autoRouterWith(t, autoConfig("auto", "", ""), nil)
			sentinel := &fakeView{}
			a.views[a.currentView] = sentinel
			tc.run(t, a)
			if a.views[a.currentView] != Screen(sentinel) {
				t.Error("the views were rebuilt although the effective theme did not change")
			}
		})
	}
}

// ── Defect 2: one startup warning per invalid candidate ──────────────────────

func writeBadCandidate(kind, name string) func(t *testing.T) {
	switch kind {
	case "unreadable":
		return func(t *testing.T) { writeThemeFile(t, name, `{"name": `) }
	case "contrast":
		return func(t *testing.T) { writeTheme(t, name, theme.DefaultTheme().ColorText) }
	}
	return func(*testing.T) {} // unknown: no file
}

// allPosts runs Init's theme part, then feeds the answer (nil: none), and
// returns every footer post of both.
func allPosts(t *testing.T, a *App, answerColor color.Color, answered bool) []components.PostFooterMsg {
	t.Helper()
	posts := footerPosts(initMsgs(t, a))
	if answered {
		posts = append(posts, footerPosts(answer(t, a, answerColor))...)
	}
	return posts
}

func wantOneWarningNaming(t *testing.T, posts []components.PostFooterMsg, names ...string) {
	t.Helper()
	if len(posts) != len(names) {
		t.Fatalf("got %d footer posts, want %d (one per invalid candidate): %+v", len(posts), len(names), posts)
	}
	for _, name := range names {
		n := 0
		for _, p := range posts {
			if p.Level != components.LevelWarning {
				t.Errorf("post %+v is not a warning", p)
			}
			if strings.Contains(p.Text, name) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%d warnings name %q, want exactly 1: %+v", n, name, posts)
		}
	}
}

func TestAnInvalidUnusedCandidateWarnsAtStartup(t *testing.T) {
	for _, kind := range []string{"unknown", "unreadable", "contrast"} {
		t.Run("theme_light "+kind+" on a dark terminal", func(t *testing.T) {
			a := autoRouterWith(t, autoConfig("auto", "", "badlight"), writeBadCandidate(kind, "badlight"))
			wantOneWarningNaming(t, allPosts(t, a, color.Black, true), "badlight")
			if theme.CurrentThemeName != "default" {
				t.Errorf("CurrentThemeName = %q, want the terminal's dark default", theme.CurrentThemeName)
			}
		})
		t.Run("theme_dark "+kind+" on a light terminal", func(t *testing.T) {
			a := autoRouterWith(t, autoConfig("auto", "baddark", ""), writeBadCandidate(kind, "baddark"))
			wantOneWarningNaming(t, allPosts(t, a, color.White, true), "baddark")
			if theme.CurrentThemeName != "github-light" {
				t.Errorf("CurrentThemeName = %q, want github-light (the terminal's)", theme.CurrentThemeName)
			}
		})
	}
}

func TestTheStartupWarningForAnUnusedCandidateComesFromInit(t *testing.T) {
	a := autoRouterWith(t, autoConfig("auto", "", "badlight"), nil)
	wantOneWarningNaming(t, footerPosts(initMsgs(t, a)), "badlight")
}

func TestBothCandidatesInvalidWarnOnceEach(t *testing.T) {
	a := autoRouterWith(t, autoConfig("auto", "baddark", "badlight"), nil)
	wantOneWarningNaming(t, allPosts(t, a, color.White, true), "baddark", "badlight")
}

// ── Defect 3: no duplicate warning ───────────────────────────────────────────

func TestAnInvalidThemeDarkAndADarkAnswerWarnOnce(t *testing.T) {
	a := autoRouterWith(t, autoConfig("auto", "baddark", ""), nil)
	wantOneWarningNaming(t, allPosts(t, a, color.Black, true), "baddark")
	if theme.CurrentThemeName != "default" {
		t.Errorf("CurrentThemeName = %q, want default", theme.CurrentThemeName)
	}
}

func TestAnInvalidThemeDarkAndATimeoutWarnOnce(t *testing.T) {
	a := autoRouterWith(t, autoConfig("auto", "baddark", ""), nil)
	init := initMsgs(t, a)
	posts := footerPosts(init)
	timeout, ok := firstOf[themeDetectTimeoutMsg](init)
	if !ok {
		t.Fatal("no timeout armed")
	}
	_, cmd := a.Update(timeout)
	posts = append(posts, footerPosts(testutil.Msgs(cmd))...)
	wantOneWarningNaming(t, posts, "baddark")
}

func TestAnInvalidThemeLightAndALightAnswerWarnOnce(t *testing.T) {
	a := autoRouterWith(t, autoConfig("auto", "", "badlight"), nil)
	wantOneWarningNaming(t, allPosts(t, a, color.White, true), "badlight")
	if theme.CurrentThemeName != "default" {
		t.Errorf("CurrentThemeName = %q, want default (the light candidate is unusable)", theme.CurrentThemeName)
	}
}
