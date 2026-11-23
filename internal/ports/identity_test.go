package ports

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// DEVDESK-709, S-01b: a signal goes only to the process that was listed. These
// are the pure parts: the Identity value, its two errors, the parsing of the
// operating system's creation stamps and the decision taken at the signal.

// ── The value ────────────────────────────────────────────────────────────────

// The zero value is "unknown", so a caller that forgets the identity cannot
// send a signal (criterion 5).
func TestTheZeroIdentityIsNotVerifiable(t *testing.T) {
	if (Identity{}).Verifiable() {
		t.Error("the zero Identity is verifiable: a forgotten identity would pass for a known one")
	}
	if !IdentityForTest(7).Verifiable() {
		t.Error("IdentityForTest(7) is not verifiable")
	}
	if !DeferredIdentityForTest(fakeEpoch).Verifiable() {
		t.Error("a deferred identity is not verifiable: the row would be greyed although it can be read at confirmation")
	}
}

// Identities are compared with == by the tests and by the UI fixtures, so two
// reads of the same stamp must be equal and two different stamps must not.
func TestIdentityIsComparable(t *testing.T) {
	a, b := IdentityForTest(7), IdentityForTest(7)
	if a != b {
		t.Error("two identities built from the same stamp differ")
	}
	if IdentityForTest(7) == IdentityForTest(8) {
		t.Error("two identities built from different stamps are equal")
	}
	if IdentityForTest(7) == (Identity{}) {
		t.Error("a known identity equals the unknown one")
	}
	if IdentityForTest(7) == DeferredIdentityForTest(fakeEpoch) {
		t.Error("a known identity equals a deferred one")
	}
}

