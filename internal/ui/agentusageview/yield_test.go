package agentusageview

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/agentusage"
)

// DEVDESK-128 (DEVDESK-98 step 4): the head of the drop order yields to the
// agent text; the activity columns do not.

// yieldingTitles are the columns that give way before an agent's type is cut,
// in drop order.
var yieldingTitles = []string{"Cache write", "Est. cost", "Cache read", "In", breakdownTitle}

// longTypes are the agent types the tests use: a built-in Claude Code type and
// two long custom ones.
var longTypes = map[string]string{
	"general-purpose": "general-purpose",
	"24 chars":        strings.Repeat("t", 24),
	"30 chars":        strings.Repeat("t", 30),
}

// typedUsage is an orchestrator and one sub-agent of the given type.
func typedUsage(typ string) agentusage.Usage {
	return agentusage.Usage{Agents: []agentusage.Agent{orchestrator(1, 0), subAgent("b01", typ, 1, 0)}}
}

func agentCellWidth(typ string) int { return utf8.RuneCountInString(lastStem + typ) }

func TestOnlyTheHeadOfTheDropOrderYieldsToContent(t *testing.T) {
	seen := 0
	for _, col := range agentColumns() {
		want := slices.Contains(yieldingTitles, col.Title)
		if col.YieldsToContent != want {
			t.Errorf("column %q YieldsToContent = %v, want %v", col.Title, col.YieldsToContent, want)
		}
		if col.YieldsToContent && !col.Optional {
			t.Errorf("column %q yields but is not Optional", col.Title)
		}
		if col.YieldsToContent {
			seen++
		}
	}
	if seen != len(yieldingTitles) {
		t.Fatalf("%d columns yield, want %d", seen, len(yieldingTitles))
	}
}

// Shrinking from 230 columns, the first five columns to disappear are the
// flagged ones, in the documented order: this guards the prefix rule if a
// column is added or reordered.
func TestTheYieldingColumnsAreTheHeadOfTheDropOrder(t *testing.T) {
	var order []string
	present := map[string]bool{}
	for w := 230; w >= 50; w-- {
		m := atPricedAgents(t, nil, w, costUsage())
		now := map[string]bool{}
		for _, c := range header(t, m.View()) {
			now[c] = true
		}
		for c := range present {
			if !now[c] {
				order = append(order, c)
			}
		}
		present = now
	}
	if len(order) < len(yieldingTitles) {
		t.Fatalf("only %d columns ever disappear: %v", len(order), order)
	}
	if got := order[:len(yieldingTitles)]; !slices.Equal(got, yieldingTitles) {
		t.Errorf("first columns to disappear = %v, want %v", got, yieldingTitles)
	}
}

// DEVDESK-46 criterion 1 on real types: Tools and Duration are not traded for
// the agent text at 80 columns. State is not required there any more
// (DEVDESK-151, Q1 option B): the dot carries it, and the State text comes back
// at 84 columns, also with a long agent type.
func TestTheActivityColumnsOutliveALongAgentTypeAt80(t *testing.T) {
	checked := 0
	for name, typ := range longTypes {
		m := atPricedAgents(t, nil, 80, typedUsage(typ))
		cols := header(t, m.View())
		for _, want := range []string{"Tools", "Duration"} {
			if !slices.Contains(cols, want) {
				t.Errorf("%s: %s is missing at 80 columns: %v", name, want, cols)
			}
		}
		if slices.Contains(cols, "State") {
			t.Errorf("%s: State must be dropped at 80 columns (it needs 84): %v", name, cols)
		}
		if at84 := header(t, atPricedAgents(t, nil, 84, typedUsage(typ)).View()); !slices.Contains(at84, "State") {
			t.Errorf("%s: State is missing at 84 columns: %v", name, at84)
		}
		// Dropping State hands its cells back to the Agent text: from its floor of
		// 14 up to the type's whole width, never past it (measured: 18, 19, 19).
		if got := m.agentTable.Table().Columns()[agentColIdx].Width; got < 14 || got > agentCellWidth(typ) {
			t.Errorf("%s: Agent column is %d wide at 80 columns, want 14..%d", name, got, agentCellWidth(typ))
		}
		checked++
	}
	if checked != len(longTypes) {
		t.Fatalf("checked %d types, want %d", checked, len(longTypes))
	}
}

// Breakdown waits until the agent type fits whole.
func TestBreakdownWaitsForTheAgentText(t *testing.T) {
	tests := []struct {
		name         string
		typ          string
		absent, show int
	}{
		{"general-purpose", "general-purpose", 113, 114},
		{"30 chars", strings.Repeat("t", 30), 128, 129},
	}
	checked := 0
	for _, tt := range tests {
		want := agentCellWidth(tt.typ)
		below := atPricedAgents(t, nil, tt.absent, typedUsage(tt.typ))
		if slices.Contains(header(t, below.View()), breakdownTitle) {
			t.Errorf("%s: %s is shown at %d columns", tt.name, breakdownTitle, tt.absent)
		}
		if got := below.agentTable.Table().Columns()[agentColIdx].Width; got != want {
			t.Errorf("%s at %d: Agent = %d, want %d", tt.name, tt.absent, got, want)
		}
		at := atPricedAgents(t, nil, tt.show, typedUsage(tt.typ))
		if !slices.Contains(header(t, at.View()), breakdownTitle) {
			t.Errorf("%s: %s is missing at %d columns", tt.name, breakdownTitle, tt.show)
		}
		if got := at.agentTable.Table().Columns()[agentColIdx].Width; got != want {
			t.Errorf("%s at %d: Agent = %d, want %d", tt.name, tt.show, got, want)
		}
		checked++
	}
	if checked != len(tests) {
		t.Fatalf("checked %d, want %d", checked, len(tests))
	}
}

// Rule 116 from 50 to 230 with long types, the bar and the cost coming and going.
func TestRule116HoldsAtEveryWidthWithLongTypes(t *testing.T) {
	checked, withBar := 0, 0
	for name, typ := range longTypes {
		for w := 50; w <= 230; w++ {
			m := atPricedAgents(t, nil, w, typedUsage(typ))
			if got := m.agentTable.RenderedWidth(); got != w-2 {
				t.Fatalf("%s at %d: rendered width = %d, want %d (Rule 116)", name, w, got, w-2)
			}
			if slices.Contains(header(t, m.View()), breakdownTitle) {
				withBar++
			}
			checked++
		}
	}
	if checked != len(longTypes)*181 {
		t.Fatalf("checked %d, want %d", checked, len(longTypes)*181)
	}
	if withBar == 0 || withBar == checked {
		t.Fatalf("the bar is shown in %d of %d layouts: the sweep proves nothing", withBar, checked)
	}
}

// Rule 114: the help says which columns give way before the type is cut.
func TestHelpSaysWhichColumnsYieldToTheAgentType(t *testing.T) {
	body := agentColumnsHelp(t)
	for _, want := range []string{
		"Cache write, Est. cost, Cache read, In and Breakdown give way before an agent's type is cut",
		"Model, State, Duration and Tools do not",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Agent columns help does not say %q:\n%s", want, body)
		}
	}
}
