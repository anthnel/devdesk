package netdiag

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/nettrace"
)

// pathChanCap holds a whole trace: every hop (at most MaxHopsLimit), the
// resolved message and the done message. The producer therefore never waits
// for a reader.
const pathChanCap = nettrace.MaxHopsLimit + 2

// pathEngine is the seam between the path tab and the network: tests replace
// it with a fake.
type pathEngine struct {
	supported func() error
	resolve   func(ctx context.Context, host string) ([]netip.Addr, error)
	trace     func(ctx context.Context, a netip.Addr, o nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error)
}

// systemPathEngine returns the engine backed by the real resolver and sockets.
func systemPathEngine() pathEngine {
	return pathEngine{
		supported: nettrace.Supported,
		resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		},
		trace: nettrace.TraceEach,
	}
}

// pathResolvedMsg reports the address the target resolved to.
type pathResolvedMsg struct {
	gen  int
	addr netip.Addr
}

// pathHopMsg carries one hop as the engine reports it.
type pathHopMsg struct {
	gen int
	hop nettrace.Hop
}

// pathDoneMsg ends a run: a result, or an error when no trace happened.
type pathDoneMsg struct {
	gen    int
	result nettrace.Result
	err    error
}

// producePath resolves the target, traces it and sends the messages to out,
// then closes out. It runs inside a Cmd and touches no model (Rule 110).
// out must have room for pathChanCap messages: sends never block, so a view
// that stops reading after canceling leaks nothing.
func producePath(ctx context.Context, gen int, tg pathTarget, resolveTimeout time.Duration, e pathEngine, out chan tea.Msg) {
	defer close(out)
	send := func(m tea.Msg) {
		select {
		case out <- m:
		default:
		}
	}
	// sendDone must never be lost: when the channel is full the oldest message
	// is dropped to make room. The producer is the only sender, so one drop
	// frees a slot for good.
	sendDone := func(m tea.Msg) {
		for range 2 {
			select {
			case out <- m:
				return
			default:
			}
			select {
			case <-out:
			default:
			}
		}
	}

	addr, err := resolvePathTarget(ctx, tg.Host, resolveTimeout, e)
	if err != nil {
		if ctx.Err() == nil { // esc during the resolution is not an error
			log.Printf("ERROR [netdiag/path] resolve %s: %v", tg.Host, err)
		}
		sendDone(pathDoneMsg{gen: gen, err: err})
		return
	}
	send(pathResolvedMsg{gen: gen, addr: addr})

	res, err := e.trace(ctx, addr, nettrace.Options{}, func(h nettrace.Hop) {
		send(pathHopMsg{gen: gen, hop: h})
	})
	if err != nil {
		log.Printf("ERROR [netdiag/path] trace %s: %v", tg.Host, err)
	}
	sendDone(pathDoneMsg{gen: gen, result: res, err: err})
}

// resolvePathTarget returns the IPv4 address to trace: a literal is used as
// is, a name is resolved under its own timeout.
func resolvePathTarget(ctx context.Context, host string, timeout time.Duration, e pathEngine) (netip.Addr, error) {
	if a, err := netip.ParseAddr(host); err == nil && a.Is4() {
		return a, nil
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addrs, err := e.resolve(rctx, host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%w: %w", errPathResolve, err)
	}
	if len(addrs) == 0 {
		return netip.Addr{}, errPathNoIPv4
	}
	return addrs[0], nil
}

// waitForPathMsg returns a Cmd that yields the next message of in, or nil
// once in is closed. The view re-arms it after every message.
func waitForPathMsg(in <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m, ok := <-in
		if !ok {
			return nil
		}
		return m
	}
}
