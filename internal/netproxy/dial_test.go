package netproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/testutil/connectproxy"
	"github.com/anthnel/devdesk/internal/testutil/loopback"
)

// trackedConn remembers whether it was closed.
type trackedConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *trackedConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

// tracker is a Dialer.DialContext that records every address asked for and
// every connection it opened.
type tracker struct {
	mu    sync.Mutex
	addrs []string
	conns []*trackedConn
}

func (tr *tracker) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	tr.mu.Lock()
	tr.addrs = append(tr.addrs, addr)
	tr.mu.Unlock()
	c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	tc := &trackedConn{Conn: c}
	tr.mu.Lock()
	tr.conns = append(tr.conns, tc)
	tr.mu.Unlock()
	return tc, nil
}

func (tr *tracker) dialed() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]string(nil), tr.addrs...)
}

func (tr *tracker) opened() []*trackedConn {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]*trackedConn(nil), tr.conns...)
}

// trusting is a client config that trusts only srv's certificate.
func trusting(srv *httptest.Server) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool}
}

func testCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

// dialTLS runs DialTLS and closes the connection when the test ends.
func dialTLS(t *testing.T, ctx context.Context, d Dialer, hostport string, cfg *tls.Config) (*tls.Conn, error) {
	t.Helper()
	conn, err := d.DialTLS(ctx, hostport, cfg)
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn, err
}

// proxyError asserts err is a *Error and returns it.
func proxyError(t *testing.T, err error) *Error {
	t.Helper()
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("error = %T %v, want a *netproxy.Error", err, err)
	}
	return pe
}

// reservedProxyURL is the URL of a proxy nobody listens on: connecting to a
// reserved port is refused at once (DEVDESK-35).
func reservedProxyURL(t *testing.T) *url.URL {
	t.Helper()
	return mustURL(t, "http://"+net.JoinHostPort("127.0.0.1", strconv.Itoa(loopback.ReservePort(t))))
}

// rawProxy is a proxy that reads one request per connection and lets answer
// write whatever bytes it wants, for answers connectproxy refuses to produce.
func rawProxy(t *testing.T, answer func(conn net.Conn)) *url.URL {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
		wg    sync.WaitGroup
		done  = make(chan struct{})
	)
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { _ = c.Close() }()
				if _, err := http.ReadRequest(bufio.NewReader(c)); err != nil {
					return
				}
				answer(c)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return mustURL(t, "http://"+ln.Addr().String())
}

// peerChain is the raw certificate chain the peer presented.
func peerChain(certs []*x509.Certificate) [][]byte {
	var chain [][]byte
	for _, c := range certs {
		chain = append(chain, c.Raw)
	}
	return chain
}

func TestDialTLSThroughAProxyPresentsTheSameChainAsHTTPS(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	d := Dialer{Proxy: fixed(proxy.URL())}

	conn, err := dialTLS(t, testCtx(t, 10*time.Second), d, "example.com:443", trusting(srv))
	if err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	state := conn.ConnectionState()
	if !state.HandshakeComplete {
		t.Fatal("the handshake is not complete")
	}
	if state.ServerName != "example.com" {
		t.Fatalf("ServerName = %q, want the host of the target", state.ServerName)
	}
	want := []connectproxy.Request{{Target: "example.com:443"}}
	if got := proxy.Requests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("proxy received %+v, want exactly %+v", got, want)
	}

	// The same target fetched by net/http, through the same proxy.
	resp, _ := httpsGet(t, NewTransport(fixed(proxy.URL())), srv, "https://example.com/")
	got, wantChain := peerChain(state.PeerCertificates), peerChain(resp.TLS.PeerCertificates)
	if len(got) == 0 || !reflect.DeepEqual(got, wantChain) {
		t.Fatalf("DialTLS chain (%d certificates) differs from the HTTPS monitor's (%d)", len(got), len(wantChain))
	}
}

