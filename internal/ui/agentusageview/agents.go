package agentusageview

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	// noValue is the dash of a cell with nothing to show.
	noValue = "-"
	// noModel stands for an agent whose transcript named no model.
	noModel = noValue
	// unknownID stands for a sub-agent whose ID is empty, so it cannot be
	// mistaken for the orchestrator, which has no short ID by design.
	unknownID = "?"
	// unknownType stands for a sub-agent whose meta file gave no type.
	unknownType = "unknown"
	// orchestratorLabel is the Agent cell of the main agent.
	orchestratorLabel = "orchestrator"
	// partialMark prefixes an output count that is a floor, not an exact value.
	partialMark = "≥ "
	// stem and lastStem draw the tree branch in front of a sub-agent's type.
	stem     = "├─ "
	lastStem = "└─ "
)

// agentIDPrefix is the shortest agent ID shown.
const agentIDPrefix = 8

// agentRow is one line of the agents table.
type agentRow struct {
	agentusage.Agent
	branch  string // stem, lastStem, or "" (orchestrator, or a sorted list)
	shortID string // ID prefix, distinct within the session; "" for the orchestrator
	share   int    // tenths of a percent of the session total, or noShare
	key     string // identity across rebuilds, see agentKeys
}

// agentRows orders a session's agents (orchestrator, then sub-agents by ID),
// gives each its short ID and share, and draws the branches when tree is true.
// The sort is stable so equal IDs keep their input order.
func agentRows(u agentusage.Usage, tree bool) []agentRow {
	var main, subs []agentusage.Agent
	for _, a := range u.Agents {
		if a.Main {
			main = append(main, a)
		} else {
			subs = append(subs, a)
		}
	}
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].ID < subs[j].ID })

	ordered := append(append([]agentusage.Agent{}, main...), subs...)
	totals := make([]int64, len(ordered))
	for i, a := range ordered {
		totals[i] = a.Tokens.Total()
	}
	mainIdx := -1
	if len(main) > 0 {
		mainIdx = 0
	}
	parts := shares(totals, u.Total().Total(), mainIdx)

	ids := make([]string, len(subs))
	for i, a := range subs {
		ids[i] = a.ID
	}
	short := shortAgentIDs(ids)

	keys := agentKeys(ordered)
	rows := make([]agentRow, 0, len(ordered))
	for i, a := range ordered {
		row := agentRow{Agent: a, share: parts[i], key: keys[i]}
		if !a.Main {
			j := i - len(main)
			row.shortID = short[j]
			if tree {
				row.branch = stem
				if j == len(subs)-1 {
					row.branch = lastStem
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// shortAgentIDs cuts every ID to one common length, the smallest n >=
// agentIDPrefix (in runes) at which all of them differ, capped at each ID's
// own length.
func shortAgentIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	longest := 0
	runes := make([][]rune, len(ids))
	for i, id := range ids {
		runes[i] = []rune(id)
		longest = max(longest, len(runes[i]))
	}
	cut := func(n int) []string {
		out := make([]string, len(runes))
		for i, r := range runes {
			out[i] = string(r[:min(n, len(r))])
		}
		return out
	}
	for n := agentIDPrefix; n < longest; n++ {
		seen := make(map[string]bool, len(ids))
		distinct := true
		for _, s := range cut(n) {
			if seen[s] {
				distinct = false
				break
			}
			seen[s] = true
		}
		if distinct {
			return cut(n)
		}
	}
	return cut(longest)
}

// agentColumns describes the agents table. Only Total sorts, largest first;
// without a sort the order is the tree. Columns give way on a narrow terminal
// in this order: Cache write, Cache read, In, Model. The ID never does, so two
// agents of one type stay apart.
func agentColumns() []datatable.Column[agentRow] {
	count := func(title string, width int, tokens func(agentRow) int64) datatable.Column[agentRow] {
		return datatable.Column[agentRow]{
			Title: title, Sizing: datatable.SizingFixed, MinWidth: width,
			Cell:  func(r agentRow) string { return formatTokens(tokens(r)) },
			Style: func(r agentRow) lipgloss.Style { return dimWhen(tokens(r) == 0) },
		}
	}
	in := count("In", 6, func(r agentRow) int64 { return r.Tokens.Input })
	in.Optional = true
	cacheRead := count("Cache read", len("Cache read"), func(r agentRow) int64 { return r.Tokens.CacheRead })
	cacheRead.Optional = true
	cacheWrite := count("Cache write", len("Cache write"), func(r agentRow) int64 { return r.Tokens.CacheWrite })
	cacheWrite.Optional, cacheWrite.DropFirst = true, true
	total := count("Total", 6, func(r agentRow) int64 { return r.Tokens.Total() })
	total.Less = func(a, b agentRow) bool { return a.Tokens.Total() < b.Tokens.Total() }
	total.DescFirst = true

	out := count("Out", 7, nil)
	out.Cell = outCell
	out.Style = func(r agentRow) lipgloss.Style { return dimWhen(r.Tokens.Output == 0) }

	return []datatable.Column[agentRow]{
		{Title: "Agent", Sizing: datatable.SizingContent, MinWidth: 14, Cell: roleCell, Search: agentSearch,
			Style: func(r agentRow) lipgloss.Style { return dimWhen(!r.Main && r.Type == "") }},
		{Title: "ID", Sizing: datatable.SizingContent, MinWidth: agentIDPrefix, Cell: idCell,
			Search: func(r agentRow) string { return r.ID },
			Style:  func(r agentRow) lipgloss.Style { return dimWhen(idCell(r) == noValue || idCell(r) == unknownID) }},
		{Title: "Model", Sizing: datatable.SizingContent, MinWidth: 12, Flex: 2, Optional: true, Cell: modelsCell, Search: modelsCell,
			Style: func(r agentRow) lipgloss.Style { return dimWhen(len(r.Models) == 0) }},
		in, out, cacheRead, cacheWrite, total,
		{Title: "Share", Sizing: datatable.SizingFixed, MinWidth: len("100.0%"), Cell: shareCell,
			Style: func(r agentRow) lipgloss.Style { return dimWhen(r.share <= 0) }},
	}
}

// dimWhen is Rule 122's colour discipline: a zero or a placeholder is dim,
// anything else keeps the default colour.
func dimWhen(dim bool) lipgloss.Style {
	if dim {
		return theme.DimStyle
	}
	return lipgloss.NewStyle()
}

// agentSearch is what `/` matches in the Agent column: the role. The full ID is
// searched through the ID column.
func agentSearch(r agentRow) string { return roleCell(r) }

// idCell is the ID column: the short ID of a sub-agent, a dash for the
// orchestrator, which has none.
func idCell(r agentRow) string {
	switch {
	case r.Main:
		return noValue
	case r.shortID == "":
		return unknownID
	}
	return r.shortID
}

// shareCell is the Share column.
func shareCell(r agentRow) string { return formatShare(r.share) }

// roleCell is the Agent cell: "orchestrator", or the sub-agent's type behind
// its branch.
func roleCell(r agentRow) string {
	if r.Main {
		return orchestratorLabel
	}
	typ := r.Type
	if typ == "" {
		typ = unknownType
	}
	return r.branch + typ
}

// modelsCell lists the models the agent used.
func modelsCell(r agentRow) string {
	if len(r.Models) == 0 {
		return noModel
	}
	return strings.Join(r.Models, ", ")
}

// outCell is the output count. A sub-agent still being written is a floor and
// says so; the orchestrator never does, its transcript is the one being read.
func outCell(r agentRow) string {
	out := formatTokens(r.Tokens.Output)
	if !r.Main && r.PartialOutput() {
		return partialMark + out
	}
	return out
}

// formatTokens renders a count compactly, floored to one decimal: 1099 is
// "1.0k", never "1.1k". From 100 of a unit up the decimal is dropped.
func formatTokens(n int64) string {
	units := []struct {
		size   int64
		suffix string
	}{
		{1_000_000_000_000, "T"}, {1_000_000_000, "B"}, {1_000_000, "M"}, {1_000, "k"},
	}
	for _, u := range units {
		if n < u.size {
			continue
		}
		tenths := n / (u.size / 10) // divide first: n*10 would overflow
		if tenths >= 1000 {
			return fmt.Sprintf("%d%s", tenths/10, u.suffix)
		}
		return fmt.Sprintf("%d.%d%s", tenths/10, tenths%10, u.suffix)
	}
	return fmt.Sprintf("%d", n)
}
