package netdiag

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-711 (S-02b): on Linux and macOS K asks the process to stop first
// (SIGTERM), watches it for up to 5 s without blocking the screen, and only a
// second confirmation sends SIGKILL. The machine lives in PortsModel.stop; every
// step is a message carrying the generation of its stop, so a tick that outlives
// its stop touches nothing.
//
// No test here reaches ports.Terminate, ports.Probe or ports.Kill: every model
// installs a fakeProcs, and the probe ticks are never waited for. The messages a
// tick or a signal would deliver are built here, with the generation read from
// the model and the instants chosen by the test (stopT0 plus an offset), so no
// test sleeps. A test that drains a Cmd shortens the footer timer first
// (testutil.FastTimers, Rule 128), and none drains a probe tick except the
// Rule 110 one, which shortens stopProbeEvery too.

// stopT0 is the instant the SIGTERM call returned in these tests: the deadline
// is stopT0 plus the 5 s grace.
var stopT0 = time.Unix(1_700_000_000, 0)

// The sentences of the story, written out in full so that a drift in the
// constants of this package fails a test instead of following them.
const (
	stoppedNginx        = "nginx (812) stopped"
	stoppedWaitingNginx = "Stopped waiting for nginx (812) — the stop request was already sent"
	notForcedNginx      = "nginx (812) left running — no SIGKILL sent"
	forceBody           = "nginx (PID 812) did not exit within 5 s. Force kill (SIGKILL)? It gets no chance to shut down cleanly."
	stopBody            = "Ask nginx (PID 812) to stop? Sends SIGTERM and waits up to 5 s. You can force it afterwards."
	anotherStopText     = "Another stop is already in progress"
	stopPendingHereText = "This process is already being stopped"
)

// stopRows are three listed processes; the cursor starts on nginx (PID 812).
func stopRows() []ports.Socket {
	return []ports.Socket{sock("812", "nginx"), sock("4242", "node"), sock("1204", "sshd")}
}

// stopModelWith is a Linux model whose cursor is on rows[0] and whose stop
// confirmation is open, at the given width.
func stopModelWith(t *testing.T, width int, rows []ports.Socket) (*Model, *fakeProcs) {
	t.Helper()
	m, procs := identityModel(t, false, rows...)
	m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: 40})
	m = feed(t, m, testutil.Key(keymap.Kill))
	if m.portsModel.confirmModal == nil {
		t.Fatalf("setup: K on %s did not open the confirmation", rows[0].Process)
	}
	return m, procs
}

// stopModel is stopModelWith on nginx, wide.
func stopModel(t *testing.T) (*Model, *fakeProcs) {
	t.Helper()
	return stopModelWith(t, 160, stopRows())
}

// signalling answers Yes: the SIGTERM is in flight. It returns the command Yes
// returned, which is the SIGTERM itself and has not been run.
func signalling(t *testing.T) (*Model, tea.Cmd, *fakeProcs) {
	t.Helper()
	m, procs := stopModel(t)
	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	if m.portsModel.stop == nil || m.portsModel.stop.phase != stopSignalling {
		t.Fatalf("setup: after Yes the stop is %+v, want phase stopSignalling", m.portsModel.stop)
	}
	return m, cmd, procs
}

// sent is signalling with the SIGTERM actually run against the fake, so the
// fake has recorded the call a real SIGTERM would have been. The result the run
// produced is dropped: the tests feed their own, with an instant they chose.
func sent(t *testing.T) (*Model, *fakeProcs) {
	t.Helper()
	m, cmd, procs := signalling(t)
	runTerm(t, cmd)
	return m, procs
}

// termDelivered feeds the result of the SIGTERM of the stop in progress, which
// returned at instant at.
func termDelivered(t *testing.T, m *Model, at time.Time) (*Model, tea.Cmd) {
	t.Helper()
	s := m.portsModel.stop
	if s == nil {
		t.Fatal("setup: no stop in progress to deliver a SIGTERM result to")
	}
	return step(t, m, portsTermResultMsg{gen: s.gen, pid: s.target.PID, process: s.target.Process, at: at})
}

// waiting is a model whose SIGTERM was delivered at stopT0: the wait has begun.
func waiting(t *testing.T) (*Model, *fakeProcs) {
	t.Helper()
	m, procs := sent(t)
	m, _ = termDelivered(t, m, stopT0)
	if m.portsModel.stop == nil || m.portsModel.stop.phase != stopWaiting {
		t.Fatalf("setup: the stop is %+v, want phase stopWaiting", m.portsModel.stop)
	}
	return m, procs
}

// probeAt feeds what the tick at instant at read: the stop in progress is
// probed and found in state l.
func probeAt(t *testing.T, m *Model, at time.Time, l ports.Liveness, err error) (*Model, tea.Cmd) {
	t.Helper()
	s := m.portsModel.stop
	if s == nil {
		t.Fatal("setup: no stop in progress to probe")
	}
	return step(t, m, portsStopProbeMsg{gen: s.gen, at: at, liveness: l, err: err})
}

// afterGrace is the instant of the first tick that finds the grace used up.
var afterGrace = stopT0.Add(5 * time.Second)

// askingToForce is a model whose process still ran at the deadline: the second
// confirmation is open.
func askingToForce(t *testing.T) (*Model, *fakeProcs) {
	t.Helper()
	m, procs := waiting(t)
	m, _ = probeAt(t, m, afterGrace, ports.ProcessRunning, nil)
	if m.portsModel.stop == nil || m.portsModel.stop.phase != stopAskingForce || m.portsModel.confirmModal == nil {
		t.Fatalf("setup: the stop is %+v with modal %v, want the second confirmation open", m.portsModel.stop, m.portsModel.confirmModal != nil)
	}
	return m, procs
}

// forcing answers Yes to the second confirmation: the SIGKILL is in flight. It
// returns the SIGKILL command, not run.
func forcing(t *testing.T) (*Model, tea.Cmd, *fakeProcs) {
	t.Helper()
	m, procs := askingToForce(t)
	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	if m.portsModel.stop == nil || m.portsModel.stop.phase != stopForcing {
		t.Fatalf("setup: after the second Yes the stop is %+v, want phase stopForcing", m.portsModel.stop)
	}
	return m, cmd, procs
}

// busyOf is the label the row of pid carries, or "" when it is not busy.
func busyOf(m *Model, pid string) string {
	label, _ := m.portsModel.table.BusyLabel(pid)
	return label
}

// refreshes drains cmd and counts the reads of the table it asked for. The
// caller shortened the footer timer.
func refreshes(cmd tea.Cmd) int { return countRefreshes(testutil.Msgs(cmd)) }

// ── Criterion 1: the first confirmation announces two steps ──────────────────

