package ports

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// creationOf reads the CreationTime of a process handle as one 64-bit FILETIME.
func creationOf(h windows.Handle) (uint64, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return 0, fmt.Errorf("reading the creation time: %w", err)
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), nil
}

// readIdentity opens pid with the least rights that read its times. A refusal of
// the system stays readable as os.ErrPermission, and an unknown PID reads as
// os.ErrProcessDone.
func readIdentity(pid int) (uint64, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // a PID fits in a DWORD
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return 0, fmt.Errorf("opening the process: %w", os.ErrProcessDone)
		}
		return 0, fmt.Errorf("opening the process: %w", err)
	}
	defer windows.CloseHandle(h) //nolint:errcheck // nothing to do if closing fails
	return creationOf(h)
}

// heldIdentity reads the creation time on the handle p holds, the one
// os.FindProcess opened: the PID cannot be reassigned while it is open.
func heldIdentity(p *os.Process, _ int) (uint64, error) {
	var raw uint64
	var readErr error
	if err := p.WithHandle(func(h uintptr) { raw, readErr = creationOf(windows.Handle(h)) }); err != nil {
		return 0, err
	}
	return raw, readErr
}

// isNoSuchProcess tells the failure of os.FindProcess for a PID nobody holds.
func isNoSuchProcess(err error) bool { return errors.Is(err, windows.ERROR_INVALID_PARAMETER) }
