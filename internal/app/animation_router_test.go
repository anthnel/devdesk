package app

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// blinkView is a fakeView that implements BlinkingView. wants is the level the
// router reads after every Update; onKey may flip it from inside a key press.
type blinkView struct {
	fakeView
	wants    bool
	onKeyWow func(*blinkView)
	// cmd, when set, is returned by every Update (a view's own command).
	cmd func(tea.Msg) tea.Cmd
}

func (v *blinkView) WantsBlink() bool { return v.wants }

func (v *blinkView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok && v.onKeyWow != nil {
		v.onKeyWow(v)
	}
	v.received = append(v.received, msg)
	if v.cmd != nil {
		return v, v.cmd(msg)
	}
	return v, nil
}

// blinkRouter builds a router whose view on screen wants a blink, with the
// clock stubbed at `at`. It returns the view and the recorded intervals.
func blinkRouter(t *testing.T, wants bool) (*App, *blinkView, *[]time.Duration) {
	t.Helper()
	got := stubAnimationEvery(t, time.UnixMilli(1000))
	v := &blinkView{wants: wants}
	a := router(t, v)
	a.anim.blinkOn = true
	return a, v, got
}

func startBlinkChain(t *testing.T, a *App) {
	t.Helper()
	if a.ensureTick() == nil {
		t.Fatal("a view that wants to blink got no animation chain")
	}
}

func tickAt(a *App, ms int64) tea.Cmd {
	_, cmd := a.Update(animationTickMsg{seq: a.anim.seq, at: time.UnixMilli(ms)})
	return cmd
}

func lastFrame(t *testing.T, v *blinkView) jobs.FrameMsg {
	t.Helper()
	frames := framesSeen(&v.fakeView)
	if len(frames) == 0 {
		t.Fatal("the view was never handed a FrameMsg")
	}
	return frames[len(frames)-1]
}

// Criterion 1: every row reads the phase of the tick.
func TestEveryRowReadsThePhaseOfTheTick(t *testing.T) {
	a, v, _ := blinkRouter(t, true)
	other := &blinkView{wants: true}
	a.views[command.ViewWorkspaces] = other
	startBlinkChain(t, a)

	for _, tt := range []struct {
		ms   int64
		want bool
	}{{1000, true}, {1500, false}, {2000, true}} {
		tickAt(a, tt.ms)
		if got := lastFrame(t, v).Blink; got != tt.want {
			t.Errorf("tick at %d ms: Blink = %v, want %v", tt.ms, got, tt.want)
		}
	}
	tickAt(a, 2500) // off
	a.switchView(command.ViewWorkspaces)
	if got := lastFrame(t, other).Blink; got != a.anim.blinkOn || got {
		t.Errorf("entering view Blink = %v, want the router's phase (off)", got)
	}
}

// Criterion 2: the chain dies when nothing animates any more.
func TestTheChainDiesWhenNothingAnimates(t *testing.T) {
	a, v, got := blinkRouter(t, true)
	startBlinkChain(t, a)

	v.wants = false
	cmd := tickAt(a, 1000)

	if _, ok := testutil.MsgOf[animationTickMsg](cmd); ok {
		t.Error("the chain renewed itself with nothing to animate")
	}
	if a.anim.ticking {
		t.Error("the router still believes a chain is alive")
	}
	*got = nil
	for i := 0; i < 10; i++ {
		a.Update(testutil.Key("down"))
	}
	if len(*got) != 0 {
		t.Errorf("idle key presses asked for %d periodic messages, want 0", len(*got))
	}
}

// Criterion 2: a row is never left on the "off" glyph.
func TestAStoppedChainLeavesThePhaseOn(t *testing.T) {
	a, v, _ := blinkRouter(t, true)
	startBlinkChain(t, a)
	tickAt(a, 1500)
	if lastFrame(t, v).Blink {
		t.Fatal("setup: the tick at 1500 ms should be an off phase")
	}

	v.wants = false
	cmd := tickAt(a, 2500) // would be off again

	if !lastFrame(t, v).Blink || !a.anim.blinkOn {
		t.Error("the chain died on an off phase; want a last FrameMsg{Blink: true}")
	}
	if _, ok := testutil.MsgOf[animationTickMsg](cmd); ok {
		t.Error("the chain renewed itself after the view stopped blinking")
	}
}

// Criterion 3: work started after the chain died restarts it exactly once.
func TestAnOperationStartedAfterTheChainDiedRestartsItOnce(t *testing.T) {
	got := stubAnimationEvery(t, time.UnixMilli(1000))
	a := router(t, &blinkView{})

	a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
	if len(*got) != 1 || (*got)[0] != animationBlinkInterval {
		t.Fatalf("intervals after a start = %v, want one of %v", *got, animationBlinkInterval)
	}
	a.Update(testutil.Key("down"))
	if len(*got) != 1 {
		t.Errorf("a later Update asked for another chain: %v", *got)
	}
}