func TestTheStopConfirmationAnnouncesTwoSteps(t *testing.T) {
	m, _ := stopModel(t)

	if got := modalTitle(m); got != "Stop Process" {
		t.Errorf("title = %q, want %q", got, "Stop Process")
	}
	if got := squash(modalBody(m)); got != stopBody {
		t.Errorf("body = %q, want %q", got, stopBody)
	}
	if strings.Contains(modalBody(m), "SIGKILL") {
		t.Errorf("the first confirmation mentions SIGKILL, which it does not offer: %q", modalBody(m))
	}
	if got := stopPrompt(killTarget{PID: "812", Process: "nginx"}); got != "Ask nginx (PID 812) to stop?\n\nSends SIGTERM and waits up to 5 s. You can force it afterwards." {
		t.Errorf("stopPrompt = %q", got)
	}
}

func TestYesSendsSIGTERMNotSIGKILL(t *testing.T) {
	m, cmd, procs := signalling(t)

	res := runTerm(t, cmd)

	if !reflect.DeepEqual(procs.terminated, []string{"812"}) {
		t.Errorf("SIGTERM went to %v, want [812]", procs.terminated)
	}
	if want := []ports.Identity{idOf("812")}; !reflect.DeepEqual(procs.terminatedWith, want) {
		t.Errorf("SIGTERM verified against %v, want the identity captured at the confirmation %v", procs.terminatedWith, want)
	}
	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v on the first Yes", procs.killed)
	}
	if res.gen != m.portsModel.stop.gen || res.gen == 0 {
		t.Errorf("result generation = %d, stop generation = %d, want one non-zero generation", res.gen, m.portsModel.stop.gen)
	}
	if res.pid != "812" || res.process != "nginx" {
		t.Errorf("result = (pid %q, process %q), want (812, nginx)", res.pid, res.process)
	}
	if res.at.IsZero() {
		t.Error("the result carries no instant: the deadline has nothing to start from")
	}
	if got := busyOf(m, "812"); got != "Stopping nginx (812)" {
		t.Errorf("busy label = %q, want %q", got, "Stopping nginx (812)")
	}
	if m.portsModel.confirmModal != nil || m.portsModel.pendingKill != nil {
		t.Error("the confirmation or its captured target survived Yes")
	}
	if want := (killTarget{PID: "812", Process: "nginx", Identity: idOf("812")}); m.portsModel.stop.target != want {
		t.Errorf("stop target = %+v, want %+v", m.portsModel.stop.target, want)
	}
}

// ── Criterion 2: leaving within the grace, or a zombie, is a stop ────────────

func TestADeliveredSIGTERMStartsTheWait(t *testing.T) {
	m, _, procs := signalling(t)

	m, cmd := termDelivered(t, m, stopT0)

	s := m.portsModel.stop
	if s == nil || s.phase != stopWaiting {
		t.Fatalf("stop = %+v, want phase stopWaiting", s)
	}
	if want := stopT0.Add(5 * time.Second); !s.deadline.Equal(want) {
		t.Errorf("deadline = %v, want %v (5 s after the signal returned)", s.deadline, want)
	}
	if got := busyOf(m, "812"); got != "Waiting for nginx (812) to exit" {
		t.Errorf("busy label = %q, want %q", got, "Waiting for nginx (812) to exit")
	}
	if cmd == nil {
		t.Error("no command returned: nothing would probe the process")
	}
	if m.portsModel.confirmModal != nil {
		t.Error("a confirmation opened while the wait begins")
	}
	if len(procs.probed) != 0 || len(procs.killed) != 0 {
		t.Errorf("probed %v and killed %v before any tick ran", procs.probed, procs.killed)
	}
}

func TestAnExitWithinTheGraceIsAStop(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, procs := waiting(t)

	m, cmd := probeAt(t, m, stopT0.Add(time.Second), ports.ProcessExited, nil)

	if m.portsModel.stop != nil {
		t.Errorf("stop = %+v after the process exited, want none", m.portsModel.stop)
	}
	if m.portsModel.table.IsBusy("812") {
		t.Error("the row is still busy after the process exited")
	}
	if got := m.portsModel.footer.Text(); got != stoppedNginx {
		t.Errorf("footer = %q, want %q", got, stoppedNginx)
	}
	if got := m.portsModel.footer.Level(); got != components.LevelInfo {
		t.Errorf("footer level = %v, want Info", got)
	}
	if n := refreshes(cmd); n != 1 {
		t.Errorf("the stop asked for %d reads of the table, want 1: the row should go without waiting for the next tick", n)
	}
	if m.portsModel.confirmModal != nil {
		t.Error("a second confirmation opened for a process that left")
	}
	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v for a process that left on its own", procs.killed)
	}
}

// A zombie has exited and its parent has not reaped it yet: the stop did its
// job, whatever ps still lists.
func TestAZombieCountsAsStopped(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, procs := waiting(t)

	m, cmd := probeAt(t, m, stopT0.Add(time.Second), ports.ProcessZombie, nil)

	if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
		t.Errorf("a zombie left the stop %+v pending, busy = %v", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
	}
	if got := m.portsModel.footer.Text(); got != stoppedNginx {
		t.Errorf("footer = %q, want %q", got, stoppedNginx)
	}
	if n := refreshes(cmd); n != 1 {
		t.Errorf("the stop asked for %d reads of the table, want 1", n)
	}
	if m.portsModel.confirmModal != nil || len(procs.killed) != 0 {
		t.Errorf("modal open = %v, killed = %v: a zombie is not forced", m.portsModel.confirmModal != nil, procs.killed)
	}
}

func TestStillRunningBeforeTheDeadlineProbesAgain(t *testing.T) {
	m, procs := waiting(t)

	m, cmd := probeAt(t, m, stopT0.Add(4750*time.Millisecond), ports.ProcessRunning, nil)

	if s := m.portsModel.stop; s == nil || s.phase != stopWaiting {
		t.Fatalf("stop = %+v, want it still waiting", s)
	}
	if cmd == nil {
		t.Error("no command returned: the next tick would never come")
	}
	if m.portsModel.confirmModal != nil {
		t.Error("the second confirmation opened before the deadline")
	}
	if got := busyOf(m, "812"); got != "Waiting for nginx (812) to exit" {
		t.Errorf("busy label = %q, want it unchanged while waiting", got)
	}
	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v before the deadline", procs.killed)
	}
}

// ── Criterion 3: no SIGKILL without a second confirmation ────────────────────

func TestStillRunningAtTheDeadlineAsksToForce(t *testing.T) {
	m, procs := waiting(t)

	m, _ = probeAt(t, m, afterGrace, ports.ProcessRunning, nil)

	if s := m.portsModel.stop; s == nil || s.phase != stopAskingForce {
		t.Fatalf("stop = %+v, want phase stopAskingForce", s)
	}
	if m.portsModel.confirmModal == nil {
		t.Fatal("the second confirmation did not open")
	}
	if got := modalTitle(m); got != "Force Kill" {
		t.Errorf("title = %q, want %q", got, "Force Kill")
	}
	if got := squash(modalBody(m)); got != forceBody {
		t.Errorf("body = %q, want %q", got, forceBody)
	}
	if got := forcePrompt(killTarget{PID: "812", Process: "nginx"}); got != "nginx (PID 812) did not exit within 5 s.\n\nForce kill (SIGKILL)? It gets no chance to shut down cleanly." {
		t.Errorf("forcePrompt = %q", got)
	}
	if m.portsModel.table.IsBusy("812") {
		t.Error("the row is still busy while the modal waits for an answer")
	}
	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v before the second confirmation was answered", procs.killed)
	}
}

