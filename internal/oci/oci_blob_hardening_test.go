package oci

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"unicode"
)

// DEVDESK-412 step 4, hardening after review and security audit. These tests sit
// on top of oci_blob_redirect_test.go and reuse its fixtures:
//
//  1. a redirect chain sends no Referer (it would carry the previous hop's
//     signed URL to the next host);
//  2. https to http is refused against the PREVIOUS hop, not only the first one;
//  3. a non-http(s) Location, a 3xx without Location and the edge cases of
//     followBlobRedirect itself;
//  4. SSRF: a blob redirect to a literal loopback or link-local host, or to
//     localhost, is refused unless the registry itself is local, and the
//     transport refuses to connect to such an address (DNS rebinding);
//  5. a third host's HTTP status never reaches an error as raw text.

// headerLog records, for each request, its Referer and a dump of all its headers.
type headerLog struct {
	mu       sync.Mutex
	referers []string
	dumps    []string
}

func (l *headerLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.referers = append(l.referers, r.Header.Get("Referer"))
	l.dumps = append(l.dumps, fmt.Sprint(r.Header))
}

func (l *headerLog) all() (referers, dumps []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.referers...), append([]string(nil), l.dumps...)
}

// dialRouted sends the connections for a ".test" name to def and every other
// connection where it was asked to go. A literal address therefore reaches its
// own server, so a redirect that is not refused is visible in that server's log.
func dialRouted(def string) *http.Transport {
	byName := dialTo(def)
	direct := &net.Dialer{}
	byName.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err == nil && strings.HasSuffix(host, ".test") {
			return direct.DialContext(ctx, network, def)
		}
		return direct.DialContext(ctx, network, addr)
	}
	return byName
}

// 1. A chain A -> B -> C: no hop receives a Referer, and the signed URL of B
// never reaches C.
func TestBlobRedirectChainSendsNoReferer(t *testing.T) {
	var aLog, bLog, cLog headerLog
	archive := clientTarGz(t)

	c := startServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cLog.record(r)
		_, _ = w.Write(archive)
	}))
	b := startServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bLog.record(r)
		http.Redirect(w, r, c.URL+"/final?sig=OTHER", http.StatusTemporaryRedirect)
	}))
	a := startServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aLog.record(r)
		if isManifestRequest(r) {
			_, _ = fmt.Fprint(w, blobManifest())
			return
		}
		http.Redirect(w, r, b.URL+"/hop?"+blobSignedQuery, http.StatusTemporaryRedirect)
	}))

	tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

	wantFixtureEntries(t, tpl, err)
	for name, l := range map[string]*headerLog{"B": &bLog, "C": &cLog} {
		referers, dumps := l.all()
		if len(referers) != 1 {
			t.Fatalf("%s received %d requests, want 1", name, len(referers))
		}
		if referers[0] != "" {
			t.Errorf("%s received Referer %q, want none", name, referers[0])
		}
		for _, d := range dumps {
			if strings.Contains(d, blobSignedQuery) || strings.Contains(d, "/hop") {
				t.Errorf("%s received headers carrying a previous hop's URL: %s", name, d)
			}
		}
	}
}

// 2. The https to http check compares with the previous hop: a chain that
// started on http, went up to https and tries to come back down to http is
// refused. A chain that stays on https is followed.
func TestBlobRedirectDowngradeComparesWithPreviousHop(t *testing.T) {
	t.Run("http to https to http is refused", func(t *testing.T) {
		var aHits, bHits, cHits hitLog
		cHost := archiveHost(t, false, &cHits, clientTarGz(t))
		b := startServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bHits.record(r)
			http.Redirect(w, r, cHost.URL+blobPrivatePath+"?"+blobSignedQuery, http.StatusTemporaryRedirect)
		}))
		a := blobRegistry(t, false, &aHits, redirectTo(b.URL+"/hop?"+blobSignedQuery))

		// b's certificate is the one every httptest TLS server shares; its
		// transport also speaks plain http.
		c := withTransport(NewClient(a.URL, clientTestUser, clientTestPass), b.Client().Transport)
		tpl, err := c.DownloadTemplate("tpl", "v1")

		if !errors.Is(err, errInsecureRedirect) {
			t.Fatalf("err = %v, want errors.Is errInsecureRedirect", err)
		}
		if tpl != nil {
			t.Errorf("template = %+v, want nil", tpl)
		}
		if n := bHits.count(); n != 1 {
			t.Errorf("the https hop received %d requests, want 1", n)
		}
		if n := cHits.count(); n != 0 {
			t.Errorf("the http target received %d requests, want 0", n)
		}
		wantNoLeak(t, err, blobPrivatePath, "/hop")
	})

	t.Run("https to https to https is followed", func(t *testing.T) {
		var aHits, bHits, cHits hitLog
		cHost := archiveHost(t, true, &cHits, clientTarGz(t))
		b := startServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bHits.record(r)
			http.Redirect(w, r, cHost.URL+"/final?"+blobSignedQuery, http.StatusTemporaryRedirect)
		}))
		a := blobRegistry(t, true, &aHits, redirectTo(b.URL+"/hop?"+blobSignedQuery))

		c := withTransport(NewClient(a.URL, clientTestUser, clientTestPass), a.Client().Transport)
		tpl, err := c.DownloadTemplate("tpl", "v1")

		wantFixtureEntries(t, tpl, err)
		if bHits.anyAuth() != "" || cHits.anyAuth() != "" {
			t.Errorf("Authorization reached another origin: B %q, C %q", bHits.anyAuth(), cHits.anyAuth())
		}
	})
}

