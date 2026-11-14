package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/netiface"
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
	routeCalls   int
	routeAddr    netip.Addr

	// Container source seam (DEVDESK-330): counters and recorded arguments.
	engineNameVal string // "" means "docker"
	osName        string // "" means a Linux engine
	osErr         error
	osCtx         context.Context
	psRows        []docker.Container
	psErr         error
	osCalls       int
	psCalls       int
	inspectCalls  int
	inspectIDs    []string
	inspectNet    docker.ContainerNetworking
	inspectErr    error
	inspectCtx    context.Context

	// Bridge seam (DEVDESK-331): the network read and this machine's interfaces.
	networkCalls int
	networkIDs   []string
	networkVal   docker.NetworkBridge
	networkErr   error
	networkCtx   context.Context
	ifacesCalls  int
	ifacesVal    []netiface.Interface
	ifacesErr    error

	routeFn   func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error)
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
		route: func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error) {
			f.routeCalls++
			f.routeAddr = a
			if f.routeFn == nil {
				return defaultRouteHop(), nil
			}
			return f.routeFn(ctx, a)
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
		engineName: func() string {
			if f.engineNameVal == "" {
				return "docker"
			}
			return f.engineNameVal
		},
		engineOS: func(ctx context.Context) (string, error) {
			f.osCalls++
			f.osCtx = ctx
			if f.osName == "" && f.osErr == nil {
				return "Debian GNU/Linux 13 (trixie)", nil
			}
			return f.osName, f.osErr
		},
		running: func(context.Context) ([]docker.Container, error) {
			f.psCalls++
			return f.psRows, f.psErr
		},
		inspect: func(ctx context.Context, id string) (docker.ContainerNetworking, error) {
			f.inspectCalls++
			f.inspectIDs = append(f.inspectIDs, id)
			f.inspectCtx = ctx
			return f.inspectNet, f.inspectErr
		},
		network: func(ctx context.Context, id string) (docker.NetworkBridge, error) {
			f.networkCalls++
			f.networkIDs = append(f.networkIDs, id)
			f.networkCtx = ctx
			return f.networkVal, f.networkErr
		},
		interfaces: func(context.Context) ([]netiface.Interface, error) {
			f.ifacesCalls++
			return f.ifacesVal, f.ifacesErr
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
	return runProducerFrom(t, ctx, gen, tg, hostSource(), rt, e)
}

// runProducerFrom is runProducer for a given source.
func runProducerFrom(t *testing.T, ctx context.Context, gen int, tg pathTarget, src pathSource, rt time.Duration, e pathEngine) []tea.Msg {
	t.Helper()
	out := make(chan tea.Msg, pathChanCap)
	done := make(chan struct{})
	go func() {
		defer close(done)
		producePath(ctx, gen, tg, src, rt, e, out)
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
		producePath(ctx, 1, pathTarget{Host: "10.0.0.6"}, hostSource(), time.Second, f.engine(), out)
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

const testCtrID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testCtrSource() pathSource {
	return pathSource{ID: testCtrID, Name: "web-1", Image: "nginx"}
}

func testCtrNetworking() docker.ContainerNetworking {
	return docker.ContainerNetworking{
		Name: "web-1", Running: true, Mode: "bridge",
		Networks: []docker.EndpointNetwork{{
			Name:    "app_net",
			IPv4:    netip.MustParseAddr("172.18.0.5"),
			Gateway: netip.MustParseAddr("172.18.0.1"),
		}},
	}
}

func TestProducePathContainerSource(t *testing.T) {
	rawErr := errors.New("inspect boom: permission denied")
	tests := []struct {
		name       string
		src        pathSource
		inspectErr error
		wantInsp   int
		wantNet    bool
		wantLogged bool
	}{
		{"host source never reads a container", hostSource(), nil, 0, false, false},
		{"container source reads its network", testCtrSource(), nil, 1, true, false},
		{"inspect failure does not stop the trace", testCtrSource(), rawErr, 1, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeEngine{traceFn: oneHopTrace, inspectNet: testCtrNetworking(), inspectErr: tc.inspectErr}
			if tc.inspectErr != nil {
				f.inspectNet = docker.ContainerNetworking{}
			}
			logs := captureLog(t)
			msgs := runProducerFrom(t, context.Background(), 9, pathTarget{Host: "1.1.1.1"}, tc.src, time.Second, f.engine())

			if got := msgKinds(t, msgs); got != "RHD" {
				t.Fatalf("message order = %q, want RHD", got)
			}
			if f.inspectCalls != tc.wantInsp {
				t.Fatalf("inspect calls = %d, want %d", f.inspectCalls, tc.wantInsp)
			}
			if f.traceCalls != 1 {
				t.Fatalf("trace calls = %d, want 1", f.traceCalls)
			}
			res := msgs[0].(pathResolvedMsg)
			if res.source != tc.src {
				t.Fatalf("resolved source = %+v, want %+v", res.source, tc.src)
			}
			if tc.src.isHost() != res.source.isHost() {
				t.Fatal("source.isHost() disagrees with the launch source")
			}
			if tc.wantInsp == 1 {
				if len(f.inspectIDs) != 1 || f.inspectIDs[0] != tc.src.ID {
					t.Fatalf("inspect ids = %v, want [%s]", f.inspectIDs, tc.src.ID)
				}
				if _, ok := f.inspectCtx.Deadline(); !ok {
					t.Fatal("inspect context carries no deadline")
				}
			}
			if tc.wantNet && !reflect.DeepEqual(res.ctr, testCtrNetworking()) {
				t.Fatalf("resolved ctr = %+v, want the fake's networking", res.ctr)
			}
			if !errors.Is(res.ctrErr, tc.inspectErr) {
				t.Fatalf("resolved ctrErr = %v, want %v", res.ctrErr, tc.inspectErr)
			}
			if got := strings.Contains(logs.String(), "inspect boom"); got != tc.wantLogged {
				t.Fatalf("raw error logged = %v, want %v (log %q)", got, tc.wantLogged, logs.String())
			}
			if tc.wantLogged && !strings.Contains(logs.String(), "ERROR [netdiag/path] inspect "+tc.src.ID) {
				t.Fatalf("log %q lacks the ERROR [netdiag/path] inspect <id> prefix", logs.String())
			}
		})
	}
}

func TestProducePathContainerResolveErrorReadsNothing(t *testing.T) {
	f := &fakeEngine{resolveFn: func(context.Context, string) ([]netip.Addr, error) { return nil, errors.New("nxdomain") }}
	msgs := runProducerFrom(t, context.Background(), 1, pathTarget{Host: "nope.invalid"}, testCtrSource(), time.Second, f.engine())

	if f.routeCalls != 0 || f.inspectCalls != 0 || f.traceCalls != 0 {
		t.Fatalf("route=%d inspect=%d trace=%d, want none after a resolution error", f.routeCalls, f.inspectCalls, f.traceCalls)
	}
	if got := msgKinds(t, msgs); got != "D" {
		t.Fatalf("message order = %q, want D", got)
	}
}

func TestProducePathInspectCanceledIsNotLogged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeEngine{traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, _ func(nettrace.Hop)) (nettrace.Result, error) {
		return nettrace.Result{Target: a, Outcome: nettrace.OutcomeCancelled}, nil
	}}
	e := f.engine()
	e.inspect = func(ctx context.Context, _ string) (docker.ContainerNetworking, error) {
		cancel() // esc during the read
		return docker.ContainerNetworking{}, fmt.Errorf("inspect canceled sentinel: %w", ctx.Err())
	}
	logs := captureLog(t)
	msgs := runProducerFrom(t, ctx, 1, pathTarget{Host: "10.0.0.8"}, testCtrSource(), time.Second, e)

	if strings.Contains(logs.String(), "sentinel") {
		t.Fatalf("a canceled inspect was logged: %q", logs.String())
	}
	if _, ok := msgs[len(msgs)-1].(pathDoneMsg); !ok {
		t.Fatalf("last = %#v, want a done message", msgs[len(msgs)-1])
	}
}