// ── The second confirmation takes no letter shortcut (DEVDESK-711) ───────────

// answerOf runs the command a key returned and says which answer it carried:
// "yes", "no", or "" when it carried none.
func answerOf(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	switch cmd().(type) {
	case components.ConfirmModalYesMsg:
		return "yes"
	case components.ConfirmModalNoMsg:
		return "no"
	}
	return ""
}

// The user typing a search ("yarn") when the deadline passes must not answer
// the force kill with the y of the word: the modal opens over the filter, takes
// the keyboard off it, and y does nothing.
func TestAYTypedIntoTheFilterAtTheDeadlineDoesNotForce(t *testing.T) {
	m, procs := waiting(t)
	m = feed(t, m, testutil.Key("/"), testutil.Key("n"), testutil.Key("g"))
	if !m.portsModel.table.InEditMode() {
		t.Fatal("setup: the filter is not being edited")
	}

	m, _ = probeAt(t, m, afterGrace, ports.ProcessRunning, nil)
	if m.portsModel.confirmModal == nil {
		t.Fatal("setup: the second confirmation did not open")
	}
	if m.portsModel.table.InEditMode() {
		t.Error("the filter still has the keyboard under the second confirmation")
	}
	if got := m.portsModel.table.FilterBar().SearchQuery(); got != "ng" {
		t.Errorf("the typed filter text = %q, want %q kept", got, "ng")
	}

	for _, key := range []string{"y", "Y"} {
		var cmd tea.Cmd
		m, cmd = step(t, m, testutil.Key(key))
		if got := answerOf(cmd); got != "" {
			t.Fatalf("%q on the second confirmation answered %q", key, got)
		}
	}
	if m.portsModel.stop == nil || m.portsModel.stop.phase != stopAskingForce || m.portsModel.confirmModal == nil {
		t.Fatalf("the second confirmation did not survive y: %+v", m.portsModel.stop)
	}
	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v on a typed letter", procs.killed)
	}
}

func TestTheSecondConfirmationAnswersYesOnlyByFocusAndEnter(t *testing.T) {
	m, _ := askingToForce(t)

	m, cmd := step(t, m, testutil.Key("enter"))
	if got := answerOf(cmd); got != "no" {
		t.Errorf("enter on the default focus answered %q, want no", got)
	}
	m, cmd = step(t, m, testutil.Key("right"))
	if cmd != nil {
		t.Error("moving the focus returned a command")
	}
	_, cmd = step(t, m, testutil.Key("enter"))
	if got := answerOf(cmd); got != "yes" {
		t.Errorf("right then enter answered %q, want yes", got)
	}
}

func TestTheSecondConfirmationAnswersNoOnNAndEsc(t *testing.T) {
	for _, key := range []string{"n", "N", "esc"} {
		m, _ := askingToForce(t)
		_, cmd := step(t, m, testutil.Key(key))
		if got := answerOf(cmd); got != "no" {
			t.Errorf("%q on the second confirmation answered %q, want no", key, got)
		}
	}
}

// Every other modal keeps its y: only the second confirmation drops it.
func TestTheFirstConfirmationKeepsItsYShortcut(t *testing.T) {
	m, _ := stopModel(t)
	_, cmd := step(t, m, testutil.Key("y"))
	if got := answerOf(cmd); got != "yes" {
		t.Errorf("y on the first confirmation answered %q, want yes", got)
	}
}

// The footer says why the modal appeared, as a warning: nothing failed, the
// process just did not leave.
func TestTheSecondConfirmationSaysWhyInTheFooter(t *testing.T) {
	m, _ := askingToForce(t)
	if got, want := m.portsModel.footer.Text(), "nginx (812) did not exit within 5 s"; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
	if got := m.portsModel.footer.Level(); got != components.LevelWarning {
		t.Errorf("footer level = %v, want Warning", got)
	}
}

// An unreadable probe is logged once per stop, not once per tick.
func TestAnUnreadableProbeIsLoggedOncePerStop(t *testing.T) {
	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	m, _ := waiting(t)
	for i := 1; i <= 5; i++ {
		m, _ = probeAt(t, m, stopT0.Add(time.Duration(i)*250*time.Millisecond), ports.ProcessRunning, fmt.Errorf("read failed %d", i))
	}
	if n := strings.Count(buf.String(), "ERROR [netdiag/ports] probe pid=812"); n != 1 {
		t.Errorf("%d probe errors logged for one stop, want 1:\n%s", n, buf.String())
	}

	// A new stop logs its own.
	m = feed(t, m, testutil.Key("esc"), testutil.Key(keymap.Kill))
	m, _ = step(t, m, components.ConfirmModalYesMsg{})
	m, _ = termDelivered(t, m, stopT0)
	probeAt(t, m, stopT0.Add(250*time.Millisecond), ports.ProcessRunning, fmt.Errorf("read failed again"))
	if n := strings.Count(buf.String(), "ERROR [netdiag/ports] probe pid=812"); n != 2 {
		t.Errorf("%d probe errors logged after a second stop, want 2:\n%s", n, buf.String())
	}
}

// Nothing the probe says after the deadline can force a process: only an
// answer to the modal can.
func TestNoSIGKILLWithoutTheSecondConfirmation(t *testing.T) {
	m, procs := waiting(t)
	var opened *components.ConfirmModal

	for i := 1; i <= 32; i++ { // 8 s of ticks, 250 ms apart
		at := stopT0.Add(time.Duration(i) * 250 * time.Millisecond)
		var cmd tea.Cmd
		m, cmd = probeAt(t, m, at, ports.ProcessRunning, nil)

		switch {
		case i < 20:
			if m.portsModel.confirmModal != nil {
				t.Fatalf("tick %d (%v after the signal): the modal opened before the deadline", i, at.Sub(stopT0))
			}
		case i == 20:
			opened = m.portsModel.confirmModal
			if opened == nil {
				t.Fatalf("tick %d (the deadline): the modal did not open", i)
			}
		default:
			if cmd != nil {
				t.Errorf("tick %d: a probe arriving while the modal is open returned a command", i)
			}
			if m.portsModel.confirmModal != opened {
				t.Errorf("tick %d: the modal was replaced", i)
			}
		}
	}
	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v with no answer to the second confirmation", procs.killed)
	}
}

