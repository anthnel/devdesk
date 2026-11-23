package ports

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Real signals for the two-step stop (DEVDESK-711, S-02b), Linux only, and only
// at children this test binary started itself (allowRealSignals). Probing sends
// nothing: it reads /proc.
//
// The children are this very binary, run on a helper test that does nothing
// unless the parent set the variable of the helper it wants.

const (
	// helperReadyEnv names the file a helper creates once it is set up.
	helperReadyEnv = "DEVDESK_PORTS_HELPER_READY"
	// helperKindEnv selects what a helper does.
	helperKindEnv = "DEVDESK_PORTS_HELPER_KIND"
)

// TestHelperProcessIgnoresTerm is not a test: it is the body of a child that
// ignores SIGTERM, so only SIGKILL can end it.
func TestHelperProcessIgnoresTerm(t *testing.T) {
	if os.Getenv(helperKindEnv) != "ignores-term" {
		t.Skip("helper process of the real-child tests")
	}
	signal.Ignore(syscall.SIGTERM)
	if path := os.Getenv(helperReadyEnv); path != "" {
		if err := os.WriteFile(path, []byte("ready\n"), 0o600); err != nil {
			os.Exit(3)
		}
	}
	time.Sleep(time.Minute)
}

// TestHelperProcessExits is not a test: it is the body of a child that exits at
// once. Left unwaited by the parent, it becomes a zombie.
func TestHelperProcessExits(t *testing.T) {
	if os.Getenv(helperKindEnv) != "exits" {
		t.Skip("helper process of the real-child tests")
	}
}

func skipUnlessLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the real-child proof is run on Linux")
	}
}

// startHelperKind starts the test binary on the helper test named run, with the
// kind the helper checks. It returns the command, started and not waited for,
// and the file the helper creates when it is ready (empty if it does not).
func startHelperKind(t *testing.T, run, kind string, withReady bool) (*exec.Cmd, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+run+"$")
	cmd.Env = append(os.Environ(), helperKindEnv+"="+kind)
	ready := ""
	if withReady {
		ready = filepath.Join(t.TempDir(), "ready")
		cmd.Env = append(cmd.Env, helperReadyEnv+"="+ready)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the helper process: %v", err)
	}
	pid := cmd.Process.Pid
	// Never signal anything that is not the child this test started.
	if pid <= 1 || pid == os.Getpid() || pid == os.Getppid() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("the helper process has PID %d (self %d, parent %d): refusing to signal it", pid, os.Getpid(), os.Getppid())
	}
	return cmd, ready
}

// waitForFile polls until path exists.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the helper process did not become ready within 10 s")
}

// startIgnoringTerm starts a child that ignores SIGTERM and waits until it says
// it does. It is waited for in a goroutine, like startHelperChild's, and ended
// at cleanup by the test itself.
func startIgnoringTerm(t *testing.T) *helperChild {
	t.Helper()
	cmd, ready := startHelperKind(t, "TestHelperProcessIgnoresTerm", "ignores-term", true)
	c := &helperChild{cmd: cmd, pid: cmd.Process.Pid, done: make(chan struct{})}
	go func() {
		c.waitErr = cmd.Wait()
		close(c.done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill() // an error here only says the child already ended
		select {
		case <-c.done:
		case <-time.After(10 * time.Second):
			t.Errorf("the helper process %d did not exit and could not be waited for", c.pid)
		}
	})
	waitForFile(t, ready)
	return c
}

// waitDone waits for the child to be gone.
func (c *helperChild) waitDone(t *testing.T, within time.Duration) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(within):
		t.Fatalf("the helper process %d was still running %v after the signal", c.pid, within)
	}
}

// exitedBy is the text of how the child ended, as os/exec words it
// ("signal: terminated", "signal: killed").
func (c *helperChild) exitedBy() string {
	if c.cmd.ProcessState == nil {
		return "still running"
	}
	return c.cmd.ProcessState.String()
}

// ── SIGTERM ──────────────────────────────────────────────────────────────────

// A well-behaved child ends on SIGTERM, and the signal is a SIGTERM: not a kill.
func TestTerminateEndsARealChildProcess(t *testing.T) {
	skipUnlessLinux(t)
	child := startHelperChild(t)
	listed := child.identityOf(t)
	logPidfd(t, child.pid)

	allowRealSignals(t)
	if err := Terminate(strconv.Itoa(child.pid), listed); err != nil {
		t.Fatalf("Terminate(%d) = %v, want nil", child.pid, err)
	}

	child.waitDone(t, 2*time.Second)
	if got := child.exitedBy(); !strings.Contains(got, "terminated") {
		t.Errorf("the helper process ended with %q, want %q: the signal was not SIGTERM", got, "signal: terminated")
	}
}