func TestDialTLSKeepsAnExplicitServerName(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	cfg := trusting(srv)
	cfg.ServerName = "example.com"
	// The target host is not in the certificate; the explicit name is.
	conn, err := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: fixed(proxy.URL())}, "other.test:443", cfg)
	if err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	if got := conn.ConnectionState().ServerName; got != "example.com" {
		t.Fatalf("ServerName = %q, want %q", got, "example.com")
	}
}

const (
	// dialHelperEnv makes the test binary run TestDialTLSEnvironmentHelper for real.
	dialHelperEnv = "NETPROXY_TEST_DIAL_HELPER"
	// dialHelperServer is the address the helper's DialContext gives for example.com:443.
	dialHelperServer = "NETPROXY_TEST_DIAL_SERVER"
	// dialHelperCert is the target's certificate, DER in base64.
	dialHelperCert = "NETPROXY_TEST_DIAL_CERT"
)

// TestDialTLSEnvironmentHelper is the child half of
// TestDialTLSNilSelectorFollowsTheEnvironment: it does nothing in a normal run.
// It dials example.com:443 with a zero Dialer (Proxy nil) and prints the
// addresses its DialContext was asked for. The proxy variables are read once
// per process, so the parent runs the test binary again with the ones it wants.
func TestDialTLSEnvironmentHelper(t *testing.T) {
	if os.Getenv(dialHelperEnv) != "1" {
		t.Skip("child of TestDialTLSNilSelectorFollowsTheEnvironment")
	}
	der, err := base64.StdEncoding.DecodeString(os.Getenv(dialHelperCert))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	var tr tracker
	server := os.Getenv(dialHelperServer)
	d := Dialer{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "example.com:443" {
			addr = server
		}
		return tr.DialContext(ctx, network, addr)
	}}
	conn, err := d.DialTLS(testCtx(t, 10*time.Second), "example.com:443", &tls.Config{RootCAs: pool})
	if err != nil {
		fmt.Println(resultPrefix + "error: " + err.Error())
		return
	}
	_ = conn.Close()
	fmt.Println(resultPrefix + "dialed " + strings.Join(tr.dialed(), ","))
}

// TestDialTLSNilSelectorFollowsTheEnvironment runs a zero Dialer in a child
// process with the proxy variables set, against a target that is not loopback
// (a loopback target is never proxied): the child reaches the target through
// the proxy by default, and directly when NO_PROXY names it.
func TestDialTLSNilSelectorFollowsTheEnvironment(t *testing.T) {
	srv := newTLSServer(t)
	real := srv.Listener.Addr().String()
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return real },
	})
	extra := map[string]string{
		dialHelperEnv:    "1",
		dialHelperServer: real,
		dialHelperCert:   base64.StdEncoding.EncodeToString(srv.Certificate().Raw),
	}
	tests := []struct {
		name         string
		vars         map[string]string
		want         string
		wantConnects int
	}{
		{"HTTPS_PROXY carries the connection", map[string]string{"HTTPS_PROXY": proxy.URL().String()},
			"dialed " + proxy.URL().Host, 1},
		{"NO_PROXY keeps it direct", map[string]string{"HTTPS_PROXY": proxy.URL().String(), "NO_PROXY": "example.com"},
			"dialed " + real, 0},
		{"no variable keeps it direct", nil, "dialed " + real, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(proxy.Requests())
			if got := runChild(t, "TestDialTLSEnvironmentHelper", extra, tt.vars); got != tt.want {
				t.Fatalf("child with %v = %q, want %q", tt.vars, got, tt.want)
			}
			reqs := proxy.Requests()[before:]
			if len(reqs) != tt.wantConnects {
				t.Fatalf("proxy received %d CONNECTs, want %d: %+v", len(reqs), tt.wantConnects, reqs)
			}
			if tt.wantConnects == 1 && reqs[0].Target != "example.com:443" {
				t.Fatalf("CONNECT target = %q, want example.com:443", reqs[0].Target)
			}
		})
	}
}

