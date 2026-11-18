package connectproxy

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/testutil/loopback"
)

// dialProxy opens a connection to the proxy, over TLS when it serves TLS.
func dialProxy(t *testing.T, p *Proxy) net.Conn {
	t.Helper()
	u := p.URL()
	var conn net.Conn
	var err error
	if u.Scheme == "https" {
		conn, err = tls.Dial("tcp", u.Host, &tls.Config{RootCAs: p.RootCAs()})
	} else {
		conn, err = net.Dial("tcp", u.Host)
	}
	if err != nil {
		t.Fatalf("dialing the proxy %s: %v", u, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// connect sends a CONNECT for target, with the extra header lines, and reads
// the proxy's answer.
func connect(t *testing.T, conn net.Conn, target string, headers ...string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	for _, h := range headers {
		req += h + "\r\n"
	}
	if _, err := io.WriteString(conn, req+"\r\n"); err != nil {
		t.Fatalf("writing CONNECT: %v", err)
	}
	br := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("reading the answer to CONNECT: %v", err)
	}
	return resp, br
}

// bufConn reads through br, which may hold bytes the proxy sent right behind
// its answer.
type bufConn struct {
	net.Conn
	r io.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func TestProxyRecordsEachConnect(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		headers []string
		target  string
		want    Request
	}{
		{"plain target", Options{Refuse: http.StatusForbidden}, nil, "example.com:443", Request{Target: "example.com:443"}},
		{"with credentials", Options{Refuse: http.StatusForbidden}, []string{"Proxy-Authorization: Basic dTpw"}, "a.corp.test:8443",
			Request{Target: "a.corp.test:8443", ProxyAuthorization: "Basic dTpw"}},
		{"over TLS", Options{Refuse: http.StatusForbidden, TLS: true}, nil, "example.com:443", Request{Target: "example.com:443"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Start(t, tt.opts)
			if got := p.Requests(); len(got) != 0 {
				t.Fatalf("a fresh proxy reports requests: %+v", got)
			}
			connect(t, dialProxy(t, p), tt.target, tt.headers...)
			got := p.Requests()
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("Requests() = %+v, want [%+v]", got, tt.want)
			}
		})
	}
}

func TestProxyRefuses(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"forbidden", http.StatusForbidden},
		{"proxy authentication required", http.StatusProxyAuthRequired},
		{"bad gateway", http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Start(t, Options{Refuse: tt.code})
			resp, _ := connect(t, dialProxy(t, p), "example.com:443")
			if resp.StatusCode != tt.code {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.code)
			}
			if n := len(p.Requests()); n != 1 {
				t.Fatalf("a refused CONNECT is still recorded: %d requests, want 1", n)
			}
		})
	}
}

func TestProxyRoutesToTheRealServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "reached")
	}))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	tests := []struct {
		name string
		opts func(realAddr string) Options
	}{
		{"http proxy", func(real string) Options {
			return Options{Route: func(string) string { return real }}
		}},
		{"https proxy", func(real string) Options {
			return Options{TLS: true, Route: func(string) string { return real }}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var routed []string
			opts := tt.opts(srv.Listener.Addr().String())
			inner := opts.Route
			opts.Route = func(target string) string {
				mu.Lock()
				defer mu.Unlock()
				routed = append(routed, target)
				return inner(target)
			}
			p := Start(t, opts)

			// example.com is in the httptest certificate; the proxy sends it to
			// the real listener.
			conn := dialProxy(t, p)
			resp, br := connect(t, conn, "example.com:443")
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("CONNECT answered %d, want 200", resp.StatusCode)
			}
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			tlsConn := tls.Client(&bufConn{Conn: conn, r: br}, &tls.Config{ServerName: "example.com", RootCAs: pool})
			if err := tlsConn.Handshake(); err != nil {
				t.Fatalf("handshake through the tunnel: %v", err)
			}
			if _, err := io.WriteString(tlsConn, "GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"); err != nil {
				t.Fatalf("writing through the tunnel: %v", err)
			}
			body, _ := io.ReadAll(tlsConn)
			if !strings.Contains(string(body), "reached") {
				t.Fatalf("answer through the tunnel = %q, want it to contain %q", body, "reached")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(routed) != 1 || routed[0] != "example.com:443" {
				t.Fatalf("Route saw %v, want [example.com:443]", routed)
			}
		})
	}
}

