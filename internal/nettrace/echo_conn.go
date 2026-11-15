package nettrace

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"time"
)

// minEchoTimeout is the smallest timeout handed to an echo call: a zero or
// negative timeout is not a valid IcmpSendEcho argument.
const minEchoTimeout = time.Millisecond

// errEchoConnClosed is returned by await on a connection already closed.
var errEchoConnClosed = errors.New("echo connection closed")

// echoAPI is the ICMP echo service behind echoProber (Windows IcmpSendEcho).
// It is an interface so the engine's contract is tested on every platform.
type echoAPI interface {
	// create returns a new echo handle.
	create() (uintptr, error)
	// echo sends one echo request with the given TTL and blocks until it is
	// answered or timeout passes (errSilent). It cannot be interrupted.
	echo(h uintptr, dst netip.Addr, ttl int, timeout time.Duration) (reply, error)
	closeHandle(h uintptr) error
}

// echoProber probes with one echo handle per TTL.
type echoProber struct {
	api echoAPI
	// preflight runs before any handle is created: it tells a missing route
	// apart from a silent path. A no-route error becomes ErrNoRoute.
	preflight func(ctx context.Context, dst netip.Addr) error
}

func (p *echoProber) open(ctx context.Context, dst netip.Addr, ttl int) (probeConn, error) {
	if err := p.preflight(ctx, dst); err != nil {
		return nil, connectError(err)
	}
	h, err := p.api.create()
	if err != nil {
		return nil, err
	}
	return &echoConn{api: p.api, handle: h, dst: dst, ttl: ttl}, nil
}

// echoConn is single-use: one send, then one await. The echo call cannot be
// interrupted, so await runs it in a goroutine and abandons it on
// cancellation; the handle is then closed by that goroutine, never under it.
type echoConn struct {
	api    echoAPI
	handle uintptr
	dst    netip.Addr
	ttl    int

	mu       sync.Mutex
	inflight bool // an echo call is running on handle
	closed   bool // close was called
}

type echoResult struct {
	rep reply
	err error
}

func (c *echoConn) send() (time.Time, error) { return time.Now(), nil }

// await waits for the echo until the given time. It returns errSilent on
// timeout and ctx.Err() when ctx ended first.
func (c *echoConn) await(ctx context.Context, until time.Time) (reply, error) {
	if err := ctx.Err(); err != nil {
		return reply{}, err
	}
	timeout := max(time.Until(until), minEchoTimeout)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return reply{}, errEchoConnClosed
	}
	c.inflight = true
	c.mu.Unlock()
	done := make(chan echoResult, 1)
	go func() {
		rep, err := c.api.echo(c.handle, c.dst, c.ttl, timeout)
		c.echoReturned()
		done <- echoResult{rep, err}
	}()

	select {
	case <-ctx.Done():
		return reply{}, ctx.Err()
	case r := <-done:
		return c.settle(ctx, until, r)
	}
}

// settle turns an echo outcome into await's answer. When ctx's own deadline is
// not after until, the trace's deadline (not the probe timeout) is what ran
// out: let ctx report it, as the Linux connection does.
func (c *echoConn) settle(ctx context.Context, until time.Time, r echoResult) (reply, error) {
	if !errors.Is(r.err, errSilent) {
		return r.rep, r.err
	}
	if dl, ok := ctx.Deadline(); ok && !dl.After(until) {
		<-ctx.Done()
		return reply{}, ctx.Err()
	}
	return reply{}, errSilent
}

// echoReturned records that the echo call is over and closes the handle when
// close was asked for meanwhile.
func (c *echoConn) echoReturned() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight = false
	if c.closed {
		_ = c.api.closeHandle(c.handle)
	}
}

// close releases the handle, once. While an echo is running the goroutine
// running it does so when it returns.
func (c *echoConn) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.inflight {
		return nil
	}
	return c.api.closeHandle(c.handle)
}
