// Package agentusage reads coding-agent usage logs. It is pure stdlib and
// has no dependency on the UI.
package agentusage

import "math"

// Tokens counts the tokens of one or more agent requests.
type Tokens struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Total returns the sum of the four counters, saturating at math.MaxInt64.
func (t Tokens) Total() int64 {
	return satAdd(satAdd(t.Input, t.Output), satAdd(t.CacheRead, t.CacheWrite))
}

// Add returns the field-wise sum of t and o, each field saturating at
// math.MaxInt64.
func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{
		Input:      satAdd(t.Input, o.Input),
		Output:     satAdd(t.Output, o.Output),
		CacheRead:  satAdd(t.CacheRead, o.CacheRead),
		CacheWrite: satAdd(t.CacheWrite, o.CacheWrite),
	}
}

// satAdd adds two non-negative counters without wrapping.
func satAdd(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}