// 3a. A Location that is not http or https is refused before any connection,
// with no URL in the error.
func TestBlobRedirectToOtherSchemeIsRefused(t *testing.T) {
	var aHits hitLog
	a := blobRegistry(t, false, &aHits, redirectTo("ftp://files.example/private/blob?"+blobSignedQuery))

	tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

	if !errors.Is(err, errBlobRedirectScheme) {
		t.Fatalf("err = %v, want errors.Is errBlobRedirectScheme", err)
	}
	if tpl != nil {
		t.Errorf("template = %+v, want nil", tpl)
	}
	wantNoLeak(t, err, "ftp://", "/private")
	if n := aHits.count(); n != 2 {
		t.Errorf("registry received %d requests, want 2 (manifest and blob)", n)
	}
}

// 3b. A 3xx with no Location is an ordinary failed download.
func TestBlobRedirectWithoutLocation(t *testing.T) {
	var aHits hitLog
	a := blobRegistry(t, false, &aHits, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusFound)
	})

	tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

	if err == nil {
		t.Fatalf("template = %+v, want an error", tpl)
	}
	if !strings.Contains(err.Error(), "failed to download layer: 302") {
		t.Errorf("error %q, want it to start with %q", err, "failed to download layer: 302")
	}
	wantNoLeak(t, err)
}

func mustReq(t *testing.T, raw string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatalf("building request for %q: %v", raw, err)
	}
	return req
}

// 3c. followBlobRedirect called directly: limits, schemes, previous-hop
// downgrade, sticky credential removal and Referer removal.
func TestFollowBlobRedirect(t *testing.T) {
	const reg = "http://registry.test:5000/v2/tpl/blobs/sha256:abc"
	const regTLS = "https://registry.test/v2/tpl/blobs/sha256:abc"
	tenHops := make([]string, MaxRedirects)
	for i := range tenHops {
		tenHops[i] = reg
	}

	cases := []struct {
		name string
		via  []string
		to   string
		want error // nil = followed
		// keepAuth is whether Authorization must survive when the redirect is
		// followed.
		keepAuth bool
	}{
		{"too many hops", tenHops, "http://registry.test:5000/next", ErrTooManyRedirects, false},
		{"one hop short of the cap", tenHops[:MaxRedirects-1], "http://registry.test:5000/next", nil, true},
		{"ftp", []string{reg}, "ftp://files.example/p/SECRET?sig=SECRET", errBlobRedirectScheme, false},
		{"file", []string{reg}, "file:///etc/passwd", errBlobRedirectScheme, false},
		{"no scheme", []string{reg}, "//files.example/p/SECRET?sig=SECRET", errBlobRedirectScheme, false},
		{"https to http", []string{regTLS}, "http://cdn.example/p/SECRET?sig=SECRET", errInsecureRedirect, false},
		{"http, https, then http", []string{reg, "https://cdn.example/hop"}, "http://other.example/p/SECRET?sig=SECRET", errInsecureRedirect, false},
		{"https, https, then http", []string{regTLS, "https://cdn.example/hop"}, "http://other.example/p/SECRET?sig=SECRET", errInsecureRedirect, false},
		{"https, https, then https", []string{regTLS, "https://cdn.example/hop"}, "https://other.example/p", nil, false},
		{"http, https, then https", []string{reg, "https://cdn.example/hop"}, "https://other.example/p", nil, false},
		{"http, http, then http", []string{reg, "http://cdn.example/hop"}, "http://other.example/p", nil, false},
		{"same origin keeps credentials", []string{reg}, "http://registry.test:5000/elsewhere", nil, true},
		{"same origin chain keeps credentials", []string{reg, "http://registry.test:5000/a"}, "http://registry.test:5000/b", nil, true},
		{"other host", []string{reg}, "http://cdn.example/p", nil, false},
		{"other port", []string{reg}, "http://registry.test:5001/p", nil, false},
		{"subdomain", []string{reg}, "http://cdn.registry.test:5000/p", nil, false},
		{"scheme change on the same host", []string{reg}, "https://registry.test:5000/p", nil, false},
		{"back to the origin after leaving it", []string{reg, "http://cdn.example/hop"}, "http://registry.test:5000/back", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			via := make([]*http.Request, len(tc.via))
			for i, v := range tc.via {
				via[i] = mustReq(t, v)
			}
			req := mustReq(t, tc.to)
			req.SetBasicAuth(clientTestUser, clientTestPass)
			req.Header.Set("Referer", reg+"?"+blobSignedQuery)

			err := followBlobRedirect(req, via)

			if tc.want == nil {
				if err != nil {
					t.Fatalf("followBlobRedirect = %v, want nil", err)
				}
			} else {
				if !errors.Is(err, tc.want) {
					t.Fatalf("followBlobRedirect = %v, want errors.Is %v", err, tc.want)
				}
				if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/p/") {
					t.Errorf("error %q carries the target's path or query", err)
				}
				return
			}
			if got := req.Header.Get("Authorization") != ""; got != tc.keepAuth {
				t.Errorf("Authorization kept = %v, want %v", got, tc.keepAuth)
			}
			if !sameOrigin(tc.via[0], tc.to) {
				if got := req.Header.Get("Referer"); got != "" {
					t.Errorf("Referer = %q after leaving the origin, want none", got)
				}
			}
		})
	}
}

