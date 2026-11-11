package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/forge"
	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-257 step 2: App.View() sets View.WindowTitle from the current view
// and context, computed at render time and held nowhere (Rule 110).

func titleOf(a *App) string { return a.View().WindowTitle }

// switchToContext completes a context switch that carries a session, so the
// router stays on its view instead of going to the auth view.
func switchToContext(a *App, name string) {
	a.Update(ContextSwitchCompleteMsg{
		ContextName: name,
		Config:      testConfig(),
		Forge:       gitlabforge.NewWithClient(nil, "https://gitlab.example.com"),
		GitLabUser:  forge.User{Username: "anthnel"},
	})
}

func wantTitle(t *testing.T, a *App, want string) {
	t.Helper()
	if got := titleOf(a); got != want {
		t.Errorf("WindowTitle = %q, want %q", got, want)
	}
}

func TestRouterSetsTheWindowTitle(t *testing.T) {
	a := routerAt(t, &fakeView{}, 80, 24)
	wantTitle(t, a, "dk · dashboard · default")
}

func TestWindowTitleFollowsTheContext(t *testing.T) {
	a := routerAt(t, &fakeView{}, 80, 24)
	switchToContext(a, "prod")
	wantTitle(t, a, "dk · dashboard · prod")
}

func TestWindowTitleFollowsTheViewWithItsCanonicalName(t *testing.T) {
	a := routerAt(t, &fakeView{}, 80, 24)
	switchToContext(a, "prod")
	a.views[command.ViewSecurity] = &fakeView{}
	press(t, a, "ctrl+p")
	for _, m := range testutil.Type("sec") {
		a.Update(m)
	}
	press(t, a, "enter")
	if a.currentView != command.ViewSecurity {
		t.Fatalf("current view = %s, want security (palette did not switch)", a.currentView)
	}
	wantTitle(t, a, "dk · security · prod")
}

func TestWindowTitleDoesNotChangeOnRenderEvents(t *testing.T) {
	events := []struct {
		name string
		msg  tea.Msg
	}{
		{"animation frame", jobs.FrameMsg{Blink: true}},
		{"jobs changed", jobs.ChangedMsg{}},
		{"resize", tea.WindowSizeMsg{Width: 100, Height: 30}},
		{"blur", tea.BlurMsg{}},
		{"focus", tea.FocusMsg{}},
	}
	for _, ev := range events {
		t.Run(ev.name, func(t *testing.T) {
			a := routerAt(t, &fakeView{}, 80, 24)
			switchToContext(a, "prod")
			before := titleOf(a)
			if before != "dk · dashboard · prod" {
				t.Fatalf("title before = %q, want the dashboard/prod title", before)
			}
			a.Update(ev.msg)
			wantTitle(t, a, before)
		})
	}
}

func TestWindowTitleIgnoresOverlays(t *testing.T) {
	a := routerAt(t, &fakeView{}, 80, 24)
	switchToContext(a, "prod")
	const want = "dk · dashboard · prod"
	wantTitle(t, a, want)

	press(t, a, "ctrl+p")
	wantTitle(t, a, want)
	press(t, a, "esc")

	a.Update(ContextListMsg{Contexts: []string{"default", "prod"}, Current: "prod"})
	if !a.showContextList {
		t.Fatal("the context list did not open")
	}
	wantTitle(t, a, want)
	a.showContextList = false

	a.switchingTo = "other" // the old context stays until the switch completes
	wantTitle(t, a, want)
}

func TestWindowTitleDoesNotDisturbTheOtherTerminalState(t *testing.T) {
	a := routerAt(t, &fakeView{body: "hello"}, 80, 24)
	v := a.View()
	if v.Content != a.Render() {
		t.Error("View().Content != Render()")
	}
	if !v.AltScreen || !v.ReportFocus {
		t.Errorf("AltScreen=%v ReportFocus=%v, want both true", v.AltScreen, v.ReportFocus)
	}
	if v.ProgressBar != nil {
		t.Errorf("ProgressBar = %v, want nil without a job", v.ProgressBar)
	}
	if v.WindowTitle == "" {
		t.Error("WindowTitle is empty")
	}
	if got, want := v.BackgroundColor, a.View().BackgroundColor; got != want {
		t.Errorf("BackgroundColor not stable: %v vs %v", got, want)
	}
}

func TestOnlyTheRouterSetsTheWindowTitle(t *testing.T) {
	assertOnlyOwnerSets(t, "internal/app/view.go", "WindowTitle")
}