// Criterion 5 for SIGTERM, on a real process: the PID is held by a live process
// whose identity is not the listed one, and nothing reaches it.
func TestTerminateSparesARealChildWhoseIdentityDiffers(t *testing.T) {
	skipUnlessLinux(t)
	child := startHelperChild(t)
	listed := child.identityOf(t)
	other := IdentityForTest(listed.raw + 1)
	logPidfd(t, child.pid)

	allowRealSignals(t)
	err := Terminate(strconv.Itoa(child.pid), other)

	var changed *IdentityChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("Terminate with another identity = %v (%T), want an *IdentityChangedError", err, err)
	}
	if changed.PID != child.pid {
		t.Errorf("PID = %d, want %d", changed.PID, child.pid)
	}
	time.Sleep(300 * time.Millisecond)
	if !child.running() {
		t.Fatalf("the helper process %d ended (%v): a SIGTERM reached a process whose identity differed", child.pid, child.waitErr)
	}
	if child.cmd.ProcessState != nil {
		t.Errorf("ProcessState = %v, want none: the child must still be running", child.cmd.ProcessState)
	}
}

// ── The probe on a real zombie (no signal at all) ────────────────────────────

// Criterion 2: a process that exited but was not reaped by its parent still has
// a /proc entry, in state Z, with its start stamp. It counts as stopped.
func TestProbeSeesARealZombie(t *testing.T) {
	skipUnlessLinux(t)
	cmd, _ := startHelperKind(t, "TestHelperProcessExits", "exits", false)
	pid := cmd.Process.Pid
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Wait() // reaps the zombie; the child has exited or is about to
		}
	})

	raw, err := realOS{}.identity(pid)
	if err != nil {
		t.Fatalf("reading the identity of the helper process %d: %v", pid, err)
	}
	listed := IdentityForTest(raw)
	pidText := strconv.Itoa(pid)

	var got Liveness
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err = Probe(pidText, listed)
		if err != nil {
			t.Fatalf("Probe(%d) = %v, %v", pid, got, err)
		}
		if got == ProcessZombie || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got != ProcessZombie {
		t.Fatalf("Probe of an exited, unreaped child = %v, want %v", got, ProcessZombie)
	}
	if !got.Stopped() {
		t.Error("a zombie does not count as stopped")
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("waiting for the helper process: %v", err)
	}
	waited = true
	got, err = Probe(pidText, listed)
	if err != nil || got != ProcessExited {
		t.Errorf("Probe after the parent reaped the child = %v, %v; want %v, nil", got, err, ProcessExited)
	}
}

// ── The two steps on a child that ignores SIGTERM ────────────────────────────

// Criteria 3 and 5: SIGTERM is delivered and ignored, the probe keeps saying
// running, and only the explicit Kill ends the child, with SIGKILL.
func TestTheTwoStepStopOnARealChildThatIgnoresSIGTERM(t *testing.T) {
	skipUnlessLinux(t)
	child := startIgnoringTerm(t)
	listed := child.identityOf(t)
	pid := strconv.Itoa(child.pid)
	logPidfd(t, child.pid)

	allowRealSignals(t)
	if err := Terminate(pid, listed); err != nil {
		t.Fatalf("Terminate(%d) = %v, want nil", child.pid, err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		got, err := Probe(pid, listed)
		if err != nil {
			t.Fatalf("Probe(%d) = %v, %v", child.pid, got, err)
		}
		if got != ProcessRunning {
			t.Fatalf("Probe = %v after a SIGTERM the child ignores, want %v", got, ProcessRunning)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !child.running() {
		t.Fatalf("the helper process %d ended (%v) although it ignores SIGTERM", child.pid, child.exitedBy())
	}

	if err := Kill(pid, listed); err != nil {
		t.Fatalf("Kill(%d) = %v, want nil", child.pid, err)
	}
	child.waitDone(t, 10*time.Second)
	if got := child.exitedBy(); !strings.Contains(got, "killed") {
		t.Errorf("the helper process ended with %q, want %q", got, "signal: killed")
	}
}

// Criterion 5 for the second step: after the SIGTERM, a PID held by a process
// with another identity is spared by the SIGKILL. The child is still the one
// the test started; the test ends it at cleanup.
func TestKillAfterTerminateSparesAReusedPID(t *testing.T) {
	skipUnlessLinux(t)
	child := startIgnoringTerm(t)
	listed := child.identityOf(t)
	other := IdentityForTest(listed.raw + 1)
	pid := strconv.Itoa(child.pid)

	allowRealSignals(t)
	if err := Terminate(pid, listed); err != nil {
		t.Fatalf("Terminate(%d) = %v, want nil", child.pid, err)
	}
	err := Kill(pid, other)

	var changed *IdentityChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("Kill with another identity = %v (%T), want an *IdentityChangedError", err, err)
	}
	time.Sleep(300 * time.Millisecond)
	if !child.running() {
		t.Fatalf("the helper process %d ended (%v): a SIGKILL reached a process whose identity differed", child.pid, child.exitedBy())
	}
	if got, err := Probe(pid, other); err != nil || got != ProcessExited {
		t.Errorf("Probe with the other identity = %v, %v; want %v (the listed process is not the holder)", got, err, ProcessExited)
	}
}
