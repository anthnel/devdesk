package nettrace

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeEchoAPI is an echoAPI whose echo either answers at once (answer set) or
// blocks until release is closed, like IcmpSendEcho does.
type fakeEchoAPI struct {
	mu        sync.Mutex
	creates   int
	closes    int
	inflight  int
	maxFlight int
	busy      map[uintptr]bool
	dsts      []netip.Addr
	ttls      []int
	timeouts  []time.Duration
	violation string

	createErr error
	// answer, when not nil, is returned at once; otherwise echo blocks.
	answer  func(ttl int) (reply, error)
	started chan struct{} // one value per echo call, buffered
	release chan struct{} // closing it frees every blocked echo
}

func newFakeEchoAPI() *fakeEchoAPI {
	return &fakeEchoAPI{
		busy:    map[uintptr]bool{},
		started: make(chan struct{}, 64),
		release: make(chan struct{}),
	}
}

func (f *fakeEchoAPI) create() (uintptr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.creates++
	return uintptr(f.creates), nil
}

func (f *fakeEchoAPI) echo(h uintptr, dst netip.Addr, ttl int, timeout time.Duration) (reply, error) {
	f.mu.Lock()
	f.busy[h] = true
	f.inflight++
	if f.inflight > f.maxFlight {
		f.maxFlight = f.inflight
	}
	f.dsts = append(f.dsts, dst)
	f.ttls = append(f.ttls, ttl)
	f.timeouts = append(f.timeouts, timeout)
	answer := f.answer
	f.mu.Unlock()
	f.started <- struct{}{}

	defer func() {
		f.mu.Lock()
		f.busy[h] = false
		f.inflight--
		f.mu.Unlock()
	}()
	if answer != nil {
		return answer(ttl)
	}
	<-f.release
	return reply{}, errSilent
}

func (f *fakeEchoAPI) closeHandle(h uintptr) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy[h] {
		f.violation = "closeHandle called while echo was still running"
	}
	f.closes++
	return nil
}

func (f *fakeEchoAPI) counts() (creates, closes, inflight int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates, f.closes, f.inflight
}

func (f *fakeEchoAPI) waitStarted(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-f.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("echo call %d of %d never started", i+1, n)
		}
	}
}

// waitFor polls cond up to 2 s; it only bounds a wait, it never replaces a sync.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func noPreflight(context.Context, netip.Addr) error { return nil }

func newEchoProber(api echoAPI) *echoProber {
	return &echoProber{api: api, preflight: noPreflight}
}

func mustOpen(t *testing.T, p *echoProber, ttl int) probeConn {
	t.Helper()
	c, err := p.open(context.Background(), engineTarget, ttl)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return c
}

func TestEchoProberImplementsProber(t *testing.T) {
	var _ prober = (*echoProber)(nil)
}

func TestEchoProberOpenPassesTTLAndDestination(t *testing.T) {
	api := newFakeEchoAPI()
	api.answer = func(int) (reply, error) { return echoReply(ipSuccess, engineTarget) }
	c := mustOpen(t, newEchoProber(api), 7)
	sentAt, err := c.send()
	if err != nil || sentAt.IsZero() {
		t.Fatalf("send = %v, %v", sentAt, err)
	}
	if _, err := c.await(context.Background(), sentAt.Add(time.Second)); err != nil {
		t.Fatalf("await: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.ttls) != 1 || api.ttls[0] != 7 {
		t.Errorf("ttls = %v, want [7]", api.ttls)
	}
	if len(api.dsts) != 1 || api.dsts[0] != engineTarget {
		t.Errorf("dsts = %v, want [%v]", api.dsts, engineTarget)
	}
}

func TestEchoProberPassesTheRemainingTimeAsTimeout(t *testing.T) {
	tests := []struct {
		name     string
		until    time.Duration // relative to the send
		min, max time.Duration
	}{
		{"remaining time", 500 * time.Millisecond, 400 * time.Millisecond, 500 * time.Millisecond},
		{"deadline already past is one millisecond", -time.Second, time.Millisecond, time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeEchoAPI()
			api.answer = func(int) (reply, error) { return reply{}, errSilent }
			c := mustOpen(t, newEchoProber(api), 3)
			sentAt, _ := c.send()
			if _, err := c.await(context.Background(), sentAt.Add(tt.until)); !errors.Is(err, errSilent) {
				t.Fatalf("await err = %v, want errSilent", err)
			}
			api.mu.Lock()
			got := append([]time.Duration(nil), api.timeouts...)
			api.mu.Unlock()
			if len(got) != 1 || got[0] < tt.min || got[0] > tt.max {
				t.Errorf("timeout = %v, want in [%v, %v]", got, tt.min, tt.max)
			}
		})
	}
}

