package nettrace

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TraceEach streams each resolved hop to a callback. These tests drive the
// engine (run) with the fake prober of engine_test.go, so they run on every
// platform; the public TraceEach is only checked where no socket is needed.

// hopRecorder is a callback that keeps what it is given in a plain slice, with
// no lock: the contract is that calls are sequential, so -race must stay clean.
// It also counts overlapping calls and calls made after the run returned.
type hopRecorder struct {
	hops    []Hop
	inCall  atomic.Int32
	over    atomic.Bool
	after   atomic.Int32
	overlap atomic.Int32
	// nap widens the window in which an overlapping call would be seen.
	nap time.Duration
	// also runs inside the callback, after recording (n = calls so far).
	also func(h Hop, n int)
}

func (r *hopRecorder) onHop(h Hop) {
	if r.over.Load() {
		r.after.Add(1)
	}
	if r.inCall.Add(1) != 1 {
		r.overlap.Add(1)
	}
	r.hops = append(r.hops, h)
	if r.nap > 0 {
		time.Sleep(r.nap)
	}
	if r.also != nil {
		r.also(h, len(r.hops))
	}
	r.inCall.Add(-1)
}

// finish marks the run as returned. Call it right after run returns.
func (r *hopRecorder) finish() { r.over.Store(true) }

func (r *hopRecorder) check(t *testing.T) {
	t.Helper()
	if n := r.after.Load(); n != 0 {
		t.Errorf("%d callback calls after the run returned", n)
	}
	if n := r.overlap.Load(); n != 0 {
		t.Errorf("%d callback calls overlapped another one", n)
	}
	for _, h := range r.hops {
		if h.Kind == HopUnresolved {
			t.Errorf("callback got an unresolved hop: %+v", h)
		}
	}
}

func (r *hopRecorder) ttls() []int {
	out := make([]int, 0, len(r.hops))
	for _, h := range r.hops {
		out = append(out, h.TTL)
	}
	sort.Ints(out)
	return out
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameHop compares what a hop says, not its measured RTT.
func sameHop(a, b Hop) bool {
	return a.TTL == b.TTL && a.Kind == b.Kind && a.Addr == b.Addr &&
		a.ICMPType == b.ICMPType && a.ICMPCode == b.ICMPCode && errors.Is(a.Err, b.Err)
}

func sameHops(a, b []Hop) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameHop(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestRunStreamsEachResolvedHopOnce(t *testing.T) {
	// TTL 2 is silent (a resolved hop), TTL 5 never answers and is cut.
	newProber := func() *fakeProber {
		return newFake(5, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
			1: {rep: routerReply("10.0.0.1"), delay: 5 * time.Millisecond},
			2: {}, // silent until the probe timeout
			3: {rep: routerReply("10.0.0.3"), delay: 10 * time.Millisecond},
			4: {rep: targetReply(), delay: 15 * time.Millisecond},
		}))
	}
	opts := Options{MaxHops: 5, ProbeTimeout: 150 * time.Millisecond}

	rec := &hopRecorder{nap: 5 * time.Millisecond}
	fp := newProber()
	res, err := run(context.Background(), engineTarget, opts, fp, rec.onHop)
	rec.finish()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rec.check(t)
	if got, want := rec.ttls(), []int{1, 2, 3, 4}; !sameInts(got, want) {
		t.Errorf("callback TTLs = %v, want %v (each resolved hop once, none above the target)", got, want)
	}
	for _, h := range rec.hops {
		if h.TTL == 2 && h.Kind != HopSilent {
			t.Errorf("TTL 2 Kind = %v, want silent", h.Kind)
		}
	}
	assertBalanced(t, fp)

	// The Result is the same with and without a callback.
	plain, err := run(context.Background(), engineTarget, opts, newProber(), nil)
	if err != nil {
		t.Fatalf("run with nil callback: %v", err)
	}
	if plain.Outcome != res.Outcome || !sameHops(plain.Hops, res.Hops) {
		t.Errorf("Result differs with a callback:\n with: %v %+v\n nil:  %v %+v", res.Outcome, res.Hops, plain.Outcome, plain.Hops)
	}
	// And here what the callback saw is exactly the Result's hops.
	byTTL := append([]Hop(nil), rec.hops...)
	sort.Slice(byTTL, func(i, j int) bool { return byTTL[i].TTL < byTTL[j].TTL })
	if !sameHops(byTTL, res.Hops) {
		t.Errorf("callback hops %+v differ from Result.Hops %+v", byTTL, res.Hops)
	}
}