// 4d. The control is wired into the transport NewClient gives the blob client:
// for a registry that is not local the transport cannot connect to a live
// loopback listener, for a local registry it can.
func TestNewClientBlobTransportRefusesLocalUnlessRegistryIsLocal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	// The listener stays open for the whole test: a refusal can only come
	// from the client, never from a closed port.

	dial := func(t *testing.T, registryURL string) error {
		t.Helper()
		tr, ok := NewClient(registryURL, "", "").blobClient.Transport.(*http.Transport)
		if !ok || tr.DialContext == nil {
			t.Fatalf("blobClient.Transport = %T, want an *http.Transport with a DialContext", NewClient(registryURL, "", "").blobClient.Transport)
		}
		conn, err := tr.DialContext(context.Background(), "tcp", ln.Addr().String())
		if err == nil {
			_ = conn.Close()
		}
		return err
	}

	if err := dial(t, "https://registry.example.com"); err == nil {
		t.Error("blob transport of a public registry connected to a loopback address, want a refusal")
	}
	for _, local := range []string{"http://127.0.0.1:5000", "http://localhost:5000", "http://[::1]:5000"} {
		if err := dial(t, local); err != nil {
			t.Errorf("blob transport of the local registry %s refused a loopback address: %v", local, err)
		}
	}
}

// statusRoundTripper answers every request with a response whose raw Status
// line is whatever a hostile server chose to write.
type statusRoundTripper struct {
	code   int
	status string
}

func (s statusRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.code,
		Status:     s.status,
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func wantPlainStatusError(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want a failure")
	}
	for _, r := range err.Error() {
		if unicode.IsControl(r) {
			t.Errorf("error %q holds the control character %U", err, r)
		}
	}
	if strings.Contains(err.Error(), "Evil") {
		t.Errorf("error %q repeats the raw status text of the server", err)
	}
	if want := fmt.Sprintf("%d %s", code, http.StatusText(code)); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q lacks %q", err, want)
	}
}

// 5. The status shown to the user is the code and the standard text of that
// code, never the status line the other host wrote.
func TestFailedStatusNeverEchoesTheServersStatusLine(t *testing.T) {
	rt := statusRoundTripper{code: http.StatusForbidden, status: "403 \x1b[31mEvil\x07"}

	t.Run("blob", func(t *testing.T) {
		c := NewClient("http://registry.test", clientTestUser, clientTestPass)
		c.blobClient.Transport = rt
		_, err := c.downloadAndExtractLayer("tpl", clientTestDigest)
		wantPlainStatusError(t, err, http.StatusForbidden)
	})
	t.Run("manifest", func(t *testing.T) {
		c := NewClient("http://registry.test", clientTestUser, clientTestPass)
		c.httpClient.Transport = rt
		_, err := c.DownloadTemplate("tpl", "v1")
		wantPlainStatusError(t, err, http.StatusForbidden)
	})
	t.Run("tags", func(t *testing.T) {
		c := NewClient("http://registry.test", clientTestUser, clientTestPass)
		c.httpClient.Transport = rt
		_, err := c.ListTags("tpl")
		wantPlainStatusError(t, err, http.StatusForbidden)
	})
}
