// Package ports reads the machine's socket table, and ends a process holding
// one.
//
// It answers for **this machine**. That is the whole reason it exists: the
// Ports tab used to run `ss` inside `docker run --rm --net=host --pid=host
// --privileged`, and on Docker Desktop `--net=host` is the namespace of the
// Linux VM, not of the host. The tab therefore listed the VM's sockets — its
// NFS daemons, with two-digit PIDs — while not one of the host's listening
// sockets appeared, and `K` killed a process of the VM under the impression it
// was freeing a port on the machine (D55).
//
// The same sentence has been written at the top of `docker.runDiagHost` since
// §3.33 rapatriated DNS, ICMP, TCP, TLS and HTTP for exactly this reason.
// `RunSS` and `KillProcess` were the two that stayed behind.
//
// Nothing here starts a container, and nothing here needs Docker: the Ports tab
// now works on a machine that has none.
package ports

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	gnet "github.com/shirou/gopsutil/v4/net"
	gproc "github.com/shirou/gopsutil/v4/process"
)

// Socket is one row of the table: a socket, and the process holding it.
//
// Every field but Identity is a string because it is displayed, and the PID
// in particular is what the table is keyed on — an int would have to be
// formatted at each of the sites that compare it.
type Socket struct {
	Protocol  string // tcp or udp, whichever family the address is on
	State     string // LISTEN, ESTABLISHED, UNCONN, TIME_WAIT…
	LocalAddr string
	PeerAddr  string
	PID       string // empty when no process could be attributed
	Process   string
	// Identity is not displayed: it is what Kill verifies the PID against. It is
	// unknown when PID is empty or the process could not be read.
	Identity Identity
}

// List returns the machine's TCP and UDP sockets.
//
// resolve asks for the addresses to be reported as host names where reverse DNS
// answers; see resolveAddrs for what that does and does not cover.
func List(ctx context.Context, resolve bool) ([]Socket, error) {
	// The instant is taken before the snapshot, never after: on Windows a process
	// created later than it is not one of the rows (see attachIdentities).
	listedAt := now()
	conns, err := gnet.ConnectionsWithContext(ctx, "inet")
	if err != nil {
		return nil, fmt.Errorf("reading the socket table: %w", err)
	}

	names := processNames(ctx)

	sockets := make([]Socket, 0, len(conns))
	for _, c := range conns {
		sockets = append(sockets, toSocket(c, names))
	}
	attachIdentities(sockets, listedAt)

	if resolve {
		resolveAddrs(ctx, sockets)
	}
	return sockets, nil
}

// toSocket turns one connection into a row.
func toSocket(c gnet.ConnectionStat, names map[int32]string) Socket {
	s := Socket{
		Protocol:  protocolOf(c.Type),
		State:     stateOf(c.Status, c.Type),
		LocalAddr: formatAddr(c.Laddr, c.Family),
		PeerAddr:  formatAddr(c.Raddr, c.Family),
	}
	// A PID of zero is not a process: it is the system declining to attribute
	// the socket to one, and `K` keys on this field. Left as "0" it would give
	// the row something that looks killable, and Kill would then be asked to
	// signal a process id that means "every process in the group" on Unix.
	if c.Pid > 0 {
		s.PID = strconv.FormatInt(int64(c.Pid), 10)
		s.Process = names[c.Pid]
	}
	return s
}

// Kill sends SIGKILL — TerminateProcess on Windows — to the process holding a
// socket.
//
// It replaces `docker run --pid=host --privileged <image> kill -9 <pid>`, which
// on Docker Desktop signalled a process of the VM (D55). The consequence worth
// knowing is that DevDesk now signals with the rights it actually has: another
// user's process, or a service, comes back as a refusal from the operating
// system rather than as a success against the wrong machine.
//
// The signal leaves through procOS.kill, the single point of the package that
// sends one; in a test binary it panics unless the test substituted procOS
// (DEVDESK-708).
//
// DevDesk itself, its direct parent, PID 1 and, on Windows, PIDs 0 and 4 are
// refused with a *ProtectedError before anything is sent. A pid outside int32
// or not in canonical form is refused first, as "not a process id" (parsePID).
// The protection check comes
// before the n <= 0 one because PID 0 is a protected process on Windows.
//
// listed is the identity the row carried (DEVDESK-709), after ConfirmIdentity
// for a deferred one. Anything but a known identity is refused with an
// *UnverifiableError, and the signal goes only to the process that has it: if
// the PID is held by another one the answer is an *IdentityChangedError.
func Kill(pid string, listed Identity) error {
	n, err := signalTarget(pid, listed)
	if err != nil {
		return err
	}
	if err := procOS.kill(n, listed, signalKill); err != nil {
		return fmt.Errorf("killing process %s: %w", pid, err)
	}
	return nil
}

