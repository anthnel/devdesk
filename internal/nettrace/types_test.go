package nettrace

import (
	"errors"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	errs := map[string]error{"ErrUnsupported": ErrUnsupported, "ErrNotIPv4": ErrNotIPv4, "ErrNoRoute": ErrNoRoute, "ErrNotUnicast": ErrNotUnicast}
	for name, err := range errs {
		if err == nil || err.Error() == "" {
			t.Errorf("%s must be a non-empty error", name)
		}
		for other, o := range errs {
			if other != name && errors.Is(err, o) {
				t.Errorf("%s must not match %s", name, other)
			}
		}
	}
}

func TestHopKindString(t *testing.T) {
	tests := []struct {
		kind HopKind
		want string
	}{
		{HopSilent, "silent"},
		{HopRouter, "router"},
		{HopTarget, "target"},
		{HopUnreachable, "unreachable"},
		{HopUnresolved, "unresolved"},
		{HopError, "error"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("HopKind(%d).String() = %q, want %q", int(tt.kind), got, tt.want)
		}
	}
	if HopKind(99).String() == "" {
		t.Error("an unknown HopKind must still render a non-empty string")
	}
}

func TestOutcomeString(t *testing.T) {
	tests := []struct {
		outcome Outcome
		want    string
	}{
		{OutcomeReached, "reached"},
		{OutcomeMaxHops, "max hops"},
		{OutcomeDeadline, "deadline"},
		{OutcomeCancelled, "canceled"},
		{OutcomeUnreachable, "unreachable"},
	}
	for _, tt := range tests {
		if got := tt.outcome.String(); got != tt.want {
			t.Errorf("Outcome(%d).String() = %q, want %q", int(tt.outcome), got, tt.want)
		}
	}
	if Outcome(99).String() == "" {
		t.Error("an unknown Outcome must still render a non-empty string")
	}
}