func TestNoOnTheForceLeavesTheProcessAlone(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, tt := range []struct {
		name   string
		answer func(t *testing.T, m *Model) *Model
	}{
		{"No", func(t *testing.T, m *Model) *Model {
			m, _ = step(t, m, components.ConfirmModalNoMsg{})
			return m
		}},
		{"esc", func(t *testing.T, m *Model) *Model {
			m, cmd := step(t, m, testutil.Key("esc"))
			if cmd == nil {
				t.Fatal("esc on the second confirmation returned no command")
			}
			if _, ok := cmd().(components.ConfirmModalNoMsg); !ok {
				t.Fatal("esc on the second confirmation did not answer No")
			}
			m, _ = step(t, m, components.ConfirmModalNoMsg{})
			return m
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, procs := askingToForce(t)

			m = tt.answer(t, m)

			if m.portsModel.stop != nil || m.portsModel.confirmModal != nil {
				t.Errorf("stop = %+v, modal open = %v after No", m.portsModel.stop, m.portsModel.confirmModal != nil)
			}
			if len(procs.killed) != 0 {
				t.Errorf("SIGKILL went to %v after No", procs.killed)
			}
			if !reflect.DeepEqual(procs.terminated, []string{"812"}) {
				t.Errorf("SIGTERM calls = %v, want only the first [812]", procs.terminated)
			}
			if got := m.portsModel.footer.Text(); got != notForcedNginx {
				t.Errorf("footer = %q, want %q", got, notForcedNginx)
			}
			if got := m.portsModel.footer.Level(); got != components.LevelInfo {
				t.Errorf("footer level = %v, want Info", got)
			}
			if m.portsModel.table.IsBusy("812") {
				t.Error("the row is busy though nothing is going on")
			}
			if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Kill) {
				t.Error("K is still greyed after the stop ended")
			}
		})
	}
}

func TestYesOnTheForceSendsSIGKILLToTheCapturedIdentity(t *testing.T) {
	m, cmd, procs := forcing(t)

	if got := busyOf(m, "812"); got != "Killing nginx (812)" {
		t.Errorf("busy label = %q, want %q", got, "Killing nginx (812)")
	}
	if m.portsModel.confirmModal != nil {
		t.Error("the second confirmation survived Yes")
	}
	res := runKill(t, cmd)

	if !reflect.DeepEqual(procs.killed, []string{"812"}) {
		t.Errorf("SIGKILL went to %v, want [812]", procs.killed)
	}
	if want := []ports.Identity{idOf("812")}; !reflect.DeepEqual(procs.killedWith, want) {
		t.Errorf("SIGKILL verified against %v, want the identity captured at the first confirmation %v", procs.killedWith, want)
	}
	if res.gen != m.portsModel.stop.gen {
		t.Errorf("kill result generation = %d, stop generation = %d", res.gen, m.portsModel.stop.gen)
	}

	m, _ = step(t, m, res)
	if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
		t.Errorf("stop = %+v, busy = %v after the SIGKILL result", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
	}
	if got := m.portsModel.footer.Text(); got != "Process 812 terminated" {
		t.Errorf("footer = %q, want %q", got, "Process 812 terminated")
	}
	if got := m.portsModel.footer.Level(); got != components.LevelInfo {
		t.Errorf("footer level = %v, want Info", got)
	}
}

// The second Yes acts on the process the first one named, not on whatever the
// cursor stands on by then (S-01a criterion 5, again).
func TestYesOnTheForceActsOnTheCapturedTargetNotTheSelection(t *testing.T) {
	m, procs := waiting(t)
	m = feed(t, m, testutil.Key("down")) // the cursor leaves nginx for node
	if row, _ := m.portsModel.table.Selected(); row.PID != "4242" {
		t.Fatalf("setup: selected row = %+v, want node", row)
	}
	m, _ = probeAt(t, m, afterGrace, ports.ProcessRunning, nil)

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	runKill(t, cmd)

	if !reflect.DeepEqual(procs.killed, []string{"812"}) {
		t.Errorf("SIGKILL went to %v, want [812]", procs.killed)
	}
}

// ── Criterion 5: the identity decides at the SIGKILL as at the SIGTERM ───────

func TestAChangedIdentityAtTheForceSendsNothing(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	const warning = "nginx (PID 812) is no longer the process that was listed — table refreshed"

	t.Run("the SIGKILL finds another process under the PID", func(t *testing.T) {
		m, _, _ := forcing(t)
		res := portsKillResultMsg{
			gen: m.portsModel.stop.gen, pid: "812", process: "nginx",
			err: fmt.Errorf("killing process 812: %w", &ports.IdentityChangedError{PID: 812}),
		}

		m, cmd := step(t, m, res)

		if got := m.portsModel.footer.Text(); got != warning {
			t.Errorf("footer = %q, want %q", got, warning)
		}
		if got := m.portsModel.footer.Level(); got != components.LevelWarning {
			t.Errorf("footer level = %v, want Warning: nothing failed, DevDesk declined", got)
		}
		if n := refreshes(cmd); n != 1 {
			t.Errorf("the refusal asked for %d reads of the table, want 1", n)
		}
		if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
			t.Errorf("stop = %+v, busy = %v after the refusal", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
		}
	})

	t.Run("the table already lists the PID under another identity", func(t *testing.T) {
		m, procs := askingToForce(t)
		m = feed(t, m, portsDataMsg{ports: []ports.Socket{
			sockWith("812", "nginx", ports.IdentityForTest(99)), sock("4242", "node"),
		}})

		m, cmd := step(t, m, components.ConfirmModalYesMsg{})

		if len(procs.killed) != 0 {
			t.Errorf("SIGKILL went to %v for a PID another process holds", procs.killed)
		}
		if got := m.portsModel.footer.Text(); got != warning {
			t.Errorf("footer = %q, want %q", got, warning)
		}
		if got := m.portsModel.footer.Level(); got != components.LevelWarning {
			t.Errorf("footer level = %v, want Warning", got)
		}
		if n := refreshes(cmd); n != 1 {
			t.Errorf("Yes asked for %d reads of the table, want 1", n)
		}
		if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
			t.Errorf("stop = %+v, busy = %v after the refusal", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
		}
	})
}

// An absent row does not stop the second step: a process that is stopping often
// closes its sockets before it exits, which is the very case the second step
// exists for. The identity is verified at the signal itself.
func TestATargetGoneFromTheTableCanStillBeForced(t *testing.T) {
	m, procs := askingToForce(t)
	m = feed(t, m, portsDataMsg{ports: []ports.Socket{sock("4242", "node"), sock("1204", "sshd")}})

	_, cmd := step(t, m, components.ConfirmModalYesMsg{})
	runKill(t, cmd)

	if !reflect.DeepEqual(procs.killed, []string{"812"}) {
		t.Errorf("SIGKILL went to %v, want [812]: an absent row is not a reason to leave a process running", procs.killed)
	}
}

// The protection is recomputed at the second Yes, as at the first.
func TestForceRechecksTheProtection(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, procs := askingToForce(t)
	procs.protectAs("812", ports.ProtectedSelf) // after the first confirmation

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	testutil.Msgs(cmd)

	if len(procs.killed) != 0 {
		t.Errorf("SIGKILL went to %v: a PID that became protected between the two Yes", procs.killed)
	}
	if got := m.portsModel.footer.Text(); !strings.Contains(got, reasonSelfText) {
		t.Errorf("footer = %q, want it to carry %q", got, reasonSelfText)
	}
	if got := m.portsModel.footer.Level(); got != components.LevelWarning {
		t.Errorf("footer level = %v, want Warning", got)
	}
	if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
		t.Errorf("stop = %+v, busy = %v after the refusal", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
	}
}

