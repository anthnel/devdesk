package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-151 step 3: what only the router can show about the agents activity
// column — a running agent in a live session makes the router frame the view
// for the blink, `app.motion: static` keeps the dot steady, and the 2 s
// incremental read never starts an animation chain of its own.

// openLiveAgentSession writes a transcript whose last line is a second old
// (an active session, an orchestrator that is running), opens it at the
// agents level, and delivers the answer of the read. It returns the router
// and the Cmd the answer produced (it holds the view's 2 s poll Cmd).
func openLiveAgentSession(t *testing.T) (*App, tea.Cmd) {
	t.Helper()
	a := commanding(t, &fakeView{})
	home := os.Getenv("HOME")
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "projects", "-srv-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lines := `{"type":"user","cwd":"` + filepath.Join(home, "app") + `","timestamp":"` + now + `"}` + "\n" +
		fmt.Sprintf(`{"type":"assistant","timestamp":%q,"uuid":"u-1","message":{"id":"m1","model":"model-a","role":"assistant","stop_reason":"end_turn","content":[],"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`, now) + "\n"
	id := "0a1b2c3d-0000-4000-8000-000000000001"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	a.Update(testutil.Resize(180, 40))
	for _, msg := range testutil.Msgs(a.switchView(command.ViewAgentUsage)) {
		a.Update(msg)
	}
	a.commandMode = false // commanding() starts with the palette open

	// Opening the session returns the read and the spinner; run them, then
	// deliver the answers one by one. The answer's own Cmd (the 2 s poll
	// wait) is kept, not run.
	var last tea.Cmd
	for _, msg := range testutil.Msgs(feedKey(t, a, testutil.Key("right"))) {
		if _, cmd := a.Update(msg); cmd != nil {
			last = cmd
		}
	}
	if !strings.Contains(ansi.Strip(a.views[command.ViewAgentUsage].View()), "orchestrator") {
		t.Fatalf("fixture error: the agents level is not on screen:\n%s", ansi.Strip(a.views[command.ViewAgentUsage].View()))
	}
	return a, last
}

func orchestratorRow(t *testing.T, a *App) string {
	t.Helper()
	return rowOf(t, ansi.Strip(a.views[command.ViewAgentUsage].View()), "orchestrator")
}

func TestAgentUsageRunningAgentBlinksItsDot(t *testing.T) {
	a, _ := openLiveAgentSession(t)
	held, ok := a.views[command.ViewAgentUsage].(BlinkingView)
	if !ok {
		t.Fatalf("the router holds a %T, which does not implement BlinkingView", a.views[command.ViewAgentUsage])
	}
	if !held.WantsBlink() {
		t.Fatal("an agent is running in an active session but the view does not ask for the blink")
	}

	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	m := a.views[command.ViewAgentUsage]
	m, _ = m.Update(jobs.FrameMsg{Blink: true})
	a.views[command.ViewAgentUsage] = m
	if row := orchestratorRow(t, a); !strings.Contains(row, on) {
		t.Errorf("blink on: the row has no %q: %q", on, row)
	}
	m, _ = m.Update(jobs.FrameMsg{Blink: false})
	a.views[command.ViewAgentUsage] = m
	if row := orchestratorRow(t, a); strings.Contains(row, on) || !strings.Contains(row, off) {
		t.Errorf("blink off: expected only the off phase %q: %q", off, row)
	}
}

func TestAgentUsageRouterStartsTheBlinkChainForARunningAgent(t *testing.T) {
	stubAnimationEvery(t, time.UnixMilli(1000))
	a, _ := openLiveAgentSession(t)
	// The Update calls during the setup already started the chain; start
	// from a router that has none, as after the chain died.
	a.anim.ticking = false
	if cmd := a.ensureTick(); cmd == nil {
		t.Fatal("a running agent on screen got no animation chain")
	}
	if a.anim.interval != animationBlinkInterval {
		t.Errorf("chain interval = %v, want the blink interval %v", a.anim.interval, animationBlinkInterval)
	}
}

func TestAgentUsageStaticMotionKeepsTheDotSteadyAndStartsNoChain(t *testing.T) {
	got := stubAnimationEvery(t, time.UnixMilli(1000))
	a, _ := openLiveAgentSession(t)
	a.setMotionStatic(true)
	a.anim.ticking = false
	*got = nil

	if cmd := a.ensureTick(); cmd != nil {
		t.Error("app.motion: static: ensureTick must return nil for a blink-only need")
	}
	if len(*got) != 0 {
		t.Errorf("static motion asked for %d periodic messages, want 0", len(*got))
	}
	// The router hands a pinned, on phase to the view.
	m, _ := a.views[command.ViewAgentUsage].Update(jobs.FrameMsg{Blink: a.frameMsg().Blink})
	a.views[command.ViewAgentUsage] = m
	on := indicator.Glyph(indicator.Running, true)
	if row := orchestratorRow(t, a); !strings.Contains(row, on) {
		t.Errorf("static motion: the dot must stay %q: %q", on, row)
	}
}

// The view's 2 s read has its own chain (a sleeping Cmd). Running the poll
// wait, the tick it yields and the read it starts must leave the router's
// animation chain untouched: same seq, no new request for a periodic message.
func TestAgentUsagePollingAddsNoAnimationTick(t *testing.T) {
	got := stubAnimationEvery(t, time.UnixMilli(1000))
	a, answerCmd := openLiveAgentSession(t)
	if !a.anim.ticking {
		t.Fatal("fixture error: the blink chain should be alive for a running agent")
	}
	seq, requests := a.anim.seq, len(*got)

	if answerCmd == nil {
		t.Fatal("fixture error: the answer returned no Cmd (the poll wait)")
	}
	// Msgs sleeps the 2 s poll interval.
	for _, msg := range testutil.Msgs(answerCmd) {
		if _, ok := msg.(animationTickMsg); ok {
			continue // the stub yields it at once; it is the chain, not the poll
		}
		_, cmd := a.Update(msg)
		for _, next := range testutil.Msgs(cmd) {
			if _, ok := next.(animationTickMsg); ok {
				continue
			}
			a.Update(next)
		}
	}
	if a.anim.seq != seq {
		t.Errorf("the poll replaced the animation chain: seq %d -> %d", seq, a.anim.seq)
	}
	if len(*got) != requests {
		t.Errorf("the incremental read asked for %d new animation ticks, want 0", len(*got)-requests)
	}
}
