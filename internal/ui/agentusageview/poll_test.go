package agentusageview

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
)

// Live updates, step 3 (DEVDESK-45): opening a session, its answer and ctrl+r
// all go through the Aggregate. The poll tick (step 4) and Leave (step 5) are
// not covered here.

const logTag = "ERROR [agentusageview]"

// openedFixture opens a copy of the basic fixture and returns the model, the
// copy's parent transcript, and the Cmd the open returned.
func openedFixture(t *testing.T) (Model, string, tea.Cmd) {
	t.Helper()
	parent := copyFixture(t)
	m := withSessions(t, parent)
	m, cmd := opened(t, m, "right")
	return m, parent, cmd
}

// firstDelta runs a Cmd and returns the single usageDeltaMsg it produced.
func firstDelta(t *testing.T, cmd tea.Cmd) usageDeltaMsg {
	t.Helper()
	got := usageMsgs(batchMsgs(t, cmd))
	if len(got) != 1 {
		t.Fatalf("want exactly one usageDeltaMsg, got %d: %+v", len(got), got)
	}
	return got[0]
}

// atFixture opens a fixture copy and delivers the answer to the open.
func atFixture(t *testing.T) (Model, string) {
	t.Helper()
	m, parent, cmd := openedFixture(t)
	return feed(t, m, firstDelta(t, cmd)), parent
}

// captureLog redirects the standard logger for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// mainOutput is the orchestrator's output token count on screen.
func mainOutput(t *testing.T, m Model) int64 {
	t.Helper()
	for _, a := range m.usage.Agents {
		if a.Main {
			return a.Tokens.Output
		}
	}
	t.Fatal("no orchestrator in the usage")
	return 0
}

// ── Opening ──────────────────────────────────────────────────────────────────

func TestOpeningReadsTheSessionThroughTheAggregate(t *testing.T) {
	m, _, cmd := openedFixture(t)

	if m.agg == nil {
		t.Fatal("opening must create the session's Aggregate")
	}
	if !m.reading || !m.agentsLoading || m.fullPending {
		t.Errorf("reading=%v agentsLoading=%v fullPending=%v, want true, true, false",
			m.reading, m.agentsLoading, m.fullPending)
	}
	msg := firstDelta(t, cmd)
	if msg.gen != m.agentGen {
		t.Errorf("gen = %d, want %d", msg.gen, m.agentGen)
	}
	if msg.err != nil || !msg.delta.Full() {
		t.Fatalf("the first read must be Full and clean, err=%v full=%v", msg.err, msg.delta.Full())
	}

	m = feed(t, m, msg)
	if m.reading || m.agentsLoading {
		t.Errorf("reading=%v agentsLoading=%v after the answer, want false, false", m.reading, m.agentsLoading)
	}
	view := m.View()
	for _, want := range []string{"orchestrator", "coder", "tester", "reviewer"} {
		if !strings.Contains(view, want) {
			t.Errorf("the basic fixture's %q is missing:\n%s", want, view)
		}
	}
}

func TestTheAnswerMatchesAFullParse(t *testing.T) {
	m, parent := atFixture(t)
	files, err := agentusage.LocateSession(parent)
	if err != nil {
		t.Fatal(err)
	}
	want, err := agentusage.ParseSession(files)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%v", m.usage.Total()) != fmt.Sprintf("%v", want.Total()) {
		t.Errorf("total on screen %v, ParseSession says %v", m.usage.Total(), want.Total())
	}
	if len(m.usage.Agents) != len(want.Agents) {
		t.Errorf("%d agents, ParseSession says %d", len(m.usage.Agents), len(want.Agents))
	}
}

// ── Which answers are accepted ───────────────────────────────────────────────

func TestAnAnswerWhileNoReadIsInFlightIsDropped(t *testing.T) {
	m, _, cmd := openedFixture(t)
	msg := firstDelta(t, cmd)
	m = feed(t, m, msg)
	before := m.View()

	updated, next := m.Update(msg) // a duplicate: reading is false
	got := updated.(Model)
	if next != nil {
		t.Error("a dropped answer must not start anything")
	}
	if got.View() != before || got.reading {
		t.Error("a duplicate answer must leave the model unchanged")
	}
}

