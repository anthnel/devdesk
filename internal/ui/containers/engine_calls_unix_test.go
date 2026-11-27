//go:build unix

package containers

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-04 (DEVDESK-800): engine calls never block Update, and the inspect
// document is the engine's stdout only.
//
// The engine is a script in a temp dir (fake_engine_unix_test.go): no real
// docker or podman is reached, and the tests hold whether the shell detection
// is launched with os/exec or through the docker runner.

// updateResult is what updateWithin returns.
type updateResult struct {
	m   Model
	cmd tea.Cmd
}

// updateWithin runs Update on a goroutine and fails the test when it does not
// return within d. A slow daemon must not be able to hold the Bubble Tea loop,
// and a test that hangs forever would report that as a timeout of the whole
// package instead of a named failure.
func updateWithin(t *testing.T, m Model, msg tea.Msg, d time.Duration) (Model, tea.Cmd) {
	t.Helper()
	done := make(chan updateResult, 1)
	go func() {
		next, cmd := m.Update(msg)
		done <- updateResult{m: next.(Model), cmd: cmd}
	}()
	select {
	case r := <-done:
		return r.m, r.cmd
	case <-time.After(d):
		t.Fatalf("Update did not return within %v: it is waiting on the engine", d)
		return m, nil
	}
}

// runningWeb returns a loaded model with the running container `web` selected.
func runningWeb(t *testing.T, newWindow bool) Model {
	t.Helper()
	m := selectNamed(t, loadedModel(t), "web")
	m.config.App.TerminalNewWindow = newWindow
	return m
}

// AC1: T on a running container starts no process inside Update; the shell
// detection runs when the returned Cmd is executed.
func TestShellKeyStartsNoProcessInsideUpdate(t *testing.T) {
	for _, tt := range []struct {
		name      string
		newWindow bool
	}{
		{"in place", false},
		{"in a new window", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fe := installFakeEngine(t)
			m := runningWeb(t, tt.newWindow)

			_, cmd := updateWithin(t, m, testutil.Key(keymap.Terminal), 5*time.Second)

			if calls := fe.calls(t); len(calls) != 0 {
				t.Fatalf("Update ran the engine %d time(s) before any Cmd ran: %q", len(calls), calls)
			}
			if cmd == nil {
				t.Fatal("T returned no Cmd: the shell detection has nowhere to run")
			}
		})
	}
}

// AC1, second half: the Cmd is what asks the engine, and it asks `which bash`
// in the selected container.
func TestShellDetectionRunsInTheCmd(t *testing.T) {
	fe := installFakeEngine(t)
	m := runningWeb(t, false)

	_, cmd := updateWithin(t, m, testutil.Key(keymap.Terminal), 5*time.Second)
	if cmd == nil {
		t.Fatal("T returned no Cmd")
	}
	if calls := fe.calls(t); len(calls) != 0 {
		t.Fatalf("the engine ran before the Cmd did: %q", calls)
	}

	cmd() // the detection answers; nothing else may have run it

	calls := fe.calls(t)
	want := "exec " + webID + " which bash"
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("engine calls after the Cmd = %q, want exactly [%q]", calls, want)
	}
}

// AC1: bash when present, sh otherwise. Observed on the command the shell is
// finally launched with, through a fake terminal emulator (new-window mode),
// so the test does not depend on the name of any helper.
func TestShellPrefersBashAndFallsBackToSh(t *testing.T) {
	for _, tt := range []struct {
		name   string
		noBash bool
		shell  string
	}{
		{"bash available", false, "/bin/bash"},
		{"bash missing", true, "/bin/sh"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fe := installFakeEngine(t)
			if tt.noBash {
				t.Setenv("FAKE_ENGINE_NO_BASH", "1")
			}
			terminalLines := fakeTerminal(t)
			m := runningWeb(t, true)

			m, cmd := updateWithin(t, m, testutil.Key(keymap.Terminal), 5*time.Second)
			if cmd == nil {
				t.Fatal("T returned no Cmd")
			}
			// Drive the detection, then the launch, as the runtime would.
			for i := 0; i < 3 && cmd != nil; i++ {
				msg := cmd()
				if _, ok := msg.(ShellWindowOpenedMsg); ok {
					break
				}
				m, cmd = step(t, m, msg)
			}

			want := "-- " + fe.bin + " exec -it " + webID + " " + tt.shell
			if !waitForLine(terminalLines, want, 3*time.Second) {
				t.Errorf("terminal was not started with %q; saw %q", want, terminalLines())
			}
		})
	}
}

// fakeTerminal installs a `kitty` through fakecli and announces KITTY_WINDOW_ID,
// which is how uiterminal.ForCmd detects a terminal. It returns a reader of the
// terminal's journal: one space-joined argv line per run.
func fakeTerminal(t *testing.T) (lines func() []string) {
	t.Helper()
	fake := fakecli.Install(t, fakecli.Scenario{{Tool: "kitty", Prefix: true}}, "kitty")
	t.Setenv("WT_SESSION", "")
	t.Setenv("KITTY_WINDOW_ID", "1")
	return func() []string {
		var out []string
		for _, inv := range fake.Invocations() {
			out = append(out, strings.Join(inv.Args, " "))
		}
		return out
	}
}

