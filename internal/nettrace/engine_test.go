package nettrace

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The engine tests run on every platform: they drive run() with a fake prober
// and real (short) time, never a socket.

var engineTarget = netip.MustParseAddr("192.0.2.99")

// probeSpec scripts what one TTL's connection does.
type probeSpec struct {
	openErr   error         // open fails with this error
	openBlock bool          // open blocks until ctx is done, then returns ctx.Err()
	sendErr   error         // send fails with this error
	sendDelay time.Duration // send takes this long
	delay     time.Duration // the answer arrives this long after the send
	rep       *reply        // the answer; with awaitErr nil too, the probe is silent
	awaitErr  error         // await fails with this error after delay
	// ignoreUntil models a probe that nothing but the trace's own deadline or
	// a cancellation can end: await does not honor its until argument.
	ignoreUntil bool
}

type fakeProber struct {
	spec func(ttl int) probeSpec
	n    int // MaxHops of the run: every open must succeed before any send

	attempts, openOK, closes, sends, awaits atomic.Int32

	onOpen, onSend, onAwait func(ttl int)
	// onReply runs just before await hands an answer back.
	onReply func(ttl int)

	// barrier: await blocks until barrierN probes are waiting at once.
	barrierN        int
	barrierCh       chan struct{}
	waiting         atomic.Int32
	barrierTimeouts atomic.Int32

	mu         sync.Mutex
	violations []string
}

func newFake(n int, spec func(ttl int) probeSpec) *fakeProber {
	return &fakeProber{spec: spec, n: n}
}

func (p *fakeProber) violate(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.violations = append(p.violations, fmt.Sprintf(format, args...))
}

func (p *fakeProber) withBarrier(n int) *fakeProber {
	p.barrierN = n
	p.barrierCh = make(chan struct{})
	return p
}

func (p *fakeProber) open(ctx context.Context, dst netip.Addr, ttl int) (probeConn, error) {
	p.attempts.Add(1)
	if dst != engineTarget {
		p.violate("open for %v, want %v", dst, engineTarget)
	}
	if p.onOpen != nil {
		p.onOpen(ttl)
	}
	s := p.spec(ttl)
	if s.openBlock {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if s.openErr != nil {
		return nil, s.openErr
	}
	p.openOK.Add(1)
	return &fakeConn{p: p, ttl: ttl, spec: s}, nil
}

type fakeConn struct {
	p      *fakeProber
	ttl    int
	spec   probeSpec
	sentAt time.Time
	closed atomic.Bool
}

func (c *fakeConn) send() (time.Time, error) {
	p := c.p
	p.sends.Add(1)
	if got := int(p.openOK.Load()); got != p.n {
		p.violate("TTL %d sent with only %d/%d connections open", c.ttl, got, p.n)
	}
	if p.onSend != nil {
		p.onSend(c.ttl)
	}
	if c.spec.sendDelay > 0 {
		time.Sleep(c.spec.sendDelay)
	}
	if c.spec.sendErr != nil {
		return time.Time{}, c.spec.sendErr
	}
	c.sentAt = time.Now()
	return c.sentAt, nil
}

func (c *fakeConn) await(ctx context.Context, until time.Time) (reply, error) {
	p := c.p
	p.awaits.Add(1)
	if p.onAwait != nil {
		p.onAwait(c.ttl)
	}
	if p.barrierCh != nil {
		if int(p.waiting.Add(1)) == p.barrierN {
			close(p.barrierCh)
		}
		select {
		case <-p.barrierCh:
		case <-ctx.Done():
			return reply{}, ctx.Err()
		case <-time.After(2 * time.Second):
			p.barrierTimeouts.Add(1)
			return reply{}, errors.New("fake: barrier timeout, probes are not in flight together")
		}
	}
	if err := ctx.Err(); err != nil {
		return reply{}, err
	}
	s := c.spec
	answers := s.rep != nil || s.awaitErr != nil
	replyAt := c.sentAt.Add(s.delay)
	var fire time.Time
	silent := false
	switch {
	case !answers && s.ignoreUntil:
		<-ctx.Done()
		return reply{}, ctx.Err()
	case !answers || (!s.ignoreUntil && replyAt.After(until)):
		fire, silent = until, true
	default:
		fire = replyAt
	}
	t := time.NewTimer(time.Until(fire))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return reply{}, ctx.Err()
	case <-t.C:
	}
	if silent {
		if err := ctx.Err(); err != nil {
			return reply{}, err
		}
		return reply{}, errSilent
	}
	if s.awaitErr != nil {
		return reply{}, s.awaitErr
	}
	if p.onReply != nil {
		p.onReply(c.ttl)
	}
	return *s.rep, nil
}

