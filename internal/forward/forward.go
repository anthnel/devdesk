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
// Nothing here knows about containers or about Docker. A caller that wants to
// forward to a container resolves its address first and hands over a host:port
// like any other.
package forward

import (
	"errors"
	"fmt"
	"io"
	"net"
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
)

// Forward is one live redirection, as a table reads it. It is a value: List
// hands out copies, so a view can hold one across frames without racing the
// goroutines that keep the real thing running.
type Forward struct {
	// ID identifies the forward across snapshots. It is a counter rather than
	// the local port, which is reused as soon as a forward is closed.
	ID        string
	LocalPort int
	Target    string
	// Label is where the forward came from — a container name, or empty when
	// the target was typed. It is decoration; nothing resolves it back.
	Label   string
	Opened  time.Time
	Active  int
	Total   int64
	LastErr string
}

// Addr is the address a client connects to.
func (f Forward) Addr() string {
	return net.JoinHostPort(loopbackHost, strconv.Itoa(f.LocalPort))
}

// entry is the live half: the listener, and the counters the connection
// goroutines write to.
type entry struct {
	forward Forward
	ln      net.Listener
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
func (r *Registry) Open(localPort int, target, label string) (Forward, error) {
	if localPort < firstUnprivilegedPort {
		return Forward{}, fmt.Errorf("%w: %d", ErrPrivilegedPort, localPort)
	}
	if err := validTarget(target); err != nil {
		return Forward{}, err
	}

	probe, err := net.DialTimeout("tcp", target, ProbeTimeout)
	if err != nil {
		return Forward{}, fmt.Errorf("%w: %s: %v", ErrTargetUnreachable, target, err)
	}
	_ = probe.Close()

	ln, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(localPort)))
	if err != nil {
		return Forward{}, fmt.Errorf("%w: %d: %v", ErrPortInUse, localPort, err)
	}

	r.mu.Lock()
	r.nextID++
	e := &entry{
		forward: Forward{
			ID:        strconv.Itoa(r.nextID),
			LocalPort: localPort,
			Target:    target,
			Label:     label,
			Opened:    time.Now(),
		},
		ln: ln,
	}
	r.entries[e.forward.ID] = e
	snapshot := e.forward
	r.mu.Unlock()

	go r.serve(e)
	return snapshot, nil
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
		_ = e.ln.Close()
	}
}

// List returns the open forwards, oldest first. The order is by open time
// rather than by port so that a row does not move when another forward is
// added below it.
func (r *Registry) List() []Forward {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Forward, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.forward)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Opened.Equal(out[j].Opened) {
			return out[i].ID < out[j].ID
		}
		return out[i].Opened.Before(out[j].Opened)
	})
	return out
}

// serve accepts until the listener is closed.
func (r *Registry) serve(e *entry) {
	for {
		local, err := e.ln.Accept()
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
