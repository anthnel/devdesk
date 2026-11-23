package ports

import (
	"errors"
	"fmt"
	"os"
)

// Liveness is what Probe says of the process a row was listed with.
type Liveness int

const (
	// ProcessRunning is the zero value on purpose: a probe that could not read
	// anything counts the process as still there, never as stopped.
	ProcessRunning Liveness = iota // the listed process still holds the PID
	ProcessZombie                  // it exited and its parent has not reaped it yet
	ProcessExited                  // no process holds the PID, or another one does
)

// Stopped reports whether the listed process is gone for good: exited, or a
// zombie, which holds a PID but runs nothing.
func (l Liveness) Stopped() bool { return l != ProcessRunning }

// String is for logs only.
func (l Liveness) String() string {
	switch l {
	case ProcessRunning:
		return "running"
	case ProcessZombie:
		return "zombie"
	case ProcessExited:
		return "exited"
	default:
		return fmt.Sprintf("Liveness(%d)", int(l))
	}
}

// Probe says, without signalling anything, what became of the process a row was
// listed with (DEVDESK-711): still running, a zombie, or gone. One read gives
// both the state and the identity stamp, and the stamp is compared with listed,
// so a PID taken by another process reads as exited.
//
// A read that fails for another reason than "no such process" returns
// ProcessRunning with the error: an unreadable probe is never taken for a stop.
// Windows has no probe (errors.ErrUnsupported): it has no clean stop to watch.
func Probe(pid string, listed Identity) (Liveness, error) {
	n, ok := parsePID(pid)
	if !ok || n <= 0 {
		return ProcessRunning, fmt.Errorf("not a process id: %q", pid)
	}
	if listed.kind != identityKnown {
		return ProcessRunning, &UnverifiableError{PID: n}
	}
	if goos == "windows" {
		return ProcessRunning, fmt.Errorf("probing process %d: %w", n, errors.ErrUnsupported)
	}
	st, err := procOS.probe(n)
	return livenessOf(listed, st, err)
}

// livenessOf is the decision of Probe, apart from the read.
func livenessOf(listed Identity, st procStatus, err error) (Liveness, error) {
	if err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return ProcessExited, nil
		}
		return ProcessRunning, fmt.Errorf("probing the process: %w", err)
	}
	if st.start != listed.raw {
		return ProcessExited, nil // the PID changed hands: the listed process is gone
	}
	if st.zombie {
		return ProcessZombie, nil
	}
	return ProcessRunning, nil
}