func TestConflicts(t *testing.T) {
	cases := []struct {
		name string
		a, b Identity
		want bool
	}{
		{"same stamp", IdentityForTest(7), IdentityForTest(7), false},
		{"different stamps", IdentityForTest(7), IdentityForTest(8), true},
		{"different stamps, the other way", IdentityForTest(8), IdentityForTest(7), true},
		{"known and deferred", IdentityForTest(7), DeferredIdentityForTest(fakeEpoch), false},
		{"deferred and known", DeferredIdentityForTest(fakeEpoch), IdentityForTest(7), false},
		{"known and unknown", IdentityForTest(7), Identity{}, false},
		{"unknown and known", Identity{}, IdentityForTest(7), false},
		{"two unknown", Identity{}, Identity{}, false},
		{"two deferred", DeferredIdentityForTest(fakeEpoch), DeferredIdentityForTest(fakeEpoch.Add(time.Hour)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Conflicts(tc.b); got != tc.want {
				t.Errorf("Conflicts = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── The creation stamps ──────────────────────────────────────────────────────

// statLine builds a /proc/<pid>/stat line of the real shape, the 22nd field
// (starttime) set to start. comm goes between the parentheses as is.
func statLine(pid int, comm string, start uint64) string {
	// Fields 3 to 22 are the 20 tokens after the last ")": the state, then
	// ppid, pgrp, session, tty_nr, tpgid, flags, minflt, cminflt, majflt,
	// cmajflt, utime, stime, cutime, cstime, priority, nice, num_threads,
	// itrealvalue, and starttime.
	rest := []string{"S", "1", "4242", "4242", "0", "-1", "4194560", "1500", "0", "0", "0", "10", "5", "0", "0", "20", "0", "11", "0"}
	rest = append(rest, fmt.Sprint(start))
	// The fields after starttime: vsize, rss, rsslim, and the rest, none of which
	// the parser may look at.
	rest = append(rest, "123456789", "5000", "18446744073709551615", "1", "1", "0", "0", "0", "17", "3", "0", "0")
	return fmt.Sprintf("%d (%s) %s\n", pid, comm, strings.Join(rest, " "))
}

func TestParseStartTicks(t *testing.T) {
	cases := []struct {
		name    string
		stat    string
		want    uint64
		wantErr bool
	}{
		{
			name: "a line as the kernel writes it",
			stat: "1234 (bash) S 1000 1234 1234 34816 1234 4194304 1500 22000 0 0 3 1 0 0 20 0 1 0 987654 12345678 1234 " +
				"18446744073709551615 94000000000000 94000000010000 140700000000000 0 0 0 65536 3670020 1266777851 0 0 0 17 3 0 0 0 0 0\n",
			want: 987654,
		},
		{name: "a plain name", stat: statLine(4242, "node", 31337), want: 31337},
		{name: "a name with spaces", stat: statLine(4242, "Web Content", 555), want: 555},
		{name: "a name with parentheses and spaces", stat: statLine(4242, "a) (b c", 777), want: 777},
		{name: "a name that looks like the rest of the line", stat: statLine(4242, ") S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 (", 888), want: 888},
		{name: "a name closing with parentheses", stat: statLine(4242, "x))", 999), want: 999},
		{name: "an empty name", stat: statLine(4242, "", 12), want: 12},
		{name: "a start of zero is a start, not an error", stat: statLine(4242, "init", 0), want: 0},
		{name: "a start beyond int64", stat: statLine(4242, "late", 18446744073709551000), want: 18446744073709551000},
		{name: "no closing parenthesis", stat: "4242 node S 1 2 3", wantErr: true},
		{name: "cut before field 22", stat: "4242 (node) S 1 4242 4242 0 -1 4194560 1500 0 0 0 10 5 0 0 20 0 11 0", wantErr: true},
		{name: "cut very early", stat: "4242 (node) S 1 4242", wantErr: true},
		{name: "nothing after the name", stat: "4242 (node)", wantErr: true},
		{name: "field 22 is not a number", stat: strings.Replace(statLine(4242, "node", 31337), "31337", "abc", 1), wantErr: true},
		{name: "field 22 is negative", stat: strings.Replace(statLine(4242, "node", 31337), "31337", "-5", 1), wantErr: true},
		{name: "field 22 is empty", stat: "4242 (node) S 1 4242 4242 0 -1 4194560 1500 0 0 0 10 5 0 0 20 0 11 0 ", wantErr: true},
		{name: "empty input", stat: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStartTicks([]byte(tc.stat))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseStartTicks = %d, nil; want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStartTicks = %v, want %d", err, tc.want)
			}
			if got != tc.want {
				t.Errorf("parseStartTicks = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTimevalStamp(t *testing.T) {
	cases := []struct {
		sec  int64
		usec int32
		want uint64
	}{
		{1, 2, 1000002},
		{0, 0, 0},
		{0, 999999, 999999},
		{1700000000, 123456, 1700000000123456},
	}
	for _, tc := range cases {
		if got := timevalStamp(tc.sec, tc.usec); got != tc.want {
			t.Errorf("timevalStamp(%d, %d) = %d, want %d", tc.sec, tc.usec, got, tc.want)
		}
	}
}

func TestFiletimeUnixNano(t *testing.T) {
	// The last FILETIME whose Unix nanoseconds still fit in an int64.
	const lastRaw = unixAsFiletime + math.MaxInt64/100
	cases := []struct {
		name   string
		raw    uint64
		want   int64
		wantOK bool
	}{
		{"the Unix epoch", unixAsFiletime, 0, true},
		{"one second after it", unixAsFiletime + 10_000_000, 1_000_000_000, true},
		{"100 ns after it", unixAsFiletime + 1, 100, true},
		{"a known instant", filetimeOf(fakeEpoch), fakeEpoch.UnixNano(), true},
		{"the last representable raw", lastRaw, math.MaxInt64 / 100 * 100, true},
		{"one past the last representable raw", lastRaw + 1, 0, false},
		{"the largest raw", math.MaxUint64, 0, false},
		{"zero, before 1677 in Unix ns", 0, 0, false},
		{"one tick before the Unix epoch", unixAsFiletime - 1, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := filetimeUnixNano(tc.raw)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("filetimeUnixNano(%d) = (%d, %v), want (%d, %v)", tc.raw, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// ── Where a Windows creation time falls against the listing instant ──────────

func TestJudgeBirth(t *testing.T) {
	const slack = 100 * time.Millisecond
	listed := fakeEpoch.UnixNano()
	cases := []struct {
		name    string
		created int64
		want    birth
	}{
		{"a second before the listing", listed - int64(time.Second), bornBefore},
		{"exactly the slack before: the bound is born before", listed - int64(slack), bornBefore},
		{"one ns inside the slack", listed - int64(slack) + 1, bornAround},
		{"50 ms before", listed - int64(50*time.Millisecond), bornAround},
		{"at the listing instant", listed, bornAround},
		{"one ns after the listing", listed + 1, bornAfter},
		{"a second after the listing", listed + int64(time.Second), bornAfter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := judgeBirth(tc.created, listed, slack); got != tc.want {
				t.Errorf("judgeBirth = %v, want %v", got, tc.want)
			}
		})
	}
}

// ── What is decided at the signal ────────────────────────────────────────────

func TestVerdictAtSignal(t *testing.T) {
	const pid = 4242
	wrappedGone := fmt.Errorf("reading /proc/4242/stat: %w", os.ErrProcessDone)

	t.Run("the same stamp sends", func(t *testing.T) {
		if err := verdictAtSignal(pid, IdentityForTest(7), 7, nil); err != nil {
			t.Errorf("verdictAtSignal = %v, want nil", err)
		}
	})

	t.Run("another stamp refuses with an IdentityChangedError", func(t *testing.T) {
		err := verdictAtSignal(pid, IdentityForTest(7), 8, nil)
		var changed *IdentityChangedError
		if !errors.As(err, &changed) {
			t.Fatalf("verdictAtSignal = %v (%T), want an *IdentityChangedError", err, err)
		}
		if changed.PID != pid {
			t.Errorf("PID = %d, want %d", changed.PID, pid)
		}
	})

	t.Run("a process that is gone is reported as gone", func(t *testing.T) {
		for _, readErr := range []error{os.ErrProcessDone, wrappedGone} {
			err := verdictAtSignal(pid, IdentityForTest(7), 0, readErr)
			if !errors.Is(err, os.ErrProcessDone) {
				t.Errorf("verdictAtSignal(%v) = %v, want errors.Is os.ErrProcessDone", readErr, err)
			}
			var changed *IdentityChangedError
			var unverifiable *UnverifiableError
			if errors.As(err, &changed) || errors.As(err, &unverifiable) {
				t.Errorf("a gone process was reported as %T: it must read as 'no longer running'", err)
			}
			if err != nil && !strings.Contains(err.Error(), "4242") {
				t.Errorf("verdictAtSignal = %q, want it to name the PID", err.Error())
			}
		}
	})

	t.Run("any other read failure is unverifiable, and the cause stays out of errors.Is", func(t *testing.T) {
		for _, readErr := range []error{os.ErrPermission, errors.New("short read"), errors.ErrUnsupported} {
			err := verdictAtSignal(pid, IdentityForTest(7), 7, readErr)
			var unverifiable *UnverifiableError
			if !errors.As(err, &unverifiable) {
				t.Fatalf("verdictAtSignal(%v) = %v (%T), want an *UnverifiableError", readErr, err, err)
			}
			if unverifiable.PID != pid {
				t.Errorf("PID = %d, want %d", unverifiable.PID, pid)
			}
			if !errors.Is(unverifiable.Cause, readErr) {
				t.Errorf("Cause = %v, want %v (kept for the log)", unverifiable.Cause, readErr)
			}
			if errors.Is(err, readErr) {
				t.Errorf("errors.Is(%v, %v) = true: the cause must not leak through Unwrap, or 'refused by the system' would be shown for a refusal that is DevDesk's own", err, readErr)
			}
		}
	})

	t.Run("a read failure wins over a matching stamp", func(t *testing.T) {
		// A read error leaves the stamp at its zero value; a want of zero must not
		// be taken for "equal".
		err := verdictAtSignal(pid, IdentityForTest(0), 0, os.ErrPermission)
		if err == nil {
			t.Error("verdictAtSignal = nil: a failed read let a signal through")
		}
	})
}

// ── The two refusals ─────────────────────────────────────────────────────────

func TestErrorWording(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unverifiable", &UnverifiableError{PID: 4242}, "Cannot verify which process PID 4242 is — not stopped"},
		{"unverifiable, with a cause", &UnverifiableError{PID: 4242, Cause: os.ErrPermission}, "Cannot verify which process PID 4242 is — not stopped"},
		{"changed", &IdentityChangedError{PID: 4242}, "PID 4242 is no longer the process that was listed — not stopped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Neither refusal wraps an operating system sentinel: they are DevDesk's own
// decisions, and the UI must not read them as "refused by the system" or "no
// longer running".
func TestTheTwoRefusalsWrapNoSentinel(t *testing.T) {
	for _, err := range []error{
		&UnverifiableError{PID: 4242, Cause: os.ErrPermission},
		&UnverifiableError{PID: 4242, Cause: os.ErrProcessDone},
		&IdentityChangedError{PID: 4242},
	} {
		for _, sentinel := range []error{os.ErrPermission, os.ErrProcessDone} {
			if errors.Is(err, sentinel) {
				t.Errorf("errors.Is(%v, %v) = true", err, sentinel)
			}
		}
	}
}
