package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// framesSeen returns every FrameMsg a view was handed, oldest first.
func framesSeen(v *fakeView) []jobs.FrameMsg {
	var out []jobs.FrameMsg
	for _, msg := range v.received {
		if typed, ok := msg.(jobs.FrameMsg); ok {
			out = append(out, typed)
		}
	}
	return out
}

// stubAnimationEvery records the interval of every chain request and returns a
// Cmd that yields the tick at `at` immediately, without sleeping.
func stubAnimationEvery(t *testing.T, at time.Time) *[]time.Duration {
	t.Helper()
	var got []time.Duration
	orig := animationEvery
	animationEvery = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		got = append(got, d)
		return func() tea.Msg { return fn(at) }
	}
	t.Cleanup(func() { animationEvery = orig })
	return &got
}

func TestOnlyOneAnimationChainIsEverAlive(t *testing.T) {
	a := router(t, &fakeView{})
	a.jobs.Start(scanRun("default", "a"))

	if a.ensureTick() == nil {
		t.Fatal("no chain was started for work that is running")
	}
	if second := a.ensureTick(); second != nil {
		t.Error("a second chain was started while the first was alive")
	}
	if !a.anim.ticking {
		t.Error("the router does not know its chain is alive")
	}
}

func TestTheChainStartsAtTheFastInterval(t *testing.T) {
	got := stubAnimationEvery(t, time.UnixMilli(1000))
	a := router(t, &fakeView{})
	a.jobs.Start(scanRun("default", "a"))

	a.ensureTick()

	if len(*got) != 1 || (*got)[0] != animationFastInterval {
		t.Errorf("chain intervals = %v, want one of %v", *got, animationFastInterval)
	}
}

func TestATickFromAnOlderChainIsDropped(t *testing.T) {
	view := &fakeView{}
	a := router(t, view)
	a.jobs.Start(scanRun("default", "a"))
	a.ensureTick()

	before := a.animationFrame()
	_, cmd := a.Update(animationTickMsg{seq: a.anim.seq - 1, at: time.UnixMilli(1000)})

	if a.animationFrame() != before {
		t.Error("a stale tick advanced the frame")
	}
	if cmd != nil {
		t.Error("a stale tick was answered with a command")
	}
	if n := len(framesSeen(view)); n != 0 {
		t.Errorf("a stale tick delivered %d FrameMsg, want 0", n)
	}
}

// A tick sends the frame and nothing else, and only to the view on screen.
func TestATickSendsAFrameAndNoSnapshot(t *testing.T) {
	onScreen, offScreen := &fakeView{}, &fakeView{}
	a := router(t, onScreen)
	a.views[command.ViewWorkspaces] = offScreen
	a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()

	frames, snaps := len(framesSeen(onScreen)), len(jobsSeen(onScreen))
	offFrames, offSnaps := len(framesSeen(offScreen)), len(jobsSeen(offScreen))

	a.Update(animationTickMsg{seq: a.anim.seq, at: time.UnixMilli(1000)})

	if got := len(framesSeen(onScreen)); got != frames+1 {
		t.Errorf("the view on screen got %d FrameMsg, want %d", got-frames, 1)
	}
	if got := len(jobsSeen(onScreen)); got != snaps {
		t.Errorf("the view on screen got %d new snapshots from a tick, want 0", got-snaps)
	}
	if got := len(framesSeen(offScreen)); got != offFrames {
		t.Error("a view off screen got a FrameMsg")
	}
	if got := len(jobsSeen(offScreen)); got != offSnaps {
		t.Error("a view off screen got a snapshot from a tick")
	}
}

func TestATickAdvancesTheFrameAndRenewsTheChain(t *testing.T) {
	testutil.FastTimers(t, &animationFastInterval)
	view := &fakeView{}
	a := router(t, view)
	a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()

	before := a.animationFrame()
	_, cmd := a.Update(animationTickMsg{seq: a.anim.seq, at: time.UnixMilli(1000)})

	if a.animationFrame() == before {
		t.Error("the frame did not advance")
	}
	if _, ok := testutil.MsgOf[animationTickMsg](cmd); !ok {
		t.Error("the chain was not renewed while work was running")
	}
}

