package agentusageview

import (
	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// unknownModel labels the model bucket of tokens no model name was recorded for.
const unknownModel = "unknown"

// breakdownLine is one entry of a block.
type breakdownLine struct {
	label  string
	role   theme.TokenRole // category block only: the swatch colour; "" elsewhere
	tokens int64
	tenths int // noShare when the session has no token
}

// breakdownBlock is one titled list of lines whose shares add up to the session.
type breakdownBlock struct {
	title string // "By category", "By model", "By agent"
	lines []breakdownLine
	total int64 // == Usage.Total().Total()
}

// breakdown is the panel's content, independent of how it is drawn.
type breakdown struct{ blocks []breakdownBlock }

// buildBreakdown builds the three blocks of u. rows are agentRows' output in
// canonical order, whatever the table's sort and filter: the agent block
// reuses their share so it always equals the Share column.
func buildBreakdown(u agentusage.Usage, rows []agentRow) breakdown {
	total := u.Total().Total()
	return breakdown{blocks: []breakdownBlock{
		categoryBlock(u, total),
		modelBlock(u, total),
		agentBlock(rows, total),
	}}
}

// categoryBlock lists the five token categories, always, even at zero. Both
// cache-write lines take the cache-write role.
func categoryBlock(u agentusage.Usage, total int64) breakdownBlock {
	c := u.Categories()
	lines := []breakdownLine{
		{label: "Input", role: theme.TokenRoleInput, tokens: c.Input},
		{label: "Output", role: theme.TokenRoleOutput, tokens: c.Output},
		{label: "Cache write 5 min", role: theme.TokenRoleCacheWrite, tokens: c.CacheWrite5m},
		{label: "Cache write 1 h", role: theme.TokenRoleCacheWrite, tokens: c.CacheWrite1h},
		{label: "Cache read", role: theme.TokenRoleCacheRead, tokens: c.CacheRead},
	}
	return newBlock("By category", lines, total)
}

// modelBlock lists the models in ModelTotals' order (the unnamed bucket last,
// shown as "unknown").
func modelBlock(u agentusage.Usage, total int64) breakdownBlock {
	var lines []breakdownLine
	for _, m := range u.ModelTotals() {
		label := m.Model
		if label == "" {
			label = unknownModel
		}
		lines = append(lines, breakdownLine{label: label, tokens: m.Tokens.Total()})
	}
	return newBlock("By model", lines, total)
}

// agentBlock lists the agents; shares come from the rows so they match the
// Share column exactly.
func agentBlock(rows []agentRow, total int64) breakdownBlock {
	lines := make([]breakdownLine, len(rows))
	for i, r := range rows {
		lines[i] = breakdownLine{label: agentLabel(r), tokens: r.Tokens.Total(), tenths: r.share}
	}
	return breakdownBlock{title: "By agent", lines: lines, total: total}
}

// agentLabel is "orchestrator", or "<type> <short ID>" with no tree branch.
func agentLabel(r agentRow) string {
	if r.Main {
		return orchestratorLabel
	}
	typ := r.Type
	if typ == "" {
		typ = unknownType
	}
	return typ + " " + idCell(r)
}

// newBlock fills in the shares of lines; the rounding residue goes to the
// largest line.
func newBlock(title string, lines []breakdownLine, total int64) breakdownBlock {
	tokens := make([]int64, len(lines))
	for i, l := range lines {
		tokens[i] = l.tokens
	}
	for i, t := range shares(tokens, total, largestIndex(tokens)) {
		lines[i].tenths = t
	}
	return breakdownBlock{title: title, lines: lines, total: total}
}

// largestIndex is the index of the largest value, the first on a tie; -1 when empty.
func largestIndex(values []int64) int {
	best := -1
	for i, v := range values {
		if best < 0 || v > values[best] {
			best = i
		}
	}
	return best
}
