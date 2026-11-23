package ports

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// readStatus reads the start time and the state of pid with sysctl
// kern.proc.pid, in one read. No such process reads as os.ErrProcessDone: the
// kernel answers with no data, which x/sys reports as EIO, and the record's own
// PID is checked as well. A zombie reads as P_stat == SZOMB; if the kernel does
// not return it at all it reads as gone, and either way the stop counts.
func readStatus(pid int) (procStatus, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.EIO) {
			return procStatus{}, fmt.Errorf("reading the process %d: %w", pid, os.ErrProcessDone)
		}
		return procStatus{}, err
	}
	if int(kp.Proc.P_pid) != pid {
		return procStatus{}, fmt.Errorf("reading the process %d: %w", pid, os.ErrProcessDone)
	}
	return procStatus{
		start:  timevalStamp(kp.Proc.P_starttime.Sec, kp.Proc.P_starttime.Usec),
		zombie: isDarwinZombie(kp.Proc.P_stat),
	}, nil
}

// readIdentity reads the start time of pid.
func readIdentity(pid int) (uint64, error) {
	st, err := readStatus(pid)
	return st.start, err
}

// heldIdentity reads the identity of pid. macOS offers no handle on a process,
// so the comparison and the signal are two steps on a PID: a residual window,
// documented, that the user's confirmation already spans many times over.
func heldIdentity(_ *os.Process, pid int) (uint64, error) { return readIdentity(pid) }

// isNoSuchProcess never holds on macOS: os.FindProcess does not fail there.
func isNoSuchProcess(error) bool { return false }