func (c *fakeConn) close() error {
	if c.closed.Swap(true) {
		c.p.violate("TTL %d closed twice", c.ttl)
	}
	c.p.closes.Add(1)
	return nil
}

// assertBalanced checks the invariants every run must keep, whatever happened.
func assertBalanced(t *testing.T, p *fakeProber) {
	t.Helper()
	if o, c := p.openOK.Load(), p.closes.Load(); o != c {
		t.Errorf("opened %d connections but closed %d", o, c)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, v := range p.violations {
		t.Error(v)
	}
}

func routerReply(addr string) *reply {
	return &reply{Addr: netip.MustParseAddr(addr), ICMPType: icmpTimeExceeded, Origin: originICMP}
}

func targetReply() *reply {
	return &reply{Addr: engineTarget, ICMPType: icmpDestUnreachable, ICMPCode: icmpCodePortUnreach, Origin: originICMP}
}

func unreachReply(addr string, code uint8) *reply {
	return &reply{Addr: netip.MustParseAddr(addr), ICMPType: icmpDestUnreachable, ICMPCode: code, Origin: originICMP}
}

// table builds a spec function from per-TTL specs, with a default for the rest.
func table(def probeSpec, m map[int]probeSpec) func(int) probeSpec {
	return func(ttl int) probeSpec {
		if s, ok := m[ttl]; ok {
			return s
		}
		return def
	}
}

type runOut struct {
	res Result
	err error
}

// runBG starts run and returns a channel for its result.
func runBG(ctx context.Context, opts Options, p prober) <-chan runOut {
	ch := make(chan runOut, 1)
	go func() {
		res, err := run(ctx, engineTarget, opts, p)
		ch <- runOut{res, err}
	}()
	return ch
}

func waitRun(t *testing.T, ch <-chan runOut) runOut {
	t.Helper()
	select {
	case out := <-ch:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return within 5s")
		return runOut{}
	}
}

func mustRun(t *testing.T, ctx context.Context, opts Options, p prober) Result {
	t.Helper()
	res, err := run(ctx, engineTarget, opts, p)
	if err != nil {
		t.Fatalf("run: unexpected error %v", err)
	}
	if res.Target != engineTarget {
		t.Errorf("Result.Target = %v, want %v", res.Target, engineTarget)
	}
	return res
}

// Criterion 1: hops in order, address, RTT, list cut at the target.
func TestRunReachesTargetAndCutsTheList(t *testing.T) {
	silentForever := probeSpec{ignoreUntil: true}
	fp := newFake(8, table(silentForever, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 10 * time.Millisecond},
		2: {rep: routerReply("10.0.0.2"), delay: 20 * time.Millisecond},
		3: {rep: routerReply("10.0.0.3"), delay: 30 * time.Millisecond},
		4: {rep: targetReply(), delay: 40 * time.Millisecond},
		// TTL 5..8 never answer: they must be canceled, not waited for.
	}))
	start := time.Now()
	res := mustRun(t, context.Background(), Options{MaxHops: 8, ProbeTimeout: 5 * time.Second}, fp)
	elapsed := time.Since(start)

	if res.Outcome != OutcomeReached {
		t.Fatalf("Outcome = %v, want reached", res.Outcome)
	}
	if len(res.Hops) != 4 {
		t.Fatalf("len(Hops) = %d, want 4 (cut at the target): %+v", len(res.Hops), res.Hops)
	}
	want := []struct {
		kind  HopKind
		addr  string
		delay time.Duration
	}{
		{HopRouter, "10.0.0.1", 10 * time.Millisecond},
		{HopRouter, "10.0.0.2", 20 * time.Millisecond},
		{HopRouter, "10.0.0.3", 30 * time.Millisecond},
		{HopTarget, engineTarget.String(), 40 * time.Millisecond},
	}
	for i, w := range want {
		h := res.Hops[i]
		if h.TTL != i+1 {
			t.Errorf("Hops[%d].TTL = %d, want %d", i, h.TTL, i+1)
		}
		if h.Kind != w.kind {
			t.Errorf("Hops[%d].Kind = %v, want %v", i, h.Kind, w.kind)
		}
		if h.Addr != netip.MustParseAddr(w.addr) {
			t.Errorf("Hops[%d].Addr = %v, want %s", i, h.Addr, w.addr)
		}
		if h.RTT < w.delay || h.RTT > w.delay+200*time.Millisecond {
			t.Errorf("Hops[%d].RTT = %v, want within [%v, %v]", i, h.RTT, w.delay, w.delay+200*time.Millisecond)
		}
	}
	if elapsed > time.Second {
		t.Errorf("run took %v: the probes above the target were waited for", elapsed)
	}
	if res.Duration <= 0 || res.Duration > elapsed+50*time.Millisecond {
		t.Errorf("Duration = %v, want in (0, %v]", res.Duration, elapsed)
	}
	if got := fp.openOK.Load(); got != 8 {
		t.Errorf("opened %d connections, want 8 (one per TTL)", got)
	}
	assertBalanced(t, fp)
}

