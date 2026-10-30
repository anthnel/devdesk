package pricing

import (
	"math"
	"strconv"
	"testing"

	"github.com/anthnel/devdesk/internal/agentusage"
)

const eps = 1e-9

func near(a, b float64) bool { return math.Abs(a-b) < eps }

const M = 1_000_000

// mu builds a ModelUsage; write is the total cache write, w1h its 1 h part.
func mu(model string, in, out, read, write, w1h int64) agentusage.ModelUsage {
	return agentusage.ModelUsage{
		Model:        model,
		Tokens:       agentusage.Tokens{Input: in, Output: out, CacheRead: read, CacheWrite: write},
		CacheWrite1h: w1h,
	}
}

// agentOf builds an agent whose Tokens is the sum of its ByModel, as the
// parser guarantees.
func agentOf(id string, main bool, by ...agentusage.ModelUsage) agentusage.Agent {
	a := agentusage.Agent{ID: id, Main: main, ByModel: by}
	for _, b := range by {
		a.Tokens = a.Tokens.Add(b.Tokens)
	}
	return a
}

// tableOf builds a Table from simple rates.
func tableOf(t *testing.T, rates map[string]Rates) Table {
	t.Helper()
	doc := ""
	for k, r := range rates {
		doc += entry(k,
			"input: "+ftoa(r.Input), "output: "+ftoa(r.Output),
			"cache_write_5m: "+ftoa(r.CacheWrite5m), "cache_write_1h: "+ftoa(r.CacheWrite1h),
			"cache_read: "+ftoa(r.CacheRead))
	}
	tbl, probs := FromConfig(prices(t, doc))
	if len(probs) != 0 {
		t.Fatalf("fixture table has problems: %v", probs)
	}
	return tbl
}

func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

var (
	rateA = Rates{Input: 1, Output: 10, CacheWrite5m: 2, CacheWrite1h: 4, CacheRead: 0.1}
	rateB = Rates{Input: 2, Output: 20, CacheWrite5m: 4, CacheWrite1h: 8, CacheRead: 0.2}
)

