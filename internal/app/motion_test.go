package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/configuration"
)

// ── DEVDESK-141: app.motion reaches the router ──────────────────────────────
//
// handleConfigSaved and the context switch rebuild the views, so every test
// installs its blinkView AFTER the message: the view that existed before it is
// gone, and the one that wants a blink is what the next ensureTick reads.

func motionConfig(motion string) *config.Config {
	cfg := testConfig()
	cfg.App.Motion = motion
	return cfg
}

// applyBy names the two ways a router takes a new config.
var applyBy = map[string]func(a *App, cfg *config.Config){
	"ConfigSavedMsg": func(a *App, cfg *config.Config) {
		a.Update(configuration.ConfigSavedMsg{Config: cfg})
	},
	"ContextSwitchCompleteMsg": func(a *App, cfg *config.Config) {
		a.Update(ContextSwitchCompleteMsg{ContextName: "work", Config: cfg, Secrets: a.sharedState.Secrets})
	},
}

// blinkingOnScreen installs a view that wants a blink as the one on screen.
func blinkingOnScreen(a *App) *blinkView {
	v := &blinkView{wants: true}
	a.currentView = command.ViewDashboard
	a.views[command.ViewDashboard] = v
	return v
}

func TestMotionSettingReachesTheRouter(t *testing.T) {
	for name, apply := range applyBy {
		t.Run(name, func(t *testing.T) {
			a := router(t, &fakeView{})
			if a.motionStatic {
				t.Fatal("a router starts out static")
			}

			apply(a, motionConfig(config.MotionStatic))
			if !a.motionStatic {
				t.Error("app.motion: static did not reach the router")
			}

			apply(a, motionConfig(config.MotionBlink))
			if a.motionStatic {
				t.Error("app.motion: blink did not bring the blinking back")
			}

			apply(a, motionConfig(config.MotionStatic))
			apply(a, motionConfig(""))
			if a.motionStatic {
				t.Error("an unset app.motion must mean blink")
			}

			apply(a, motionConfig(config.MotionStatic))
			apply(a, motionConfig("wobble"))
			if a.motionStatic {
				t.Error("an unknown app.motion must keep blinking")
			}
		})
	}
}

func TestStaticMotionHoldsTheBlinkPhaseOnAndStartsNoChain(t *testing.T) {
	for name, apply := range applyBy {
		t.Run(name, func(t *testing.T) {
			got := stubAnimationEvery(t, time.UnixMilli(1000))
			a := router(t, &fakeView{})
			a.anim.blinkOn = false

			apply(a, motionConfig(config.MotionStatic))
			blinkingOnScreen(a)

			if cmd := a.ensureTick(); cmd != nil {
				t.Error("static motion started a chain for a blink-only view")
			}
			if len(*got) != 0 {
				t.Errorf("static motion registered intervals %v, want none", *got)
			}
			if !a.anim.blinkOn {
				t.Error("static motion left the phase off")
			}
		})
	}
}

func TestStaticMotionHandsTheViewOnScreenThePinnedFrame(t *testing.T) {
	got := stubAnimationEvery(t, time.UnixMilli(1000))
	v := &blinkView{wants: true}
	a := router(t, v)
	a.anim.blinkOn = false

	// handleConfigSaved rebuilds the views, so the frame goes to whatever
	// createView put on screen; use the router's own entry point instead.
	// The Cmd is the view's own answer to the frame (nil for this fake), so only
	// the frame it received is checked.
	_ = a.applyMotion(motionConfig(config.MotionStatic))
	if len(*got) != 0 {
		t.Errorf("applying static registered intervals %v, want none", *got)
	}
	frames := framesSeen(&v.fakeView)
	if len(frames) == 0 || !frames[len(frames)-1].Blink {
		t.Error("the view on screen was not handed a pinned-on frame")
	}
}

func TestApplyMotionBlinkReturnsNoCmd(t *testing.T) {
	a := router(t, &fakeView{})
	a.motionStatic = true
	if cmd := a.applyMotion(motionConfig(config.MotionBlink)); cmd != nil {
		t.Error("applyMotion(blink) returned a Cmd; the Update wrapper's ensureTick restarts the chain")
	}
	if a.motionStatic {
		t.Error("applyMotion(blink) left the router static")
	}
}

func TestStartingWithStaticMotionPinsTheRouter(t *testing.T) {
	isolateHome(t)
	got := stubAnimationEvery(t, time.UnixMilli(1000))

	a := newWithSize(motionConfig(config.MotionStatic), 120, 40)

	if !a.motionStatic {
		t.Fatal("a router built from a static config does not start static")
	}
	blinkingOnScreen(a)
	if cmd := a.ensureTick(); cmd != nil {
		t.Error("a static start still began a blink chain")
	}
	if len(*got) != 0 {
		t.Errorf("a static start registered intervals %v, want none", *got)
	}
}

func TestStartingWithBlinkMotionLeavesTheRouterBlinking(t *testing.T) {
	isolateHome(t)
	a := newWithSize(motionConfig(""), 120, 40)
	if a.motionStatic {
		t.Error("a router built from a default config starts static")
	}
}

func TestBackToBlinkRestartsTheHalfSecondChain(t *testing.T) {
	for name, apply := range applyBy {
		t.Run(name, func(t *testing.T) {
			got := stubAnimationEvery(t, time.UnixMilli(1000))
			a := router(t, &fakeView{})

			apply(a, motionConfig(config.MotionStatic))
			blinkingOnScreen(a)
			if a.ensureTick() != nil {
				t.Fatal("static motion started a chain")
			}

			apply(a, motionConfig(config.MotionBlink))
			blinkingOnScreen(a)
			if a.ensureTick() == nil {
				t.Fatal("back to blink did not restart the chain")
			}
			if len(*got) != 1 || (*got)[0] != blinkHalfPeriod {
				t.Errorf("intervals = %v, want one of %v", *got, blinkHalfPeriod)
			}
			if !a.anim.ticking {
				t.Error("the router does not know its restarted chain is alive")
			}
		})
	}
}

func TestALiveBlinkChainIsNotRenewedAfterStatic(t *testing.T) {
	for name, apply := range applyBy {
		t.Run(name, func(t *testing.T) {
			got := stubAnimationEvery(t, time.UnixMilli(1500))
			a := router(t, &fakeView{})
			blinkingOnScreen(a)
			a.anim.blinkOn = true
			startBlinkChain(t, a)
			seq := a.anim.seq
			before := len(*got)

			apply(a, motionConfig(config.MotionStatic))
			blinkingOnScreen(a) // the views were rebuilt

			var cmd tea.Cmd
			_, cmd = a.Update(animationTickMsg{seq: seq, at: time.UnixMilli(1500)})

			if len(*got) != before {
				t.Errorf("a blink chain was renewed after static: intervals %v", (*got)[before:])
			}
			if a.anim.ticking {
				t.Error("the router still believes a chain is alive")
			}
			if cmd != nil {
				t.Error("the trailing tick produced a Cmd although nothing needs one")
			}
			if a.anim.seq != seq {
				t.Error("static motion started a new chain")
			}
		})
	}
}
