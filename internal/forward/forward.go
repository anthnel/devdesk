// Package forward redirects a local TCP port to a host:port, in this process.
//
// It exists so that a port nothing published can be reached without restarting
// anything — a container's EXPOSEd-but-unpublished port, a service on another
// machine — and it does that with no privilege at all: a net.Listen and two
// io.Copy. That is the whole reason the feature is shaped this way. The
// alternative that was considered and dropped was writing host names into
// /etc/hosts, which needs root on Linux and macOS and a UAC elevation on
// Windows even for an administrator account, and would have walked back §3.43
// and §3.44 — the two entries that removed the last privileged container from
// the application.
//
// Two decisions are load-bearing and are checked by tests:
//
//   - The listener binds loopback only. Binding 0.0.0.0 would put a service the
//     container deliberately kept to itself on the LAN, which is the thing
//     §3.64 exists to notice; doing it here by default would be the
//     application performing it.
//   - A port below 1024 is refused before the syscall. The refusal is
//     structural — there is no unprivileged way around it on Unix — so saying
//     so is more useful than relaying "bind: permission denied", which reads
//     like something a retry might fix.
//
// Nothing here knows about containers or about Docker: a target is a host:port.
// A forward carries no record of where its target came from, because nothing
// ever supplied one — the container pre-fill that would have (§3.1) was not
// built, and a field nothing fills is a column that only ever reads "-".
package forward

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	// loopbackHost is the only address a forward ever binds. See the package
	// doc: this is a security decision, not a default.
	loopbackHost = "127.0.0.1"

	// firstUnprivilegedPort is where the refusal below stops. Unix reserves
	// everything under it for root; Windows has no reserved range, and the
	// limit is applied there too so that a forward set up on one machine is a
	// forward that works on another.
	firstUnprivilegedPort = 1024

	// ProbeTimeout bounds the one dial made before the listener opens, to
	// check the target answers at all.
	ProbeTimeout = 3 * time.Second

	// dialTimeout bounds each per-connection dial once the forward is live.
	// Longer than the probe: the probe is a question asked to fail fast, this
	// one carries a connection someone is waiting on.
	dialTimeout = 10 * time.Second
)

// The failures a caller has to tell apart. Each one leads to a different
// sentence on screen, and matching on a sentinel is what keeps that mapping out
// of the business of reading an error's text.
var (
	// ErrPrivilegedPort is a local port below firstUnprivilegedPort.
	ErrPrivilegedPort = errors.New("a port below 1024 needs privileges DevDesk does not ask for")
	// ErrPortInUse wraps whatever the operating system said about the bind.
	ErrPortInUse = errors.New("the local port is already in use")
	// ErrTargetUnreachable means the probe dial did not connect. On Docker
	// Desktop this is what a container IP looks like from the host: the
	// address is real inside the VM and routes to nothing outside it (D55).
	ErrTargetUnreachable = errors.New("the target did not answer")
	// ErrNoSuchForward is a Close for an id the registry does not hold.
	ErrNoSuchForward = errors.New("no such forward")
	// ErrToggleBusy is a second Toggle for a forward whose first is still
	// dialling. Two of them would race for the same port.
	ErrToggleBusy = errors.New("that forward is already being switched")
)

// State says whether a forward's listener is bound. The registry holds what is
// wanted, and the wanted is not always what is running.
type State int

const (
	// StateLive is a bound listener. It is the zero value, so a Forward built
	// by Open needs to say nothing.
	StateLive State = iota
	// StatePaused is a forward the user stopped without deleting: the listener
	// is closed, the port is free, the entry stays in the file.
	StatePaused
	// StateUnbound is a forward that is wanted and could not be bound — the
	// port was taken or the target silent at the last attempt. LastErr says
	// which. It is retried at the next launch, and by a Toggle.
	StateUnbound
)

// String is the word a table shows.
func (s State) String() string {
	switch s {
	case StatePaused:
		return "paused"
	case StateUnbound:
		return "unbound"
	default:
		return "live"
	}
}

// Forward is one live redirection, as a table reads it. It is a value: List
// hands out copies, so a view can hold one across frames without racing the
// goroutines that keep the real thing running.
type Forward struct {
	// ID identifies the forward across snapshots. It is a counter rather than
	// the local port, which is reused as soon as a forward is closed.
	ID string
	// LocalPort is the port a client connects to. For a named route it is the
	// proxy's, which is what goes in the URL.
	LocalPort int
	// Name is a route's host name (§3.74); empty for a raw TCP forward.
	Name    string
	Target  string
	State   State
	Opened  time.Time
	Active  int
	Total   int64
	LastErr string
}

