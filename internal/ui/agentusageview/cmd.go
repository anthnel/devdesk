package agentusageview

import (
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
)

// listSessionsCmd resolves the projects root and lists it, all inside the Cmd:
// the model is not touched (Rule 110) and the answer carries gen back.
func listSessionsCmd(gen int) tea.Cmd {
	return func() tea.Msg {
		root, err := agentusage.ProjectsRoot()
		if err != nil {
			return sessionListLoadedMsg{gen: gen, err: err}
		}
		listing, err := agentusage.ListSessions(root)
		return sessionListLoadedMsg{gen: gen, root: root, listing: listing, err: err}
	}
}

// pollInterval is the pause between two reads of the open session; a variable
// so tests can shorten it. quietPeriod is how long a session must be quiet to
// be considered settled and no longer polled.
var (
	pollInterval = 2 * time.Second
	quietPeriod  = 5 * time.Minute
	// idleAfter is how long a "running" agent may stay silent before it reads idle.
	idleAfter = 5 * time.Minute
)

// now is the clock the poll chain reads, replaceable in tests.
var now = time.Now

// usageDeltaMsg carries one incremental read. gen is agentGen at the time the
// read started; at is when it finished.
type usageDeltaMsg struct {
	gen   int
	delta agentusage.Delta
	err   error
	at    time.Time
}

// String and GoString keep the delta's paths out of any formatted dump: fmt
// cannot call the Delta's own redacting methods through an unexported field.
func (m usageDeltaMsg) String() string {
	return fmt.Sprintf("usageDeltaMsg{gen: %d, delta: %v, failed: %t}", m.gen, m.delta, m.err != nil)
}

// GoString is String.
func (m usageDeltaMsg) GoString() string { return m.String() }

// usagePollTickMsg says the poll interval of generation gen has elapsed.
type usagePollTickMsg struct{ gen int }

// agentsResumeMsg asks Update to restart a poll chain that stopped.
type agentsResumeMsg struct{}

// readDeltaCmd reads what changed since c. It owns its Cursor copy and touches
// no model (Rule 110); the answer carries gen and the time of the read.
func readDeltaCmd(gen int, c agentusage.Cursor) tea.Cmd {
	return func() tea.Msg {
		delta, err := agentusage.ReadDelta(c)
		return usageDeltaMsg{gen: gen, delta: delta, err: err, at: now()}
	}
}

// pollTickCmd waits pollInterval, then answers with a tick of generation gen.
// The interval is read when the Cmd is built (Update's goroutine), never from
// the Cmd's own. The Cmd may be run twice (a caller inspecting it, then
// running it): the wait happens once and the answer is kept.
func pollTickCmd(gen int) tea.Cmd {
	d := pollInterval
	var once sync.Once
	return func() tea.Msg {
		once.Do(func() { time.Sleep(d) })
		return usagePollTickMsg{gen: gen}
	}
}

// resumeCmd answers agentsResumeMsg at once.
func resumeCmd() tea.Cmd {
	return func() tea.Msg { return agentsResumeMsg{} }
}
