// Package pricing turns the per-model token counts of internal/agentusage
// into an indicative cost in USD. It ships no price: the table is built from
// the user's configuration (agent_usage.prices), and a model without a rate
// has no cost rather than a zero one.
package pricing

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/anthnel/devdesk/internal/config"
)

// perMillion is the token count a rate is quoted for.
const perMillion = 1_000_000.0

// Rates is the price of one model, in USD per million tokens.
type Rates struct {
	Input        float64
	Output       float64
	CacheWrite5m float64
	CacheWrite1h float64
	CacheRead    float64
}

// Problem describes a price entry that was left out of the table. Key is the
// user's own spelling of the config key; Reason is meant for a log line.
type Problem struct {
	Key    string
	Reason string
}

// Table maps normalized model names to their rates. The zero value is a
// valid empty table.
type Table struct {
	rates map[string]Rates
}

// Len returns the number of priced models.
func (t Table) Len() int { return len(t.rates) }

// normalize makes a model name comparable: trimmed and lower case.
func normalize(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// namedRate is one of the five rates of an entry, with its config key.
type namedRate struct {
	name string
	rate config.Rate
	dst  *float64
}

// FromConfig builds a Table from the configured prices. An entry that is
// incomplete, holds a rate that is not a finite non-negative number, or whose
// key collides with another after normalization is left out with a Problem;
// the other entries are unaffected. Problems are sorted by key.
func FromConfig(prices map[string]config.ModelPrice) (Table, []Problem) {
	var problems []Problem
	type candidate struct {
		key   string
		rates Rates
	}
	byNorm := map[string][]candidate{}

	for key, p := range prices {
		norm := normalize(key)
		if norm == "" {
			problems = append(problems, Problem{Key: key, Reason: "empty model name"})
			continue
		}
		r, reason := ratesOf(p)
		if reason != "" {
			problems = append(problems, Problem{Key: key, Reason: reason})
			continue
		}
		byNorm[norm] = append(byNorm[norm], candidate{key: key, rates: r})
	}

	t := Table{}
	for norm, cs := range byNorm {
		if len(cs) > 1 {
			for _, c := range cs {
				problems = append(problems, Problem{
					Key:    c.key,
					Reason: fmt.Sprintf("duplicate of another key once case and spaces are ignored (%q)", norm),
				})
			}
			continue
		}
		if t.rates == nil {
			t.rates = map[string]Rates{}
		}
		t.rates[norm] = cs[0].rates
	}
	sortProblems(problems)
	return t, problems
}

// ratesOf validates the five rates of an entry and returns them, or a reason
// naming the first rate that is unusable.
func ratesOf(p config.ModelPrice) (Rates, string) {
	var r Rates
	list := []namedRate{
		{"input", p.Input, &r.Input},
		{"output", p.Output, &r.Output},
		{"cache_write_5m", p.CacheWrite5m, &r.CacheWrite5m},
		{"cache_write_1h", p.CacheWrite1h, &r.CacheWrite1h},
		{"cache_read", p.CacheRead, &r.CacheRead},
	}
	for _, nr := range list {
		v, state := nr.rate.Value()
		switch {
		case state == config.RateMissing:
			return Rates{}, "incomplete: missing " + nr.name
		case state != config.RateSet:
			return Rates{}, nr.name + " is not a number"
		case math.IsNaN(v) || math.IsInf(v, 0):
			return Rates{}, nr.name + " is not a finite number"
		case v < 0:
			return Rates{}, nr.name + " is negative"
		}
		*nr.dst = v
	}
	return r, ""
}

func sortProblems(ps []Problem) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Key != ps[j].Key {
			return ps[i].Key < ps[j].Key
		}
		return ps[i].Reason < ps[j].Reason
	})
}