func TestEchoConnAwaitReturnsTheReply(t *testing.T) {
	api := newFakeEchoAPI()
	api.answer = func(int) (reply, error) { return echoReply(ipTTLExpiredTransit, netip.MustParseAddr("10.0.0.1")) }
	c := mustOpen(t, newEchoProber(api), 1)
	sentAt, _ := c.send()
	rep, err := c.await(context.Background(), sentAt.Add(time.Second))
	if err != nil {
		t.Fatalf("await: %v", err)
	}
	if rep.Addr != netip.MustParseAddr("10.0.0.1") || rep.Origin != originEcho || rep.Status != ipTTLExpiredTransit {
		t.Errorf("reply = %+v", rep)
	}
	if err := c.close(); err != nil {
		t.Errorf("close: %v", err)
	}
	if _, closes, _ := api.counts(); closes != 1 {
		t.Errorf("closeHandle calls = %d, want 1", closes)
	}
}

func TestEchoConnAwaitPropagatesAFailure(t *testing.T) {
	api := newFakeEchoAPI()
	api.answer = func(int) (reply, error) { return reply{}, &EchoStatusError{Status: ipGeneralFailure} }
	c := mustOpen(t, newEchoProber(api), 1)
	sentAt, _ := c.send()
	_, err := c.await(context.Background(), sentAt.Add(time.Second))
	var se *EchoStatusError
	if !errors.As(err, &se) || se.Status != ipGeneralFailure {
		t.Fatalf("await err = %v, want *EchoStatusError %d", err, ipGeneralFailure)
	}
}

func TestEchoConnCancelDuringBlockedEcho(t *testing.T) {
	api := newFakeEchoAPI()
	c := mustOpen(t, newEchoProber(api), 5)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentAt, _ := c.send()

	done := make(chan error, 1)
	go func() {
		_, err := c.await(ctx, sentAt.Add(10*time.Second))
		done <- err
	}()
	api.waitStarted(t, 1)

	cancelledAt := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("await err = %v, want context.Canceled", err)
		}
		if d := time.Since(cancelledAt); d > 50*time.Millisecond {
			t.Errorf("await took %v to notice the cancellation, want < 50ms", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("await did not return after cancel")
	}

	// The echo is still blocked: the handle must stay open, close or not.
	_ = c.close()
	_ = c.close()
	if _, closes, inflight := api.counts(); closes != 0 || inflight != 1 {
		t.Fatalf("before release: closes = %d, inflight = %d, want 0 and 1", closes, inflight)
	}

	close(api.release)
	waitFor(t, "the deferred closeHandle", func() bool { _, c, _ := api.counts(); return c == 1 })
	waitFor(t, "echo to return", func() bool { _, _, f := api.counts(); return f == 0 })
	_ = c.close()
	if _, closes, _ := api.counts(); closes != 1 {
		t.Errorf("closeHandle calls = %d, want exactly 1", closes)
	}
	if api.violation != "" {
		t.Error(api.violation)
	}
}

