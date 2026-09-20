package forward

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Named routes (§3.74): one HTTP listener on loopback, one port for every name,
// and the request's Host header decides where it goes. http://api.localhost:8080
// and http://app.localhost:8080 differ by name and not by port, which is the
// whole point of naming a service.
//
// Nothing here needs a privilege or a DNS entry. RFC 6761 reserves *.localhost
// for the loopback, and the platforms and browsers that were measured resolve it
// without any configuration.
//
// The limits are the ones the backlog states rather than discovers: HTTP only,
// so no certificate to trust; only the .localhost suffix; and the port stays in
// the URL, because 80 is privileged.

// routeSuffix is the only suffix a route name may carry. It is the one that the
// platform resolves to the loopback on its own.
const routeSuffix = ".localhost"

// maxHostLen and maxLabelLen are DNS's own limits.
const (
	maxHostLen  = 253
	maxLabelLen = 63
)

// proxyHeaderTimeout bounds how long a client may take to send its headers. It
// is a loopback listener and the client is a browser on this machine; the bound
// is here so that a stalled connection does not hold a goroutine forever.
const proxyHeaderTimeout = 10 * time.Second

// The failures a route adds to the ones a TCP forward already has.
var (
	// ErrBadRouteName is a name that is not a valid host name ending in
	// .localhost.
	ErrBadRouteName = errors.New("a route name must be a host name ending in .localhost")
	// ErrRouteNameTaken is a second route for a name.
	ErrRouteNameTaken = errors.New("a route with that name already exists")
	// ErrProxyPortInUse is the proxy's own bind failing. It is not ErrPortInUse:
	// the port is not one the user typed in the form, it is network.proxy_port,
	// and the sentence that follows has to say where to change it.
	ErrProxyPortInUse = errors.New("the proxy port is already in use")
	// ErrProxyPortTaken is a TCP forward asking for the proxy's port.
	ErrProxyPortTaken = errors.New("that port is the proxy's")
	// ErrNoProxyPort is a route opened before the router said which port the
	// proxy serves on.
	ErrNoProxyPort = errors.New("no proxy port is configured")
)

// normalizeRouteName validates a name and returns it in the form it is stored
// and compared in: lower case, since host names are case-insensitive.
func normalizeRouteName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(name, routeSuffix) || len(name) == len(routeSuffix) {
		return "", fmt.Errorf("%w: %q", ErrBadRouteName, name)
	}
	if len(name) > maxHostLen {
		return "", fmt.Errorf("%w: %q is longer than %d characters", ErrBadRouteName, name, maxHostLen)
	}
	for _, label := range strings.Split(name, ".") {
		if !validLabel(label) {
			return "", fmt.Errorf("%w: %q", ErrBadRouteName, name)
		}
	}
	return name, nil
}

