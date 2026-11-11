package app

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// DEVDESK-258, review 2/2: the start view's Init and app.theme: auto.
//
// The start view is :workspaces (app.default_view), whose Init is quick and
// local (HOME is isolated) and yields exactly one workspaces.EntriesLoadedMsg
// per call: counting that message counts Init runs, with no test seam.
//
// Behavior these tests impose on the implementation:
//   - while the startup detection is pending, App.Init does not run the start
//     view's Init;
//   - settleTheme (terminal answer, dark or light, or timeout) runs it, once,
//     on the view that is current after rebuildViews when the theme changes;
//   - a late answer or a stale timeout after settling runs nothing more;
//   - without auto at startup, App.Init runs it as before and no answer
//     triggers another run;
//   - choosing auto in session (handleConfigSaved) never runs the Init of a
//     view that was already initialised, at save or at the answer.

func workspacesAuto(t *testing.T, themeName string) *App {
	t.Helper()
	cfg := autoConfig(themeName, "", "")
	cfg.App.DefaultView = "workspaces"
	a := autoRouter(t, cfg)
	if a.currentView != command.ViewWorkspaces {
		t.Fatalf("setup: currentView = %q, want workspaces", a.currentView)
	}
	return a
}

// appInit drains App.Init with the timers shortened.
func appInit(t *testing.T, a *App) []tea.Msg {
	t.Helper()
	testutil.FastTimers(t, &themeDetectTimeout)
	testutil.FastTimers(t, &components.FooterMsgDuration)
	return testutil.Msgs(a.Init())
}

// startInits counts how many times the start view's Init ran in msgs.
func startInits(msgs []tea.Msg) int {
	n := 0
	for _, m := range msgs {
		if _, ok := m.(workspaces.EntriesLoadedMsg); ok {
			n++
		}
	}
	return n
}

func wantInits(t *testing.T, what string, msgs []tea.Msg, want int) {
	t.Helper()
	if got := startInits(msgs); got != want {
		t.Errorf("%s: the start view's Init ran %d time(s), want %d", what, got, want)
	}
}

func updateMsgs(t *testing.T, a *App, msg tea.Msg) []tea.Msg {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	_, cmd := a.Update(msg)
	return testutil.Msgs(cmd)
}

// (a)
func TestAutoPendingInitDoesNotRunTheStartViewInit(t *testing.T) {
	a := workspacesAuto(t, "auto")
	msgs := appInit(t, a)
	if _, ok := firstOf[themeDetectTimeoutMsg](msgs); !ok {
		t.Fatal("setup: Init armed no detection timeout, auto is not pending")
	}
	wantInits(t, "App.Init while detection is pending", msgs, 0)
}

// (b)
func TestALightAnswerRunsTheRebuiltViewsInitOnce(t *testing.T) {
	a := workspacesAuto(t, "auto")
	wantInits(t, "App.Init", appInit(t, a), 0)

	msgs := updateMsgs(t, a, tea.BackgroundColorMsg{Color: color.White})
	if theme.CurrentThemeName != "github-light" {
		t.Fatalf("CurrentThemeName = %q, want github-light", theme.CurrentThemeName)
	}
	wantInits(t, "light answer", msgs, 1)

	// The loading result reaches the view on screen, not a dropped one.
	loaded, ok := firstOf[workspaces.EntriesLoadedMsg](msgs)
	if !ok {
		t.Fatal("no EntriesLoadedMsg to route")
	}
	// The Cmd was built after rebuildViews, so it belongs to the view now in
	// a.views; routing its result must find that view.
	if _, ok := a.views[a.currentView]; !ok {
		t.Fatal("no current view to receive the loading result")
	}
	a.Update(loaded)
}

// (c)
func TestADarkAnswerRunsTheStartViewInitOnce(t *testing.T) {
	a := workspacesAuto(t, "auto")
	wantInits(t, "App.Init", appInit(t, a), 0)

	msgs := updateMsgs(t, a, tea.BackgroundColorMsg{Color: color.Black})
	wantInits(t, "dark answer", msgs, 1)
}

// (d)
func TestATimeoutRunsTheStartViewInitOnce(t *testing.T) {
	a := workspacesAuto(t, "auto")
	init := appInit(t, a)
	wantInits(t, "App.Init", init, 0)
	timeout, ok := firstOf[themeDetectTimeoutMsg](init)
	if !ok {
		t.Fatal("Init armed no timeout")
	}
	wantInits(t, "timeout", updateMsgs(t, a, timeout), 1)
}