func TestEchoConnCloseWithoutAwaitClosesAtOnce(t *testing.T) {
	api := newFakeEchoAPI()
	c := mustOpen(t, newEchoProber(api), 2)
	if err := c.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if creates, closes, _ := api.counts(); creates != 1 || closes != 1 {
		t.Fatalf("creates = %d, closes = %d, want 1 and 1", creates, closes)
	}
	if err := c.close(); err != nil {
		t.Errorf("second close: %v", err)
	}
	if _, closes, _ := api.counts(); closes != 1 {
		t.Errorf("closeHandle calls after two closes = %d, want 1", closes)
	}
}

func TestEchoConnSilentEchoWithTheTraceDeadlineReportsTheContext(t *testing.T) {
	api := newFakeEchoAPI()
	api.answer = func(int) (reply, error) { return reply{}, errSilent }
	c := mustOpen(t, newEchoProber(api), 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	sentAt, _ := c.send()
	// The trace deadline is before the probe's: it is what ran out.
	_, err := c.await(ctx, sentAt.Add(time.Hour))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("await err = %v, want context.DeadlineExceeded", err)
	}
	if ctx.Err() == nil {
		t.Error("await returned before ctx ended")
	}
}

func TestEchoConnSilentEchoBeforeTheTraceDeadlineIsSilent(t *testing.T) {
	api := newFakeEchoAPI()
	api.answer = func(int) (reply, error) { return reply{}, errSilent }
	c := mustOpen(t, newEchoProber(api), 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	sentAt, _ := c.send()
	if _, err := c.await(ctx, sentAt.Add(10*time.Millisecond)); !errors.Is(err, errSilent) {
		t.Fatalf("await err = %v, want errSilent", err)
	}
}

func TestEchoProberPreflightNoRouteSkipsCreate(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"raw network unreachable", syscall.ENETUNREACH},
		{"raw host unreachable", syscall.EHOSTUNREACH},
		{"already ErrNoRoute", ErrNoRoute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := newFakeEchoAPI()
			var gotDst netip.Addr
			p := &echoProber{api: api, preflight: func(_ context.Context, dst netip.Addr) error {
				gotDst = dst
				return tt.err
			}}
			c, err := p.open(context.Background(), engineTarget, 1)
			if err == nil || c != nil {
				t.Fatalf("open = %v, %v, want an error and no conn", c, err)
			}
			if !errors.Is(err, ErrNoRoute) {
				t.Errorf("err = %v, want errors.Is ErrNoRoute", err)
			}
			if !errors.Is(err, tt.err) {
				t.Errorf("err = %v, should keep the cause %v", err, tt.err)
			}
			if gotDst != engineTarget {
				t.Errorf("preflight dst = %v, want %v", gotDst, engineTarget)
			}
			if creates, _, _ := api.counts(); creates != 0 {
				t.Errorf("create calls = %d, want 0", creates)
			}
		})
	}
}

func TestEchoProberPreflightOtherErrorIsReturnedAsIs(t *testing.T) {
	api := newFakeEchoAPI()
	boom := errors.New("boom")
	p := &echoProber{api: api, preflight: func(context.Context, netip.Addr) error { return boom }}
	_, err := p.open(context.Background(), engineTarget, 1)
	if !errors.Is(err, boom) || errors.Is(err, ErrNoRoute) {
		t.Fatalf("err = %v, want boom and not ErrNoRoute", err)
	}
	if creates, _, _ := api.counts(); creates != 0 {
		t.Errorf("create calls = %d, want 0", creates)
	}
}

func TestEchoProberCreateFailureIsReturned(t *testing.T) {
	api := newFakeEchoAPI()
	api.createErr = errors.New("no handle")
	_, err := newEchoProber(api).open(context.Background(), engineTarget, 1)
	if !errors.Is(err, api.createErr) {
		t.Fatalf("err = %v, want the create error", err)
	}
}

