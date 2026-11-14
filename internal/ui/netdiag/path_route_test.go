package netdiag

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
)

// defaultRouteHop is the fake engine's default route answer.
func defaultRouteHop() netcheck.RouteHop {
	return netcheck.RouteHop{
		Interface: "eth0",
		Source:    net.ParseIP("192.168.1.21"),
		Gateway:   net.ParseIP("192.168.1.1"),
	}
}

// captureLog redirects the standard logger for the test and returns the buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// msgKinds names the message types in order: R resolved, H hop, D done.
func msgKinds(t *testing.T, msgs []tea.Msg) string {
	t.Helper()
	var b strings.Builder
	for _, m := range msgs {
		switch m.(type) {
		case pathResolvedMsg:
			b.WriteByte('R')
		case pathHopMsg:
			b.WriteByte('H')
		case pathDoneMsg:
			b.WriteByte('D')
		default:
			t.Fatalf("unexpected message type %T", m)
		}
	}
	return b.String()
}

func oneHopTrace(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
	onHop(hopAt(1))
	return nettrace.Result{Target: a, Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{hopAt(1)}}, nil
}

func TestProducePathAsksTheRouteOfTheTracedLiteral(t *testing.T) {
	f := &fakeEngine{traceFn: oneHopTrace}
	msgs := runProducer(t, context.Background(), 4, pathTarget{Host: "1.1.1.1"}, time.Second, f.engine())

	if f.routeCalls != 1 || f.routeAddr != netip.MustParseAddr("1.1.1.1") {
		t.Fatalf("route calls=%d addr=%v, want one call for 1.1.1.1", f.routeCalls, f.routeAddr)
	}
	if got := msgKinds(t, msgs); got != "RHD" {
		t.Fatalf("message order = %q, want RHD (no extra message for the route)", got)
	}
	res := msgs[0].(pathResolvedMsg)
	want := defaultRouteHop()
	if res.routeErr != nil || res.route.Interface != want.Interface ||
		!res.route.Source.Equal(want.Source) || !res.route.Gateway.Equal(want.Gateway) {
		t.Fatalf("resolved = %#v, want the fake's RouteHop and no routeErr", res)
	}
	if res.gen != 4 || res.addr != netip.MustParseAddr("1.1.1.1") {
		t.Fatalf("resolved gen/addr = %d/%v, want 4/1.1.1.1", res.gen, res.addr)
	}
}

func TestProducePathRouteIsAskedForTheTracedAddressNotAnother(t *testing.T) {
	first := netip.MustParseAddr("93.184.216.34")
	f := &fakeEngine{resolveFn: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{first, netip.MustParseAddr("1.2.3.4")}, nil
	}}
	runProducer(t, context.Background(), 1, pathTarget{Host: "example.com"}, time.Second, f.engine())

	if f.routeCalls != 1 || f.routeAddr != first {
		t.Fatalf("route calls=%d addr=%v, want one call for the first address %v", f.routeCalls, f.routeAddr, first)
	}
	if f.routeAddr != f.traceAddr {
		t.Fatalf("route asked for %v but %v was traced", f.routeAddr, f.traceAddr)
	}
}

func TestProducePathRouteResultRidesTheResolvedMessageAndCapIsUnchanged(t *testing.T) {
	custom := netcheck.RouteHop{Interface: "wg0", Source: net.ParseIP("10.2.0.2")}
	f := &fakeEngine{
		routeFn: func(context.Context, netip.Addr) (netcheck.RouteHop, error) { return custom, nil },
		traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
			for ttl := 1; ttl <= nettrace.MaxHopsLimit; ttl++ { // the most a real trace sends
				onHop(hopAt(ttl))
			}
			return nettrace.Result{Target: a, Outcome: nettrace.OutcomeMaxHops}, nil
		},
	}
	msgs := runProducer(t, context.Background(), 1, pathTarget{Host: "10.9.9.9"}, time.Second, f.engine())

	if pathChanCap != 32 {
		t.Fatalf("pathChanCap = %d, want 32 (the route adds no message)", pathChanCap)
	}
	if len(msgs) > pathChanCap {
		t.Fatalf("got %d messages, more than pathChanCap %d", len(msgs), pathChanCap)
	}
	if _, ok := msgs[len(msgs)-1].(pathDoneMsg); !ok {
		t.Fatalf("last = %#v, want the done message to survive a full channel", msgs[len(msgs)-1])
	}
	for _, m := range msgs {
		if r, ok := m.(pathResolvedMsg); ok {
			if r.route.Interface != "wg0" {
				t.Fatalf("resolved route = %#v, want wg0", r.route)
			}
			return
		}
	}
	t.Fatal("the resolved message was dropped")
}

