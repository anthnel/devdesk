package netdiag

import (
	"context"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ports"
)

// NewPortsDataMsg hands socket rows to the Ports view without reading the real
// socket table (guard seed, DEVDESK-150 H8).
func NewPortsDataMsg(sockets []ports.Socket) tea.Msg { return portsDataMsg{ports: sockets} }

// defaultPortsRefresh is what the interval falls back to. A non-positive one —
// a hand-edited 0 in the config — would make tea.Tick fire without pausing and
// spin a docker exec per frame, so it is refused here rather than trusted.
const defaultPortsRefresh = 2 * time.Second

func portsTickCmd(every time.Duration) tea.Cmd {
	if every <= 0 {
		every = defaultPortsRefresh
	}
	return tea.Tick(every, func(time.Time) tea.Msg {
		return portsTickMsg{}
	})
}

// portsFetchTimeout bounds one read. Reverse DNS is the part that can hang —
// the socket table itself is a system call — and a fetch outliving several
// ticks would stack one behind another.
const portsFetchTimeout = 5 * time.Second

func fetchPortsCmd(numeric bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), portsFetchTimeout)
		defer cancel()
		sockets, err := ports.List(ctx, !numeric)
		return portsDataMsg{ports: sockets, err: err}
	}
}

// processOps is what the Ports tab does to the machine's processes, held in
// one value so a test can replace it and no test signals a real process.
type processOps struct {
	// protection says whether DevDesk refuses to signal the process with this
	// PID. It is read in Update and at render, never in a Cmd (Rule 110).
	protection func(pid string) ports.Protection
	// confirm settles which process a row is when its confirmation opens: it
	// hands back the identity the signal will be verified against, or the reason
	// DevDesk cannot tell. For a row listed with an identity it reads nothing;
	// only a deferred one (Windows, over the listing budget) costs a system
	// call, in Update and at the press of K (DEVDESK-709).
	confirm func(pid string, listed ports.Identity) (ports.Identity, error)
	// terminate asks the process with this PID to stop (SIGTERM), provided it
	// still is the process with the identity the row was listed with. Linux and
	// macOS only; Windows never calls it (DEVDESK-711).
	terminate func(pid string, listed ports.Identity) error
	// probe reads, without signalling, whether the process the row was listed
	// with still runs. It is called from the tick of a stop, never from Update or
	// at render.
	probe func(pid string, listed ports.Identity) (ports.Liveness, error)
	// kill ends the process with this PID, provided it still is the process
	// with the identity the row was listed with.
	kill func(pid string, listed ports.Identity) error
	// windows says the platform has no clean stop, which changes the wording
	// of the confirmation and of the help, nothing else.
	windows bool
}

// realProcessOps is the production wiring: ports.Terminate, ports.Probe and
// ports.Kill on the platform the binary runs on.
func realProcessOps() processOps {
	return processOps{
		protection: ports.ProtectionOf,
		confirm:    ports.ConfirmIdentity,
		terminate:  ports.Terminate,
		probe:      ports.Probe,
		kill:       ports.Kill,
		windows:    runtime.GOOS == "windows",
	}
}

// terminateCmd sends the SIGTERM of the stop of generation gen. The function and
// the target are values captured in Update (Rule 110): the command never reads
// the model. at is read after the call, so the grace runs from the moment the
// signal returned.
func terminateCmd(terminate func(string, ports.Identity) error, t killTarget, gen uint64) tea.Cmd {
	return func() tea.Msg {
		err := terminate(t.PID, t.Identity)
		return portsTermResultMsg{gen: gen, pid: t.PID, process: t.Process, at: time.Now(), err: err}
	}
}

// stopProbeCmd waits stopProbeEvery, then probes the target once. It is a
// tick, not a sleep: the answer carries the tick instant the deadline is
// compared with. Values only (Rule 110); the probe signals nothing.
func stopProbeCmd(probe func(string, ports.Identity) (ports.Liveness, error), t killTarget, gen uint64) tea.Cmd {
	return tea.Tick(stopProbeEvery, func(at time.Time) tea.Msg {
		l, err := probe(t.PID, t.Identity)
		return portsStopProbeMsg{gen: gen, at: at, liveness: l, err: err}
	})
}

// killProcessCmd signals the target through kill. Both are values captured in
// Update (Rule 110): the command never reads the model. gen is the generation of
// the two-step stop the kill closes, or 0 for a Windows kill that has none.
func killProcessCmd(kill func(string, ports.Identity) error, t killTarget, gen uint64) tea.Cmd {
	return func() tea.Msg {
		return portsKillResultMsg{gen: gen, pid: t.PID, process: t.Process, err: kill(t.PID, t.Identity)}
	}
}

// portsRefreshMsg asks for one read of the socket table now, outside the tick
// chain: a second portsTickMsg would start a second chain (DEVDESK-709).
type portsRefreshMsg struct{}

// requestPortsRefresh returns the command that delivers a portsRefreshMsg. It
// reads nothing itself; Update turns the message into the read.
func requestPortsRefresh() tea.Cmd {
	return func() tea.Msg { return portsRefreshMsg{} }
}
