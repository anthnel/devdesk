package netproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/testutil/connectproxy"
)

const (
	// helperEnv makes the test binary run TestFromEnvironmentHelper for real.
	helperEnv = "NETPROXY_TEST_HELPER"
	// helperTarget is the URL the helper asks FromEnvironment about.
	helperTarget = "NETPROXY_TEST_TARGET"
	// resultPrefix marks the helper's answer among the test binary's output.
	resultPrefix = "RESULT "
)

func mustURL(t testing.TB, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	return u
}

// fixed is a Selector that always picks proxy.
func fixed(proxy *url.URL) Selector {
	return func(*url.URL) (*url.URL, error) { return proxy, nil }
}

// direct is a Selector that never picks a proxy.
func direct(*url.URL) (*url.URL, error) { return nil, nil }

func sameURL(a, b *url.URL) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.String() == b.String()
}

func TestSelectorForRequest(t *testing.T) {
	proxy := mustURL(t, "http://proxy.corp:3128")
	boom := errors.New("invalid proxy setting")
	tests := []struct {
		name    string
		sel     Selector
		want    *url.URL
		wantErr error
	}{
		{"a proxy", fixed(proxy), proxy, nil},
		{"direct", direct, nil, nil},
		{"an error", func(*url.URL) (*url.URL, error) { return nil, boom }, nil, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.sel.ForRequest(&http.Request{URL: mustURL(t, "https://example.com/")})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if !sameURL(got, tt.want) {
				t.Fatalf("proxy = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectorForRequestPassesTheRequestURL(t *testing.T) {
	var seen *url.URL
	sel := Selector(func(u *url.URL) (*url.URL, error) { seen = u; return nil, nil })
	req := &http.Request{URL: mustURL(t, "https://a.corp.test:8443/path?q=1")}
	if _, err := sel.ForRequest(req); err != nil {
		t.Fatal(err)
	}
	if seen != req.URL {
		t.Fatalf("selector saw %v, want the request's own URL %v", seen, req.URL)
	}
}

func TestNewTransportProxyFollowsTheSelector(t *testing.T) {
	proxy := mustURL(t, "http://proxy.corp:3128")
	tests := []struct {
		name string
		sel  Selector
		want *url.URL
	}{
		{"a proxy", fixed(proxy), proxy},
		{"direct", direct, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTransport(tt.sel)
			if tr.Proxy == nil {
				t.Fatal("Transport.Proxy is nil")
			}
			got, err := tr.Proxy(&http.Request{URL: mustURL(t, "https://example.com/")})
			if err != nil {
				t.Fatalf("Proxy: %v", err)
			}
			if !sameURL(got, tt.want) {
				t.Fatalf("Proxy = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTransportProxyReportsTheSelectorError(t *testing.T) {
	boom := errors.New("invalid proxy setting")
	tr := NewTransport(func(*url.URL) (*url.URL, error) { return nil, boom })
	if tr.Proxy == nil {
		t.Fatal("Transport.Proxy is nil")
	}
	if _, err := tr.Proxy(&http.Request{URL: mustURL(t, "https://example.com/")}); !errors.Is(err, boom) {
		t.Fatalf("Proxy error = %v, want %v", err, boom)
	}
}

// TestNewTransportKeepsTheDefaultTransportSettings checks every exported field
// but Proxy (and the two the standard library fills lazily) against
// http.DefaultTransport, so the HTTPS monitor's timeouts and limits carry over.
func TestNewTransportKeepsTheDefaultTransportSettings(t *testing.T) {
	skip := map[string]bool{"Proxy": true, "TLSClientConfig": true, "TLSNextProto": true}
	def := reflect.ValueOf(http.DefaultTransport.(*http.Transport).Clone()).Elem()
	got := reflect.ValueOf(NewTransport(direct)).Elem()
	typ := got.Type()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() || skip[f.Name] {
			continue
		}
		g, d := got.Field(i), def.Field(i)
		if g.Kind() == reflect.Func {
			if g.IsNil() != d.IsNil() {
				t.Errorf("%s: nil = %v, DefaultTransport nil = %v", f.Name, g.IsNil(), d.IsNil())
			}
			continue
		}
		if !reflect.DeepEqual(g.Interface(), d.Interface()) {
			t.Errorf("%s = %v, DefaultTransport has %v", f.Name, g.Interface(), d.Interface())
		}
	}
	if got.FieldByName("DialContext").IsNil() {
		t.Error("DialContext is nil: connections would not use the default dialer")
	}
}

func TestNewTransportIsACopyNotDefaultTransport(t *testing.T) {
	a, b := NewTransport(direct), NewTransport(direct)
	if a == http.DefaultTransport || a == b {
		t.Fatal("NewTransport returned a shared transport")
	}
	before := http.DefaultTransport.(*http.Transport).MaxIdleConns
	a.MaxIdleConns = before + 7
	if http.DefaultTransport.(*http.Transport).MaxIdleConns != before {
		t.Fatal("editing the new transport changed http.DefaultTransport")
	}
	if b.MaxIdleConns != before {
		t.Fatal("editing one transport changed another")
	}
	if p := http.DefaultTransport.(*http.Transport).Proxy; p == nil {
		t.Fatal("http.DefaultTransport lost its Proxy")
	}
}

// httpsGet fetches rawURL through tr, trusting only srv's certificate.
func httpsGet(t *testing.T, tr *http.Transport, srv *httptest.Server, rawURL string) (*http.Response, string) {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	t.Cleanup(tr.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func newTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "reached")
	}))
	// Handshakes that fail on purpose would otherwise log to the test output.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestNewTransportTunnelsThroughTheSelectedProxy(t *testing.T) {
	srv := newTLSServer(t)
	// example.com is a name in the httptest certificate; the proxy redirects it
	// to the real listener.
	proxy := connectproxy.Start(t, connectproxy.Options{
		Route: func(string) string { return srv.Listener.Addr().String() },
	})
	resp, body := httpsGet(t, NewTransport(fixed(proxy.URL())), srv, "https://example.com/")
	if resp.StatusCode != http.StatusOK || body != "reached" {
		t.Fatalf("answer = %d %q, want 200 %q", resp.StatusCode, body, "reached")
	}
	want := []connectproxy.Request{{Target: "example.com:443"}}
	if got := proxy.Requests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("proxy received %+v, want exactly %+v", got, want)
	}
}

func TestNewTransportGoesDirectWhenTheSelectorSaysSo(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{})
	// The proxy is really handed to the selector, which answers "direct" for
	// this target, like a NO_PROXY match would: Requests() being empty then
	// means the transport obeyed the selector, not that no proxy was given.
	var asked []string
	sel := func(u *url.URL) (*url.URL, error) {
		asked = append(asked, u.Host)
		if u.Host == srv.Listener.Addr().String() {
			return nil, nil
		}
		return proxy.URL(), nil
	}
	resp, body := httpsGet(t, NewTransport(sel), srv, srv.URL)
	if resp.StatusCode != http.StatusOK || body != "reached" {
		t.Fatalf("answer = %d %q, want 200 %q", resp.StatusCode, body, "reached")
	}
	if len(asked) == 0 {
		t.Fatal("the selector was never consulted")
	}
	if got := proxy.Requests(); len(got) != 0 {
		t.Fatalf("a direct connection reached the proxy: %+v", got)
	}
}

func TestNewTransportSurfacesAProxyRefusal(t *testing.T) {
	srv := newTLSServer(t)
	proxy := connectproxy.Start(t, connectproxy.Options{Refuse: http.StatusForbidden})
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tr := NewTransport(fixed(proxy.URL()))
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	t.Cleanup(tr.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/", nil)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("GET through a refusing proxy succeeded")
	}
	// net/http reports the proxy's status text, so the refusal is visible in the error.
	if !strings.Contains(err.Error(), "Forbidden") {
		t.Fatalf("error = %q, want it to carry the proxy's 403 Forbidden", err)
	}
	if got := proxy.Requests(); len(got) != 1 {
		t.Fatalf("proxy received %d CONNECTs, want 1", len(got))
	}
}

// TestFromEnvironmentIsHTTPProxyFromEnvironment holds in any environment: both
// calls read the same per-process cache.
func TestFromEnvironmentIsHTTPProxyFromEnvironment(t *testing.T) {
	tests := []string{
		"https://example.com:443",
		"https://a.corp.test:8443",
		"https://localhost:8443",
		"https://127.0.0.1:8443",
		"https://10.1.2.3:443",
		"http://example.com:80",
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			u := mustURL(t, raw)
			want, wantErr := http.ProxyFromEnvironment(&http.Request{URL: u})
			got, err := FromEnvironment(u)
			if (err == nil) != (wantErr == nil) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			if !sameURL(got, want) {
				t.Fatalf("proxy = %v, want %v", got, want)
			}
		})
	}
}

