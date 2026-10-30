package agentusageview

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Live updates, step 1 (DEVDESK-45): rebuilding the agent rows while keeping
// the cursor on the same agent, the filter and the sort, and remeasuring only
// when the table holds a different population.

// selectAgent moves the cursor onto the visible row with the given ID ("" is
// the orchestrator).
func selectAgent(t *testing.T, m Model, id string) Model {
	t.Helper()
	for i, r := range m.agentTable.Visible() {
		if r.ID == id {
			m.agentTable.SetCursor(i)
			return m
		}
	}
	t.Fatalf("no visible agent %q in %q", id, visibleIDs(m))
	return m
}

func selectedID(t *testing.T, m Model) string {
	t.Helper()
	r, ok := m.agentTable.Selected()
	if !ok {
		t.Fatal("no agent selected")
	}
	return r.ID
}

func agentColumnWidth(m Model) int { return m.agentTable.Table().Columns()[0].Width }

func TestAgentKeysIdentifyRowsAcrossRebuilds(t *testing.T) {
	tests := []struct {
		name   string
		agents []agentusage.Agent
		want   []string
	}{
		{"orchestrator alone", []agentusage.Agent{orchestrator(1, 0)}, []string{"main"}},
		{
			"tree order",
			[]agentusage.Agent{orchestrator(1, 0), subAgent("a01", "coder", 1, 0), subAgent("a02", "tester", 1, 0)},
			[]string{"main", "sub:a01#0", "sub:a02#0"},
		},
		{
			"two sub-agents sharing one ID stay distinct",
			[]agentusage.Agent{subAgent("dup", "coder", 1, 0), subAgent("dup", "tester", 1, 0)},
			[]string{"sub:dup#0", "sub:dup#1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, r := range agentRows(agentusage.Usage{Agents: tt.agents}, true, statusClock{}) {
				got = append(got, r.key)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("row keys = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestACursorStaysOnItsAgentWhenAnotherSortsBeforeIt(t *testing.T) {
	base := agentusage.Usage{Agents: []agentusage.Agent{
		orchestrator(10, 0), subAgent("a01", "coder", 10, 0), subAgent("a02", "tester", 10, 0),
	}}
	m := selectAgent(t, atWideAgents(t, base), "a02")
	if m.agentTable.Cursor() != 2 {
		t.Fatalf("setup: cursor = %d, want 2", m.agentTable.Cursor())
	}

	grown := agentusage.Usage{Agents: append([]agentusage.Agent{subAgent("a00", "planner", 10, 0)}, base.Agents...)}
	m = m.withUsage(grown, false)

	if got := selectedID(t, m); got != "a02" {
		t.Errorf("selected %q after a00 joined, want a02", got)
	}
	if got := m.agentTable.Cursor(); got != 3 {
		t.Errorf("cursor = %d, want 3 (main, a00, a01, a02)", got)
	}
}

func TestACursorFollowsItsAgentWhenTheTotalSortReordersRows(t *testing.T) {
	small := agentusage.Usage{Agents: []agentusage.Agent{
		totalled(orchestrator(0, 0), 1000, 0),
		totalled(subAgent("a01", "coder", 0, 0), 800, 0),
		totalled(subAgent("a02", "tester", 0, 0), 300, 0),
	}}
	m := atWideAgents(t, small)
	m, _ = opened(t, m, ".") // Total, largest first
	if got := visibleIDs(m); got != ",a01,a02" {
		t.Fatalf("setup: sorted order = %q", got)
	}
	m = selectAgent(t, m, "a01")
	if m.agentTable.Cursor() != 1 {
		t.Fatalf("setup: cursor = %d, want 1", m.agentTable.Cursor())
	}

	grown := agentusage.Usage{Agents: []agentusage.Agent{
		small.Agents[0],
		totalled(subAgent("a01", "coder", 0, 0), 2000, 0),
		small.Agents[2],
	}}
	m = m.withUsage(grown, false)

	if got := visibleIDs(m); got != "a01,,a02" {
		t.Fatalf("order after growth = %q, want a01,,a02", got)
	}
	if got := selectedID(t, m); got != "a01" {
		t.Errorf("selected %q, want the coder still", got)
	}
	if got := m.agentTable.Cursor(); got != 0 {
		t.Errorf("cursor = %d, want 0", got)
	}
}

// twinsUsage has two sub-agents sharing one ID, told apart by their type.
func twinsUsage(a, b int64) agentusage.Usage {
	return agentusage.Usage{Agents: []agentusage.Agent{
		totalled(subAgent("dup", "coder", 0, 0), a, 0),
		totalled(subAgent("dup", "tester", 0, 0), b, 0),
	}}
}

func selectedType(t *testing.T, m Model) string {
	t.Helper()
	r, ok := m.agentTable.Selected()
	if !ok {
		t.Fatal("no agent selected")
	}
	return r.Type
}

func TestACursorStaysOnItsTwinWhenTheTotalSortSwapsTheirRanks(t *testing.T) {
	m := atWideAgents(t, twinsUsage(200, 100))
	m, _ = opened(t, m, ".") // Total, largest first: coder, tester
	if got := selectedType(t, m); got != "coder" {
		t.Fatalf("setup: selected %q, want coder", got)
	}

	m = m.withUsage(twinsUsage(200, 300), false) // tester overtakes coder

	if got := selectedType(t, m); got != "coder" {
		t.Errorf("selected %q after the ranks swapped, want coder", got)
	}
	if got := m.agentTable.Cursor(); got != 1 {
		t.Errorf("cursor = %d, want 1 (tester, coder)", got)
	}
}

func TestCyclingTheSortKeepsTheCursorOnItsAgent(t *testing.T) {
	m := atWideAgents(t, sortUsage(0))
	m = selectAgent(t, m, "a01")

	m, _ = opened(t, m, ".") // tree order -> Total, largest first

	if got := selectedID(t, m); got != "a01" {
		t.Errorf("selected %q after the sort changed, want a01", got)
	}
}

func TestAVanishedAgentClampsTheCursorWithoutPanicking(t *testing.T) {
	m := selectAgent(t, atWideAgents(t, threeSubs()), "a03")

	m = m.withUsage(agentusage.Usage{Agents: threeSubs().Agents[:2]}, true)

	if n := len(m.agentTable.Visible()); n != 2 {
		t.Fatalf("%d rows, want 2", n)
	}
	if _, ok := m.agentTable.Selected(); !ok {
		t.Error("a row must stay selected")
	}
	if c := m.agentTable.Cursor(); c < 0 || c > 1 {
		t.Errorf("cursor = %d out of range", c)
	}
}

func TestTheFilterAndItsBarSurviveAnUpdate(t *testing.T) {
	m := atWideAgents(t, threeSubs())
	m = send(t, m, testutil.Key("/"))
	for _, k := range testutil.Type("test") {
		m = feed(t, m, k)
	}
	m = send(t, m, testutil.Key("enter"))
	if got := visibleIDs(m); got != "a02" {
		t.Fatalf("setup: filter shows %q", got)
	}

	grown := threeSubs()
	grown.Agents[2] = subAgent("a02", "tester", 999, 0)
	m = m.withUsage(grown, false)

	if got := visibleIDs(m); got != "a02" {
		t.Errorf("visible = %q, want only the tester", got)
	}
	if q := m.agentTable.FilterBar().SearchQuery(); q != "test" {
		t.Errorf("query = %q, want test", q)
	}
	if !m.FilterBarVisible() {
		t.Error("the filter bar must stay visible")
	}
}

func TestWithUsageClearsTheLoadingAndErrorStates(t *testing.T) {
	m := atWideAgents(t, threeSubs())
	m.agentsLoading, m.agentsErr = true, true

	m = m.withUsage(threeSubs(), false)

	if m.agentsLoading || m.agentsErr {
		t.Errorf("agentsLoading=%v agentsErr=%v, want both false", m.agentsLoading, m.agentsErr)
	}
}

func TestRemeasureHappensOnlyOnAFullReadOrAChangedPopulation(t *testing.T) {
	long := strings.Repeat("x", 30)
	coderOnly := func(typ string, models ...string) agentusage.Usage {
		c := subAgent("a01", typ, 1, 0)
		if models != nil {
			c.Models = models
		}
		return agentusage.Usage{Agents: []agentusage.Agent{orchestrator(1, 0), c}}
	}
	tests := []struct {
		name      string
		next      agentusage.Usage
		remeasure bool
		widened   bool
	}{
		{"same population, longer type, partial delta", coderOnly(long), false, false},
		{"same population, full read", coderOnly(long), true, true},
		{"a new sub-agent", agentusage.Usage{Agents: []agentusage.Agent{
			orchestrator(1, 0), subAgent("a01", "coder", 1, 0), subAgent("a02", long, 1, 0),
		}}, false, true},
		{"the model list of an existing agent changes", coderOnly(long, "m-b", "m-c"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := atWideAgents(t, coderOnly("coder"))
			before := agentColumnWidth(m)

			m = m.withUsage(tt.next, tt.remeasure)

			got := agentColumnWidth(m)
			if tt.widened && got < utf8.RuneCountInString(lastStem+long) {
				t.Errorf("Agent column is %d wide, want at least %d", got, utf8.RuneCountInString(lastStem+long))
			}
			if !tt.widened && got != before {
				t.Errorf("Agent column went from %d to %d, want unchanged", before, got)
			}
		})
	}
}

func TestANewSubAgentIsRenderedWholeAtOnce(t *testing.T) {
	m := atWideAgents(t, agentusage.Usage{Agents: []agentusage.Agent{
		orchestrator(1, 0), subAgent("a01", "coder", 1, 0),
	}})

	m = m.withUsage(agentusage.Usage{Agents: []agentusage.Agent{
		orchestrator(1, 0), subAgent("a01", "coder", 1, 0), subAgent("a02", "general-purpose", 1, 0),
	}}, false)

	if view := stripANSI(m.View()); !strings.Contains(view, lastStem+"general-purpose") {
		t.Errorf("the new agent's type is cut:\n%s", view)
	}
}
