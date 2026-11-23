package ports

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
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

// osProcs is everything this package asks the operating system about a single
// process: when it was created, which process it is, and to end it.
type osProcs interface {
	// createTime is the creation time in ms since the epoch. It is opaque: it
	// is only compared with another value of the same function.
	createTime(pid int) (int64, error)
	// identity is the raw creation stamp of the process holding pid, in the unit
	// of the OS. It wraps os.ErrProcessDone when there is no such process.
	identity(pid int) (uint64, error)
	// kill is SIGKILL, or TerminateProcess on Windows. It signals only the
	// process whose identity is want.
	kill(pid int, want Identity) error
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

// kill is the only place of this package that signals a process, and its first
// statement is the run-time guard (TestOnlyRealOSSendsASignal).
//
// The process is held first (a pidfd on Linux 5.4 and later, a handle on
// Windows), then its identity is read and compared, then the signal leaves
// through the same held process: the PID cannot change hands in between. Without
// a held process (macOS, an older Linux) a window remains between the
// comparison and the signal; heldIdentity says how the identity is read.
func (realOS) kill(pid int, want Identity) error {
	refuseRealSignalUnderTest(pid)
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