// An RTT the prober already filled in is kept as is.
func TestRunKeepsAnRTTTheProberFilledIn(t *testing.T) {
	r := targetReply()
	r.RTT = 7 * time.Millisecond
	fp := newFake(2, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: r, delay: 20 * time.Millisecond},
	}))
	res := mustRun(t, context.Background(), Options{MaxHops: 2, ProbeTimeout: time.Second}, fp)
	if len(res.Hops) != 1 || res.Hops[0].RTT != 7*time.Millisecond {
		t.Fatalf("Hops = %+v, want one hop with RTT 7ms", res.Hops)
	}
	assertBalanced(t, fp)
}

// Criterion 2: a silent hop does not stop the trace.
func TestRunSilentHopThenTarget(t *testing.T) {
	fp := newFake(6, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 5 * time.Millisecond},
		2: {rep: routerReply("10.0.0.2"), delay: 10 * time.Millisecond},
		3: {}, // silent: await returns errSilent at sendTime+ProbeTimeout
		4: {rep: targetReply(), delay: 20 * time.Millisecond},
	}))
	start := time.Now()
	res := mustRun(t, context.Background(), Options{MaxHops: 6, ProbeTimeout: 200 * time.Millisecond}, fp)
	elapsed := time.Since(start)

	if res.Outcome != OutcomeReached {
		t.Fatalf("Outcome = %v, want reached", res.Outcome)
	}
	if len(res.Hops) != 4 {
		t.Fatalf("len(Hops) = %d, want 4: %+v", len(res.Hops), res.Hops)
	}
	if res.Hops[2].Kind != HopSilent || res.Hops[2].TTL != 3 {
		t.Errorf("Hops[2] = %+v, want a silent hop at TTL 3", res.Hops[2])
	}
	if res.Hops[3].Kind != HopTarget {
		t.Errorf("Hops[3].Kind = %v, want target", res.Hops[3].Kind)
	}
	if elapsed < 190*time.Millisecond {
		t.Errorf("run took %v: it did not wait for the silent hop's timeout", elapsed)
	}
	assertBalanced(t, fp)
}

// Criterion 3: nothing answers, MaxHops runs out after about one ProbeTimeout.
func TestRunAllSilentEndsAtMaxHops(t *testing.T) {
	fp := newFake(5, table(probeSpec{}, nil))
	const timeout = 150 * time.Millisecond
	start := time.Now()
	res := mustRun(t, context.Background(), Options{MaxHops: 5, ProbeTimeout: timeout}, fp)
	elapsed := time.Since(start)

	if res.Outcome != OutcomeMaxHops {
		t.Fatalf("Outcome = %v, want max hops", res.Outcome)
	}
	if len(res.Hops) != 5 {
		t.Fatalf("len(Hops) = %d, want 5", len(res.Hops))
	}
	for i, h := range res.Hops {
		if h.Kind != HopSilent || h.TTL != i+1 {
			t.Errorf("Hops[%d] = %+v, want silent at TTL %d", i, h, i+1)
		}
	}
	if elapsed < timeout-10*time.Millisecond || elapsed > 700*time.Millisecond {
		t.Errorf("run took %v, want about %v (probes in parallel)", elapsed, timeout)
	}
	assertBalanced(t, fp)
}