func TestAnAnswerOfAnotherGenerationDoesNotEndTheRead(t *testing.T) {
	m, _, cmd := openedFixture(t)
	msg := firstDelta(t, cmd)
	msg.gen--
	m = feed(t, m, msg)
	if !m.reading || !m.agentsLoading {
		t.Error("a stale answer must leave the read in flight")
	}
}

func TestAnErrorAnswerWhileNoReadIsInFlightIsDropped(t *testing.T) {
	buf := captureLog(t)
	m, _ := atFixture(t)
	m = feed(t, m, usageDeltaMsg{gen: m.agentGen, err: errors.New("late")})
	if m.agentsErr || strings.Contains(buf.String(), logTag) {
		t.Error("an unsolicited error answer must be ignored, and not logged")
	}
}

func TestADeltaThatDoesNotFollowTheCursorIsReReadAtOnce(t *testing.T) {
	buf := captureLog(t)
	m, _, cmd := openedFixture(t)
	msg := firstDelta(t, cmd)
	m = feed(t, m, msg)
	before := m.View()

	// Same gen, same delta, read from a cursor the aggregate has moved past.
	m.reading = true
	updated, next := m.Update(msg)
	m = updated.(Model)

	if m.View() != before {
		t.Error("a stale delta must not change what is on screen")
	}
	if !m.reading {
		t.Error("the re-read is in flight: reading stays true")
	}
	again := firstDelta(t, next)
	if again.gen != m.agentGen || again.err != nil {
		t.Errorf("re-read gen=%d err=%v, want gen %d and no error", again.gen, again.err, m.agentGen)
	}
	if again.delta.Full() {
		t.Error("the re-read starts from the current cursor, it is not a full read")
	}
	if n := strings.Count(buf.String(), logTag); n != 1 {
		t.Errorf("want one log line for the stale delta, got %d:\n%s", n, buf.String())
	}
}

// ── Errors ───────────────────────────────────────────────────────────────────

func TestAReadErrorKeepsTheRowsAndSchedulesARetry(t *testing.T) {
	m, _ := atFixture(t)
	m.reading = true
	updated, next := m.Update(usageDeltaMsg{gen: m.agentGen, err: errors.New("boom")})
	m = updated.(Model)
	if len(ticksOf(runCmd(t, next))) != 1 {
		t.Error("a transient error answer must schedule the next tick (step 4)")
	}
	if !m.agentsErr || m.agentsLoading || m.reading {
		t.Errorf("agentsErr=%v agentsLoading=%v reading=%v, want true, false, false", m.agentsErr, m.agentsLoading, m.reading)
	}
	if !strings.Contains(m.View(), "├─ coder") {
		t.Error("rows already shown stay after a failure")
	}
	if !strings.Contains(footer(m), "Failed to read this session") {
		t.Errorf("footer should report the failure:\n%s", footer(m))
	}
}

func TestOnlyTheFirstFailureOfAStreakIsLogged(t *testing.T) {
	buf := captureLog(t)
	m, _ := atFixture(t)
	fail := func() {
		m.reading = true
		m = feed(t, m, usageDeltaMsg{gen: m.agentGen, err: errors.New("boom")})
	}
	succeed := func() {
		m.reading = true
		m = feed(t, m, runDelta(t, m.agentGen, m.agg.Cursor()))
	}

	fail()
	fail()
	if n := strings.Count(buf.String(), logTag); n != 1 {
		t.Fatalf("two failures in a row: %d log lines, want 1", n)
	}
	succeed()
	if m.agentsErr {
		t.Error("a successful read ends the failure state")
	}
	fail()
	if n := strings.Count(buf.String(), logTag); n != 2 {
		t.Errorf("a failure after a success is a new streak: %d log lines, want 2", n)
	}
}

// ── ctrl+r reads everything again ────────────────────────────────────────────

