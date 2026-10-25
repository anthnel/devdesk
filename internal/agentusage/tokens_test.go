package agentusage

import (
	"math"
	"testing"
)

func TestTokensTotal(t *testing.T) {
	tests := []struct {
		name string
		in   Tokens
		want int64
	}{
		{"zero", Tokens{}, 0},
		{"all four", Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}, 10},
		{"one field", Tokens{CacheWrite: 7}, 7},
		{"beyond int32", Tokens{Input: 3_000_000_000, Output: 3_000_000_000}, 6_000_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Total(); got != tt.want {
				t.Errorf("Total() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTokensAdd(t *testing.T) {
	tests := []struct {
		name string
		a, b Tokens
		want Tokens
	}{
		{"zeros", Tokens{}, Tokens{}, Tokens{}},
		{"identity", Tokens{Input: 5, Output: 6}, Tokens{}, Tokens{Input: 5, Output: 6}},
		{
			"field-wise",
			Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
			Tokens{Input: 10, Output: 20, CacheRead: 30, CacheWrite: 40},
			Tokens{Input: 11, Output: 22, CacheRead: 33, CacheWrite: 44},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Add(tt.b); got != tt.want {
				t.Errorf("Add() = %+v, want %+v", got, tt.want)
			}
			if got := tt.b.Add(tt.a); got != tt.want {
				t.Errorf("Add() not commutative: %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTokensAddDoesNotMutate(t *testing.T) {
	a := Tokens{Input: 1}
	_ = a.Add(Tokens{Input: 2})
	if a.Input != 1 {
		t.Errorf("receiver mutated: %+v", a)
	}
}

const maxI = int64(math.MaxInt64)

// Behavior change: counters saturate instead of wrapping to negative.
func TestTokensAddSaturates(t *testing.T) {
	got := Tokens{Input: maxI, Output: maxI - 1, CacheRead: 5, CacheWrite: maxI}.
		Add(Tokens{Input: maxI, Output: 5, CacheRead: 5, CacheWrite: 1})
	want := Tokens{Input: maxI, Output: maxI, CacheRead: 10, CacheWrite: maxI}
	if got != want {
		t.Fatalf("Add() = %+v, want %+v", got, want)
	}
}

func TestTokensTotalSaturates(t *testing.T) {
	got := Tokens{Input: maxI, Output: maxI, CacheRead: 1, CacheWrite: maxI}.Total()
	if got != maxI {
		t.Fatalf("Total() = %d, want MaxInt64", got)
	}
	if got := (Tokens{Input: maxI, Output: maxI}).Total(); got < 0 {
		t.Fatalf("Total() negative: %d", got)
	}
}