// The process left between the deadline and the second Yes: the stop did its
// job, and "no longer running" would read as an error where nothing went wrong.
func TestAProcessThatExitedBeforeTheForceReadsAsStopped(t *testing.T) {
	m, _, _ := forcing(t)

	m, _ = step(t, m, portsKillResultMsg{
		gen: m.portsModel.stop.gen, pid: "812", process: "nginx",
		err: fmt.Errorf("killing process 812: %w", os.ErrProcessDone),
	})

	if got := m.portsModel.footer.Text(); got != stoppedNginx {
		t.Errorf("footer = %q, want %q", got, stoppedNginx)
	}
	if got := m.portsModel.footer.Level(); got != components.LevelInfo {
		t.Errorf("footer level = %v, want Info", got)
	}
	if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
		t.Errorf("stop = %+v, busy = %v after the result", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
	}
}

// The system's refusal of the SIGKILL stays the system's.
func TestARefusedSIGKILLKeepsItsSentence(t *testing.T) {
	m, _, _ := forcing(t)

	m, _ = step(t, m, portsKillResultMsg{
		gen: m.portsModel.stop.gen, pid: "812", process: "nginx",
		err: fmt.Errorf("killing process 812: %w", os.ErrPermission),
	})

	if got, want := m.portsModel.footer.Text(), "Refused by the system — DevDesk cannot signal PID 812"; got != want {
		t.Errorf("footer = %q, want %q", got, want)
	}
	if got := m.portsModel.footer.Level(); got != components.LevelError {
		t.Errorf("footer level = %v, want Error", got)
	}
	if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
		t.Errorf("stop = %+v, busy = %v after the refusal", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
	}
}

// ── Criterion 4: esc stops the waiting, takes nothing back, sends nothing ────

func TestEscStopsTheWait(t *testing.T) {
	m, procs := waiting(t)
	oldGen := m.portsModel.stop.gen

	m, cmd := step(t, m, testutil.Key("esc"))

	if m.portsModel.stop != nil {
		t.Errorf("stop = %+v after esc, want none", m.portsModel.stop)
	}
	if m.portsModel.table.IsBusy("812") {
		t.Error("the row is still busy after esc")
	}
	if got := m.portsModel.footer.Text(); got != stoppedWaitingNginx {
		t.Errorf("footer = %q, want %q", got, stoppedWaitingNginx)
	}
	if got := m.portsModel.footer.Level(); got != components.LevelInfo {
		t.Errorf("footer level = %v, want Info", got)
	}
	if cmd == nil {
		t.Error("esc returned no command, so its footer line would never clear")
	}
	if !reflect.DeepEqual(procs.terminated, []string{"812"}) || len(procs.killed) != 0 {
		t.Errorf("esc signalled: SIGTERM %v, SIGKILL %v, want only the SIGTERM already sent", procs.terminated, procs.killed)
	}
	if m.portsModel.confirmModal != nil {
		t.Error("esc opened a confirmation")
	}

	// The tick that was in flight arrives anyway, at the deadline: it is stale.
	before := m.portsModel.footer.Text()
	m, cmd = step(t, m, portsStopProbeMsg{gen: oldGen, at: afterGrace, liveness: ports.ProcessRunning})
	if m.portsModel.confirmModal != nil {
		t.Error("a stale tick opened the second confirmation")
	}
	if got := m.portsModel.footer.Text(); got != before {
		t.Errorf("a stale tick changed the footer to %q", got)
	}
	if cmd != nil {
		t.Error("a stale tick returned a command: the chain would go on")
	}
	if len(procs.killed) != 0 {
		t.Errorf("a stale tick led to SIGKILL on %v", procs.killed)
	}
}

// A stop begun right after esc has a generation of its own, so the old tick,
// still in flight, cannot touch it.
func TestAStaleTickDoesNotTouchTheNextStop(t *testing.T) {
	m, procs := waiting(t)
	oldGen := m.portsModel.stop.gen
	m = feed(t, m, testutil.Key("esc"))

	m = feed(t, m, testutil.Key("down")) // node, PID 4242
	m = feed(t, m, testutil.Key(keymap.Kill))
	m, cmd := step(t, m, components.ConfirmModalYesMsg{})
	runTerm(t, cmd)
	m, _ = termDelivered(t, m, stopT0.Add(time.Second))
	newGen := m.portsModel.stop.gen
	if newGen == oldGen {
		t.Fatalf("two stops share generation %d: a stale tick could not be told apart", oldGen)
	}

	m, cmd = step(t, m, portsStopProbeMsg{gen: oldGen, at: afterGrace.Add(time.Second), liveness: ports.ProcessRunning})

	if s := m.portsModel.stop; s == nil || s.phase != stopWaiting || s.gen != newGen || s.target.PID != "4242" {
		t.Errorf("stop = %+v, want the new stop still waiting", s)
	}
	if m.portsModel.confirmModal != nil || cmd != nil {
		t.Errorf("the old tick opened a modal (%v) or returned a command (%v)", m.portsModel.confirmModal != nil, cmd != nil)
	}
	if !reflect.DeepEqual(procs.terminated, []string{"812", "4242"}) {
		t.Errorf("SIGTERM calls = %v, want [812 4242]", procs.terminated)
	}
}

// A message of another generation never touches the stop in progress, whichever
// way it points.
func TestAMessageOfAnotherGenerationIsIgnored(t *testing.T) {
	m, _ := waiting(t)
	s := *m.portsModel.stop
	other := s.gen + 1

	for name, msg := range map[string]tea.Msg{
		"a tick that found it gone": portsStopProbeMsg{gen: other, at: stopT0.Add(time.Second), liveness: ports.ProcessExited},
		"a tick at the deadline":    portsStopProbeMsg{gen: other, at: afterGrace, liveness: ports.ProcessRunning},
		"a SIGTERM result":          portsTermResultMsg{gen: other, pid: "812", process: "nginx", at: stopT0.Add(time.Hour)},
		"a refused SIGTERM result":  portsTermResultMsg{gen: other, pid: "812", process: "nginx", err: os.ErrPermission},
	} {
		t.Run(name, func(t *testing.T) {
			next, cmd := step(t, m, msg)

			if got := next.portsModel.stop; got == nil || *got != s {
				t.Errorf("stop = %+v, want it untouched %+v", got, s)
			}
			if next.portsModel.footer.IsSet() || next.portsModel.confirmModal != nil {
				t.Errorf("footer = %q, modal open = %v: a message of another generation had an effect", next.portsModel.footer.Text(), next.portsModel.confirmModal != nil)
			}
			if cmd != nil {
				t.Error("a message of another generation returned a command")
			}
		})
	}
}

