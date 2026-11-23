package ports

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Terminate is the first half of the two-step stop (DEVDESK-711, S-02b): SIGTERM
// through the same single point as Kill, with the same checks. Everything here
// runs on the fake procOS; the real signals are in stop_real_test.go.

// ── The signal ───────────────────────────────────────────────────────────────

func TestTerminateSendsSIGTERMWithTheListedIdentity(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")

	if err := Terminate("4242", IdentityForTest(9)); err != nil {
		t.Fatalf("Terminate(4242) = %v, want nil", err)
	}
	if !slices.Equal(fake.kills, []int{4242}) {
		t.Errorf("procOS.kill was called with %v, want [4242]", fake.kills)
	}
	if !slices.Equal(fake.sigs, []stopSignal{signalTerm}) {
		t.Errorf("procOS.kill was asked for %v, want [signalTerm]", fake.sigs)
	}
	if len(fake.killedWith) != 1 || fake.killedWith[0] != IdentityForTest(9) {
		t.Errorf("procOS.kill got the identity %v, want the listed one %v", fake.killedWith, IdentityForTest(9))
	}
	if len(fake.idCalls) != 0 {
		t.Errorf("Terminate read the identity of %v itself: it must trust only what it was given", fake.idCalls)
	}
}

// Kill keeps meaning SIGKILL for its callers (the Windows path and the second
// step of the stop).
func TestKillStillSendsSIGKILL(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")

	if err := Kill("4242", IdentityForTest(9)); err != nil {
		t.Fatalf("Kill(4242) = %v, want nil", err)
	}
	if !slices.Equal(fake.sigs, []stopSignal{signalKill}) {
		t.Errorf("procOS.kill was asked for %v, want [signalKill]", fake.sigs)
	}
}

// The two signals have distinct, non-zero values: the zero value of stopSignal
// names no signal and must stay unused.
func TestStopSignalsAreDistinctAndNonZero(t *testing.T) {
	if signalTerm == 0 || signalKill == 0 {
		t.Errorf("signalTerm = %d, signalKill = %d: the zero value must name no signal", signalTerm, signalKill)
	}
	if signalTerm == signalKill {
		t.Errorf("signalTerm and signalKill are both %d", signalTerm)
	}
}

// ── The checks Terminate shares with Kill ────────────────────────────────────

func TestTerminateRefusesLikeKill(t *testing.T) {
	const self, parent = 100, 99
	cases := []struct {
		name        string
		pid         string
		listed      Identity
		wantType    string // "protected", "unverifiable" or "notpid"
		wantMessage string
	}{
		{"DevDesk itself", "100", IdentityForTest(1), "protected", ""},
		{"its parent", "99", IdentityForTest(1), "protected", ""},
		{"PID 1", "1", IdentityForTest(1), "protected", ""},
		{"an unknown identity", "4242", Identity{}, "unverifiable", "Cannot verify which process PID 4242 is — not stopped"},
		{"a deferred identity", "4242", DeferredIdentityForTest(fakeEpoch), "unverifiable", ""},
		{"not a number", "abc", IdentityForTest(1), "notpid", "not a process id"},
		{"zero", "0", IdentityForTest(1), "notpid", "not a process id"},
		{"past int32", "4294967297", IdentityForTest(1), "notpid", "not a process id"},
		{"not canonical", "007", IdentityForTest(1), "notpid", "not a process id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := useFakeOS(t, self, parent, "linux")

			err := Terminate(tc.pid, tc.listed)

			if err == nil {
				t.Fatalf("Terminate(%q) returned no error", tc.pid)
			}
			var perr *ProtectedError
			var uerr *UnverifiableError
			switch tc.wantType {
			case "protected":
				if !errors.As(err, &perr) {
					t.Errorf("Terminate(%q) = %v (%T), want a *ProtectedError", tc.pid, err, err)
				}
			case "unverifiable":
				if !errors.As(err, &uerr) {
					t.Errorf("Terminate(%q) = %v (%T), want an *UnverifiableError", tc.pid, err, err)
				}
			case "notpid":
				if errors.As(err, &perr) || errors.As(err, &uerr) {
					t.Errorf("Terminate(%q) = %v (%T), want a plain refusal", tc.pid, err, err)
				}
			}
			if tc.wantMessage != "" && !strings.Contains(err.Error(), tc.wantMessage) {
				t.Errorf("Terminate(%q) = %q, want it to say %q", tc.pid, err.Error(), tc.wantMessage)
			}
			if len(fake.kills) != 0 {
				t.Errorf("procOS.kill was called with %v for %q", fake.kills, tc.pid)
			}
		})
	}
}

