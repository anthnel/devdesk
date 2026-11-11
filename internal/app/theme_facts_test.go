package app

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-258, step 6b at the router level: the :config view's "Theme in use"
// row follows the router's resolution. The router builds the view with
// configuration.New(cfg, mcp, a.themeFacts()) and sends a
// configuration.ThemeFactsMsg at startup, when detection ends and on every
// change, either by returning it from Update as a Cmd or by handing it to the
// view directly; these tests accept both (a ThemeFactsMsg found in a returned
// Cmd is fed back to the router, as the runtime would).

// openConfigView makes :config the view on screen.
func openConfigView(a *App) {
	a.createView(command.ViewConfiguration)
	a.currentView = command.ViewConfiguration
	// The runtime sizes a new view through requestResize; an unsized
	// :config view renders nothing, so do it here.
	a.resize(a.width, a.height)
}

// themeInUseRow is the rendered "Theme in use" line of the :config view.
func themeInUseRow(t *testing.T, a *App) string {
	t.Helper()
	v, ok := a.views[command.ViewConfiguration]
	if !ok {
		t.Fatal("no :config view")
	}
	for _, line := range strings.Split(ansi.Strip(v.Render()), "\n") {
		if strings.Contains(line, "Theme in use") {
			return line
		}
	}
	t.Fatalf("no Theme in use row in:\n%s", ansi.Strip(v.Render()))
	return ""
}

// settleThemeFacts drains cmd and feeds back every ThemeFactsMsg it holds.
func settleThemeFacts(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	testutil.FastTimers(t, &themeDetectTimeout)
	for _, m := range testutil.Msgs(cmd) {
		if f, ok := m.(configuration.ThemeFactsMsg); ok {
			a.Update(f)
		}
	}
}

func wantThemeRow(t *testing.T, a *App, parts ...string) {
	t.Helper()
	row := themeInUseRow(t, a)
	for _, p := range parts {
		if !strings.Contains(row, p) {
			t.Errorf("Theme in use row %q lacks %q", row, p)
		}
	}
}

func TestConfigViewOpensWithTheThemeInUse(t *testing.T) {
	a := autoRouter(t, autoConfig("nord", "", ""))
	openConfigView(a)
	wantThemeRow(t, a, "nord")
}

func TestConfigViewFollowsTheTerminalAnswer(t *testing.T) {
	for _, tt := range []struct {
		name string
		bg   color.Color
		want []string
	}{
		{"light", color.White, []string{"github-light", "terminal is light"}},
		{"dark", color.Black, []string{"default", "terminal is dark"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := autoRouter(t, autoConfig("auto", "", ""))
			openConfigView(a)
			_, cmd := a.Update(tea.BackgroundColorMsg{Color: tt.bg})
			settleThemeFacts(t, a, cmd)
			wantThemeRow(t, a, tt.want...)
		})
	}
}

func TestConfigViewSaysThereWasNoAnswer(t *testing.T) {
	testutil.FastTimers(t, &themeDetectTimeout)
	a := autoRouter(t, autoConfig("auto", "", ""))
	openConfigView(a)
	_, cmd := a.Update(themeDetectTimeoutMsg{gen: a.theme.gen})
	settleThemeFacts(t, a, cmd)
	wantThemeRow(t, a, "default", "no answer from the terminal")
}

func TestConfigViewFollowsACustomAutoPair(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "nord", "solarized-light"))
	openConfigView(a)
	_, cmd := a.Update(tea.BackgroundColorMsg{Color: color.White})
	settleThemeFacts(t, a, cmd)
	wantThemeRow(t, a, "solarized-light", "terminal is light")
}

// A save from :config re-resolves and the row follows, including leaving auto.
func TestConfigViewFollowsASave(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	openConfigView(a)
	_, cmd := a.Update(tea.BackgroundColorMsg{Color: color.White})
	settleThemeFacts(t, a, cmd)

	_, cmd = a.Update(configuration.ConfigSavedMsg{Config: autoConfig("nord", "", ""), ThemeChanged: true})
	settleThemeFacts(t, a, cmd)

	row := themeInUseRow(t, a)
	if !strings.Contains(row, "nord") || strings.Contains(row, "terminal is") {
		t.Errorf("after choosing nord the row reads %q, want nord and no auto reason", row)
	}
}

// Choosing auto in session with a cached answer applies it at once and the row says so.
func TestConfigViewAfterChoosingAutoWithACachedAnswer(t *testing.T) {
	a := autoRouter(t, autoConfig("auto", "", ""))
	openConfigView(a)
	_, cmd := a.Update(tea.BackgroundColorMsg{Color: color.White})
	settleThemeFacts(t, a, cmd)
	_, cmd = a.Update(configuration.ConfigSavedMsg{Config: autoConfig("nord", "", ""), ThemeChanged: true})
	settleThemeFacts(t, a, cmd)

	_, cmd = a.Update(configuration.ConfigSavedMsg{Config: autoConfig("auto", "", ""), ThemeChanged: true})
	settleThemeFacts(t, a, cmd)
	wantThemeRow(t, a, "github-light", "terminal is light")
}