// A message of the right generation that the phase does not wait for is ignored
// too: the deadline is set once, by the first SIGTERM result.
func TestAMessageForAnotherPhaseIsIgnored(t *testing.T) {
	t.Run("a second SIGTERM result does not move the deadline", func(t *testing.T) {
		m, _ := waiting(t)
		deadline := m.portsModel.stop.deadline

		m, _ = termDelivered(t, m, stopT0.Add(10*time.Second))

		if s := m.portsModel.stop; s == nil || s.phase != stopWaiting || !s.deadline.Equal(deadline) {
			t.Errorf("stop = %+v, want the deadline to stay %v", s, deadline)
		}
	})
	t.Run("a tick while the SIGTERM is in flight", func(t *testing.T) {
		m, _, _ := signalling(t)

		m, cmd := probeAt(t, m, stopT0.Add(time.Second), ports.ProcessExited, nil)

		if s := m.portsModel.stop; s == nil || s.phase != stopSignalling {
			t.Errorf("stop = %+v, want it still signalling", s)
		}
		if m.portsModel.footer.IsSet() || cmd != nil {
			t.Errorf("footer = %q, command = %v: a tick before the signal returned had an effect", m.portsModel.footer.Text(), cmd != nil)
		}
	})
	t.Run("a tick while the SIGKILL is in flight", func(t *testing.T) {
		m, _, _ := forcing(t)
		// The warning that opened the second confirmation is still up.
		before := m.portsModel.footer.Text()

		m, cmd := probeAt(t, m, afterGrace.Add(time.Second), ports.ProcessExited, nil)

		if s := m.portsModel.stop; s == nil || s.phase != stopForcing {
			t.Errorf("stop = %+v, want it still forcing", s)
		}
		if m.portsModel.footer.Text() != before || cmd != nil {
			t.Errorf("footer = %q (was %q), command = %v: a late tick had an effect", m.portsModel.footer.Text(), before, cmd != nil)
		}
	})
}

// With no stop in progress, a stop message does nothing, whatever its
// generation: a result that outlived the view's stop must not announce a stop
// that was never asked for.
func TestStopMessagesWithNoStopAreIgnored(t *testing.T) {
	for name, msg := range map[string]tea.Msg{
		"a SIGTERM result":    portsTermResultMsg{gen: 99, pid: "812", process: "sshd", at: stopT0},
		"a tick that is gone": portsStopProbeMsg{gen: 99, at: afterGrace, liveness: ports.ProcessExited},
		"a tick that runs":    portsStopProbeMsg{gen: 99, at: afterGrace, liveness: ports.ProcessRunning},
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := identityModel(t, false, stopRows()...)

			m, cmd := step(t, m, msg)

			if m.portsModel.stop != nil || m.portsModel.confirmModal != nil {
				t.Errorf("stop = %+v, modal open = %v", m.portsModel.stop, m.portsModel.confirmModal != nil)
			}
			if m.portsModel.footer.IsSet() || cmd != nil {
				t.Errorf("footer = %q, command = %v", m.portsModel.footer.Text(), cmd != nil)
			}
			if len(m.portsModel.table.BusyLabels()) != 0 {
				t.Errorf("busy rows = %v", m.portsModel.table.BusyLabels())
			}
		})
	}
}

// esc only does something while the wait is on. During the few milliseconds of
// the SIGTERM and while the SIGKILL is in flight there is nothing to cancel, and
// at rest the key means nothing on this tab.
func TestEscOutsideAWaitDoesNothing(t *testing.T) {
	check := func(t *testing.T, m *Model, wantPhase stopPhase) {
		t.Helper()
		before := m.portsModel.stop
		footerBefore := m.portsModel.footer.Text() // a stop may leave its own line up
		next, cmd := step(t, m, testutil.Key("esc"))

		if cmd != nil {
			t.Error("esc returned a command")
		}
		if next.portsModel.footer.Text() != footerBefore {
			t.Errorf("esc said %q, the footer was %q", next.portsModel.footer.Text(), footerBefore)
		}
		if next.portsModel.stop != before {
			t.Errorf("esc changed the stop: %+v, was %+v", next.portsModel.stop, before)
		}
		if wantPhase != 0 && (next.portsModel.stop == nil || next.portsModel.stop.phase != wantPhase) {
			t.Errorf("stop = %+v, want phase %v kept", next.portsModel.stop, wantPhase)
		}
	}
	t.Run("at rest", func(t *testing.T) {
		m, _ := identityModel(t, false, stopRows()...)
		check(t, m, 0)
	})
	t.Run("while the SIGTERM is in flight", func(t *testing.T) {
		m, _, _ := signalling(t)
		check(t, m, stopSignalling)
	})
	t.Run("while the SIGKILL is in flight", func(t *testing.T) {
		m, _, _ := forcing(t)
		check(t, m, stopForcing)
	})
}

// The table keeps refreshing and every key keeps answering while the wait runs.
func TestTheTableAndKeysLiveDuringTheWait(t *testing.T) {
	m, _ := waiting(t)

	// The refresh chain is the table's, independent of the stop's.
	m, cmd := step(t, m, portsTickMsg{})
	if cmd == nil {
		t.Error("a refresh tick returned no command during the wait: the table would freeze")
	}

	more := append(stopRows(), sock("9001", "extra"))
	m = feed(t, m, portsDataMsg{ports: more})
	if got := len(m.portsModel.table.Items()); got != 4 {
		t.Errorf("the table holds %d rows after a refresh during the wait, want 4", got)
	}

	m = feed(t, m, testutil.Key("t"))
	if !m.portsModel.table.IsTokenActive("tcp") {
		t.Error("t did not toggle the tcp filter during the wait")
	}
	before := m.portsModel.table.Cursor()
	m = feed(t, m, testutil.Key("down"))
	if m.portsModel.table.Cursor() == before {
		t.Error("down did not move the cursor during the wait")
	}
	if s := m.portsModel.stop; s == nil || s.phase != stopWaiting {
		t.Errorf("stop = %+v, want the wait to go on through refreshes and keys", s)
	}
}

// ── Criterion 6: a refused or impossible SIGTERM does not wait ───────────────

