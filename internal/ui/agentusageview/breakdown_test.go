package agentusageview

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/agentusage/pricing"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// bdModel builds a ByModel entry whose Tokens carry the given counters.
func bdModel(name string, in, out, write, write1h, read int64) agentusage.ModelUsage {
	return agentusage.ModelUsage{
		Model:        name,
		Tokens:       agentusage.Tokens{Input: in, Output: out, CacheWrite: write, CacheRead: read},
		CacheWrite1h: write1h,
	}
}

// bdAgent builds an agent whose Tokens are the sum of its ByModel.
func bdAgent(id, typ string, main bool, models ...agentusage.ModelUsage) agentusage.Agent {
	a := agentusage.Agent{ID: id, Type: typ, Main: main, ByModel: models}
	for _, m := range models {
		a.Tokens = a.Tokens.Add(m.Tokens)
	}
	return a
}

// bdUsage is a session with an orchestrator, two coders and an unnamed model bucket.
func bdUsage() agentusage.Usage {
	return agentusage.Usage{Agents: []agentusage.Agent{
		bdAgent("", "", true, bdModel("model-a", 101, 203, 307, 100, 409), bdModel("", 3, 5, 7, 0, 11)),
		bdAgent("aaaa1111", "coder", false, bdModel("model-b", 17, 19, 23, 23, 29)),
		bdAgent("bbbb2222", "coder", false, bdModel("model-a", 31, 37, 41, 0, 43)),
	}}
}

func buildFor(u agentusage.Usage) breakdown {
	return buildBreakdown(u, agentRows(u, true, statusClock{}, pricing.Table{}))
}

func blockByTitle(t *testing.T, b breakdown, title string) breakdownBlock {
	t.Helper()
	for _, bl := range b.blocks {
		if bl.title == title {
			return bl
		}
	}
	t.Fatalf("no block %q in %+v", title, b.blocks)
	return breakdownBlock{}
}

func TestBuildBreakdownHasThreeBlocksInOrder(t *testing.T) {
	b := buildFor(bdUsage())
	var titles []string
	for _, bl := range b.blocks {
		titles = append(titles, bl.title)
	}
	if want := []string{"By category", "By model", "By agent"}; !slices.Equal(titles, want) {
		t.Fatalf("titles = %v, want %v", titles, want)
	}
}

func TestBuildBreakdownEachBlockSumsToAThousandTenths(t *testing.T) {
	usages := map[string]agentusage.Usage{
		"mixed": bdUsage(),
		"single orchestrator": {Agents: []agentusage.Agent{
			bdAgent("", "", true, bdModel("m", 1, 1, 1, 0, 1)),
		}},
		"thirds": {Agents: []agentusage.Agent{
			bdAgent("", "", true, bdModel("m1", 1, 0, 0, 0, 0)),
			bdAgent("x", "t", false, bdModel("m2", 1, 0, 0, 0, 0)),
			bdAgent("y", "t", false, bdModel("m3", 1, 0, 0, 0, 0)),
		}},
		"huge": {Agents: []agentusage.Agent{
			bdAgent("", "", true, bdModel("m1", 1<<62, 1<<61, 0, 0, 1)),
			bdAgent("x", "t", false, bdModel("m2", 7, 0, 0, 0, 0)),
		}},
	}
	for name, u := range usages {
		t.Run(name, func(t *testing.T) {
			for _, bl := range buildFor(u).blocks {
				sum := 0
				for _, l := range bl.lines {
					if l.tenths < 0 {
						t.Fatalf("%s: tenths %d in a session with tokens", bl.title, l.tenths)
					}
					sum += l.tenths
				}
				if sum != shareScale {
					t.Errorf("%s sums to %d tenths, want %d", bl.title, sum, shareScale)
				}
			}
		})
	}
}

func TestBuildBreakdownTokensMatchTheSessionTotal(t *testing.T) {
	u := bdUsage()
	want := u.Total().Total()
	for _, bl := range buildFor(u).blocks {
		var sum int64
		for _, l := range bl.lines {
			sum += l.tokens
		}
		if sum != want {
			t.Errorf("%s: tokens sum to %d, want %d", bl.title, sum, want)
		}
		if bl.total != want {
			t.Errorf("%s: total = %d, want %d", bl.title, bl.total, want)
		}
	}
}

