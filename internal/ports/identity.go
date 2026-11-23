package ports

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"
	"time"
)

// Identity tells one process from a later one that got the same PID
// (DEVDESK-709). It is the creation stamp in the operating system's own unit,
// read once per PID when the table is listed. It is opaque: the interface only
// asks Verifiable, and this package compares. The zero value is "unknown", which
// Kill refuses, so a caller that forgets the identity cannot send a signal.
type Identity struct {
	kind identityKind
	raw  uint64
}

type identityKind uint8

const (
	// identityNone is the zero value: not read, or unreadable.
	identityNone identityKind = iota
	// identityKnown carries the creation stamp, in the unit of the OS.
	identityKnown
	// identityDeferred is Windows only: the listing stopped reading because its
	// budget ran out. raw is the listing instant (Unix ns); the stamp is read
	// when the confirmation opens (ConfirmIdentity).
	identityDeferred
)

// The Windows budget and slack (S-01b criterion 7, hypothesis H1).
const (
	// windowsIdentityBudget caps what reading identities adds to one refresh on
	// Windows. Past it the remaining PIDs are deferred. The starting value of the
	// story: adjust it from docs/measurements.
	windowsIdentityBudget = 100 * time.Millisecond
	// windowsBirthSlack is the granularity allowed between a creation time and the
	// listing instant before a process counts as born before the listing.
	windowsBirthSlack = 100 * time.Millisecond
)

// Verifiable reports whether a stop can still be attempted for the row: the
// identity is known, or deferred and read when the confirmation opens.
func (i Identity) Verifiable() bool { return i.kind != identityNone }

// Conflicts reports whether two identities say, with certainty, that they are
// two different processes: both known and different. An unknown or deferred one
// never conflicts.
func (i Identity) Conflicts(j Identity) bool {
	return i.kind == identityKnown && j.kind == identityKnown && i.raw != j.raw
}

// deferredAt is the identity of a PID not read at the listing taken at listedAt.
func deferredAt(listedAt time.Time) Identity {
	return Identity{kind: identityDeferred, raw: uint64(listedAt.UnixNano())} //nolint:gosec // listing instants are after 1970
}

// IdentityForTest builds a known identity for the fixtures of a test. It panics
// outside a test binary.
func IdentityForTest(raw uint64) Identity {
	mustBeUnderTest("IdentityForTest")
	return Identity{kind: identityKnown, raw: raw}
}

// DeferredIdentityForTest builds a deferred identity, as a Windows listing does
// for a PID it did not read in time. It panics outside a test binary.
func DeferredIdentityForTest(listedAt time.Time) Identity {
	mustBeUnderTest("DeferredIdentityForTest")
	return deferredAt(listedAt)
}

func mustBeUnderTest(name string) {
	if !testing.Testing() {
		panic("ports: " + name + " is reserved for tests (DEVDESK-709)")
	}
}

// UnverifiableError is the refusal when DevDesk cannot tell which process holds
// the PID. It is DevDesk's own decision, not the system's: it wraps no OS
// sentinel, so the interface cannot read it as "refused by the system".
type UnverifiableError struct {
	PID   int
	Cause error // for the log only; deliberately not returned by Unwrap
}

func (e *UnverifiableError) Error() string {
	return fmt.Sprintf("Cannot verify which process PID %d is — not stopped", e.PID)
}

// IdentityChangedError is the refusal when the PID is held by another process
// than the one that was listed.
type IdentityChangedError struct{ PID int }

func (e *IdentityChangedError) Error() string {
	return fmt.Sprintf("PID %d is no longer the process that was listed — not stopped", e.PID)
}

// parseStat reads the state (field 3) and the starttime (field 22, ticks since
// boot) of a /proc/<pid>/stat line in one pass. The command name (field 2) sits
// between parentheses and may hold spaces and parentheses itself, so the fields
// are counted from the last ")".
func parseStat(stat []byte) (state byte, start uint64, err error) {
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, 0, errors.New("no closing parenthesis in the stat line")
	}
	// Fields 3 onward follow the name: the state is the first of them, field 22
	// the 20th.
	const startIndex = 22 - 3
	fields := bytes.Fields(stat[end+1:])
	if len(fields) <= startIndex {
		return 0, 0, fmt.Errorf("the stat line stops at field %d, before starttime (22)", len(fields)+2)
	}
	ticks, err := strconv.ParseUint(string(fields[startIndex]), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("reading starttime from the stat line: %w", err)
	}
	return fields[0][0], ticks, nil
}

// parseStartTicks reads the starttime of a /proc/<pid>/stat line.
func parseStartTicks(stat []byte) (uint64, error) {
	_, ticks, err := parseStat(stat)
	return ticks, err
}

// isLinuxZombie reports whether the process has exited, from field 3 of its
// /proc/<pid>/stat and its thread count. X is dead. Z is the state of the
// thread-group leader once it has exited, but the leader reads Z as well when
// only the main thread called pthread_exit and the other threads run on: that
// process is alive. So Z counts as a zombie only when the leader is the one
// thread left (threads == 1); a count above 1, or one that could not be read
// (threads <= 0), says alive, the safe side.
func isLinuxZombie(state byte, threads int) bool {
	switch state {
	case 'X':
		return true
	case 'Z':
		return threads == 1
	}
	return false
}

