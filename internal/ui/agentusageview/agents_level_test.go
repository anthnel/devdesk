package agentusageview

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
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

// answer shows u as the answer to the read in flight: it goes through the same
// entry the delta handler uses once the aggregate has applied the delta
// (withUsage), then marks the read as answered. Synthetic usage cannot travel
// as a Delta, whose fields are private to internal/agentusage.
func answer(t *testing.T, m Model, u agentusage.Usage) Model {
	t.Helper()
	m = m.withUsage(u, true)
	m.reading = false
	return m
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

func usageMsgs(msgs []tea.Msg) []usageDeltaMsg {
	var got []usageDeltaMsg
	for _, m := range msgs {
		if u, ok := m.(usageDeltaMsg); ok {
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

	m = feed(t, m, usageDeltaMsg{gen: genA})
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
	m = feed(t, m, usageDeltaMsg{gen: gen})
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
	m = feed(t, m, usageDeltaMsg{gen: m.agentGen, err: errors.New("boom")})
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

// Init at the agents level keeps listing and also resumes the open session:
// it cannot change the model, so it asks Update to (agentsResumeMsg). It reads
// no session itself; the resume does, once Update has the message.
func TestInitAtTheAgentsLevelRelistsAndResumes(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")

	msgs := batchMsgs(t, m.Init())
	var lists, resumes int
	for _, msg := range msgs {
		switch msg.(type) {
		case sessionListLoadedMsg:
			lists++
		case agentsResumeMsg:
			resumes++
		}
	}
	if lists != 1 {
		t.Errorf("Init must still re-list, got %d listings", lists)
	}
	if resumes != 1 {
		t.Errorf("Init must ask to resume the open session, got %d agentsResumeMsg in %+v", resumes, msgs)
	}
	if got := usageMsgs(msgs); len(got) != 0 {
		t.Errorf("Init reads no session by itself, got %+v", got)
	}

	// At the sessions level Init reads no session and resumes nothing.
	top := withSessions(t, "/p/a.jsonl")
	topMsgs := batchMsgs(t, top.Init())
	if got := usageMsgs(topMsgs); len(got) != 0 {
		t.Errorf("sessions-level Init must not read a session, got %+v", got)
	}
	for _, msg := range topMsgs {
		if _, ok := msg.(agentsResumeMsg); ok {
			t.Error("sessions-level Init must not resume")
		}
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

// header returns the column titles of the rendered header row.
func header(t *testing.T, view string) []string {
	t.Helper()
	for _, line := range strings.Split(stripANSI(view), "\n") {
		line = strings.NewReplacer("│", " ", "▲", " ", "▼", " ").Replace(line)
		if strings.Contains(line, "Agent") && strings.Contains(line, "Total") {
			return splitTitles(line)
		}
	}
	t.Fatalf("no header row in:\n%s", view)
	return nil
}

// splitTitles reads known titles off a header line, "Cache read" being two words.
func splitTitles(line string) []string {
	var got []string
	for _, title := range []string{"Agent", "ID", "Model", "In", "Out", "Cache read", "Cache write", "Total", "Share"} {
		for _, f := range strings.Split(line, "  ") {
			if strings.TrimSpace(f) == title {
				got = append(got, title)
			}
		}
	}
	return got
}

func TestNarrowTerminalsDropColumnsInTheOrderThePlanComputed(t *testing.T) {
	tests := []struct {
		width int
		want  []string
	}{
		{100, []string{"Agent", "ID", "Model", "In", "Out", "Cache read", "Total", "Share"}},
		{80, []string{"Agent", "ID", "Model", "In", "Out", "Total", "Share"}},
		{60, []string{"Agent", "ID", "Out", "Total", "Share"}},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.width), func(t *testing.T) {
			m := withSessions(t, "/p/a.jsonl")
			m = feed(t, m, testutil.Resize(tt.width, 20))
			m, _ = opened(t, m, "right")
			m = answer(t, m, threeSubs())
			if got := m.agentTable.RenderedWidth(); got != tt.width-2 {
				t.Errorf("rendered width = %d, want %d (Rule 116)", got, tt.width-2)
			}
			got := header(t, m.View())
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("columns at %d = %v, want %v:\n%s", tt.width, got, tt.want, m.View())
			}
		})
	}
}

// Criterion 3 on screen: two agents of one type stay apart on a narrow terminal.
func TestTwoCodersStayDistinctOnANarrowTerminal(t *testing.T) {
	u := agentusage.Usage{Agents: []agentusage.Agent{
		orchestrator(1, 0),
		subAgent("a0044dcf4954e5b04", "coder", 1, 0),
		subAgent("a029ee8f056637296", "coder", 1, 0),
	}}
	for _, width := range []int{80, 60} {
		m := withSessions(t, "/p/a.jsonl")
		m = feed(t, m, testutil.Resize(width, 20))
		m, _ = opened(t, m, "right")
		m = answer(t, m, u)
		var ids []string
		for _, line := range strings.Split(stripANSI(m.View()), "\n") {
			if !strings.Contains(line, "coder") {
				continue
			}
			for _, id := range []string{"a0044dcf", "a029ee8f"} {
				if strings.Contains(line, id) {
					ids = append(ids, id)
				}
			}
		}
		if len(ids) != 2 || ids[0] == ids[1] {
			t.Errorf("width %d: the two coder lines must show two different IDs, got %v:\n%s", width, ids, m.View())
		}
	}
}

// ── Sort and share (DEVDESK-43) ──────────────────────────────────────────────

// totalled sets an agent's tokens: output out, the rest as input.
func totalled(a agentusage.Agent, total, out int64) agentusage.Agent {
	a.Tokens = agentusage.Tokens{Input: total - out, Output: out}
	return a
}

// sortUsage is the plan's usage U: totals 1000 / 5000 / 3000 / 200 of 9200,
// the tester's output being a floor.
func sortUsage(testerPartial int) agentusage.Usage {
	return agentusage.Usage{Agents: []agentusage.Agent{
		totalled(orchestrator(0, 0), 1000, 0),
		totalled(subAgent("a01", "coder", 0, 0), 5000, 0),
		totalled(subAgent("a02", "tester", 0, testerPartial), 3000, 38),
		totalled(subAgent("a03", "reviewer", 0, 0), 200, 0),
	}}
}

func visibleIDs(m Model) string {
	var ids []string
	for _, r := range m.agentTable.Visible() {
		ids = append(ids, r.ID)
	}
	return strings.Join(ids, ",")
}

// atWideAgents is the agents level at 120 columns, so no column gives way.
func atWideAgents(t *testing.T, u agentusage.Usage) Model {
	t.Helper()
	m := withSessions(t, "/p/a.jsonl", "/p/b.jsonl")
	m = feed(t, m, testutil.Resize(120, 20))
	m, _ = opened(t, m, "right")
	return answer(t, m, u)
}

func lineOf(t *testing.T, m Model, role string) string {
	t.Helper()
	for _, line := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(line, role) {
			return line
		}
	}
	t.Fatalf("no line for %s:\n%s", role, m.View())
	return ""
}

// Criteria 1 and 4: the largest agent comes first, with no branches.
func TestDotSortsTheAgentsLargestFirst(t *testing.T) {
	m := atWideAgents(t, sortUsage(1))
	if got := visibleIDs(m); got != ",a01,a02,a03" {
		t.Fatalf("tree order = %q", got)
	}
	m, _ = opened(t, m, ".")
	if got := visibleIDs(m); got != "a01,a02,,a03" {
		t.Errorf("sorted order = %q, want a01,a02,,a03", got)
	}
	if col, desc := m.agentTable.SortState(); col != 7 || !desc {
		t.Errorf("SortState = (%d, %v), want (7 Total, descending)", col, desc)
	}
	view := m.View()
	if !strings.Contains(view, "Total ▼") {
		t.Errorf("header lacks the descending arrow:\n%s", view)
	}
	if strings.Contains(view, "├─") || strings.Contains(view, "└─") {
		t.Errorf("a sorted list is flat, no branches:\n%s", view)
	}
	if line := lineOf(t, m, "tester"); !strings.Contains(line, "≥ 38") {
		t.Errorf("the partial Out keeps its ≥: %q", line)
	}
	if line := lineOf(t, m, "orchestrator"); !strings.Contains(line, "orchestrator") {
		t.Errorf("orchestrator row = %q", line)
	}
}

// A partial sub-agent ranks on its total alone.
func TestAPartialOutputDoesNotChangeTheRanking(t *testing.T) {
	for _, partial := range []int{1, 0} {
		m := atWideAgents(t, sortUsage(partial))
		m, _ = opened(t, m, ".")
		ids := strings.Split(visibleIDs(m), ",")
		if ids[1] != "a02" || ids[2] != "" {
			t.Errorf("partial=%d: order %v, the tester (3000) must rank above the orchestrator (1000)", partial, ids)
		}
	}
}

func TestDotCyclesTreeDescendingAscendingTree(t *testing.T) {
	m := atWideAgents(t, sortUsage(1))
	m, _ = opened(t, m, ".")
	m, _ = opened(t, m, ".")
	if got := visibleIDs(m); got != "a03,,a02,a01" {
		t.Errorf("ascending order = %q, want a03,,a02,a01", got)
	}
	if !strings.Contains(m.View(), "Total ▲") {
		t.Errorf("header lacks the ascending arrow:\n%s", m.View())
	}
	m, _ = opened(t, m, ".")
	if got := visibleIDs(m); got != ",a01,a02,a03" {
		t.Errorf("third press = %q, want the tree back", got)
	}
	if col, _ := m.agentTable.SortState(); col != -1 {
		t.Errorf("sort column = %d, want -1 (unsorted)", col)
	}
	view := m.View()
	for _, want := range []string{"├─ coder", "└─ reviewer"} {
		if !strings.Contains(view, want) {
			t.Errorf("the tree must come back with %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "▲") || strings.Contains(view, "▼") {
		t.Errorf("no arrow once unsorted:\n%s", view)
	}
}

// Equal totals keep ID order, descending and ascending alike.
func TestTiesKeepIDOrderInBothDirections(t *testing.T) {
	u := agentusage.Usage{Agents: []agentusage.Agent{
		totalled(orchestrator(0, 0), 100, 0),
		totalled(subAgent("a01", "x", 0, 0), 500, 0),
		totalled(subAgent("a02", "x", 0, 0), 500, 0),
		totalled(subAgent("a03", "x", 0, 0), 900, 0),
	}}
	m := atWideAgents(t, u)
	m, _ = opened(t, m, ".")
	if got := visibleIDs(m); got != "a03,a01,a02," {
		t.Errorf("descending = %q, want a03,a01,a02,", got)
	}
	m, _ = opened(t, m, ".")
	if got := visibleIDs(m); got != ",a01,a02,a03" {
		t.Errorf("ascending = %q, want ,a01,a02,a03", got)
	}
}

// Criterion 2 on screen: 543 / 326 / 21 tenths, the orchestrator taking the rest.
func TestSharesAddUpAndSortDoesNotChangeThem(t *testing.T) {
	want := map[string]string{"coder": "54.3%", "tester": "32.6%", "reviewer": "2.1%", "orchestrator": "11.0%"}
	m := atWideAgents(t, sortUsage(1))
	for _, sorted := range []bool{false, true} {
		sum := 0
		for _, r := range m.agentTable.Visible() {
			sum += r.share
		}
		if sum != shareScale {
			t.Errorf("sorted=%v: shares add up to %d tenths, want %d", sorted, sum, shareScale)
		}
		for role, pct := range want {
			if line := lineOf(t, m, role); !strings.Contains(line, pct) {
				t.Errorf("sorted=%v: %s line lacks %s: %q", sorted, role, pct, line)
			}
		}
		m, _ = opened(t, m, ".")
	}
}

func TestAFilterKeepsTheShareOfTheWholeSession(t *testing.T) {
	m := atWideAgents(t, sortUsage(1))
	m = send(t, m, testutil.Key("/"))
	for _, k := range testutil.Type("coder") {
		m = feed(t, m, k)
	}
	if n := len(m.agentTable.Visible()); n != 1 {
		t.Fatalf("%d rows after filtering, want 1", n)
	}
	if line := lineOf(t, m, "coder"); !strings.Contains(line, "54.3%") {
		t.Errorf("a filter does not change a share: %q", line)
	}
	if got := headerValue(t, m, "Tokens"); got != "5.0k" {
		t.Errorf("Tokens header = %q, want 5.0k", got)
	}
}

func TestDotWhileTheParseIsInFlightIsHarmless(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	m, _ = opened(t, m, "right")
	m, _ = opened(t, m, ".")
	m = answer(t, m, sortUsage(1))
	if got := visibleIDs(m); got != "a01,a02,,a03" {
		t.Errorf("the answer must arrive sorted, got %q", got)
	}
	if strings.Contains(m.View(), "├─") {
		t.Error("no branches in a sorted answer")
	}
}

// H8: the sort belongs to the table, not to a session.
func TestTheSortSurvivesAnotherSessionAndARefresh(t *testing.T) {
	m := atWideAgents(t, sortUsage(1))
	m, _ = opened(t, m, ".")
	m, _ = opened(t, m, "esc")
	m, _ = opened(t, m, "down")
	m, _ = opened(t, m, "right")
	m = answer(t, m, sortUsage(1))
	if got := visibleIDs(m); got != "a01,a02,,a03" {
		t.Errorf("another session opens sorted, got %q", got)
	}
	if strings.Contains(m.View(), "├─") || strings.Contains(m.View(), "└─") {
		t.Error("the other session must open flat")
	}
	m, _ = opened(t, m, "ctrl+r")
	m = answer(t, m, sortUsage(1))
	if got := visibleIDs(m); got != "a01,a02,,a03" {
		t.Errorf("ctrl+r keeps the sort, got %q", got)
	}
	if strings.Contains(m.View(), "├─") {
		t.Error("ctrl+r must not bring the branches back under an active sort")
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

func TestTheBasicFixtureSortsByTotalTokens(t *testing.T) {
	m := withSessions(t, fixtureParent)
	m = feed(t, m, testutil.Resize(120, 20))
	m, cmd := opened(t, m, "enter")
	got := usageMsgs(batchMsgs(t, cmd))
	if len(got) != 1 || got[0].err != nil {
		t.Fatalf("the fixture must parse cleanly, got %+v", got)
	}
	m = feed(t, m, got[0])
	m, _ = opened(t, m, ".")

	rows := m.agentTable.Visible()
	totals := make(map[string]int64)
	for _, a := range m.usage.Agents {
		totals[a.ID] = a.Tokens.Total()
	}
	if len(rows) != len(totals) {
		t.Fatalf("%d rows, want %d", len(rows), len(totals))
	}
	sum := 0
	for i, r := range rows {
		sum += r.share
		if i > 0 && totals[r.ID] > totals[rows[i-1].ID] {
			t.Errorf("row %d (%d tokens) outranks row %d (%d)", i, totals[r.ID], i-1, totals[rows[i-1].ID])
		}
	}
	if sum != shareScale {
		t.Errorf("shares add up to %d, want %d", sum, shareScale)
	}
	if line := lineOf(t, m, "tester"); !strings.Contains(line, "≥") {
		t.Errorf("the tester row keeps its ≥: %q", line)
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
