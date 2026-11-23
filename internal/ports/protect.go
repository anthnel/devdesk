package ports

import (
	"fmt"
	"strconv"
)

// Protection says why DevDesk refuses to signal a process, or that it does not
// (DEVDESK-708). The zero value is Unprotected, so a Protection nobody set
// never blocks anything.
type Protection int

const (
	// Unprotected is a process DevDesk may signal.
	Unprotected Protection = iota
	// ProtectedSelf is DevDesk's own process.
	ProtectedSelf
	// ProtectedInit is PID 1.
	ProtectedInit
	// ProtectedWindowsSystem is PID 0 or 4 on Windows (Idle and System).
	ProtectedWindowsSystem
	// ProtectedParent is the process that started DevDesk: the direct parent
	// only, never a grandparent.
	ProtectedParent
)

// The sentences shown to the user for each refusal.
const (
	ReasonSelf          = "This is DevDesk itself"
	ReasonParent        = "This process started DevDesk"
	ReasonInit          = "PID 1 is the system's init process"
	ReasonWindowsSystem = "Windows system process"
)

// Reason is the sentence to show for p, or "" for Unprotected.
func (p Protection) Reason() string {
	switch p {
	case ProtectedSelf:
		return ReasonSelf
	case ProtectedParent:
		return ReasonParent
	case ProtectedInit:
		return ReasonInit
	case ProtectedWindowsSystem:
		return ReasonWindowsSystem
	default:
		return ""
	}
}

// hostView is what protectionFor needs to know about the host. parentBornFirst
// is a function so the creation time, which costs a system call, is read only
// when the PID asked about is the parent's.
type hostView struct {
	self, parent    int
	windows         bool
	parentBornFirst func() bool
}

// protectionFor is the rule, free of any system call. The order matters when
// one PID qualifies twice: DevDesk itself first, then PID 1 (also a parent
// inside a container), then the Windows system PIDs, then the parent. A parent
// PID of 0 means unknown (the parent lives outside this PID namespace) and
// never matches.
func protectionFor(pid int, h hostView) Protection {
	switch {
	case pid == h.self:
		return ProtectedSelf
	case pid == 1:
		return ProtectedInit
	case h.windows && (pid == 0 || pid == 4):
		return ProtectedWindowsSystem
	case h.parent > 0 && pid == h.parent && h.parentBornFirst():
		return ProtectedParent
	default:
		return Unprotected
	}
}

// parentStartedFirst reports whether parent was created no later than self. A
// parent born after DevDesk is a process that reused the PID of the real one,
// which exited. Anything unreadable, and a tie, counts as true: when in doubt
// the parent is protected.
func parentStartedFirst(parent, self int) bool {
	parentCreated, err := procOS.createTime(parent)
	if err != nil {
		return true
	}
	selfCreated, err := procOS.createTime(self)
	if err != nil {
		return true
	}
	return parentCreated <= selfCreated
}

// protectionOf feeds protectionFor from the seams.
func protectionOf(n int) Protection {
	self, parent := selfPID(), parentPID()
	return protectionFor(n, hostView{
		self:            self,
		parent:          parent,
		windows:         goos == "windows",
		parentBornFirst: func() bool { return parentStartedFirst(parent, self) },
	})
}

// parsePID reads a PID in the one form DevDesk accepts: canonical decimal that
// fits in 32 bits. The kernel keeps only 32 bits (pid_t on Unix, a DWORD on
// Windows), so a wider number would reach a different process than the one
// checked: "4294967295" would become kill(-1), "4294967297" PID 1. Kill is the
// exported border, so it is checked here, once, for Kill and ProtectionOf.
// Signs, leading zeros and spaces are refused: the text must be what
// strconv.Itoa prints.
func parsePID(pid string) (int, bool) {
	n, err := strconv.ParseInt(pid, 10, 32)
	if err != nil || strconv.Itoa(int(n)) != pid {
		return 0, false
	}
	return int(n), true
}

// ProtectionOf says whether DevDesk refuses to signal the process pid. An
// empty, non-numeric, out-of-range or non-canonical pid is Unprotected: Kill
// refuses it as not a process id, so there is nothing to protect. It runs on
// every render, hence the lazy creation-time read.
func ProtectionOf(pid string) Protection {
	n, ok := parsePID(pid)
	if !ok {
		return Unprotected
	}
	return protectionOf(n)
}

// ProtectedError is the refusal Kill returns for a protected process. It is
// not an operating system error: it does not wrap os.ErrPermission.
type ProtectedError struct {
	PID        int
	Protection Protection
}

func (e *ProtectedError) Error() string {
	return fmt.Sprintf("PID %d not stopped: %s", e.PID, e.Protection.Reason())
}
