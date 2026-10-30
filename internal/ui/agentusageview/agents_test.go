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
		name        string
		usage       agentusage.Usage
		tree        bool
		wantIDs     []string
		wantBranch  []string
		wantShortID []string
	}{
		{
			name: "tree: orchestrator and three sub-agents, sorted by ID",
			usage: agentusage.Usage{Agents: []agentusage.Agent{
				orchestrator(1, 0),
				subAgent("a03", "x", 1, 0), subAgent("a01", "x", 1, 0), subAgent("a02", "x", 1, 0),
			}},
			tree:        true,
			wantIDs:     []string{"", "a01", "a02", "a03"},
			wantBranch:  []string{"", stem, stem, lastStem},
			wantShortID: []string{"", "a01", "a02", "a03"},
		},
		{
			name: "flat: no branch on any row",
			usage: agentusage.Usage{Agents: []agentusage.Agent{
				orchestrator(1, 0), subAgent("a02", "x", 1, 0), subAgent("a01", "x", 1, 0),
			}},
			tree:        false,
			wantIDs:     []string{"", "a01", "a02"},
			wantBranch:  []string{"", "", ""},
			wantShortID: []string{"", "a01", "a02"},
		},
		{
			name:        "orchestrator alone has no stem",
			usage:       agentusage.Usage{Agents: []agentusage.Agent{orchestrator(1, 0)}},
			tree:        true,
			wantIDs:     []string{""},
			wantBranch:  []string{""},
			wantShortID: []string{""},
		},
		{
			name:  "no agents",
			usage: agentusage.Usage{},
			tree:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := agentRows(tt.usage, tt.tree, statusClock{})
			if len(rows) != len(tt.wantIDs) {
				t.Fatalf("got %d rows, want %d", len(rows), len(tt.wantIDs))
			}
			for i, r := range rows {
				if r.ID != tt.wantIDs[i] {
					t.Errorf("row %d ID = %q, want %q", i, r.ID, tt.wantIDs[i])
				}
				if r.branch != tt.wantBranch[i] {
					t.Errorf("row %d branch = %q, want %q", i, r.branch, tt.wantBranch[i])
				}
				if r.shortID != tt.wantShortID[i] {
					t.Errorf("row %d shortID = %q, want %q", i, r.shortID, tt.wantShortID[i])
				}
			}
			if len(rows) > 0 && !rows[0].Main {
				t.Error("the orchestrator is not first")
			}
		})
	}
}

// The shares of a session add up to exactly 1000 tenths, the orchestrator
// absorbing the rounding residue.
func TestAgentRowsSharesAddUpAndTheOrchestratorAbsorbsTheResidue(t *testing.T) {
	// Totals 1000 / 5000 / 3000 / 200 of 9200 (the plan's criterion 2 case).
	withTotal := func(a agentusage.Agent, total int64) agentusage.Agent {
		a.Tokens = agentusage.Tokens{Output: total}
		return a
	}
	u := agentusage.Usage{Agents: []agentusage.Agent{
		withTotal(orchestrator(0, 0), 1000),
		withTotal(subAgent("a01", "coder", 0, 0), 5000),
		withTotal(subAgent("a02", "tester", 0, 1), 3000),
		withTotal(subAgent("a03", "reviewer", 0, 0), 200),
	}}
	rows := agentRows(u, true, statusClock{})
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	want := map[string]int{"a01": 543, "a02": 326, "a03": 21, "": 110}
	sum := 0
	for _, r := range rows {
		sum += r.share
		if r.share != want[r.ID] {
			t.Errorf("share of %q = %d, want %d", r.ID, r.share, want[r.ID])
		}
	}
	if sum != shareScale {
		t.Errorf("shares add up to %d, want %d", sum, shareScale)
	}
}

func TestAgentRowsOfASessionWithNoTokensHaveNoShare(t *testing.T) {
	u := agentusage.Usage{Agents: []agentusage.Agent{
		{Main: true}, {ID: "a01", Type: "coder"},
	}}
	for _, r := range agentRows(u, true, statusClock{}) {
		if r.share != noShare {
			t.Errorf("share of %q = %d, want noShare", r.ID, r.share)
		}
	}
}