// URL is what a client opens for a named route, and empty for a TCP forward.
func (f Forward) URL() string {
	if f.Name == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(f.Name, strconv.Itoa(f.LocalPort))
}

// Addr is the address a client connects to.
func (f Forward) Addr() string {
	return net.JoinHostPort(loopbackHost, strconv.Itoa(f.LocalPort))
}

// entry is the live half: the listener, and the counters the connection
// goroutines write to.
type entry struct {
	forward Forward
	// ln is nil unless the forward is live.
	ln net.Listener
	// seq is the order the entries were created in, and the order List keeps.
	// It is a number rather than the ID's text, which sorts "10" before "2".
	seq int
	// switching is set while a Toggle is dialling, outside the lock.
	switching bool
}

// Registry holds the open forwards.
//
// Unlike jobs.Registry, which is deliberately unsynchronised because only
// Update touches it, this one carries a mutex — and the difference is real
// rather than an inconsistency. A forward's accept and connection goroutines
// genuinely write to its counters while Update reads them, so the lock is what
// makes List safe to call from Update. It is never held across I/O.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
	nextID  int

	// proxyPort is the port named routes are served on, set by SetProxyPort.
	// Guarded by mu.
	proxyPort int

	// proxyMu serialises the proxy's lifecycle — start, stop, rebind. Lock
	// order is proxyMu then mu, never the reverse, and the request handler takes
	// mu only, so serving never waits on a bind.
	proxyMu  sync.Mutex
	proxySrv *http.Server
	proxyLn  net.Listener
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{entries: make(map[string]*entry)}
}

// Open probes target, binds localPort on loopback, and starts forwarding.
//
// The probe comes first on purpose. Without it the bind succeeds, the forward
// shows up healthy, and the failure only surfaces when someone points a client
// at it — by which time the message arrives far from the action that caused it.
func (r *Registry) Open(localPort int, target string) (Forward, error) {
	ln, err := r.bindTCP(localPort, target)
	if err != nil {
		return Forward{}, err
	}

	r.mu.Lock()
	e := r.add(localPort, target, ln)
	snapshot := e.forward
	r.mu.Unlock()

	go r.serve(e, ln)
	return snapshot, nil
}

// bindTCP is bind for a TCP forward: the same steps, and a refusal of the port
// the proxy serves on, which a raw forward must not take.
func (r *Registry) bindTCP(localPort int, target string) (net.Listener, error) {
	if r.ProxyPort() == localPort && localPort != 0 {
		return nil, fmt.Errorf("%w: %d is network.proxy_port", ErrProxyPortTaken, localPort)
	}
	return bind(localPort, target)
}

// bind runs the checks and the two I/O steps a live forward needs: the port is
// validated, the target probed, the port bound. It holds no lock and touches no
// registry state, so it can run in parallel.
func bind(localPort int, target string) (net.Listener, error) {
	if localPort < firstUnprivilegedPort {
		return nil, fmt.Errorf("%w: %d", ErrPrivilegedPort, localPort)
	}
	if err := validTarget(target); err != nil {
		return nil, err
	}

	probe, err := net.DialTimeout("tcp", target, ProbeTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrTargetUnreachable, target, err)
	}
	_ = probe.Close()

	ln, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(localPort)))
	if err != nil {
		return nil, fmt.Errorf("%w: %d: %v", ErrPortInUse, localPort, err)
	}
	return ln, nil
}

// add registers an entry. The caller holds the lock; ln is nil for one that is
// not bound.
func (r *Registry) add(localPort int, target string, ln net.Listener) *entry {
	r.nextID++
	e := &entry{
		forward: Forward{
			ID:        strconv.Itoa(r.nextID),
			LocalPort: localPort,
			Target:    target,
			Opened:    time.Now(),
		},
		ln:  ln,
		seq: r.nextID,
	}
	r.entries[e.forward.ID] = e
	return e
}

// Close stops a forward and drops it. Connections it is carrying end with it:
// a redirection the user asked to stop should stop, and draining would leave a
// row gone from the table while its sockets were still live.
func (r *Registry) Close(id string) error {
	r.mu.Lock()
	e, ok := r.entries[id]
	if ok {
		delete(r.entries, id)
	}
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("%w: %s", ErrNoSuchForward, id)
	}
	if e.forward.Name != "" {
		r.releaseProxyIfIdle()
		return nil
	}
	if e.ln == nil {
		return nil
	}
	return e.ln.Close()
}

