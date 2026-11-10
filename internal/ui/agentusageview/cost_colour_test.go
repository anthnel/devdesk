package agentusageview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/agentusage/pricing"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-151 step 5: Est. cost is coloured by the agent's share of the
// session's estimated cost. This is the D6 exception (a share of money, not an
// activity colour). Below LoadWarnPercent the cell is neutral: LoadTextStyle
// would paint it green there, and a green cost says "good", which it does not.

func TestEstCostColourFollowsTheCostShare(t *testing.T) {
	col := column(t, costTitle)
	none := lipgloss.NewStyle().GetForeground()
	dim := theme.DimStyle.GetForeground()
	medium := lipgloss.TerminalColor(theme.ColorSeverityMedium)
	critical := lipgloss.TerminalColor(theme.ColorSeverityCritical)
	tests := []struct {
		name string
		row  agentRow
		want lipgloss.TerminalColor
	}{
		{"74.9 is neutral", agentRow{Agent: orchestrator(1, 0), cost: priced(1), costPct: 74.9, costRanked: true}, none},
		{"75 is orange", agentRow{Agent: orchestrator(1, 0), cost: priced(1), costPct: 75, costRanked: true}, medium},
		{"89.9 is orange", agentRow{Agent: orchestrator(1, 0), cost: priced(1), costPct: 89.9, costRanked: true}, medium},
		{"90 is red", agentRow{Agent: orchestrator(1, 0), cost: priced(1), costPct: 90, costRanked: true}, critical},
		{"100 is red", agentRow{Agent: orchestrator(1, 0), cost: priced(1), costPct: 100, costRanked: true}, critical},
		{"0 is neutral, never green", agentRow{Agent: orchestrator(1, 0), cost: priced(1), costPct: 0, costRanked: true}, none},
		{"60 is neutral, never green", agentRow{Agent: orchestrator(1, 0), cost: priced(1), costPct: 60, costRanked: true}, none},
		{"a share nobody ranked is neutral even at 95", agentRow{Agent: orchestrator(1, 0), cost: priced(1), costPct: 95, costRanked: false}, none},
		{"a sub-agent follows the same thresholds", agentRow{Agent: subAgent("a", "coder", 1, 0), cost: priced(1), costPct: 92, costRanked: true}, critical},
		{"unpriced is dim whatever the share", agentRow{Agent: orchestrator(1, 0), cost: pricing.Estimate{Status: pricing.Unpriced}, costPct: 95, costRanked: true}, dim},
		{"no tokens is dim", agentRow{Agent: subAgent("a", "coder", 0, 0), cost: pricing.Estimate{Status: pricing.NoTokens}}, dim},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := col.Style(tt.row).GetForeground(); got != tt.want {
				t.Errorf("foreground = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEstCostColourDoesNotChangeTheCellText(t *testing.T) {
	col := column(t, costTitle)
	r := agentRow{Agent: orchestrator(1, 0), cost: priced(1.5), costPct: 95, costRanked: true}
	plain := r
	plain.costPct, plain.costRanked = 0, false
	if col.Cell(r) != col.Cell(plain) || col.Cell(r) != "~$1.50" {
		t.Errorf("cell = %q / %q, want the same plain ~$1.50 whatever the share", col.Cell(r), col.Cell(plain))
	}
}

// End to end: the shares come out of agentRows, from the session's estimates.
// model-a costs 10 per million output tokens (pricesYAML).
func TestEstCostColourComesFromTheSessionShares(t *testing.T) {
	prices, _ := pricing.FromConfig(cfgWith(t, pricesYAML).AgentUsage.Prices)
	col := column(t, costTitle)
	none := lipgloss.NewStyle().GetForeground()
	dim := theme.DimStyle.GetForeground()
	out := func(a agentusage.Agent, k int64) agentusage.Agent {
		return byModel(a, mu("model-a", agentusage.Tokens{Output: k}, 0))
	}
	fg := func(r agentRow) lipgloss.TerminalColor { return col.Style(r).GetForeground() }

	t.Run("95 and 5", func(t *testing.T) {
		u := agentusage.Usage{Agents: []agentusage.Agent{
			out(agentusage.Agent{Main: true}, 950_000), out(agentusage.Agent{ID: "a01", Type: "coder"}, 50_000)}}
		rows := agentRows(u, true, statusClock{}, prices)
		if got := fg(rows[0]); got != lipgloss.TerminalColor(theme.ColorSeverityCritical) {
			t.Errorf("orchestrator at 95%% = %v, want critical", got)
		}
		if got := fg(rows[1]); got != none {
			t.Errorf("sub-agent at 5%% = %v, want neutral", got)
		}
	})
	t.Run("80 and 20", func(t *testing.T) {
		u := agentusage.Usage{Agents: []agentusage.Agent{
			out(agentusage.Agent{Main: true}, 800_000), out(agentusage.Agent{ID: "a01", Type: "coder"}, 200_000)}}
		rows := agentRows(u, true, statusClock{}, prices)
		if got := fg(rows[0]); got != lipgloss.TerminalColor(theme.ColorSeverityMedium) {
			t.Errorf("orchestrator at 80%% = %v, want orange", got)
		}
	})
	t.Run("60 and 40", func(t *testing.T) {
		u := agentusage.Usage{Agents: []agentusage.Agent{
			out(agentusage.Agent{Main: true}, 600_000), out(agentusage.Agent{ID: "a01", Type: "coder"}, 400_000)}}
		for i, r := range agentRows(u, true, statusClock{}, prices) {
			if got := fg(r); got != none {
				t.Errorf("row %d at 60/40 = %v, want neutral", i, got)
			}
		}
	})
	t.Run("a single priced agent colours nothing", func(t *testing.T) {
		u := agentusage.Usage{Agents: []agentusage.Agent{
			out(agentusage.Agent{Main: true}, 1_000_000), {ID: "a01", Type: "coder"}}}
		rows := agentRows(u, true, statusClock{}, prices)
		if got := fg(rows[0]); got != none {
			t.Errorf("lone priced agent at 100%% = %v, want neutral", got)
		}
		if got := fg(rows[1]); got != dim {
			t.Errorf("unpriced agent = %v, want dim", got)
		}
	})
	t.Run("no price table: every cell is a dim dash", func(t *testing.T) {
		u := agentusage.Usage{Agents: []agentusage.Agent{
			out(agentusage.Agent{Main: true}, 950_000), out(agentusage.Agent{ID: "a01", Type: "coder"}, 50_000)}}
		for i, r := range agentRows(u, true, statusClock{}, pricing.Table{}) {
			if got := fg(r); got != dim {
				t.Errorf("row %d = %v, want dim", i, got)
			}
		}
	})
}

func TestTheHelpSaysWhatTheCostColoursMean(t *testing.T) {
	var body string
	for _, s := range runningTeam(t).GetHelpContent().Sections {
		if s.Title == "Costs" {
			body = s.Body
		}
	}
	if body == "" {
		t.Fatal("no Costs section in the help")
	}
	for _, want := range []string{"75%", "90%"} {
		if !strings.Contains(body, want) {
			t.Errorf("Costs help does not mention %s as a colour threshold:\n%s", want, body)
		}
	}
}