// ErrNoCleanStop is what Terminate answers on Windows, which has no SIGTERM: the
// only way to end a process there is TerminateProcess, which Kill does. It is
// DevDesk's own answer, not an OS error: it wraps no sentinel.
var ErrNoCleanStop = errors.New("no clean stop on Windows")

// Terminate asks the process holding a socket to stop, with SIGTERM, and returns
// as soon as the signal is sent: it waits for nothing (DEVDESK-711). Probe says
// afterwards whether the process left; Kill is the forced stop.
//
// It runs the same checks as Kill, in the same order — protection, then
// identity, then the signal through the single procOS.kill, which verifies the
// identity again on the held process — so a graceful stop is never less careful
// than a forced one. On Windows it answers ErrNoCleanStop after the protection,
// so the row of DevDesk itself still says what it is.
func Terminate(pid string, listed Identity) error {
	n, err := signalTarget(pid, listed)
	if err != nil {
		return err
	}
	if goos == "windows" {
		return ErrNoCleanStop
	}
	if err := procOS.kill(n, listed, signalTerm); err != nil {
		return fmt.Errorf("stopping process %s: %w", pid, err)
	}
	return nil
}

// signalTarget is what Kill and Terminate check before anything is sent: the PID
// is a process id, it is not protected, and the identity is known. It returns
// the PID as a number.
func signalTarget(pid string, listed Identity) (int, error) {
	n, ok := parsePID(pid)
	if !ok {
		return 0, fmt.Errorf("not a process id: %q", pid)
	}
	if p := protectionOf(n); p != Unprotected {
		return 0, &ProtectedError{PID: n, Protection: p}
	}
	if n <= 0 {
		return 0, fmt.Errorf("not a process id: %q", pid)
	}
	if listed.kind != identityKnown {
		return 0, &UnverifiableError{PID: n}
	}
	return n, nil
}

// processNames maps PID to process name, from **one** enumeration.
//
// The obvious implementation — process.NewProcess(pid).Name() per socket — is
// the one that does not work. On Windows that path goes through OpenProcess and
// QueryFullProcessImageName, which needs rights over the target: measured on
// this machine, 98 of 189 sockets came back "Access denied", and the Process
// column would have been empty for every service on the box. Processes() reads
// a Toolhelp32 snapshot instead and named all 294 processes in 11 ms, elevated
// or not.
//
// So the bulk enumeration is not an optimisation over the per-PID call. It is
// the difference between a column that is filled in and one that is not.
//
// A failure here is not a failure of the listing: a table of sockets with no
// process names still says which ports are open, and that is most of what it is
// read for.
func processNames(ctx context.Context) map[int32]string {
	procs, err := gproc.ProcessesWithContext(ctx)
	if err != nil {
		return nil
	}
	names := make(map[int32]string, len(procs))
	for _, p := range procs {
		if name, err := p.NameWithContext(ctx); err == nil && name != "" {
			names[p.Pid] = name
		}
	}
	return names
}

// protocolOf reports tcp or udp. Both families print the same word, as `ss`
// does: the address beside it already says which one it is on.
func protocolOf(sockType uint32) string {
	switch sockType {
	case 1: // SOCK_STREAM
		return "tcp"
	case 2: // SOCK_DGRAM
		return "udp"
	default:
		return ""
	}
}

// stateOf normalises the state, and neither substitution is cosmetic.
//
// An unconnected UDP socket is reported as "NONE" on Linux and as nothing at all
// on Windows, so the same socket read differently depending on where DevDesk was
// running. UNCONN is what the table showed when `ss` produced it, and it is what
// the socket is.
//
// ESTABLISHED is shortened to ESTAB for the reason `ss` shortens it: the State
// column is ten cells wide, so the long spelling is truncated to "ESTABLISHE" —
// and the filter token the user toggles with `e` reads better short.
func stateOf(status string, sockType uint32) string {
	if sockType == 2 && (status == "" || status == "NONE") {
		return "UNCONN"
	}
	if status == "ESTABLISHED" {
		return "ESTAB"
	}
	return status
}

// formatAddr renders an endpoint the way the table has always shown one:
// host:port, bracketed for IPv6, and the wildcard form for the peer of a socket
// that has none.
func formatAddr(a gnet.Addr, family uint32) string {
	if a.IP == "" && a.Port == 0 {
		if isIPv6(family) {
			return "[::]:*"
		}
		return "0.0.0.0:*"
	}
	return net.JoinHostPort(a.IP, strconv.FormatUint(uint64(a.Port), 10))
}

// isIPv6 reports whether the address family is AF_INET6. The constant differs
// between platforms — 23 on Windows, 10 on Linux, 30 on Darwin — and gopsutil
// passes through whichever the kernel used, so all three are named here rather
// than compared against one.
func isIPv6(family uint32) bool {
	return family == 23 || family == 10 || family == 30
}
