package nettrace

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"syscall"
	"time"
)

// prober opens one probe connection per TTL. The system implementation lives
// in the platform files; the tests drive run with a fake.
type prober interface {
	// open returns a connection whose probes leave with the given TTL.
	open(ctx context.Context, dst netip.Addr, ttl int) (probeConn, error)
}

// probeConn sends one probe and waits for what answers it.
type probeConn interface {
	// send emits the probe and returns the time it left.
	send() (time.Time, error)
	// await waits for the answer until the given time. It returns errSilent
	// when nothing came back by then, and ctx.Err() when ctx ended first.
	await(ctx context.Context, until time.Time) (reply, error)
	close() error
}

// errSilent is await's answer when nothing came back before its deadline.
var errSilent = errors.New("no answer before the probe timeout")

// probeResult is what one TTL's goroutine hands back. resolved is false when
// the probe was cut short (cancellation, deadline, upper TTL no longer needed).
type probeResult struct {
	ttl      int
	hop      Hop
	resolved bool
}

// run traces the path with p. Every connection is opened first, in the
// caller's goroutine, so that a failing open stops everything before any
// probe is sent; then one goroutine per TTL sends and awaits in parallel.
func run(ctx context.Context, target netip.Addr, opts Options, p prober) (Result, error) {
	opts = opts.Normalized()
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, opts.Deadline)
	defer cancel()

	conns, err := openAll(runCtx, p, target, opts.MaxHops)
	if err != nil {
		if runCtx.Err() != nil {
			return endedEarly(runCtx, target, nil, opts.MaxHops, start), nil
		}
		return Result{}, openError(err)
	}
	return probeAll(runCtx, target, opts, conns, start), nil
}

// openAll opens one connection per TTL, 1 to n. On failure it closes the ones
// already open and returns the error.
func openAll(ctx context.Context, p prober, target netip.Addr, n int) ([]probeConn, error) {
	conns := make([]probeConn, 0, n)
	for ttl := 1; ttl <= n; ttl++ {
		if err := ctx.Err(); err != nil {
			closeAll(conns)
			return nil, err
		}
		c, err := p.open(ctx, target, ttl)
		if err != nil {
			closeAll(conns)
			return nil, err
		}
		conns = append(conns, c)
	}
	return conns, nil
}

func closeAll(conns []probeConn) {
	for _, c := range conns {
		_ = c.close()
	}
}

// openError turns "the kernel refuses this socket option or socket" into
// ErrUnsupported, keeping the raw error for the message. Anything else is
// returned as is.
func openError(err error) error {
	for _, errno := range []error{syscall.ENOPROTOOPT, syscall.EOPNOTSUPP, syscall.EPERM, syscall.EACCES} {
		if errors.Is(err, errno) {
			return fmt.Errorf("%w: %w", ErrUnsupported, err)
		}
	}
	return err
}

// probeAll runs phase 2: one goroutine per connection.
func probeAll(ctx context.Context, target netip.Addr, opts Options, conns []probeConn, start time.Time) Result {
	n := len(conns)
	results := make(chan probeResult, n)
	cancels := make([]context.CancelFunc, n+1) // indexed by TTL
	var wg sync.WaitGroup
	for i, c := range conns {
		ttl := i + 1
		var tctx context.Context
		tctx, cancels[ttl] = context.WithCancel(ctx)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { _ = c.close() }()
			results <- probeOne(tctx, c, ttl, target, opts.ProbeTimeout)
		}()
	}

	t := newTally(n)
	cutAbove := func() {
		if term := t.terminal(); term > 0 {
			for ttl := term + 1; ttl <= n; ttl++ {
				cancels[ttl]()
			}
		}
	}
collect:
	for !t.complete() {
		select {
		case r := <-results:
			t.add(r)
			cutAbove()
		case <-ctx.Done():
			break collect
		}
	}
	for _, cancel := range cancels[1:] {
		cancel()
	}
	wg.Wait()
	close(results)
	for r := range results { // answers that landed while stopping
		t.add(r)
	}

	if t.complete() {
		return t.result(target, start)
	}
	return endedEarly(ctx, target, t, n, start)
}

