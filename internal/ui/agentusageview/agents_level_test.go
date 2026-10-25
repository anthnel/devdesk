package agentusageview

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The second level of the view: a session's agents (DEVDESK-42, step 2).

// fixtureParent is the basic fixture's parent transcript, relative to this
// package. Its sub-agents are coder, tester (partial output) and reviewer.
const fixtureParent = "../../agentusage/testdata/v2.1.302-basic/projects/-synthetic-proj-basic/00000000-0000-4000-8000-000000000302.jsonl"

// sentinel is planted in the fixture's meta files; it must never reach a screen.
const sentinel = "SENTINEL-DO-NOT-LEAK"

// threeSubs is a usage with an orchestrator and three sub-agents.
func threeSubs() agentusage.Usage {
	return agentusage.Usage{Agents: []agentusage.Agent{
		orchestrator(320, 0),
		subAgent("a01", "coder", 140, 0),
		subAgent("a02", "tester", 38, 1),
		subAgent("a03", "reviewer", 90, 0),
	}}
}

// withSessions loads two sessions whose parents are the given paths.
func withSessions(t *testing.T, parents ...string) Model {
	t.Helper()
	var ss []agentusage.Session
	for i, p := range parents {
		s := session("id-"+string(rune('a'+i))+"000000-0000", "/srv/p"+string(rune('a'+i)), time.Duration(i)*time.Hour)
		s.Parent = p
		ss = append(ss, s)
	}
	return loaded(t, newModel(t), ss...)
}

// opened presses → on the selected row and returns the model and the Cmd.
func opened(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(testutil.Key(key))
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T", updated)
	}
	return next, cmd
}

// answer delivers a parse result for the model's current agent generation.
func answer(t *testing.T, m Model, u agentusage.Usage) Model {
	t.Helper()
	return feed(t, m, sessionUsageLoadedMsg{gen: m.agentGen, usage: u})
}

// batchMsgs runs every Cmd of a batch concurrently and returns what they
// produced. testutil.Msgs would do the same; this one is bounded so a stuck
// Cmd fails the test instead of hanging it.
func batchMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	cmds := []tea.Cmd{cmd}
	if b, ok := cmd().(tea.BatchMsg); ok {
		cmds = b
	}
	out := make(chan tea.Msg, len(cmds))
	for _, c := range cmds {
		go func() {
			if c == nil {
				out <- nil
				return
			}
			out <- c()
		}()
	}
	var msgs []tea.Msg
	for range cmds {
		select {
		case m := <-out:
			msgs = append(msgs, m)
		case <-time.After(5 * time.Second):
			t.Fatal("a Cmd of the batch did not answer")
		}
	}
	return msgs
}

func usageMsgs(msgs []tea.Msg) []sessionUsageLoadedMsg {
	var got []sessionUsageLoadedMsg
	for _, m := range msgs {
		if u, ok := m.(sessionUsageLoadedMsg); ok {
			got = append(got, u)
		}
	}
	return got
}

// ── Opening ──────────────────────────────────────────────────────────────────

func TestRightOpensTheAgentsOfTheSelectedSession(t *testing.T) {
	for _, key := range []string{"right", "enter"} {
		t.Run(key, func(t *testing.T) {
			m := withSessions(t, "/p/a.jsonl", "/p/b.jsonl")
			m.table.SetCursor(1)
			sel, _ := m.table.Selected()

			m, cmd := opened(t, m, key)
			if m.level != levelAgents {
				t.Fatalf("level = %v after %s, want levelAgents", m.level, key)
			}
			if m.open.ID != sel.ID {
				t.Errorf("open.ID = %q, want the selected row's %q", m.open.ID, sel.ID)
			}
			if !m.agentsLoading {
				t.Error("opening starts a parse")
			}
			if got := usageMsgs(batchMsgs(t, cmd)); len(got) != 1 || got[0].gen != m.agentGen {
				t.Fatalf("the batch must hold one parse for gen %d, got %+v", m.agentGen, got)
			}

			m = answer(t, m, threeSubs())
			view := m.View()
			for _, want := range []string{"orchestrator", "├─ coder", "├─ tester", "└─ reviewer", "model-a", "model-b"} {
				if !strings.Contains(view, want) {
					t.Errorf("agents view lacks %q:\n%s", want, view)
				}
			}
			if strings.Contains(view, "/srv/pb") {
				t.Errorf("session rows must not show at the agents level:\n%s", view)
			}
		})
	}
}

