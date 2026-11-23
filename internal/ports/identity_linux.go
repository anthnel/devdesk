package ports

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

// readStatus reads the state and the starttime of pid from /proc/<pid>/stat,
// in one read. A PID with no process reads as os.ErrProcessDone. A zombie still
// has a stat file, with state Z and its starttime kept (proc(5)); a Z leader
// whose other threads still run is alive, which the thread count tells.
func readStatus(pid int) (procStatus, error) {
	path := "/proc/" + strconv.Itoa(pid) + "/stat"
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return procStatus{}, fmt.Errorf("reading %s: %w", path, os.ErrProcessDone)
		}
		return procStatus{}, err
	}
	state, start, err := parseStat(data)
	if err != nil {
		return procStatus{}, err
	}
	threads := 0 // unknown counts as alive (isLinuxZombie)
	if state == 'Z' {
		threads, err = readThreads(pid)
		if errors.Is(err, os.ErrProcessDone) {
			return procStatus{}, err
		}
		// Any other error leaves threads at 0: the process reads as running.
	}
	return procStatus{start: start, zombie: isLinuxZombie(state, threads)}, nil
}

// readThreads reads the thread count of pid from /proc/<pid>/status. It is asked
// only for a process whose stat says Z, to tell a zombie (one thread left, the
// leader) from a live process whose main thread has exited.
func readThreads(pid int) (int, error) {
	path := "/proc/" + strconv.Itoa(pid) + "/status"
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return 0, fmt.Errorf("reading %s: %w", path, os.ErrProcessDone)
		}
		return 0, err
	}
	return parseThreads(data)
}

// readIdentity reads the starttime of pid.
func readIdentity(pid int) (uint64, error) {
	st, err := readStatus(pid)
	return st.start, err
}

// heldIdentity reads the identity while p holds the process. On Linux 5.4 and
// later os.FindProcess opened a pidfd, so a PID reused after that cannot receive
// the signal: the stamp read here belongs to the PID's holder at this instant,
// which is p's process or a newcomer, and a newcomer reads as another stamp.
func heldIdentity(_ *os.Process, pid int) (uint64, error) { return readIdentity(pid) }

// isNoSuchProcess never holds on Linux: os.FindProcess does not fail there.
func isNoSuchProcess(error) bool { return false }
