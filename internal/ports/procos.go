package ports

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"

	gproc "github.com/shirou/gopsutil/v4/process"
)

// What the package asks of the host about processes goes through these seams,
// so a test can substitute them (DEVDESK-708). They are written only by tests
// of this package, never while a Cmd runs.
var (
	selfPID           = os.Getpid
	parentPID         = os.Getppid
	goos              = runtime.GOOS
	procOS    osProcs = realOS{}
)

// osProcs is everything this package asks the operating system about a single
// process: when it was created, and to end it.
type osProcs interface {
	// createTime is the creation time in ms since the epoch. It is opaque: it
	// is only compared with another value of the same function.
	createTime(pid int) (int64, error)
	// kill is SIGKILL, or TerminateProcess on Windows.
	kill(pid int) error
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

// kill is the only place of this package that signals a process, and its first
// statement is the run-time guard (TestOnlyRealOSSendsASignal).
func (realOS) kill(pid int) error {
	refuseRealSignalUnderTest(pid)
	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding the process: %w", err)
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
