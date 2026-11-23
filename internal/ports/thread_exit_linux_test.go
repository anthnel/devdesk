//go:build linux

package ports

import (
	"os"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// A process whose main thread ended while other threads run (pthread_exit on the
// leader) reads state Z at field 3 of /proc/<pid>/stat although it lives on
// (DEVDESK-711). The child below is that process: this test binary, whose
// package init ends the leader thread alone (SYS_EXIT, not exit_group) once it
// has said it is ready. The runtime's own threads (sysmon) keep the process up.

const threadExitKind = "main-thread-exits"

func init() {
	if os.Getenv(helperKindEnv) != threadExitKind {
		return
	}
	// init runs on the main thread, the thread-group leader; keep it there.
	runtime.LockOSThread()
	if path := os.Getenv(helperReadyEnv); path != "" {
		if err := os.WriteFile(path, []byte("ready\n"), 0o600); err != nil {
			os.Exit(3)
		}
	}
	_, _, _ = syscall.RawSyscall(syscall.SYS_EXIT, 0, 0, 0) // ends this thread only; never returns
}

// The probe says running for a live process whose leader thread is a zombie.
func TestProbeCountsALiveProcessWithAZombieLeaderAsRunning(t *testing.T) {
	cmd, ready := startHelperKind(t, "TestHelperProcessNeverRuns", threadExitKind, true)
	pid := cmd.Process.Pid
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait() // returns only once every thread is gone
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill() // the helper child this test started itself
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Errorf("the helper process %d did not exit and could not be waited for", pid)
		}
	})
	waitForFile(t, ready)

	raw, err := realOS{}.identity(pid)
	if err != nil {
		t.Fatalf("reading the identity of the helper process %d: %v", pid, err)
	}
	listed := IdentityForTest(raw)

	// Wait for the leader to read Z: the precondition of the whole test.
	statPath := "/proc/" + strconv.Itoa(pid) + "/stat"
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(statPath)
		if err != nil {
			t.Fatalf("reading %s: %v", statPath, err)
		}
		if state, _, err := parseStat(data); err == nil && state == 'Z' {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("the leader thread never read Z: this kernel or runtime does not reproduce the case")
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, err := Probe(strconv.Itoa(pid), listed)
	if err != nil {
		t.Fatalf("Probe(%d) = %v, %v", pid, got, err)
	}
	if got != ProcessRunning {
		t.Fatalf("Probe of a live process with a zombie leader = %v, want %v", got, ProcessRunning)
	}
	if got.Stopped() {
		t.Error("a live process must never count as stopped")
	}
}

// TestHelperProcessNeverRuns is the -test.run target of the helper above: the
// child ends its leader thread in init and never gets as far as a test.
func TestHelperProcessNeverRuns(t *testing.T) {
	if os.Getenv(helperKindEnv) != threadExitKind {
		t.Skip("helper process of the real-child tests")
	}
}