func TestEchoProberRunReachesTheTarget(t *testing.T) {
	api := newFakeEchoAPI()
	api.answer = func(ttl int) (reply, error) {
		switch ttl {
		case 1:
			return echoReply(ipTTLExpiredTransit, netip.MustParseAddr("10.0.0.1"))
		case 2:
			return echoReply(ipSuccess, engineTarget)
		}
		return reply{}, errSilent
	}
	res := mustRun(t, context.Background(), Options{MaxHops: 6, ProbeTimeout: time.Second}, newEchoProber(api))
	if res.Outcome != OutcomeReached || len(res.Hops) != 2 {
		t.Fatalf("Outcome = %v, hops = %+v, want reached with 2 hops", res.Outcome, res.Hops)
	}
	if h := res.Hops[0]; h.Kind != HopRouter || h.Addr != netip.MustParseAddr("10.0.0.1") || h.ICMPType != icmpTimeExceeded || h.TTL != 1 {
		t.Errorf("Hops[0] = %+v", h)
	}
	if h := res.Hops[1]; h.Kind != HopTarget || h.Addr != engineTarget || h.TTL != 2 {
		t.Errorf("Hops[1] = %+v", h)
	}
	waitFor(t, "every handle closed", func() bool { c, cl, _ := api.counts(); return c == 6 && cl == 6 })
}

func TestEchoProberRunAllSilentOpensAndClosesEveryTTL(t *testing.T) {
	api := newFakeEchoAPI()
	api.answer = func(int) (reply, error) { return reply{}, errSilent }
	res := mustRun(t, context.Background(), Options{MaxHops: 30, ProbeTimeout: time.Second}, newEchoProber(api))
	if res.Outcome != OutcomeMaxHops || len(res.Hops) != 30 {
		t.Fatalf("Outcome = %v, hops = %d, want max-hops with 30", res.Outcome, len(res.Hops))
	}
	waitFor(t, "30 creates and 30 closes, no echo left", func() bool {
		c, cl, f := api.counts()
		return c == 30 && cl == 30 && f == 0
	})
	if api.violation != "" {
		t.Error(api.violation)
	}
}

func TestEchoProberRunCancelledLeavesNoGoroutine(t *testing.T) {
	api := newFakeEchoAPI()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for i := 0; i < 30; i++ { // every TTL is blocked inside echo
			select {
			case <-api.started:
			case <-time.After(2 * time.Second):
				return // the main goroutine's checks fail on the counts
			}
		}
		cancel()
	}()

	res := mustRun(t, ctx, Options{MaxHops: 30, ProbeTimeout: 10 * time.Second}, newEchoProber(api))
	if res.Outcome != OutcomeCancelled {
		t.Fatalf("Outcome = %v, want cancelled", res.Outcome)
	}
	// run has returned while the 30 echoes are still blocked: no handle may be
	// closed under them, and none may be leaked afterwards.
	if creates, closes, inflight := api.counts(); creates != 30 || closes != 0 || inflight != 30 {
		t.Fatalf("at return: creates = %d, closes = %d, inflight = %d, want 30, 0, 30", creates, closes, inflight)
	}
	close(api.release)
	waitFor(t, "30 closes and no echo left", func() bool {
		_, cl, f := api.counts()
		return cl == 30 && f == 0
	})
	if api.violation != "" {
		t.Error(api.violation)
	}
}

func TestEchoConnAwaitAfterCloseFailsWithoutEcho(t *testing.T) {
	api := newFakeEchoAPI()
	api.answer = func(int) (reply, error) { return echoReply(ipSuccess, engineTarget) }
	c := mustOpen(t, newEchoProber(api), 1)
	if err := c.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := c.await(context.Background(), time.Now().Add(time.Second)); err == nil {
		t.Fatal("await after close returned no error")
	}
	_ = c.close()
	api.mu.Lock()
	echoes := len(api.ttls)
	api.mu.Unlock()
	if echoes != 0 {
		t.Errorf("echo calls = %d, want 0", echoes)
	}
	if _, closes, _ := api.counts(); closes != 1 {
		t.Errorf("closeHandle calls = %d, want 1", closes)
	}
}
