package netdiag

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
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
)

// pathTarget is the validated input of the path trace. Port is optional (0 =
// none); it is shown but does not change the probes.
type pathTarget struct {
	Host string
	Port int
}

// String renders the target as typed: "host" or "host:port".
func (t pathTarget) String() string {
	if t.Port == 0 {
		return t.Host
	}
	return t.Host + ":" + strconv.Itoa(t.Port)
}

// parsePathTarget validates "host" or "host:port". IPv6 literals are refused.
func parsePathTarget(input string) (pathTarget, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return pathTarget{}, errors.New("target is required")
	}
	// Brackets or several colons can only be an IPv6 literal.
	if strings.Contains(s, "[") || strings.Count(s, ":") > 1 {
		return pathTarget{}, errPathIPv6
	}
	host, portText, hasPort := strings.Cut(s, ":")
	if err := validateTarget(host); err != nil {
		return pathTarget{}, err
	}
	if !hasPort {
		return pathTarget{Host: host}, nil
	}
	if err := validatePort(portText); err != nil {
		return pathTarget{}, err
	}
	port, _ := strconv.Atoi(portText) // validatePort accepted it
	return pathTarget{Host: host, Port: port}, nil
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
	case errors.Is(err, nettrace.ErrNoRoute):
		return "No route to " + host + " from this machine — no probe was sent"
	case errors.Is(err, nettrace.ErrUnsupported):
		return "The path trace is not supported on this platform"
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