func TestWhileParsingTheAgentTableKeepsItsHeadersAndHasNoRows(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	view := m.View()
	for _, title := range []string{"Agent", "Model", "In", "Out", "Total"} {
		if !strings.Contains(view, title) {
			t.Errorf("loading agents view lacks column %q:\n%s", title, view)
		}
	}
	if strings.Contains(view, "orchestrator") {
		t.Error("no row before the parse answers")
	}
	if !m.agentsLoading {
		t.Error("agentsLoading must be true")
	}
	if f := footer(m); !strings.Contains(f, "Reading session...") {
		t.Errorf("footer should show the parse status:\n%s", f)
	}
}

func TestOpeningWithNoSessionWarnsAndDoesNotCrash(t *testing.T) {
	for _, key := range []string{"right", "enter"} {
		t.Run(key, func(t *testing.T) {
			m := loaded(t, newModel(t))
			before := footer(m)
			m, cmd := opened(t, m, key)
			if m.level != levelSessions {
				t.Fatalf("level = %v, an empty list has nothing to open", m.level)
			}
			if m.agentsLoading {
				t.Error("no parse may start")
			}
			if cmd == nil {
				t.Error("the Warn carries its timer")
			}
			if after := footer(m); after == before {
				t.Errorf("the refusal must reach the footer, got:\n%s", after)
			}
		})
	}
}

// ── Going back ───────────────────────────────────────────────────────────────

func TestBackReturnsToTheSameRowWithTheFilterKept(t *testing.T) {
	for _, key := range []string{"left", "esc"} {
		t.Run(key, func(t *testing.T) {
			m := withSessions(t, "/p/a.jsonl", "/p/b.jsonl", "/p/c.jsonl")
			m.table.SetCursor(1)
			m = send(t, m, testutil.Key("/"))
			for _, k := range testutil.Type("srv") {
				m = feed(t, m, k)
			}
			m = send(t, m, testutil.Key("enter")) // confirms the filter
			visible := len(m.table.Visible())
			cursor := m.table.Cursor()

			m, _ = opened(t, m, "right")
			m = answer(t, m, threeSubs())
			m, _ = opened(t, m, key)

			if m.level != levelSessions {
				t.Fatalf("level = %v after %s, want levelSessions", m.level, key)
			}
			if got := m.table.Cursor(); got != cursor {
				t.Errorf("cursor = %d after going back, want %d", got, cursor)
			}
			if got := len(m.table.Visible()); got != visible || m.table.FilterBar().SearchQuery() != "srv" {
				t.Errorf("filter lost: %d visible (want %d), query %q", got, visible, m.table.FilterBar().SearchQuery())
			}
			if m.agentsLoading {
				t.Error("going back stops the loading state")
			}
		})
	}
}

func TestAStaleParseIsIgnored(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl", "/p/b.jsonl")
	m, _ = opened(t, m, "right")
	genA := m.agentGen
	m, _ = opened(t, m, "esc")
	m.table.SetCursor(1)
	m, _ = opened(t, m, "right")
	if m.agentGen <= genA+1 {
		t.Fatalf("agentGen = %d, open/back/open must advance it past %d", m.agentGen, genA+1)
	}

	m = feed(t, m, sessionUsageLoadedMsg{gen: genA, usage: threeSubs()})
	if !m.agentsLoading {
		t.Error("the answer for A must not end B's loading")
	}
	if strings.Contains(m.View(), "orchestrator") {
		t.Error("the answer for A must not fill B's table")
	}
}

func TestAnAnswerAfterBackIsIgnored(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	gen := m.agentGen
	m, _ = opened(t, m, "esc")
	m = feed(t, m, sessionUsageLoadedMsg{gen: gen, usage: threeSubs()})
	if m.level != levelSessions {
		t.Errorf("level = %v, a late answer must not reopen the level", m.level)
	}
	if len(m.agentTable.Items()) != 0 {
		t.Error("a late answer must not fill the agent table")
	}
}

// ── Errors ───────────────────────────────────────────────────────────────────

func TestAParseErrorShowsInTheFooterAndLogsNoPath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "secret-project-name", "00000000-0000-4000-8000-0000000000aa.jsonl")

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	m := withSessions(t, missing)
	m, cmd := opened(t, m, "right")
	got := usageMsgs(batchMsgs(t, cmd))
	if len(got) != 1 || got[0].err == nil {
		t.Fatalf("a missing transcript must come back as an error, got %+v", got)
	}
	m = feed(t, m, got[0])

	if m.level != levelAgents || !m.agentsErr || m.agentsLoading {
		t.Errorf("level=%v agentsErr=%v loading=%v, want the agents level in error", m.level, m.agentsErr, m.agentsLoading)
	}
	if f := footer(m); !strings.Contains(f, "Failed to read this session") {
		t.Errorf("footer should report the failure:\n%s", f)
	}
	if strings.Contains(buf.String(), dir) || strings.Contains(buf.String(), "secret-project-name") {
		t.Errorf("the log must not carry the session path:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "ERROR [agentusageview]") {
		t.Errorf("the failure must be logged in the standard format:\n%s", buf.String())
	}
	if strings.Contains(m.View(), "orchestrator") {
		t.Error("a first failure leaves the table empty")
	}
}