func TestProducePathRouteErrorDoesNotStopTheTrace(t *testing.T) {
	raw := errors.New("no source IP found for 1.1.1.1")
	f := &fakeEngine{
		routeFn: func(context.Context, netip.Addr) (netcheck.RouteHop, error) { return netcheck.RouteHop{}, raw },
		traceFn: oneHopTrace,
	}
	logs := captureLog(t)
	msgs := runProducer(t, context.Background(), 1, pathTarget{Host: "1.1.1.1"}, time.Second, f.engine())

	if f.traceCalls != 1 {
		t.Fatalf("trace calls = %d, want 1: a route error is not fatal", f.traceCalls)
	}
	if got := msgKinds(t, msgs); got != "RHD" {
		t.Fatalf("message order = %q, want RHD", got)
	}
	if res := msgs[0].(pathResolvedMsg); !errors.Is(res.routeErr, raw) {
		t.Fatalf("resolved.routeErr = %v, want the route error", res.routeErr)
	}
	if d := msgs[2].(pathDoneMsg); d.err != nil || d.result.Outcome != nettrace.OutcomeReached {
		t.Fatalf("done = %#v, want a clean done: the route error must not fail the run", d)
	}
	if want := "ERROR [netdiag/path] route 1.1.1.1: " + raw.Error(); !strings.Contains(logs.String(), want) {
		t.Fatalf("log = %q, want it to contain %q", logs.String(), want)
	}
}

func TestProducePathResolveFailureNeverAsksTheRoute(t *testing.T) {
	f := &fakeEngine{resolveFn: func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("no such host")
	}}
	msgs := runProducer(t, context.Background(), 1, pathTarget{Host: "nope.invalid"}, time.Second, f.engine())

	if f.routeCalls != 0 || f.traceCalls != 0 {
		t.Fatalf("route calls=%d trace calls=%d, want none for an unresolved target", f.routeCalls, f.traceCalls)
	}
	if got := msgKinds(t, msgs); got != "D" {
		t.Fatalf("message order = %q, want D only", got)
	}
}

func TestProducePathCancelDuringTheRouteLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	f := &fakeEngine{
		routeFn: func(ctx context.Context, _ netip.Addr) (netcheck.RouteHop, error) {
			close(started)
			<-ctx.Done() // the lookup stops on the context only
			return netcheck.RouteHop{}, ctx.Err()
		},
		traceFn: func(ctx context.Context, a netip.Addr, _ nettrace.Options, _ func(nettrace.Hop)) (nettrace.Result, error) {
			return nettrace.Result{Target: a, Outcome: nettrace.OutcomeCancelled}, nil
		},
	}
	logs := captureLog(t)
	out := make(chan tea.Msg, pathChanCap)
	done := make(chan struct{})
	go func() {
		defer close(done)
		producePath(ctx, 1, pathTarget{Host: "10.0.0.7"}, hostSource(), time.Second, f.engine(), out)
	}()

	select {
	case <-started:
	case <-time.After(producerWait):
		t.Fatal("route lookup never started")
	}
	cancel() // nobody drains out
	select {
	case <-done:
	case <-time.After(producerWait):
		t.Fatal("producer still alive after cancel during the route lookup")
	}
	var msgs []tea.Msg
	for m := range out {
		msgs = append(msgs, m)
	}
	if len(msgs) == 0 {
		t.Fatal("no message at all after a canceled route lookup")
	}
	d, ok := msgs[len(msgs)-1].(pathDoneMsg)
	if !ok || d.result.Outcome != nettrace.OutcomeCancelled {
		t.Fatalf("last = %#v, want a done with OutcomeCancelled", msgs[len(msgs)-1])
	}
	if f.traceCalls != 1 || f.traceCtxErr == nil {
		t.Fatalf("trace calls=%d ctxErr=%v, want the trace to see the canceled context", f.traceCalls, f.traceCtxErr)
	}
	if strings.Contains(logs.String(), "route 10.0.0.7") {
		t.Fatalf("log = %q: a canceled lookup is not an error to log", logs.String())
	}
}
