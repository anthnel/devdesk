package agentusageview

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-46 step 5: the State, Tools and Duration columns of the agents
// table, formatDuration, and the rebuild of the rows on every successful read.

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name  string
		d     time.Duration
		known bool
		want  string
	}{
		{"unknown", 0, false, "-"},
		{"unknown ignores the value", time.Hour, false, "-"},
		{"zero but known", 0, true, "0s"},
		{"under a second", 400 * time.Millisecond, true, "0s"},
		{"one second", time.Second, true, "1s"},
		{"seconds, floored", 59900 * time.Millisecond, true, "59s"},
		{"one minute and a second", 61 * time.Second, true, "1m01s"},
		{"three minutes seven", 3*time.Minute + 7*time.Second, true, "3m07s"},
		{"just under an hour", 3599 * time.Second, true, "59m59s"},
		{"an hour five, seconds dropped", time.Hour + 5*time.Minute + 59*time.Second, true, "1h05m"},
		{"just under a day", 23*time.Hour + 59*time.Minute, true, "23h59m"},
		{"two days three hours", 51 * time.Hour, true, "2d03h"},
		{"99 days", 99*24*time.Hour + 5*time.Hour, true, "99d05h"},
		{"120 days", 120 * 24 * time.Hour, true, ">99d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d, tt.known)
			if got != tt.want {
				t.Errorf("formatDuration(%v, %v) = %q, want %q", tt.d, tt.known, got, tt.want)
			}
			if utf8.RuneCountInString(got) > 6 {
				t.Errorf("%q is wider than 6 cells", got)
			}
		})
	}
}

func TestActivityColumnsAreWideEnough(t *testing.T) {
	for title, min := range map[string]int{"State": 7, "Tools": 5, "Duration": 8} {
		if got := column(t, title).MinWidth; got < min {
			t.Errorf("%s MinWidth = %d, want >= %d", title, got, min)
		}
	}
}

// stamped is an agent whose transcript spans [first, last].
func stamped(a agentusage.Agent, tools int, first, last time.Time) agentusage.Agent {
	a.ToolCalls, a.FirstActivity, a.LastActivity = tools, first, last
	return a
}

func rowWith(a agentusage.Agent, st runState) agentRow {
	return agentRow{Agent: a, state: st}
}

func TestAgentRowsCarryTheirState(t *testing.T) {
	tn := time.Now()
	u := agentusage.Usage{Agents: []agentusage.Agent{
		stamped(orchestrator(1, 0), 1, tn.Add(-time.Hour), tn),
		stamped(withState(subAgent("a01", "coder", 1, 0), agentusage.StateUnfinished), 2, tn.Add(-time.Hour), tn),
		stamped(withState(subAgent("a02", "tester", 1, 0), agentusage.StateFinished), 2, tn.Add(-time.Hour), tn.Add(-30*time.Minute)),
		stamped(withState(subAgent("a03", "reviewer", 1, 0), agentusage.StateUnfinished), 2, tn.Add(-time.Hour), tn.Add(-10*time.Minute)),
	}}
	got := agentRows(u, true, statusClock{now: tn, alive: true})
	want := []runState{stateRunning, stateRunning, stateDone, stateIdle}
	for i, r := range got {
		if r.state != want[i] {
			t.Errorf("row %d (%s): state = %s, want %s", i, roleCell(r), r.state.label(), want[i].label())
		}
	}
}

func withState(a agentusage.Agent, s agentusage.State) agentusage.Agent {
	a.State = s
	return a
}

func TestActivityCellsArePlainText(t *testing.T) {
	tn := time.Now()
	a := stamped(subAgent("a01", "coder", 1, 0), 12, tn.Add(-3*time.Minute-7*time.Second), tn)
	for _, st := range []runState{stateRunning, stateIdle, stateDone, stateUnknown} {
		for _, title := range []string{"State", "Tools", "Duration"} {
			if cell := column(t, title).Cell(rowWith(a, st)); strings.Contains(cell, "\x1b") {
				t.Errorf("%s cell carries an escape sequence: %q (Rule 122)", title, cell)
			}
		}
	}
}