func TestARefreshFailureKeepsTheAgentRows(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	m = answer(t, m, threeSubs())
	m, _ = opened(t, m, "ctrl+r")
	m = feed(t, m, sessionUsageLoadedMsg{gen: m.agentGen, err: errors.New("boom")})
	if !m.agentsErr {
		t.Fatal("agentsErr must be set")
	}
	if !strings.Contains(m.View(), "├─ coder") {
		t.Error("rows already shown stay after a failed refresh")
	}
}

// ── Refresh and Init ─────────────────────────────────────────────────────────

func TestCtrlRAtTheAgentsLevelParsesAgain(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	m = answer(t, m, threeSubs())
	gen, listGen := m.agentGen, m.gen

	m, cmd := opened(t, m, "ctrl+r")
	if m.agentGen != gen+1 {
		t.Errorf("agentGen = %d, want %d", m.agentGen, gen+1)
	}
	if m.gen != listGen {
		t.Error("ctrl+r at the agents level must not re-list")
	}
	if !m.agentsLoading || m.level != levelAgents {
		t.Error("ctrl+r keeps the level and starts loading")
	}
	if !strings.Contains(m.View(), "├─ coder") {
		t.Error("rows stay on screen during a refresh (Rule 139)")
	}
	got := usageMsgs(batchMsgs(t, cmd))
	if len(got) != 1 || got[0].gen != m.agentGen {
		t.Errorf("want one parse for gen %d, got %+v", m.agentGen, got)
	}
}

func TestInitAtTheAgentsLevelParsesTheOpenSessionToo(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")

	msgs := batchMsgs(t, m.Init())
	if got := usageMsgs(msgs); len(got) != 1 || got[0].gen != m.agentGen {
		t.Errorf("Init must parse the open session for gen %d, got %+v", m.agentGen, got)
	}
	var lists int
	for _, msg := range msgs {
		if _, ok := msg.(sessionListLoadedMsg); ok {
			lists++
		}
	}
	if lists != 1 {
		t.Errorf("Init must still re-list, got %d listings", lists)
	}

	// At the sessions level Init parses nothing.
	top := withSessions(t, "/p/a.jsonl")
	if got := usageMsgs(batchMsgs(t, top.Init())); len(got) != 0 {
		t.Errorf("sessions-level Init must not parse, got %+v", got)
	}
}

func TestASpinnerTickKeepsRunningWhileTheAgentsLoad(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	if _, cmd := m.Update(spinnerTick()); cmd != nil {
		t.Error("not loading at either level: the chain ends")
	}
	m, _ = opened(t, m, "right")
	if _, cmd := m.Update(spinnerTick()); cmd == nil {
		t.Error("loading agents: the chain continues")
	}
	m = answer(t, m, threeSubs())
	if _, cmd := m.Update(spinnerTick()); cmd != nil {
		t.Error("the answer ends the chain")
	}
}

// ── Filter ───────────────────────────────────────────────────────────────────

func TestTheAgentsLevelHasItsOwnFilter(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	m = answer(t, m, threeSubs())

	m = send(t, m, testutil.Key("/"))
	if !m.InEditMode() {
		t.Fatal("/ opens the agents filter")
	}
	for _, k := range testutil.Type("coder") {
		m = feed(t, m, k)
	}
	view := m.View()
	if !strings.Contains(view, "├─ coder") || strings.Contains(view, "orchestrator") {
		t.Errorf("only coder should remain:\n%s", view)
	}
	if m.table.FilterBar().IsVisible() {
		t.Error("the sessions filter must stay untouched")
	}
	// enter while typing confirms the filter; it opens nothing.
	m = send(t, m, testutil.Key("enter"))
	if m.level != levelAgents || m.agentsLoading {
		t.Error("enter in the filter must not reopen the session")
	}
}

// ── Layout (Rules 108, 116) ──────────────────────────────────────────────────

func TestBothTablesAreResizedTogether(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m = feed(t, m, testutil.Resize(80, 20))
	m, _ = opened(t, m, "right")
	m = answer(t, m, threeSubs())
	if got := m.agentTable.RenderedWidth(); got != 78 {
		t.Errorf("agent table width = %d, want 78", got)
	}

	m = feed(t, m, testutil.Resize(100, 20)) // resized while the agents level is open
	m, _ = opened(t, m, "esc")
	if got := m.table.RenderedWidth(); got != 98 {
		t.Errorf("session table width = %d after a resize at the agents level, want 98", got)
	}
}