// Criterion 3: two agents of the same type stay distinct.
func TestTwoAgentsOfTheSameTypeStayDistinct(t *testing.T) {
	u := agentusage.Usage{Agents: []agentusage.Agent{
		orchestrator(1, 0),
		subAgent("a0044dcf4954e5b04", "coder", 1, 0),
		subAgent("a029ee8f056637296", "coder", 1, 0),
	}}
	rows := agentRows(u, false, statusClock{})
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	a, b := rows[1], rows[2]
	if roleCell(a) != roleCell(b) {
		t.Fatalf("role cells differ (%q, %q): the test needs the same role", roleCell(a), roleCell(b))
	}
	if a.shortID == b.shortID || a.shortID == "" || b.shortID == "" {
		t.Errorf("short IDs = %q and %q, want two distinct non-empty values", a.shortID, b.shortID)
	}
}

func TestShortAgentIDs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"distinct at the minimum length",
			[]string{"a0044dcf4954e5b04", "a029ee8f056637296"},
			[]string{"a0044dcf", "a029ee8f"}},
		{"one common length when a pair collides",
			[]string{"a0044dcf4954e5b04", "a0044dcf9954e5b04", "a1111111111111111"},
			[]string{"a0044dcf4", "a0044dcf9", "a11111111"}},
		{"shorter than the minimum shows whole", []string{"x1"}, []string{"x1"}},
		{"identical IDs terminate and stay whole", []string{"abc", "abc"}, []string{"abc", "abc"}},
		{"multibyte IDs are cut on runes",
			[]string{"éééééééé1", "éééééééé2"},
			[]string{"éééééééé1", "éééééééé2"}},
		{"nil", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortAgentIDs(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("shortAgentIDs(%v) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("shortAgentIDs(%v)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
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
		{"sub-agent", agentRow{Agent: subAgent("a1", "coder", 1, 0), branch: stem}, "├─ coder"},
		{"last sub-agent", agentRow{Agent: subAgent("a1", "coder", 1, 0), branch: lastStem}, "└─ coder"},
		{"sub-agent in a sorted list", agentRow{Agent: subAgent("a1", "coder", 1, 0)}, "coder"},
		{"unknown type", agentRow{Agent: subAgent("a1", "", 1, 0), branch: stem}, "├─ unknown"},
		{"unknown type, last", agentRow{Agent: subAgent("a1", "", 1, 0), branch: lastStem}, "└─ unknown"},
		{"unknown type, sorted list", agentRow{Agent: subAgent("a1", "", 1, 0)}, "unknown"},
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
	wantTitles := []string{"Agent", "ID", "State", "Tools", "Duration", "Model", "In", "Out", "Cache read", "Cache write", "Total", "Share"}
	if len(cols) != len(wantTitles) {
		t.Fatalf("got %d columns, want %d", len(cols), len(wantTitles))
	}
	optional := map[string]bool{"State": true, "Tools": true, "Duration": true, "Model": true, "In": true, "Cache read": true, "Cache write": true}
	for i, col := range cols {
		if col.Title != wantTitles[i] {
			t.Errorf("column %d = %q, want %q", i, col.Title, wantTitles[i])
		}
		if col.Sizing == datatable.SizingUnset {
			t.Errorf("column %q has no Sizing", col.Title)
		}
		if (col.Less != nil) != (col.Title == "Total") {
			t.Errorf("column %q Less set = %v: only Total sorts", col.Title, col.Less != nil)
		}
		if col.DescFirst != (col.Title == "Total") {
			t.Errorf("column %q DescFirst = %v: only Total sorts largest first", col.Title, col.DescFirst)
		}
		if col.Optional != optional[col.Title] {
			t.Errorf("column %q Optional = %v, want %v", col.Title, col.Optional, optional[col.Title])
		}
		if col.DropFirst != (col.Title == "Cache write") {
			t.Errorf("column %q DropFirst = %v", col.Title, col.DropFirst)
		}
	}
}

// column returns the agents column with the given title.
func column(t *testing.T, title string) datatable.Column[agentRow] {
	t.Helper()
	for _, c := range agentColumns() {
		if c.Title == title {
			return c
		}
	}
	t.Fatalf("no %q column", title)
	return datatable.Column[agentRow]{}
}

func TestTotalColumnSortsByTotalTokens(t *testing.T) {
	less := column(t, "Total").Less
	if less == nil {
		t.Fatal("Total has no Less")
	}
	at := func(total int64) agentRow {
		return agentRow{Agent: agentusage.Agent{Tokens: agentusage.Tokens{Input: total / 2, Output: total - total/2}}}
	}
	if !less(at(100), at(200)) {
		t.Error("100 must sort before 200")
	}
	if less(at(200), at(100)) || less(at(100), at(100)) {
		t.Error("larger or equal must not sort before")
	}
	// A partial sub-agent's output changes nothing in the ranking.
	partial := agentRow{Agent: subAgent("a1", "x", 100, 1)}
	exact := agentRow{Agent: subAgent("a1", "x", 100, 0)}
	if less(partial, exact) || less(exact, partial) {
		t.Error("PartialMessages must not change the ranking")
	}
}

func TestIDAndShareCells(t *testing.T) {
	id, share := column(t, "ID"), column(t, "Share")
	tests := []struct {
		name         string
		row          agentRow
		wantID, want string
	}{
		{"orchestrator", agentRow{Agent: orchestrator(1, 0), share: 110}, "-", "11.0%"},
		{"sub-agent", agentRow{Agent: subAgent("a0044dcf4954e5b04", "coder", 1, 0), shortID: "a0044dcf", share: 543}, "a0044dcf", "54.3%"},
		{"no tokens", agentRow{Agent: subAgent("a1", "coder", 0, 0), shortID: "a1", share: noShare}, "a1", "-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := id.Cell(tt.row); got != tt.wantID {
				t.Errorf("ID cell = %q, want %q", got, tt.wantID)
			}
			if got := share.Cell(tt.row); got != tt.want {
				t.Errorf("Share cell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIDColumnSearchesTheFullID(t *testing.T) {
	r := agentRow{Agent: subAgent("a0044dcf4954e5b04", "coder", 1, 0), shortID: "a0044dcf"}
	col := column(t, "ID")
	if col.Search == nil || !strings.Contains(col.Search(r), "a0044dcf4954e5b04") {
		t.Error("`/` must keep matching the full agent ID, now through the ID column")
	}
	if got := agentSearch(r); strings.Contains(got, "a0044dcf4954e5b04") {
		t.Errorf("the ID moved to its own column, agentSearch still holds it: %q", got)
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
		{Agent: subAgent("a2", "", 0, 0), branch: lastStem, shortID: "a2", share: noShare},
		{Agent: subAgent("a4", "coder", 5, 0), shortID: "a4", share: 123},
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
			if !got(agentRow{Agent: agentusage.Agent{ID: "u"}, branch: lastStem}) {
				t.Errorf("Agent %q must be dim", unknownType)
			}
			if got(full) || got(agentRow{Agent: agentusage.Agent{Main: true}}) {
				t.Error("a known role keeps the default colour")
			}
		case "ID":
			if !got(agentRow{Agent: agentusage.Agent{Main: true}}) {
				t.Error("the orchestrator's placeholder ID must be dim")
			}
			if got(agentRow{Agent: agentusage.Agent{ID: "a1"}, shortID: "a1"}) {
				t.Error("a real ID keeps the default colour")
			}
		case "Share":
			if !got(agentRow{share: 0}) || !got(agentRow{share: noShare}) || got(agentRow{share: 123}) {
				t.Error("Share: only 0 and the placeholder are dim")
			}
		case "State", "Tools", "Duration":
			continue // covered by activity_columns_test.go
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