func TestDialTLSDirectSelectorDialsTheTarget(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{})
	var tr tracker
	real := srv.Listener.Addr().String()
	d := Dialer{
		Proxy: direct,
		// example.com is a name in the httptest certificate; route it to the server.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "example.com:443" {
				addr = real
			}
			return tr.DialContext(ctx, network, addr)
		},
	}
	if _, err := dialTLS(t, testCtx(t, 10*time.Second), d, "example.com:443", trusting(srv)); err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	if got := tr.dialed(); !reflect.DeepEqual(got, []string{real}) {
		t.Fatalf("dialed %v, want the target only", got)
	}
	if got := proxy.Requests(); len(got) != 0 {
		t.Fatalf("a direct connection reached the proxy: %+v", got)
	}
}

func TestDialTLSAsksTheSelectorAboutAnHTTPSURL(t *testing.T) {
	srv := newTLSServer(t)
	var seen *url.URL
	sel := func(u *url.URL) (*url.URL, error) { seen = u; return nil, nil }
	real := srv.Listener.Addr().String()
	d := Dialer{Proxy: sel, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, real)
	}}
	if _, err := dialTLS(t, testCtx(t, 10*time.Second), d, "example.com:443", trusting(srv)); err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	if seen == nil || seen.Scheme != "https" || seen.Host != "example.com:443" {
		t.Fatalf("selector saw %v, want https://example.com:443", seen)
	}
}

func TestDialTLSRefusesAHostWithoutAPort(t *testing.T) {
	var tr tracker
	if _, err := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: direct, DialContext: tr.DialContext}, "example.com", nil); err == nil {
		t.Fatal("DialTLS accepted a host without a port")
	}
	if got := tr.dialed(); len(got) != 0 {
		t.Fatalf("dialed %v for an invalid address", got)
	}
}

// TestDialTLSRefusesControlBytesInTheTarget: a CR, LF, space or other control
// byte in the target would end the CONNECT line or add a header to it. Nothing
// is dialed and the selector is not even asked.
func TestDialTLSRefusesControlBytesInTheTarget(t *testing.T) {
	proxy := connectproxy.Start(t, connectproxy.Options{})
	tests := []struct{ name, hostport string }{
		{"header injection in a bracketed host", "[a\r\nX-Inj: 1]:443"},
		{"line feed in the host", "example.com\n:443"},
		{"space in the host", "exa mple.com:443"},
		{"tab in the host", "exa\tmple.com:443"},
		{"NUL in the host", "example.com\x00:443"},
		{"DEL in the host", "example.com\x7f:443"},
		{"line break after the port", "example.com:443\r\nX-Inj: 1"},
		{"space in the port", "example.com:44 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tr tracker
			asked := false
			sel := func(*url.URL) (*url.URL, error) { asked = true; return proxy.URL(), nil }
			d := Dialer{Proxy: sel, DialContext: tr.DialContext}
			conn, err := dialTLS(t, testCtx(t, 10*time.Second), d, tt.hostport, nil)
			if err == nil || conn != nil {
				t.Fatalf("DialTLS(%q) = %v, %v; want an error", tt.hostport, conn, err)
			}
			if strings.ContainsAny(err.Error(), "\r\n\x00\x7f") {
				t.Fatalf("the error repeats the control bytes raw: %q", err)
			}
			if asked {
				t.Fatal("the selector was asked about an invalid target")
			}
			if got := tr.dialed(); len(got) != 0 {
				t.Fatalf("dialed %v for an invalid target", got)
			}
			if got := proxy.Requests(); len(got) != 0 {
				t.Fatalf("the proxy received %+v", got)
			}
		})
	}
}

