package ports

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Kill needs the identity of the row it was asked to stop (DEVDESK-709,
// criteria 2, 3, 5 and 6, the ports side). The decision itself is
// verdictAtSignal; here is what Kill and ConfirmIdentity do around it.

// ── Kill with the fake procOS ────────────────────────────────────────────────

// Criterion 5, word for word: an unknown identity is refused, and nothing is
// sent.
func TestKillRefusesAnUnknownIdentity(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")

	err := Kill("4242", Identity{})

	var unverifiable *UnverifiableError
	if !errors.As(err, &unverifiable) {
		t.Fatalf("Kill = %v (%T), want an *UnverifiableError", err, err)
	}
	if want := "Cannot verify which process PID 4242 is — not stopped"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if unverifiable.PID != 4242 {
		t.Errorf("PID = %d, want 4242", unverifiable.PID)
	}
	if len(fake.kills) != 0 {
		t.Errorf("procOS.kill was called with %v although the identity is unknown", fake.kills)
	}
	if len(fake.idCalls) != 0 {
		t.Errorf("Kill read the identity of %v itself: it must trust only what it was given", fake.idCalls)
	}
}

// A deferred identity is a promise to read it at confirmation: it never reaches
// Kill, and if it does the answer is a refusal.
func TestKillRefusesADeferredIdentity(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")

	err := Kill("4412", DeferredIdentityForTest(fake.clock))

	var unverifiable *UnverifiableError
	if !errors.As(err, &unverifiable) {
		t.Fatalf("Kill = %v (%T), want an *UnverifiableError", err, err)
	}
	if len(fake.kills) != 0 {
		t.Errorf("procOS.kill was called with %v for a deferred identity", fake.kills)
	}
}

// The order is protection, then identity, then signal: DevDesk's own row says
// "This is DevDesk itself", not "Cannot verify".
func TestProtectionComesBeforeIdentity(t *testing.T) {
	const self, parent = 100, 99
	cases := []struct{ name, pid string }{
		{"DevDesk itself", "100"},
		{"its parent", "99"},
		{"init", "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := useFakeOS(t, self, parent, "linux")

			err := Kill(tc.pid, Identity{})

			var protected *ProtectedError
			if !errors.As(err, &protected) {
				t.Fatalf("Kill(%s, unknown identity) = %v (%T), want a *ProtectedError", tc.pid, err, err)
			}
			if len(fake.kills) != 0 {
				t.Errorf("procOS.kill was called with %v", fake.kills)
			}
		})
	}
}

// Criterion 3: the identity the row carried is what the signal point verifies.
func TestKillPassesTheListedIdentity(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")

	if err := Kill("4242", IdentityForTest(9)); err != nil {
		t.Fatalf("Kill = %v, want nil", err)
	}
	if want := []int{4242}; !slices.Equal(fake.kills, want) {
		t.Errorf("procOS.kill was called with %v, want %v", fake.kills, want)
	}
	if want := []Identity{IdentityForTest(9)}; !slices.Equal(fake.killedWith, want) {
		t.Errorf("procOS.kill was asked to verify %+v, want %+v", fake.killedWith, want)
	}
}

