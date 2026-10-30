package pricing

import (
	"math"
	"sort"

	"github.com/anthnel/devdesk/internal/agentusage"
)

// noModelLabel names the bucket of tokens recorded under no model.
const noModelLabel = "(no model)"

// Status tells what an Estimate holds.
type Status int

const (
	// NoTokens means there was nothing to price; USD is 0 and means nothing.
	NoTokens Status = iota
	// Priced means every model with tokens has a rate; USD is the cost.
	Priced
	// Unpriced means at least one model with tokens has no rate. USD is 0 and
	// must not be shown: a partial sum would under-state without saying so.
	Unpriced
)

// Estimate is the indicative cost of an agent, or of a sum of agents.
type Estimate struct {
	USD    float64
	Status Status
	// Unpriced lists the models with tokens and no rate, in first-appearance
	// order; the bucket of no model is "(no model)".
	Unpriced []string
}

// Cost returns the cost in USD of one model's tokens. The 5 minute cache
// write is the total write minus its 1 hour part.
func (r Rates) Cost(m agentusage.ModelUsage) float64 {
	t := m.Tokens
	return (float64(t.Input)*r.Input +
		float64(t.Output)*r.Output +
		float64(m.CacheWrite5m())*r.CacheWrite5m +
		float64(m.CacheWrite1h)*r.CacheWrite1h +
		float64(t.CacheRead)*r.CacheRead) / perMillion
}

// hasTokens reports whether a model bucket carries any token.
func hasTokens(m agentusage.ModelUsage) bool {
	t := m.Tokens
	return t.Input != 0 || t.Output != 0 || t.CacheRead != 0 || t.CacheWrite != 0
}

// Agent estimates one agent. It never returns a partial cost: one model with
// tokens and no rate makes the whole agent Unpriced.
func (t Table) Agent(a agentusage.Agent) Estimate {
	var e Estimate
	buckets := a.ByModel
	if len(buckets) == 0 && a.Tokens != (agentusage.Tokens{}) {
		// Tokens without a split cannot be priced per model.
		buckets = []agentusage.ModelUsage{{Tokens: a.Tokens}}
	}
	for _, m := range buckets {
		if !hasTokens(m) {
			continue
		}
		e.Status = Priced
		r, ok := t.Lookup(m.Model)
		if !ok {
			e.Unpriced = appendOnce(e.Unpriced, modelLabel(m.Model))
			continue
		}
		cost := r.Cost(m)
		if !isFinite(cost) {
			// A huge rate times a huge count is not a cost worth showing.
			e.Unpriced = appendOnce(e.Unpriced, modelLabel(m.Model))
			continue
		}
		e.USD += cost
	}
	if len(e.Unpriced) > 0 || !isFinite(e.USD) {
		return Estimate{Status: Unpriced, Unpriced: e.Unpriced}
	}
	return e
}

// isFinite reports whether f is neither NaN nor an infinity.
func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func modelLabel(model string) string {
	if model == "" {
		return noModelLabel
	}
	return model
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// Sum adds estimates: Unpriced if any is, NoTokens if all are.
func Sum(es []Estimate) Estimate {
	var out Estimate
	for _, e := range es {
		switch e.Status {
		case Priced:
			out.USD += e.USD
			if out.Status == NoTokens {
				out.Status = Priced
			}
		case Unpriced:
			out.Status = Unpriced
			for _, m := range e.Unpriced {
				out.Unpriced = appendOnce(out.Unpriced, m)
			}
		}
	}
	if !isFinite(out.USD) {
		// The sum of finite estimates can still overflow.
		out.Status = Unpriced
	}
	if out.Status == Unpriced {
		out.USD = 0
	}
	return out
}

// Summary is the cost picture of a whole session.
type Summary struct {
	Agents []Estimate // same order as Usage.Agents
	Billed agentusage.Billed
	// Unattributed is Billed minus the agents' estimates; it may be negative
	// (the price table is higher than what was recorded). It is meaningful
	// only when UnattributedKnown.
	Unattributed      float64
	UnattributedKnown bool
	Unpriced          []string // sorted, distinct, over the whole session
}

// Session estimates every agent of a session. Unattributed is known only when
// Billed is known and every agent with tokens is priced.
func (t Table) Session(u agentusage.Usage) Summary {
	s := Summary{Agents: make([]Estimate, len(u.Agents)), Billed: u.Billed}
	for i, a := range u.Agents {
		s.Agents[i] = t.Agent(a)
	}
	total := Sum(s.Agents)
	s.Unpriced = total.Unpriced
	sort.Strings(s.Unpriced)
	if u.Billed.Known && total.Status != Unpriced {
		s.Unattributed = u.Billed.USD - total.USD
		s.UnattributedKnown = true
	}
	return s
}