func TestBuildBreakdownCategoryBlock(t *testing.T) {
	u := bdUsage()
	bl := blockByTitle(t, buildFor(u), "By category")
	c := u.Categories()
	want := []breakdownLine{
		{label: "Input", role: theme.TokenRoleInput, tokens: c.Input},
		{label: "Output", role: theme.TokenRoleOutput, tokens: c.Output},
		{label: "Cache write 5 min", role: theme.TokenRoleCacheWrite, tokens: c.CacheWrite5m},
		{label: "Cache write 1 h", role: theme.TokenRoleCacheWrite, tokens: c.CacheWrite1h},
		{label: "Cache read", role: theme.TokenRoleCacheRead, tokens: c.CacheRead},
	}
	if len(bl.lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(bl.lines), len(want), bl.lines)
	}
	for i, w := range want {
		g := bl.lines[i]
		if g.label != w.label || g.role != w.role || g.tokens != w.tokens {
			t.Errorf("line %d = {%q %q %d}, want {%q %q %d}", i, g.label, g.role, g.tokens, w.label, w.role, w.tokens)
		}
	}
	if c.CacheWrite1h == 0 {
		t.Fatal("fixture must carry 1 h writes")
	}
}

func TestBuildBreakdownResidueGoesToTheLargestEntry(t *testing.T) {
	// Three equal categories: 333+333+333 leaves 1, which goes to the first.
	u := agentusage.Usage{Agents: []agentusage.Agent{bdAgent("", "", true, bdModel("m", 1, 1, 0, 0, 1))}}
	bl := blockByTitle(t, buildFor(u), "By category")
	got := []int{bl.lines[0].tenths, bl.lines[1].tenths, bl.lines[4].tenths}
	if want := []int{334, 333, 333}; !slices.Equal(got, want) {
		t.Errorf("input/output/read tenths = %v, want %v", got, want)
	}
	for _, i := range []int{2, 3} {
		if bl.lines[i].tenths != 0 {
			t.Errorf("line %d (%s) tenths = %d, want 0", i, bl.lines[i].label, bl.lines[i].tenths)
		}
	}
}

func TestBuildBreakdownModelBlock(t *testing.T) {
	bl := blockByTitle(t, buildFor(bdUsage()), "By model")
	var labels []string
	for _, l := range bl.lines {
		labels = append(labels, l.label)
		if l.role != "" {
			t.Errorf("model line %q carries role %q, want none", l.label, l.role)
		}
	}
	if want := []string{"model-a", "model-b", "unknown"}; !slices.Equal(labels, want) {
		t.Errorf("labels = %v, want %v", labels, want)
	}
	// model-a is merged across the orchestrator and the second coder.
	if want := int64(101 + 203 + 307 + 409 + 31 + 37 + 41 + 43); bl.lines[0].tokens != want {
		t.Errorf("model-a tokens = %d, want %d", bl.lines[0].tokens, want)
	}
}

func TestBuildBreakdownUnknownIsLastEvenWhenLargest(t *testing.T) {
	u := agentusage.Usage{Agents: []agentusage.Agent{
		bdAgent("", "", true, bdModel("", 900, 0, 0, 0, 0), bdModel("small", 10, 0, 0, 0, 0)),
	}}
	bl := blockByTitle(t, buildFor(u), "By model")
	if n := len(bl.lines); n != 2 || bl.lines[n-1].label != "unknown" || bl.lines[0].label != "small" {
		t.Fatalf("lines = %+v, want small then unknown", bl.lines)
	}
	// The residue goes to the largest entry, wherever it sits.
	if sum := bl.lines[0].tenths + bl.lines[1].tenths; sum != shareScale {
		t.Errorf("sum = %d, want %d", sum, shareScale)
	}
}

func TestBuildBreakdownAgentBlockEqualsTheShareColumn(t *testing.T) {
	u := bdUsage()
	rows := agentRows(u, true, statusClock{}, pricing.Table{})
	bl := blockByTitle(t, buildBreakdown(u, rows), "By agent")
	if len(bl.lines) != len(rows) {
		t.Fatalf("got %d lines for %d rows", len(bl.lines), len(rows))
	}
	for i, r := range rows {
		if bl.lines[i].tenths != r.share {
			t.Errorf("line %d (%s) tenths = %d, Share column = %d", i, bl.lines[i].label, bl.lines[i].tenths, r.share)
		}
		if bl.lines[i].tokens != r.Tokens.Total() {
			t.Errorf("line %d tokens = %d, want %d", i, bl.lines[i].tokens, r.Tokens.Total())
		}
		if bl.lines[i].role != "" {
			t.Errorf("agent line %d carries role %q, want none", i, bl.lines[i].role)
		}
	}
}

