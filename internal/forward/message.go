package forward

import tea "github.com/charmbracelet/bubbletea"

// The messages a view exchanges with the router about forwards.
//
// They live here rather than in internal/app so that a view can speak about a
// forward without importing the router — the arrangement internal/jobs already
// uses. A view never touches the Registry: it asks, and it is told.
//
// The registry is the router's, for the reason the MCP server is
// (internal/app/mcp.go): reinitializeViews drops every view on a config save
// and on a context switch, so a listener held by a view model would be leaked
// there with nothing to report it.

// OpenMsg asks the router to open a forward. It is a request, not a result:
// opening probes the target and binds a port, which is I/O, so the router
// answers it with a Cmd rather than doing it in Update (Rule 110).
type OpenMsg struct {
	LocalPort int
	Target    string
}

// CloseMsg asks the router to stop a forward.
type CloseMsg struct {
	ID string
}

// RefreshMsg asks the router for a fresh snapshot.
//
// It exists because a forward's counters move without any router event: bytes
// flow, connections come and go, and nothing in Update hears about it. The
// view that shows them ticks and asks; the chain therefore dies with that
// view, instead of the router animating rows nobody is looking at.
type RefreshMsg struct{}

// OpenedMsg reports what became of an OpenMsg. Err is why there is no forward,
// and it wraps one of this package's sentinels so the view can say which
// refusal it was without reading the text.
type OpenedMsg struct {
	Forward Forward
	Err     error
}

// ClosedMsg reports what became of a CloseMsg.
type ClosedMsg struct {
	ID  string
	Err error
}

// ChangedMsg is the snapshot the router broadcasts after anything moved. It
// carries values, so a view can hold it across frames without racing the
// goroutines that keep the forwards running.
type ChangedMsg struct {
	Forwards []Forward
}

// Open asks for a forward. A view returns this rather than calling the
// registry, which it cannot reach.
func Open(localPort int, target string) tea.Cmd {
	return func() tea.Msg {
		return OpenMsg{LocalPort: localPort, Target: target}
	}
}

// Close asks for a forward to stop.
func Close(id string) tea.Cmd {
	return func() tea.Msg { return CloseMsg{ID: id} }
}

// Refresh asks for a fresh snapshot.
func Refresh() tea.Msg { return RefreshMsg{} }