func TestNarrowTerminalsDropTheCacheColumnsLastWriteFirst(t *testing.T) {
	tests := []struct {
		width       int
		read, write bool
	}{
		{80, true, false},
		{60, false, false},
	}
	for _, tt := range tests {
		m := withSessions(t, "/p/a.jsonl")
		m = feed(t, m, testutil.Resize(tt.width, 20))
		m, _ = opened(t, m, "right")
		m = answer(t, m, threeSubs())
		view := m.View()
		if got := strings.Contains(view, "Cache read"); got != tt.read {
			t.Errorf("width %d: Cache read shown = %v, want %v", tt.width, got, tt.read)
		}
		if got := strings.Contains(view, "Cache write"); got != tt.write {
			t.Errorf("width %d: Cache write shown = %v, want %v", tt.width, got, tt.write)
		}
		for _, title := range []string{"Agent", "Model", "In", "Out", "Total"} {
			if !strings.Contains(view, title) {
				t.Errorf("width %d: column %q must stay:\n%s", tt.width, title, view)
			}
		}
	}
}

// ── End to end ───────────────────────────────────────────────────────────────

func TestTheBasicFixtureShowsItsFourAgents(t *testing.T) {
	m := withSessions(t, fixtureParent)
	m, cmd := opened(t, m, "enter")
	got := usageMsgs(batchMsgs(t, cmd))
	if len(got) != 1 || got[0].err != nil {
		t.Fatalf("the fixture must parse cleanly, got %+v", got)
	}
	m = feed(t, m, got[0])

	if n := len(m.agentTable.Visible()); n != 4 {
		t.Fatalf("%d agent rows, want 4", n)
	}
	lines := map[string]string{}
	for _, line := range strings.Split(m.View(), "\n") {
		for _, role := range []string{"orchestrator", "coder", "tester", "reviewer"} {
			if strings.Contains(line, role) {
				lines[role] = line
			}
		}
	}
	want := map[string]string{
		"orchestrator": "320",
		"coder":        "140",
		"tester":       "≥ 38",
		"reviewer":     "90",
	}
	for role, out := range want {
		line, ok := lines[role]
		if !ok {
			t.Errorf("no row for %s:\n%s", role, m.View())
			continue
		}
		if !strings.Contains(line, out) {
			t.Errorf("%s row lacks Out %q: %q", role, out, line)
		}
		if role != "tester" && strings.Contains(line, "≥") {
			t.Errorf("only the tester's output is a floor: %q", line)
		}
	}
	if strings.Contains(m.View(), sentinel) || strings.Contains(footer(m), sentinel) {
		t.Error("the fixture's sentinel text reached the screen")
	}
}

// ── Review fixes ─────────────────────────────────────────────────────────────

func TestOpeningAnotherSessionRemeasuresTheAgentColumn(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl", "/p/b.jsonl")
	m = feed(t, m, testutil.Resize(130, 20))
	m, _ = opened(t, m, "right")
	m = answer(t, m, threeSubs())
	m, _ = opened(t, m, "esc")

	long := strings.Repeat("t", 24)
	m, _ = opened(t, m, "down")
	m, _ = opened(t, m, "right")
	m = answer(t, m, agentusage.Usage{Agents: []agentusage.Agent{orchestrator(1, 0), subAgent("b01", long, 1, 0)}})
	if got, want := m.agentTable.Table().Columns()[0].Width, utf8.RuneCountInString(lastStem+long); got < want {
		t.Errorf("Agent column is %d wide, want at least %d", got, want)
	}
}

func TestOpeningASessionDropsTheAgentsFilter(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl", "/p/b.jsonl")
	m, _ = opened(t, m, "right")
	m = answer(t, m, threeSubs())
	m = send(t, m, testutil.Key("/"))
	for _, k := range testutil.Type("coder") {
		m = feed(t, m, k)
	}
	m = send(t, m, testutil.Key("enter"))
	m, _ = opened(t, m, "esc")

	m, _ = opened(t, m, "down")
	m, _ = opened(t, m, "right")
	m = answer(t, m, threeSubs())
	if got := len(m.agentTable.Visible()); got != 4 {
		t.Errorf("%d agents visible after opening another session, want 4", got)
	}
	if q := m.agentTable.FilterBar().SearchQuery(); q != "" {
		t.Errorf("filter %q survived", q)
	}
}