// Criterion 3: the overall deadline wins over a long probe timeout.
func TestRunDeadlineLeavesUnresolvedHops(t *testing.T) {
	fp := newFake(3, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 10 * time.Millisecond},
	}))
	start := time.Now()
	res := mustRun(t, context.Background(), Options{MaxHops: 3, ProbeTimeout: 2 * time.Second, Deadline: 200 * time.Millisecond}, fp)
	elapsed := time.Since(start)

	if res.Outcome != OutcomeDeadline {
		t.Fatalf("Outcome = %v, want deadline", res.Outcome)
	}
	if elapsed < 180*time.Millisecond || elapsed > time.Second {
		t.Errorf("run took %v, want about 200ms (under 1s)", elapsed)
	}
	if len(res.Hops) != 3 {
		t.Fatalf("len(Hops) = %d, want 3: %+v", len(res.Hops), res.Hops)
	}
	if res.Hops[0].Kind != HopRouter {
		t.Errorf("Hops[0].Kind = %v, want router (answered before the deadline)", res.Hops[0].Kind)
	}
	for _, i := range []int{1, 2} {
		if res.Hops[i].Kind != HopUnresolved || res.Hops[i].TTL != i+1 {
			t.Errorf("Hops[%d] = %+v, want unresolved at TTL %d", i, res.Hops[i], i+1)
		}
	}
	assertBalanced(t, fp)
}

// The caller's own context deadline is a Deadline outcome, not a cancel.
func TestRunCallerDeadlineIsDeadlineOutcome(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	fp := newFake(3, table(probeSpec{ignoreUntil: true}, nil))
	start := time.Now()
	res := mustRun(t, ctx, Options{MaxHops: 3}, fp)
	if res.Outcome != OutcomeDeadline {
		t.Errorf("Outcome = %v, want deadline", res.Outcome)
	}
	if e := time.Since(start); e > 500*time.Millisecond {
		t.Errorf("run took %v, want about 100ms", e)
	}
	assertBalanced(t, fp)
}

// Criterion 4: cancellation while a probe is in await, with partial hops kept.
func TestRunCancelDuringAwait(t *testing.T) {
	fp := newFake(4, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 10 * time.Millisecond},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fp.onReply = func(int) { cancel() } // hop 1 is answered, then the trace is canceled

	out := waitRun(t, runBG(ctx, Options{MaxHops: 4, ProbeTimeout: 10 * time.Second}, fp))
	if out.err != nil {
		t.Fatalf("run: unexpected error %v", out.err)
	}
	if out.res.Outcome != OutcomeCancelled {
		t.Errorf("Outcome = %v, want canceled", out.res.Outcome)
	}
	if len(out.res.Hops) < 1 || out.res.Hops[0].Kind != HopRouter || out.res.Hops[0].Addr != netip.MustParseAddr("10.0.0.1") {
		t.Errorf("Hops = %+v, want the partial hop 1 (router 10.0.0.1) kept", out.res.Hops)
	}
	assertBalanced(t, fp)
}

// A canceled run keeps one entry per TTL, index TTL-1, the unanswered ones
// HopUnresolved: here TTL 1 and 3 answered while TTL 2 and 4 were waiting.
func TestRunCancelKeepsTheResultShape(t *testing.T) {
	fp := newFake(4, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 5 * time.Millisecond},
		3: {rep: routerReply("10.0.0.3"), delay: 10 * time.Millisecond},
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var replies atomic.Int32
	fp.onReply = func(int) {
		if replies.Add(1) == 2 {
			cancel()
		}
	}
	out := waitRun(t, runBG(ctx, Options{MaxHops: 4, ProbeTimeout: 10 * time.Second}, fp))
	if out.err != nil || out.res.Outcome != OutcomeCancelled {
		t.Fatalf("got (%v, %v), want (canceled, nil)", out.res.Outcome, out.err)
	}
	if len(out.res.Hops) != 4 {
		t.Fatalf("len(Hops) = %d, want 4: %+v", len(out.res.Hops), out.res.Hops)
	}
	want := []HopKind{HopRouter, HopUnresolved, HopRouter, HopUnresolved}
	for i, h := range out.res.Hops {
		if h.TTL != i+1 || h.Kind != want[i] {
			t.Errorf("Hops[%d] = %+v, want TTL %d kind %v", i, h, i+1, want[i])
		}
	}
	assertBalanced(t, fp)
}