func TestActivityCellValues(t *testing.T) {
	tn := time.Now()
	a := stamped(subAgent("a01", "coder", 1, 0), 12, tn.Add(-3*time.Minute-7*time.Second), tn)
	state, tools, dur := column(t, "State"), column(t, "Tools"), column(t, "Duration")
	for st, want := range map[runState]string{stateRunning: "running", stateIdle: "idle", stateDone: "done", stateUnknown: "unknown"} {
		if got := state.Cell(rowWith(a, st)); got != want {
			t.Errorf("State cell for %s = %q, want %q", want, got, want)
		}
	}
	if got := tools.Cell(rowWith(a, stateRunning)); got != "12" {
		t.Errorf("Tools cell = %q, want 12", got)
	}
	if got := dur.Cell(rowWith(a, stateRunning)); got != "3m07s" {
		t.Errorf("Duration cell = %q, want 3m07s", got)
	}
	none := subAgent("a02", "tester", 1, 0)
	if got := dur.Cell(rowWith(none, stateRunning)); got != "-" {
		t.Errorf("Duration with no timestamp = %q, want -", got)
	}
}

func TestActivityColumnColours(t *testing.T) {
	tn := time.Now()
	a := stamped(subAgent("a01", "coder", 1, 0), 12, tn.Add(-time.Minute), tn)
	state, tools, dur := column(t, "State"), column(t, "Tools"), column(t, "Duration")
	fg := func(s lipgloss.Style) lipgloss.TerminalColor { return s.GetForeground() }
	plain := fg(lipgloss.NewStyle())

	if got, want := fg(state.Style(rowWith(a, stateIdle))), fg(theme.SeverityTextStyle("MEDIUM")); got != want {
		t.Errorf("idle foreground = %v, want the MEDIUM orange %v", got, want)
	}
	for _, st := range []runState{stateRunning, stateDone} {
		if got := fg(state.Style(rowWith(a, st))); got != plain {
			t.Errorf("%s must keep the default colour, got %v", st.label(), got)
		}
	}
	if got, want := fg(state.Style(rowWith(a, stateUnknown))), fg(theme.DimStyle); got != want {
		t.Errorf("unknown foreground = %v, want dim %v", got, want)
	}

	zero := a
	zero.ToolCalls = 0
	if got, want := fg(tools.Style(rowWith(zero, stateRunning))), fg(theme.DimStyle); got != want {
		t.Errorf("Tools 0 foreground = %v, want dim", got)
	}
	if got := fg(tools.Style(rowWith(a, stateRunning))); got != plain {
		t.Errorf("Tools 12 must keep the default colour, got %v", got)
	}
	if got, want := fg(dur.Style(rowWith(subAgent("x", "y", 0, 0), stateRunning))), fg(theme.DimStyle); got != want {
		t.Errorf("Duration - foreground = %v, want dim", got)
	}
	if got := fg(dur.Style(rowWith(a, stateRunning))); got != plain {
		t.Errorf("Duration 1m00s must keep the default colour, got %v", got)
	}
}

func TestSearchMatchesTheStateLabel(t *testing.T) {
	a := subAgent("a01", "coder", 1, 0)
	state := column(t, "State")
	if state.Search == nil {
		t.Fatal("State must be searchable so /idle keeps the idle agents")
	}
	if got := state.Search(rowWith(a, stateIdle)); !strings.Contains(got, "idle") {
		t.Errorf("Search(idle row) = %q", got)
	}
	if got := state.Search(rowWith(a, stateRunning)); strings.Contains(got, "idle") {
		t.Errorf("Search(running row) = %q must not match idle", got)
	}
}

// ── On screen ────────────────────────────────────────────────────────────────

func sizedAgents(t *testing.T, width int, u agentusage.Usage, c statusClock) Model {
	t.Helper()
	m := withSessions(t, "/p/a.jsonl")
	m = feed(t, m, testutil.Resize(width, 20))
	m, _ = opened(t, m, "right")
	m.clock = c
	return answer(t, m, u)
}

