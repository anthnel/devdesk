package status

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/netproxy"
	"github.com/anthnel/devdesk/internal/testutil/connectproxy"
)

// DEVDESK-581: the SSL monitor opens its own TLS connection, so it must follow
// the same proxy path as the HTTPS monitor. These tests drive the package
// variables certDialer and certRootCAs; none of them runs in parallel.
//
// Everything that targets "example.com:443" is redirected by the proxy (or by
// an injected DialContext) to a local test server; nothing reaches the network.

const (
	// testIssuer is the CommonName of the generated certificate (self-signed,
	// so also its issuer).
	testIssuer = "DevDesk Test"

	// envHelper makes the test binary run TestSSLEnvironmentHelper for real.
	envHelper = "STATUS_TEST_SSL_HELPER"
	// envTarget is the target the helper fetches a chain from.
	envTarget = "STATUS_TEST_SSL_TARGET"
	// envServer is the address the helper's DialContext gives the target when
	// it is dialed directly.
	envServer = "STATUS_TEST_SSL_SERVER"
	// envCert is the server certificate, DER in base64: the helper's roots.
	envCert = "STATUS_TEST_SSL_CERT"

	// resultPrefix marks the helper's answer among the test binary's output.
	resultPrefix = "RESULT "
)

// newCertServer starts a TLS server whose self-signed certificate covers every
// name and address the tests use as a target, and returns it with a pool that
// trusts it. The certificate is valid for years, so a check reads it as OK.
func newCertServer(t *testing.T) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(581),
		Subject:               pkix.Name{CommonName: testIssuer},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"example.com", "a.corp.test", "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1"), net.ParseIP("10.1.2.3")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "reached")
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // failed handshakes are on purpose
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv, pool
}

// fixed is a Selector that always answers u.
func fixed(u *url.URL) netproxy.Selector {
	return func(*url.URL) (*url.URL, error) { return u, nil }
}

// direct is a Selector that never uses a proxy.
func direct(*url.URL) (*url.URL, error) { return nil, nil }

// useCertDialer swaps certDialer and certRootCAs for the test and restores
// them when it ends.
func useCertDialer(t *testing.T, d netproxy.Dialer, roots *x509.CertPool) {
	t.Helper()
	oldDialer, oldRoots := certDialer, certRootCAs
	certDialer, certRootCAs = d, roots
	t.Cleanup(func() { certDialer, certRootCAs = oldDialer, oldRoots })
}

// routeTo is a Dialer.DialContext that sends the connection for any of names
// to addr and leaves other addresses alone.
func routeTo(addr string, names ...string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, a string) (net.Conn, error) {
		for _, n := range names {
			if a == n {
				a = addr
			}
		}
		return (&net.Dialer{}).DialContext(ctx, network, a)
	}
}

func sslComponent(target string) config.ComponentConfig {
	return config.ComponentConfig{Name: "cert", Type: "ssl", Target: target}
}

func testContext(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

func leafFingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// captureTransport remembers the chain the server presented on each response,
// which is what the HTTPS monitor saw.
type captureTransport struct {
	base http.RoundTripper

	mu     sync.Mutex
	chains [][]*x509.Certificate
}

func (c *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.base.RoundTrip(req)
	if resp != nil && resp.TLS != nil {
		c.mu.Lock()
		c.chains = append(c.chains, resp.TLS.PeerCertificates)
		c.mu.Unlock()
	}
	return resp, err
}

func (c *captureTransport) seen() [][]*x509.Certificate {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]*x509.Certificate(nil), c.chains...)
}

func TestSSLDefaultsFollowTheEnvironment(t *testing.T) {
	// The zero Dialer reads the proxy variables, and nothing pins the roots:
	// without a proxy variable the monitor behaves as it always did.
	if certDialer.Proxy != nil || certDialer.DialContext != nil || certDialer.ProxyTLSConfig != nil {
		t.Errorf("certDialer = %+v, want the zero netproxy.Dialer", certDialer)
	}
	if certRootCAs != nil {
		t.Error("certRootCAs is set, want nil (system roots)")
	}
}