// validLabel is one dot-separated part of a host name: letters, digits and
// hyphens, not starting or ending with one.
func validLabel(label string) bool {
	if label == "" || len(label) > maxLabelLen {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, c := range label {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}

// normalizeHost turns a request's Host into what a route is stored under: no
// port, no trailing dot, lower case.
func normalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// SetProxyPort says which port the proxy serves on. The router calls it at
// startup and whenever a context switch changes network.proxy_port.
//
// A proxy that is already serving is closed and bound again on the new port; if
// that bind fails, every route that was live becomes unbound with the reason,
// and the error is returned so the router can say it. Nothing is lost from the
// file: the routes are still wanted.
//
// It binds, so the router calls it from a Cmd.
func (r *Registry) SetProxyPort(port int) error {
	r.proxyMu.Lock()
	defer r.proxyMu.Unlock()

	r.mu.Lock()
	if r.proxyPort == port {
		r.mu.Unlock()
		return nil
	}
	r.proxyPort = port
	wasServing := r.proxySrv != nil
	r.mu.Unlock()

	if !wasServing {
		return nil
	}
	r.stopProxyLocked()
	err := r.startProxyLocked()
	if err != nil {
		r.mu.Lock()
		for _, e := range r.entries {
			if e.forward.Name != "" && e.forward.State == StateLive {
				e.forward.State = StateUnbound
				e.forward.LastErr = err.Error()
			}
		}
		r.mu.Unlock()
	}
	return err
}

// ProxyPort is the port the proxy serves on, zero before SetProxyPort.
func (r *Registry) ProxyPort() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.proxyPort
}

// OpenRoute adds a named route and starts the proxy if this is the first one.
//
// Like Open, it refuses rather than keeps: a typo answers at the form, and the
// target is dialled once first so that an address that answers nothing is
// refused now and not at the first request.
func (r *Registry) OpenRoute(name, target string) (Forward, error) {
	name, err := normalizeRouteName(name)
	if err != nil {
		return Forward{}, err
	}
	if err := validTarget(target); err != nil {
		return Forward{}, err
	}
	if r.routeNameTaken(name) {
		return Forward{}, fmt.Errorf("%w: %s", ErrRouteNameTaken, name)
	}
	if err := probeTarget(target); err != nil {
		return Forward{}, err
	}

	r.proxyMu.Lock()
	defer r.proxyMu.Unlock()
	if err := r.startProxyLocked(); err != nil {
		return Forward{}, err
	}

	r.mu.Lock()
	if r.routeNameTakenLocked(name) {
		r.mu.Unlock()
		// Someone else added it while this one dialled. The proxy this call
		// may have just started serves nothing yet.
		r.stopProxyIfIdleLocked()
		return Forward{}, fmt.Errorf("%w: %s", ErrRouteNameTaken, name)
	}
	e := r.add(0, target, nil)
	e.forward.Name = name
	snapshot := r.view(e)
	r.mu.Unlock()
	return snapshot, nil
}

// routeNameTaken reports whether a route already answers to name, in any state:
// a paused route still owns its name, or resuming it would find the name gone.
func (r *Registry) routeNameTaken(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.routeNameTakenLocked(name)
}

func (r *Registry) routeNameTakenLocked(name string) bool {
	for _, e := range r.entries {
		if e.forward.Name == name {
			return true
		}
	}
	return false
}

// view is an entry as a table reads it. A route has no port of its own: it is
// served on the proxy's, and that is the number to put in the URL. The caller
// holds the lock.
func (r *Registry) view(e *entry) Forward {
	f := e.forward
	if f.Name != "" {
		f.LocalPort = r.proxyPort
	}
	return f
}

// probeTarget dials target once, to learn that something answers.
func probeTarget(target string) error {
	probe, err := net.DialTimeout("tcp", target, ProbeTimeout)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrTargetUnreachable, target, err)
	}
	_ = probe.Close()
	return nil
}

// toggleRoute is Toggle for a named route. Pausing takes it out of the routing
// table, and the proxy closes with the last route that was being served.
// Resuming dials the target and, if needed, binds the proxy.
func (r *Registry) toggleRoute(id string) (Forward, error) {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return Forward{}, fmt.Errorf("%w: %s", ErrNoSuchForward, id)
	}
	if e.switching {
		snapshot := r.view(e)
		r.mu.Unlock()
		return snapshot, ErrToggleBusy
	}

	if e.forward.State == StateLive {
		e.forward.State = StatePaused
		e.forward.LastErr = ""
		snapshot := r.view(e)
		r.mu.Unlock()

		r.proxyMu.Lock()
		r.stopProxyIfIdleLocked()
		r.proxyMu.Unlock()
		return snapshot, nil
	}

	e.switching = true
	target := e.forward.Target
	r.mu.Unlock()

	err := probeTarget(target)
	if err == nil {
		r.proxyMu.Lock()
		err = r.startProxyLocked()
		r.proxyMu.Unlock()
	}

	r.mu.Lock()
	e.switching = false
	if _, still := r.entries[id]; !still {
		r.mu.Unlock()
		// Closed while dialling. The proxy just started may serve nothing.
		r.proxyMu.Lock()
		r.stopProxyIfIdleLocked()
		r.proxyMu.Unlock()
		return Forward{}, fmt.Errorf("%w: %s", ErrNoSuchForward, id)
	}
	if err != nil {
		e.forward.State = StateUnbound
		e.forward.LastErr = err.Error()
		snapshot := r.view(e)
		r.mu.Unlock()
		return snapshot, err
	}
	e.forward.State = StateLive
	e.forward.LastErr = ""
	e.forward.Opened = time.Now()
	snapshot := r.view(e)
	r.mu.Unlock()
	return snapshot, nil
}

