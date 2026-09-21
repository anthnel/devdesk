package templates

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/template"
)

// What an agent asks this view to do (§3.61). See workspaces/mcp.go for why the
// request is a message to the view rather than work the router builds itself.

// SyncRequestedMsg asks for a sync of one template of the catalog: `F`, reached
// from an agent.
//
// One slug and not a list, because `F` acts on the selected row. The catalog is
// what templates_list reports, so the slug is one the agent has just read; a
// row the registry lists without it being in the catalog is not addressable
// here, and is refused rather than guessed at.
type SyncRequestedMsg struct {
	Slug       string
	Invocation string
}

// handleSyncRequested is `F` reached from an agent.
//
// It consults the same availability the header greys the shortcut with — the
// sync being already under way — and starts the same run a key starts, so the
// spinner, the footer and `:jobs` cannot tell who asked. Credentials are
// resolved here exactly as for the key (template.CredentialsFor, which only
// ever lends the forge's token to the forge's own host) and travel into the Cmd;
// none of it reaches the reply.
func (m Model) handleSyncRequested(msg SyncRequestedMsg) (tea.Model, tea.Cmd) {
	if cmd, waiting := m.deferUntilOpened(msg); waiting {
		return m, cmd
	}
	if m.storeErr != nil {
		return m, jobs.Refuse(msg.Invocation, reasonUnreadable)
	}

	entry, ok := m.declaredEntry(msg.Slug)
	if !ok {
		if msg.Slug == "" {
			return m, jobs.Refuse(msg.Invocation, "No template named")
		}
		return m, jobs.Refuse(msg.Invocation, "No template "+msg.Slug+" in the catalog — templates_list is what it holds")
	}
	if m.working(jobs.KindSync, entry.Slug) {
		return m, jobs.Refuse(msg.Invocation, reasonSyncRunning)
	}
	return m, jobs.WithInvocation(msg.Invocation, m.syncStart(entry))
}

// declaredEntry finds a slug among the catalog's own entries.
func (m Model) declaredEntry(slug string) (template.Entry, bool) {
	for _, e := range m.declared {
		if e.Slug == slug {
			return e, true
		}
	}
	return template.Entry{}, false
}

// deferUntilOpened holds a request that arrived before the catalog was read.
//
// The router builds the view on demand, and building it is not filling it: the
// entries only arrive from loadCatalogCmd, which Init dispatches
// asynchronously. Served against the empty model, the request would refuse with
// "No template ... in the catalog" — a statement about a file the view has not
// read yet. It waits, and is replayed as a message once the catalog lands, so
// it takes the path a request that did not wait takes.
func (m *Model) deferUntilOpened(msg tea.Msg) (tea.Cmd, bool) {
	if m.store != nil || m.storeErr != nil {
		return nil, false
	}
	m.pendingRequests = append(m.pendingRequests, msg)
	// One load however many requests queue behind it.
	if len(m.pendingRequests) > 1 {
		return nil, true
	}
	return loadCatalogCmd(m.path), true
}

// drainPendingRequests re-emits what waited for the catalog, as messages.
func (m *Model) drainPendingRequests() tea.Cmd {
	if len(m.pendingRequests) == 0 {
		return nil
	}
	queued := m.pendingRequests
	m.pendingRequests = nil

	cmds := make([]tea.Cmd, 0, len(queued))
	for _, msg := range queued {
		cmds = append(cmds, func() tea.Msg { return msg })
	}
	return tea.Batch(cmds...)
}

// refusePendingRequests answers everything that waited for a catalog that could
// not be read. An agent left waiting would hang until its own timeout, with the
// reason in a log it cannot see.
func (m *Model) refusePendingRequests(reason string) tea.Cmd {
	if len(m.pendingRequests) == 0 {
		return nil
	}
	queued := m.pendingRequests
	m.pendingRequests = nil

	cmds := make([]tea.Cmd, 0, len(queued))
	for _, msg := range queued {
		if req, ok := msg.(SyncRequestedMsg); ok {
			cmds = append(cmds, jobs.Refuse(req.Invocation, reason))
		}
	}
	return tea.Batch(cmds...)
}