// Criteria 3 and 6: what the signal point returns stays reachable through Kill's
// wrapping, so the UI can tell a refusal of the system from DevDesk's own.
func TestKillKeepsTheOSErrorReadable(t *testing.T) {
	t.Run("sentinels, bare and wrapped by the opening of the process", func(t *testing.T) {
		causes := []error{
			os.ErrPermission,
			os.ErrProcessDone,
			fmt.Errorf("opening the process: %w", os.ErrPermission),
			fmt.Errorf("process 4242: %w", os.ErrProcessDone),
		}
		for _, cause := range causes {
			fake := useFakeOS(t, 100, 99, "linux")
			fake.killErr = cause

			err := Kill("4242", IdentityForTest(1))

			sentinel := os.ErrPermission
			if errors.Is(cause, os.ErrProcessDone) {
				sentinel = os.ErrProcessDone
			}
			if !errors.Is(err, sentinel) {
				t.Errorf("Kill with %q: errors.Is(%v, %v) = false: the cause was not wrapped with %%w", cause, err, sentinel)
			}
			var unverifiable *UnverifiableError
			var changed *IdentityChangedError
			if errors.As(err, &unverifiable) || errors.As(err, &changed) {
				t.Errorf("Kill turned %q into %T", cause, err)
			}
		}
	})

	t.Run("DevDesk's own refusals", func(t *testing.T) {
		fake := useFakeOS(t, 100, 99, "linux")

		fake.killErr = &IdentityChangedError{PID: 4242}
		var changed *IdentityChangedError
		if err := Kill("4242", IdentityForTest(1)); !errors.As(err, &changed) {
			t.Errorf("Kill = %v (%T), want it to carry the *IdentityChangedError", err, err)
		}

		fake.killErr = &UnverifiableError{PID: 4242, Cause: os.ErrPermission}
		var unverifiable *UnverifiableError
		err := Kill("4242", IdentityForTest(1))
		if !errors.As(err, &unverifiable) {
			t.Fatalf("Kill = %v (%T), want it to carry the *UnverifiableError", err, err)
		}
		if errors.Is(err, os.ErrPermission) {
			t.Error("the permission cause of an UnverifiableError leaked through Kill")
		}
	})
}

// ── ConfirmIdentity ──────────────────────────────────────────────────────────

func TestConfirmIdentity(t *testing.T) {
	const pid = "4412"
	deferred := func(f *fakeOS) Identity { return DeferredIdentityForTest(f.clock) }

	cases := []struct {
		name      string
		listed    func(f *fakeOS) Identity
		read      func(f *fakeOS) // sets what the fake answers for 4412
		want      func(f *fakeOS) Identity
		check     func(t *testing.T, err error)
		wantReads int
	}{
		{
			name:   "a known identity is returned as it is, with no read",
			listed: func(*fakeOS) Identity { return IdentityForTest(9) },
			want:   func(*fakeOS) Identity { return IdentityForTest(9) },
		},
		{
			name:   "an unknown identity is unverifiable, with no read",
			listed: func(*fakeOS) Identity { return Identity{} },
			check: func(t *testing.T, err error) {
				var u *UnverifiableError
				if !errors.As(err, &u) {
					t.Errorf("err = %v (%T), want an *UnverifiableError", err, err)
				}
			},
		},
		{
			name:      "deferred, born a second before the listing: the read becomes the identity",
			listed:    deferred,
			read:      func(f *fakeOS) { f.ids[4412] = filetimeOf(f.clock.Add(-time.Second)) },
			want:      func(f *fakeOS) Identity { return IdentityForTest(filetimeOf(f.clock.Add(-time.Second))) },
			wantReads: 1,
		},
		{
			name:   "deferred, born a second after the listing: not the process that was listed",
			listed: deferred,
			read:   func(f *fakeOS) { f.ids[4412] = filetimeOf(f.clock.Add(time.Second)) },
			check: func(t *testing.T, err error) {
				var c *IdentityChangedError
				if !errors.As(err, &c) {
					t.Fatalf("err = %v (%T), want an *IdentityChangedError", err, err)
				}
				if c.PID != 4412 {
					t.Errorf("PID = %d, want 4412", c.PID)
				}
			},
			wantReads: 1,
		},
		{
			name:   "deferred, born within the slack: cannot tell",
			listed: deferred,
			read:   func(f *fakeOS) { f.ids[4412] = filetimeOf(f.clock.Add(-50 * time.Millisecond)) },
			check: func(t *testing.T, err error) {
				var u *UnverifiableError
				if !errors.As(err, &u) {
					t.Errorf("err = %v (%T), want an *UnverifiableError", err, err)
				}
			},
			wantReads: 1,
		},
		{
			name:   "deferred, the handle is refused: the system's refusal stays readable",
			listed: deferred,
			read:   func(f *fakeOS) { f.idErr[4412] = fmt.Errorf("opening the process: %w", os.ErrPermission) },
			check: func(t *testing.T, err error) {
				if !errors.Is(err, os.ErrPermission) {
					t.Errorf("err = %v, want errors.Is os.ErrPermission (criterion 6, the fallback)", err)
				}
			},
			wantReads: 1,
		},
		{
			name:   "deferred, the process is gone: it reads as gone",
			listed: deferred,
			read:   func(f *fakeOS) { f.idErr[4412] = os.ErrProcessDone },
			check: func(t *testing.T, err error) {
				if !errors.Is(err, os.ErrProcessDone) {
					t.Errorf("err = %v, want errors.Is os.ErrProcessDone", err)
				}
			},
			wantReads: 1,
		},
		{
			name:   "deferred, any other failure: unverifiable, and the cause is not an OS sentinel",
			listed: deferred,
			read:   func(f *fakeOS) { f.idErr[4412] = errors.New("short read") },
			check: func(t *testing.T, err error) {
				var u *UnverifiableError
				if !errors.As(err, &u) {
					t.Errorf("err = %v (%T), want an *UnverifiableError", err, err)
				}
			},
			wantReads: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := useFakeOS(t, 100, 99, "windows")
			listed := tc.listed(fake)
			if tc.read != nil {
				tc.read(fake)
			}

			got, err := ConfirmIdentity(pid, listed)

			if len(fake.idCalls) != tc.wantReads {
				t.Errorf("identity was read %d times (%v), want %d", len(fake.idCalls), fake.idCalls, tc.wantReads)
			}
			if tc.check != nil {
				if err == nil {
					t.Fatalf("ConfirmIdentity = %+v, nil; want an error", got)
				}
				if got.Verifiable() {
					t.Errorf("ConfirmIdentity returned the verifiable identity %+v together with the error %v", got, err)
				}
				tc.check(t, err)
				return
			}
			if err != nil {
				t.Fatalf("ConfirmIdentity = %v, want nil", err)
			}
			if want := tc.want(fake); got != want {
				t.Errorf("ConfirmIdentity = %+v, want %+v", got, want)
			}
			if len(fake.kills) != 0 {
				t.Errorf("ConfirmIdentity signalled %v", fake.kills)
			}
		})
	}
}