// CloseAll stops every forward. Nothing calls it on quit — the process exiting
// is what releases the ports, exactly as it is for the MCP server — but a test
// needs it, and so would a future teardown.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	live := make([]*entry, 0, len(r.entries))
	for id, e := range r.entries {
		live = append(live, e)
		delete(r.entries, id)
	}
	r.mu.Unlock()

	for _, e := range live {
		if e.ln != nil {
			_ = e.ln.Close()
		}
	}
	r.proxyMu.Lock()
	r.stopProxyLocked()
	r.proxyMu.Unlock()
}

// List returns the forwards, oldest first. The order is the order they were
// created in, not the port and not the time they were last bound, so a row does
// not move when another forward is added below it or when one is paused and
// resumed.
func (r *Registry) List() []Forward {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Forward, 0, len(r.entries))
	for _, e := range r.sorted() {
		out = append(out, r.view(e))
	}
	return out
}

// sorted returns the entries in creation order. The caller holds the lock.
func (r *Registry) sorted() []*entry {
	out := make([]*entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// Entries is what a Store should keep: the wanted forwards, in creation order,
// live or not. An unbound one is saved as a plain entry, which is what makes it
// retried at the next launch rather than forgotten.
func (r *Registry) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.sorted() {
		saved := Entry{
			Name:   e.forward.Name,
			Target: e.forward.Target,
			Paused: e.forward.State == StatePaused,
		}
		if e.forward.Name == "" {
			saved.LocalPort = e.forward.LocalPort
		}
		out = append(out, saved)
	}
	return out
}

// Restored says what became of the entries Restore was given.
type Restored struct {
	Live, Paused, Unbound int
}

// Total is how many entries there were.
func (r Restored) Total() int { return r.Live + r.Paused + r.Unbound }

// Restore reopens saved entries, and is meant to run once, at startup.
//
// Unlike Open, a failure here keeps the entry. A refusal at creation answers a
// typo and belongs to the form; a refusal at launch answers a service that is
// not up yet, or a port some other process holds today, and deleting the route
// for that would make the file forget things by being started at the wrong
// moment. The row is unbound, says why, and is retried by Toggle.
//
// The binds run in parallel: each one probes its target with a timeout, and a
// list with several silent targets would otherwise take that many timeouts
// before any row appeared.
func (r *Registry) Restore(entries []Entry) Restored {
	type outcome struct {
		ln  net.Listener
		err error
	}
	outcomes := make([]outcome, len(entries))

	var wg sync.WaitGroup
	for i, saved := range entries {
		if saved.Paused {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if saved.Name != "" {
				outcomes[i].err = probeTarget(saved.Target)
				return
			}
			outcomes[i].ln, outcomes[i].err = r.bindTCP(saved.LocalPort, saved.Target)
		}()
	}
	wg.Wait()

	// The proxy is bound once, for every route whose target answered — after the
	// probes, so that a file whose routes are all silent binds nothing.
	proxyErr := r.startProxyForRestore(entries, func(i int) bool { return outcomes[i].err == nil })

	var summary Restored
	type bound struct {
		e  *entry
		ln net.Listener
	}
	var live []bound

	r.mu.Lock()
	for i, saved := range entries {
		e := r.add(saved.LocalPort, saved.Target, outcomes[i].ln)
		e.forward.Name = saved.Name
		err := outcomes[i].err
		if err == nil && saved.Name != "" {
			err = proxyErr
		}
		switch {
		case saved.Paused:
			e.forward.State = StatePaused
			summary.Paused++
		case err != nil:
			e.forward.State = StateUnbound
			e.forward.LastErr = err.Error()
			summary.Unbound++
		default:
			if saved.Name == "" {
				live = append(live, bound{e, outcomes[i].ln})
			}
			summary.Live++
		}
	}
	r.mu.Unlock()

	for _, b := range live {
		go r.serve(b.e, b.ln)
	}
	return summary
}

// startProxyForRestore binds the proxy if any route among entries passed its
// probe, and returns why it could not. It is the one place Restore touches the
// proxy, kept out of Restore's loop because it takes proxyMu.
func (r *Registry) startProxyForRestore(entries []Entry, probed func(int) bool) error {
	wanted := false
	for i, saved := range entries {
		if saved.Name != "" && !saved.Paused && probed(i) {
			wanted = true
			break
		}
	}
	if !wanted {
		return nil
	}
	r.proxyMu.Lock()
	defer r.proxyMu.Unlock()
	return r.startProxyLocked()
}

// Toggle pauses a live forward, and resumes a paused or unbound one.
//
// Resuming is a bind like any other: the target is probed and the port taken,
// and either can fail. The failure is returned *and* recorded — the forward
// becomes unbound with LastErr — so the row and the footer say the same thing.
// Pausing cannot fail.
//
// It does I/O when resuming, so the router calls it from a Cmd.
func (r *Registry) Toggle(id string) (Forward, error) {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return Forward{}, fmt.Errorf("%w: %s", ErrNoSuchForward, id)
	}
	if e.forward.Name != "" {
		r.mu.Unlock()
		return r.toggleRoute(id)
	}
	if e.switching {
		r.mu.Unlock()
		return e.forward, ErrToggleBusy
	}

	if e.forward.State == StateLive {
		ln := e.ln
		e.ln = nil
		e.forward.State = StatePaused
		e.forward.LastErr = ""
		snapshot := e.forward
		r.mu.Unlock()
		if ln != nil {
			_ = ln.Close()
		}
		return snapshot, nil
	}

	e.switching = true
	port, target := e.forward.LocalPort, e.forward.Target
	r.mu.Unlock()

	ln, err := r.bindTCP(port, target)

	r.mu.Lock()
	defer r.mu.Unlock()
	e.switching = false
	if _, still := r.entries[id]; !still {
		// Closed while dialling: the user has already said they no longer want
		// it, and the listener just bound would be one nothing can stop.
		if ln != nil {
			_ = ln.Close()
		}
		return Forward{}, fmt.Errorf("%w: %s", ErrNoSuchForward, id)
	}
	if err != nil {
		e.forward.State = StateUnbound
		e.forward.LastErr = err.Error()
		return e.forward, err
	}
	e.ln = ln
	e.forward.State = StateLive
	e.forward.LastErr = ""
	e.forward.Opened = time.Now()
	go r.serve(e, ln)
	return e.forward, nil
}