// probeOne sends the probe for one TTL and turns what comes back into a Hop.
func probeOne(ctx context.Context, c probeConn, ttl int, target netip.Addr, timeout time.Duration) probeResult {
	sentAt, err := c.send()
	if err != nil {
		if ctx.Err() != nil {
			return probeResult{ttl: ttl}
		}
		return probeResult{ttl: ttl, hop: Hop{TTL: ttl, Kind: HopError, Err: err}, resolved: true}
	}
	rep, err := c.await(ctx, sentAt.Add(timeout))
	recvAt := time.Now()
	switch {
	case err != nil && ctx.Err() != nil:
		return probeResult{ttl: ttl}
	case errors.Is(err, errSilent):
		return probeResult{ttl: ttl, hop: Hop{TTL: ttl, Kind: HopSilent}, resolved: true}
	case err != nil:
		return probeResult{ttl: ttl, hop: Hop{TTL: ttl, Kind: HopError, Err: err}, resolved: true}
	}
	rtt := rep.RTT
	if rtt == 0 {
		rtt = recvAt.Sub(sentAt)
	}
	hop := Hop{TTL: ttl, Kind: classify(rep, target), Addr: rep.Addr, RTT: rtt, ICMPType: rep.ICMPType, ICMPCode: rep.ICMPCode}
	return probeResult{ttl: ttl, hop: hop, resolved: true}
}

// tally collects the resolved hops of a run.
type tally struct {
	hops []probeResult // indexed by TTL, index 0 unused
}

func newTally(n int) *tally { return &tally{hops: make([]probeResult, n+1)} }

func (t *tally) add(r probeResult) {
	if r.resolved {
		t.hops[r.ttl] = r
	}
}

// terminal returns the smallest TTL that reached or was refused, or 0.
func (t *tally) terminal() int {
	for ttl := 1; ttl < len(t.hops); ttl++ {
		if r := t.hops[ttl]; r.resolved && (r.hop.Kind == HopTarget || r.hop.Kind == HopUnreachable) {
			return ttl
		}
	}
	return 0
}

// complete reports whether the trace can end: every TTL up to the terminal
// one (or every TTL when there is none) is resolved.
func (t *tally) complete() bool {
	limit := len(t.hops) - 1
	if term := t.terminal(); term > 0 {
		limit = term
	}
	for ttl := 1; ttl <= limit; ttl++ {
		if !t.hops[ttl].resolved {
			return false
		}
	}
	return true
}

// result builds the Result of a complete tally.
func (t *tally) result(target netip.Addr, start time.Time) Result {
	last, outcome := len(t.hops)-1, OutcomeMaxHops
	if term := t.terminal(); term > 0 {
		last = term
		outcome = OutcomeReached
		if t.hops[term].hop.Kind == HopUnreachable {
			outcome = OutcomeUnreachable
		}
	}
	hops := make([]Hop, 0, last)
	for ttl := 1; ttl <= last; ttl++ {
		hops = append(hops, t.hops[ttl].hop)
	}
	return Result{Target: target, Outcome: outcome, Hops: hops, Duration: time.Since(start)}
}

// endedEarly builds the Result of a run that ctx cut short. A deadline lists
// every TTL, the unanswered ones as HopUnresolved; a cancellation keeps only
// the hops that were resolved. t is nil when nothing was probed yet.
func endedEarly(ctx context.Context, target netip.Addr, t *tally, n int, start time.Time) Result {
	res := Result{Target: target, Outcome: OutcomeCancelled, Duration: time.Since(start)}
	deadline := errors.Is(ctx.Err(), context.DeadlineExceeded)
	if deadline {
		res.Outcome = OutcomeDeadline
	}
	if t == nil {
		return res
	}
	for ttl := 1; ttl <= n; ttl++ {
		switch r := t.hops[ttl]; {
		case r.resolved:
			res.Hops = append(res.Hops, r.hop)
		case deadline:
			res.Hops = append(res.Hops, Hop{TTL: ttl, Kind: HopUnresolved})
		}
	}
	return res
}
