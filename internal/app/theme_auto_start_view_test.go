package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
)

// DEVDESK-643: the theme tests drain, through testutil.Msgs, the start view's
// deferred Init (settleTheme -> runDeferredStartInit). The default start view is
// the dashboard, whose Init arms scheduleRefresh, a real 10 s tea.Tick, so every
// such test slept 10 s. The auto-theme helpers therefore must not leave the
// dashboard as the start view. A Cmd cannot be inspected for a tick without
// running it (and waiting for it), so the guard checks the start view itself.
// It checks the start view only, not that the start view's Init is free of
// timers: another start view that arms a timer later would not be caught here
// (a duration guard is tracked in S-02c, DEVDESK-646).

// TestAutoThemeHelpersDoNotStartOnTheDashboard fails when a router built by the
// auto-theme helpers starts on the dashboard, which brings back the 10 s wait in
// every test that settles the theme.
func TestAutoThemeHelpersDoNotStartOnTheDashboard(t *testing.T) {
	tests := []struct {
		name  string
		start func(t *testing.T) command.ViewType
	}{
		{"autoConfig", func(t *testing.T) command.ViewType {
			return defaultView(autoConfig("auto", "", ""))
		}},
		{"autoRouter", func(t *testing.T) command.ViewType {
			return autoRouter(t, autoConfig("auto", "", "")).currentView
		}},
		{"autoRouterWith", func(t *testing.T) command.ViewType {
			return autoRouterWith(t, autoConfig("auto", "", ""), nil).currentView
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.start(t); got == command.ViewDashboard {
				t.Errorf("start view = %q: its deferred Init arms a 10 s refresh tick, so every "+
					"test that settles the theme waits 10 s (set cfg.App.DefaultView in autoConfig)", got)
			}
		})
	}
}
