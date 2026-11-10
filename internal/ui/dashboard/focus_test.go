package dashboard

import (
	"reflect"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-210 step 7: the three clocks keep their timers while the terminal is
// blurred but skip the I/O; one read per clock on the focus edge, no new timer
// (the chain is still alive: one chain per clock).

// isolateFocusTest keeps the Cmds the tests run away from the host: no docker
// binary on PATH, a throwaway HOME for the caches and workspaces directory.
func isolateFocusTest(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
}

// collect runs cmd (flattening batches) and returns the messages that arrive
// within window. A Cmd that blocks longer, like the 30 s slow clock, simply
// contributes nothing: tea.Tick cannot be drained by testutil.Msgs.
func collect(cmd tea.Cmd, window time.Duration) []tea.Msg {
	var (
		mu  sync.Mutex
		out []tea.Msg
		run func(tea.Cmd)
	)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
				return
			}
			if msg == nil {
				return
			}
			mu.Lock()
			out = append(out, msg)
			mu.Unlock()
		}()
	}
	run(cmd)
	time.Sleep(window)
	mu.Lock()
	defer mu.Unlock()
	return append([]tea.Msg(nil), out...)
}

func hasType[T any](msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(T); ok {
			return true
		}
	}
	return false
}

func types(msgs []tea.Msg) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, reflect.TypeOf(m).String())
	}
	return out
}

func blurred(t *testing.T) Model {
	t.Helper()
	m, _ := newTestModel(t)
	return feed(t, m, shared.TerminalFocusMsg{Focused: false})
}

func TestBlurredHostTickKeepsItsTimerAndSkipsTheSample(t *testing.T) {
	isolateFocusTest(t)
	testutil.FastTimers(t, &hostTickInterval) // a var, not a const, once this lands
	m := blurred(t)

	_, cmd := m.Update(HostTickMsg(time.Now()))
	msgs := collect(cmd, 300*time.Millisecond)

	if !hasType[HostTickMsg](msgs) {
		t.Errorf("blurred HostTickMsg dropped its timer; got %v", types(msgs))
	}
	if hasType[HostSampleMsg](msgs) {
		t.Errorf("blurred HostTickMsg sampled the host; got %v", types(msgs))
	}
}

func TestBlurredDockerTickKeepsItsTimerAndSkipsDockerStats(t *testing.T) {
	isolateFocusTest(t)
	testutil.FastTimers(t, &dockerTickInterval)
	m := blurred(t)

	_, cmd := m.Update(DockerTickMsg(time.Now()))
	msgs := collect(cmd, 300*time.Millisecond)

	if !hasType[DockerTickMsg](msgs) {
		t.Errorf("blurred DockerTickMsg dropped its timer; got %v", types(msgs))
	}
	if hasType[DockerMetricsMsg](msgs) {
		t.Errorf("blurred DockerTickMsg ran docker stats; got %v", types(msgs))
	}
}

func TestBlurredSlowClockSkipsTheRound(t *testing.T) {
	isolateFocusTest(t)
	m := blurred(t)

	_, cmd := m.Update(RefreshTickMsg(time.Now()))
	if cmd == nil {
		t.Fatal("blurred RefreshTickMsg returned no Cmd: the timer must be kept")
	}
	msgs := collect(cmd, 500*time.Millisecond)

	for _, bad := range []bool{
		hasType[StatusCheckMsg](msgs), hasType[ForgeStatsMsg](msgs), hasType[DockerStatsMsg](msgs),
		hasType[OCIStatsMsg](msgs), hasType[WorkspaceStatsMsg](msgs), hasType[DiskUsageMsg](msgs),
		hasType[PostureMsg](msgs),
	} {
		if bad {
			t.Fatalf("blurred RefreshTickMsg ran part of the slow round; got %v", types(msgs))
		}
	}
}

func TestBlurredTickStillRunsWithoutAFocusMessage(t *testing.T) {
	// Degradation (H1): a terminal that never reports focus is never blurred.
	isolateFocusTest(t)
	testutil.FastTimers(t, &hostTickInterval)
	m, _ := newTestModel(t)

	_, cmd := m.Update(HostTickMsg(time.Now()))
	msgs := collect(cmd, 500*time.Millisecond)

	if !hasType[HostSampleMsg](msgs) || !hasType[HostTickMsg](msgs) {
		t.Errorf("a never-blurred dashboard must sample and rearm; got %v", types(msgs))
	}
}

func TestFocusResumeReadsEachClockOnceWithoutANewTimer(t *testing.T) {
	isolateFocusTest(t)
	testutil.FastTimers(t, &hostTickInterval)
	testutil.FastTimers(t, &dockerTickInterval)
	m := blurred(t)

	m2, cmd := m.Update(shared.TerminalFocusMsg{Focused: true, Resume: true})
	if cmd == nil {
		t.Fatal("a resuming TerminalFocusMsg returned no Cmd")
	}
	msgs := collect(cmd, time.Second)

	for name, ok := range map[string]bool{
		"HostSampleMsg":     hasType[HostSampleMsg](msgs),
		"DockerMetricsMsg":  hasType[DockerMetricsMsg](msgs),
		"WorkspaceStatsMsg": hasType[WorkspaceStatsMsg](msgs),
		"DiskUsageMsg":      hasType[DiskUsageMsg](msgs),
		"PostureMsg":        hasType[PostureMsg](msgs),
	} {
		if !ok {
			t.Errorf("resume did not read once: no %s; got %v", name, types(msgs))
		}
	}
	// Timers are 1 ms here: a second chain would show within the window. The
	// 30 s slow clock cannot be observed this way, the review covers it.
	if hasType[HostTickMsg](msgs) || hasType[DockerTickMsg](msgs) {
		t.Errorf("resume started a second tick chain; got %v", types(msgs))
	}

	// And the dashboard is back to its normal self: the next tick samples.
	_, cmd = m2.Update(HostTickMsg(time.Now()))
	if !hasType[HostSampleMsg](collect(cmd, 500*time.Millisecond)) {
		t.Error("after the resume, a HostTickMsg no longer samples the host")
	}
}

func TestFocusLevelWithoutResumeStartsNothing(t *testing.T) {
	m, _ := newTestModel(t)
	for _, msg := range []shared.TerminalFocusMsg{
		{Focused: false, Resume: false},
		{Focused: true, Resume: false}, // entry level
	} {
		if _, cmd := m.Update(msg); cmd != nil {
			t.Errorf("%+v returned a Cmd, want nil (flag only)", msg)
		}
	}
}