func TestBuildBreakdownAgentLabelsAndTwoCodersGetTwoIDs(t *testing.T) {
	u := bdUsage()
	rows := agentRows(u, true, statusClock{}, pricing.Table{})
	bl := blockByTitle(t, buildBreakdown(u, rows), "By agent")
	if bl.lines[0].label != orchestratorLabel {
		t.Errorf("first label = %q, want %q", bl.lines[0].label, orchestratorLabel)
	}
	want1 := "coder " + idCell(rows[1])
	want2 := "coder " + idCell(rows[2])
	if bl.lines[1].label != want1 || bl.lines[2].label != want2 {
		t.Errorf("labels = %q, %q, want %q, %q", bl.lines[1].label, bl.lines[2].label, want1, want2)
	}
	if bl.lines[1].label == bl.lines[2].label {
		t.Errorf("two coders share the label %q", bl.lines[1].label)
	}
}

func TestBuildBreakdownIgnoresTableOrderAndTreeDrawing(t *testing.T) {
	u := bdUsage()
	flat := buildBreakdown(u, agentRows(u, false, statusClock{}, pricing.Table{}))
	tree := buildBreakdown(u, agentRows(u, true, statusClock{}, pricing.Table{}))
	for i := range flat.blocks {
		if !slices.Equal(flat.blocks[i].lines, tree.blocks[i].lines) {
			t.Errorf("%s differs between flat and tree rows", flat.blocks[i].title)
		}
	}
	for _, l := range tree.blocks[2].lines {
		if strings.ContainsAny(l.label, "├└│─") {
			t.Errorf("label %q carries branch drawing", l.label)
		}
	}
}

func TestBuildBreakdownSessionWithoutTokens(t *testing.T) {
	usages := map[string]agentusage.Usage{
		"empty":          {},
		"zero orchestra": {Agents: []agentusage.Agent{{Main: true}}},
		"zero subagent":  {Agents: []agentusage.Agent{{Main: true}, {ID: "abcd1234", Type: "coder"}}},
	}
	for name, u := range usages {
		t.Run(name, func(t *testing.T) {
			b := buildFor(u) // must not panic
			for _, bl := range b.blocks {
				if bl.total != 0 {
					t.Errorf("%s total = %d, want 0", bl.title, bl.total)
				}
				for _, l := range bl.lines {
					if l.tenths != noShare || l.tokens != 0 {
						t.Errorf("%s/%s = {tokens %d, tenths %d}, want {0, noShare}", bl.title, l.label, l.tokens, l.tenths)
					}
				}
			}
		})
	}
}

func TestBuildBreakdownOnlyCountersAndNamesReachThePanel(t *testing.T) {
	u := bdUsage()
	allowed := map[string]bool{"orchestrator": true, "coder": true, "unknown": true,
		"model-a": true, "model-b": true,
		"Input": true, "Output": true, "Cache write 5 min": true, "Cache write 1 h": true, "Cache read": true}
	for _, a := range u.Agents {
		if !a.Main {
			allowed[a.ID] = true
		}
	}
	rows := agentRows(u, true, statusClock{}, pricing.Table{})
	for _, bl := range buildBreakdown(u, rows).blocks {
		for _, l := range bl.lines {
			for _, word := range strings.Fields(l.label) {
				// a short ID is a prefix of an agent ID, anything else must be listed
				ok := allowed[word] || allowed[l.label] || slices.ContainsFunc(u.Agents, func(a agentusage.Agent) bool {
					return !a.Main && strings.HasPrefix(a.ID, word)
				}) || word == "5" || word == "min" || word == "1" || word == "h" || word == "write" || word == "Cache" || word == "read"
				if !ok {
					t.Errorf("%s: label %q has unexpected word %q", bl.title, l.label, word)
				}
			}
		}
	}
}

func TestLargestIndex(t *testing.T) {
	tests := []struct {
		name string
		in   []int64
		want int
	}{
		{"nil", nil, -1},
		{"empty", []int64{}, -1},
		{"single", []int64{5}, 0},
		{"largest in the middle", []int64{1, 9, 3}, 1},
		{"first on a tie", []int64{4, 9, 9, 2}, 1},
		{"all equal", []int64{1, 1, 1}, 0},
		{"all zero", []int64{0, 0}, 0},
		{"last", []int64{1, 2, 3}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := largestIndex(tt.in); got != tt.want {
				t.Errorf("largestIndex(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
