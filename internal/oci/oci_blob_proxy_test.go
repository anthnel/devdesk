package oci

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/anthnel/devdesk/internal/netproxy"
)

// DEVDESK-412 step 4: a local proxy (cntlm, a corporate proxy on 127.0.0.1) is
// the user's own configuration, so the blob transport's dial control lets
// through the address of the proxy it was told to use for a request, and still
// refuses every other loopback or link-local address. The proxy is injected
// through newBlobTransport: netproxy.FromEnvironment reads the environment once
// per process, so t.Setenv cannot steer it.

func fixedProxy(raw string) netproxy.Selector {
	u, err := url.Parse(raw)
	return func(*url.URL) (*url.URL, error) { return u, err }
}

func noProxy(*url.URL) (*url.URL, error) { return nil, nil }

// listenLoopback opens a listener that stays open for the whole test, so a
// refusal can only come from the client and never from a closed port.
func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// dialAfterProxy asks tr which proxy it uses for a public registry, as net/http
// does before it dials, then dials addr.
func dialAfterProxy(t *testing.T, tr *http.Transport, addr string) error {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://registry.example.com/v2/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Proxy(req); err != nil {
		t.Fatalf("Proxy: %v", err)
	}
	conn, err := tr.DialContext(context.Background(), "tcp", addr)
	if err == nil {
		_ = conn.Close()
	}
	return err
}

// (a) The configured proxy is reachable; another loopback address is not.
func TestBlobDialControlAllowsTheConfiguredProxy(t *testing.T) {
	proxy, other := listenLoopback(t), listenLoopback(t)
	_, proxyPort, _ := net.SplitHostPort(proxy.Addr().String())

	for _, proxyURL := range []string{
		"http://127.0.0.1:" + proxyPort,
		"http://localhost:" + proxyPort,
		"http://LOCALHOST:" + proxyPort,
		"http://user:pass@127.0.0.1:" + proxyPort,
		"https://127.0.0.1:" + proxyPort,
	} {
		t.Run(proxyURL, func(t *testing.T) {
			tr := newBlobTransport(false, fixedProxy(proxyURL))
			if err := dialAfterProxy(t, tr, proxy.Addr().String()); err != nil {
				t.Errorf("dialing the proxy %s: %v", proxyURL, err)
			}
			if err := dialAfterProxy(t, tr, other.Addr().String()); !errors.Is(err, errBlobDialLocal) {
				t.Errorf("dialing another loopback port = %v, want errBlobDialLocal", err)
			}
		})
	}
}

// (b) No proxy for the request (none configured, or NO_PROXY excludes the host):
// the proxy's address is a loopback address like any other.
func TestBlobDialControlRefusesLocalWithoutProxy(t *testing.T) {
	ln := listenLoopback(t)
	tr := newBlobTransport(false, noProxy)
	if err := dialAfterProxy(t, tr, ln.Addr().String()); !errors.Is(err, errBlobDialLocal) {
		t.Errorf("dial without a proxy = %v, want errBlobDialLocal", err)
	}

	// A proxy that is never asked for does not open anything either.
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	untouched := newBlobTransport(false, fixedProxy("http://127.0.0.1:"+port))
	conn, err := untouched.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Error("dial before any Proxy call connected to a loopback address, want a refusal")
	}
}