func TestConfirmIdentityRefusesWhatIsNotAProcessID(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")
	for _, pid := range []string{"", "sshd", "4294967297", "007", "8 12"} {
		_, err := ConfirmIdentity(pid, IdentityForTest(9))
		if err == nil {
			t.Errorf("ConfirmIdentity(%q) returned no error", pid)
			continue
		}
		if !strings.Contains(err.Error(), "not a process id") {
			t.Errorf("ConfirmIdentity(%q) = %q, want it to say %q", pid, err.Error(), "not a process id")
		}
	}
	if len(fake.idCalls) != 0 {
		t.Errorf("identity was read for %v", fake.idCalls)
	}
}

// ── Real children (Linux): the only real signals, and the proof of criterion 2 ──

const helperEnv = "DEVDESK_PORTS_HELPER"

// TestHelperProcessSleeps is not a test: it is the body of the child process the
// tests below start from this very binary. It does nothing unless the parent set
// the variable.
func TestHelperProcessSleeps(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("helper process of the real-child tests")
	}
	time.Sleep(time.Minute)
}

// helperChild is a process the test started itself and always waits for, so it
// leaves neither a zombie nor an orphan.
type helperChild struct {
	cmd     *exec.Cmd
	pid     int
	done    chan struct{}
	waitErr error // valid once done is closed
}

// startHelperChild starts the test binary on the helper above. Its cleanup ends
// and waits for the child: that signal is the test's, aimed at its own child,
// through os/exec and not through the package under test.
func startHelperChild(t *testing.T) *helperChild {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessSleeps$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the helper process: %v", err)
	}
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
	// Never signal anything that is not the child this test started.
	if c.pid <= 1 || c.pid == os.Getpid() || c.pid == os.Getppid() {
		t.Fatalf("the helper process has PID %d (self %d, parent %d): refusing to signal it", c.pid, os.Getpid(), os.Getppid())
	}
	return c
}

