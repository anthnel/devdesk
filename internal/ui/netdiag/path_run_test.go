package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/nettrace"
)

const producerWait = 2 * time.Second

// fakeEngine records its calls; no socket, no resolver.
type fakeEngine struct {
	resolveCalls int
	resolveHost  string
	resolveCtx   context.Context
	traceCalls   int
	traceAddr    netip.Addr
	traceOpts    nettrace.Options
	traceCtxErr  error

	resolveFn func(ctx context.Context, host string) ([]netip.Addr, error)
	traceFn   func(ctx context.Context, a netip.Addr, o nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error)
}

func (f *fakeEngine) engine() pathEngine {
	return pathEngine{
		supported: func() error { return nil },
		resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			f.resolveCalls++
			f.resolveHost = host
			f.resolveCtx = ctx
			if f.resolveFn == nil {
				return nil, errors.New("unexpected resolve")
			}
			return f.resolveFn(ctx, host)
		},
		trace: func(ctx context.Context, a netip.Addr, o nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
			f.traceCalls++
			f.traceAddr = a
			f.traceOpts = o
			f.traceCtxErr = ctx.Err()
			if f.traceFn == nil {
				return nettrace.Result{Target: a}, nil
			}
			return f.traceFn(ctx, a, o, onHop)
		},
	}
}

func hopAt(ttl int) nettrace.Hop {
	return nettrace.Hop{TTL: ttl, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr("10.0.0.1")}
}

// runProducer calls producePath synchronously with nobody reading the
// channel, failing if it does not return (the channel capacity must hold).
func runProducer(t *testing.T, ctx context.Context, gen int, tg pathTarget, rt time.Duration, e pathEngine) []tea.Msg {
	t.Helper()
	out := make(chan tea.Msg, pathChanCap)
	done := make(chan struct{})
	go func() {
		defer close(done)
		producePath(ctx, gen, tg, rt, e, out)
	}()
	select {
	case <-done:
	case <-time.After(producerWait):
		t.Fatal("producePath did not return: the producer blocked")
	}
	var msgs []tea.Msg
	for {
		select {
		case m, ok := <-out:
			if !ok {
				return msgs
			}
			msgs = append(msgs, m)
		default:
			t.Fatal("channel not closed after producePath returned")
			return nil
		}
	}
}

func TestPathChanCapHoldsAWholeTrace(t *testing.T) {
	if want := nettrace.MaxHopsLimit + 2; pathChanCap != want {
		t.Fatalf("pathChanCap = %d, want %d", pathChanCap, want)
	}
	if pathChanCap != 32 {
		t.Fatalf("pathChanCap = %d, want 32", pathChanCap)
	}
}

func TestProducePathLiteralIPv4SkipsResolve(t *testing.T) {
	f := &fakeEngine{traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		onHop(hopAt(1))
		onHop(hopAt(2))
		return nettrace.Result{Target: a, Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{hopAt(1), hopAt(2)}}, nil
	}}
	msgs := runProducer(t, context.Background(), 7, pathTarget{Host: "1.1.1.1"}, time.Second, f.engine())

	if f.resolveCalls != 0 {
		t.Fatalf("resolve called %d times for a literal IPv4", f.resolveCalls)
	}
	if f.traceOpts != (nettrace.Options{}) {
		t.Fatalf("trace options = %+v, want the zero Options", f.traceOpts)
	}
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4 (resolved, 2 hops, done): %#v", len(msgs), msgs)
	}
	res, ok := msgs[0].(pathResolvedMsg)
	if !ok || res.addr != netip.MustParseAddr("1.1.1.1") || res.gen != 7 {
		t.Fatalf("msgs[0] = %#v, want pathResolvedMsg{7, 1.1.1.1}", msgs[0])
	}
	for i, ttl := range []int{1, 2} {
		h, ok := msgs[1+i].(pathHopMsg)
		if !ok || h.hop.TTL != ttl || h.gen != 7 {
			t.Fatalf("msgs[%d] = %#v, want pathHopMsg ttl %d gen 7", 1+i, msgs[1+i], ttl)
		}
	}
	d, ok := msgs[3].(pathDoneMsg)
	if !ok || d.err != nil || d.gen != 7 || d.result.Outcome != nettrace.OutcomeReached || len(d.result.Hops) != 2 {
		t.Fatalf("msgs[3] = %#v, want a clean pathDoneMsg", msgs[3])
	}
}

func TestProducePathNameResolvesFirstAddress(t *testing.T) {
	first := netip.MustParseAddr("93.184.216.34")
	f := &fakeEngine{resolveFn: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{first, netip.MustParseAddr("1.2.3.4")}, nil
	}}
	msgs := runProducer(t, context.Background(), 1, pathTarget{Host: "example.com", Port: 443}, time.Second, f.engine())

	if f.resolveCalls != 1 || f.resolveHost != "example.com" {
		t.Fatalf("resolve calls=%d host=%q, want 1 call for the bare host", f.resolveCalls, f.resolveHost)
	}
	if f.traceAddr != first {
		t.Fatalf("traced %v, want the first address %v", f.traceAddr, first)
	}
	res, ok := msgs[0].(pathResolvedMsg)
	if !ok || res.addr != first {
		t.Fatalf("msgs[0] = %#v, want pathResolvedMsg{%v}", msgs[0], first)
	}
}