func TestDialTLSProxyRefusal(t *testing.T) {
	tests := []struct {
		name string
		code int
		want string
	}{
		{"forbidden", http.StatusForbidden, "proxy: CONNECT refused (403 Forbidden)"},
		{"proxy authentication required", http.StatusProxyAuthRequired, "proxy: CONNECT refused (407 Proxy Authentication Required)"},
		{"bad gateway", http.StatusBadGateway, "proxy: CONNECT refused (502 Bad Gateway)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := connectproxy.Start(t, connectproxy.Options{Refuse: tt.code})
			var tr tracker
			d := Dialer{Proxy: fixed(proxy.URL()), DialContext: tr.DialContext}
			conn, err := dialTLS(t, testCtx(t, 10*time.Second), d, "example.com:443", nil)
			if conn != nil || err == nil {
				t.Fatalf("DialTLS = %v, %v; want an error and no connection", conn, err)
			}
			pe := proxyError(t, err)
			if pe.Step != StepConnect || pe.StatusCode != tt.code {
				t.Fatalf("Step, StatusCode = %q, %d; want %q, %d", pe.Step, pe.StatusCode, StepConnect, tt.code)
			}
			if pe.Proxy != proxy.URL().Host {
				t.Fatalf("Proxy = %q, want %q", pe.Proxy, proxy.URL().Host)
			}
			if err.Error() != tt.want {
				t.Fatalf("text = %q, want %q", err, tt.want)
			}
			for _, c := range tr.opened() {
				if !c.closed.Load() {
					t.Fatal("the connection to the proxy was left open after the failure")
				}
			}
		})
	}
}

// TestDialTLSNeverRepeatsTheProxysWords: a proxy is not trusted to word an
// error shown to the user; the text comes from the status code alone.
func TestDialTLSNeverRepeatsTheProxysWords(t *testing.T) {
	hostile := "HTTP/1.1 403 hostile-phrase-do-not-print\r\nContent-Length: 0\r\n\r\n"
	proxy := rawProxy(t, func(c net.Conn) { _, _ = fmt.Fprint(c, hostile) })
	_, err := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: fixed(proxy)}, "example.com:443", nil)
	pe := proxyError(t, err)
	if pe.Step != StepConnect || pe.StatusCode != http.StatusForbidden {
		t.Fatalf("Step, StatusCode = %q, %d; want %q, 403", pe.Step, pe.StatusCode, StepConnect)
	}
	if want := "proxy: CONNECT refused (403 Forbidden)"; err.Error() != want {
		t.Fatalf("text = %q, want %q", err, want)
	}
	if strings.Contains(fmt.Sprintf("%v %+v", err, err), "hostile-phrase") {
		t.Fatal("the proxy's own status phrase leaked into the error")
	}
}

func TestDialTLSUnreadableAnswer(t *testing.T) {
	padding := strings.Repeat("a", 100<<10) // well past the 64 KiB cap
	tests := []struct {
		name   string
		answer string
	}{
		{"not HTTP", "this is not http\r\n\r\n"},
		{"empty answer", ""},
		{"header larger than the cap", "HTTP/1.1 200 OK\r\nX-Pad: " + padding + "\r\n\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := rawProxy(t, func(c net.Conn) { _, _ = fmt.Fprint(c, tt.answer) })
			start := time.Now()
			_, err := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: fixed(proxy)}, "example.com:443", nil)
			pe := proxyError(t, err)
			if pe.Step != StepConnect || pe.StatusCode != 0 {
				t.Fatalf("Step, StatusCode = %q, %d; want %q, 0", pe.Step, pe.StatusCode, StepConnect)
			}
			if want := "proxy: unreadable answer to CONNECT from " + proxy.Host; err.Error() != want {
				t.Fatalf("text = %q, want %q", err, want)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatalf("took %v: the answer was not cut at the cap", time.Since(start))
			}
		})
	}
}

