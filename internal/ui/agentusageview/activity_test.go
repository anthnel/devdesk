package agentusageview

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-151 step 3: the router's frame reaches the agents table, and the
// view says when it needs the blink (WantsBlink, a level, computed from the
// rows on screen). The pure State/dot parts are covered elsewhere.

// The two hooks are looked up through interfaces so that, before they exist,
// each test fails on its own message instead of the whole package refusing to
// compile.
type blinkingView interface{ WantsBlink() bool }

type activityTablesView interface {
	ActivityTables() map[string]datatable.ActivityTable
}

func wantsBlink(t *testing.T, m Model) bool {
	t.Helper()
	v, ok := any(m).(blinkingView)
	if !ok {
		t.Fatal("agentusageview.Model has no WantsBlink() bool (the router's BlinkingView hook)")
	}
	return v.WantsBlink()
}

// liveAt opens a session at the agents level with the given agents, read now
// from a live session, so unfinished sub-agents and the orchestrator are
// running when they have a recent line.
func liveAt(t *testing.T, agents ...agentusage.Agent) Model {
	t.Helper()
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	m.clock = statusClock{now: time.Now(), alive: true}
	return answer(t, m, agentusage.Usage{Agents: agents})
}

func recent(a agentusage.Agent, st agentusage.State, ago time.Duration) agentusage.Agent {
	a.State = st
	a.FirstActivity = time.Now().Add(-ago - time.Minute)
	a.LastActivity = time.Now().Add(-ago)
	return a
}

// runningTeam has a running coder (unfinished, recent) next to a done tester.
func runningTeam(t *testing.T) Model {
	t.Helper()
	return liveAt(t,
		recent(orchestrator(10, 0), agentusage.StateUnknown, 10*time.Minute), // idle: nobody wrote for 10 min
		recent(subAgent("a01", "coder", 5, 0), agentusage.StateUnfinished, time.Second),
		recent(subAgent("a02", "tester", 5, 0), agentusage.StateFinished, time.Hour),
	)
}

func TestActivityTablesNamesTheAgentsTable(t *testing.T) {
	m := runningTeam(t)
	v, ok := any(m).(activityTablesView)
	if !ok {
		t.Fatal("agentusageview.Model has no ActivityTables() map[string]datatable.ActivityTable")
	}
	got := v.ActivityTables()
	if len(got) != 1 {
		t.Fatalf("ActivityTables has %d entries, want exactly agent-usage/agents: %v", len(got), got)
	}
	tbl, ok := got["agent-usage/agents"]
	if !ok {
		t.Fatalf("no agent-usage/agents entry: %v", got)
	}
	if tbl.RenderedWidth() != m.agentTable.RenderedWidth() || len(tbl.Shapes()) != len(m.agentTable.Shapes()) {
		t.Error("agent-usage/agents is not the agents table")
	}
}

func TestWantsBlinkOnlyForARunningVisibleRow(t *testing.T) {
	idleOrch := recent(orchestrator(10, 0), agentusage.StateUnknown, 10*time.Minute)
	done := recent(subAgent("a02", "tester", 5, 0), agentusage.StateFinished, time.Hour)
	running := recent(subAgent("a01", "coder", 5, 0), agentusage.StateUnfinished, time.Second)
	idleSub := recent(subAgent("a03", "reviewer", 5, 0), agentusage.StateUnfinished, 10*time.Minute)

	tests := []struct {
		name   string
		agents []agentusage.Agent
		want   bool
	}{
		{"a running agent", []agentusage.Agent{idleOrch, running, done}, true},
		{"only idle and done", []agentusage.Agent{idleOrch, idleSub, done}, false},
		{"only done", []agentusage.Agent{done}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := liveAt(t, tt.agents...)
			if got := wantsBlink(t, m); got != tt.want {
				states := []string{}
				for _, r := range m.agentTable.Visible() {
					states = append(states, r.state.label())
				}
				t.Errorf("WantsBlink = %v, want %v (states %v)", got, tt.want, states)
			}
		})
	}
}

