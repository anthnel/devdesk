// Package connectproxy is an HTTP CONNECT proxy for tests (DEVDESK-581). It
// answers CONNECT the way a corporate proxy does, can redirect a target to a
// real test server, refuse, or stay silent, and records what it was asked.
//
// It imports testing and is meant to be imported from _test.go files only, so
// it never reaches the production binary.
//
// The proxy owns its listener from the start (httptest binds 127.0.0.1:0 and
// keeps the socket open until the test ends), so it never hands a "free" port
// to code that binds it later (DEVDESK-35).
package connectproxy

import (
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// Options shapes how the proxy answers a CONNECT. The zero value tunnels to
// the requested target as is.
type Options struct {
	// Route maps the CONNECT target ("example.com:443") to the address the
	// proxy really dials. Nil dials the target itself.
	Route func(target string) string
	// Refuse, when non-zero, answers every CONNECT with this status code. It
	// takes precedence over Silent.
	Refuse int
	// Silent accepts the connection and never answers. Ignored when Refuse is set.
	Silent bool
	// TLS serves the proxy itself over TLS, to be used as an https:// proxy.
	TLS bool
}

// Request is what the proxy saw in one CONNECT.
type Request struct {
	// Target is the authority of the CONNECT line, "host:port".
	Target string
	// ProxyAuthorization is the Proxy-Authorization header, empty when absent.
	ProxyAuthorization string
}

// Proxy is a running test proxy. Start returns one; it stops on t.Cleanup.
type Proxy struct {
	srv  *httptest.Server
	opts Options

	mu    sync.Mutex
	reqs  []Request
	conns []net.Conn
	done  bool
}

// Start launches a proxy on 127.0.0.1 and stops it when the test ends.
func Start(t testing.TB, opts Options) *Proxy {
	t.Helper()
	p := &Proxy{opts: opts}
	p.srv = httptest.NewUnstartedServer(http.HandlerFunc(p.serve))
	if opts.TLS {
		p.srv.StartTLS()
	} else {
		p.srv.Start()
	}
	t.Cleanup(p.stop)
	return p
}

// URL is the proxy address to hand to the code under test: http:// or, with
// Options.TLS, https://. Each call returns a fresh copy the caller may edit,
// for instance to add a userinfo.
func (p *Proxy) URL() *url.URL {
	u, err := url.Parse(p.srv.URL)
	if err != nil {
		panic("connectproxy: unparsable listener URL: " + err.Error())
	}
	return u
}

// RootCAs is a pool that trusts the proxy's own certificate; it is only
// meaningful with Options.TLS.
func (p *Proxy) RootCAs() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(p.srv.Certificate())
	return pool
}

// Requests returns the CONNECTs received so far, oldest first.
func (p *Proxy) Requests() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Request(nil), p.reqs...)
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	target := r.Host
	p.mu.Lock()
	p.reqs = append(p.reqs, Request{Target: target, ProxyAuthorization: r.Header.Get("Proxy-Authorization")})
	p.mu.Unlock()

	if p.opts.Refuse != 0 {
		http.Error(w, http.StatusText(p.opts.Refuse), p.opts.Refuse)
		return
	}
	if p.opts.Silent {
		if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
			p.hold(conn)
		}
		return
	}

	addr := target
	if p.opts.Route != nil {
		addr = p.opts.Route(target)
	}
	upstream, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}
	client, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	if !p.hold(client) || !p.hold(upstream) {
		return
	}
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	// Bytes the client sent right behind the CONNECT sit in rw's reader.
	var pipes sync.WaitGroup
	pipes.Add(2)
	go func() {
		defer pipes.Done()
		_, _ = io.Copy(upstream, rw.Reader)
		_ = upstream.Close()
	}()
	go func() {
		defer pipes.Done()
		_, _ = io.Copy(client, upstream)
		_ = client.Close()
	}()
	pipes.Wait()
}

// hold remembers conn so stop can close it, since a hijacked connection is no
// longer tracked by the HTTP server. It closes conn and reports false when the
// proxy is already stopped.
func (p *Proxy) hold(conn net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		_ = conn.Close()
		return false
	}
	p.conns = append(p.conns, conn)
	return true
}

func (p *Proxy) stop() {
	p.mu.Lock()
	p.done = true
	conns := p.conns
	p.conns = nil
	p.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	p.srv.Close()
}
