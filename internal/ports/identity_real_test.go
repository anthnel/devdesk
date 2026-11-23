package ports

import (
	"errors"
	"os"
	"runtime"
	"testing"
)

// The real readers (DEVDESK-709, criterion 1, the system part). They read; they
// never signal.

func skipUnlessIdentityIsReadable(t *testing.T) {
	t.Helper()
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		t.Skipf("no identity reader on %s: every row there is unverifiable", runtime.GOOS)
	}
}

func TestRealIdentityOfThisProcessIsStable(t *testing.T) {
	skipUnlessIdentityIsReadable(t)

	first, err := realOS{}.identity(os.Getpid())
	if err != nil {
		t.Fatalf("reading the identity of this process: %v", err)
	}
	second, err := realOS{}.identity(os.Getpid())
	if err != nil {
		t.Fatalf("reading it again: %v", err)
	}
	if first != second {
		t.Errorf("the identity of a running process changed between two reads: %d then %d", first, second)
	}
}

// A PID above the default Linux pid_max has no process: the reader says so with
// the sentinel the UI turns into "no longer running", not with a generic error.
func TestRealIdentityOfAMissingPIDSaysGone(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the /proc reader is Linux only")
	}
	_, err := realOS{}.identity(unreachablePID)
	if !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("identity(%d) = %v, want errors.Is os.ErrProcessDone", unreachablePID, err)
	}
}

// The identity is what tells two holders of a PID apart, so a child must not
// read like its parent.
func TestRealIdentityOfAChildDiffersFromItsParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the child helper of this package is exercised on Linux only")
	}
	child := startHelperChild(t)

	own, err := realOS{}.identity(os.Getpid())
	if err != nil {
		t.Fatalf("reading the identity of this process: %v", err)
	}
	theirs, err := realOS{}.identity(child.pid)
	if err != nil {
		t.Fatalf("reading the identity of the child %d: %v", child.pid, err)
	}
	if own == theirs {
		t.Errorf("the child and its parent read the same identity %d", own)
	}
}

// The parsing is the same code path as the reader: /proc/self/stat read by hand
// and parsed must give what the reader gives.
func TestRealIdentityMatchesProcSelfStat(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/self/stat is Linux only")
	}
	raw, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Skipf("/proc is not readable here: %v", err)
	}
	want, err := parseStartTicks(raw)
	if err != nil {
		t.Fatalf("parseStartTicks(/proc/self/stat) = %v; the parser does not read a real line", err)
	}
	got, err := realOS{}.identity(os.Getpid())
	if err != nil {
		t.Fatalf("identity(self) = %v", err)
	}
	if got != want {
		t.Errorf("identity(self) = %d, /proc/self/stat says %d", got, want)
	}
}