// startProxyLocked binds the proxy if it is not bound. The caller holds
// proxyMu and not mu: the bind is I/O, and mu is never held across it.
func (r *Registry) startProxyLocked() error {
	if r.proxySrv != nil {
		return nil
	}
	port := r.ProxyPort()
	if port == 0 {
		return ErrNoProxyPort
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("%w: %d: %v", ErrProxyPortInUse, port, err)
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(r.serveHTTP),
		ReadHeaderTimeout: proxyHeaderTimeout,
	}
	r.proxySrv, r.proxyLn = srv, ln
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// stopProxyLocked closes the proxy and the connections it carries: a route the
// user stopped should stop, the same rule as Close.
func (r *Registry) stopProxyLocked() {
	if r.proxySrv != nil {
		_ = r.proxySrv.Close()
		// Close reaches the listener only once Serve has registered it, which
		// happens on the goroutine startProxyLocked launched. Closing it here
		// too is what makes the port free when this returns, and a rebind on the
		// next line — a pause then a resume, a port change — depends on that.
		_ = r.proxyLn.Close()
		r.proxySrv, r.proxyLn = nil, nil
	}
}

// stopProxyIfIdleLocked closes the proxy when no route is being served. The
// caller holds proxyMu.
func (r *Registry) stopProxyIfIdleLocked() {
	r.mu.Lock()
	serving := false
	for _, e := range r.entries {
		if e.forward.Name != "" && e.forward.State == StateLive {
			serving = true
			break
		}
	}
	r.mu.Unlock()
	if !serving {
		r.stopProxyLocked()
	}
}

// releaseProxyIfIdle is stopProxyIfIdleLocked for a caller that does not hold
// proxyMu.
func (r *Registry) releaseProxyIfIdle() {
	r.proxyMu.Lock()
	defer r.proxyMu.Unlock()
	r.stopProxyIfIdleLocked()
}

// routeFor finds the live route a Host belongs to.
func (r *Registry) routeFor(host string) (id, target string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.forward.Name == host && e.forward.State == StateLive {
			return e.forward.ID, e.forward.Target, true
		}
	}
	return "", "", false
}

// targetKey carries a route's target to the reverse proxy's Rewrite.
type targetKey struct{}

// routeTransport dials with the same bound as a TCP forward's connections.
var routeTransport = &http.Transport{
	DialContext:         (&net.Dialer{Timeout: dialTimeout}).DialContext,
	MaxIdleConnsPerHost: 4,
	IdleConnTimeout:     30 * time.Second,
}

// serveHTTP answers one request: the route it names, or a page saying there is
// none.
func (r *Registry) serveHTTP(w http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)
	id, target, ok := r.routeFor(host)
	if !ok {
		r.writeNoRoute(w, host)
		return
	}

	r.note(id, func(f *Forward) {
		f.Active++
		f.Total++
	})
	defer r.note(id, func(f *Forward) { f.Active-- })

	failed := false
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: pr.In.Context().Value(targetKey{}).(string)})
			// The inbound Host is kept: an application behind a named route
			// often keys on it, and SetURL would replace it with the target's.
			pr.Out.Host = pr.In.Host
			pr.SetXForwarded()
		},
		Transport: routeTransport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			failed = true
			r.note(id, func(f *Forward) { f.LastErr = err.Error() })
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprintf(w, "%s did not answer.\n\n%v\n", target, err)
		},
	}
	proxy.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), targetKey{}, target)))
	if !failed {
		// A request that got through says the route works, the way a carried
		// TCP connection does.
		r.note(id, func(f *Forward) { f.LastErr = "" })
	}
}

// writeNoRoute is the 404: it names what was asked for and lists what is
// served, which is the answer to nearly every way of arriving here.
func (r *Registry) writeNoRoute(w http.ResponseWriter, host string) {
	r.mu.Lock()
	port := r.proxyPort
	var names []string
	targets := map[string]string{}
	for _, e := range r.entries {
		if e.forward.Name != "" && e.forward.State == StateLive {
			names = append(names, e.forward.Name)
			targets[e.forward.Name] = e.forward.Target
		}
	}
	r.mu.Unlock()
	sort.Strings(names)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	_, _ = fmt.Fprintf(w, "No route for %q.\n\nServed here:\n", host)
	for _, name := range names {
		_, _ = fmt.Fprintf(w, "  http://%s:%d  ->  %s\n", name, port, targets[name])
	}
	if len(names) == 0 {
		_, _ = fmt.Fprintln(w, "  (nothing)")
	}
}