func TestWantsBlinkIsFalseWithoutAnAgentsLevelOnTheAgentsTab(t *testing.T) {
	// Sessions level: nothing to blink even when a table holds a running row.
	top := withSessions(t, "/p/a.jsonl")
	if wantsBlink(t, top) {
		t.Error("sessions level: WantsBlink must be false")
	}

	// Breakdown tab: the agents table is not on screen (H4).
	m := runningTeam(t)
	if !wantsBlink(t, m) {
		t.Fatal("fixture error: the Agents tab with a running coder must want the blink")
	}
	m = feed(t, m, testutil.Key("tab"))
	if m.tab != tabBreakdown {
		t.Fatal("fixture error: tab did not reach the Breakdown tab")
	}
	if wantsBlink(t, m) {
		t.Error("Breakdown tab: WantsBlink must be false")
	}
	m = feed(t, m, testutil.Key("tab"))
	if !wantsBlink(t, m) {
		t.Error("back on the Agents tab: WantsBlink must be true again")
	}
}

func TestWantsBlinkIgnoresARunningRowTheFilterHides(t *testing.T) {
	m := runningTeam(t)
	m = send(t, m, testutil.Key("/"))
	for _, k := range testutil.Type("tester") {
		m = feed(t, m, k)
	}
	m = send(t, m, testutil.Key("enter"))
	if n := len(m.agentTable.Visible()); n != 1 {
		t.Fatalf("fixture error: the filter leaves %d rows, want 1", n)
	}
	if wantsBlink(t, m) {
		t.Error("the only running row is filtered out: WantsBlink must be false")
	}
}

// agentLine is the rendered line naming the coder, ANSI stripped.
func agentLine(t *testing.T, m Model) string {
	t.Helper()
	for _, line := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(line, "coder") {
			return line
		}
	}
	t.Fatalf("no coder row in:\n%s", m.View())
	return ""
}

func TestFrameMsgBlinksTheRunningDot(t *testing.T) {
	m := runningTeam(t)
	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	if on == off {
		t.Fatalf("fixture error: running glyph does not blink (%q)", on)
	}

	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if line := agentLine(t, m); !strings.Contains(line, on) || strings.Contains(line, off) {
		t.Errorf("blink on: want %q and not %q in %q", on, off, line)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if line := agentLine(t, m); !strings.Contains(line, off) || strings.Contains(line, on) {
		t.Errorf("blink off: want %q and not %q in %q", off, on, line)
	}
	if m.agentTable.BlinkPhase() {
		t.Error("the agents table did not receive the blink phase")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if !m.agentTable.BlinkPhase() {
		t.Error("blink back on: the table phase did not follow")
	}
}

func TestFrameMsgStartsNoCmd(t *testing.T) {
	m := runningTeam(t)
	if _, cmd := m.Update(jobs.FrameMsg{Blink: false}); cmd != nil {
		t.Error("a frame is rendering state only: the view returns no Cmd for it")
	}
}

// The 2 s incremental read is the view's own chain (pollTickCmd, a sleeping
// Cmd). It must not add an animation tick: the router owns the only chain.
func TestPollingStartsNoAnimationTick(t *testing.T) {
	m, _, cmd := openedFixture(t)
	msg := firstDelta(t, cmd)

	m, msgs := answered(t, m, msg)
	for _, got := range msgs {
		if !isViewOwned(got) {
			t.Errorf("answer produced %T, only the poll tick is expected", got)
		}
	}
	m, msgs = pollTick(t, m, m.agentGen)
	for _, got := range msgs {
		if !isViewOwned(got) {
			t.Errorf("poll tick produced %T, only the read is expected", got)
		}
	}
	_ = m
}

func isViewOwned(msg tea.Msg) bool {
	switch msg.(type) {
	case usagePollTickMsg, usageDeltaMsg, nil:
		return true
	}
	return false
}