// Criterion 4: cancellation while open is still blocked on a TTL.
func TestRunCancelDuringOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fp := newFake(5, table(probeSpec{}, map[int]probeSpec{3: {openBlock: true}}))
	fp.onOpen = func(ttl int) {
		if ttl == 3 {
			time.AfterFunc(20*time.Millisecond, cancel)
		}
	}
	out := waitRun(t, runBG(ctx, Options{MaxHops: 5, ProbeTimeout: 5 * time.Second}, fp))
	if out.err != nil {
		t.Fatalf("run: unexpected error %v", out.err)
	}
	if out.res.Outcome != OutcomeCancelled {
		t.Errorf("Outcome = %v, want canceled", out.res.Outcome)
	}
	if got := fp.sends.Load(); got != 0 {
		t.Errorf("%d probes were sent although open never finished", got)
	}
	if len(out.res.Hops) != 5 {
		t.Fatalf("len(Hops) = %d, want 5 unresolved hops", len(out.res.Hops))
	}
	for i, h := range out.res.Hops {
		if h.TTL != i+1 || h.Kind != HopUnresolved {
			t.Errorf("Hops[%d] = %+v, want unresolved at TTL %d", i, h, i+1)
		}
	}
	if got := fp.openOK.Load(); got != 2 {
		t.Errorf("%d connections opened, want 2 (TTL 1 and 2)", got)
	}
	assertBalanced(t, fp)
}

// Criterion 4: cancellation while a send is in progress.
func TestRunCancelDuringSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fp := newFake(4, table(probeSpec{sendDelay: 40 * time.Millisecond, ignoreUntil: true}, nil))
	var once sync.Once
	fp.onSend = func(int) { once.Do(cancel) }

	out := waitRun(t, runBG(ctx, Options{MaxHops: 4, ProbeTimeout: 10 * time.Second}, fp))
	if out.err != nil {
		t.Fatalf("run: unexpected error %v", out.err)
	}
	if out.res.Outcome != OutcomeCancelled {
		t.Errorf("Outcome = %v, want canceled", out.res.Outcome)
	}
	assertBalanced(t, fp)
}

// Criterion 4: no goroutine survives a canceled run, over many cycles of every
// phase, and every opened connection is closed.
func TestRunCancelLeavesNoGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		var fp *fakeProber
		switch i % 3 {
		case 0: // cancel while probes await
			fp = newFake(4, table(probeSpec{ignoreUntil: true}, nil))
			var once sync.Once
			fp.onAwait = func(int) { once.Do(cancel) }
		case 1: // cancel while open blocks
			fp = newFake(4, table(probeSpec{}, map[int]probeSpec{3: {openBlock: true}}))
			fp.onOpen = func(ttl int) {
				if ttl == 3 {
					cancel()
				}
			}
		default: // cancel while a send is running
			fp = newFake(4, table(probeSpec{sendDelay: 5 * time.Millisecond, ignoreUntil: true}, nil))
			var once sync.Once
			fp.onSend = func(int) { once.Do(cancel) }
		}
		out := waitRun(t, runBG(ctx, Options{MaxHops: 4, ProbeTimeout: 10 * time.Second}, fp))
		cancel()
		if out.err != nil || out.res.Outcome != OutcomeCancelled {
			t.Fatalf("cycle %d: got (%v, %v), want (canceled, nil)", i, out.res.Outcome, out.err)
		}
		if o, c := fp.openOK.Load(), fp.closes.Load(); o != c {
			t.Fatalf("cycle %d: opened %d, closed %d", i, o, c)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+5 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+5 {
		t.Errorf("goroutines: %d before, %d after 100 canceled runs", before, after)
	}
}

