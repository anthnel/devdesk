package agentusageview

import (
	"fmt"
	"slices"
	"strings"

	"github.com/anthnel/devdesk/internal/agentusage"
)

// mainKey is the key of the orchestrator, of which a session has one.
const mainKey = "main"

// agentKeys names agents so the same one keeps its key from one rebuild to the
// next: "main" for the orchestrator, "sub:<ID>#n" for a sub-agent, n counting
// the earlier agents that share its ID (an ID is not guaranteed unique). It
// must be given the canonical order (agentRows), never the sorted or filtered
// one: under a sort, two twins would trade keys whenever they trade ranks.
func agentKeys(agents []agentusage.Agent) []string {
	seen := make(map[string]int, len(agents))
	keys := make([]string, len(agents))
	for i, a := range agents {
		if a.Main {
			keys[i] = mainKey
			continue
		}
		keys[i] = fmt.Sprintf("sub:%s#%d", a.ID, seen[a.ID])
		seen[a.ID]++
	}
	return keys
}

// populationOf lists what makes a table "another population" for the column
// widths: which agents it holds, and the models each one names. Counts change
// on every delta and do not count. A type can change too (its meta arrives
// after the transcript) but stays out on purpose: the column is remeasured at
// the next ctrl+r or change of population.
func populationOf(rows []agentRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.key + "|" + strings.Join(r.Models, ",")
	}
	return out
}

// withUsage shows u in the agents table, keeping the cursor on the agent it was
// on (whatever the filter and the sort do to the order), and clears the
// loading and error states. The columns are remeasured only on a full read or
// when the population changed, so a two-second delta never reshuffles them.
func (m Model) withUsage(u agentusage.Usage, full bool) Model {
	m = m.withRows(u, full)
	m.agentsLoading, m.agentsErr = false, false
	return m
}

// withRows rebuilds the agents table from u without touching the loading and
// error states, so a red footer survives the rebuild.
func (m Model) withRows(u agentusage.Usage, full bool) Model {
	before := populationOf(m.agentTable.Items())
	selected, hadSelection := m.selectedAgentKey()
	cursor := m.agentTable.Cursor()

	rows := agentRows(u, m.agentsTree(), m.clock, m.prices)
	m.usage = u
	m.cost = m.prices.Session(u)
	m.agentTable.SetItems(rows)

	if full || !slices.Equal(before, populationOf(rows)) {
		m.agentTable.Remeasure()
	}
	m.agentTable.SetCursor(cursor) // clamps when the agent vanished
	if hadSelection {
		m.selectAgentKey(selected)
	}
	return m
}

// selectAgentKey moves the cursor onto the visible row with that key, if any.
func (m *Model) selectAgentKey(key string) {
	if i := slices.IndexFunc(m.agentTable.Visible(), func(r agentRow) bool { return r.key == key }); i >= 0 {
		m.agentTable.SetCursor(i)
	}
}

// selectedAgentKey is the key of the row under the cursor.
func (m Model) selectedAgentKey() (string, bool) {
	r, ok := m.agentTable.Selected()
	return r.key, ok
}
