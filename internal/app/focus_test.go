package app

import (
	"io"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── DEVDESK-210 step 1: terminal focus and the animation chain ──────────────

// blurred sends a BlurMsg through the router, the way the terminal would.
func blurred(t *testing.T, a *App) {
	t.Helper()
	a.Update(tea.BlurMsg{})
	if !a.terminalBlurred {
		t.Fatal("setup: a BlurMsg did not blur the router")
	}
}

func TestAppViewReportsFocus(t *testing.T) {
	a := router(t, &fakeView{})
	if !a.View().ReportFocus {
		t.Error("App.View() does not ask the terminal to report focus (mode 1004)")
	}
}

func TestBlurStopsTheBlinkChain(t *testing.T) {
	a, v, got := blinkRouter(t, true)
	startBlinkChain(t, a)
	before := len(*got)

	blurred(t, a)
	cmd := tickAt(a, 2500) // 2500/500 is odd: the off phase

	if _, ok := testutil.MsgOf[animationTickMsg](cmd); ok {
		t.Error("the chain renewed itself while the terminal is blurred")
	}
	if a.anim.ticking {
		t.Error("the router still believes a chain is alive")
	}
	if len(*got) != before {
		t.Errorf("blur registered intervals %v", (*got)[before:])
	}
	if !lastFrame(t, v).Blink {
		t.Error("the chain died on an off phase; want a last FrameMsg{Blink: true}")
	}
}

// A blur that lands during an "on" phase still hands the view one last pinned
// frame: the in-flight tick is the last the view sees while the terminal is
// unfocused, so it must not be swallowed just because the phase is already on.
func TestBlurDuringTheOnPhaseSendsOneLastFrame(t *testing.T) {
	a, v, _ := blinkRouter(t, true)
	startBlinkChain(t, a)
	a.anim.blinkOn = true

	blurred(t, a)
	before := len(framesSeen(&v.fakeView))
	cmd := tickAt(a, 2000) // 2000/500 is even: the on phase

	if _, ok := testutil.MsgOf[animationTickMsg](cmd); ok {
		t.Error("the chain renewed itself while the terminal is blurred")
	}
	if a.anim.ticking {
		t.Error("the router still believes a chain is alive")
	}
	if got := len(framesSeen(&v.fakeView)); got != before+1 {
		t.Fatalf("frames after the blur tick = %d, want %d (one last frame)", got, before+1)
	}
	if !lastFrame(t, v).Blink {
		t.Error("the last frame is not pinned on; want FrameMsg{Blink: true}")
	}
}

func TestNoChainStartsWhileBlurred(t *testing.T) {
	a, _, got := blinkRouter(t, true)
	blurred(t, a)

	_, cmd := a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
	if _, ok := testutil.MsgOf[animationTickMsg](cmd); ok {
		t.Error("a run started while blurred began an animation chain")
	}
	if a.jobs.Running() == 0 {
		t.Fatal("setup: the run is not running")
	}
	a.jobsChanged()
	if a.ensureTick() != nil {
		t.Error("ensureTick started a chain while blurred")
	}
	if len(*got) != 0 {
		t.Errorf("intervals recorded while blurred: %v", *got)
	}
}

// AC3: a blur never cancels, pauses or delays a job.
func TestBlurNeverTouchesRunningJobs(t *testing.T) {
	a, _, _ := blinkRouter(t, false)
	a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
	id := a.jobs.Snapshot()[0].ID

	blurred(t, a)
	a.jobs.Advance(id, "a", jobs.ItemRunning, "")
	a.jobsChanged()
	if got := a.jobs.Running(); got != 1 {
		t.Fatalf("Running() after blur = %d, want 1", got)
	}
	a.jobs.Advance(id, "a", jobs.ItemDone, "")
	a.jobsChanged()

	if got := a.jobs.Running(); got != 0 {
		t.Errorf("Running() after the item finished = %d, want 0", got)
	}
	for _, it := range a.jobs.Snapshot()[0].Items {
		if it.State != jobs.ItemDone {
			t.Errorf("item %q is %q, want done", it.Target, it.State)
		}
	}
}

// AC4: the failure memory survives a blur and is handed to the view on focus.
func TestAFailureDuringBlurIsShownOnFocus(t *testing.T) {
	a, v, _ := blinkRouter(t, false)
	a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
	id := a.jobs.Snapshot()[0].ID

	blurred(t, a)
	a.jobs.Advance(id, "a", jobs.ItemFailed, "boom")
	a.jobsChanged()
	a.Update(tea.FocusMsg{})

	key := jobs.FailureKey{Kind: jobs.KindScan, Target: "a", Context: "default"}
	has := func(fs []jobs.Failure) bool {
		for _, f := range fs {
			if f.FailureKey == key {
				return true
			}
		}
		return false
	}
	if !has(a.jobs.Failures()) {
		t.Error("the registry lost the failure that happened during the blur")
	}
	if !has(lastJobs(t, &v.fakeView).Failures) {
		t.Error("the view on screen was not handed the failure")
	}
}

// AC5: focus restarts exactly one chain.
func TestFocusRestartsExactlyOneChain(t *testing.T) {
	t.Run("blink only", func(t *testing.T) {
		a, _, got := blinkRouter(t, true)
		startBlinkChain(t, a)
		blurred(t, a)
		tickAt(a, 1000) // the in-flight tick lands, the chain dies
		before, seq := len(*got), a.anim.seq

		_, cmd := a.Update(tea.FocusMsg{})

		if _, ok := testutil.MsgOf[animationTickMsg](cmd); !ok {
			t.Fatal("focus did not restart the chain")
		}
		if len(*got) != before+1 || (*got)[before] != animationBlinkInterval {
			t.Errorf("intervals after focus = %v, want one of %v", (*got)[before:], animationBlinkInterval)
		}
		if a.anim.seq != seq+1 {
			t.Errorf("seq = %d, want %d", a.anim.seq, seq+1)
		}
		a.Update(tea.FocusMsg{})
		if len(*got) != before+1 {
			t.Error("a second FocusMsg started another chain")
		}
	})
	t.Run("running job", func(t *testing.T) {
		a, _, got := blinkRouter(t, false)
		a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
		blurred(t, a)
		tickAt(a, 1000)
		before := len(*got)

		a.Update(tea.FocusMsg{})

		if len(*got) != before+1 || (*got)[before] != animationBlinkInterval {
			t.Errorf("intervals after focus = %v, want one of %v", (*got)[before:], animationBlinkInterval)
		}
	})
	t.Run("focus before the in-flight tick lands", func(t *testing.T) {
		a, _, got := blinkRouter(t, true)
		startBlinkChain(t, a)
		before, seq := len(*got), a.anim.seq
		blurred(t, a)

		a.Update(tea.FocusMsg{})

		if len(*got) != before || a.anim.seq != seq || !a.anim.ticking {
			t.Errorf("a quick blur+focus changed the chain: intervals %v, seq %d->%d, ticking %v",
				(*got)[before:], seq, a.anim.seq, a.anim.ticking)
		}
		cmd := tickAt(a, 1000)
		if m, ok := testutil.MsgOf[animationTickMsg](cmd); !ok || m.seq != seq {
			t.Error("the in-flight tick did not renew the same chain")
		}
	})
}

// Degradation: a terminal that never sends BlurMsg behaves as before, and a
// FocusMsg with no blur does nothing.
func TestNeverBlurredBehavesAsToday(t *testing.T) {
	type outcome struct {
		intervals []time.Duration
		blinks    []bool
	}
	run := func(t *testing.T, focusFirst bool) outcome {
		a, v, got := blinkRouter(t, true)
		if focusFirst {
			if _, cmd := a.Update(tea.FocusMsg{}); cmd != nil {
				t.Error("a FocusMsg with no prior blur returned a Cmd")
			}
		}
		startBlinkChain(t, a)
		for _, ms := range []int64{1000, 1500, 2000} {
			tickAt(a, ms)
		}
		a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
		id := a.jobs.Snapshot()[0].ID
		a.jobs.Advance(id, "a", jobs.ItemDone, "")
		a.jobsChanged()
		tickAt(a, 2500)
		var o outcome
		o.intervals = append(o.intervals, *got...)
		for _, f := range framesSeen(&v.fakeView) {
			o.blinks = append(o.blinks, f.Blink)
		}
		return o
	}
	plain, withFocus := run(t, false), run(t, true)
	if !reflect.DeepEqual(plain, withFocus) {
		t.Errorf("a lone FocusMsg changed behaviour:\nplain %+v\nfocus %+v", plain, withFocus)
	}
}

// AC6: app.motion: static never blinks, across a blur and a focus either.
func TestStaticMotionNeverBlinksAcrossFocus(t *testing.T) {
	t.Run("running job", func(t *testing.T) {
		a, v, _ := blinkRouter(t, false)
		a.setMotionStatic(true)
		a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
		blurred(t, a)
		a.Update(tea.FocusMsg{})
		for _, ms := range []int64{1500, 2500, 3500} {
			tickAt(a, ms)
		}
		frames := framesSeen(&v.fakeView)
		if len(frames) == 0 {
			t.Fatal("no frame was handed to the view")
		}
		for i, f := range frames {
			if !f.Blink {
				t.Errorf("frame %d has Blink=false under static motion", i)
			}
		}
	})
	t.Run("blink only", func(t *testing.T) {
		a, _, got := blinkRouter(t, true)
		a.setMotionStatic(true)
		blurred(t, a)
		_, cmd := a.Update(tea.FocusMsg{})
		if _, ok := testutil.MsgOf[animationTickMsg](cmd); ok || len(*got) != 0 || a.ensureTick() != nil {
			t.Errorf("focus started a chain for a pinned blink: intervals %v", *got)
		}
	})
}

// H2: a key or a paste proves the terminal has the focus.
func TestAKeyWhileBlurredCountsAsFocus(t *testing.T) {
	for name, msg := range map[string]tea.Msg{
		"key":   testutil.Key("down"),
		"paste": testutil.Paste("x"),
	} {
		t.Run(name, func(t *testing.T) {
			a, _, _ := blinkRouter(t, true)
			startBlinkChain(t, a)
			blurred(t, a)
			tickAt(a, 1000) // the chain dies

			_, cmd := a.Update(msg)

			if a.terminalBlurred {
				t.Error("the router is still blurred after input")
			}
			if _, ok := testutil.MsgOf[animationTickMsg](cmd); !ok {
				t.Error("input while blurred did not restart the chain")
			}
		})
	}
}

// countingModel counts the animation ticks the router handles.
type countingModel struct {
	programModel
	ticks *atomic.Int64
}

func (m countingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(animationTickMsg); ok {
		m.ticks.Add(1)
	}
	_, cmd := m.programModel.Update(msg)
	return m, cmd
}

// -race: a blur and a focus under a real tea.Program.
func TestTheChainStopsOnBlurAndResumesOnFocusUnderARealProgram(t *testing.T) {
	testutil.FastTimers(t, &animationBlinkInterval)

	a := router(t, &blinkView{wants: true})
	var ticks atomic.Int64
	p := tea.NewProgram(countingModel{programModel{app: a}, &ticks},
		tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())

	var running, blurredN, idle, resumed int64
	go func() {
		p.Send(testutil.Key("down")) // gives the router one Update, which starts the chain
		time.Sleep(60 * time.Millisecond)
		running = ticks.Load()
		p.Send(tea.BlurMsg{})
		time.Sleep(60 * time.Millisecond) // the in-flight tick lands
		blurredN = ticks.Load()
		time.Sleep(60 * time.Millisecond)
		idle = ticks.Load()
		p.Send(tea.FocusMsg{})
		time.Sleep(60 * time.Millisecond)
		resumed = ticks.Load()
		p.Quit()
	}()
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the program did not exit")
	}

	if running == 0 {
		t.Fatal("setup: the chain never ticked")
	}
	if idle != blurredN {
		t.Errorf("%d ticks while blurred, want 0", idle-blurredN)
	}
	if resumed <= idle {
		t.Error("the chain did not resume after the focus")
	}
}
