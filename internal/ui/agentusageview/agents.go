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
	// noModel stands for an agent whose transcript named no model.
	noModel = "-"
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

// agentRow is one line of the agents table: an agent, and whether it is the
// last sub-agent, which decides the shape of its branch.
type agentRow struct {
	agentusage.Agent
	last bool
}

// agentRows orders a session's agents: the orchestrator first, then the
// sub-agents by ID. The sort is stable so equal IDs keep their input order.
func agentRows(u agentusage.Usage) []agentRow {
	var main, subs []agentusage.Agent
	for _, a := range u.Agents {
		if a.Main {
			main = append(main, a)
		} else {
			subs = append(subs, a)
		}
	}
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].ID < subs[j].ID })

	rows := make([]agentRow, 0, len(u.Agents))
	for _, a := range main {
		rows = append(rows, agentRow{Agent: a})
	}
	for i, a := range subs {
		rows = append(rows, agentRow{Agent: a, last: i == len(subs)-1})
	}
	return rows
}

// agentColumns describes the agents table. Nothing sorts: the order is the
// tree. Cache columns give way first on a narrow terminal, writes before reads.
func agentColumns() []datatable.Column[agentRow] {
	count := func(title string, width int, tokens func(agentRow) int64) datatable.Column[agentRow] {
		return datatable.Column[agentRow]{
			Title: title, Sizing: datatable.SizingFixed, MinWidth: width,
			Cell:  func(r agentRow) string { return formatTokens(tokens(r)) },
			Style: func(r agentRow) lipgloss.Style { return dimWhen(tokens(r) == 0) },
		}
	}
	in := count("In", 6, func(r agentRow) int64 { return r.Tokens.Input })
	cacheRead := count("Cache read", len("Cache read"), func(r agentRow) int64 { return r.Tokens.CacheRead })
	cacheRead.Optional = true
	cacheWrite := count("Cache write", len("Cache write"), func(r agentRow) int64 { return r.Tokens.CacheWrite })
	cacheWrite.Optional, cacheWrite.DropFirst = true, true
	total := count("Total", 6, func(r agentRow) int64 { return r.Tokens.Total() })

	out := count("Out", 7, nil)
	out.Cell = outCell
	out.Style = func(r agentRow) lipgloss.Style { return dimWhen(r.Tokens.Output == 0) }

	return []datatable.Column[agentRow]{
		{Title: "Agent", Sizing: datatable.SizingContent, MinWidth: 14, Cell: roleCell, Search: agentSearch,
			Style: func(r agentRow) lipgloss.Style { return dimWhen(!r.Main && r.Type == "") }},
		{Title: "Model", Sizing: datatable.SizingContent, MinWidth: 12, Flex: 2, Cell: modelsCell, Search: modelsCell,
			Style: func(r agentRow) lipgloss.Style { return dimWhen(len(r.Models) == 0) }},
		in, out, cacheRead, cacheWrite, total,
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

// agentSearch is what `/` matches in the Agent column: the role and the ID,
// which no column shows.
func agentSearch(r agentRow) string { return roleCell(r) + " " + r.ID }

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
	if r.last {
		return lastStem + typ
	}
	return stem + typ
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