// serve accepts until the listener is closed. It is handed the listener rather
// than reading e.ln: a pause sets that field to nil, and this loop is exactly
// the goroutine that would still be reading it.
func (r *Registry) serve(e *entry, ln net.Listener) {
	for {
		local, err := ln.Accept()
		if err != nil {
			// net.ErrClosed is Close doing its job, the same way
			// http.ErrServerClosed is for the MCP server. Anything else
			// ended an accept loop nobody asked to end, and the count of
			// live forwards is what already says the row is gone.
			return
		}
		go r.proxy(e, local)
	}
}

// proxy carries one connection, both ways, until either side is done.
func (r *Registry) proxy(e *entry, local net.Conn) {
	defer func() { _ = local.Close() }()

	remote, err := net.DialTimeout("tcp", e.forward.Target, dialTimeout)
	if err != nil {
		r.note(e.forward.ID, func(f *Forward) { f.LastErr = err.Error() })
		return
	}
	defer func() { _ = remote.Close() }()

	r.note(e.forward.ID, func(f *Forward) {
		f.Active++
		f.Total++
		f.LastErr = ""
	})
	defer r.note(e.forward.ID, func(f *Forward) { f.Active-- })

	var wg sync.WaitGroup
	wg.Add(2)
	go copyThenHalfClose(&wg, remote, local)
	go copyThenHalfClose(&wg, local, remote)
	wg.Wait()
}

// copyThenHalfClose copies src into dst, then shuts down dst's write side so
// the peer sees EOF. A plain Close on either side instead would cut the other
// direction short while it was still delivering — the shape of a truncated
// response that looks like a server fault.
func copyThenHalfClose(wg *sync.WaitGroup, dst, src net.Conn) {
	defer wg.Done()
	_, _ = io.Copy(dst, src)
	if cw, ok := dst.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = dst.Close()
}

// note applies a change to a live forward's counters. A forward closed while a
// connection was ending is simply gone, which is why the id is looked up again
// rather than the entry being written to directly.
func (r *Registry) note(id string, apply func(*Forward)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		apply(&e.forward)
	}
}

// validTarget rejects what net.Dial would only reject later, so that a typo is
// answered by the form rather than by a probe timing out.
func validTarget(target string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return fmt.Errorf("%q is not a host:port", target)
	}
	if host == "" {
		return fmt.Errorf("%q names no host", target)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%q is not a port number", port)
	}
	return nil
}
