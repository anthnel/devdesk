package app

import (
	"fmt"
	"image/color"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-258 step 4: app.theme "auto" at startup, at the router level.
//
// Names these tests impose on the implementation (the plan's names):
//   - newWithSize applies app.theme itself (palette, CurrentThemeName);
//   - (*App).themeInitCmd() tea.Cmd: the theme part of Init(), batched by Init;
//     nil without "auto" and without a startup warning;
//   - package var themeDetectTimeout (500 ms) and message themeDetectTimeoutMsg{gen int};
//   - tea.BackgroundColorMsg handled by the router, never forwarded to a view;
//   - (*App).themeResolution() theme.Resolution: what is in force (step 6).

const wantDarkBg = "#1e1e2e" // the default theme

func autoConfig(theme_, dark, light string) *config.Config {
	cfg := testConfig()
	cfg.App.Theme = theme_
	cfg.App.ThemeDark = dark
	cfg.App.ThemeLight = light
	return cfg
}

// autoRouter builds the real router (newWithSize) on a TrueColor terminal, with
// HOME isolated and the global palette restored afterwards.
func autoRouter(t *testing.T, cfg *config.Config) *App {
	t.Helper()
	isolateHome(t)
	reset := func() {
		theme.ApplyTheme(theme.DefaultTheme())
		theme.CurrentThemeName = "default"
	}
	t.Cleanup(reset)
	reset()
	a := newWithSize(cfg, 120, 40)
	sendTrueColor(t, a)
	return a
}

func isBackgroundRequest(m tea.Msg) bool {
	return fmt.Sprintf("%T", m) == fmt.Sprintf("%T", tea.RequestBackgroundColor())
}

func hasBackgroundRequest(msgs []tea.Msg) bool {
	for _, m := range msgs {
		if isBackgroundRequest(m) {
			return true
		}
	}
	return false
}

// initMsgs runs the theme part of Init with every timer shortened.
func initMsgs(t *testing.T, a *App) []tea.Msg {
	t.Helper()
	testutil.FastTimers(t, &themeDetectTimeout)
	testutil.FastTimers(t, &components.FooterMsgDuration)
	return testutil.Msgs(a.themeInitCmd())
}

func footerPosts(msgs []tea.Msg) []components.PostFooterMsg {
	var out []components.PostFooterMsg
	for _, m := range msgs {
		if p, ok := m.(components.PostFooterMsg); ok {
			out = append(out, p)
		}
	}
	return out
}

// answer feeds the terminal's reply and returns what the router emitted.
func answer(t *testing.T, a *App, c color.Color) []tea.Msg {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	_, cmd := a.Update(tea.BackgroundColorMsg{Color: c})
	return testutil.Msgs(cmd)
}

// ── Criterion 1: nothing without auto ────────────────────────────────────────

func TestThemeAbsentOrDefaultNeverAsksTheTerminal(t *testing.T) {
	for _, name := range []string{"", "default"} {
		t.Run("app.theme="+name, func(t *testing.T) {
			a := autoRouter(t, autoConfig(name, "", ""))
			msgs := initMsgs(t, a)
			if hasBackgroundRequest(msgs) {
				t.Error("Init asked for the terminal background without app.theme: auto")
			}
			for _, m := range msgs {
				if _, ok := m.(themeDetectTimeoutMsg); ok {
					t.Error("a detection timer was armed without app.theme: auto")
				}
			}
			if got := bgHex(a); got != wantDarkBg {
				t.Errorf("View background = %s, want %s (unchanged rendering)", got, wantDarkBg)
			}
			if theme.CurrentThemeName != "default" {
				t.Errorf("CurrentThemeName = %q, want default", theme.CurrentThemeName)
			}
		})
	}
}

func TestANamedThemeAtStartupNeverAsksTheTerminal(t *testing.T) {
	a := autoRouter(t, autoConfig("github-dark", "", ""))
	if hasBackgroundRequest(initMsgs(t, a)) {
		t.Error("a named theme triggered a background request")
	}
	if theme.CurrentThemeName != "github-dark" {
		t.Errorf("CurrentThemeName = %q, want github-dark", theme.CurrentThemeName)
	}
}

func TestAnUnknownThemeAtStartupWarnsOnceFromInit(t *testing.T) {
	a := autoRouter(t, autoConfig("nope", "", ""))
	posts := footerPosts(initMsgs(t, a))
	wantDefaultWithWarning(t, posts, "nope")
}

// ── Criteria 3 and 4: the startup state ──────────────────────────────────────

func TestThemeAutoAppliesTheDarkPaletteAtOnceAndAsksOnce(t *testing.T) {
	if themeDetectTimeout != 500*time.Millisecond {
		t.Fatalf("themeDetectTimeout = %v, want 500ms", themeDetectTimeout)
	}
	a := autoRouter(t, autoConfig("auto", "", ""))
	if theme.CurrentThemeName != "default" {
		t.Errorf("CurrentThemeName = %q, want theme_dark's default", theme.CurrentThemeName)
	}
	if got := theme.Hex(theme.ColorBackground); got != wantDarkBg {
		t.Errorf("palette background = %s, want %s applied at once", got, wantDarkBg)
	}

	msgs := initMsgs(t, a)
	requests := 0
	for _, m := range msgs {
		if isBackgroundRequest(m) {
			requests++
		}
	}
	if requests != 1 {
		t.Errorf("Init asked for the background %d times, want exactly 1: %v", requests, msgs)
	}
	if _, ok := firstOf[themeDetectTimeoutMsg](msgs); !ok {
		t.Errorf("Init armed no themeDetectTimeoutMsg: %v", msgs)
	}
	if posts := footerPosts(msgs); len(posts) != 0 {
		t.Errorf("a clean auto start posted footer messages: %+v", posts)
	}
}

func TestThemeAutoStartsFromThemeDark(t *testing.T) {
	autoRouter(t, autoConfig("auto", "github-dark", ""))
	if theme.CurrentThemeName != "github-dark" {
		t.Errorf("CurrentThemeName = %q, want github-dark (app.theme_dark)", theme.CurrentThemeName)
	}
}

// The guard of the plan: while the answer is pending DevDesk has set no terminal
// background, so the terminal cannot answer with DevDesk's own colour.
func TestThemeAutoLeavesTheTerminalBackgroundUnsetWhilePending(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	if c := a.View().BackgroundColor; c != nil {
		t.Errorf("View().BackgroundColor = %s while detection is pending, want nil", theme.Hex(c))
	}
}

// ── Criterion 2: the answer picks the theme ──────────────────────────────────

func TestThemeAutoFollowsTheTerminalAnswer(t *testing.T) {
	cases := []struct {
		name       string
		answer     color.Color
		dark       string
		light      string
		wantTheme  string
		wantBg     string
		wantReason string
	}{
		{"dark terminal, defaults", color.Black, "", "", "default", wantDarkBg, "terminal is dark"},
		{"light terminal, defaults", color.White, "", "", "github-light", "#ffffff", "terminal is light"},
		{"dark terminal, theme_dark set", color.Black, "github-dark", "", "github-dark", "#0d1117", "terminal is dark"},
		{"light terminal, theme_light set", color.White, "", "github-light", "github-light", "#ffffff", "terminal is light"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := autoRouter(t, autoConfig("auto", tc.dark, tc.light))
			msgs := answer(t, a, tc.answer)
			if posts := footerPosts(msgs); len(posts) != 0 {
				t.Errorf("posted footer messages: %+v", posts)
			}
			if theme.CurrentThemeName != tc.wantTheme {
				t.Errorf("CurrentThemeName = %q, want %q", theme.CurrentThemeName, tc.wantTheme)
			}
			if got := bgHex(a); got != tc.wantBg {
				t.Errorf("View background = %s, want %s (the theme's uniform background)", got, tc.wantBg)
			}
			r := a.themeResolution()
			if r.Name != tc.wantTheme || r.Reason != tc.wantReason || r.Requested != "auto" {
				t.Errorf("themeResolution = %+v, want Name %q Reason %q Requested auto", r, tc.wantTheme, tc.wantReason)
			}
		})
	}
}