// Criterion 6, deterministic: a probe's await cannot return until every probe
// is waiting. A sequential engine never gets there and times out.
func TestRunProbesAreInFlightTogether(t *testing.T) {
	const hops = 6
	fp := newFake(hops, table(probeSpec{}, nil)).withBarrier(hops)
	res := mustRun(t, context.Background(), Options{MaxHops: hops, ProbeTimeout: 100 * time.Millisecond}, fp)

	if got := fp.barrierTimeouts.Load(); got != 0 {
		t.Fatalf("%d probes timed out at the barrier: the %d probes did not run in parallel", got, hops)
	}
	if res.Outcome != OutcomeMaxHops || len(res.Hops) != hops {
		t.Fatalf("got %v with %d hops, want max hops with %d", res.Outcome, len(res.Hops), hops)
	}
	for i, h := range res.Hops {
		if h.Kind != HopSilent {
			t.Errorf("Hops[%d].Kind = %v, want silent", i, h.Kind)
		}
	}
	assertBalanced(t, fp)
}

// Criterion 6, timing: 8 hops answer in 100 ms each, the 9th is silent. In
// parallel the trace lasts one ProbeTimeout, not the sum.
func TestRunSlowHopsDoNotAddUp(t *testing.T) {
	m := map[int]probeSpec{}
	for ttl := 1; ttl <= 8; ttl++ {
		m[ttl] = probeSpec{rep: routerReply(fmt.Sprintf("10.0.0.%d", ttl)), delay: 100 * time.Millisecond}
	}
	fp := newFake(9, table(probeSpec{}, m))
	start := time.Now()
	res := mustRun(t, context.Background(), Options{MaxHops: 9, ProbeTimeout: time.Second}, fp)
	elapsed := time.Since(start)

	if res.Outcome != OutcomeMaxHops || len(res.Hops) != 9 {
		t.Fatalf("got %v with %d hops, want max hops with 9", res.Outcome, len(res.Hops))
	}
	if res.Hops[8].Kind != HopSilent {
		t.Errorf("Hops[8].Kind = %v, want silent", res.Hops[8].Kind)
	}
	if elapsed < 950*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Errorf("run took %v, want about 1s (a sequential engine needs about 1.8s)", elapsed)
	}
	assertBalanced(t, fp)
}

// Criterion 7: an open that fails stops everything before any probe is sent.
func TestRunOpenFailure(t *testing.T) {
	routeErr := fmt.Errorf("connect: %w", ErrNoRoute)
	tests := []struct {
		name     string
		failAt   int
		err      error
		wantIs   []error
		wantNot  []error
		wantOpen int32 // connections that did open before the failure
	}{
		{
			name:     "option not supported at TTL 7 becomes ErrUnsupported",
			failAt:   7,
			err:      fmt.Errorf("setsockopt IP_RECVERR: %w", syscall.ENOPROTOOPT),
			wantIs:   []error{ErrUnsupported, syscall.ENOPROTOOPT},
			wantOpen: 6,
		},
		{
			name:     "permission errors are not mapped by the engine",
			failAt:   3,
			err:      fmt.Errorf("connect: %w", syscall.EACCES),
			wantIs:   []error{syscall.EACCES},
			wantNot:  []error{ErrUnsupported},
			wantOpen: 2,
		},
		{
			name:     "any other open error comes back as is",
			failAt:   5,
			err:      routeErr,
			wantIs:   []error{ErrNoRoute},
			wantNot:  []error{ErrUnsupported},
			wantOpen: 4,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fp := newFake(10, table(probeSpec{}, map[int]probeSpec{tc.failAt: {openErr: tc.err}}))
			res, err := run(context.Background(), engineTarget, Options{MaxHops: 10, ProbeTimeout: time.Second}, fp)
			if err == nil {
				t.Fatalf("run returned no error, result %+v", res)
			}
			for _, w := range tc.wantIs {
				if !errors.Is(err, w) {
					t.Errorf("error %v does not wrap %v", err, w)
				}
			}
			for _, w := range tc.wantNot {
				if errors.Is(err, w) {
					t.Errorf("error %v wraps %v, want it untouched", err, w)
				}
			}
			if len(res.Hops) != 0 || res.Duration != 0 || res.Target.IsValid() {
				t.Errorf("Result = %+v, want the zero Result", res)
			}
			if got := fp.sends.Load(); got != 0 {
				t.Errorf("%d probes sent after an open failure, want none", got)
			}
			if got := fp.awaits.Load(); got != 0 {
				t.Errorf("%d awaits after an open failure, want none", got)
			}
			if got := fp.openOK.Load(); got != tc.wantOpen {
				t.Errorf("%d connections opened, want %d before the failure", got, tc.wantOpen)
			}
			assertBalanced(t, fp)
		})
	}
}