func TestRatesCost(t *testing.T) {
	tests := []struct {
		name string
		r    Rates
		m    agentusage.ModelUsage
		want float64
	}{
		{"plan example: 1M each, 400k of the write is 1 h", rateA, mu("a", M, M, M, M, 400_000), 13.9},
		{"input only", rateA, mu("a", M, 0, 0, 0, 0), 1},
		{"output only", rateA, mu("a", 0, M, 0, 0, 0), 10},
		{"cache read only", rateA, mu("a", 0, 0, M, 0, 0), 0.1},
		{"all cache write at 5 min", rateA, mu("a", 0, 0, 0, M, 0), 2},
		{"all cache write at 1 h", rateA, mu("a", 0, 0, 0, M, M), 4},
		{"5 min part is total minus 1 h", rateA, mu("a", 0, 0, 0, 300_000, 100_000), 0.2*2 + 0.1*4},
		{"fraction of a million", rateA, mu("a", 500_000, 250_000, 0, 0, 0), 0.5 + 2.5},
		{"no tokens", rateA, mu("a", 0, 0, 0, 0, 0), 0},
		{"zero rates", Rates{}, mu("a", M, M, M, M, M/2), 0},
		{"large counts stay finite", rateB, mu("a", 9_000_000_000, 0, 0, 0, 0), 18000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Cost(tc.m); !near(got, tc.want) {
				t.Errorf("Cost = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTableAgent(t *testing.T) {
	tbl := tableOf(t, map[string]Rates{"a": rateA, "b": rateB})
	tests := []struct {
		name         string
		agent        agentusage.Agent
		wantStatus   Status
		wantUSD      float64 // checked for Priced only
		wantUnpriced []string
	}{
		{"one priced model", agentOf("x", true, mu("a", M, M, 0, 0, 0)), Priced, 11, nil},
		{"two priced models are summed", agentOf("x", true, mu("a", M, 0, 0, 0, 0), mu("b", M, 0, 0, 0, 0)), Priced, 3, nil},
		{"model id is matched like Lookup (case, dated)", agentOf("x", true, mu("A-20251001", M, 0, 0, 0, 0)), Priced, 1, nil},
		{"no tokens at all", agentOf("x", true), NoTokens, 0, nil},
		{"models with zero tokens only", agentOf("x", true, mu("a", 0, 0, 0, 0, 0), mu("zz", 0, 0, 0, 0, 0)), NoTokens, 0, nil},
		{"one model unpriced", agentOf("x", true, mu("zz", M, 0, 0, 0, 0)), Unpriced, 0, []string{"zz"}},
		{"priced and unpriced: no partial sum", agentOf("x", true, mu("a", M, 0, 0, 0, 0), mu("zz", M, 0, 0, 0, 0)), Unpriced, 0, []string{"zz"}},
		{"unpriced model with zero tokens is ignored", agentOf("x", true, mu("a", M, 0, 0, 0, 0), mu("zz", 0, 0, 0, 0, 0)), Priced, 1, nil},
		{"the empty bucket with tokens is unpriced", agentOf("x", true, mu("a", M, 0, 0, 0, 0), mu("", 5, 0, 0, 0, 0)), Unpriced, 0, []string{"(no model)"}},
		{"the empty bucket alone with tokens", agentOf("x", true, mu("", 5, 0, 0, 0, 0)), Unpriced, 0, []string{"(no model)"}},
		{"the empty bucket with zero tokens is ignored", agentOf("x", true, mu("a", M, 0, 0, 0, 0), mu("", 0, 0, 0, 0, 0)), Priced, 1, nil},
		{"two unpriced models, first-appearance order", agentOf("x", false, mu("q", 1, 0, 0, 0, 0), mu("p", 1, 0, 0, 0, 0)), Unpriced, 0, []string{"q", "p"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tbl.Agent(tc.agent)
			if got.Status != tc.wantStatus {
				t.Fatalf("Status = %v, want %v (%+v)", got.Status, tc.wantStatus, got)
			}
			if tc.wantStatus == Priced && !near(got.USD, tc.wantUSD) {
				t.Errorf("USD = %v, want %v", got.USD, tc.wantUSD)
			}
			if tc.wantStatus == NoTokens && got.USD != 0 {
				t.Errorf("USD = %v, want 0 for NoTokens", got.USD)
			}
			if len(got.Unpriced) != len(tc.wantUnpriced) {
				t.Fatalf("Unpriced = %v, want %v", got.Unpriced, tc.wantUnpriced)
			}
			for i := range tc.wantUnpriced {
				if got.Unpriced[i] != tc.wantUnpriced[i] {
					t.Errorf("Unpriced = %v, want %v", got.Unpriced, tc.wantUnpriced)
				}
			}
		})
	}
}

func TestAgentWithAnEmptyTableIsUnpricedNeverZero(t *testing.T) {
	var tbl Table
	got := tbl.Agent(agentOf("x", true, mu("a", M, 0, 0, 0, 0)))
	if got.Status != Unpriced {
		t.Errorf("Status = %v, want Unpriced: an unknown cost must not read as 0", got.Status)
	}
}

func TestSum(t *testing.T) {
	p := func(usd float64) Estimate { return Estimate{USD: usd, Status: Priced} }
	tests := []struct {
		name       string
		in         []Estimate
		wantStatus Status
		wantUSD    float64
	}{
		{"nil: nothing to sum", nil, NoTokens, 0},
		{"all NoTokens", []Estimate{{Status: NoTokens}, {Status: NoTokens}}, NoTokens, 0},
		{"priced add up", []Estimate{p(1.5), p(2.25)}, Priced, 3.75},
		{"NoTokens does not hide the priced ones", []Estimate{{Status: NoTokens}, p(2)}, Priced, 2},
		{"any unpriced makes the sum unpriced", []Estimate{p(1), {Status: Unpriced, Unpriced: []string{"z"}}, p(2)}, Unpriced, 0},
		{"unpriced alone", []Estimate{{Status: Unpriced, Unpriced: []string{"z"}}}, Unpriced, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Sum(tc.in)
			if got.Status != tc.wantStatus {
				t.Fatalf("Status = %v, want %v", got.Status, tc.wantStatus)
			}
			if tc.wantStatus == Priced && !near(got.USD, tc.wantUSD) {
				t.Errorf("USD = %v, want %v", got.USD, tc.wantUSD)
			}
		})
	}
}

func TestSession(t *testing.T) {
	tbl := tableOf(t, map[string]Rates{"a": rateA})
	// 1 USD per million input at rateA.
	usd := func(v float64, model string) agentusage.ModelUsage {
		return mu(model, int64(v*M), 0, 0, 0, 0)
	}
	main := func(v float64) agentusage.Agent { return agentOf("main", true, usd(v, "a")) }
	sub := func(id string, v float64) agentusage.Agent { return agentOf(id, false, usd(v, "a")) }

	tests := []struct {
		name          string
		usage         agentusage.Usage
		wantKnown     bool
		wantUnattrib  float64
		wantUnpriced  []string
		wantEstimates []Status
	}{
		{
			name: "criterion 5: billed 10.00, agents 8.20 => 1.80",
			usage: agentusage.Usage{
				Agents: []agentusage.Agent{main(5), sub("s1", 3), sub("s2", 0.2)},
				Billed: agentusage.Billed{USD: 10, Known: true},
			},
			wantKnown: true, wantUnattrib: 1.8, wantEstimates: []Status{Priced, Priced, Priced},
		},
		{
			name: "stale table: agents 10.40 vs billed 10.00 => -0.40 kept",
			usage: agentusage.Usage{
				Agents: []agentusage.Agent{main(10), sub("s1", 0.4)},
				Billed: agentusage.Billed{USD: 10, Known: true},
			},
			wantKnown: true, wantUnattrib: -0.4, wantEstimates: []Status{Priced, Priced},
		},
		{
			name: "criterion 6: running session (billed unknown) => no unattributed",
			usage: agentusage.Usage{
				Agents: []agentusage.Agent{main(5)},
				Billed: agentusage.Billed{},
			},
			wantKnown: false, wantEstimates: []Status{Priced},
		},
		{
			name: "billed unknown, even with a stale USD value",
			usage: agentusage.Usage{
				Agents: []agentusage.Agent{main(5)},
				Billed: agentusage.Billed{USD: 99, Known: false},
			},
			wantKnown: false, wantEstimates: []Status{Priced},
		},
		{
			name: "one agent unpriced => unattributed not known",
			usage: agentusage.Usage{
				Agents: []agentusage.Agent{main(5), agentOf("s1", false, mu("zz", M, 0, 0, 0, 0))},
				Billed: agentusage.Billed{USD: 10, Known: true},
			},
			wantKnown: false, wantUnpriced: []string{"zz"}, wantEstimates: []Status{Priced, Unpriced},
		},
		{
			name: "an agent with no tokens does not block it",
			usage: agentusage.Usage{
				Agents: []agentusage.Agent{main(5), agentOf("s1", false)},
				Billed: agentusage.Billed{USD: 6, Known: true},
			},
			wantKnown: true, wantUnattrib: 1, wantEstimates: []Status{Priced, NoTokens},
		},
		{
			name: "billed 0 and agents 0 => 0, known",
			usage: agentusage.Usage{
				Agents: []agentusage.Agent{agentOf("main", true)},
				Billed: agentusage.Billed{USD: 0, Known: true},
			},
			wantKnown: true, wantUnattrib: 0, wantEstimates: []Status{NoTokens},
		},
		{
			name: "no agents at all, billed known => everything is unattributed",
			usage: agentusage.Usage{
				Billed: agentusage.Billed{USD: 3, Known: true},
			},
			wantKnown: true, wantUnattrib: 3, wantEstimates: []Status{},
		},
		{
			name: "unpriced list is sorted and distinct over the whole session",
			usage: agentusage.Usage{
				Agents: []agentusage.Agent{
					agentOf("main", true, mu("zz", 1, 0, 0, 0, 0), mu("", 1, 0, 0, 0, 0)),
					agentOf("s1", false, mu("mm", 1, 0, 0, 0, 0), mu("zz", 1, 0, 0, 0, 0)),
				},
				Billed: agentusage.Billed{USD: 1, Known: true},
			},
			wantKnown: false, wantUnpriced: []string{"(no model)", "mm", "zz"}, wantEstimates: []Status{Unpriced, Unpriced},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tbl.Session(tc.usage)
			if got.UnattributedKnown != tc.wantKnown {
				t.Fatalf("UnattributedKnown = %v, want %v", got.UnattributedKnown, tc.wantKnown)
			}
			if tc.wantKnown && !near(got.Unattributed, tc.wantUnattrib) {
				t.Errorf("Unattributed = %v, want %v", got.Unattributed, tc.wantUnattrib)
			}
			if !tc.wantKnown && got.Unattributed != 0 {
				t.Errorf("Unattributed = %v, want 0 when not known", got.Unattributed)
			}
			if got.Billed != tc.usage.Billed {
				t.Errorf("Billed = %+v, want it passed through (%+v)", got.Billed, tc.usage.Billed)
			}
			if len(got.Agents) != len(tc.usage.Agents) || len(got.Agents) != len(tc.wantEstimates) {
				t.Fatalf("len(Agents) = %d, want %d (same order as Usage.Agents)", len(got.Agents), len(tc.usage.Agents))
			}
			for i, s := range tc.wantEstimates {
				if got.Agents[i].Status != s {
					t.Errorf("Agents[%d].Status = %v, want %v", i, got.Agents[i].Status, s)
				}
			}
			if len(got.Unpriced) != len(tc.wantUnpriced) {
				t.Fatalf("Unpriced = %v, want %v", got.Unpriced, tc.wantUnpriced)
			}
			for i := range tc.wantUnpriced {
				if got.Unpriced[i] != tc.wantUnpriced[i] {
					t.Errorf("Unpriced = %v, want %v", got.Unpriced, tc.wantUnpriced)
				}
			}
		})
	}
}

func TestSessionAgentsKeepUsageOrder(t *testing.T) {
	tbl := tableOf(t, map[string]Rates{"a": rateA})
	u := agentusage.Usage{Agents: []agentusage.Agent{
		agentOf("main", true, mu("a", M, 0, 0, 0, 0)),
		agentOf("s1", false, mu("a", 3*M, 0, 0, 0, 0)),
		agentOf("s2", false, mu("a", 2*M, 0, 0, 0, 0)),
	}}
	got := tbl.Session(u)
	for i, want := range []float64{1, 3, 2} {
		if !near(got.Agents[i].USD, want) {
			t.Errorf("Agents[%d].USD = %v, want %v", i, got.Agents[i].USD, want)
		}
	}
}

// A finite rate times a huge count overflows to +Inf; that is not a cost.
func TestNonFiniteCostIsUnpriced(t *testing.T) {
	huge := Rates{Input: 1e300, Output: 1e300, CacheWrite5m: 1e300, CacheWrite1h: 1e300, CacheRead: 1e300}
	tbl := tableOf(t, map[string]Rates{"big": huge, "ok": rateA})
	bucket := mu("big", 1<<62, 1<<62, 1<<62, 1<<62, 0)

	if c := huge.Cost(bucket); !math.IsInf(c, 0) && !math.IsNaN(c) {
		t.Fatalf("fixture does not overflow: %v", c)
	}

	a := agentOf("a", true, bucket)
	e := tbl.Agent(a)
	if e.Status != Unpriced || e.USD != 0 {
		t.Errorf("Agent = %+v, want Unpriced with USD 0", e)
	}
	if len(e.Unpriced) != 1 || e.Unpriced[0] != "big" {
		t.Errorf("Agent.Unpriced = %v, want [big]", e.Unpriced)
	}

	u := agentusage.Usage{
		Agents: []agentusage.Agent{a, agentOf("b", false, mu("ok", 10, 10, 0, 0, 0))},
		Billed: agentusage.Billed{Known: true, USD: 5},
	}
	s := tbl.Session(u)
	if s.Agents[0].Status != Unpriced {
		t.Errorf("Session agent status = %v, want Unpriced", s.Agents[0].Status)
	}
	if s.UnattributedKnown || math.IsInf(s.Unattributed, 0) || math.IsNaN(s.Unattributed) {
		t.Errorf("Unattributed = %v known=%v, want unknown and finite", s.Unattributed, s.UnattributedKnown)
	}
	if len(s.Unpriced) != 1 || s.Unpriced[0] != "big" {
		t.Errorf("Session.Unpriced = %v, want [big]", s.Unpriced)
	}
}

// Two finite agents whose sum overflows are not Priced either.
func TestSumOverflowIsUnpriced(t *testing.T) {
	e := Sum([]Estimate{
		{USD: math.MaxFloat64, Status: Priced},
		{USD: math.MaxFloat64, Status: Priced},
	})
	if e.Status != Unpriced || e.USD != 0 {
		t.Errorf("Sum = %+v, want Unpriced with USD 0", e)
	}
}
