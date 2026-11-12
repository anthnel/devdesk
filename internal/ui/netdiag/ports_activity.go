package netdiag

import "github.com/anthnel/devdesk/internal/ui/datatable"

// ActivityTables returns the read-only face of the view's tables, keyed by the
// name the activity-column guard (internal/app) knows them by. Only the Ports
// table has an activity column: a failed network test stays in the Verdict.
func (m *Model) ActivityTables() map[string]datatable.ActivityTable {
	return map[string]datatable.ActivityTable{"netdiag/ports": &m.portsModel.table}
}

// WantsBlink is the router's BlinkingView hook: true while a kill runs on a
// row, or while the active tab's footer shows the shared activity dot (a run,
// an interfaces read), so the router frames the view for the dot's blink. A
// level, not an edge.
func (m *Model) WantsBlink() bool {
	if len(m.portsModel.table.BusyLabels()) > 0 {
		return true
	}
	_, st := m.activeFooter()
	return st.Activity
}
