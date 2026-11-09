package agentusageview

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-151 step 4: the footer writes the selected agent's state in words
// (the dot repeats it), and the help carries the legend.

// agentDetailer is looked up through an interface so the tests fail on their
// own message before agentDetail exists, not on a compile error.
type agentDetailer interface {
	agentDetail() components.RowDetail
}

func detailOf(t *testing.T, m Model) components.RowDetail {
	t.Helper()
	v, ok := any(m).(agentDetailer)
	if !ok {
		t.Fatal("agentusageview.Model has no agentDetail() components.RowDetail")
	}
	return v.agentDetail()
}

// session at the agents level; alive=false is a paused session.
func sessionOf(t *testing.T, alive bool, agents ...agentusage.Agent) Model {
	t.Helper()
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	m.clock = statusClock{now: time.Now(), alive: alive}
	return answer(t, m, agentusage.Usage{Agents: agents})
}

func selectRow(t *testing.T, m Model, downs int) Model {
	t.Helper()
	for range downs {
		m, _ = opened(t, m, "down")
	}
	return m
}

func footerLine(m Model) string {
	lines := strings.Split(ansi.Strip(m.RenderFooter(160)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func TestTheFooterWritesTheStateOfTheSelectedAgent(t *testing.T) {
	// recent() puts FirstActivity one minute before LastActivity: ran 1m00s.
	liveOrch := recent(orchestrator(10, 0), agentusage.StateUnknown, 2*time.Minute)
	tests := []struct {
		name  string
		alive bool
		agent agentusage.Agent
		down  int
		state indicator.State
		want  string
	}{
		{"running", true, recent(subAgent("a01", "coder", 5, 0), agentusage.StateUnfinished, 2*time.Minute), 1,
			indicator.Running, "Agent running — last transcript line 2 min ago"},
		{"idle", true, recent(subAgent("a01", "coder", 5, 0), agentusage.StateUnfinished, 10*time.Minute), 1,
			indicator.Suspended, "Agent idle — last transcript line 10 min ago"},
		{"done", true, recent(subAgent("a01", "coder", 5, 0), agentusage.StateFinished, time.Hour), 1,
			indicator.Done, "Agent done — ran 1m00s"},
		{"unknown sub-agent", false, recent(subAgent("a01", "coder", 5, 0), agentusage.StateUnknown, time.Hour), 1,
			indicator.Unknown, "Agent state unknown — transcript has no end marker"},
		{"unknown orchestrator", false, recent(orchestrator(10, 0), agentusage.StateUnknown, time.Hour), 0,
			indicator.Unknown, "Agent state unknown — updates are paused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Model
			if tt.down == 0 {
				m = sessionOf(t, tt.alive, tt.agent)
			} else {
				m = selectRow(t, sessionOf(t, tt.alive, liveOrch, tt.agent), tt.down)
			}
			want := components.DescribeRow(components.RowQuery{Observed: tt.state, ObservedText: tt.want}).Detail
			if got := detailOf(t, m); got != want {
				t.Fatalf("agentDetail = %+v, want %+v", got, want)
			}
			if got := m.status().Detail; got != want {
				t.Errorf("status Detail = %+v, want the agentDetail %+v", got, want)
			}
			if line, wantLine := footerLine(m), indicator.Glyph(tt.state, true)+" "+tt.want; !strings.Contains(line, wantLine) {
				t.Errorf("footer line = %q, want it to contain %q", line, wantLine)
			}
		})
	}
}

func TestTheFooterFollowsTheCursor(t *testing.T) {
	// The orchestrator counts as running while a sub-agent runs (even though it
	// wrote 10 min ago), then come the running coder and the done tester.
	m := runningTeam(t)
	for down, want := range []string{"Agent running — last transcript line 10 min ago", "Agent running — last transcript line now", "Agent done"} {
		if line := footerLine(selectRow(t, m, down)); !strings.Contains(line, want) {
			t.Errorf("cursor %d: footer line = %q, want %q", down, line, want)
		}
	}
}

func TestNoAgentDetailOutsideTheAgentsTab(t *testing.T) {
	m := runningTeam(t)
	m.tab = tabBreakdown
	if d := detailOf(t, m); !d.IsZero() {
		t.Errorf("Breakdown tab carries a detail: %+v", d)
	}
	if line := footerLine(m); strings.Contains(line, "Agent ") {
		t.Errorf("Breakdown footer = %q", line)
	}
	top := withSessions(t, "/p/a.jsonl")
	if !top.status().Detail.IsZero() {
		t.Errorf("sessions level carries a detail: %+v", top.status().Detail)
	}
}

func TestAReadErrorStillWinsOverTheAgentDetail(t *testing.T) {
	m := runningTeam(t)
	m.agentsErr = true
	st := m.status()
	if st.Level != components.LevelError || !strings.Contains(st.Text, "Failed to read this session") {
		t.Errorf("status = %+v, want the read error kept", st)
	}
	if !st.Detail.IsZero() {
		t.Errorf("an error status carries the row detail %+v", st.Detail)
	}
	m = runningTeam(t)
	m.agentsLoading = true
	if st := m.status(); !st.Spinner || !st.Detail.IsZero() {
		t.Errorf("a loading status = %+v, want the spinner alone", st)
	}
}

func TestTheHelpCarriesTheActivityLegend(t *testing.T) {
	want := indicator.Legend(indicator.AgentsLegend...)
	var found bool
	for _, s := range runningTeam(t).GetHelpContent().Sections {
		if s.Title == want.Title {
			found = true
			if s.Body != want.Body {
				t.Errorf("legend body = %q, want %q", s.Body, want.Body)
			}
		}
	}
	if !found {
		t.Fatalf("no %q section in the help", want.Title)
	}
	for _, line := range []string{"idle (no activity for 5 min)", "done", "unknown"} {
		if !strings.Contains(want.Body, line) {
			t.Fatalf("the agents legend lost %q", line)
		}
	}
}

func TestTheActivityFooterAddsNoShortcut(t *testing.T) {
	want := strings.Join(testutil.ShortcutKeys(withSessionsAtAgents(t).GetShortcuts()), ",")
	got := strings.Join(testutil.ShortcutKeys(runningTeam(t).GetShortcuts()), ",")
	if got != want {
		t.Errorf("shortcuts changed with live agents: %q, want %q", got, want)
	}
}

func withSessionsAtAgents(t *testing.T) Model {
	t.Helper()
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	return answer(t, m, agentusage.Usage{Agents: []agentusage.Agent{orchestrator(10, 0)}})
}