func TestTheChainDiesWhenNothingRunsAndRestartsAfterwards(t *testing.T) {
	testutil.FastTimers(t, &animationFastInterval)
	view := &fakeView{}
	a := router(t, view)
	id := a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()

	a.jobs.Advance(id, "a", jobs.ItemDone, "")
	_, cmd := a.Update(animationTickMsg{seq: a.anim.seq, at: time.UnixMilli(1000)})

	if _, ok := testutil.MsgOf[animationTickMsg](cmd); ok {
		t.Error("the chain renewed itself with nothing left to animate")
	}
	if a.anim.ticking {
		t.Error("the router still believes a chain is alive")
	}
	a.jobs.Start(scanRun("default", "b"))
	if a.ensureTick() == nil {
		t.Error("a run started after the chain died got no chain")
	}
}

// Rule 122: the frame is bare, its rendering is not.
func TestTheFrameIsBareAndItsRenderingIsNot(t *testing.T) {
	testutil.TrueColor(t)
	view := &fakeView{}
	a := router(t, view)
	a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()

	frames := framesSeen(view)
	if len(frames) == 0 {
		t.Fatal("the view on screen was never handed a FrameMsg")
	}
	f := frames[len(frames)-1]
	if f.Frame == "" {
		t.Fatal("no spinner frame was carried")
	}
	if strings.Contains(f.Frame, "\x1b") {
		t.Errorf("Frame = %q carries an escape sequence; Rule 122", f.Frame)
	}
	if !strings.Contains(f.RenderedFrame, "\x1b") || !strings.Contains(f.RenderedFrame, f.Frame) {
		t.Errorf("RenderedFrame = %q, want the styled Frame %q (Rule 128)", f.RenderedFrame, f.Frame)
	}
}

// The blink phase is read off the message's own time, not the wall clock.
func TestTheFrameCarriesThePhaseOfTheTickTime(t *testing.T) {
	testutil.FastTimers(t, &animationFastInterval)
	view := &fakeView{}
	a := router(t, view)
	a.jobs.Start(scanRun("default", "a"))
	a.jobsChanged()

	for _, tt := range []struct {
		ms   int64
		want bool
	}{{1000, true}, {1500, false}, {2000, true}} {
		a.Update(animationTickMsg{seq: a.anim.seq, at: time.UnixMilli(tt.ms)})
		frames := framesSeen(view)
		if len(frames) == 0 {
			t.Fatalf("tick at %d ms delivered no FrameMsg", tt.ms)
		}
		if got := frames[len(frames)-1].Blink; got != tt.want {
			t.Errorf("tick at %d ms: Blink = %v, want %v", tt.ms, got, tt.want)
		}
	}
}

// A view entering the screen gets the snapshot, then the frame.
func TestAViewEnteringTheScreenGetsTheSnapshotAndTheFrame(t *testing.T) {
	ws := &fakeView{}
	a := router(t, &fakeView{})
	a.views[command.ViewWorkspaces] = ws
	a.jobs.Start(scanRun("default", "a"))
	a.anim.blinkOn = true

	a.switchView(command.ViewWorkspaces)

	changed, frame := -1, -1
	for i, msg := range ws.received {
		switch msg.(type) {
		case jobs.ChangedMsg:
			changed = i
		case jobs.FrameMsg:
			frame = i
		}
	}
	if changed < 0 || frame < 0 {
		t.Fatalf("entering view got ChangedMsg at %d and FrameMsg at %d, want both", changed, frame)
	}
	if changed > frame {
		t.Error("the FrameMsg came before the ChangedMsg")
	}
	if got := framesSeen(ws)[0]; got.Blink != a.anim.blinkOn {
		t.Errorf("entering FrameMsg.Blink = %v, want the router's %v", got.Blink, a.anim.blinkOn)
	}
}

// A registry change reaches every held view; the frame only the one on screen.
func TestARegistryChangeStillReachesEveryView(t *testing.T) {
	onScreen, other := &fakeView{}, &fakeView{}
	a := router(t, onScreen)
	a.views[command.ViewWorkspaces] = other

	a.Update(jobs.StartMsg{Run: scanRun("default", "a")})

	for name, v := range map[string]*fakeView{"on screen": onScreen, "other": other} {
		if len(jobsSeen(v)) == 0 {
			t.Errorf("%s was not handed a ChangedMsg", name)
		}
	}
	if len(framesSeen(onScreen)) == 0 {
		t.Error("the view on screen was not handed a FrameMsg")
	}
	if len(framesSeen(other)) != 0 {
		t.Error("a view off screen was handed a FrameMsg")
	}
}
