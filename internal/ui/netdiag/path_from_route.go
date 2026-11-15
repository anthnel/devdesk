package netdiag

import (
	"log"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

const (
	// reasonNotARouteRow is the refusal of H off the Local route row. The header
	// greys the key out and the handler says the same thing at the footer.
	reasonNotARouteRow = "Select the Local route row to trace its path"

	// reasonPathBusy is the refusal when a trace is already running.
	reasonPathBusy = "A path trace is already running — esc cancels it"
)

// pathFromRoute is the one answer to "can H open the trace for this row", read
// by the header and by the handler (Rule 130). The reasons come in this order:
// the platform, the row, then the target the trace would be asked about.
func (m *Model) pathFromRoute() shortcut.Availability {
	if a := m.traceable(); !a.Enabled() {
		return a
	}
	if c, ok := m.checksTable.Selected(); !ok || c.ID != netcheck.CheckRoute {
		return shortcut.Unavailable(reasonNotARouteRow)
	}
	if _, err := parsePathTarget(m.targetInput.Value()); err != nil {
		return shortcut.Unavailable(capitalize(err.Error()))
	}
	return shortcut.Availability{}
}

// tracePathFromRoute opens the trace sub-level on the results' target, in its
// ready state: nothing is resolved or sent until the user presses enter there.
func (m *Model) tracePathFromRoute() (*Model, tea.Cmd) {
	if a := m.pathFromRoute(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	if m.pathModel.running() {
		return m, m.footer.Warn(reasonPathBusy)
	}
	tg, err := m.buildTarget()
	if err != nil {
		log.Printf("ERROR [netdiag] target invalid on path hand-off: %v", err)
		return m, m.footer.Error("Cannot trace — the target is no longer valid")
	}
	m.openTrace(StateResults, pathTarget{Host: tg.Host})
	return m, nil
}