func TestSSLCheckerThroughProxyMatchesTheHTTPSMonitor(t *testing.T) {
	srv, pool := newCertServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	sel := fixed(proxy.URL())
	useCertDialer(t, netproxy.Dialer{Proxy: sel}, pool)

	// The HTTPS monitor, through the same proxy and the same trust.
	tr := netproxy.NewTransport(sel)
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	t.Cleanup(tr.CloseIdleConnections)
	capture := &captureTransport{base: tr}
	httpChecker := NewHTTPChecker(5 * time.Second)
	httpChecker.client.Transport = capture

	ctx := testContext(t, 20*time.Second)
	httpRes := httpChecker.Check(ctx, config.ComponentConfig{Name: "web", Type: "https", Target: "example.com"})
	if httpRes.Status != StatusOK {
		t.Fatalf("HTTPS monitor = %s (%s), want OK", httpRes.Status, httpRes.Error)
	}
	chains := capture.seen()
	if len(chains) != 1 || len(chains[0]) == 0 {
		t.Fatalf("the HTTPS monitor saw %d chains, want 1 with a leaf", len(chains))
	}
	leaf := chains[0][0]

	res := NewSSLChecker(5*time.Second).Check(ctx, sslComponent("example.com"))
	if res.Status != StatusOK {
		t.Fatalf("SSL monitor = %s (%s), want OK", res.Status, res.Error)
	}
	if res.ErrorStage != "" {
		t.Errorf("ErrorStage = %q on a success, want empty", res.ErrorStage)
	}
	if res.SSLExpires == nil || !res.SSLExpires.Equal(leaf.NotAfter) {
		t.Errorf("SSLExpires = %v, want the HTTPS monitor's %v", res.SSLExpires, leaf.NotAfter)
	}
	if res.SSLIssuer != leaf.Issuer.CommonName || res.SSLIssuer != testIssuer {
		t.Errorf("SSLIssuer = %q, want %q (the HTTPS monitor's %q)", res.SSLIssuer, testIssuer, leaf.Issuer.CommonName)
	}
	wantDays := int(time.Until(leaf.NotAfter).Hours() / 24)
	if res.SSLDaysLeft == nil || *res.SSLDaysLeft < wantDays-1 || *res.SSLDaysLeft > wantDays+1 {
		t.Errorf("SSLDaysLeft = %v, want about %d", res.SSLDaysLeft, wantDays)
	}

	// Both monitors went through the proxy, to the same target.
	reqs := proxy.Requests()
	if len(reqs) != 2 {
		t.Fatalf("proxy received %d CONNECTs, want 2 (one per monitor): %+v", len(reqs), reqs)
	}
	for _, r := range reqs {
		if r.Target != "example.com:443" {
			t.Errorf("CONNECT target = %q, want example.com:443", r.Target)
		}
	}
}

func TestSSLFetchChainThroughProxyReturnsTheServerChain(t *testing.T) {
	srv, pool := newCertServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	useCertDialer(t, netproxy.Dialer{Proxy: fixed(proxy.URL())}, pool)

	chain, err := FetchCertificateChainContext(testContext(t, 20*time.Second), "example.com", 5*time.Second)
	if err != nil {
		t.Fatalf("FetchCertificateChainContext: %v", err)
	}
	if len(chain) == 0 || leafFingerprint(chain[0]) != leafFingerprint(srv.Certificate()) {
		t.Fatalf("chain = %d certificates, leaf not the server's", len(chain))
	}
	reqs := proxy.Requests()
	if len(reqs) != 1 || reqs[0].Target != "example.com:443" {
		t.Fatalf("CONNECTs = %+v, want one to example.com:443 (a bare target defaults to :443)", reqs)
	}
}

