package agentusageview

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// The reasons, written once so the header, the footer and the tests cannot
// drift apart on the wording (Rule 129 — English US).
const (
	reasonNoSession     = "No session to open"
	reasonInsideSession = "Already inside a session"
	reasonTopLevel      = "Already at the session list"
)

// actions is what the keys allow right now, computed once and read by both
// halves of the view: GetShortcuts to grey out, the handlers to refuse
// (Rule 130).
type actions struct {
	Open  shortcut.Availability // → and enter
	Close shortcut.Availability // ← and esc
}

// availability computes the answers from the level and the selected row.
func (m Model) availability() actions {
	var a actions
	if m.level == levelAgents {
		a.Open = shortcut.Unavailable(reasonInsideSession)
		return a
	}
	a.Close = shortcut.Unavailable(reasonTopLevel)
	if _, ok := m.table.Selected(); !ok {
		a.Open = shortcut.Unavailable(reasonNoSession)
	}
	return a
}

// guard runs act when the action is available, and otherwise refuses with the
// reason as a Warn: nothing failed, the action does not apply (Rule 128).
func (m Model) guard(a shortcut.Availability, act func() (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	return act()
}
