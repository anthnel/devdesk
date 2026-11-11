package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── DEVDESK-210 step 6: app.when_unfocused ──────────────────────────────────

func policyConfig(v string) *config.Config {
	cfg := testConfig()
	cfg.App.WhenUnfocused = v
	return cfg
}

// runPolicyRouter is a blink router whose policy was set to run through the
// router's own entry point (applyWhenUnfocused, called by handleConfigSaved
// and the context switch).
func runPolicyRouter(t *testing.T) (*App, *blinkView) {
	t.Helper()
	a, v, _ := blinkRouter(t, true)
	_ = a.applyWhenUnfocused(policyConfig(config.WhenUnfocusedRun))
	return a, v
}

func TestRunPolicyReachesTheRouterOnSaveAndContextSwitch(t *testing.T) {
	for name, apply := range applyBy {
		t.Run(name, func(t *testing.T) {
			a := router(t, &fakeView{})
			apply(a, policyConfig(config.WhenUnfocusedRun))
			// Views were rebuilt: install the one under observation now.
			v := &fakeView{}
			a.currentView = command.ViewDashboard
			a.views[command.ViewDashboard] = v

			a.Update(tea.BlurMsg{})
			if n := len(focusMsgsOf(v)); n != 0 {
				t.Errorf("run: a blur told the view %d times, want 0", n)
			}

			apply(a, policyConfig("")) // absent means pause
			v = &fakeView{}
			a.currentView = command.ViewDashboard // a context switch moves to the auth view
			a.views[command.ViewDashboard] = v
			a.terminalBlurred = false
			a.Update(tea.BlurMsg{})
			if n := len(focusMsgsOf(v)); n != 1 {
				t.Errorf("pause: a blur told the view %d times, want 1", n)
			}
		})
	}
}

func TestRunPolicyBlurKeepsTheChainAlive(t *testing.T) {
	a, v := runPolicyRouter(t)
	startBlinkChain(t, a)

	a.Update(tea.BlurMsg{})
	cmd := tickAt(a, 1500)

	if _, ok := testutil.MsgOf[animationTickMsg](cmd); !ok {
		t.Error("run: the chain was not renewed while the terminal is blurred")
	}
	if !a.anim.ticking {
		t.Error("run: the router no longer believes a chain is alive")
	}
	if d, ok := a.wantedCadence(); !ok || d == 0 {
		t.Errorf("run: wantedCadence = (%v, %v) while blurred, want a cadence", d, ok)
	}
	if lastFrame(t, v).Blink {
		t.Error("run: the off phase was not handed to the view (animation is not suspended)")
	}
}

func TestRunPolicySendsNoFocusMessageOnEitherEdge(t *testing.T) {
	a, v := runPolicyRouter(t)

	a.Update(tea.BlurMsg{})
	a.Update(tea.FocusMsg{})

	if n := len(focusMsgsOf(&v.fakeView)); n != 0 {
		t.Errorf("run: views received %d TerminalFocusMsg, want none (nothing to resume)", n)
	}
}

func TestPausePolicyIsTheDefault(t *testing.T) {
	a, v, _ := blinkRouter(t, true)
	startBlinkChain(t, a)
	a.Update(tea.BlurMsg{})
	if _, ok := a.wantedCadence(); ok {
		t.Error("default policy: wantedCadence is wanted while blurred")
	}
	if n := len(focusMsgsOf(&v.fakeView)); n != 1 {
		t.Errorf("default policy: view told %d times, want 1", n)
	}
}

// Saving the setting while blurred applies at once.
func TestPauseToRunWhileBlurredResumesTheViewOnScreen(t *testing.T) {
	a, v, _ := blinkRouter(t, true)
	a.Update(tea.BlurMsg{})

	_ = a.applyWhenUnfocused(policyConfig(config.WhenUnfocusedRun))

	msgs := focusMsgsOf(&v.fakeView)
	if len(msgs) != 2 || msgs[1] != (shared.TerminalFocusMsg{Focused: true, Resume: true}) {
		t.Fatalf("view got %+v, want [blur, {Focused:true Resume:true}]", msgs)
	}
	if a.ensureTick() == nil {
		t.Error("pause -> run while blurred: no chain restarted")
	}
}

func TestRunToPauseWhileBlurredSuspendsAtOnce(t *testing.T) {
	a, v := runPolicyRouter(t)
	a.Update(tea.BlurMsg{})

	_ = a.applyWhenUnfocused(policyConfig(config.WhenUnfocusedPause))

	msgs := focusMsgsOf(&v.fakeView)
	if len(msgs) != 1 || msgs[0] != (shared.TerminalFocusMsg{Focused: false, Resume: false}) {
		t.Fatalf("view got %+v, want [{Focused:false Resume:false}]", msgs)
	}
	if _, ok := a.wantedCadence(); ok {
		t.Error("run -> pause while blurred: a chain is still wanted")
	}
}

func TestChangingThePolicyWhileFocusedTellsNobody(t *testing.T) {
	for _, to := range []string{config.WhenUnfocusedRun, config.WhenUnfocusedPause} {
		a, v, _ := blinkRouter(t, true)
		_ = a.applyWhenUnfocused(policyConfig(to))
		if n := len(focusMsgsOf(&v.fakeView)); n != 0 {
			t.Errorf("-> %s while focused: view told %d times, want 0", to, n)
		}
	}
}

func TestSaveWhileBlurredThroughConfigSavedMsg(t *testing.T) {
	a := router(t, &fakeView{})
	a.Update(tea.BlurMsg{})

	a.Update(configuration.ConfigSavedMsg{Config: policyConfig(config.WhenUnfocusedRun)})

	// handleConfigSaved rebuilt the views; whatever is on screen now is not
	// suspended, and the router no longer treats the blur as a pause.
	if a.suspended() {
		t.Error("run saved while blurred: the router is still suspended")
	}
	v := &blinkView{wants: true}
	a.views[command.ViewDashboard] = v
	if d, ok := a.wantedCadence(); !ok || d == 0 {
		t.Errorf("run saved while blurred: wantedCadence = (%v, %v), want a cadence", d, ok)
	}
}