func TestBackgroundColorMsgIsNotForwardedToTheView(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	fv := &fakeView{}
	a.views[a.currentView] = fv
	answer(t, a, color.White)
	if msg, ok := receivedOf[tea.BackgroundColorMsg](fv); ok {
		t.Errorf("the view received %v; the router owns it", msg)
	}
}

// ── Criteria 4 and 5: timeout, late reply, one query per process ─────────────

func TestThemeAutoTimeoutFallsBackToDarkSilently(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	timeout, ok := firstOf[themeDetectTimeoutMsg](initMsgs(t, a))
	if !ok {
		t.Fatal("Init armed no themeDetectTimeoutMsg")
	}
	_, cmd := a.Update(timeout)
	if posts := footerPosts(testutil.Msgs(cmd)); len(posts) != 0 {
		t.Errorf("the timeout posted footer messages: %+v", posts)
	}
	if theme.CurrentThemeName != "default" {
		t.Errorf("CurrentThemeName = %q, want the dark default", theme.CurrentThemeName)
	}
	if got := bgHex(a); got != wantDarkBg {
		t.Errorf("View background = %s, want %s once the wait is over", got, wantDarkBg)
	}
	if r := a.themeResolution(); r.Reason != "no answer from the terminal" || r.Warning != "" {
		t.Errorf("themeResolution = %+v, want the no-answer reason and no warning", r)
	}
}

func TestThemeAutoIgnoresAReplyAfterTheTimeout(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	timeout, _ := firstOf[themeDetectTimeoutMsg](initMsgs(t, a))
	a.Update(timeout)
	answer(t, a, color.White)
	if theme.CurrentThemeName != "default" {
		t.Errorf("a late light reply switched the palette to %q", theme.CurrentThemeName)
	}
	if got := bgHex(a); got != wantDarkBg {
		t.Errorf("View background = %s after a late reply, want %s", got, wantDarkBg)
	}
}

