package agentusageview

import (
	"bytes"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Live updates, step 5 (DEVDESK-45, DEVDESK-79): the view stops its chain when
// the router leaves it and picks it up again when Init resumes it. The router
// calls Leave through app.LeavingView from switchView and switchContext, and
// never when the view already on screen is named again.

// leaver is app.LeavingView, restated here: internal/app imports this package.
type leaver interface {
	Leave() (tea.Model, tea.Cmd, bool)
}

// leave calls Leave the way the router does and returns the settled view. A
// view without the method fails the test where it is used, not at compile time.
func leave(t *testing.T, m Model) (Model, tea.Cmd, bool) {
	t.Helper()
	l, ok := tea.Model(m).(leaver)
	if !ok {
		t.Fatal("Model must implement Leave() (tea.Model, tea.Cmd, bool)")
	}
	settled, cmd, allowed := l.Leave()
	next, ok := settled.(Model)
	if !ok {
		t.Fatalf("Leave returned %T, want agentusageview.Model", settled)
	}
	return next, cmd, allowed
}

// resumed delivers agentsResumeMsg and returns the model and the Cmd.
func resumed(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(agentsResumeMsg{})
	return updated.(Model), cmd
}

func TestLeaveAtTheAgentsLevelStopsTheChainAndNeverRefuses(t *testing.T) {
	m, _ := atFixture(t)
	m, _ = pollTick(t, m, m.agentGen) // a read is now in flight
	if !m.reading {
		t.Fatal("precondition: a read is in flight")
	}
	gen := m.agentGen

	left, cmd, ok := leave(t, m)

	if !ok {
		t.Error("Leave must never refuse")
	}
	if cmd != nil {
		t.Errorf("Leave has nothing to say, got a Cmd")
	}
	if left.agentGen != gen+1 {
		t.Errorf("agentGen = %d, want %d", left.agentGen, gen+1)
	}
	if left.reading || left.fullPending {
		t.Errorf("a lost read must not block anything: reading=%t fullPending=%t", left.reading, left.fullPending)
	}
	if left.level != levelAgents || left.agg == nil {
		t.Error("Leave keeps the open session: coming back finds it")
	}
}

func TestAnythingOfTheGenerationLeftBehindIsIgnoredAfterLeave(t *testing.T) {
	m, parent := atFixture(t)
	appendLine(t, parent, usageLine("msg_away", "model-alpha", 1, 7))
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("precondition: one read in flight, got %+v", msgs)
	}
	stale := m.agentGen
	before := mainOutput(t, m)

	left, _, _ := leave(t, m)

	next, out := answered(t, left, reads[0])
	if len(out) != 0 || mainOutput(t, next) != before {
		t.Errorf("an answer of gen %d must be dropped, got %+v", stale, out)
	}
	if _, out := pollTick(t, left, stale); len(out) != 0 {
		t.Errorf("a tick of gen %d must be dropped, got %+v", stale, out)
	}
}

func TestLeaveAtTheSessionsLevelChangesNothing(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")

	left, cmd, ok := leave(t, m)

	if !ok || cmd != nil {
		t.Errorf("ok=%t cmd=%v, want true and no Cmd", ok, cmd != nil)
	}
	if left.agentGen != m.agentGen || left.gen != m.gen || left.level != levelSessions {
		t.Error("the sessions level has no chain: nothing to bump")
	}
}

// The review scenario: a read lost with the view must not leave ctrl+r waiting
// for an answer that will never come.
func TestCtrlRStartsAReadAfterTheViewWasLeftWhileOneWasInFlight(t *testing.T) {
	m, _, cmd := openedFixture(t)
	_ = cmd // the open's answer never arrives: the view was left first
	if !m.reading {
		t.Fatal("precondition: the open's read is in flight")
	}
	m, _, _ = leave(t, m)

	m, cmd = opened(t, m, "ctrl+r")

	reads := usageMsgs(batchMsgs(t, cmd))
	if len(reads) != 1 || !reads[0].delta.Full() || reads[0].gen != m.agentGen {
		t.Fatalf("ctrl+r must start one full read of the current gen, got %+v", reads)
	}
	if m.fullPending {
		t.Error("nothing was in flight to wait for: no pending full read")
	}
}