func TestCtrlRReadsFromTheStartWhatAnIncrementalReadCannotSee(t *testing.T) {
	m, parent := atFixture(t)
	before := mainOutput(t, m)

	// Rewrite the parent at the same size and mtime: an incremental read
	// cannot tell (DEVDESK-44 D4).
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(parent)
	if err != nil {
		t.Fatal(err)
	}
	// msg_p1 appears three times (output 5, 40, 120) and the last line wins,
	// so only the final value is visible to a full parse.
	edited := bytes.Replace(raw, []byte(`"output_tokens":120`), []byte(`"output_tokens":121`), 1)
	if bytes.Count(raw, []byte(`"output_tokens":120`)) != 1 || bytes.Equal(edited, raw) || len(edited) != len(raw) {
		t.Fatal("the fixture no longer holds the expected token count")
	}
	if err := os.WriteFile(parent, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(parent, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}

	if inc := runDelta(t, m.agentGen, m.agg.Cursor()); !inc.delta.Empty() {
		t.Fatal("precondition: the incremental read must not see a same-size rewrite")
	}

	m, cmd := opened(t, m, "ctrl+r")
	if !m.reading || !m.agentsLoading {
		t.Errorf("reading=%v agentsLoading=%v, want a full read in flight with its spinner", m.reading, m.agentsLoading)
	}
	if !strings.Contains(m.View(), "├─ coder") {
		t.Error("rows stay on screen during ctrl+r (Rule 139)")
	}
	msg := firstDelta(t, cmd)
	if msg.gen != m.agentGen || !msg.delta.Full() {
		t.Fatalf("ctrl+r must read everything again: gen=%d (want %d) full=%v", msg.gen, m.agentGen, msg.delta.Full())
	}

	m = feed(t, m, msg)
	if after := mainOutput(t, m); after == before {
		t.Errorf("orchestrator output still %d after ctrl+r, the rewrite was not picked up", after)
	}
	if m.agentsLoading || m.reading {
		t.Error("the answer ends the full read")
	}
}

// A Full answer always rebuilds the rows, whether or not it reports changes
// (handleUsageDelta: Full() || !Empty()).
func TestAFullAnswerRebuildsTheRowsEvenOnUnchangedFiles(t *testing.T) {
	m, _ := atFixture(t)
	m, cmd := opened(t, m, "ctrl+r")
	msg := firstDelta(t, cmd)
	if !msg.delta.Full() {
		t.Fatal("precondition: ctrl+r gives a Full delta")
	}
	m.agentTable.SetItems(nil) // stale rows the answer must replace
	m = feed(t, m, msg)
	if !strings.Contains(m.View(), "├─ coder") {
		t.Error("a Full answer must rebuild the rows")
	}
	if m.agentsLoading || m.reading {
		t.Error("the answer ends the full read")
	}
}

func TestCtrlRStartsANewGenerationWhenNothingIsInFlight(t *testing.T) {
	m, _ := atFixture(t)
	gen := m.agentGen
	m, cmd := opened(t, m, "ctrl+r")
	if m.agentGen != gen+1 || m.fullPending {
		t.Errorf("agentGen=%d fullPending=%v, want %d and false", m.agentGen, m.fullPending, gen+1)
	}
	if got := firstDelta(t, cmd); got.gen != gen+1 || !got.delta.Full() {
		t.Errorf("want a Full read of gen %d, got gen=%d full=%v", gen+1, got.gen, got.delta.Full())
	}
}

func TestCtrlRWhileAReadIsInFlightWaitsForIt(t *testing.T) {
	m, _, cmd := openedFixture(t)
	inFlight := firstDelta(t, cmd) // the open's read, not yet delivered
	gen := m.agentGen

	var next tea.Cmd
	for range 3 {
		m, next = opened(t, m, "ctrl+r")
		if got := usageMsgs(batchMsgs(t, next)); len(got) != 0 {
			t.Fatalf("ctrl+r during a read must not start a second one, got %+v", got)
		}
	}
	if m.agentGen != gen || !m.fullPending || !m.agentsLoading || !m.reading {
		t.Fatalf("agentGen=%d (want %d) fullPending=%v agentsLoading=%v reading=%v",
			m.agentGen, gen, m.fullPending, m.agentsLoading, m.reading)
	}

	// The in-flight answer is applied, and exactly one full read follows.
	updated, follow := m.Update(inFlight)
	m = updated.(Model)
	if !strings.Contains(m.View(), "├─ coder") {
		t.Error("the in-flight answer must still be shown")
	}
	again := firstDelta(t, follow)
	if again.gen != m.agentGen || !again.delta.Full() || again.err != nil {
		t.Errorf("the pending ctrl+r must read from the start: gen=%d full=%v err=%v", again.gen, again.delta.Full(), again.err)
	}
	if m.fullPending || !m.reading || !m.agentsLoading {
		t.Errorf("fullPending=%v reading=%v agentsLoading=%v, want false, true, true", m.fullPending, m.reading, m.agentsLoading)
	}

	m = feed(t, m, again)
	if m.reading || m.agentsLoading {
		t.Error("the full read ends the loading")
	}
}

// ── The spinner is for full reads only ───────────────────────────────────────

func TestAnIncrementalAnswerNeverShowsTheSpinner(t *testing.T) {
	m, _ := atFixture(t)
	m.reading = true // what a poll would have set; the tick itself is step 4
	delta := runDelta(t, m.agentGen, m.agg.Cursor())
	if delta.delta.Full() {
		t.Fatal("precondition: an incremental delta")
	}
	m = feed(t, m, delta)
	if m.agentsLoading {
		t.Error("an incremental read must not set agentsLoading")
	}
	if m.reading {
		t.Error("the answer ends the read")
	}
}

// ── Leaving the session ──────────────────────────────────────────────────────

func TestGoingBackDropsTheAggregateAndTheReadState(t *testing.T) {
	m, _, cmd := openedFixture(t)
	late := firstDelta(t, cmd)
	m, _ = opened(t, m, "ctrl+r") // fullPending, since the open's read is in flight
	if !m.fullPending {
		t.Fatal("precondition: a pending full read")
	}
	m, _ = opened(t, m, "esc")

	if m.agg != nil || m.reading || m.fullPending {
		t.Errorf("agg=%v reading=%v fullPending=%v, want nil, false, false", m.agg, m.reading, m.fullPending)
	}
	m = feed(t, m, late)
	if m.level != levelSessions || len(m.agentTable.Items()) != 0 {
		t.Error("a late answer must not reopen the agents")
	}
}

func TestOpeningAnotherSessionStartsFromAFreshAggregate(t *testing.T) {
	a, b := copyFixture(t), copyFixture(t)
	m := withSessions(t, a, b)
	m, cmdA := opened(t, m, "right")
	lateA := firstDelta(t, cmdA)
	aggA := m.agg
	m, _ = opened(t, m, "ctrl+r") // fullPending on A
	m, _ = opened(t, m, "esc")
	m, _ = opened(t, m, "down")
	m, cmdB := opened(t, m, "right")

	if m.agg == nil || m.agg == aggA {
		t.Error("another session needs its own Aggregate")
	}
	if m.fullPending || !m.reading || !m.agentsLoading {
		t.Errorf("fullPending=%v reading=%v agentsLoading=%v, want false, true, true", m.fullPending, m.reading, m.agentsLoading)
	}
	m = feed(t, m, lateA)
	if !m.agentsLoading || len(m.agentTable.Items()) != 0 {
		t.Error("A's answer must not fill B's table")
	}
	m = feed(t, m, firstDelta(t, cmdB))
	if len(m.agentTable.Items()) != 4 {
		t.Errorf("%d agents after B's answer, want 4", len(m.agentTable.Items()))
	}
}

// ── Privacy ──────────────────────────────────────────────────────────────────

func TestNothingOfTheTranscriptsContentLeaksThroughTheDeltaPath(t *testing.T) {
	buf := captureLog(t)
	m, parent, cmd := openedFixture(t)
	msg := firstDelta(t, cmd)
	m = feed(t, m, msg)
	m.reading = true
	m = feed(t, m, usageDeltaMsg{gen: m.agentGen, err: errors.New("boom")})

	var headers strings.Builder
	for _, h := range m.GetHeaderInfo("") {
		headers.WriteString(h.Key + "=" + h.Value + "\n")
	}
	surfaces := map[string]string{
		"View":   m.View(),
		"footer": footer(m),
		"header": headers.String(),
		"log":    buf.String(),
		"msg":    fmt.Sprintf("%+v %v %#v", msg, msg, msg),
		"model":  fmt.Sprintf("%v", m.agg),
	}
	for name, s := range surfaces {
		if strings.Contains(s, sentinel) {
			t.Errorf("%s carries the transcripts' content", name)
		}
		if strings.Contains(s, parent) {
			t.Errorf("%s carries the transcript path", name)
		}
	}
}
