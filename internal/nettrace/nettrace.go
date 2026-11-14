package nettrace

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

const (
	// MaxHopsLimit is the highest TTL a trace will probe.
	MaxHopsLimit = 30
	// DefaultProbeTimeout is how long one hop waits for an answer.
	DefaultProbeTimeout = 3 * time.Second
	// DeadlineLimit is the longest a whole trace may run.
	DeadlineLimit = 30 * time.Second
)

var (
	// ErrUnsupported is returned on platforms without a trace engine.
	ErrUnsupported = errors.New("path trace not supported on this platform")
	// ErrNotIPv4 is returned for a target that is not an IPv4 address.
	ErrNotIPv4 = errors.New("path trace supports IPv4 targets only")
	// ErrNoRoute is returned when the system has no route to the target.
	ErrNoRoute = errors.New("no route to the target")
)

// Options tunes a trace. The zero value means "all defaults".
type Options struct {
	// MaxHops is the highest TTL probed, 1 to MaxHopsLimit.
	MaxHops int
	// ProbeTimeout is the wait for one hop's answer.
	ProbeTimeout time.Duration
	// Deadline bounds the whole trace.
	Deadline time.Duration
}

// Normalized returns the options with every out-of-range or unset value
// replaced by its default, and ProbeTimeout capped at Deadline.
func (o Options) Normalized() Options {
	if o.MaxHops <= 0 || o.MaxHops > MaxHopsLimit {
		o.MaxHops = MaxHopsLimit
	}
	if o.Deadline <= 0 || o.Deadline > DeadlineLimit {
		o.Deadline = DeadlineLimit
	}
	if o.ProbeTimeout <= 0 {
		o.ProbeTimeout = DefaultProbeTimeout
	}
	if o.ProbeTimeout > o.Deadline {
		o.ProbeTimeout = o.Deadline
	}
	return o
}

// HopKind says what a hop's probe got back.
type HopKind int

// The kinds of hop.
const (
	// HopSilent: no answer before the probe timeout.
	HopSilent HopKind = iota
	// HopRouter: an intermediate router answered (time exceeded).
	HopRouter
	// HopTarget: the target itself answered.
	HopTarget
	// HopUnreachable: a router reported the target as unreachable.
	HopUnreachable
	// HopUnresolved: an answer arrived but its sender could not be read.
	HopUnresolved
	// HopError: the probe could not be sent, or the answer was unexpected
	// (an error from the local stack, or a reply that is not ICMP).
	HopError
)

// String returns the kind's name.
func (k HopKind) String() string {
	switch k {
	case HopSilent:
		return "silent"
	case HopRouter:
		return "router"
	case HopTarget:
		return "target"
	case HopUnreachable:
		return "unreachable"
	case HopUnresolved:
		return "unresolved"
	case HopError:
		return "error"
	}
	return "unknown"
}

// Hop is the result of probing one TTL.
type Hop struct {
	TTL      int
	Kind     HopKind
	Addr     netip.Addr
	RTT      time.Duration
	ICMPType uint8
	ICMPCode uint8
	Err      error
}

// Outcome says why a trace ended.
type Outcome int

// The ways a trace ends. The zero value is OutcomeReached, so read an Outcome
// only when Trace returned no error.
const (
	// OutcomeReached: the target answered.
	OutcomeReached Outcome = iota
	// OutcomeMaxHops: MaxHops was exhausted without reaching the target.
	OutcomeMaxHops
	// OutcomeDeadline: the overall deadline expired.
	OutcomeDeadline
	// OutcomeCancelled: the caller's context was canceled.
	OutcomeCancelled
	// OutcomeUnreachable: a router reported the target as unreachable.
	OutcomeUnreachable
)

// String returns the outcome in US English.
func (o Outcome) String() string {
	switch o {
	case OutcomeReached:
		return "reached"
	case OutcomeMaxHops:
		return "max hops"
	case OutcomeDeadline:
		return "deadline"
	case OutcomeCancelled:
		return "canceled"
	case OutcomeUnreachable:
		return "unreachable"
	}
	return "unknown"
}

// Result is a finished trace. Always check the error Trace returned before
// reading it: the zero Result (and so the zero Outcome, OutcomeReached) means
// "no trace", never "reached".
type Result struct {
	Target   netip.Addr
	Outcome  Outcome
	Hops     []Hop
	Duration time.Duration
}

// Supported reports whether this platform can trace: nil, or ErrUnsupported.
func Supported() error {
	return supported()
}

// Trace probes the path to target. An IPv4-mapped IPv6 target is converted to
// IPv4; any other non-IPv4 target gets ErrNotIPv4 and nothing is sent.
func Trace(ctx context.Context, target netip.Addr, opts Options) (Result, error) {
	if err := supported(); err != nil {
		return Result{}, err
	}
	target = target.Unmap()
	if !target.Is4() {
		return Result{}, ErrNotIPv4
	}
	return run(ctx, target, opts, systemProber())
}