// Terminate and Kill refuse the same way, word for word, for the same input.
func TestTerminateAndKillRefuseWithTheSameWords(t *testing.T) {
	for _, tc := range []struct {
		pid    string
		listed Identity
	}{
		{"100", IdentityForTest(1)},
		{"1", IdentityForTest(1)},
		{"4242", Identity{}},
		{"abc", IdentityForTest(1)},
	} {
		t.Run(tc.pid, func(t *testing.T) {
			useFakeOS(t, 100, 99, "linux")
			killErr := Kill(tc.pid, tc.listed)
			termErr := Terminate(tc.pid, tc.listed)
			if killErr == nil || termErr == nil {
				t.Fatalf("Kill = %v, Terminate = %v: both must refuse", killErr, termErr)
			}
			if killErr.Error() != termErr.Error() {
				t.Errorf("Kill says %q, Terminate says %q: the refusal must read the same", killErr, termErr)
			}
		})
	}
}

// The order is protection, then identity, then the signal: a protected row
// whose identity is unknown reads as protected (the line says "This is DevDesk
// itself", not "cannot verify"), and nothing is signalled before both passed.
func TestTerminateChecksProtectionThenIdentityThenSignals(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")

	var perr *ProtectedError
	if err := Terminate("100", Identity{}); !errors.As(err, &perr) {
		t.Errorf("Terminate(self, unknown identity) = %v (%T), want a *ProtectedError first", err, err)
	}
	var uerr *UnverifiableError
	if err := Terminate("4242", Identity{}); !errors.As(err, &uerr) {
		t.Errorf("Terminate(other, unknown identity) = %v (%T), want an *UnverifiableError", err, err)
	}
	if len(fake.kills) != 0 {
		t.Fatalf("a signal was requested (%v) before protection and identity both passed", fake.kills)
	}

	if err := Terminate("4242", IdentityForTest(3)); err != nil {
		t.Fatalf("Terminate with a known identity = %v", err)
	}
	if len(fake.kills) != 1 {
		t.Errorf("procOS.kill was called %d times after the checks passed, want 1", len(fake.kills))
	}
}

// ── Windows has no clean stop ────────────────────────────────────────────────

func TestTerminateOnWindowsHasNoCleanStop(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")

	err := Terminate("4412", IdentityForTest(1))

	if !errors.Is(err, ErrNoCleanStop) {
		t.Fatalf("Terminate on Windows = %v, want errors.Is ErrNoCleanStop", err)
	}
	if len(fake.kills) != 0 {
		t.Errorf("procOS.kill was called with %v on Windows: there is no SIGTERM there", fake.kills)
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrProcessDone) {
		t.Errorf("ErrNoCleanStop wraps an OS sentinel (%v): it is not an OS error", err)
	}
}

// The protection comes first, so the line of DevDesk itself keeps saying what it
// is on every platform.
func TestTerminateOnWindowsRefusesAProtectedProcessFirst(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")

	var perr *ProtectedError
	for _, pid := range []string{"4", "100"} {
		err := Terminate(pid, IdentityForTest(1))
		if !errors.As(err, &perr) {
			t.Errorf("Terminate(%s) on Windows = %v (%T), want a *ProtectedError", pid, err, err)
		}
		if errors.Is(err, ErrNoCleanStop) {
			t.Errorf("Terminate(%s) on Windows answered ErrNoCleanStop before the protection", pid)
		}
	}
	if len(fake.kills) != 0 {
		t.Errorf("procOS.kill was called with %v", fake.kills)
	}
}

