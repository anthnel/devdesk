package netdiag

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/anthnel/devdesk/internal/nettrace"
)

var (
	// errPathResolve marks a target name that could not be resolved.
	errPathResolve = errors.New("could not resolve the path target")
	// errPathNoIPv4 marks a target name that resolved to no IPv4 address.
	errPathNoIPv4 = errors.New("the path target has no IPv4 address")
	// errPathIPv6 is the validation message for a literal IPv6 target. It is
	// shown as is in the footer, hence the capital letter.
	errPathIPv6 = errors.New( //nolint:staticcheck // ST1005: user-facing text, same wording as pathErrorLabel
		"Only IPv4 targets are supported by the path trace")
	// errPathPort is the validation message for a target carrying a port. It
	// is shown as is in the footer, hence the capital letter.
	errPathPort = errors.New( //nolint:staticcheck // ST1005: user-facing text
		"Path ignores ports: enter a host or IP address only")
)

// pathTarget is the validated input of the path trace: a host name or an IPv4
// address. The trace probes the path to a host, not to a service, so a port is
// refused at parse time (errPathPort).
type pathTarget struct {
	Host string
}

// String renders the target as typed.
func (t pathTarget) String() string { return t.Host }

// parsePathTarget validates a bare host name or IPv4 address. A port ("host:80",
// "[::1]:443", "host:") is refused with errPathPort; IPv6 literals ("::1",
// "[::1]") are refused with errPathIPv6.
func parsePathTarget(input string) (pathTarget, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return pathTarget{}, errors.New("target is required")
	}
	// "[addr]:xxx" is a bracketed literal followed by a port.
	if strings.HasPrefix(s, "[") && strings.Contains(s, "]:") {
		return pathTarget{}, errPathPort
	}
	// Brackets or several colons can only be an IPv6 literal.
	if strings.Contains(s, "[") || strings.Count(s, ":") > 1 {
		return pathTarget{}, errPathIPv6
	}
	// Exactly one colon: whatever follows it is a port, valid or not.
	if strings.Contains(s, ":") {
		return pathTarget{}, errPathPort
	}
	if err := validateTarget(s); err != nil {
		return pathTarget{}, err
	}
	return pathTarget{Host: s}, nil
}

// visiblePathHops returns a new slice of the hops to display: sorted by TTL,
// unresolved hops dropped, everything above the first target or unreachable
// hop cut. The input is left untouched.
func visiblePathHops(hops []nettrace.Hop) []nettrace.Hop {
	sorted := slices.Clone(hops)
	slices.SortStableFunc(sorted, func(a, b nettrace.Hop) int { return a.TTL - b.TTL })
	out := make([]nettrace.Hop, 0, len(sorted))
	for _, h := range sorted {
		if h.Kind == nettrace.HopUnresolved {
			continue
		}
		out = append(out, h)
		if h.Kind == nettrace.HopTarget || h.Kind == nettrace.HopUnreachable {
			break
		}
	}
	return out
}

// pathErrorLabel maps a trace error to a fixed footer sentence. It never
// carries the raw error text (errno, resolver message).
func pathErrorLabel(host string, err error) string {
	switch {
	case errors.Is(err, errPathResolve):
		return "Could not resolve " + host + " — no probe was sent"
	case errors.Is(err, errPathNoIPv4):
		return host + " has no IPv4 address — the path trace is IPv4 only"
	case errors.Is(err, nettrace.ErrNotIPv4):
		return "Only IPv4 targets are supported by the path trace"
	case errors.Is(err, nettrace.ErrNotUnicast):
		return "The path trace needs a unicast address"
	case nettrace.IsNoRoute(err):
		return "No route to " + host + " from this machine — no probe was sent"
	case errors.Is(err, nettrace.ErrUnsupported):
		return reasonPathUnsupported
	}
	return "The path trace failed — check logs"
}

// pathOutcomeLabel describes how a finished trace ended; N is the number of
// hops in the result.
func pathOutcomeLabel(res nettrace.Result) string {
	n := len(res.Hops)
	switch res.Outcome {
	case nettrace.OutcomeReached:
		return fmt.Sprintf("Reached at hop %d", n)
	case nettrace.OutcomeUnreachable:
		return fmt.Sprintf("Reported unreachable at hop %d", n)
	case nettrace.OutcomeMaxHops:
		return fmt.Sprintf("No answer from the target within %d hops", n)
	case nettrace.OutcomeDeadline:
		return fmt.Sprintf("Stopped after %d s", int(nettrace.DeadlineLimit.Seconds()))
	case nettrace.OutcomeCancelled:
		return "Canceled"
	}
	return "The path trace ended"
}