func TestThemeAutoIgnoresAStaleTimeout(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	timeout, _ := firstOf[themeDetectTimeoutMsg](initMsgs(t, a))
	timeout.gen += 1000
	a.Update(timeout)
	if c := a.View().BackgroundColor; c != nil {
		t.Errorf("a stale timeout ended the wait (background %s)", theme.Hex(c))
	}
}

func TestThemeAutoAnswersOnceAndCachesTheFirst(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	answer(t, a, color.White)
	answer(t, a, color.Black) // a second reply must not flip the theme
	if theme.CurrentThemeName != "github-light" {
		t.Errorf("CurrentThemeName = %q after a second reply, want github-light", theme.CurrentThemeName)
	}
}

// ── Criterion 7 inside auto, and criterion 8 ─────────────────────────────────

func TestThemeAutoCandidateThatCannotBeUsedGivesDefaultAndOneWarning(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T)
		light string
	}{
		{"unknown", func(*testing.T) {}, "nope"},
		{"unreadable", func(t *testing.T) { writeThemeFile(t, "garbled", `{"name": `) }, "garbled"},
		{"contrast", func(t *testing.T) { writeTheme(t, "flat", theme.DefaultTheme().ColorText) }, "flat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The file exists before the router is built: the warning is
			// emitted once at startup and not repeated on the reply.
			a := autoRouterWith(t, autoConfig("auto", "", tc.light), tc.setup)
			posts := footerPosts(initMsgs(t, a))
			posts = append(posts, footerPosts(answer(t, a, color.White))...)
			wantDefaultWithWarning(t, posts, tc.light)
		})
	}
}

// ── Criterion 6: an explicit choice replaces auto ────────────────────────────

func TestANamedThemeChosenInSessionReplacesAutoAndIsKept(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	posts := saveThemeFooter(t, a, "github-dark")
	if len(posts) != 0 {
		t.Errorf("posted footer messages: %+v", posts)
	}
	if theme.CurrentThemeName != "github-dark" {
		t.Errorf("CurrentThemeName = %q, want github-dark", theme.CurrentThemeName)
	}
	if a.config.App.Theme != "github-dark" {
		t.Errorf("router config theme = %q, want github-dark (no longer auto)", a.config.App.Theme)
	}
	if got := bgHex(a); got != "#0d1117" {
		t.Errorf("View background = %s, want #0d1117: the wait must be cancelled", got)
	}
	answer(t, a, color.White) // the reply that was still on its way
	if theme.CurrentThemeName != "github-dark" {
		t.Errorf("a late reply replaced the explicit choice with %q", theme.CurrentThemeName)
	}
}

func TestChoosingAutoInSessionReusesTheCachedAnswerWithoutAskingAgain(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	answer(t, a, color.White)
	saveThemeFooter(t, a, "default")
	if theme.CurrentThemeName != "default" {
		t.Fatalf("setup: CurrentThemeName = %q", theme.CurrentThemeName)
	}

	testutil.FastTimers(t, &components.FooterMsgDuration)
	testutil.FastTimers(t, &themeDetectTimeout)
	cfg := autoConfig("auto", "", "")
	_, cmd := a.Update(configuration.ConfigSavedMsg{Config: cfg, ThemeChanged: true})
	if hasBackgroundRequest(testutil.Msgs(cmd)) {
		t.Error("auto with a cached answer queried the terminal again")
	}
	if theme.CurrentThemeName != "github-light" {
		t.Errorf("CurrentThemeName = %q, want github-light from the cached answer", theme.CurrentThemeName)
	}
	if got := bgHex(a); got != "#ffffff" {
		t.Errorf("View background = %s, want #ffffff at once", got)
	}
}

// ── Rule 110: the detection is race free ─────────────────────────────────────

// The Cmds run on their own goroutines while Update and View run on the loop's.
// Under `mise run test-race` a Cmd that reads router state would be reported.
func TestThemeAutoDetectionIsRaceFree(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	testutil.FastTimers(t, &themeDetectTimeout)
	testutil.FastTimers(t, &components.FooterMsgDuration)
	cmd := a.themeInitCmd()

	var wg sync.WaitGroup
	var msgs []tea.Msg
	wg.Add(1)
	go func() {
		defer wg.Done()
		msgs = testutil.Msgs(cmd)
	}()
	for range 50 {
		_ = a.View()
	}
	a.Update(tea.BackgroundColorMsg{Color: color.White})
	wg.Wait()

	if timeout, ok := firstOf[themeDetectTimeoutMsg](msgs); ok {
		a.Update(timeout) // arrives after the answer: must change nothing
	}
	if theme.CurrentThemeName != "github-light" {
		t.Errorf("CurrentThemeName = %q, want github-light", theme.CurrentThemeName)
	}
}

func firstOf[T tea.Msg](msgs []tea.Msg) (T, bool) {
	for _, m := range msgs {
		if v, ok := m.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}
