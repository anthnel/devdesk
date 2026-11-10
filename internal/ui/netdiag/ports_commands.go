package netdiag

import (
	"context"
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

func killProcessCmd(pid string) tea.Cmd {
	return func() tea.Msg {
		return portsKillResultMsg{pid: pid, err: ports.Kill(pid)}
	}
}