// waitForLine polls the terminal's journal until it holds the line, within d.
func waitForLine(lines func() []string, line string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if slices.Contains(lines(), line) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// AC2: with a daemon that does not answer, T and then another key: the other key
// is handled at once and the footer shows that something is pending.
//
// The slow daemon is the fake engine blocked on a FIFO: no sleep, and the test
// decides when it answers.
func TestAKeyAfterTIsHandledWhileTheDaemonIsSlow(t *testing.T) {
	fe := installFakeEngine(t)
	release := slowDaemon(t)
	m := runningWeb(t, false)
	if got := m.getSelectedContainer().Name; got != "web" {
		t.Fatalf("setup: selected %q, want web", got)
	}
	// The same second key on a model that never pressed T, for comparing footers.
	idle, _ := step(t, m, testutil.Key("up"))
	idleFooter := ansi.Strip(idle.RenderFooter(160))

	m, cmd := updateWithin(t, m, testutil.Key(keymap.Terminal), 2*time.Second)
	if cmd == nil {
		t.Fatal("T returned no Cmd")
	}
	// The runtime runs the Cmd off the loop; here it blocks on the slow daemon.
	answered := make(chan tea.Msg, 1)
	go func() { answered <- cmd() }()

	m, _ = updateWithin(t, m, testutil.Key("up"), 2*time.Second)

	if got := m.getSelectedContainer().Name; got == "web" {
		t.Error("the key pressed after T was not handled: the selection did not move")
	}
	if !m.WantsBlink() {
		t.Error("WantsBlink() = false while the shell detection is pending: the footer shows no activity")
	}
	if got := ansi.Strip(m.RenderFooter(160)); got == idleFooter {
		t.Errorf("the footer is the same as without T; it must show the pending shell detection:\n%s", got)
	}

	// The daemon answers: the activity ends.
	release()
	select {
	case msg := <-answered:
		m, _ = step(t, m, msg)
	case <-time.After(5 * time.Second):
		t.Fatalf("the Cmd never returned after the daemon answered; engine calls: %q", fe.calls(t))
	}
	if m.WantsBlink() {
		t.Error("WantsBlink() is still true after the shell detection answered")
	}
}

// AC3: an engine that warns on stderr and prints JSON on stdout: the inspect
// document is the JSON alone.
func TestInspectDocumentHoldsStdoutOnly(t *testing.T) {
	installFakeEngine(t)
	m := runningWeb(t, false)

	_, cmd := step(t, m, testutil.Key("enter"))
	data, err := openRequestSource(t, cmd).Load()

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	doc := strings.TrimSpace(string(data))
	if strings.Contains(doc, "WARNING") || strings.Contains(doc, "deprecated") {
		t.Errorf("the engine's stderr reached the viewer:\n%s", doc)
	}
	if !strings.HasPrefix(doc, "[") || !strings.HasSuffix(doc, "]") {
		t.Errorf("the document is not just the JSON array:\n%s", doc)
	}
}

// In place, the answer of the detection hands the terminal to the shell through
// a Cmd (teaexec) and ends the wait; the Cmd itself is not run, it would take
// the test's terminal.
func TestShellDetectedInPlaceStartsTheShellAndEndsTheWait(t *testing.T) {
	installFakeEngine(t)
	m := runningWeb(t, false)
	m, _ = updateWithin(t, m, testutil.Key(keymap.Terminal), 5*time.Second)
	if !m.WantsBlink() {
		t.Fatal("setup: T did not mark the detection pending")
	}

	m, cmd := step(t, m, ShellDetectedMsg{ID: webID, Shell: "/bin/bash"})

	if m.WantsBlink() {
		t.Error("the footer still shows the shell detection after the answer")
	}
	if cmd == nil {
		t.Error("the answer started no shell")
	}
}

// A second T while the first detection is pending is refused out loud, and the
// engine is not asked again.
func TestASecondShellKeyIsRefusedWhileOneIsPending(t *testing.T) {
	fe := installFakeEngine(t)
	m := runningWeb(t, false)
	m, _ = updateWithin(t, m, testutil.Key(keymap.Terminal), 5*time.Second)

	m, cmd := updateWithin(t, m, testutil.Key(keymap.Terminal), 5*time.Second)

	if got := m.footer.Text(); got != "Already opening a shell" {
		t.Errorf("footer = %q, want the refusal", got)
	}
	if cmd == nil {
		t.Error("the refusal has no timer to clear it")
	}
	if calls := fe.calls(t); len(calls) != 0 {
		t.Errorf("the refused key ran the engine: %q", calls)
	}
}

// An answer for a user who left the view ends the wait and starts nothing.
func TestAnAbandonedShellDetectionEndsTheWaitAndStartsNothing(t *testing.T) {
	installFakeEngine(t)
	m := runningWeb(t, false)
	m, _ = updateWithin(t, m, testutil.Key(keymap.Terminal), 5*time.Second)

	m, cmd := step(t, m, ShellDetectedMsg{ID: webID, Shell: "/bin/sh", Abandoned: true})

	if m.WantsBlink() {
		t.Error("the wait did not end")
	}
	if cmd != nil {
		t.Error("an abandoned detection started a shell")
	}
}