func TestDialTLSProxyUnreachable(t *testing.T) {
	proxy := reservedProxyURL(t)
	_, err := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: fixed(proxy)}, "example.com:443", nil)
	pe := proxyError(t, err)
	if pe.Step != StepDial || pe.Proxy != proxy.Host {
		t.Fatalf("Step, Proxy = %q, %q; want %q, %q", pe.Step, pe.Proxy, StepDial, proxy.Host)
	}
	if want := "proxy: cannot reach " + proxy.Host + ": "; !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("text = %q, want it to start with %q", err, want)
	}
	if errors.Unwrap(err) == nil {
		t.Fatal("the dial failure is not wrapped")
	}
}

func TestDialTLSSilentProxyTimesOut(t *testing.T) {
	proxy := connectproxy.Start(t, connectproxy.Options{Silent: true})
	start := time.Now()
	_, err := dialTLS(t, testCtx(t, 200*time.Millisecond), Dialer{Proxy: fixed(proxy.URL())}, "example.com:443", nil)
	elapsed := time.Since(start)
	pe := proxyError(t, err)
	if pe.Step != StepConnect || pe.StatusCode != 0 {
		t.Fatalf("Step, StatusCode = %q, %d; want %q, 0", pe.Step, pe.StatusCode, StepConnect)
	}
	if want := "proxy: no answer from " + proxy.URL().Host + " (timeout)"; err.Error() != want {
		t.Fatalf("text = %q, want %q", err, want)
	}
	if elapsed > time.Second {
		t.Fatalf("returned after %v, want under 1s for a 200ms deadline", elapsed)
	}
	if n := len(proxy.Requests()); n != 1 {
		t.Fatalf("proxy received %d CONNECTs, want 1", n)
	}
}

func TestDialTLSCancellationEndsASilentProxy(t *testing.T) {
	proxy := connectproxy.Start(t, connectproxy.Options{Silent: true})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := dialTLS(t, ctx, Dialer{Proxy: fixed(proxy.URL())}, "example.com:443", nil)
	if err == nil {
		t.Fatal("DialTLS succeeded against a silent proxy")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("returned after %v, want under 1s after the cancellation", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want errors.Is(context.Canceled)", err)
	}
	pe := proxyError(t, err)
	if pe.Step != StepConnect || pe.StatusCode != 0 {
		t.Fatalf("Step, StatusCode = %q, %d; want %q, 0", pe.Step, pe.StatusCode, StepConnect)
	}
	if want := "proxy: CONNECT to " + proxy.URL().Host + " canceled"; err.Error() != want {
		t.Fatalf("text = %q, want %q", err, want)
	}
}

// TestDialTLSTunnelHandsBackBytesReadWithTheAnswer: a proxy that writes its 200
// and the first bytes of the tunnel in one segment must not lose those bytes to
// the reader that parsed the header; the tunnel reads them first.
func TestDialTLSTunnelHandsBackBytesReadWithTheAnswer(t *testing.T) {
	const early = "early tunnel bytes"
	proxy := rawProxy(t, func(c net.Conn) {
		// One Write: the header and the tunnel's first bytes arrive together.
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"+early)
	})
	conn, err := Dialer{}.tunnel(testCtx(t, 10*time.Second), proxy, "example.com:443")
	if err != nil {
		t.Fatalf("tunnel: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, ok := conn.(*bufferedConn); !ok {
		t.Fatalf("tunnel returned %T, want the buffered connection that holds the early bytes", conn)
	}
	got, err := io.ReadAll(conn) // the proxy closes right after: EOF ends it
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != early {
		t.Fatalf("tunnel read %q, want %q first", got, early)
	}
}

func TestProxyAddr(t *testing.T) {
	tests := []struct{ proxy, want string }{
		{"http://proxy.corp", "proxy.corp:80"},
		{"https://proxy.corp", "proxy.corp:443"},
		{"http://proxy.corp:3128", "proxy.corp:3128"},
		{"https://proxy.corp:8443", "proxy.corp:8443"},
		{"http://user:secret@proxy.corp", "proxy.corp:80"},
		{"https://user:secret@proxy.corp:8443", "proxy.corp:8443"},
		{"http://[::1]", "[::1]:80"},
		{"https://[::1]", "[::1]:443"},
		{"http://[::1]:3128", "[::1]:3128"},
	}
	for _, tt := range tests {
		t.Run(tt.proxy, func(t *testing.T) {
			if got := proxyAddr(mustURL(t, tt.proxy)); got != tt.want {
				t.Fatalf("proxyAddr(%s) = %q, want %q", tt.proxy, got, tt.want)
			}
		})
	}
}

// TestDialTLSOneDeadlineCoversTheHandshake: the CONNECT succeeds, then the
// target never answers the handshake; the same deadline must end the wait, and
// the failure is the target's, not the proxy's.
func TestDialTLSOneDeadlineCoversTheHandshake(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return held.Addr().String() },
	})
	start := time.Now()
	_, err = dialTLS(t, testCtx(t, 200*time.Millisecond), Dialer{Proxy: fixed(proxy.URL())}, "example.com:443", nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("DialTLS succeeded against a target that never answers")
	}
	var pe *Error
	if errors.As(err, &pe) {
		t.Fatalf("a stalled target handshake is reported as a proxy failure: %v", err)
	}
	if !isTimeout(err) {
		t.Fatalf("error = %v, want a timeout", err)
	}
	if elapsed > time.Second {
		t.Fatalf("returned after %v, want under 1s for a 200ms deadline", elapsed)
	}
}