// running reports whether the child has not exited, without blocking.
func (c *helperChild) running() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// identityOf reads the child's real identity.
func (c *helperChild) identityOf(t *testing.T) Identity {
	t.Helper()
	raw, err := realOS{}.identity(c.pid)
	if err != nil {
		t.Fatalf("reading the identity of the helper process %d: %v", c.pid, err)
	}
	return IdentityForTest(raw)
}

// logPidfd tells the PR which kernel the guarantee was run on: with a pidfd the
// comparison is made with the process held, without one a residual window
// remains (documented).
func logPidfd(t *testing.T, pid int) {
	t.Helper()
	p, err := os.FindProcess(pid)
	if err != nil {
		t.Logf("os.FindProcess(%d): %v", pid, err)
		return
	}
	defer p.Release() //nolint:errcheck // the only effect is releasing a descriptor
	if err := p.WithHandle(func(uintptr) {}); err != nil {
		t.Logf("no process handle on this kernel (%v): the comparison has a residual window", err)
		return
	}
	t.Logf("the process is held by a handle (pidfd): the comparison is made with the process held")
}

// TestKillEndsARealChildProcess sends a real signal to a child the test started
// itself, with the identity that child really has: a matching identity must not
// get in the way.
func TestKillEndsARealChildProcess(t *testing.T) {
	child := startHelperChild(t)
	listed := child.identityOf(t)
	logPidfd(t, child.pid)

	allowRealSignals(t)
	if err := Kill(strconv.Itoa(child.pid), listed); err != nil {
		t.Fatalf("Kill(%d) = %v, want nil", child.pid, err)
	}

	select {
	case <-child.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the helper process %d was still running 10 s after Kill", child.pid)
	}
	if child.waitErr == nil {
		t.Error("the helper process exited successfully: it was not ended by the signal")
	}
	if child.cmd.ProcessState == nil || child.cmd.ProcessState.Success() {
		t.Errorf("ProcessState = %v, want an unsuccessful exit", child.cmd.ProcessState)
	}
}

// TestKillSparesARealChildWhoseIdentityDiffers is criterion 2 on a real process:
// the PID is held by a live process whose identity is not the one that was
// listed, and no signal reaches it.
func TestKillSparesARealChildWhoseIdentityDiffers(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the real-child proof is run on Linux, where the process is held by a pidfd")
	}
	child := startHelperChild(t)
	listed := child.identityOf(t)
	other := IdentityForTest(listed.raw + 1)
	logPidfd(t, child.pid)

	allowRealSignals(t)
	err := Kill(strconv.Itoa(child.pid), other)

	var changed *IdentityChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("Kill with another identity = %v (%T), want an *IdentityChangedError", err, err)
	}
	if changed.PID != child.pid {
		t.Errorf("PID = %d, want %d", changed.PID, child.pid)
	}
	time.Sleep(300 * time.Millisecond)
	if !child.running() {
		t.Fatalf("the helper process %d ended (%v): a signal reached a process whose identity differed", child.pid, child.waitErr)
	}
	if child.cmd.ProcessState != nil {
		t.Errorf("ProcessState = %v, want none: the child must still be running", child.cmd.ProcessState)
	}
}

// TestKillOfAnExitedChildSendsNothing: the listed process is gone. Either the
// PID is free (no process) or it was taken by another process (a different
// identity); a signal is never sent, and Kill never reports success.
func TestKillOfAnExitedChildSendsNothing(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the real-child proof is run on Linux")
	}
	child := startHelperChild(t)
	listed := child.identityOf(t)

	_ = child.cmd.Process.Kill() // the test's own child, ended by the test
	select {
	case <-child.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("the helper process %d did not exit", child.pid)
	}

	allowRealSignals(t)
	err := Kill(strconv.Itoa(child.pid), listed)

	var changed *IdentityChangedError
	if err == nil {
		t.Fatal("Kill of an exited process returned nil")
	}
	if !errors.Is(err, os.ErrProcessDone) && !errors.As(err, &changed) {
		t.Errorf("Kill = %v (%T), want errors.Is os.ErrProcessDone or an *IdentityChangedError", err, err)
	}
}