// Kill on Windows is unchanged: TerminateProcess, through the same kill.
func TestKillOnWindowsIsUnchanged(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")

	if err := Kill("4412", IdentityForTest(1)); err != nil {
		t.Fatalf("Kill on Windows = %v, want nil", err)
	}
	if !slices.Equal(fake.sigs, []stopSignal{signalKill}) {
		t.Errorf("Kill on Windows asked for %v, want [signalKill]", fake.sigs)
	}
}

// ── The error stays readable ─────────────────────────────────────────────────

// The Ports tab classifies a failure with errors.Is / errors.As: the wrapping
// of Terminate must keep every cause reachable, like Kill's.
func TestTerminateKeepsTheOSErrorReadable(t *testing.T) {
	causes := []error{
		os.ErrPermission,
		os.ErrProcessDone,
		&IdentityChangedError{PID: 4242},
	}
	for _, cause := range causes {
		t.Run(fmt.Sprintf("%T", cause), func(t *testing.T) {
			fake := useFakeOS(t, 100, 99, "linux")
			fake.killErr = cause

			err := Terminate("4242", IdentityForTest(1))

			if err == nil {
				t.Fatal("Terminate returned nil although the OS refused")
			}
			if !errors.Is(err, cause) {
				t.Errorf("errors.Is(%v, %v) = false: the cause was not wrapped with %%w", err, cause)
			}
			var changed *IdentityChangedError
			if _, isChanged := cause.(*IdentityChangedError); isChanged && !errors.As(err, &changed) {
				t.Errorf("errors.As(%v, *IdentityChangedError) = false", err)
			}
		})
	}
}

func TestTerminateNamesTheStopInItsErrorText(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")
	fake.killErr = os.ErrPermission

	err := Terminate("4242", IdentityForTest(1))

	if err == nil || !strings.Contains(err.Error(), "stopping process 4242") {
		t.Errorf("Terminate = %v, want the text to start with %q", err, "stopping process 4242")
	}
}

// ── realOS.kill and the signal it is told to send ────────────────────────────

// A kill that names no signal is refused, whatever the PID: the zero value of
// stopSignal must never fall through to the default of "send SIGKILL". The PID
// is above pid_max, so no process could be reached even by a broken check.
func TestANamelessSignalIsRefused(t *testing.T) {
	allowRealSignals(t)

	for _, sig := range []stopSignal{0, -1, signalKill + 7} {
		err := realOS{}.kill(unreachablePID, IdentityForTest(1), sig)
		if err == nil {
			t.Errorf("realOS.kill(%d) with the signal %d returned nil", unreachablePID, sig)
			continue
		}
		if !strings.Contains(err.Error(), "no signal named") {
			t.Errorf("realOS.kill with the signal %d = %q, want it to say %q", sig, err.Error(), "no signal named")
		}
	}
}

// Terminate reaches the signal through the default procOS: with no substitution
// a test calling it must hit the guard, not the machine.
func TestTerminateWithoutASubstitutedProcOSHitsTheGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Terminate stops at ErrNoCleanStop on Windows and never reaches realOS.kill")
	}
	if _, ok := procOS.(realOS); !ok {
		t.Fatalf("procOS defaults to %T, want realOS", procOS)
	}
	got := recoverPanic(func() { _ = Terminate(strconv.Itoa(unreachablePID), IdentityForTest(1)) })
	if got == nil {
		t.Fatal("Terminate reached a real signal in a test binary without allowRealSignals")
	}
	if !strings.Contains(fmt.Sprint(got), "DEVDESK-708") {
		t.Errorf("panic = %v, want a message naming DEVDESK-708", got)
	}
}