func basicAuth(userinfo string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(userinfo))
}

func TestDialTLSSendsTheProxyCredentials(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	u := proxy.URL()
	u.User = url.UserPassword("u", "secret")
	if _, err := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: fixed(u)}, "example.com:443", trusting(srv)); err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	want := []connectproxy.Request{{Target: "example.com:443", ProxyAuthorization: basicAuth("u:secret")}}
	if got := proxy.Requests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("proxy received %+v, want %+v", got, want)
	}
}

func TestDialTLSNoProxyCredentialsWithoutUserinfo(t *testing.T) {
	proxy := connectproxy.Start(t, connectproxy.Options{Refuse: http.StatusForbidden})
	_, _ = dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: fixed(proxy.URL())}, "example.com:443", nil)
	got := proxy.Requests()
	if len(got) != 1 || got[0].ProxyAuthorization != "" {
		t.Fatalf("proxy received %+v, want one CONNECT without Proxy-Authorization", got)
	}
}

// TestDialTLSNeverShowsTheUserinfo: whatever fails, the secret in the proxy URL
// is in no text the error can produce.
func TestDialTLSNeverShowsTheUserinfo(t *testing.T) {
	withSecret := func(u *url.URL) *url.URL {
		u.User = url.UserPassword("u", "secret")
		return u
	}
	tests := []struct {
		name  string
		proxy func(t *testing.T) *url.URL
		ctxd  time.Duration
	}{
		{"refused", func(t *testing.T) *url.URL {
			return connectproxy.Start(t, connectproxy.Options{Refuse: http.StatusProxyAuthRequired}).URL()
		}, 10 * time.Second},
		{"unreachable", reservedProxyURL, 10 * time.Second},
		{"silent", func(t *testing.T) *url.URL {
			return connectproxy.Start(t, connectproxy.Options{Silent: true}).URL()
		}, 200 * time.Millisecond},
		{"proxy TLS", func(t *testing.T) *url.URL {
			return connectproxy.Start(t, connectproxy.Options{TLS: true}).URL()
		}, 10 * time.Second},
		{"unsupported scheme", func(t *testing.T) *url.URL { return mustURL(t, "socks5://proxy.corp:1080") }, 10 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := withSecret(tt.proxy(t))
			_, err := dialTLS(t, testCtx(t, tt.ctxd), Dialer{Proxy: fixed(u)}, "example.com:443", nil)
			if err == nil {
				t.Fatal("DialTLS succeeded")
			}
			pe := proxyError(t, err)
			for _, text := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), pe.Proxy} {
				if strings.Contains(text, "secret") {
					t.Fatalf("the password appears in %q", text)
				}
			}
			if strings.Contains(pe.Proxy, "u:") || strings.Contains(pe.Proxy, "@") {
				t.Fatalf("Proxy = %q, want host:port only", pe.Proxy)
			}
		})
	}
}