func TestARefusedSIGTERMDoesNotWait(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, tt := range []struct {
		name      string
		err       error
		want      string
		level     components.Level
		refreshes int
	}{
		{"the system refuses", fmt.Errorf("stopping process 812: %w", os.ErrPermission),
			"Refused by the system — DevDesk cannot signal PID 812", components.LevelError, 0},
		{"the process is gone", fmt.Errorf("stopping process 812: %w", os.ErrProcessDone),
			"PID 812 is no longer running", components.LevelError, 0},
		{"another process holds the PID", fmt.Errorf("stopping process 812: %w", &ports.IdentityChangedError{PID: 812}),
			"nginx (PID 812) is no longer the process that was listed — table refreshed", components.LevelWarning, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, procs := stopModel(t)
			procs.termErr = tt.err
			m, cmd := step(t, m, components.ConfirmModalYesMsg{})
			res := runTerm(t, cmd)

			m, cmd = step(t, m, res)
			msgs := testutil.Msgs(cmd)

			if got := m.portsModel.footer.Text(); got != tt.want {
				t.Errorf("footer = %q, want %q", got, tt.want)
			}
			if got := m.portsModel.footer.Level(); got != tt.level {
				t.Errorf("footer level = %v, want %v", got, tt.level)
			}
			if got := countRefreshes(msgs); got != tt.refreshes {
				t.Errorf("the refusal asked for %d reads of the table, want %d", got, tt.refreshes)
			}
			for _, msg := range msgs {
				if _, ok := msg.(portsStopProbeMsg); ok {
					t.Error("a probe tick was armed after a refused SIGTERM: nothing is waiting")
				}
			}
			if m.portsModel.stop != nil || m.portsModel.table.IsBusy("812") {
				t.Errorf("stop = %+v, busy = %v after the refusal", m.portsModel.stop, m.portsModel.table.IsBusy("812"))
			}
			if len(procs.probed) != 0 || len(procs.killed) != 0 {
				t.Errorf("probed %v and killed %v after a refused SIGTERM", procs.probed, procs.killed)
			}
			if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Kill) {
				t.Error("K is still greyed after the refusal")
			}
		})
	}
}

// ── Criterion 7: one stop at a time ──────────────────────────────────────────

func TestTheStopReasonsAreTheAgreedSentences(t *testing.T) {
	if reasonAnotherStop != anotherStopText {
		t.Errorf("reasonAnotherStop = %q, want the agreed %q", reasonAnotherStop, anotherStopText)
	}
	if reasonStopPendingHere != stopPendingHereText {
		t.Errorf("reasonStopPendingHere = %q, want %q", reasonStopPendingHere, stopPendingHereText)
	}
}

func TestOneStopAtATime(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	phases := []struct {
		name  string
		start func(t *testing.T) *Model
	}{
		{"the SIGTERM is in flight", func(t *testing.T) *Model { m, _, _ := signalling(t); return m }},
		{"the wait is on", func(t *testing.T) *Model { m, _ := waiting(t); return m }},
		{"the SIGKILL is in flight", func(t *testing.T) *Model { m, _, _ := forcing(t); return m }},
	}
	for _, ph := range phases {
		t.Run(ph.name, func(t *testing.T) {
			m := ph.start(t)

			// Another row: the generic reason.
			other := feed(t, m, testutil.Key("down"))
			if row, _ := other.portsModel.table.Selected(); row.PID != "4242" {
				t.Fatalf("setup: selected row = %+v, want node", row)
			}
			if !testutil.HasShortcut(other.GetShortcuts(), keymap.Kill) {
				t.Fatal("K disappeared from the header instead of being greyed (Rule 130)")
			}
			if !testutil.ShortcutDisabled(other.GetShortcuts(), keymap.Kill) {
				t.Error("K is offered on another row while a stop is in progress")
			}
			next, cmd := step(t, other, testutil.Key(keymap.Kill))
			testutil.Msgs(cmd)
			if got := next.portsModel.footer.Text(); got != anotherStopText {
				t.Errorf("footer = %q, want %q", got, anotherStopText)
			}
			if got := next.portsModel.footer.Level(); got != components.LevelWarning {
				t.Errorf("footer level = %v, want Warning: the action cannot be honored, nothing failed", got)
			}
			if next.portsModel.confirmModal != nil || next.portsModel.pendingKill != nil {
				t.Error("K opened a confirmation while a stop is in progress")
			}

			// The row being stopped: its own reason.
			here, _ := step(t, next, testutil.Key("up"))
			if !testutil.ShortcutDisabled(here.GetShortcuts(), keymap.Kill) {
				t.Error("K is offered on the row being stopped")
			}
			here, cmd = step(t, here, testutil.Key(keymap.Kill))
			testutil.Msgs(cmd)
			if got := here.portsModel.footer.Text(); got != stopPendingHereText {
				t.Errorf("footer = %q, want %q", got, stopPendingHereText)
			}
			if here.portsModel.confirmModal != nil {
				t.Error("K opened a confirmation on the row being stopped")
			}
		})
	}

	// Every way a stop ends gives K back.
	ends := []struct {
		name string
		end  func(t *testing.T) *Model
	}{
		{"the process exits within the grace", func(t *testing.T) *Model {
			m, _ := waiting(t)
			m, _ = probeAt(t, m, stopT0.Add(time.Second), ports.ProcessExited, nil)
			return m
		}},
		{"esc stops the wait", func(t *testing.T) *Model {
			m, _ := waiting(t)
			return feed(t, m, testutil.Key("esc"))
		}},
		{"No on the second confirmation", func(t *testing.T) *Model {
			m, _ := askingToForce(t)
			return feed(t, m, components.ConfirmModalNoMsg{})
		}},
		{"the SIGKILL result", func(t *testing.T) *Model {
			m, _, _ := forcing(t)
			return feed(t, m, portsKillResultMsg{gen: m.portsModel.stop.gen, pid: "812", process: "nginx"})
		}},
		{"a refused SIGTERM", func(t *testing.T) *Model {
			m, _, _ := signalling(t)
			return feed(t, m, portsTermResultMsg{gen: m.portsModel.stop.gen, pid: "812", process: "nginx", err: os.ErrPermission})
		}},
	}
	for _, e := range ends {
		t.Run("after "+e.name, func(t *testing.T) {
			m := e.end(t)

			if m.portsModel.stop != nil {
				t.Fatalf("stop = %+v, want none", m.portsModel.stop)
			}
			if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Kill) {
				t.Error("K is still greyed after the stop ended")
			}
			opened := feed(t, m, testutil.Key(keymap.Kill))
			if opened.portsModel.confirmModal == nil {
				t.Error("K does not open a confirmation after the stop ended")
			}
		})
	}
}

// Rule 130: the set of keys is the same at rest, during a stop and on a
// protected row; only what is greyed changes.
func TestTheKeySetHoldsDuringTheStop(t *testing.T) {
	rest, procs := identityModel(t, false, stopRows()...)
	want := testutil.ShortcutKeys(rest.GetShortcuts())

	procs.protectAs("812", ports.ProtectedSelf)
	if got := testutil.ShortcutKeys(rest.GetShortcuts()); !reflect.DeepEqual(got, want) {
		t.Errorf("a protected row advertises %v, an ordinary one %v", got, want)
	}
	procs.protectAs("812", ports.Unprotected)

	m, _ := waiting(t)
	if got := testutil.ShortcutKeys(m.GetShortcuts()); !reflect.DeepEqual(got, want) {
		t.Errorf("a wait advertises %v, rest %v", got, want)
	}
	other := feed(t, m, testutil.Key("down"))
	if got := testutil.ShortcutKeys(other.GetShortcuts()); !reflect.DeepEqual(got, want) {
		t.Errorf("another row during the wait advertises %v, rest %v", got, want)
	}
}

// ── Reading the probe ────────────────────────────────────────────────────────