// A nil callback gives the same result whatever the scenario.
func TestRunNilCallbackMatchesCallback(t *testing.T) {
	silentForever := probeSpec{ignoreUntil: true}
	tests := []struct {
		name string
		n    int
		spec func(int) probeSpec
		want Outcome
	}{
		{"reaches the target", 6, table(silentForever, map[int]probeSpec{
			1: {rep: routerReply("10.0.0.1")}, 2: {rep: routerReply("10.0.0.2"), delay: 5 * time.Millisecond},
			3: {rep: targetReply(), delay: 10 * time.Millisecond},
		}), OutcomeReached},
		{"all silent ends at max hops", 3, table(probeSpec{}, nil), OutcomeMaxHops},
		{"router reports unreachable", 4, table(silentForever, map[int]probeSpec{
			1: {rep: routerReply("10.0.0.1")}, 2: {rep: unreachReply("10.0.0.2", 1), delay: 5 * time.Millisecond},
		}), OutcomeUnreachable},
		{"probe errors become error hops", 3, table(probeSpec{}, map[int]probeSpec{
			1: {sendErr: syscall.EPERM},
		}), OutcomeMaxHops},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{MaxHops: tc.n, ProbeTimeout: 100 * time.Millisecond}
			rec := &hopRecorder{}
			with, err := run(context.Background(), engineTarget, opts, newFake(tc.n, tc.spec), rec.onHop)
			rec.finish()
			if err != nil {
				t.Fatalf("run with callback: %v", err)
			}
			without, err := run(context.Background(), engineTarget, opts, newFake(tc.n, tc.spec), nil)
			if err != nil {
				t.Fatalf("run with nil callback: %v", err)
			}
			if with.Outcome != tc.want || without.Outcome != tc.want {
				t.Errorf("Outcome with/without = %v/%v, want %v", with.Outcome, without.Outcome, tc.want)
			}
			if !sameHops(with.Hops, without.Hops) {
				t.Errorf("Hops differ:\n with: %+v\n nil:  %+v", with.Hops, without.Hops)
			}
			rec.check(t)
		})
	}
}

// Calls are sequential even when every probe answers at the same instant.
func TestRunCallbackCallsAreSequential(t *testing.T) {
	const n = 12
	fp := newFake(n, table(probeSpec{rep: routerReply("10.0.0.1"), delay: 10 * time.Millisecond}, nil)).withBarrier(n)
	rec := &hopRecorder{nap: 3 * time.Millisecond}
	res, err := run(context.Background(), engineTarget, Options{MaxHops: n, ProbeTimeout: time.Second}, fp, rec.onHop)
	rec.finish()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Outcome != OutcomeMaxHops || len(res.Hops) != n {
		t.Fatalf("Outcome = %v, %d hops; want max hops, %d", res.Outcome, len(res.Hops), n)
	}
	rec.check(t)
	if len(rec.hops) != n {
		t.Errorf("callback got %d hops, want %d", len(rec.hops), n)
	}
	assertBalanced(t, fp)
}

// Cancel with two hops resolved: the callback saw those two, never an
// unresolved one, and never anything once run returned.
func TestRunCancelMidTraceStreamsOnlyResolvedHops(t *testing.T) {
	fp := newFake(5, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 5 * time.Millisecond},
		2: {rep: routerReply("10.0.0.2"), delay: 10 * time.Millisecond},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &hopRecorder{also: func(_ Hop, n int) {
		if n == 2 {
			cancel()
		}
	}}
	done := make(chan runOut, 1)
	go func() {
		res, err := run(ctx, engineTarget, Options{MaxHops: 5, ProbeTimeout: 10 * time.Second}, fp, rec.onHop)
		rec.finish()
		done <- runOut{res, err}
	}()
	out := waitRun(t, done)
	if out.err != nil {
		t.Fatalf("run: %v", out.err)
	}
	if out.res.Outcome != OutcomeCancelled {
		t.Errorf("Outcome = %v, want canceled", out.res.Outcome)
	}
	if got, want := rec.ttls(), []int{1, 2}; !sameInts(got, want) {
		t.Errorf("callback TTLs = %v, want %v", got, want)
	}
	rec.check(t)
	// The Result still carries the unresolved entries; the callback never did.
	unresolved := 0
	for _, h := range out.res.Hops {
		if h.Kind == HopUnresolved {
			unresolved++
		}
	}
	if unresolved != 3 {
		t.Errorf("Result has %d unresolved hops, want 3", unresolved)
	}
	assertBalanced(t, fp)
}

// An answer that lands while the run is stopping is handed over before run
// returns (the drain loop), once, and nothing comes after.
func TestRunDrainedAnswerIsStreamedBeforeReturn(t *testing.T) {
	const n = 4
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fp := newFake(n, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 5 * time.Millisecond},
	}))
	fp.onReply = func(int) { cancel() } // the answer races the cancellation
	rec := &hopRecorder{}
	done := make(chan runOut, 1)
	go func() {
		res, err := run(ctx, engineTarget, Options{MaxHops: n, ProbeTimeout: 10 * time.Second}, fp, rec.onHop)
		rec.finish()
		done <- runOut{res, err}
	}()
	out := waitRun(t, done)
	if out.err != nil {
		t.Fatalf("run: %v", out.err)
	}
	rec.check(t)
	// Whether the answer won the race or not, the callback agrees with the Result.
	resolved := 0
	for _, h := range out.res.Hops {
		if h.Kind != HopUnresolved {
			resolved++
		}
	}
	if len(rec.hops) != resolved {
		t.Errorf("callback got %d hops, Result has %d resolved: %+v", len(rec.hops), resolved, out.res.Hops)
	}
}

