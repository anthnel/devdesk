package agentusage

import (
	"math"
	"reflect"
	"testing"
)

// DEVDESK-98 step 5: session-wide sums by kind (Usage.Categories) and by model
// (Usage.ModelTotals). Pure functions of a Usage.

// bmAgent builds an agent whose Tokens is the sum of its ByModel entries.
func bmAgent(by ...ModelUsage) Agent {
	var a Agent
	for _, m := range by {
		a.Tokens = a.Tokens.Add(m.Tokens)
	}
	a.ByModel = by
	return a
}

func mu(model string, in, out, read, write, w1h int64) ModelUsage {
	return ModelUsage{Model: model, Tokens: Tokens{Input: in, Output: out, CacheRead: read, CacheWrite: write}, CacheWrite1h: w1h}
}

func TestCategoriesTotal(t *testing.T) {
	tests := []struct {
		name string
		c    Categories
		want int64
	}{
		{"zero", Categories{}, 0},
		{"sum of the five", Categories{Input: 1, Output: 2, CacheWrite5m: 3, CacheWrite1h: 4, CacheRead: 5}, 15},
		{"saturates", Categories{Input: math.MaxInt64, Output: 1, CacheWrite5m: 1, CacheWrite1h: math.MaxInt64, CacheRead: 1}, math.MaxInt64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Total(); got != tt.want {
				t.Errorf("Total() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCategories(t *testing.T) {
	tests := []struct {
		name string
		u    Usage
		want Categories
	}{
		{"empty usage", Usage{}, Categories{}},
		{"agent without tokens", Usage{Agents: []Agent{{}}}, Categories{}},
		{
			"one agent, 5m and 1h split",
			Usage{Agents: []Agent{bmAgent(mu("m", 10, 20, 30, 100, 40))}},
			Categories{Input: 10, Output: 20, CacheRead: 30, CacheWrite5m: 60, CacheWrite1h: 40},
		},
		{
			"1h summed over agents and models",
			Usage{Agents: []Agent{
				bmAgent(mu("a", 1, 2, 3, 10, 4), mu("b", 1, 1, 1, 20, 5)),
				bmAgent(mu("a", 2, 2, 2, 30, 30)),
			}},
			Categories{Input: 4, Output: 5, CacheRead: 6, CacheWrite5m: 21, CacheWrite1h: 39},
		},
		{
			"agent without ByModel: all writes are 5m",
			Usage{Agents: []Agent{{Tokens: Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}}}},
			Categories{Input: 1, Output: 2, CacheRead: 3, CacheWrite5m: 4},
		},
		{
			"1h above the total write is clamped",
			Usage{Agents: []Agent{{
				Tokens:  Tokens{CacheWrite: 50},
				ByModel: []ModelUsage{{Tokens: Tokens{CacheWrite: 50}, CacheWrite1h: 80}},
			}}},
			Categories{CacheWrite5m: 0, CacheWrite1h: 50},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.u.Categories()
			if got != tt.want {
				t.Errorf("Categories() = %+v, want %+v", got, tt.want)
			}
			if gt, wt := got.Total(), tt.u.Total().Total(); gt != wt {
				t.Errorf("Categories().Total() = %d, want Usage.Total().Total() = %d", gt, wt)
			}
			if sum := got.CacheWrite5m + got.CacheWrite1h; sum != tt.u.Total().CacheWrite {
				t.Errorf("5m + 1h = %d, want Total().CacheWrite = %d", sum, tt.u.Total().CacheWrite)
			}
		})
	}
}

func TestCategoriesMaxInt64Counters(t *testing.T) {
	m := int64(math.MaxInt64)
	u := Usage{Agents: []Agent{
		{Tokens: Tokens{m, m, m, m}, ByModel: []ModelUsage{{Model: "x", Tokens: Tokens{m, m, m, m}, CacheWrite1h: m}}},
		{Tokens: Tokens{m, m, m, m}, ByModel: []ModelUsage{{Model: "x", Tokens: Tokens{m, m, m, m}, CacheWrite1h: m}}},
		{Tokens: Tokens{1, 1, 1, 1}},
	}}
	c := u.Categories() // must not panic
	for name, v := range map[string]int64{"Input": c.Input, "Output": c.Output, "CacheWrite5m": c.CacheWrite5m, "CacheWrite1h": c.CacheWrite1h, "CacheRead": c.CacheRead} {
		if v < 0 {
			t.Errorf("%s = %d, want >= 0", name, v)
		}
	}
	if got := c.Total(); got != m {
		t.Errorf("Total() = %d, want saturated %d", got, m)
	}
	for _, mt := range u.ModelTotals() { // must not panic either
		if mt.Tokens.Input < 0 || mt.Tokens.CacheWrite < 0 || mt.CacheWrite1h < 0 {
			t.Errorf("ModelTotals entry %+v has a negative field", mt)
		}
	}
}

func TestModelTotals(t *testing.T) {
	tests := []struct {
		name string
		u    Usage
		want []ModelUsage
	}{
		{"empty usage", Usage{}, nil},
		{
			"same model in two agents is merged",
			Usage{Agents: []Agent{
				bmAgent(mu("opus", 1, 2, 3, 10, 4)),
				bmAgent(mu("opus", 10, 20, 30, 100, 40)),
			}},
			[]ModelUsage{mu("opus", 11, 22, 33, 110, 44)},
		},
		{
			"ordered by tokens descending, then name",
			Usage{Agents: []Agent{bmAgent(
				mu("b", 5, 0, 0, 0, 0), mu("c", 50, 0, 0, 0, 0), mu("a", 5, 0, 0, 0, 0),
			)}},
			[]ModelUsage{mu("c", 50, 0, 0, 0, 0), mu("a", 5, 0, 0, 0, 0), mu("b", 5, 0, 0, 0, 0)},
		},
		{
			"unnamed bucket merged and last, even when largest",
			Usage{Agents: []Agent{
				bmAgent(mu("small", 1, 0, 0, 0, 0), mu("", 100, 0, 0, 0, 0)),
				bmAgent(mu("", 200, 0, 0, 0, 0)),
			}},
			[]ModelUsage{mu("small", 1, 0, 0, 0, 0), mu("", 300, 0, 0, 0, 0)},
		},
		{
			"agent with tokens and no ByModel goes to the unnamed bucket",
			Usage{Agents: []Agent{
				{Tokens: Tokens{Input: 7, Output: 3, CacheRead: 2, CacheWrite: 5}},
				bmAgent(mu("", 1, 1, 1, 1, 1)),
				bmAgent(mu("m", 4, 0, 0, 0, 0)),
			}},
			[]ModelUsage{mu("m", 4, 0, 0, 0, 0), mu("", 8, 4, 3, 6, 1)},
		},
		{
			"agent without tokens and without ByModel adds nothing",
			Usage{Agents: []Agent{{}, bmAgent(mu("m", 1, 0, 0, 0, 0))}},
			[]ModelUsage{mu("m", 1, 0, 0, 0, 0)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.u.ModelTotals()
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ModelTotals() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// sumModelTotals adds every entry of ModelTotals field-wise.
func sumModelTotals(ms []ModelUsage) (Tokens, int64) {
	var t Tokens
	var w1h int64
	for _, m := range ms {
		t = t.Add(m.Tokens)
		w1h += m.CacheWrite1h
	}
	return t, w1h
}

func TestModelTotalsSumEqualsTotal(t *testing.T) {
	u := Usage{Agents: []Agent{
		bmAgent(mu("a", 1, 2, 3, 4, 1), mu("", 5, 6, 7, 8, 2)),
		bmAgent(mu("a", 9, 8, 7, 6, 3), mu("b", 1, 1, 1, 1, 0)),
		{Tokens: Tokens{Input: 3, Output: 3, CacheRead: 3, CacheWrite: 3}},
	}}
	got, _ := sumModelTotals(u.ModelTotals())
	if want := u.Total(); got != want {
		t.Errorf("sum of ModelTotals = %+v, want Total() = %+v", got, want)
	}
}

// The fixture holds a resumed session with a main and a sub-agent transcript.
func TestBreakdownFixtureAgreesWithByModel(t *testing.T) {
	u := parseFixture(t, "v2.1.302-cost")
	got, got1h := sumModelTotals(u.ModelTotals())
	if want := u.Total(); got != want {
		t.Errorf("sum of ModelTotals = %+v, want Total() = %+v", got, want)
	}
	var want1h int64
	for _, a := range u.Agents {
		for _, m := range a.ByModel {
			want1h += m.CacheWrite1h
		}
	}
	if got1h != want1h {
		t.Errorf("sum of CacheWrite1h = %d, want %d (ByModel's)", got1h, want1h)
	}
	c := u.Categories()
	if c.Total() != u.Total().Total() {
		t.Errorf("Categories().Total() = %d, want %d", c.Total(), u.Total().Total())
	}
	if c.CacheWrite1h != want1h {
		t.Errorf("CacheWrite1h = %d, want %d", c.CacheWrite1h, want1h)
	}
	if c.CacheWrite5m+c.CacheWrite1h != u.Total().CacheWrite {
		t.Errorf("5m + 1h = %d, want %d", c.CacheWrite5m+c.CacheWrite1h, u.Total().CacheWrite)
	}
}

// The incremental Aggregate must give the same sums as the full parse.
func TestBreakdownAggregateEqualsFullParse(t *testing.T) {
	orig := fixtureSession(t, "v2.1.302-cost")
	s, _ := cloneSession(t, orig, false)
	a := NewAggregate(s.parent())
	readApply(t, a)
	want, err := ParseSession(orig)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}
	got := a.Usage()
	if g, w := got.Categories(), want.Categories(); g != w {
		t.Errorf("Categories: aggregate %+v, parse %+v", g, w)
	}
	if g, w := got.ModelTotals(), want.ModelTotals(); !reflect.DeepEqual(g, w) {
		t.Errorf("ModelTotals: aggregate %+v, parse %+v", g, w)
	}
}
