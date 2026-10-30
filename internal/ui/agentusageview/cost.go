package agentusageview

import (
	"fmt"
	"log"
	"math"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/agentusage/pricing"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/datatable"
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
		Title: "Est. cost", Sizing: datatable.SizingFixed, MinWidth: costWidth, Optional: true, DropFirst: true,
		Cell:  costCell,
		Style: func(r agentRow) lipgloss.Style { return dimWhen(r.cost.Status != pricing.Priced) },
	}
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