func TestProducePathContainerFullTraceKeepsDoneLast(t *testing.T) {
	f := &fakeEngine{inspectNet: testCtrNetworking(), traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		for i := 0; i < pathChanCap+5; i++ {
			onHop(hopAt(i + 1))
		}
		return nettrace.Result{Target: a}, nil
	}}
	msgs := runProducerFrom(t, context.Background(), 1, pathTarget{Host: "10.9.9.9"}, testCtrSource(), time.Second, f.engine())

	if _, ok := msgs[len(msgs)-1].(pathDoneMsg); !ok {
		t.Fatalf("last = %#v, want the done message even with the channel full", msgs[len(msgs)-1])
	}
	if pathChanCap != 32 {
		t.Fatalf("pathChanCap = %d, want 32 (unchanged)", pathChanCap)
	}
}

func TestListPathSources(t *testing.T) {
	running := []docker.Container{
		{ID: "bbbbbbbbbbbb", Name: "web-1", Image: "nginx", State: "running"},
		{ID: "aaaaaaaaaaaa", Name: "api", Image: "app", State: "running"},
	}
	tests := []struct {
		name      string
		fake      fakeEngine
		wantNote  string
		wantNames []string
		wantOS    int
		wantPS    int
	}{
		{"podman short-circuits before any exec", fakeEngine{engineNameVal: "podman", psRows: running}, notePodman, nil, 0, 0},
		{"engine absent", fakeEngine{osErr: errors.New("exec: docker: not found")}, noteNoEngine, nil, 1, 0},
		{"docker desktop refused after info", fakeEngine{osName: "Docker Desktop", psRows: running}, noteDesktop, nil, 1, 0},
		{"running containers feed the list", fakeEngine{psRows: running}, "", []string{"api", "web-1"}, 1, 1},
		{"no running container", fakeEngine{}, noteNoContainer, nil, 1, 1},
		{"ps failure", fakeEngine{psErr: errors.New("daemon down")}, noteNoEngine, nil, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.fake
			logs := captureLog(t)
			got := listPathSources(context.Background(), f.engine())

			if got.Note != tc.wantNote {
				t.Fatalf("note = %q, want %q", got.Note, tc.wantNote)
			}
			var names []string
			for _, c := range got.Containers {
				names = append(names, c.Name)
			}
			if !reflect.DeepEqual(names, tc.wantNames) {
				t.Fatalf("containers = %v, want %v", names, tc.wantNames)
			}
			if f.osCalls != tc.wantOS || f.psCalls != tc.wantPS {
				t.Fatalf("engineOS calls = %d, running calls = %d, want %d and %d", f.osCalls, f.psCalls, tc.wantOS, tc.wantPS)
			}
			for _, raw := range []string{"not found", "daemon down"} {
				if strings.Contains(got.Note, raw) {
					t.Fatalf("note %q leaks the raw error", got.Note)
				}
			}
			if (tc.fake.osErr != nil || tc.fake.psErr != nil) && !strings.Contains(logs.String(), "ERROR [netdiag/path] sources:") {
				t.Fatalf("raw error not logged: %q", logs.String())
			}
		})
	}
}