func TestSSLCheckerProxyRefusal(t *testing.T) {
	tests := []struct {
		name string
		code int
		want string
	}{
		{"forbidden", http.StatusForbidden, "proxy: CONNECT refused (403"},
		{"proxy authentication required", http.StatusProxyAuthRequired, "proxy: CONNECT refused (407"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, pool := newCertServer(t)
			proxy := connectproxy.Start(t, connectproxy.Options{Refuse: tt.code})
			useCertDialer(t, netproxy.Dialer{Proxy: fixed(proxy.URL())}, pool)

			res := NewSSLChecker(5*time.Second).Check(testContext(t, 20*time.Second), sslComponent("example.com"))
			if res.Status != StatusDown {
				t.Errorf("Status = %s, want %s", res.Status, StatusDown)
			}
			if res.ErrorStage != StageProxy {
				t.Errorf("ErrorStage = %q, want %q", res.ErrorStage, StageProxy)
			}
			if !strings.HasPrefix(res.Error, tt.want) {
				t.Errorf("Error = %q, want a prefix %q", res.Error, tt.want)
			}
			if res.SSLExpires != nil || res.SSLDaysLeft != nil || res.SSLIssuer != "" {
				t.Errorf("SSL fields set on a failure: %v %v %q", res.SSLExpires, res.SSLDaysLeft, res.SSLIssuer)
			}
		})
	}
}

func TestSSLCheckerCertificateErrorIsNotAProxyStage(t *testing.T) {
	srv, pool := newCertServer(t)
	real := srv.Listener.Addr().String()
	proxy := connectproxy.Start(t, connectproxy.Options{Route: func(string) string { return real }})

	tests := []struct {
		name   string
		dialer netproxy.Dialer
		roots  *x509.CertPool
		target string
	}{
		// The proxy tunnel works; the certificate is not trusted.
		{"untrusted through the proxy", netproxy.Dialer{Proxy: fixed(proxy.URL())}, nil, "example.com"},
		{"untrusted direct", netproxy.Dialer{Proxy: direct, DialContext: routeTo(real, "example.com:443")}, nil, "example.com"},
		// The root is trusted; the name is not in the certificate: still verified.
		{"wrong name through the proxy", netproxy.Dialer{Proxy: fixed(proxy.URL())}, pool, "other.test"},
		{"wrong name direct", netproxy.Dialer{Proxy: direct, DialContext: routeTo(real, "other.test:443")}, pool, "other.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useCertDialer(t, tt.dialer, tt.roots)

			res := NewSSLChecker(5*time.Second).Check(testContext(t, 20*time.Second), sslComponent(tt.target))
			if res.Status != StatusDown {
				t.Errorf("Status = %s, want %s", res.Status, StatusDown)
			}
			if res.ErrorStage != "" {
				t.Errorf("ErrorStage = %q, want empty: the proxy did its job", res.ErrorStage)
			}
			if !strings.Contains(res.Error, "x509") {
				t.Errorf("Error = %q, want an x509 verification error", res.Error)
			}
			if strings.HasPrefix(res.Error, "proxy:") {
				t.Errorf("Error = %q must not read as a proxy failure", res.Error)
			}
			if res.SSLExpires != nil || res.SSLIssuer != "" {
				t.Errorf("an unverified chain must not fill the SSL fields: %v %q", res.SSLExpires, res.SSLIssuer)
			}
		})
	}
}

