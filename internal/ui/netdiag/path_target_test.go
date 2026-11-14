package netdiag

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/anthnel/devdesk/internal/nettrace"
)

func TestParsePathTarget(t *testing.T) {
	const (
		errPort = "port must be a number between 1 and 65535"
		errV6   = "Only IPv4 targets are supported by the path trace"
	)
	tests := []struct {
		name    string
		in      string
		want    pathTarget
		wantErr string
	}{
		{"ipv4", "1.1.1.1", pathTarget{Host: "1.1.1.1"}, ""},
		{"host with port", "example.com:443", pathTarget{Host: "example.com", Port: 443}, ""},
		{"trimmed", " host ", pathTarget{Host: "host"}, ""},
		{"ipv4 with port", "10.0.0.1:65535", pathTarget{Host: "10.0.0.1", Port: 65535}, ""},
		{"empty", "", pathTarget{}, "target is required"},
		{"blank", "   ", pathTarget{}, "target is required"},
		{"space inside", "a b", pathTarget{}, "target must be a hostname or IP address"},
		{"port zero", "host:0", pathTarget{}, errPort},
		{"port too large", "host:70000", pathTarget{}, errPort},
		{"port not a number", "host:x", pathTarget{}, errPort},
		{"ipv6 loopback", "::1", pathTarget{}, errV6},
		{"ipv6 bracketed with port", "[::1]:443", pathTarget{}, errV6},
		{"ipv6 global", "2001:db8::1", pathTarget{}, errV6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePathTarget(tt.in)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("parsePathTarget(%q) error = %v, want %q", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePathTarget(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parsePathTarget(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestPathTargetString(t *testing.T) {
	tests := []struct {
		tg   pathTarget
		want string
	}{
		{pathTarget{Host: "1.1.1.1"}, "1.1.1.1"},
		{pathTarget{Host: "example.com", Port: 443}, "example.com:443"},
	}
	for _, tt := range tests {
		if got := tt.tg.String(); got != tt.want {
			t.Errorf("%+v.String() = %q, want %q", tt.tg, got, tt.want)
		}
	}
}

func TestVisiblePathHops(t *testing.T) {
	h := func(ttl int, k nettrace.HopKind) nettrace.Hop { return nettrace.Hop{TTL: ttl, Kind: k} }
	ttls := func(hops []nettrace.Hop) []int {
		out := []int{}
		for _, x := range hops {
			out = append(out, x.TTL)
		}
		return out
	}
	tests := []struct {
		name string
		in   []nettrace.Hop
		want []int
	}{
		{"sorted by TTL", []nettrace.Hop{h(3, nettrace.HopRouter), h(1, nettrace.HopRouter)}, []int{1, 3}},
		{"cut after first target", []nettrace.Hop{h(1, nettrace.HopRouter), h(2, nettrace.HopTarget), h(3, nettrace.HopTarget)}, []int{1, 2}},
		{"cut after first target, unsorted", []nettrace.Hop{h(3, nettrace.HopTarget), h(2, nettrace.HopTarget), h(1, nettrace.HopRouter)}, []int{1, 2}},
		{"cut after unreachable", []nettrace.Hop{h(1, nettrace.HopRouter), h(2, nettrace.HopUnreachable), h(3, nettrace.HopRouter)}, []int{1, 2}},
		{"unresolved dropped", []nettrace.Hop{h(1, nettrace.HopUnresolved), h(2, nettrace.HopRouter)}, []int{2}},
		{"silent and error kept", []nettrace.Hop{h(1, nettrace.HopSilent), h(2, nettrace.HopError), h(3, nettrace.HopRouter)}, []int{1, 2, 3}},
		{"empty", nil, []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := append([]nettrace.Hop(nil), tt.in...)
			got := visiblePathHops(tt.in)
			if !reflect.DeepEqual(ttls(got), tt.want) {
				t.Errorf("TTLs = %v, want %v", ttls(got), tt.want)
			}
			if !reflect.DeepEqual(tt.in, orig) {
				t.Errorf("input was modified: %v, want %v", tt.in, orig)
			}
		})
	}
}

func TestPathErrorLabel(t *testing.T) {
	const host = "example.com"
	raw := fmt.Errorf("%w: %w", nettrace.ErrNoRoute, syscall.ENETUNREACH)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"resolve", errPathResolve, "Could not resolve example.com — no probe was sent"},
		{"resolve wrapped", fmt.Errorf("%w: %w", errPathResolve, errors.New("lookup example.com: i/o timeout")), "Could not resolve example.com — no probe was sent"},
		{"no ipv4", errPathNoIPv4, "example.com has no IPv4 address — the path trace is IPv4 only"},
		{"not ipv4", nettrace.ErrNotIPv4, "Only IPv4 targets are supported by the path trace"},
		{"not unicast", nettrace.ErrNotUnicast, "The path trace needs a unicast address"},
		{"no route", nettrace.ErrNoRoute, "No route to example.com from this machine — no probe was sent"},
		{"no route wrapped", raw, "No route to example.com from this machine — no probe was sent"},
		{"unsupported", nettrace.ErrUnsupported, "The path trace is not supported on this platform"},
		{"other", errors.New("sendto: operation not permitted"), "The path trace failed — check logs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathErrorLabel(host, tt.err)
			if got != tt.want {
				t.Errorf("pathErrorLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The label never carries the raw error text (errno, resolver message).
func TestPathErrorLabelNeverLeaksRawText(t *testing.T) {
	errs := []error{
		fmt.Errorf("%w: %w", nettrace.ErrNoRoute, syscall.ENETUNREACH),
		fmt.Errorf("%w: %w", errPathResolve, errors.New("lookup x on 10.0.0.53:53: no such host")),
		errors.New("sendto: operation not permitted"),
		nettrace.ErrNotIPv4,
		nettrace.ErrUnsupported,
	}
	for _, err := range errs {
		label := pathErrorLabel("h.example", err)
		for _, frag := range []string{err.Error(), "network is unreachable", "no such host", "operation not permitted", "10.0.0.53"} {
			if strings.Contains(label, frag) {
				t.Errorf("label %q leaks %q", label, frag)
			}
		}
	}
}

func TestPathOutcomeLabel(t *testing.T) {
	hops := func(n int) []nettrace.Hop {
		out := make([]nettrace.Hop, n)
		for i := range out {
			out[i] = nettrace.Hop{TTL: i + 1, Kind: nettrace.HopRouter}
		}
		return out
	}
	tests := []struct {
		name string
		res  nettrace.Result
		want string
	}{
		{"reached", nettrace.Result{Outcome: nettrace.OutcomeReached, Hops: hops(4)}, "Reached at hop 4"},
		{"unreachable", nettrace.Result{Outcome: nettrace.OutcomeUnreachable, Hops: hops(6)}, "Reported unreachable at hop 6"},
		{"max hops", nettrace.Result{Outcome: nettrace.OutcomeMaxHops, Hops: hops(30)}, "No answer from the target within 30 hops"},
		{"deadline", nettrace.Result{Outcome: nettrace.OutcomeDeadline, Hops: hops(30)}, "Stopped after 30 s"},
		{"canceled", nettrace.Result{Outcome: nettrace.OutcomeCancelled}, "Canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathOutcomeLabel(tt.res); got != tt.want {
				t.Errorf("pathOutcomeLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
