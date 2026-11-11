package agentusageview

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/shared"
)

// Terminal focus (DEVDESK-210, step 3): a blurred terminal suspends the open
// session's poll chain (stopChain), the focus edge that follows a blur resumes
// it (handleResume), and a view entering the screen only clears the flag.

const notFocused = "paused (terminal not focused)"

func blur() shared.TerminalFocusMsg { return shared.TerminalFocusMsg{Focused: false} }

func refocus() shared.TerminalFocusMsg {
	return shared.TerminalFocusMsg{Focused: true, Resume: true}
}

func entry() shared.TerminalFocusMsg { return shared.TerminalFocusMsg{Focused: true} }

// focused delivers a TerminalFocusMsg and returns the model and the Cmd.
func focused(t *testing.T, m Model, msg shared.TerminalFocusMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

func spinnerTicks(msgs []tea.Msg) int {
	n := 0
	for _, m := range msgs {
		if _, ok := m.(spinner.TickMsg); ok {
			n++
		}
	}
	return n
}

// 1
func TestBlurOnALiveSessionPausesTheHeaderAndDropsTheOldChain(t *testing.T) {
	m, _ := atFixture(t)
	wantUpdates(t, "precondition", m, "live")
	oldGen := m.agentGen
	m, msgs := pollTick(t, m, oldGen) // a read is in flight
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("precondition: one read in flight, got %+v", msgs)
	}

	m, cmd := focused(t, m, blur())

	if cmd != nil {
		t.Error("a blur has nothing to run")
	}
	wantUpdates(t, "blurred", m, notFocused)
	if m.agentGen == oldGen {
		t.Error("the blur must start a new generation so the old chain dies")
	}
	if _, out := pollTick(t, m, oldGen); len(out) != 0 {
		t.Errorf("a tick of the previous generation must be dropped, got %+v", out)
	}
	_, out := answered(t, m, reads[0])
	if len(out) != 0 {
		t.Errorf("an answer of the previous generation must be dropped with no tick, got %+v", out)
	}
}

// 2
func TestFocusAfterABlurResumesWithOneSilentRead(t *testing.T) {
	m, parent := atFixture(t)
	before := mainOutput(t, m)
	m, _ = focused(t, m, blur())
	appendLine(t, parent, usageLine("msg_blur", "model-alpha", 2, 33))

	m, cmd := focused(t, m, refocus())

	wantUpdates(t, "refocused", m, "live")
	msgs := batchMsgs(t, cmd)
	reads := usageMsgs(msgs)
	if len(reads) != 1 || reads[0].delta.Full() || reads[0].gen != m.agentGen {
		t.Fatalf("want one incremental read of gen %d, got %+v", m.agentGen, msgs)
	}
	if n := spinnerTicks(msgs); n != 0 || m.agentsLoading {
		t.Errorf("a resume is silent: spinner ticks=%d loading=%t", n, m.agentsLoading)
	}
	m, _ = answered(t, m, reads[0])
	if got := mainOutput(t, m); got != before+33 {
		t.Errorf("output = %d, want %d: the line written while blurred", got, before+33)
	}
}

// 3
func TestBlurAndFocusOnASettledSessionReadOnceAndSettleAgain(t *testing.T) {
	m, _ := settledModel(t)
	wantUpdates(t, "precondition", m, "paused")

	m, _ = focused(t, m, blur())
	wantUpdates(t, "blurred", m, notFocused)

	m, cmd := focused(t, m, refocus())
	reads := usageMsgs(batchMsgs(t, cmd))
	if len(reads) != 1 {
		t.Fatalf("a settled session is read once on focus, got %+v", reads)
	}
	m, msgs := answered(t, m, reads[0])
	if len(msgs) != 0 {
		t.Errorf("still quiet: the chain stops again, got %+v", msgs)
	}
	wantUpdates(t, "settled again", m, "paused")
}

// 4
func TestAFullReadInFlightAtBlurResumesAsAFullReadWithASpinner(t *testing.T) {
	m, _ := atFixture(t)
	m, _ = opened(t, m, "ctrl+r") // full read in flight
	if !m.reading || !m.agentsLoading {
		t.Fatalf("precondition: reading=%t loading=%t", m.reading, m.agentsLoading)
	}

	m, _ = focused(t, m, blur())
	if m.reading || m.agentsLoading {
		t.Errorf("the lost read must not block anything: reading=%t loading=%t", m.reading, m.agentsLoading)
	}
	m, cmd := focused(t, m, refocus())

	if !m.agentsLoading {
		t.Error("a resumed full read shows its spinner")
	}
	msgs := batchMsgs(t, cmd)
	reads := usageMsgs(msgs)
	if len(reads) != 1 || !reads[0].delta.Full() {
		t.Fatalf("the resume must start one full read, got %+v", msgs)
	}
	if spinnerTicks(msgs) == 0 {
		t.Error("the spinner must tick with the full read")
	}
}

// 5
func TestFocusOnEntryOnlyClearsTheFlagAndInitResumesTheRead(t *testing.T) {
	m, _ := atFixture(t)
	m, _ = focused(t, m, blur())

	m, cmd := focused(t, m, entry())

	if cmd != nil {
		t.Error("an entry focus message never starts a read")
	}
	m, cmd = resumed(t, m)
	if reads := usageMsgs(batchMsgs(t, cmd)); len(reads) != 1 {
		t.Fatalf("Init's resume must start one read once the flag is clear, got %+v", reads)
	}
	wantUpdates(t, "after entry and resume", m, "live")
}

// 6
func TestResumeMessageWhileBlurredIsDropped(t *testing.T) {
	m, _ := atFixture(t)
	m, _ = focused(t, m, blur())
	gen := m.agentGen

	next, cmd := resumed(t, m)

	if cmd != nil || next.reading || next.agentGen != gen {
		t.Errorf("a blurred view must not resume: cmd=%t reading=%t gen %d -> %d", cmd != nil, next.reading, gen, next.agentGen)
	}
	wantUpdates(t, "still blurred", next, notFocused)
}

// 7
func TestFocusEdgesAtTheSessionsLevelDoNothing(t *testing.T) {
	m := withSessions(t, "/p/a.jsonl")
	gen := m.agentGen

	m, cmd := focused(t, m, blur())
	if cmd != nil {
		t.Error("blur at the sessions level: no Cmd")
	}
	m, cmd = focused(t, m, refocus())
	if cmd != nil || m.agentGen != gen {
		t.Errorf("focus at the sessions level: the list is not live, cmd=%t gen %d -> %d", cmd != nil, gen, m.agentGen)
	}
	if v, ok := updatesValue(m); ok {
		t.Errorf("the sessions level has no Updates entry, got %q", v)
	}
}

// 8
func TestFocusEdgesLeaveNoFooterMessage(t *testing.T) {
	m, _ := atFixture(t)
	before := footerText(m)

	m, _ = focused(t, m, blur())
	if got := footerText(m); got != before {
		t.Errorf("footer changed on blur:\n%s\nwant\n%s", got, before)
	}
	m, _ = focused(t, m, refocus())
	if got := footerText(m); got != before {
		t.Errorf("footer changed on focus:\n%s\nwant\n%s", got, before)
	}
}