func TestSSLFetchChainContextHonoursItsDeadline(t *testing.T) {
	proxy := connectproxy.Start(t, connectproxy.Options{Silent: true})
	useCertDialer(t, netproxy.Dialer{Proxy: fixed(proxy.URL())}, nil)

	tests := []struct {
		name    string
		ctx     func(t *testing.T) context.Context
		timeout time.Duration
	}{
		{"timeout argument", func(t *testing.T) context.Context { return testContext(t, time.Minute) }, 300 * time.Millisecond},
		{"context deadline", func(t *testing.T) context.Context { return testContext(t, 300*time.Millisecond) }, time.Minute},
		{"context cancelled", func(t *testing.T) context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(300*time.Millisecond, cancel)
			t.Cleanup(cancel)
			return ctx
		}, time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			chain, err := FetchCertificateChainContext(tt.ctx(t), "example.com:443", tt.timeout)
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Errorf("returned after %v, want about 300ms", elapsed)
			}
			if err == nil || len(chain) != 0 {
				t.Fatalf("got %d certificates and err %v, want an error", len(chain), err)
			}
			var pe *netproxy.Error
			if !errors.As(err, &pe) {
				t.Fatalf("error = %T %v, want a *netproxy.Error", err, err)
			}
			if !strings.Contains(err.Error(), proxy.URL().Host) {
				t.Errorf("error = %q, want it to name the proxy %s", err, proxy.URL().Host)
			}
		})
	}
}

func TestSSLCheckerHonoursItsTimeoutThroughTheProxy(t *testing.T) {
	proxy := connectproxy.Start(t, connectproxy.Options{Silent: true})
	useCertDialer(t, netproxy.Dialer{Proxy: fixed(proxy.URL())}, nil)

	start := time.Now()
	res := NewSSLChecker(300*time.Millisecond).Check(testContext(t, time.Minute), sslComponent("example.com"))
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Check took %v, want about 300ms", elapsed)
	}
	if res.Status != StatusDown || res.ErrorStage != StageProxy {
		t.Errorf("Status, ErrorStage = %s, %q; want %s, %q (error %q)", res.Status, res.ErrorStage, StatusDown, StageProxy, res.Error)
	}
	if !strings.Contains(res.Error, proxy.URL().Host) {
		t.Errorf("Error = %q, want it to name the proxy %s", res.Error, proxy.URL().Host)
	}
}

// TestSSLEnvironmentHelper is the child half of
// TestSSLFetchChainFollowsTheProxyEnvironment: it does nothing in a normal run.
// http.ProxyFromEnvironment reads the proxy variables once per process, so
// t.Setenv cannot steer it: the parent runs the test binary again with the
// variables it wants. The child keeps the zero Dialer's proxy choice (the
// environment) and only redirects the direct dial of the target to the test
// server; it prints which addresses it dialed and which chain it obtained.
func TestSSLEnvironmentHelper(t *testing.T) {
	if os.Getenv(envHelper) != "1" {
		t.Skip("child of TestSSLFetchChainFollowsTheProxyEnvironment")
	}
	der, err := base64.StdEncoding.DecodeString(os.Getenv(envCert))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	var (
		mu     sync.Mutex
		dialed []string
	)
	target := os.Getenv(envTarget)
	server := os.Getenv(envServer)
	useCertDialer(t, netproxy.Dialer{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == target {
			addr = server
		}
		mu.Lock()
		dialed = append(dialed, addr)
		mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}, pool)

	chain, err := FetchCertificateChainContext(testContext(t, 10*time.Second), target, 5*time.Second)
	mu.Lock()
	defer mu.Unlock()
	if err != nil {
		fmt.Println(resultPrefix + "error: " + err.Error())
		return
	}
	if len(chain) == 0 {
		fmt.Println(resultPrefix + "error: empty chain")
		return
	}
	fmt.Printf("%schain %s dialed %s\n", resultPrefix, leafFingerprint(chain[0]), strings.Join(dialed, ","))
}

// childEnvVars are removed from the parent's environment before the child
// starts, so a developer's own proxy settings never leak into a case.
var childEnvVars = []string{
	"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy",
	"NO_PROXY", "no_proxy", "REQUEST_METHOD",
	envHelper, envTarget, envServer, envCert,
}