func TestDialTLSThroughAnHTTPSProxy(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{
		TLS:   true,
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	d := Dialer{Proxy: fixed(proxy.URL()), ProxyTLSConfig: &tls.Config{RootCAs: proxy.RootCAs()}}
	conn, err := dialTLS(t, testCtx(t, 10*time.Second), d, "example.com:443", trusting(srv))
	if err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	// The tunnel carries the target's session, not the proxy's.
	if got := conn.ConnectionState().PeerCertificates[0].Raw; !reflect.DeepEqual(got, srv.Certificate().Raw) {
		t.Fatal("the leaf certificate is not the target's")
	}
	want := []connectproxy.Request{{Target: "example.com:443"}}
	if got := proxy.Requests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("proxy received %+v, want %+v", got, want)
	}
}

func TestDialTLSProxyTLSFailure(t *testing.T) {
	tests := []struct {
		name string
		cfg  *tls.Config
	}{
		{"roots that do not trust the proxy", &tls.Config{RootCAs: x509.NewCertPool()}},
		{"system roots", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := connectproxy.Start(t, connectproxy.Options{TLS: true})
			d := Dialer{Proxy: fixed(proxy.URL()), ProxyTLSConfig: tt.cfg}
			_, err := dialTLS(t, testCtx(t, 10*time.Second), d, "example.com:443", nil)
			pe := proxyError(t, err)
			if pe.Step != StepProxyTLS || pe.Proxy != proxy.URL().Host {
				t.Fatalf("Step, Proxy = %q, %q; want %q, %q", pe.Step, pe.Proxy, StepProxyTLS, proxy.URL().Host)
			}
			if want := "proxy: TLS to " + proxy.URL().Host + " failed: "; !strings.HasPrefix(err.Error(), want) {
				t.Fatalf("text = %q, want it to start with %q", err, want)
			}
			var unknown x509.UnknownAuthorityError
			if !errors.As(err, &unknown) {
				t.Fatalf("error = %v, want the x509 cause kept", err)
			}
			if n := len(proxy.Requests()); n != 0 {
				t.Fatalf("a CONNECT was sent over a connection the client distrusted: %d", n)
			}
		})
	}
}

// TestDialTLSTargetFailuresAreNotProxyFailures: once the proxy has accepted the
// CONNECT, a certificate problem is the target's, identical to a direct dial.
func TestDialTLSTargetFailuresAreNotProxyFailures(t *testing.T) {
	srv := newTLSServer(t)
	tests := []struct {
		name   string
		target string
		cfg    *tls.Config
		check  func(error) bool
	}{
		{"certificate not trusted", "example.com:443", nil, func(err error) bool {
			var e x509.UnknownAuthorityError
			return errors.As(err, &e)
		}},
		{"certificate not trusted, empty roots", "example.com:443", &tls.Config{RootCAs: x509.NewCertPool()}, func(err error) bool {
			var e x509.UnknownAuthorityError
			return errors.As(err, &e)
		}},
		{"name mismatch", "wrong.example.org:443", trusting(srv), func(err error) bool {
			var e x509.HostnameError
			return errors.As(err, &e)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := connectproxy.Start(t, connectproxy.Options{
				Route: func(string) string { return srv.Listener.Addr().String() },
			})
			conn, err := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: fixed(proxy.URL())}, tt.target, tt.cfg)
			if conn != nil || err == nil {
				t.Fatalf("DialTLS = %v, %v; want an error and no connection", conn, err)
			}
			var pe *Error
			if errors.As(err, &pe) {
				t.Fatalf("a target failure is wrapped as a proxy failure: %v", err)
			}
			if !tt.check(err) {
				t.Fatalf("error = %T %v, not the expected x509 error", err, err)
			}
			if n := len(proxy.Requests()); n != 1 {
				t.Fatalf("proxy received %d CONNECTs, want 1: the proxy step succeeded", n)
			}
		})
	}
}

