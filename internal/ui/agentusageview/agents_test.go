package agentusageview

import (
	"math"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// orchestrator and subAgent build agents for the table-driven tests below.
func orchestrator(out int64, partial int) agentusage.Agent {
	return agentusage.Agent{
		Main: true, Models: []string{"model-a"}, PartialMessages: partial,
		Tokens: agentusage.Tokens{Output: out},
	}
}

func subAgent(id, typ string, out int64, partial int) agentusage.Agent {
	return agentusage.Agent{
		ID: id, Type: typ, Models: []string{"model-b"}, PartialMessages: partial,
		Tokens: agentusage.Tokens{Output: out},
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{845, "845"},
		{999, "999"},
		{1000, "1.0k"},
		{1049, "1.0k"},
		{1099, "1.0k"}, // floored, never rounded up
		{12345, "12.3k"},
		{99999, "99.9k"},
		{123456, "123k"},
		{999999, "999k"},
		{1_000_000, "1.0M"},
		{4_640_000, "4.6M"},
		{1_250_000_000, "1.2B"},
		{100_000_000, "100M"},
		{123_456_789, "123M"},
		{999_999_999, "999M"},
		{123_456_789_012, "123B"},
		{123_456_789_012_345, "123T"},
	}
	for _, tt := range tests {
		if got := formatTokens(tt.in); got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := formatTokens(math.MaxInt64); !strings.HasSuffix(got, "T") {
		t.Errorf("formatTokens(MaxInt64) = %q, want a T suffix", got)
	}
}

// The key criterion: the floor marker belongs to partial sub-agents only.
func TestOutCell(t *testing.T) {
	tests := []struct {
		name string
		row  agentRow
		want string
	}{
		{"partial sub-agent", agentRow{Agent: subAgent("a1", "coder", 8, 1)}, "≥ 8"},
		{"complete sub-agent", agentRow{Agent: subAgent("a1", "coder", 8, 0)}, "8"},
		{"orchestrator never partial", agentRow{Agent: orchestrator(320, 3)}, "320"},
		{"partial sub-agent, compact", agentRow{Agent: subAgent("a1", "coder", 12345, 2)}, "≥ 12.3k"},
		{"zero output", agentRow{Agent: subAgent("a1", "coder", 0, 0)}, "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outCell(tt.row); got != tt.want {
				t.Errorf("outCell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentRows(t *testing.T) {
	tests := []struct {
		name     string
		usage    agentusage.Usage
		wantIDs  []string
		wantLast []bool
	}{
		{
			name: "orchestrator and three sub-agents, sorted by ID",
			usage: agentusage.Usage{Agents: []agentusage.Agent{
				orchestrator(1, 0),
				subAgent("a03", "x", 1, 0), subAgent("a01", "x", 1, 0), subAgent("a02", "x", 1, 0),
			}},
			wantIDs:  []string{"", "a01", "a02", "a03"},
			wantLast: []bool{false, false, false, true},
		},
		{
			name:     "orchestrator alone has no stem",
			usage:    agentusage.Usage{Agents: []agentusage.Agent{orchestrator(1, 0)}},
			wantIDs:  []string{""},
			wantLast: []bool{false},
		},
		{
			name:  "no agents",
			usage: agentusage.Usage{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := agentRows(tt.usage)
			if len(rows) != len(tt.wantIDs) {
				t.Fatalf("got %d rows, want %d", len(rows), len(tt.wantIDs))
			}
			for i, r := range rows {
				if r.ID != tt.wantIDs[i] {
					t.Errorf("row %d ID = %q, want %q", i, r.ID, tt.wantIDs[i])
				}
				if r.last != tt.wantLast[i] {
					t.Errorf("row %d last = %v, want %v", i, r.last, tt.wantLast[i])
				}
			}
			if len(rows) > 0 && !rows[0].Main {
				t.Error("the orchestrator is not first")
			}
		})
	}
}

func TestRoleCell(t *testing.T) {
	tests := []struct {
		name string
		row  agentRow
		want string
	}{
		{"orchestrator", agentRow{Agent: orchestrator(1, 0)}, "orchestrator"},
		{"sub-agent", agentRow{Agent: subAgent("a1", "coder", 1, 0)}, "├─ coder"},
		{"last sub-agent", agentRow{Agent: subAgent("a1", "coder", 1, 0), last: true}, "└─ coder"},
		{"unknown type", agentRow{Agent: subAgent("a1", "", 1, 0)}, "├─ unknown"},
		{"unknown type, last", agentRow{Agent: subAgent("a1", "", 1, 0), last: true}, "└─ unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roleCell(tt.row); got != tt.want {
				t.Errorf("roleCell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModelsCell(t *testing.T) {
	tests := []struct {
		name   string
		models []string
		want   string
	}{
		{"one", []string{"model-a"}, "model-a"},
		{"two", []string{"a", "b"}, "a, b"},
		{"nil", nil, "-"},
		{"empty", []string{}, "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := agentRow{Agent: agentusage.Agent{Models: tt.models}}
			if got := modelsCell(r); got != tt.want {
				t.Errorf("modelsCell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentColumnsLayout(t *testing.T) {
	cols := agentColumns()
	wantTitles := []string{"Agent", "Model", "In", "Out", "Cache read", "Cache write", "Total"}
	if len(cols) != len(wantTitles) {
		t.Fatalf("got %d columns, want %d", len(cols), len(wantTitles))
	}
	for i, col := range cols {
		if col.Title != wantTitles[i] {
			t.Errorf("column %d = %q, want %q", i, col.Title, wantTitles[i])
		}
		if col.Sizing == datatable.SizingUnset {
			t.Errorf("column %q has no Sizing", col.Title)
		}
		if col.Less != nil {
			t.Errorf("column %q has a Less, sorting is out of scope", col.Title)
		}
		wantOptional := col.Title == "Cache read" || col.Title == "Cache write"
		if col.Optional != wantOptional {
			t.Errorf("column %q Optional = %v, want %v", col.Title, col.Optional, wantOptional)
		}
		if col.DropFirst != (col.Title == "Cache write") {
			t.Errorf("column %q DropFirst = %v", col.Title, col.DropFirst)
		}
	}
}

// Rule 122: a cell is measured before it is styled, so it carries no escape
// sequence of its own.
func TestNoAgentCellCarriesAnEscapeSequence(t *testing.T) {
	testutil.TrueColor(t)
	cols := agentColumns()
	if len(cols) == 0 {
		t.Fatal("the table has no columns")
	}
	rows := []agentRow{
		{Agent: orchestrator(320, 3)},
		{Agent: subAgent("a1", "coder", 8, 1)},
		{Agent: subAgent("a2", "", 0, 0), last: true},
		{Agent: agentusage.Agent{ID: "a3", Type: "\x1b[31mred", Models: []string{"\x1b[31mm"}}},
	}
	for _, row := range rows {
		for _, col := range cols {
			got := col.Cell(row)
			if strings.Contains(got, "\x1b") && !strings.Contains(row.Type+strings.Join(row.Models, ""), "\x1b") {
				t.Errorf("column %q adds an escape sequence: %q", col.Title, got)
			}
		}
	}
}

// Rule 122 "Color discipline": zeros and placeholders are dim, everything else
// keeps the default colour. The colour is a Style, never part of a Cell.
func TestAgentColumnsDimZeroesAndPlaceholders(t *testing.T) {
	dim := theme.DimStyle.GetForeground()
	zero := agentRow{Agent: agentusage.Agent{ID: "z"}} // no type, no model, 0 tokens
	full := agentRow{Agent: agentusage.Agent{
		ID: "f", Type: "coder", Models: []string{"m"},
		Tokens: agentusage.Tokens{Input: 5, Output: 6, CacheRead: 7, CacheWrite: 8},
	}}
	for _, col := range agentColumns() {
		if col.Style == nil {
			t.Errorf("column %q declares no Style", col.Title)
			continue
		}
		got := func(r agentRow) bool { return col.Style(r).GetForeground() == dim }
		switch col.Title {
		case "Agent":
			if !got(agentRow{Agent: agentusage.Agent{ID: "u"}, last: true}) {
				t.Errorf("Agent %q must be dim", unknownType)
			}
			if got(full) || got(agentRow{Agent: agentusage.Agent{Main: true}}) {
				t.Error("a known role keeps the default colour")
			}
		case "Model":
			if !got(zero) || got(full) {
				t.Error("Model: only the placeholder is dim")
			}
		default:
			if !got(zero) || got(full) {
				t.Errorf("%s: only a zero is dim", col.Title)
			}
		}
	}
}