// Criterion 7: the cadence follows the registry.
func TestTheCadenceFollowsTheRegistry(t *testing.T) {
	a, _, got := blinkRouter(t, true)
	startBlinkChain(t, a)
	if (*got)[len(*got)-1] != animationBlinkInterval {
		t.Fatalf("blink only: intervals %v, want %v", *got, animationBlinkInterval)
	}

	id := a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()
	if (*got)[len(*got)-1] != animationBlinkInterval {
		t.Fatalf("run started: intervals %v, want %v", *got, animationBlinkInterval)
	}

	seq := a.anim.seq
	a.jobs.Advance(id, "a", jobs.ItemDone, "")
	cmd := tickAt(a, 1000)
	if m, ok := testutil.MsgOf[animationTickMsg](cmd); !ok || m.seq != seq {
		t.Errorf("run done, view still blinking: want the same chain renewed, got %v %v", m, ok)
	}
	if last := (*got)[len(*got)-1]; last != animationBlinkInterval {
		t.Errorf("renewal interval = %v, want %v", last, animationBlinkInterval)
	}
}

// Criterion 5: static motion pins the phase on and starts no chain for pastilles.
func TestStaticMotionNeverStartsAChainForIndicators(t *testing.T) {
	a, _, got := blinkRouter(t, true)
	a.anim.blinkOn = false

	a.setMotionStatic(true)

	if !a.anim.blinkOn {
		t.Error("setMotionStatic(true) did not pin the phase on")
	}
	if a.ensureTick() != nil {
		t.Error("static motion started a chain for a blink")
	}
	a.Update(testutil.Key("down"))
	if len(*got) != 0 {
		t.Errorf("static motion recorded intervals %v", *got)
	}
}

// Criterion 5: a running job still ticks at the blink interval under static motion, with the phase pinned on.
func TestStaticMotionPinsThePhaseOn(t *testing.T) {
	a, v, got := blinkRouter(t, true)
	a.setMotionStatic(true)
	a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
	if len(*got) == 0 || (*got)[len(*got)-1] != animationBlinkInterval {
		t.Fatalf("static motion with a run: intervals %v, want %v", *got, animationBlinkInterval)
	}
	for _, ms := range []int64{1500, 2500} {
		tickAt(a, ms)
		if !lastFrame(t, v).Blink {
			t.Errorf("tick at %d ms: Blink = false under static motion", ms)
		}
	}
}

// Criterion 6: the router asks the view after the very Update that changed it.
func TestTheRouterAsksTheViewOnScreenAfterEveryUpdate(t *testing.T) {
	a, v, _ := blinkRouter(t, false)
	v.onKeyWow = func(b *blinkView) { b.wants = true }

	_, cmd := a.Update(testutil.Key("down"))

	m, ok := testutil.MsgOf[animationTickMsg](cmd)
	if !ok {
		t.Fatal("the Update that made the view want a blink did not return the chain")
	}
	if m.seq != a.anim.seq || !a.anim.ticking {
		t.Error("the chain is not the router's live one")
	}
	for _, msg := range v.received {
		if _, ok := msg.(animationTickMsg); ok {
			t.Error("the view was asked to send the tick itself")
		}
	}
}

// Criterion 6: only the view on screen is asked.
func TestAViewOffScreenDoesNotKeepTheChainAlive(t *testing.T) {
	got := stubAnimationEvery(t, time.UnixMilli(1000))
	on := &blinkView{wants: true}
	off := &blinkView{wants: true}
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = off
	a.anim.blinkOn = true

	if a.ensureTick() != nil {
		t.Error("a view off screen started a chain")
	}
	a.Update(testutil.Key("down"))
	if len(*got) != 0 {
		t.Errorf("a view off screen recorded intervals %v", *got)
	}

	// A chain kept by the view on screen dies when the user leaves it.
	a.views[command.ViewDashboard] = on
	startBlinkChain(t, a)
	a.currentView = command.ViewDashboard
	a.views[command.ViewDashboard] = &fakeView{}
	cmd := tickAt(a, 1000)
	if _, ok := testutil.MsgOf[animationTickMsg](cmd); ok || a.anim.ticking {
		t.Error("the chain outlived the view that wanted it")
	}
}

// spinnerView mimics a view with a footer spinner: it owns a bubbles spinner
// chain and returns the next Tick for each of its own TickMsg.
type spinnerView struct {
	blinkView
	sp spinner.Model
}