// TestFromEnvironmentHelper is the child half of TestFromEnvironmentFollowsTheEnvironment.
// It does nothing in a normal run. http.ProxyFromEnvironment reads the proxy
// variables once per process, so t.Setenv cannot steer it: the parent runs the
// test binary again with the variables it wants.
func TestFromEnvironmentHelper(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("child of TestFromEnvironmentFollowsTheEnvironment")
	}
	proxy, err := FromEnvironment(mustURL(t, os.Getenv(helperTarget)))
	switch {
	case err != nil:
		fmt.Println(resultPrefix + "error: " + err.Error())
	case proxy == nil:
		fmt.Println(resultPrefix + "direct")
	default:
		fmt.Println(resultPrefix + proxy.String())
	}
}

var proxyVars = []string{
	"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy",
	"NO_PROXY", "no_proxy", "REQUEST_METHOD", helperEnv, helperTarget,
	dialHelperEnv, dialHelperServer, dialHelperCert,
}

// runHelper runs the child with exactly the given proxy variables and returns
// what it answered.
func runHelper(t *testing.T, target string, vars map[string]string) string {
	t.Helper()
	return runChild(t, "TestFromEnvironmentHelper", map[string]string{helperEnv: "1", helperTarget: target}, vars)
}

// runChild runs the test binary on the single test run, with the proxy
// variables of the parent removed, extra added, then exactly vars, and returns
// the RESULT line the child printed.
func runChild(t *testing.T, run string, extra, vars map[string]string) string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, v := range proxyVars {
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
	cmd := exec.Command(os.Args[0], "-test.run=^"+run+"$", "-test.count=1")
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

func TestFromEnvironmentFollowsTheEnvironment(t *testing.T) {
	const proxy = "http://proxy.corp:3128"
	tests := []struct {
		name   string
		target string
		vars   map[string]string
		want   string
	}{
		{"no variable", "https://example.com:443", nil, "direct"},
		{"HTTPS_PROXY", "https://a.corp.test:443", map[string]string{"HTTPS_PROXY": proxy}, proxy},
		{"https_proxy in lower case", "https://a.corp.test:443", map[string]string{"https_proxy": proxy}, proxy},
		{"HTTP_PROXY does not carry https", "https://a.corp.test:443", map[string]string{"HTTP_PROXY": proxy}, "direct"},
		{"userinfo is kept for the dialer", "https://a.corp.test:443",
			map[string]string{"HTTPS_PROXY": "http://u:secret@proxy.corp:3128"}, "http://u:secret@proxy.corp:3128"},
		{"https proxy scheme", "https://a.corp.test:443", map[string]string{"HTTPS_PROXY": "https://proxy.corp:3129"}, "https://proxy.corp:3129"},
		{"NO_PROXY exact name", "https://a.corp.test:443", map[string]string{"HTTPS_PROXY": proxy, "NO_PROXY": "a.corp.test"}, "direct"},
		{"no_proxy in lower case", "https://a.corp.test:443", map[string]string{"HTTPS_PROXY": proxy, "no_proxy": "a.corp.test"}, "direct"},
		{"NO_PROXY domain covers a sub-domain", "https://a.corp.test:443", map[string]string{"HTTPS_PROXY": proxy, "NO_PROXY": "corp.test"}, "direct"},
		{"NO_PROXY leading dot covers a sub-domain", "https://a.corp.test:443", map[string]string{"HTTPS_PROXY": proxy, "NO_PROXY": ".corp.test"}, "direct"},
		{"NO_PROXY leading dot leaves the domain itself", "https://corp.test:443", map[string]string{"HTTPS_PROXY": proxy, "NO_PROXY": ".corp.test"}, proxy},
		{"NO_PROXY other name", "https://b.corp.test:443", map[string]string{"HTTPS_PROXY": proxy, "NO_PROXY": "a.corp.test"}, proxy},
		{"NO_PROXY CIDR", "https://10.1.2.3:443", map[string]string{"HTTPS_PROXY": proxy, "NO_PROXY": "10.0.0.0/8"}, "direct"},
		{"NO_PROXY CIDR leaves other addresses", "https://192.168.1.1:443", map[string]string{"HTTPS_PROXY": proxy, "NO_PROXY": "10.0.0.0/8"}, proxy},
		{"NO_PROXY star", "https://a.corp.test:443", map[string]string{"HTTPS_PROXY": proxy, "NO_PROXY": "*"}, "direct"},
		{"localhost is never proxied", "https://localhost:8443", map[string]string{"HTTPS_PROXY": proxy}, "direct"},
		{"loopback address is never proxied", "https://127.0.0.1:8443", map[string]string{"HTTPS_PROXY": proxy}, "direct"},
		{"*.localhost is proxied", "https://foo.localhost:8443", map[string]string{"HTTPS_PROXY": proxy}, proxy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runHelper(t, tt.target, tt.vars); got != tt.want {
				t.Fatalf("FromEnvironment(%s) with %v = %q, want %q", tt.target, tt.vars, got, tt.want)
			}
		})
	}
}

// TestNilSelectorIsFromEnvironment pins the documented default: a nil Selector
// decides like FromEnvironment, in ForRequest and in NewTransport.
func TestNilSelectorIsFromEnvironment(t *testing.T) {
	u := mustURL(t, "https://a.corp.test:443")
	want, wantErr := FromEnvironment(u)
	req := &http.Request{URL: u}
	var sel Selector
	check := func(name string, got *url.URL, err error) {
		t.Helper()
		if (err == nil) != (wantErr == nil) || !sameURL(got, want) {
			t.Errorf("%s = %v, %v; want %v, %v", name, got, err, want, wantErr)
		}
	}
	got, err := sel.ForRequest(req)
	check("ForRequest", got, err)
	tr := NewTransport(nil)
	if tr.Proxy == nil {
		t.Fatal("Transport.Proxy is nil")
	}
	got, err = tr.Proxy(req)
	check("NewTransport(nil).Proxy", got, err)
}