// runChild runs the test binary on the helper test, with the proxy variables
// of the parent removed, extra added, then exactly vars, and returns the RESULT
// line the child printed.
func runChild(t *testing.T, extra, vars map[string]string) string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, v := range childEnvVars {
			if strings.EqualFold(name, v) {
				drop = true
			}
		}
		if !drop {
			env = append(env, kv)
		}
	}
	// GORACE: under -race each child would otherwise sleep a second at exit.
	env = append(env, "GORACE=atexit_sleep_ms=0")
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSSLEnvironmentHelper$", "-test.count=1")
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the helper: %v\n%s", err, out.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), resultPrefix); ok {
			return rest
		}
	}
	t.Fatalf("the helper printed no %q line:\n%s", resultPrefix, out.String())
	return ""
}

// TestSSLFetchChainFollowsTheProxyEnvironment runs the SSL monitor's fetch in a
// child process with real HTTPS_PROXY and NO_PROXY values, as a user would set
// them: a target that is not excluded travels through the proxy (one CONNECT),
// an excluded one is dialed directly (no CONNECT) and yields the same chain.
func TestSSLFetchChainFollowsTheProxyEnvironment(t *testing.T) {
	srv, _ := newCertServer(t)
	real := srv.Listener.Addr().String()
	_, realPort, err := net.SplitHostPort(real)
	if err != nil {
		t.Fatal(err)
	}
	proxy := connectproxy.Start(t, connectproxy.Options{Route: func(string) string { return real }})
	proxyHost := proxy.URL().Host
	wantChain := leafFingerprint(srv.Certificate())
	httpsProxy := map[string]string{"HTTPS_PROXY": proxy.URL().String()}
	withNoProxy := func(v string) map[string]string {
		return map[string]string{"HTTPS_PROXY": proxy.URL().String(), "NO_PROXY": v}
	}

	tests := []struct {
		name         string
		target       string
		vars         map[string]string
		wantDialed   string // the address the child's DialContext was asked for
		wantConnects int
	}{
		{"not excluded goes through the proxy", "a.corp.test:443", httpsProxy, proxyHost, 1},
		{"NO_PROXY on another host does not exclude", "a.corp.test:443", withNoProxy("b.corp.test"), proxyHost, 1},
		{"no proxy variable is direct", "a.corp.test:443", nil, real, 0},
		{"NO_PROXY exact name", "a.corp.test:443", withNoProxy("a.corp.test"), real, 0},
		{"NO_PROXY domain suffix", "a.corp.test:443", withNoProxy("corp.test"), real, 0},
		{"NO_PROXY dotted suffix", "a.corp.test:443", withNoProxy(".corp.test"), real, 0},
		{"NO_PROXY CIDR", "10.1.2.3:443", withNoProxy("10.0.0.0/8"), real, 0},
		// Go never proxies localhost nor a loopback address, whatever NO_PROXY says.
		{"localhost is never proxied", "localhost:" + realPort, httpsProxy, real, 0},
		{"loopback address is never proxied", "127.0.0.1:" + realPort, httpsProxy, real, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extra := map[string]string{
				envHelper: "1",
				envTarget: tt.target,
				envServer: real,
				envCert:   base64.StdEncoding.EncodeToString(srv.Certificate().Raw),
			}
			before := len(proxy.Requests())
			got := runChild(t, extra, tt.vars)
			if want := "chain " + wantChain + " dialed " + tt.wantDialed; got != want {
				t.Fatalf("child with %v = %q, want %q", tt.vars, got, want)
			}
			reqs := proxy.Requests()[before:]
			if len(reqs) != tt.wantConnects {
				t.Fatalf("proxy received %d CONNECTs, want %d: %+v", len(reqs), tt.wantConnects, reqs)
			}
			if tt.wantConnects == 1 && reqs[0].Target != tt.target {
				t.Fatalf("CONNECT target = %q, want %q", reqs[0].Target, tt.target)
			}
		})
	}
}