func activityUsage(tn time.Time) agentusage.Usage {
	return agentusage.Usage{Agents: []agentusage.Agent{
		stamped(orchestrator(5, 0), 7, tn.Add(-time.Hour), tn),
		stamped(withState(subAgent("a01", "coder", 1, 0), agentusage.StateUnfinished), 12,
			tn.Add(-3*time.Minute-7*time.Second), tn),
		stamped(withState(subAgent("a02", "tester", 1, 0), agentusage.StateFinished), 3,
			tn.Add(-time.Hour), tn.Add(-time.Hour+45*time.Second)),
	}}
}

// Criterion 1 at 80 columns: every line shows a state, a tool count, a duration.
func TestEveryAgentLineShowsStateToolsAndDurationAt80Columns(t *testing.T) {
	tn := time.Now()
	m := sizedAgents(t, 80, activityUsage(tn), statusClock{now: tn, alive: true})
	view := stripANSI(m.View())
	lines := map[string][]string{
		"orchestrator": {"running", "7", "1h00m"},
		"coder":        {"running", "12", "3m07s"},
		"tester":       {"done", "3", "45s"},
	}
	for role, wants := range lines {
		var line string
		for _, l := range strings.Split(view, "\n") {
			if strings.Contains(l, role) {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("no line for %s in:\n%s", role, view)
		}
		for _, w := range wants {
			if !strings.Contains(line, w) {
				t.Errorf("%s line lacks %q: %q", role, w, line)
			}
		}
	}
}

func TestNoActivityColumnsAt60Columns(t *testing.T) {
	tn := time.Now()
	m := sizedAgents(t, 60, activityUsage(tn), statusClock{now: tn, alive: true})
	view := stripANSI(m.View())
	for _, title := range []string{"State", "Tools", "Duration"} {
		if strings.Contains(view, title) {
			t.Errorf("%s must be dropped at 60 columns:\n%s", title, view)
		}
	}
}

func TestSortKeepsEveryRowsState(t *testing.T) {
	tn := time.Now()
	u := activityUsage(tn)
	u.Agents[1].LastActivity = tn.Add(-20 * time.Minute) // unfinished and quiet: idle
	m := sizedAgents(t, 120, u, statusClock{now: tn, alive: true})
	m, _ = opened(t, m, ".")
	if got := stripANSI(m.View()); !strings.Contains(got, "idle") || !strings.Contains(got, "done") {
		t.Errorf("a sorted list lost the states:\n%s", got)
	}
	for _, r := range m.agentTable.Items() {
		if r.Main {
			continue
		}
		if r.state == stateUnknown {
			t.Errorf("sub-agent %s fell back to unknown after the sort", r.ID)
		}
	}
}

// ── The rebuild on every successful read ─────────────────────────────────────

// quietPoll opens the fixture, then makes a poll read answer with an Empty
// delta: nothing was appended. It returns the model with that read in flight
// and the answer to feed back.
func quietPoll(t *testing.T) (Model, usageDeltaMsg) {
	t.Helper()
	m, _ := atFixture(t)
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 || !reads[0].delta.Empty() || reads[0].err != nil {
		t.Fatalf("precondition: want one clean Empty delta, got %+v", reads)
	}
	return m, reads[0]
}

func stateOf(t *testing.T, m Model, id string) runState {
	t.Helper()
	for _, r := range m.agentTable.Items() {
		if r.ID == id {
			return r.state
		}
	}
	t.Fatalf("no %q row", id)
	return stateUnknown
}

func agentHeaderLine(m Model) string {
	for _, l := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(l, "Agent") && strings.Contains(l, "Total") {
			return l
		}
	}
	return ""
}