func TestProxyAnswers502WhenTheRouteIsUnreachable(t *testing.T) {
	// A reserved port is bound but never listening: connecting to it is refused
	// at once, with no DNS and no reliance on a port that was merely free.
	dead := net.JoinHostPort("127.0.0.1", strconv.Itoa(loopback.ReservePort(t)))
	p := Start(t, Options{Route: func(string) string { return dead }})
	resp, _ := connect(t, dialProxy(t, p), "example.com:443")
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}

func TestProxyRefuseWinsOverSilent(t *testing.T) {
	p := Start(t, Options{Refuse: http.StatusForbidden, Silent: true})
	resp, _ := connect(t, dialProxy(t, p), "example.com:443")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestProxySilentNeverAnswers(t *testing.T) {
	p := Start(t, Options{Silent: true})
	conn := dialProxy(t, p)
	if _, err := io.WriteString(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	n, err := conn.Read(make([]byte, 1))
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() || n != 0 {
		t.Fatalf("Read = (%d, %v), want a timeout and no byte", n, err)
	}
	if got := p.Requests(); len(got) != 1 || got[0].Target != "example.com:443" {
		t.Fatalf("Requests() = %+v, want the one CONNECT", got)
	}
}

func TestProxyRefusesWhatIsNotConnect(t *testing.T) {
	p := Start(t, Options{})
	conn := dialProxy(t, p)
	if _, err := io.WriteString(conn, "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
	if n := len(p.Requests()); n != 0 {
		t.Fatalf("a non-CONNECT request is recorded: %d requests, want 0", n)
	}
}

func TestProxyURL(t *testing.T) {
	tests := []struct {
		name   string
		tls    bool
		scheme string
	}{
		{"plain", false, "http"},
		{"TLS", true, "https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Start(t, Options{TLS: tt.tls})
			u := p.URL()
			if u.Scheme != tt.scheme {
				t.Fatalf("scheme = %q, want %q", u.Scheme, tt.scheme)
			}
			host, _, err := net.SplitHostPort(u.Host)
			if err != nil || host != "127.0.0.1" {
				t.Fatalf("host = %q (%v), want 127.0.0.1 with a port", u.Host, err)
			}
			// A caller editing its copy must not change the next one.
			u.Host = "elsewhere:1"
			if p.URL().Host == "elsewhere:1" {
				t.Fatal("URL() hands out a shared value")
			}
		})
	}
}

// TestProxyStopsWithTheTest checks that stopping the proxy cuts a tunnel that
// is still open, without dialing a released port (DEVDESK-35).
func TestProxyStopsWithTheTest(t *testing.T) {
	// The upstream only has to exist: the kernel completes the proxy's dial.
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = upstream.Close() }()

	var conn net.Conn
	t.Run("inner", func(t *testing.T) {
		p := Start(t, Options{Route: func(string) string { return upstream.Addr().String() }})
		// Not dialProxy: its cleanup would close the connection with the subtest.
		c, err := net.Dial("tcp", p.URL().Host)
		if err != nil {
			t.Fatal(err)
		}
		conn = c
		if resp, _ := connect(t, c, "example.com:443"); resp.StatusCode != http.StatusOK {
			t.Fatalf("CONNECT answered %d, want 200", resp.StatusCode)
		}
	})
	defer func() { _ = conn.Close() }()

	// The subtest's cleanup ran: the tunnel must be closed from the proxy's side.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(make([]byte, 1))
	var ne net.Error
	if n != 0 || err == nil || (errors.As(err, &ne) && ne.Timeout()) {
		t.Fatalf("Read = (%d, %v), want the tunnel closed (EOF or reset) once the proxy stopped", n, err)
	}
}