// A router's Destination Unreachable ends the trace.
func TestRunUnreachableEndsTheTrace(t *testing.T) {
	fp := newFake(6, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 5 * time.Millisecond},
		2: {rep: unreachReply("10.0.0.2", 1), delay: 15 * time.Millisecond}, // host unreachable
		4: {rep: targetReply(), delay: 5 * time.Millisecond},                // beyond: must not win
	}))
	res := mustRun(t, context.Background(), Options{MaxHops: 6, ProbeTimeout: 5 * time.Second}, fp)

	if res.Outcome != OutcomeUnreachable {
		t.Fatalf("Outcome = %v, want unreachable", res.Outcome)
	}
	if len(res.Hops) != 2 {
		t.Fatalf("len(Hops) = %d, want 2: %+v", len(res.Hops), res.Hops)
	}
	h := res.Hops[1]
	if h.Kind != HopUnreachable || h.Addr != netip.MustParseAddr("10.0.0.2") || h.ICMPType != icmpDestUnreachable || h.ICMPCode != 1 {
		t.Errorf("Hops[1] = %+v, want unreachable from 10.0.0.2 with ICMP type 3 code 1", h)
	}
	assertBalanced(t, fp)
}

// The terminal hop is the smallest TTL, even when a larger one answers first.
func TestRunTerminalIsTheSmallestTTL(t *testing.T) {
	fp := newFake(6, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: routerReply("10.0.0.1"), delay: 5 * time.Millisecond},
		2: {rep: routerReply("10.0.0.2"), delay: 10 * time.Millisecond},
		3: {rep: targetReply(), delay: 80 * time.Millisecond}, // slow
		5: {rep: targetReply(), delay: 5 * time.Millisecond},  // fast but further
	}))
	res := mustRun(t, context.Background(), Options{MaxHops: 6, ProbeTimeout: 5 * time.Second}, fp)

	if res.Outcome != OutcomeReached {
		t.Fatalf("Outcome = %v, want reached", res.Outcome)
	}
	if len(res.Hops) != 3 || res.Hops[2].Kind != HopTarget || res.Hops[2].TTL != 3 {
		t.Fatalf("Hops = %+v, want 3 hops ending at the target on TTL 3", res.Hops)
	}
	assertBalanced(t, fp)
}

// Probe failures become HopError hops and do not stop the trace.
func TestRunProbeErrorsBecomeErrorHops(t *testing.T) {
	sendErr := errors.New("send: network is down")
	recvErr := errors.New("recvmsg: connection refused")
	localOrigin := &reply{Addr: netip.MustParseAddr("10.0.0.3"), Origin: originLocal}
	fp := newFake(5, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {sendErr: sendErr},
		2: {awaitErr: recvErr, delay: 5 * time.Millisecond},
		3: {rep: localOrigin, delay: 5 * time.Millisecond},
		4: {rep: targetReply(), delay: 10 * time.Millisecond},
	}))
	res := mustRun(t, context.Background(), Options{MaxHops: 5, ProbeTimeout: 5 * time.Second}, fp)

	if res.Outcome != OutcomeReached || len(res.Hops) != 4 {
		t.Fatalf("got %v with %d hops, want reached with 4: %+v", res.Outcome, len(res.Hops), res.Hops)
	}
	if h := res.Hops[0]; h.Kind != HopError || !errors.Is(h.Err, sendErr) {
		t.Errorf("Hops[0] = %+v, want HopError wrapping the send error", h)
	}
	if h := res.Hops[1]; h.Kind != HopError || !errors.Is(h.Err, recvErr) {
		t.Errorf("Hops[1] = %+v, want HopError wrapping the await error", h)
	}
	if h := res.Hops[2]; h.Kind != HopError {
		t.Errorf("Hops[2].Kind = %v, want error (reply not from ICMP)", h.Kind)
	}
	if h := res.Hops[3]; h.Kind != HopTarget {
		t.Errorf("Hops[3].Kind = %v, want target", h.Kind)
	}
	assertBalanced(t, fp)
}
