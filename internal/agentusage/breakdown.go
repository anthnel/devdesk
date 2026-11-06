package agentusage

import "sort"

// Categories is a session's tokens by kind, the cache writes split by cache
// lifetime.
type Categories struct {
	Input, Output, CacheWrite5m, CacheWrite1h, CacheRead int64
}

// Total is the sum of the five kinds, saturating at math.MaxInt64.
func (c Categories) Total() int64 {
	return satAdd(satAdd(satAdd(c.Input, c.Output), satAdd(c.CacheWrite5m, c.CacheWrite1h)), c.CacheRead)
}

// Categories sums every agent's tokens by kind. Input, Output, CacheRead and
// the total cache write come from Agent.Tokens (so the total equals
// Usage.Total().Total()); the 1 h part is the sum of ByModel's CacheWrite1h,
// clamped to the total write; the 5 min part is the rest. An agent without
// ByModel counts all its writes as 5 min.
func (u Usage) Categories() Categories {
	t := u.Total()
	var w1h int64
	for _, a := range u.Agents {
		for _, m := range a.ByModel {
			w1h = satAdd(w1h, boundedWrite1h(m))
		}
	}
	w1h = min(w1h, t.CacheWrite)
	return Categories{
		Input:        t.Input,
		Output:       t.Output,
		CacheRead:    t.CacheRead,
		CacheWrite1h: w1h,
		CacheWrite5m: t.CacheWrite - w1h,
	}
}

// boundedWrite1h is an entry's 1 h cache write, kept within [0, its own total
// write] so one inconsistent entry cannot eat another's 5 min share.
func boundedWrite1h(m ModelUsage) int64 {
	return min(max(m.CacheWrite1h, 0), max(m.Tokens.CacheWrite, 0))
}

// ModelTotals merges every agent's ByModel by exact model name. An agent with
// tokens and no ByModel adds its tokens to the "" bucket (as pricing.Agent
// does). Order: tokens descending, then name; the "" bucket last.
func (u Usage) ModelTotals() []ModelUsage {
	byName := map[string]*ModelUsage{}
	add := func(m ModelUsage) {
		e := byName[m.Model]
		if e == nil {
			e = &ModelUsage{Model: m.Model}
			byName[m.Model] = e
		}
		e.Tokens = e.Tokens.Add(m.Tokens)
		e.CacheWrite1h = satAdd(e.CacheWrite1h, boundedWrite1h(m))
	}
	for _, a := range u.Agents {
		if len(a.ByModel) == 0 {
			if a.Tokens != (Tokens{}) {
				add(ModelUsage{Tokens: a.Tokens})
			}
			continue
		}
		for _, m := range a.ByModel {
			add(m)
		}
	}
	if len(byName) == 0 {
		return nil
	}
	out := make([]ModelUsage, 0, len(byName))
	for _, e := range byName {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Model == "") != (b.Model == "") {
			return b.Model == ""
		}
		if at, bt := a.Tokens.Total(), b.Tokens.Total(); at != bt {
			return at > bt
		}
		return a.Model < b.Model
	})
	return out
}
