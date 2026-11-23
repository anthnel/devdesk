package ports

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// readIdentity reads the start time of pid with sysctl kern.proc.pid. No such
// process reads as os.ErrProcessDone: the kernel answers with no data, which
// x/sys reports as EIO, and the record's own PID is checked as well.
func readIdentity(pid int) (uint64, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.EIO) {
			return 0, fmt.Errorf("reading the process %d: %w", pid, os.ErrProcessDone)
		}
		return 0, err
	}
	if int(kp.Proc.P_pid) != pid {
		return 0, fmt.Errorf("reading the process %d: %w", pid, os.ErrProcessDone)
	}
	return timevalStamp(kp.Proc.P_starttime.Sec, kp.Proc.P_starttime.Usec), nil
}

// heldIdentity reads the identity of pid. macOS offers no handle on a process,
// so the comparison and the signal are two steps on a PID: a residual window,
// documented, that the user's confirmation already spans many times over.
func heldIdentity(_ *os.Process, pid int) (uint64, error) { return readIdentity(pid) }

// isNoSuchProcess never holds on macOS: os.FindProcess does not fail there.
func isNoSuchProcess(error) bool { return false }