// Criterion 2 end to end: an agent turns idle with no new data, because the
// read that finds nothing still rebuilds the rows against its own clock.
func TestAnAgentTurnsIdleOnAnEmptyDelta(t *testing.T) {
	m, quiet := quietPoll(t)
	tn := time.Now()
	u := agentusage.Usage{Agents: []agentusage.Agent{
		stamped(orchestrator(5, 0), 1, tn.Add(-time.Hour), tn),
		stamped(withState(subAgent("a01", "coder", 1, 0), agentusage.StateUnfinished), 2, tn.Add(-time.Hour), tn),
		stamped(withState(subAgent("a02", "tester", 1, 0), agentusage.StateUnfinished), 2, tn.Add(-time.Hour), tn),
	}}
	m.clock = statusClock{now: tn.Add(time.Minute), alive: true}
	m = m.withUsage(u, true)
	m = feed(t, m, testutil.Key("down"))
	m = feed(t, m, testutil.Key("down"))
	selected, _ := m.selectedAgentKey()
	if got := stateOf(t, m, "a01"); got != stateRunning {
		t.Fatalf("precondition: a01 = %s, want running", got.label())
	}
	before := agentHeaderLine(m)

	quiet.at = tn.Add(6 * time.Minute) // nothing written for 6 minutes
	m, msgs := answered(t, m, quiet)
	if len(ticksOf(msgs)) != 1 {
		t.Errorf("an agent turning idle must not end the poll chain, got %+v", msgs)
	}

	if got := stateOf(t, m, "a01"); got != stateIdle {
		t.Errorf("a01 = %s after 6 quiet minutes, want idle", got.label())
	}
	if !strings.Contains(stripANSI(m.View()), "idle") {
		t.Errorf("idle is not on screen:\n%s", m.View())
	}
	if now, _ := m.selectedAgentKey(); now != selected {
		t.Errorf("selection moved from %q to %q", selected, now)
	}
	if after := agentHeaderLine(m); after != before {
		t.Errorf("columns were remeasured on an Empty delta:\n before %q\n after  %q", before, after)
	}
}

func TestACleanReadRefreshesTheClockEvenWhenNothingIsIdleYet(t *testing.T) {
	m, quiet := quietPoll(t)
	quiet.at = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	m = feed(t, m, quiet)
	if !m.clock.now.Equal(quiet.at) {
		t.Errorf("clock.now = %v, want the read's own time %v", m.clock.now, quiet.at)
	}
}

// withRows is not withUsage: rebuilding the rows must not clear the error.
func TestWithRowsKeepsTheErrorState(t *testing.T) {
	m, _ := atFixture(t)
	m.agentsErr, m.agentsLoading = true, true
	m = m.withRows(m.usage, false)
	if !m.agentsErr || !m.agentsLoading {
		t.Errorf("withRows cleared agentsErr=%v agentsLoading=%v", m.agentsErr, m.agentsLoading)
	}
	m = m.withUsage(m.usage, false)
	if m.agentsErr || m.agentsLoading {
		t.Errorf("withUsage must still clear both, got agentsErr=%v agentsLoading=%v", m.agentsErr, m.agentsLoading)
	}
}

func TestADeletedTranscriptGoesNotAliveAndKeepsTheRedFooter(t *testing.T) {
	m, quiet := quietPoll(t)
	tn := time.Now()
	m.clock = statusClock{now: tn, alive: true}
	m = m.withUsage(agentusage.Usage{Agents: []agentusage.Agent{
		stamped(orchestrator(5, 0), 1, tn.Add(-time.Hour), tn),
		stamped(withState(subAgent("a01", "coder", 1, 0), agentusage.StateUnfinished), 2, tn.Add(-time.Hour), tn),
	}}, true)
	if got := stateOf(t, m, ""); got != stateRunning {
		t.Fatalf("precondition: orchestrator = %s, want running", got.label())
	}

	gone := usageDeltaMsg{gen: quiet.gen, err: os.ErrNotExist, at: tn.Add(10 * time.Minute)}
	m = feed(t, m, gone)

	if !m.agentsErr {
		t.Error("the red footer state was cleared by the rebuild")
	}
	if got := stateOf(t, m, ""); got != stateUnknown {
		t.Errorf("orchestrator = %s once the transcript is gone, want unknown", got.label())
	}
	if got := stateOf(t, m, "a01"); got != stateIdle {
		t.Errorf("a01 = %s, want idle (unfinished, quiet 10 min)", got.label())
	}
	if m.clock.alive || !m.clock.now.Equal(gone.at) {
		t.Errorf("clock = %+v, want not alive at %v", m.clock, gone.at)
	}
}

