package agentusageview

import (
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ActivityTables returns the read-only face of the view's tables, keyed by the
// name the activity-column guard (internal/app) knows them by. Only the agents
// table has an activity column: the sessions list is not live.
func (m Model) ActivityTables() map[string]datatable.ActivityTable {
	return map[string]datatable.ActivityTable{"agent-usage/agents": &m.agentTable}
}

// WantsBlink is the router's BlinkingView hook: true while a running agent is
// on screen, so the router frames the view for the dot's blink. A level, not
// an edge: only the agents level, on the Agents tab, and only for the rows the
// filter leaves visible.
func (m Model) WantsBlink() bool {
	if m.level != levelAgents || m.tab != tabAgents {
		return false
	}
	for _, r := range m.agentTable.Visible() {
		if r.state == stateRunning {
			return true
		}
	}
	return false
}

// agentDetail is what the footer says about the selected agent: its state in
// words, since the dot only repeats it in colour. It is empty off the Agents
// tab and when no row is selected.
func (m Model) agentDetail() components.RowDetail {
	if m.level != levelAgents || m.tab != tabAgents {
		return components.RowDetail{}
	}
	r, ok := m.agentTable.Selected()
	if !ok {
		return components.RowDetail{}
	}
	return components.DescribeRow(components.RowQuery{
		Observed: r.state.indicator(), ObservedText: agentStateText(r),
	}).Detail
}

// agentStateText is the sentence for one agent's state.
func agentStateText(r agentRow) string {
	switch r.state {
	case stateRunning:
		return "Agent running — last transcript line " + theme.TimeAgo(r.LastActivity)
	case stateIdle:
		return "Agent idle — last transcript line " + theme.TimeAgo(r.LastActivity)
	case stateDone:
		return "Agent done — ran " + formatDuration(r.Duration(), !r.FirstActivity.IsZero())
	case stateUnknown:
		if r.Main {
			return "Agent state unknown — updates are paused"
		}
		return "Agent state unknown — transcript has no end marker"
	}
	return ""
}
