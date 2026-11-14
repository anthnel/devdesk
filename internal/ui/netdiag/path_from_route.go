package netdiag

import (
	"log"
	"net/netip"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

const (
	// reasonNotARouteRow is the refusal of H off the Local route row. The header
	// greys the key out and the handler says the same thing at the footer.
	reasonNotARouteRow = "Select the Local route row to trace its path"

	// reasonPathBusy is shown on the Path footer when H lands on a running trace.
	reasonPathBusy = "A path trace is already running — esc cancels it"
)

// pathFromRoute is the one answer to "can H hand this row to the Path tab",
// read by the header and by the handler (Rule 130).
func (m *Model) pathFromRoute() shortcut.Availability {
	if c, ok := m.checksTable.Selected(); ok && c.ID == netcheck.CheckRoute {
		return shortcut.Availability{}
	}
	return shortcut.Unavailable(reasonNotARouteRow)
}

// tracePathFromRoute moves the results' target to the Path tab and leaves it
// editing: nothing is resolved or sent until the user presses enter there.
func (m *Model) tracePathFromRoute() (*Model, tea.Cmd) {
	if a := m.pathFromRoute(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	if m.pathModel.running() {
		m.activeTab = tabPath
		return m, m.pathModel.footer.Warn(reasonPathBusy)
	}
	tg, err := m.buildTarget()
	if err != nil {
		log.Printf("ERROR [netdiag] target invalid on path hand-off: %v", err)
		return m, m.footer.Error("Cannot trace — the target is no longer valid")
	}
	m.activeTab = tabPath
	return m, m.pathModel.prefill(tg.Host)
}

// prefill puts host in the field and returns the tab to editing, dropping the
// outcome of an earlier trace.
func (pm *PathModel) prefill(host string) tea.Cmd {
	pm.state = pathEditing
	pm.target = pathTarget{}
	pm.addr = netip.Addr{}
	pm.hops = nil
	pm.result = nil
	pm.errText = ""
	pm.table.SetItems(nil)
	pm.input.SetValue(host)
	pm.input.CursorEnd()
	return pm.input.Focus()
}