func TestAnotherReadErrorLeavesRowsAndClockAlone(t *testing.T) {
	m, quiet := quietPoll(t)
	tn := time.Now()
	m.clock = statusClock{now: tn, alive: true}
	m = m.withUsage(activityUsage(tn), true)
	before, clock := fmt.Sprint(m.agentTable.Items()), m.clock

	m = feed(t, m, usageDeltaMsg{gen: quiet.gen, err: os.ErrPermission, at: tn.Add(time.Hour)})

	if fmt.Sprint(m.agentTable.Items()) != before {
		t.Error("rows changed on a failed read")
	}
	if m.clock != clock {
		t.Errorf("clock moved on a failed read: %+v -> %+v", clock, m.clock)
	}
}

func TestLeavingTheSessionResetsTheClock(t *testing.T) {
	m, _ := atFixture(t)
	m.clock = statusClock{now: time.Now(), alive: true}
	m, _ = opened(t, m, "esc")
	if m.clock != (statusClock{}) {
		t.Errorf("clock = %+v after closing the agents level, want zero", m.clock)
	}
}

// ── Privacy ──────────────────────────────────────────────────────────────────

// Criterion 3: a tool call is counted, never shown. Its id, name and input
// carry the sentinel and none of it may reach a screen, a footer or a dump.
func TestToolContentNeverReachesTheView(t *testing.T) {
	m, parent := atFixture(t)
	appendLine(t, parent, `{"type":"assistant","timestamp":"2026-10-24T10:00:00.000Z","message":{"id":"msg_sentinel","model":"model-alpha","role":"assistant","content":[`+
		`{"type":"tool_use","id":"toolu_`+sentinel+`_1","name":"`+sentinel+`","input":{"cmd":"`+sentinel+`"}},`+
		`{"type":"text","text":"`+sentinel+`"},{"type":"thinking","thinking":"`+sentinel+`"}],`+
		`"usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`)
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("want one read, got %d", len(reads))
	}
	m, _ = answered(t, m, reads[0])
	m = feed(t, m, testutil.Resize(140, 30))

	for name, out := range map[string]string{
		"view":    m.View(),
		"footer":  footer(m),
		"rows":    fmt.Sprintf("%+v %#v", m.agentTable.Items(), m.usage),
		"message": fmt.Sprintf("%+v %#v", reads[0], reads[0]),
	} {
		if strings.Contains(out, sentinel) || strings.Contains(out, "toolu_") {
			t.Errorf("%s leaks tool content", name)
		}
	}
}

func TestActivityColumnsAreOptionalFixedAndUnsortable(t *testing.T) {
	for _, title := range []string{"State", "Tools", "Duration"} {
		c := column(t, title)
		if !c.Optional || c.Sizing != datatable.SizingFixed {
			t.Errorf("%s: Optional=%v Sizing=%v, want Optional, SizingFixed", title, c.Optional, c.Sizing)
		}
		if c.Less != nil || c.DropFirst {
			t.Errorf("%s: Less set=%v DropFirst=%v, want neither", title, c.Less != nil, c.DropFirst)
		}
	}
}

// A read that pauses the session turns the orchestrator to unknown and the
// header to paused from the same instant: both come from the one statusClock.
func TestAPausedSessionShowsUnknownAndPausedTogether(t *testing.T) {
	m, _ := settledModel(t)
	if got := stateOf(t, m, ""); got != stateUnknown {
		t.Errorf("orchestrator = %s on a paused session, want unknown", got.label())
	}
	wantUpdates(t, "paused session", m, "paused")
	if m.clock.alive {
		t.Error("the clock still says alive on a paused session")
	}
}
