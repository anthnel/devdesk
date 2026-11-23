package ports

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	gproc "github.com/shirou/gopsutil/v4/process"
)

// What the package asks of the host about processes goes through these seams,
// so a test can substitute them (DEVDESK-708). They are written only by tests
// of this package, never while a Cmd runs.
var (
	selfPID           = os.Getpid
	parentPID         = os.Getppid
	goos              = runtime.GOOS
	now               = time.Now // the listing instant and the Windows reading budget
	procOS    osProcs = realOS{}
)

// stopSignal is what realOS.kill sends. The zero value names no signal and is
// refused, so a caller that forgets it cannot fall through to a default of
// "send SIGKILL".
type stopSignal int

const (
	// signalTerm is SIGTERM. It is never sent on Windows: Terminate stops at
	// ErrNoCleanStop there.
	signalTerm stopSignal = iota + 1
	// signalKill is SIGKILL, or TerminateProcess on Windows.
	signalKill
)

// procStatus is one read of a process: its identity stamp and whether it is a
// zombie, from a single read of the OS.
type procStatus struct {
	start  uint64
	zombie bool // Linux state Z or X; macOS P_stat == SZOMB
}

// osProcs is everything this package asks the operating system about a single
// process: when it was created, which process it is, what state it is in, and to
// end it.
type osProcs interface {
	// createTime is the creation time in ms since the epoch. It is opaque: it
	// is only compared with another value of the same function.
	createTime(pid int) (int64, error)
	// identity is the raw creation stamp of the process holding pid, in the unit
	// of the OS. It wraps os.ErrProcessDone when there is no such process.
	identity(pid int) (uint64, error)
	// probe reads, without signalling, the state of the process holding pid. It
	// wraps os.ErrProcessDone when no process holds it.
	probe(pid int) (procStatus, error)
	// kill sends sig (SIGTERM, or SIGKILL / TerminateProcess on Windows) to the
	// process whose identity is want, and to no other.
	kill(pid int, want Identity, sig stopSignal) error
}

// realOS is the production osProcs.
type realOS struct{}

func (realOS) createTime(pid int) (int64, error) {
	p, err := gproc.NewProcess(int32(pid)) //nolint:gosec // a PID fits in int32 on every supported OS
	if err != nil {
		return 0, err
	}
	return p.CreateTime()
}

func (realOS) identity(pid int) (uint64, error) { return readIdentity(pid) }

// probe reads the state and the stamp of pid in one read; it sends nothing.
func (realOS) probe(pid int) (procStatus, error) { return readStatus(pid) }

// kill is the only place of this package that signals a process, and its first
// statement is the run-time guard (TestOnlyRealOSSendsASignal). It sends SIGTERM
// or SIGKILL through the same checks, so a graceful stop and a forced one are
// verified identically (DEVDESK-711).
//
// The process is held first (a pidfd on Linux 5.4 and later, a handle on
// Windows), then its identity is read and compared, then the signal leaves
// through the same held process: the PID cannot change hands in between. Without
// a held process (macOS, an older Linux) a window remains between the
// comparison and the signal; heldIdentity says how the identity is read.
func (realOS) kill(pid int, want Identity, sig stopSignal) error {
	refuseRealSignalUnderTest(pid)
	if sig != signalTerm && sig != signalKill {
		return fmt.Errorf("no signal named (%d)", sig)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		if isNoSuchProcess(err) {
			return fmt.Errorf("opening the process: %w", os.ErrProcessDone)
		}
		return fmt.Errorf("opening the process: %w", err)
	}
	defer p.Release() //nolint:errcheck // the only effect is releasing a descriptor
	got, readErr := heldIdentity(p, pid)
	if err := verdictAtSignal(pid, want, got, readErr); err != nil {
		return err
	}
	if sig == signalTerm {
		return p.Signal(syscall.SIGTERM) // pidfd_send_signal on Linux 5.4 and later
	}
	return p.Kill()
}

// realSignalsAllowed is flipped only by allowRealSignals(t) in the _test.go
// files of this package.
var realSignalsAllowed atomic.Bool

// refuseRealSignalUnderTest panics in a test binary that did not allow real
// signals. It panics rather than returning an error: an error would be shown as
// an ordinary failed kill and the test could pass, leaving the guard silent.
func refuseRealSignalUnderTest(pid int) {
	if testing.Testing() && !realSignalsAllowed.Load() {
		panic(fmt.Sprintf("ports: a test reached a real signal to PID %d without a substituted procOS; "+
			"replace procOS, or call allowRealSignals(t) for a child the test started "+
			"(DEVDESK-708, docs/architecture/network.md)", pid))
	}
}