// An unreadable probe is not an exit: DevDesk announces nothing it did not see.
// The deadline brings the second confirmation, whose SIGKILL verifies the
// identity itself.
func TestAnUnreadableProbeCountsAsRunning(t *testing.T) {
	m, _ := waiting(t)
	unreadable := fmt.Errorf("probing process 812: %w", os.ErrPermission)

	m, cmd := probeAt(t, m, stopT0.Add(time.Second), ports.ProcessRunning, unreadable)

	if s := m.portsModel.stop; s == nil || s.phase != stopWaiting {
		t.Fatalf("stop = %+v, want it still waiting", s)
	}
	if cmd == nil {
		t.Error("an unreadable probe ended the chain: the deadline would never come")
	}
	if m.portsModel.footer.IsSet() {
		t.Errorf("footer = %q: nothing was seen", m.portsModel.footer.Text())
	}

	m, _ = probeAt(t, m, afterGrace, ports.ProcessRunning, unreadable)
	if s := m.portsModel.stop; s == nil || s.phase != stopAskingForce || m.portsModel.confirmModal == nil {
		t.Errorf("stop = %+v, modal open = %v, want the second confirmation at the deadline", s, m.portsModel.confirmModal != nil)
	}
}

// Rule 110: the probe command is a tick that carries values. It reads neither
// the model nor its processOps when it runs, and its message carries the
// generation of the stop that armed it.
func TestTheProbeCommandCarriesValues(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	testutil.FastTimers(t, &stopProbeEvery) // before the Update that arms the tick
	m, procs := sent(t)
	gen := m.portsModel.stop.gen

	m, cmd := termDelivered(t, m, stopT0)
	if cmd == nil {
		t.Fatal("the delivered SIGTERM armed no tick")
	}
	m.portsModel.stop = nil
	m.portsModel.table.SetItems(nil)
	m.portsModel.ops = processOps{probe: func(string, ports.Identity) (ports.Liveness, error) {
		panic("the tick read the model's processOps after Update returned")
	}}

	msg, ok := cmd().(portsStopProbeMsg)
	if !ok {
		t.Fatalf("the tick did not return a portsStopProbeMsg")
	}

	if !reflect.DeepEqual(procs.probed, []string{"812"}) {
		t.Errorf("probed %v, want [812]", procs.probed)
	}
	if want := []ports.Identity{idOf("812")}; !reflect.DeepEqual(procs.probedWith, want) {
		t.Errorf("probed against %v, want the identity captured at the first confirmation %v", procs.probedWith, want)
	}
	if msg.gen != gen {
		t.Errorf("probe generation = %d, want %d", msg.gen, gen)
	}
	if msg.at.IsZero() {
		t.Error("the probe carries no instant: the deadline could never be compared")
	}
	if msg.liveness != ports.ProcessRunning || msg.err != nil {
		t.Errorf("probe = (%v, %v), want what the fake answered", msg.liveness, msg.err)
	}
	if want := []string{"812"}; !reflect.DeepEqual(procs.signals(), want) {
		t.Errorf("signals = %v, want only the SIGTERM already sent %v: the probe signals nothing", procs.signals(), want)
	}
}

// ── Windows keeps S-02a ──────────────────────────────────────────────────────

// There is no clean stop on Windows: Yes is the kill, with no SIGTERM, no wait
// and no second confirmation, and the result belongs to no two-step stop.
func TestWindowsNeverWaits(t *testing.T) {
	m, procs := windowsNode(t)

	m, cmd := step(t, m, components.ConfirmModalYesMsg{})

	if m.portsModel.stop != nil {
		t.Errorf("stop = %+v: Windows has no two-step stop", m.portsModel.stop)
	}
	if got := busyOf(m, "4412"); got != "Killing node.exe (4412)" {
		t.Errorf("busy label = %q, want %q", got, "Killing node.exe (4412)")
	}
	res := runKill(t, cmd)
	if len(procs.terminated) != 0 || len(procs.probed) != 0 {
		t.Errorf("Windows asked for a SIGTERM %v or a probe %v", procs.terminated, procs.probed)
	}
	if !reflect.DeepEqual(procs.killed, []string{"4412"}) {
		t.Errorf("killed %v, want [4412]", procs.killed)
	}
	if res.gen != 0 {
		t.Errorf("the result carries generation %d, want 0", res.gen)
	}

	m, _ = step(t, m, res)
	if m.portsModel.stop != nil || m.portsModel.confirmModal != nil {
		t.Errorf("stop = %+v, modal open = %v after the result", m.portsModel.stop, m.portsModel.confirmModal != nil)
	}
	if got := m.portsModel.footer.Text(); got != "Process 4412 terminated" {
		t.Errorf("footer = %q, want %q", got, "Process 4412 terminated")
	}
}

// A Windows model has no esc to stop a wait: the key does nothing.
func TestEscDoesNothingOnWindows(t *testing.T) {
	m, _ := windowsNode(t)
	m, _ = step(t, m, components.ConfirmModalYesMsg{})

	next, cmd := step(t, m, testutil.Key("esc"))

	if cmd != nil || next.portsModel.footer.IsSet() {
		t.Errorf("esc on Windows returned a command (%v) or said %q", cmd != nil, next.portsModel.footer.Text())
	}
	if !next.portsModel.table.IsBusy("4412") {
		t.Error("esc lifted the busy mark of a Windows kill, which cannot be cancelled")
	}
}

// ── Timing and wiring ────────────────────────────────────────────────────────

func TestStopTimingMatchesTheTexts(t *testing.T) {
	if stopGrace != 5*time.Second {
		t.Errorf("stopGrace = %v, want 5s: the texts say 5 s", stopGrace)
	}
	if stopProbeEvery != 250*time.Millisecond {
		t.Errorf("stopProbeEvery = %v, want 250ms", stopProbeEvery)
	}
	target := killTarget{PID: "812", Process: "nginx"}
	for name, text := range map[string]string{"stopPrompt": stopPrompt(target), "forcePrompt": forcePrompt(target)} {
		if !strings.Contains(text, "5 s") {
			t.Errorf("%s = %q, want it to say the grace (5 s)", name, text)
		}
	}
}

func TestRunningCountsAPendingStop(t *testing.T) {
	rest, _ := identityModel(t, false, stopRows()...)
	if rest.Running() {
		t.Error("Running() with nothing in progress")
	}
	for name, start := range map[string]func(t *testing.T) *Model{
		"the SIGTERM is in flight": func(t *testing.T) *Model { m, _, _ := signalling(t); return m },
		"the wait is on":           func(t *testing.T) *Model { m, _ := waiting(t); return m },
		"the second confirmation":  func(t *testing.T) *Model { m, _ := askingToForce(t); return m },
		"the SIGKILL is in flight": func(t *testing.T) *Model { m, _, _ := forcing(t); return m },
	} {
		t.Run(name, func(t *testing.T) {
			if !start(t).Running() {
				t.Error("Running() is false during a stop: the router could drop the view and lose the wait")
			}
		})
	}
	m, _ := waiting(t)
	if m = feed(t, m, testutil.Key("esc")); m.Running() {
		t.Error("Running() after esc ended the wait")
	}
}