func TestProducePathResolveIsBoundedByTheTimeout(t *testing.T) {
	const timeout = 40 * time.Millisecond
	f := &fakeEngine{resolveFn: func(ctx context.Context, _ string) ([]netip.Addr, error) {
		<-ctx.Done() // blocks until the producer's own deadline fires
		return nil, ctx.Err()
	}}
	start := time.Now()
	msgs := runProducer(t, context.Background(), 1, pathTarget{Host: "slow.example"}, timeout, f.engine())

	dl, ok := f.resolveCtx.Deadline()
	if !ok {
		t.Fatal("resolve context carries no deadline")
	}
	if dl.Sub(start) > timeout+producerWait/2 {
		t.Fatalf("resolve deadline %v after start, want about %v", dl.Sub(start), timeout)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want exactly one done: %#v", len(msgs), msgs)
	}
	d, ok := msgs[0].(pathDoneMsg)
	if !ok || !errors.Is(d.err, errPathResolve) {
		t.Fatalf("msgs[0] = %#v, want pathDoneMsg wrapping errPathResolve", msgs[0])
	}
}

func TestProducePathResolveFailureSendsNoHop(t *testing.T) {
	raw := errors.New("lookup nope.invalid: no such host")
	f := &fakeEngine{resolveFn: func(context.Context, string) ([]netip.Addr, error) { return nil, raw }}
	msgs := runProducer(t, context.Background(), 3, pathTarget{Host: "nope.invalid"}, time.Second, f.engine())

	if f.traceCalls != 0 {
		t.Fatal("trace ran after a resolution error: a probe would have been sent")
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want exactly one: %#v", len(msgs), msgs)
	}
	d, ok := msgs[0].(pathDoneMsg)
	if !ok || d.gen != 3 {
		t.Fatalf("msgs[0] = %#v, want pathDoneMsg gen 3", msgs[0])
	}
	if !errors.Is(d.err, errPathResolve) || !errors.Is(d.err, raw) {
		t.Fatalf("err = %v, want errPathResolve wrapping the resolver error", d.err)
	}
	if got, want := pathErrorLabel("nope.invalid", d.err), "Could not resolve nope.invalid — no probe was sent"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestProducePathNoAddressIsNoIPv4(t *testing.T) {
	f := &fakeEngine{resolveFn: func(context.Context, string) ([]netip.Addr, error) { return nil, nil }}
	msgs := runProducer(t, context.Background(), 1, pathTarget{Host: "v6only.example"}, time.Second, f.engine())

	if f.traceCalls != 0 || len(msgs) != 1 {
		t.Fatalf("traceCalls=%d msgs=%#v, want no trace and one done", f.traceCalls, msgs)
	}
	d, ok := msgs[0].(pathDoneMsg)
	if !ok || !errors.Is(d.err, errPathNoIPv4) {
		t.Fatalf("msgs[0] = %#v, want pathDoneMsg wrapping errPathNoIPv4", msgs[0])
	}
}

func TestProducePathTraceErrorIsCarriedByDone(t *testing.T) {
	wrapped := fmt.Errorf("%w: %w", nettrace.ErrNoRoute, errors.New("network is unreachable"))
	f := &fakeEngine{traceFn: func(context.Context, netip.Addr, nettrace.Options, func(nettrace.Hop)) (nettrace.Result, error) {
		return nettrace.Result{}, wrapped
	}}
	msgs := runProducer(t, context.Background(), 2, pathTarget{Host: "10.1.2.3"}, time.Second, f.engine())

	for _, m := range msgs {
		if _, isHop := m.(pathHopMsg); isHop {
			t.Fatalf("a hop was sent for a trace that failed to open: %#v", msgs)
		}
	}
	d, ok := msgs[len(msgs)-1].(pathDoneMsg)
	if !ok || !errors.Is(d.err, nettrace.ErrNoRoute) {
		t.Fatalf("last = %#v, want pathDoneMsg wrapping ErrNoRoute", msgs[len(msgs)-1])
	}
}

func TestProducePathFullTraceNeedsNoReader(t *testing.T) {
	f := &fakeEngine{traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		hops := make([]nettrace.Hop, 0, nettrace.MaxHopsLimit)
		for ttl := 1; ttl <= nettrace.MaxHopsLimit; ttl++ {
			onHop(hopAt(ttl))
			hops = append(hops, hopAt(ttl))
		}
		return nettrace.Result{Target: a, Outcome: nettrace.OutcomeMaxHops, Hops: hops}, nil
	}}
	// runProducer fails the test if the producer blocks on a full channel.
	msgs := runProducer(t, context.Background(), 1, pathTarget{Host: "10.9.9.9"}, time.Second, f.engine())

	if want := nettrace.MaxHopsLimit + 2; len(msgs) != want {
		t.Fatalf("got %d messages, want %d", len(msgs), want)
	}
}

func TestProducePathCanceledBeforeTheCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeEngine{traceFn: func(ctx context.Context, a netip.Addr, _ nettrace.Options, _ func(nettrace.Hop)) (nettrace.Result, error) {
		return nettrace.Result{Target: a, Outcome: nettrace.OutcomeCancelled}, nil
	}}
	msgs := runProducer(t, ctx, 1, pathTarget{Host: "10.0.0.5"}, time.Second, f.engine())

	if f.traceCalls != 1 || f.traceCtxErr == nil {
		t.Fatalf("traceCalls=%d ctxErr=%v, want the trace to see the canceled context", f.traceCalls, f.traceCtxErr)
	}
	d, ok := msgs[len(msgs)-1].(pathDoneMsg)
	if !ok || d.result.Outcome != nettrace.OutcomeCancelled {
		t.Fatalf("last = %#v, want a done with OutcomeCancelled", msgs[len(msgs)-1])
	}
}

func TestProducePathCancelMidTraceReturnsWithoutAReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	f := &fakeEngine{traceFn: func(ctx context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		onHop(hopAt(1))
		close(started)
		<-ctx.Done() // the engine stops on the context only
		return nettrace.Result{Target: a, Outcome: nettrace.OutcomeCancelled}, nil
	}}
	out := make(chan tea.Msg, pathChanCap)
	done := make(chan struct{})
	go func() {
		defer close(done)
		producePath(ctx, 1, pathTarget{Host: "10.0.0.6"}, time.Second, f.engine(), out)
	}()

	select {
	case <-started:
	case <-time.After(producerWait):
		t.Fatal("trace never started")
	}
	cancel() // the view cancels, then stops reading: nobody drains out
	select {
	case <-done:
	case <-time.After(producerWait):
		t.Fatal("producer goroutine still alive after cancel: leaked or blocked")
	}
	var last tea.Msg
	for m := range out { // closed by the producer, so this terminates
		last = m
	}
	if d, ok := last.(pathDoneMsg); !ok || d.result.Outcome != nettrace.OutcomeCancelled {
		t.Fatalf("last = %#v, want a done with OutcomeCancelled", last)
	}
}

func TestProducePathEveryMessageCarriesTheGeneration(t *testing.T) {
	f := &fakeEngine{
		resolveFn: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("9.9.9.9")}, nil
		},
		traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
			onHop(hopAt(1))
			return nettrace.Result{Target: a}, nil
		},
	}
	const gen = 42
	msgs := runProducer(t, context.Background(), gen, pathTarget{Host: "dns.example"}, time.Second, f.engine())

	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %#v", len(msgs), msgs)
	}
	for i, m := range msgs {
		var got int
		switch v := m.(type) {
		case pathResolvedMsg:
			got = v.gen
		case pathHopMsg:
			got = v.gen
		case pathDoneMsg:
			got = v.gen
		default:
			t.Fatalf("msgs[%d] has unexpected type %T", i, m)
		}
		if got != gen {
			t.Fatalf("msgs[%d].gen = %d, want %d", i, got, gen)
		}
	}
}

func TestWaitForPathMsgReturnsMessagesThenNilOnClose(t *testing.T) {
	ch := make(chan tea.Msg, 4)
	ch <- pathResolvedMsg{gen: 1, addr: netip.MustParseAddr("1.1.1.1")}
	ch <- pathHopMsg{gen: 1, hop: hopAt(1)}
	close(ch)

	cmd := waitForPathMsg(ch)
	if cmd == nil {
		t.Fatal("waitForPathMsg returned a nil Cmd")
	}
	if _, ok := cmd().(pathResolvedMsg); !ok {
		t.Fatal("first wait should yield the resolved message")
	}
	// A fresh Cmd (the re-arm) yields the next one.
	if _, ok := waitForPathMsg(ch)().(pathHopMsg); !ok {
		t.Fatal("re-armed wait should yield the hop message")
	}
	if got := waitForPathMsg(ch)(); got != nil {
		t.Fatalf("wait on a closed channel = %#v, want nil", got)
	}
}

func TestWaitForPathMsgBlocksUntilAMessageArrives(t *testing.T) {
	ch := make(chan tea.Msg, 1)
	got := make(chan tea.Msg, 1)
	go func() { got <- waitForPathMsg(ch)() }()

	select {
	case m := <-got:
		t.Fatalf("wait returned %#v before anything was sent", m)
	case <-time.After(20 * time.Millisecond):
	}
	ch <- pathDoneMsg{gen: 5}
	select {
	case m := <-got:
		if d, ok := m.(pathDoneMsg); !ok || d.gen != 5 {
			t.Fatalf("got %#v, want pathDoneMsg gen 5", m)
		}
	case <-time.After(producerWait):
		t.Fatal("wait did not return after a message was sent")
	}
}
