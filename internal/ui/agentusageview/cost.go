package agentusageview

import (
	"fmt"
	"log"
	"math"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/agentusage/pricing"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

const (
	// minus is the real minus sign (U+2212), never an ASCII hyphen.
	minus = "−"
	// estimateMark prefixes an amount computed from a user-written price table.
	estimateMark = "~"
	// costWidth fits the widest sub-agent cell, "≥ ~$999.99".
	costWidth = 10
	// wholeDollarsFrom is where cents stop being shown.
	wholeDollarsFrom = 999.995
	// maxDollars is the largest amount shown exactly; past it the cell saturates.
	maxDollars = 99999.5
	// saturated is what formatUSD shows past maxDollars: "at least".
	saturated = "$99999+"
)

// formatUSD renders an amount: two decimals up to $999.99, whole dollars from
// $1000, "$99999+" past $99999. A non-zero amount under half a cent is
// "<$0.01", never "$0.00"; a negative one keeps its sign ("−<$0.01"). NaN and
// the infinities are unknown amounts and show a dash. The estimate mark is costCell's, not this function's.
func formatUSD(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return noValue
	}
	if v < 0 {
		return minus + formatUSD(-v)
	}
	switch {
	case v >= maxDollars:
		return saturated
	case v >= wholeDollarsFrom:
		return fmt.Sprintf("$%.0f", math.Floor(v+0.5))
	case v > 0 && v < 0.005:
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", v)
}

// costCell is the Est. cost cell: an estimate for the orchestrator, a floor
// (partialMark) for a sub-agent, whose transcript may be incomplete; a dash
// when the cost is unknown. A saturated amount already means "at least", so a
// sub-agent drops the floor mark to stay within costWidth.
func costCell(r agentRow) string {
	if r.cost.Status != pricing.Priced {
		return noValue
	}
	amount := formatUSD(r.cost.USD)
	if r.Main || amount == saturated {
		return estimateMark + amount
	}
	return partialMark + estimateMark + amount
}

// costColumn is the Est. cost column. It has no Less: `.` keeps cycling Total.
func costColumn() datatable.Column[agentRow] {
	return datatable.Column[agentRow]{
		Title: "Est. cost", Sizing: datatable.SizingFixed, MinWidth: costWidth, Optional: true, DropFirst: true, YieldsToContent: true,
		Cell:  costCell,
		Style: costStyle,
	}
}

// costStyle colours the cell by the agent's share of the session's cost: dim
// when unpriced, neutral below theme.LoadWarnPercent (LoadTextStyle would paint
// it green, and a green cost says "good"), orange then red above. The cell
// text stays plain (Rule 122).
func costStyle(r agentRow) lipgloss.Style {
	switch {
	case r.cost.Status != pricing.Priced:
		return theme.DimStyle
	case !r.costRanked || r.costPct < theme.LoadWarnPercent:
		return lipgloss.NewStyle()
	}
	return theme.LoadTextStyle(r.costPct)
}

// pricesFrom builds the price table of a context. The table is read once, when
// the view is built, and a rejected entry is logged once, naming the user's key.
func pricesFrom(cfg *config.Config) pricing.Table {
	if cfg == nil {
		return pricing.Table{}
	}
	table, problems := pricing.FromConfig(cfg.AgentUsage.Prices)
	for _, p := range problems {
		log.Printf("WARN [agentusageview] agent_usage.prices %q: %s", p.Key, p.Reason)
	}
	return table
}

// costShares returns each agent's share of the session's estimated cost, in
// percent, in the order of ests. Agents without a priced estimate get 0 and
// stay out of the denominator. ranked is false when fewer than two agents are
// priced or the priced sum is not a positive number: a share of one is noise.
func costShares(ests []pricing.Estimate) (pct []float64, ranked bool) {
	pct = make([]float64, len(ests))
	var sum float64
	priced := 0
	for _, e := range ests {
		if e.Status == pricing.Priced {
			sum += e.USD
			priced++
		}
	}
	if priced < 2 || math.IsNaN(sum) || math.IsInf(sum, 0) || sum <= 0 {
		return pct, false
	}
	for i, e := range ests {
		if e.Status == pricing.Priced {
			pct[i] = e.USD / sum * 100
		}
	}
	return pct, true
}