// A hop above the target can answer before the target does: the callback gets
// it while the terminal is unknown, and the Result is cut at the target.
func TestRunStreamsHopAboveTerminalAnsweringFirst(t *testing.T) {
	fp := newFake(5, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 10 * time.Millisecond},
		2: {rep: routerReply("10.0.0.2"), delay: 10 * time.Millisecond},
		3: {rep: routerReply("10.0.0.3"), delay: 10 * time.Millisecond},
		5: {rep: routerReply("10.0.0.5"), delay: 5 * time.Millisecond},
		4: {rep: targetReply(), delay: 60 * time.Millisecond},
	}))
	rec := &hopRecorder{}
	res, err := run(context.Background(), engineTarget, Options{MaxHops: 5, ProbeTimeout: 5 * time.Second}, fp, rec.onHop)
	rec.finish()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rec.check(t)
	if got := rec.hops; len(got) != 5 || got[0].TTL != 5 || got[4].TTL != 4 {
		t.Errorf("callback hops = %+v, want TTL 5 first, TTL 1-3 next, TTL 4 last", got)
	}
	if res.Outcome != OutcomeReached || len(res.Hops) != 4 || res.Hops[3].Kind != HopTarget {
		t.Errorf("Result = %v with %d hops, want reached and cut at the target (TTL 4)", res.Outcome, len(res.Hops))
	}
	assertBalanced(t, fp)
}

// An open that fails: no probe, no callback, the error comes back.
func TestRunOpenFailureNeverCallsBack(t *testing.T) {
	routeErr := fmt.Errorf("connect: %w: %w", ErrNoRoute, syscall.ENETUNREACH)
	fp := newFake(6, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{3: {openErr: routeErr}}))
	rec := &hopRecorder{}
	res, err := run(context.Background(), engineTarget, Options{MaxHops: 6, ProbeTimeout: time.Second}, fp, rec.onHop)
	rec.finish()
	if !errors.Is(err, ErrNoRoute) || !errors.Is(err, syscall.ENETUNREACH) {
		t.Fatalf("err = %v, want ErrNoRoute wrapping ENETUNREACH", err)
	}
	if len(rec.hops) != 0 {
		t.Errorf("callback called %d times after an open failure, want never: %+v", len(rec.hops), rec.hops)
	}
	if len(res.Hops) != 0 {
		t.Errorf("Result = %+v, want the zero Result", res)
	}
	if got := fp.sends.Load(); got != 0 {
		t.Errorf("%d probes sent, want none", got)
	}
	assertBalanced(t, fp)
}

// Trace is TraceEach with a nil callback: the refusals before any probe are
// the same, and the callback is never called for them. Platform-neutral: off
// Linux both return ErrUnsupported, which is still the same error.
func TestTraceEachRefusalsMatchTrace(t *testing.T) {
	tests := []struct {
		name   string
		target netip.Addr
	}{
		{"IPv6", netip.MustParseAddr("2001:db8::1")},
		{"unspecified", netip.MustParseAddr("0.0.0.0")},
		{"multicast", netip.MustParseAddr("224.0.0.1")},
		{"limited broadcast", netip.MustParseAddr("255.255.255.255")},
		{"invalid address", netip.Addr{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, wantErr := Trace(context.Background(), tc.target, Options{})
			if wantErr == nil {
				t.Fatal("Trace accepted the target, the case is wrong")
			}
			calls := 0
			res, err := TraceEach(context.Background(), tc.target, Options{}, func(Hop) { calls++ })
			if !errors.Is(err, wantErr) {
				t.Errorf("TraceEach err = %v, want the one Trace returns: %v", err, wantErr)
			}
			if calls != 0 {
				t.Errorf("callback called %d times for a refused target", calls)
			}
			if len(res.Hops) != 0 || res.Target.IsValid() {
				t.Errorf("Result = %+v, want the zero Result", res)
			}
			if _, err := TraceEach(context.Background(), tc.target, Options{}, nil); !errors.Is(err, wantErr) {
				t.Errorf("TraceEach with nil callback err = %v, want %v", err, wantErr)
			}
		})
	}
}

// Options is passed by value and compared by callers: it must stay comparable
// (no func, slice or map field).
func TestOptionsStaysComparable(t *testing.T) {
	a := Options{MaxHops: 5, ProbeTimeout: time.Second, Deadline: 2 * time.Second}
	b := a
	if a != b {
		t.Error("equal Options compare unequal")
	}
	b.MaxHops = 6
	if a == b {
		t.Error("different Options compare equal")
	}
	_ = map[Options]struct{}{a: {}} // does not compile if Options is not comparable
}