// parseThreads reads the "Threads:" line of a /proc/<pid>/status.
func parseThreads(status []byte) (int, error) {
	for line := range bytes.Lines(status) {
		value, ok := bytes.CutPrefix(line, []byte("Threads:"))
		if !ok {
			continue
		}
		n, err := strconv.Atoi(string(bytes.TrimSpace(value)))
		if err != nil || n < 1 {
			return 0, fmt.Errorf("reading the Threads line of the status file: %q", bytes.TrimSpace(line))
		}
		return n, nil
	}
	return 0, errors.New("no Threads line in the status file")
}

// darwinSZOMB is P_stat of a zombie in <sys/proc.h> (SIDL 1, SRUN 2, SSLEEP 3,
// SSTOP 4, SZOMB 5); x/sys/unix exports no constant for it.
const darwinSZOMB = 5

// isDarwinZombie reports whether a kinfo_proc P_stat is SZOMB.
func isDarwinZombie(pStat int8) bool { return pStat == darwinSZOMB }

// timevalStamp is a macOS process start (seconds and microseconds) as one value.
func timevalStamp(sec int64, usec int32) uint64 {
	return uint64(sec)*1_000_000 + uint64(usec) //nolint:gosec // a start time is positive
}

// unixAsFiletime is the Unix epoch as a Windows FILETIME (100 ns since 1601).
const unixAsFiletime = 116444736000000000

// maxFiletimeTicks is the largest count of 100 ns ticks after the Unix epoch
// whose nanoseconds still fit in an int64.
const maxFiletimeTicks = math.MaxInt64 / 100

// filetimeUnixNano turns a Windows FILETIME into Unix nanoseconds. A value
// before the Unix epoch or past what an int64 of nanoseconds holds is not a
// creation time DevDesk can compare: it comes back false, an unknown identity,
// rather than a wrapped-around instant.
func filetimeUnixNano(raw uint64) (int64, bool) {
	if raw < unixAsFiletime {
		return 0, false
	}
	ticks := raw - unixAsFiletime
	if ticks > maxFiletimeTicks {
		return 0, false
	}
	return int64(ticks) * 100, true //nolint:gosec // bounded by maxFiletimeTicks above
}

// birth says where a Windows creation time falls against the listing instant.
type birth int

const (
	// bornBefore: the process existed at the listing, with the slack to spare.
	bornBefore birth = iota
	// bornAround: too close to the listing instant to tell.
	bornAround
	// bornAfter: the process did not exist at the listing.
	bornAfter
)

// judgeBirth places a creation time (Unix ns) against the listing instant
// (Unix ns). The slack absorbs the granularity of the creation time and of the
// clock.
func judgeBirth(createdNs, listedAtNs int64, slack time.Duration) birth {
	switch {
	case createdNs <= listedAtNs-int64(slack):
		return bornBefore
	case createdNs > listedAtNs:
		return bornAfter
	default:
		return bornAround
	}
}

// verdictAtSignal is the decision realOS.kill applies to what it read with the
// process held: nil means send. A failed read never sends, whatever the stamp.
func verdictAtSignal(pid int, want Identity, got uint64, readErr error) error {
	if readErr != nil {
		if errors.Is(readErr, os.ErrProcessDone) {
			return fmt.Errorf("process %d: %w", pid, os.ErrProcessDone)
		}
		return &UnverifiableError{PID: pid, Cause: readErr}
	}
	if want.kind != identityKnown {
		return &UnverifiableError{PID: pid}
	}
	if got != want.raw {
		return &IdentityChangedError{PID: pid}
	}
	return nil
}

// ConfirmIdentity gives the identity a stop of the row must be verified
// against. A known identity comes back as it is, with no system call; a
// deferred one (Windows, over the listing budget) is read now and anchored to
// the listing instant; an unknown one is refused. The errors are those of Kill:
// an OS refusal or a gone process stays readable through errors.Is, DevDesk's
// own refusals are an *UnverifiableError and an *IdentityChangedError.
func ConfirmIdentity(pid string, listed Identity) (Identity, error) {
	n, ok := parsePID(pid)
	if !ok || n <= 0 {
		return Identity{}, fmt.Errorf("not a process id: %q", pid)
	}
	switch listed.kind {
	case identityKnown:
		return listed, nil
	case identityDeferred:
		return confirmDeferred(n, listed)
	default:
		return Identity{}, &UnverifiableError{PID: n}
	}
}

// confirmDeferred reads the stamp of a PID the listing did not read, and keeps
// it only if the process was born before the listing.
func confirmDeferred(n int, listed Identity) (Identity, error) {
	raw, err := procOS.identity(n)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrProcessDone) {
			return Identity{}, fmt.Errorf("reading the identity of process %d: %w", n, err)
		}
		return Identity{}, &UnverifiableError{PID: n, Cause: err}
	}
	createdNs, ok := filetimeUnixNano(raw)
	if !ok {
		return Identity{}, &UnverifiableError{PID: n}
	}
	switch judgeBirth(createdNs, int64(listed.raw), windowsBirthSlack) { //nolint:gosec // a listing instant fits in int64
	case bornBefore:
		return Identity{kind: identityKnown, raw: raw}, nil
	case bornAfter:
		return Identity{}, &IdentityChangedError{PID: n}
	default:
		return Identity{}, &UnverifiableError{PID: n}
	}
}