func (v *spinnerView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		v.sp, cmd = v.sp.Update(msg)
		return v, cmd
	case tea.KeyMsg:
		if v.onKeyWow != nil {
			v.onKeyWow(&v.blinkView)
		}
		return v, v.sp.Tick
	}
	return v, nil
}

// Risk: the wrapper must not eat or replace the view's own command, with or
// without a chain started in the same Update, and the spinner must go on.
func TestTheWrapperKeepsTheViewsOwnCommand(t *testing.T) {
	for _, tc := range []struct {
		name      string
		startsRun bool
	}{{"no chain started", false}, {"chain started in the same Update", true}} {
		t.Run(tc.name, func(t *testing.T) {
			stubAnimationEvery(t, time.UnixMilli(1000))
			v := &spinnerView{sp: spinner.New(spinner.WithSpinner(spinner.Dot))}
			if tc.startsRun {
				v.onKeyWow = func(b *blinkView) { b.wants = true }
			}
			a := router(t, v)

			_, cmd := a.Update(testutil.Key("down"))
			tick, ok := testutil.MsgOf[spinner.TickMsg](cmd)
			if !ok {
				t.Fatal("the router lost the view's spinner command")
			}
			if tc.startsRun {
				if _, ok := testutil.MsgOf[animationTickMsg](cmd); !ok {
					t.Fatal("the chain was not started alongside the spinner")
				}
			}
			// The spinner's own chain goes on through the router.
			_, next := a.Update(tick)
			if _, ok := testutil.MsgOf[spinner.TickMsg](next); !ok {
				t.Error("the footer spinner stopped after going through the router")
			}
		})
	}
}

// Risk: a dropped tick adds nothing, even with a chain alive.
func TestAStaleTickIsStillAnsweredWithNothing(t *testing.T) {
	a, _, _ := blinkRouter(t, true)
	startBlinkChain(t, a)

	_, cmd := a.Update(animationTickMsg{seq: a.anim.seq - 1, at: time.UnixMilli(1000)})

	if cmd != nil {
		t.Error("the wrapper added a command to a dropped tick")
	}
}

// -race: Cmds run on their own goroutines. The chain and the view's Cmd are
// built by Update and executed concurrently; neither may read the router.
func TestTheCommandsOfAnUpdateCanRunConcurrently(t *testing.T) {
	a, v, _ := blinkRouter(t, true)
	v.cmd = func(tea.Msg) tea.Cmd { return func() tea.Msg { return spinner.TickMsg{} } }
	// No chain is alive yet, so this one Update starts it AND returns the view's own
	// Cmd: the two must come out as a batch.
	_, cmd := a.Update(testutil.Key("down"))
	if cmd == nil {
		t.Fatal("no command to run")
	}
	msgs := make(chan tea.Msg, 8)
	done := make(chan struct{})
	failures := make(chan string, 1)
	go func() {
		defer close(done)
		batch, ok := cmd().(tea.BatchMsg)
		if !ok {
			failures <- "the Update's command is not a BatchMsg: the chain and the view's Cmd are not concurrent"
			return
		}
		finished := make(chan struct{}, len(batch))
		for _, c := range batch {
			go func() { defer func() { finished <- struct{}{} }(); msgs <- c() }()
		}
		for range batch {
			<-finished
		}
	}()
	// Meanwhile the router goes on mutating its own state in Update.
	for i := 0; i < 20; i++ {
		a.Update(animationTickMsg{seq: a.anim.seq, at: time.UnixMilli(int64(1000 + 500*i))})
	}
	<-done
	select {
	case msg := <-failures:
		t.Fatal(msg)
	default:
	}
}

// Review: leaving static motion on while a chain left the view on the off phase
// must hand the view on screen an "on" frame, since no tick will.
func TestStaticMotionHandsTheViewOnScreenAnOnFrame(t *testing.T) {
	a, v, _ := blinkRouter(t, true)
	startBlinkChain(t, a)
	tickAt(a, 1500) // 1500/500 is odd: the off half
	if lastFrame(t, v).Blink {
		t.Fatal("setup: the tick at 1500 ms should be the off phase")
	}

	a.setMotionStatic(true)

	if !lastFrame(t, v).Blink {
		t.Error("setMotionStatic(true) left the view on screen on Blink=false")
	}
}

// Review: rescanInOrigin shows a view, so it hands that view the current frame.
func TestRescanInOriginHandsTheViewItShowsAFrame(t *testing.T) {
	a, _, _ := blinkRouter(t, false)
	origin := &blinkView{}
	a.views[command.ViewOCIResources] = origin

	a.rescanInOrigin(command.ViewOCIResources, struct{}{})

	if len(framesSeen(&origin.fakeView)) == 0 {
		t.Error("rescanInOrigin showed a view without handing it a FrameMsg")
	}
}
