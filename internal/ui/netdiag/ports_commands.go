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
	// kill ends the process with this PID.
	kill func(pid string) error
	// windows says the platform has no clean stop, which changes the wording
	// of the confirmation and of the help, nothing else.
	windows bool
}

// realProcessOps is the production wiring: ports.Kill on the platform the
// binary runs on.
func realProcessOps() processOps {
	return processOps{protection: ports.ProtectionOf, kill: ports.Kill, windows: runtime.GOOS == "windows"}
}

// killProcessCmd signals pid through kill. Both are values captured in
// Update (Rule 110): the command never reads the model.
func killProcessCmd(kill func(string) error, pid string) tea.Cmd {
	return func() tea.Msg {
		return portsKillResultMsg{pid: pid, err: kill(pid)}
	}
}
