package ports

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

// readIdentity reads the starttime of pid from /proc/<pid>/stat. A PID with no
// process reads as os.ErrProcessDone.
func readIdentity(pid int) (uint64, error) {
	path := "/proc/" + strconv.Itoa(pid) + "/stat"
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return 0, fmt.Errorf("reading %s: %w", path, os.ErrProcessDone)
		}
		return 0, err
	}
	return parseStartTicks(data)
}

// heldIdentity reads the identity while p holds the process. On Linux 5.4 and
// later os.FindProcess opened a pidfd, so a PID reused after that cannot receive
// the signal: the stamp read here belongs to the PID's holder at this instant,
// which is p's process or a newcomer, and a newcomer reads as another stamp.
func heldIdentity(_ *os.Process, pid int) (uint64, error) { return readIdentity(pid) }

// isNoSuchProcess never holds on Linux: os.FindProcess does not fail there.
func isNoSuchProcess(error) bool { return false }