// (e)
func TestNothingRunsTheStartViewInitTwice(t *testing.T) {
	cases := []struct {
		name  string
		first func(timeout tea.Msg) tea.Msg
		then  func(timeout tea.Msg) tea.Msg
	}{
		{"answer then timeout",
			func(tea.Msg) tea.Msg { return tea.BackgroundColorMsg{Color: color.White} },
			func(to tea.Msg) tea.Msg { return to }},
		{"timeout then late answer",
			func(to tea.Msg) tea.Msg { return to },
			func(tea.Msg) tea.Msg { return tea.BackgroundColorMsg{Color: color.White} }},
		{"answer then second answer",
			func(tea.Msg) tea.Msg { return tea.BackgroundColorMsg{Color: color.Black} },
			func(tea.Msg) tea.Msg { return tea.BackgroundColorMsg{Color: color.White} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := workspacesAuto(t, "auto")
			init := appInit(t, a)
			timeout, ok := firstOf[themeDetectTimeoutMsg](init)
			if !ok {
				t.Fatal("Init armed no timeout")
			}
			wantInits(t, "first settle", updateMsgs(t, a, tc.first(timeout)), 1)
			wantInits(t, "after settling", updateMsgs(t, a, tc.then(timeout)), 0)
		})
	}
}

// (f)
func TestWithoutAutoInitRunsInAppInitAndNoAnswerRerunsIt(t *testing.T) {
	for _, name := range []string{"", "default", "nord"} {
		t.Run("app.theme="+name, func(t *testing.T) {
			a := workspacesAuto(t, name)
			wantInits(t, "App.Init", appInit(t, a), 1)
			late := updateMsgs(t, a, tea.BackgroundColorMsg{Color: color.White})
			wantInits(t, "an unsolicited answer", late, 0)
		})
	}
}

// (g)
func TestChoosingAutoInSessionDoesNotReplayInit(t *testing.T) {
	a := workspacesAuto(t, "nord")
	wantInits(t, "App.Init", appInit(t, a), 1)
	testutil.FastTimers(t, &themeQueryDelay)

	cfg := autoConfig("auto", "", "")
	cfg.App.DefaultView = "workspaces"
	save := updateMsgs(t, a, configuration.ConfigSavedMsg{Config: cfg, ThemeChanged: true})
	wantInits(t, "the save", save, 0)

	due, ok := firstOf[themeQueryDueMsg](save)
	if !ok {
		t.Fatal("setup: the save deferred no terminal query")
	}
	wantInits(t, "the deferred query", updateMsgs(t, a, due), 0)
	wantInits(t, "the answer", updateMsgs(t, a, tea.BackgroundColorMsg{Color: color.White}), 0)
}

// ── Review 3: the deferred start Init is consumed by whoever runs an Init first ──
//
// Behavior these tests impose on the implementation:
//   - switchView and reinitializeViews run Init on the view they put on screen;
//     while startInitDeferred is set they clear it, so settleTheme (answer or
//     timeout) never runs a second Init;
//   - handleConfigSaved, when a named theme ends the wait without settleTheme
//     (pending false, startInitDeferred still set), runs the on-screen view's
//     Init after rebuildViews, once, and clears the flag.

// autoAt builds an auto router (detection pending) starting on view.
func autoAt(t *testing.T, view string) *App {
	t.Helper()
	cfg := autoConfig("auto", "", "")
	cfg.App.DefaultView = view
	a := autoRouter(t, cfg)
	testutil.FastTimers(t, &themeDetectTimeout)
	testutil.FastTimers(t, &themeQueryDelay)
	return a
}

// settlers are the three ways a pending detection ends.
var settlers = []struct {
	name string
	msg  func(timeout tea.Msg) tea.Msg
}{
	{"light answer", func(tea.Msg) tea.Msg { return tea.BackgroundColorMsg{Color: color.White} }},
	{"dark answer", func(tea.Msg) tea.Msg { return tea.BackgroundColorMsg{Color: color.Black} }},
	{"timeout", func(to tea.Msg) tea.Msg { return to }},
}

func drain(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	return testutil.Msgs(cmd)
}

// (h) switchView during the wait: its Init is the only one of the target view.
func TestSwitchViewDuringTheWaitIsNotInitialisedAgainBySettling(t *testing.T) {
	for _, s := range settlers {
		t.Run(s.name, func(t *testing.T) {
			a := autoAt(t, "status")
			init := appInit(t, a)
			timeout, ok := firstOf[themeDetectTimeoutMsg](init)
			if !ok {
				t.Fatal("setup: Init armed no timeout, auto is not pending")
			}
			sw := drain(t, a.switchView(command.ViewWorkspaces))
			wantInits(t, "switchView", sw, 1)
			if a.currentView != command.ViewWorkspaces {
				t.Fatalf("setup: currentView = %q", a.currentView)
			}
			wantInits(t, "the settling that follows ("+s.name+")", updateMsgs(t, a, s.msg(timeout)), 0)
		})
	}
}