// A proxy URL that cannot resolve adds nothing, and a proxy on a public address
// does not make the loopback reachable.
func TestBlobDialControlProxyEdgeCases(t *testing.T) {
	ln := listenLoopback(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	for _, proxyURL := range []string{
		"http://proxy.invalid:" + port,
		"http://203.0.113.9:" + port,
	} {
		tr := newBlobTransport(false, fixedProxy(proxyURL))
		if err := dialAfterProxy(t, tr, ln.Addr().String()); !errors.Is(err, errBlobDialLocal) {
			t.Errorf("proxy %s: dial = %v, want errBlobDialLocal", proxyURL, err)
		}
	}
}

func TestProxyPortDefaults(t *testing.T) {
	for raw, want := range map[string]uint16{
		"http://p":          80,
		"https://p":         443,
		"socks5://p":        1080,
		"socks5h://p":       1080,
		"http://p:3128":     3128,
		"http://[::1]:8080": 8080,
	} {
		u, _ := url.Parse(raw)
		if got := proxyPort(u); got != want {
			t.Errorf("proxyPort(%s) = %d, want %d", raw, got, want)
		}
	}
}

// (c) End to end: a registry with a public name reached only through a proxy on
// loopback. The proxy answers for the registry; the manifest and the blob both
// arrive, and the blob transport's control let the proxy connection through.
func TestBlobDownloadThroughLocalProxy(t *testing.T) {
	var proxyHits hitLog
	archive := clientTarGz(t)
	proxy := startServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.record(r)
		if !strings.HasPrefix(r.Host, "registry.test") {
			http.Error(w, "unexpected host", http.StatusBadGateway)
			return
		}
		if isManifestRequest(r) {
			_, _ = fmt.Fprint(w, blobManifest())
			return
		}
		_, _ = w.Write(archive)
	}))

	c := NewClient("http://registry.test:5000", clientTestUser, clientTestPass)
	proxyFor := fixedProxy(proxy.URL)
	plain := http.DefaultTransport.(*http.Transport).Clone()
	plain.Proxy = proxyFor.ForRequest
	c.httpClient.Transport = plain
	c.blobClient.Transport = newBlobTransport(false, proxyFor)

	tpl, err := c.DownloadTemplate("tpl", "v1")

	wantFixtureEntries(t, tpl, err)
	if n := proxyHits.count(); n != 2 {
		t.Errorf("the proxy received %d requests, want 2 (manifest and blob)", n)
	}
}

// resolverStub counts its calls and answers with results, one per call (the last
// one repeats). A result with no addresses is a failure.
type resolverStub struct {
	calls   atomic.Int32
	results [][]netip.Addr
	gate    chan struct{} // when set, every call waits for it to be closed
}

func (r *resolverStub) resolve(string) ([]netip.Addr, error) {
	if r.gate != nil {
		<-r.gate
	}
	i := int(r.calls.Add(1)) - 1
	if i >= len(r.results) {
		i = len(r.results) - 1
	}
	if len(r.results[i]) == 0 {
		return nil, errors.New("no such host")
	}
	return r.results[i], nil
}

// A proxy whose name did not resolve is looked up again by the next request:
// the failure is not remembered.
func TestProxyAddrsRetriesAfterAFailedResolution(t *testing.T) {
	loop := netip.MustParseAddr("127.0.0.1")
	stub := &resolverStub{results: [][]netip.Addr{nil, {loop}}}
	p := newProxyAddrs()
	p.resolve = stub.resolve
	proxy, _ := url.Parse("http://proxy.corp.example:3128")
	want := netip.AddrPortFrom(loop, 3128)

	p.add(proxy)
	if p.has(want) {
		t.Fatal("the failed lookup allowed an address")
	}
	p.add(proxy)
	if !p.has(want) {
		t.Error("the second request did not retry the lookup")
	}
	p.add(proxy)
	if n := stub.calls.Load(); n != 2 {
		t.Errorf("resolver called %d times, want 2 (failure, success, then remembered)", n)
	}
}

// Concurrent requests for the same proxy: none of them gets past add before the
// addresses are in, and the name is looked up once. Run under -race.
func TestProxyAddrsConcurrentAdd(t *testing.T) {
	loop := netip.MustParseAddr("127.0.0.1")
	stub := &resolverStub{results: [][]netip.Addr{{loop}}, gate: make(chan struct{})}
	p := newProxyAddrs()
	p.resolve = stub.resolve
	proxy, _ := url.Parse("http://proxy.corp.example:3128")
	want := netip.AddrPortFrom(loop, 3128)

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan string, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.add(proxy)
			if !p.has(want) {
				errs <- "add returned before the proxy address was recorded"
			}
		}()
	}
	close(stub.gate)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if n := stub.calls.Load(); n != 1 {
		t.Errorf("resolver called %d times, want 1", n)
	}
}
