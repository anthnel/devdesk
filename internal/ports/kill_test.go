package ports

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

// ── Kill goes through procOS ─────────────────────────────────────────────────

// Kill's only way to a signal is procOS.kill: that is what lets a test observe
// it, and what lets the guard stop a real one.
func TestKillSendsThroughProcOS(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")

	if err := Kill("4242", IdentityForTest(1)); err != nil {
		t.Fatalf("Kill(4242) = %v, want nil", err)
	}
	if len(fake.kills) != 1 || fake.kills[0] != 4242 {
		t.Errorf("procOS.kill was called with %v, want [4242]", fake.kills)
	}
}

// ── The PID the kernel reads is not the PID the string says ──────────────────

// The kernel reads the low 32 bits of the PID (pidfd_open, kill, OpenProcess
// all take 32 bits), so 1<<32+1 is PID 1 to it while an int comparison sees
// something else, and 4294967295 is kill(-1, SIGKILL): everything the user may
// signal. Kill and ProtectionOf share one parser that accepts only a canonical
// decimal in the int32 range (DEVDESK-708, criterion 4). Anything else is "not
// a process id", never a ProtectedError, and never reaches the OS.
func TestKillRefusesAPIDOutsideInt32OrNotCanonical(t *testing.T) {
	const self, parent = 100, 99
	cases := []struct {
		name string
		goos string
		pid  string
	}{
		{"kill(-1) once truncated to 32 bits", "linux", "4294967295"},
		{"PID 1 once truncated to 32 bits", "linux", "4294967297"},
		{"one past int32", "linux", "2147483648"},
		{"2^32, PID 0 once truncated", "linux", "4294967296"},
		{"DevDesk itself once truncated", "linux", strconv.FormatInt(1<<32+int64(self), 10)},
		{"the parent once truncated", "linux", strconv.FormatInt(1<<32+int64(parent), 10)},
		{"the parent once truncated, far above", "linux", strconv.FormatInt(1<<40+int64(parent), 10)},
		{"overflows even int64", "linux", "99999999999999999999"},
		{"negative below int32", "linux", "-2147483649"},
		{"explicit plus sign", "linux", "+5"},
		{"leading zeros", "linux", "007"},
		{"negative zero", "linux", "-0"},
		{"leading space", "linux", " 5"},
		{"trailing space", "linux", "5 "},
		{"2^32 on Windows, the Idle process once truncated", "windows", "4294967296"},
		{"2^32+4 on Windows, System once truncated", "windows", "4294967300"},
		{"DevDesk itself once truncated on Windows", "windows", strconv.FormatInt(1<<32+int64(self), 10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := useFakeOS(t, self, parent, tc.goos)

			err := Kill(tc.pid, IdentityForTest(1))
			if err == nil {
				t.Errorf("Kill(%q) returned no error", tc.pid)
			} else {
				var perr *ProtectedError
				if errors.As(err, &perr) {
					t.Errorf("Kill(%q) = %v, a ProtectedError: the value is not a process id at all", tc.pid, err)
				}
				if !strings.Contains(err.Error(), "not a process id") {
					t.Errorf("Kill(%q) = %q, want it to say %q", tc.pid, err.Error(), "not a process id")
				}
			}
			if len(fake.kills) != 0 {
				t.Errorf("procOS.kill was called with %v for %q", fake.kills, tc.pid)
			}
			if got := ProtectionOf(tc.pid); got != Unprotected {
				t.Errorf("ProtectionOf(%q) = %v, want Unprotected", tc.pid, got)
			}
		})
	}
}

// The parser must not over-refuse: the largest int32 is a canonical PID and
// reaches the OS.
func TestKillAcceptsTheLargestInt32PID(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")

	if got := ProtectionOf("2147483647"); got != Unprotected {
		t.Errorf("ProtectionOf(2147483647) = %v, want Unprotected", got)
	}
	if err := Kill("2147483647", IdentityForTest(1)); err != nil {
		t.Fatalf("Kill(2147483647) = %v, want nil", err)
	}
	if len(fake.kills) != 1 || fake.kills[0] != 2147483647 {
		t.Errorf("procOS.kill was called with %v, want [2147483647]", fake.kills)
	}
}

// The Ports tab reads the failure with errors.Is (permission, already gone): the
// wrapping must keep the cause reachable.
func TestKillWrapsTheOSErrorSoTheUICanClassifyIt(t *testing.T) {
	for _, cause := range []error{os.ErrPermission, os.ErrProcessDone} {
		t.Run(cause.Error(), func(t *testing.T) {
			fake := useFakeOS(t, 100, 99, "linux")
			fake.killErr = cause

			err := Kill("4242", IdentityForTest(1))
			if err == nil {
				t.Fatal("Kill returned nil although the OS refused")
			}
			if !errors.Is(err, cause) {
				t.Errorf("errors.Is(%v, %v) = false: the cause was not wrapped with %%w", err, cause)
			}
		})
	}
}