// (i) reinitializeViews (context switch) during the wait, same demand.
func TestReinitializeViewsDuringTheWaitIsNotInitialisedAgainBySettling(t *testing.T) {
	for _, s := range settlers {
		t.Run(s.name, func(t *testing.T) {
			a := workspacesAuto(t, "auto")
			testutil.FastTimers(t, &themeDetectTimeout)
			init := appInit(t, a)
			timeout, ok := firstOf[themeDetectTimeoutMsg](init)
			if !ok {
				t.Fatal("setup: Init armed no timeout, auto is not pending")
			}
			wantInits(t, "App.Init", init, 0)
			wantInits(t, "reinitializeViews", drain(t, a.reinitializeViews()), 1)
			wantInits(t, "the settling that follows ("+s.name+")", updateMsgs(t, a, s.msg(timeout)), 0)
		})
	}
}

// savedNamed is a save that picks a named theme.
func savedNamed(view string) configuration.ConfigSavedMsg {
	cfg := autoConfig("nord", "", "")
	cfg.App.DefaultView = view
	return configuration.ConfigSavedMsg{Config: cfg, ThemeChanged: true}
}

// (j) a named theme chosen during the wait must not lose the start view's Init.
// The start view is the configuration view, which a save keeps rather than
// rebuilds: its fake counts the Inits it receives.
func TestANamedThemeDuringTheWaitInitialisesTheKeptStartViewOnce(t *testing.T) {
	a := autoAt(t, "configuration")
	if a.currentView != command.ViewConfiguration {
		t.Fatalf("setup: currentView = %q, want configuration", a.currentView)
	}
	fake := &fakeView{}
	a.views[command.ViewConfiguration] = fake
	init := appInit(t, a)
	timeout, ok := firstOf[themeDetectTimeoutMsg](init)
	if !ok {
		t.Fatal("setup: Init armed no timeout, auto is not pending")
	}
	if fake.inits != 0 {
		t.Fatalf("setup: App.Init ran the start view's Init (%d) while pending", fake.inits)
	}

	updateMsgs(t, a, savedNamed("configuration"))
	if a.themeDetectionPending() {
		t.Fatal("setup: a named theme left the detection pending")
	}
	if fake.inits != 1 {
		t.Errorf("after the named theme: start view Init ran %d time(s), want 1", fake.inits)
	}

	// Nothing afterwards runs it again: stale timeout, late answer, another
	// named theme, then auto chosen again and answered.
	updateMsgs(t, a, timeout)
	updateMsgs(t, a, tea.BackgroundColorMsg{Color: color.White})
	updateMsgs(t, a, savedNamed("configuration"))
	cfg := autoConfig("auto", "", "")
	cfg.App.DefaultView = "configuration"
	save := updateMsgs(t, a, configuration.ConfigSavedMsg{Config: cfg, ThemeChanged: true})
	if due, ok := firstOf[themeQueryDueMsg](save); ok {
		updateMsgs(t, a, due)
	}
	updateMsgs(t, a, tea.BackgroundColorMsg{Color: color.Black})
	if fake.inits != 1 {
		t.Errorf("after later theme choices: start view Init ran %d time(s), want still 1", fake.inits)
	}
}

// (k) same loss on a view the save rebuilds: counted through its loading message.
func TestANamedThemeDuringTheWaitInitialisesTheRebuiltStartViewOnce(t *testing.T) {
	a := workspacesAuto(t, "auto")
	testutil.FastTimers(t, &themeDetectTimeout)
	init := appInit(t, a)
	timeout, ok := firstOf[themeDetectTimeoutMsg](init)
	if !ok {
		t.Fatal("setup: Init armed no timeout, auto is not pending")
	}
	wantInits(t, "App.Init", init, 0)
	wantInits(t, "the named theme", updateMsgs(t, a, savedNamed("workspaces")), 1)
	wantInits(t, "the stale timeout", updateMsgs(t, a, timeout), 0)
	wantInits(t, "a late answer", updateMsgs(t, a, tea.BackgroundColorMsg{Color: color.White}), 0)
	wantInits(t, "another named theme", updateMsgs(t, a, savedNamed("workspaces")), 0)
}

// (l) a named theme saved when no Init was held back (startup was not auto)
// runs nothing: the flag, not the save, decides.
func TestANamedThemeWithoutADeferredInitRunsNothing(t *testing.T) {
	a := workspacesAuto(t, "nord")
	wantInits(t, "App.Init", appInit(t, a), 1)
	wantInits(t, "the save", updateMsgs(t, a, savedNamed("workspaces")), 0)
}