// The plan keeps a ctrl+r typed before Leave: the resume turns it into a full
// read, so the refresh the user asked for is not lost.
func TestACtrlRWaitingOnALostReadIsKeptByLeaveAndResumesAsAFullRead(t *testing.T) {
	m, _, _ := openedFixture(t)
	m, _ = opened(t, m, "ctrl+r") // read in flight: the full read is queued
	if !m.fullPending {
		t.Fatal("precondition: ctrl+r is queued behind the read")
	}

	left, _, _ := leave(t, m)

	if !left.fullPending || left.reading {
		t.Errorf("reading=%t fullPending=%t, want false and true", left.reading, left.fullPending)
	}
	if left.agentsLoading {
		t.Error("no read is in flight: the footer must not keep a loading status")
	}
	back, cmd := resumed(t, left)
	reads := usageMsgs(batchMsgs(t, cmd))
	if len(reads) != 1 || !reads[0].delta.Full() {
		t.Fatalf("the resume must start one full read, got %+v", reads)
	}
	if back.fullPending {
		t.Error("the full read consumed the pending refresh")
	}
	if !back.agentsLoading {
		t.Error("a resumed full read shows its spinner")
	}
}

// rewriteSameSize changes the orchestrator's last output count without moving
// the size or the mtime, which an incremental read cannot see (DEVDESK-44 D4).
func rewriteSameSize(t *testing.T, parent string) {
	t.Helper()
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(parent)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(raw, []byte(`"output_tokens":120`), []byte(`"output_tokens":121`), 1)
	if bytes.Equal(edited, raw) || len(edited) != len(raw) {
		t.Fatal("the fixture no longer holds the expected token count")
	}
	if err := os.WriteFile(parent, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(parent, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
}

// A ctrl+r with no read in flight starts a full read at once, so nothing is
// pending; leaving before its answer must not turn it into an incremental read
// on return, or a same-size rewrite stays invisible.
func TestACtrlRInFlightWhenTheViewIsLeftResumesAsAFullRead(t *testing.T) {
	m, parent := atFixture(t)
	before := mainOutput(t, m)
	rewriteSameSize(t, parent)
	m, _ = opened(t, m, "ctrl+r") // full read in flight; its answer never arrives
	if !m.reading || !m.agentsLoading || m.fullPending {
		t.Fatalf("precondition: reading=%t loading=%t pending=%t", m.reading, m.agentsLoading, m.fullPending)
	}
	m, _, _ = leave(t, m)

	m, cmd := resumed(t, m)

	if !m.agentsLoading {
		t.Error("the resumed full read shows its spinner")
	}
	reads := usageMsgs(batchMsgs(t, cmd))
	if len(reads) != 1 || !reads[0].delta.Full() {
		t.Fatalf("the resume must start one full read, got %+v", reads)
	}
	m, _ = answered(t, m, reads[0])
	if after := mainOutput(t, m); after == before {
		t.Errorf("output still %d: the same-size rewrite stayed invisible", after)
	}
	if m.agentsLoading || m.reading {
		t.Error("the answer ends the full read")
	}
}

// The open's first read is full (fresh cursor) and shows a spinner; leaving
// before its answer must keep both on return.
func TestAnOpenLeftBeforeItsFirstAnswerResumesWithAFullReadAndASpinner(t *testing.T) {
	m, _, _ := openedFixture(t)
	if !m.reading || !m.agentsLoading {
		t.Fatal("precondition: the open's read is in flight with its spinner")
	}
	m, _, _ = leave(t, m)

	m, cmd := resumed(t, m)

	if !m.agentsLoading {
		t.Error("the table is empty until the answer: the spinner must show")
	}
	reads := usageMsgs(batchMsgs(t, cmd))
	if len(reads) != 1 || !reads[0].delta.Full() {
		t.Fatalf("the resume must start one full read, got %+v", reads)
	}
	m, _ = answered(t, m, reads[0])
	if m.agentsLoading || len(m.usage.Agents) == 0 {
		t.Errorf("loading=%t agents=%d, want rows and no spinner", m.agentsLoading, len(m.usage.Agents))
	}
}

func TestResumeReadsWhatChangedWhileAwayWithoutASpinner(t *testing.T) {
	m, parent := atFixture(t)
	before := mainOutput(t, m)
	m, _, _ = leave(t, m)
	appendLine(t, parent, usageLine("msg_away", "model-alpha", 3, 41))
	gen := m.agentGen

	m, cmd := resumed(t, m)

	if m.agentGen != gen+1 {
		t.Errorf("agentGen = %d, want %d", m.agentGen, gen+1)
	}
	if m.agentsLoading {
		t.Error("a resume is silent: no spinner")
	}
	reads := usageMsgs(batchMsgs(t, cmd))
	if len(reads) != 1 || reads[0].delta.Full() || reads[0].gen != m.agentGen {
		t.Fatalf("want one incremental read of gen %d, got %+v", m.agentGen, reads)
	}
	m, msgs := answered(t, m, reads[0])
	if got := mainOutput(t, m); got != before+41 {
		t.Errorf("output = %d, want %d: the line written while away", got, before+41)
	}
	ticks := ticksOf(msgs)
	if len(ticks) != 1 {
		t.Fatalf("a live session goes on polling, got %+v", msgs)
	}
	if _, again := pollTick(t, m, ticks[0].gen); len(usageMsgs(again)) != 1 {
		t.Errorf("the chain must go on after the resume, got %+v", again)
	}
}

func TestResumeOutsideTheAgentsLevelDoesNothing(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")

	next, cmd := resumed(t, m)

	if cmd != nil || next.agentGen != m.agentGen {
		t.Errorf("no session open: nothing to resume, got a Cmd=%t gen %d", cmd != nil, next.agentGen)
	}
}

// Leave, then a quick return: the tick that was pending before leaving arrives
// after the resume and must not start a second chain.
func TestAQuickReturnLeavesExactlyOneChain(t *testing.T) {
	m, _ := atFixture(t)
	oldTick := m.agentGen
	m, _, _ = leave(t, m)
	m, cmd := resumed(t, m)
	reads := usageMsgs(batchMsgs(t, cmd))
	if len(reads) != 1 {
		t.Fatalf("the resume starts one read, got %+v", reads)
	}

	// The old tick lands while the resume's read is in flight, and again after.
	m, msgs := pollTick(t, m, oldTick)
	if len(msgs) != 0 {
		t.Errorf("a tick of the gen before Leave must be ignored, got %+v", msgs)
	}
	m, msgs = answered(t, m, reads[0])
	if len(ticksOf(msgs)) != 1 {
		t.Fatalf("one tick follows the resume's answer, got %+v", msgs)
	}
	if _, msgs = pollTick(t, m, oldTick); len(msgs) != 0 {
		t.Errorf("the old tick is still ignored after the answer, got %+v", msgs)
	}
}

// `:au` typed again does not call Leave: the second resume must kill the chain
// the first one started.
func TestAResumeTwiceInARowKeepsTheLastChainOnly(t *testing.T) {
	m, _ := atFixture(t)
	m, first := resumed(t, m)
	firstRead := usageMsgs(batchMsgs(t, first))
	m, second := resumed(t, m)
	secondRead := usageMsgs(batchMsgs(t, second))
	if len(firstRead) != 1 || len(secondRead) != 1 {
		t.Fatalf("each resume starts a read: %+v / %+v", firstRead, secondRead)
	}

	m, msgs := answered(t, m, firstRead[0])
	if len(msgs) != 0 {
		t.Errorf("the first chain's answer must be dropped, got %+v", msgs)
	}
	if !m.reading {
		t.Error("the second chain's read is still awaited")
	}
	_, msgs = answered(t, m, secondRead[0])
	if len(ticksOf(msgs)) != 1 {
		t.Errorf("the second chain goes on, got %+v", msgs)
	}
}

func TestResumeOnASettledSessionReadsOnceThenStopsAgainIfStillQuiet(t *testing.T) {
	parent := copyFixture(t)
	finishTester(t, parent)
	ageAll(t, parent, 10*quietPeriod)
	m := withSessions(t, parent)
	m, cmd := opened(t, m, "right")
	m, msgs := answered(t, m, firstDelta(t, cmd))
	if len(msgs) != 0 {
		t.Fatalf("precondition: the chain stopped, got %+v", msgs)
	}

	m, cmd = resumed(t, m)
	reads := usageMsgs(batchMsgs(t, cmd))
	if len(reads) != 1 {
		t.Fatalf("a settled session is read once on resume, got %+v", reads)
	}
	_, msgs = answered(t, m, reads[0])
	if len(msgs) != 0 {
		t.Errorf("still quiet: the chain stops again, got %+v", msgs)
	}
}

func TestResumeOnASettledSessionThatGrewGoesLiveAgain(t *testing.T) {
	parent := copyFixture(t)
	finishTester(t, parent)
	ageAll(t, parent, 10*quietPeriod)
	m := withSessions(t, parent)
	m, cmd := opened(t, m, "right")
	m, _ = answered(t, m, firstDelta(t, cmd))
	m, _, _ = leave(t, m)
	appendLine(t, parent, usageLine("msg_back", "model-alpha", 1, 2))

	m, cmd = resumed(t, m)
	m, msgs := answered(t, m, firstDelta(t, cmd))

	ticks := ticksOf(msgs)
	if len(ticks) != 1 {
		t.Fatalf("a fresh write brings the chain back, got %+v", msgs)
	}
	if _, again := pollTick(t, m, ticks[0].gen); len(usageMsgs(again)) != 1 {
		t.Errorf("the tick of the revived chain must start a read, got %+v", again)
	}
}