// TestDialTLSTargetFailureMatchesADirectDial: the same untrusted certificate
// gives the same kind of error with and without a proxy.
func TestDialTLSTargetFailureMatchesADirectDial(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	_, viaProxy := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: fixed(proxy.URL())}, "example.com:443", nil)
	_, noProxy := dialTLS(t, testCtx(t, 10*time.Second), Dialer{Proxy: direct}, srv.Listener.Addr().String(), nil)
	if viaProxy == nil || noProxy == nil {
		t.Fatalf("errors = %v, %v; want both to fail", viaProxy, noProxy)
	}
	var a, b x509.UnknownAuthorityError
	if !errors.As(viaProxy, &a) || !errors.As(noProxy, &b) {
		t.Fatalf("via proxy: %T %v, direct: %T %v; want the same x509 error", viaProxy, viaProxy, noProxy, noProxy)
	}
}

func TestDialTLSUnsupportedScheme(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"socks5://proxy.corp:1080", "proxy: scheme socks5 is not supported by the SSL check"},
		{"socks5h://proxy.corp:1080", "proxy: scheme socks5h is not supported by the SSL check"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			var tr tracker
			d := Dialer{Proxy: fixed(mustURL(t, tt.raw)), DialContext: tr.DialContext}
			_, err := dialTLS(t, testCtx(t, 10*time.Second), d, "example.com:443", nil)
			pe := proxyError(t, err)
			if pe.Step != StepScheme || pe.StatusCode != 0 {
				t.Fatalf("Step, StatusCode = %q, %d; want %q, 0", pe.Step, pe.StatusCode, StepScheme)
			}
			if err.Error() != tt.want {
				t.Fatalf("text = %q, want %q", err, tt.want)
			}
			if got := tr.dialed(); len(got) != 0 {
				t.Fatalf("dialed %v: an unsupported scheme must not touch the network", got)
			}
		})
	}
}

func TestDialTLSSelectorError(t *testing.T) {
	boom := errors.New("bad HTTPS_PROXY")
	var tr tracker
	d := Dialer{
		Proxy:       func(*url.URL) (*url.URL, error) { return nil, boom },
		DialContext: tr.DialContext,
	}
	_, err := dialTLS(t, testCtx(t, 10*time.Second), d, "example.com:443", nil)
	pe := proxyError(t, err)
	if pe.Step != StepSelect {
		t.Fatalf("Step = %q, want %q", pe.Step, StepSelect)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the selector's error kept", err)
	}
	if want := "proxy: invalid proxy setting: " + boom.Error(); err.Error() != want {
		t.Fatalf("text = %q, want %q", err, want)
	}
	if got := tr.dialed(); len(got) != 0 {
		t.Fatalf("dialed %v: an invalid setting must not touch the network", got)
	}
}

// TestDialTLSClosesTheConnectionItReturns: closing the *tls.Conn closes the
// connection under it, the tunnel, so a check leaks no socket.
func TestDialTLSClosesTheConnectionItReturns(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	var tr tracker
	d := Dialer{Proxy: fixed(proxy.URL()), DialContext: tr.DialContext}
	conn, err := d.DialTLS(testCtx(t, 10*time.Second), "example.com:443", trusting(srv))
	if err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Logf("Close: %v", err)
	}
	opened := tr.opened()
	if len(opened) != 1 {
		t.Fatalf("%d connections opened, want 1 (one per check)", len(opened))
	}
	if !opened[0].closed.Load() {
		t.Fatal("closing the TLS connection left the tunnel open")
	}
}
