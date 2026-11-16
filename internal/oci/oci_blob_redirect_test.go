package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DEVDESK-412 step 4: the layer blob of oci.Client follows a redirect to
// another origin, but never with credentials; the chain is bounded in hops and
// in compressed bytes, https never downgrades to http, and no error carries the
// (possibly signed) URL of a hop. Server A plays the registry (it serves the
// manifest and redirects the blob), server B the other host.
//
// The manifest and tags calls keep refusing any other origin (step 3), and
// those refusals are re-checked here at the end.

const (
	// blobSignedQuery stands for a presigned URL query; it must appear in no
	// error and no log.
	blobSignedQuery = "sig=SECRET"
	// blobPrivatePath is a path that must never be echoed by an error.
	blobPrivatePath = "/private-blob-path"
)

// blobManifest is the manifest every fixture registry serves for tpl:v1: its
// layer is the one clientTarGz serves.
func blobManifest() string {
	return blobManifestFor(clientTestDigest)
}

func isManifestRequest(r *http.Request) bool {
	return strings.Contains(r.URL.Path, "/manifests/")
}

// blobRegistry starts a registry that records every request in log, serves the
// manifest and hands every other request to rest.
func blobRegistry(t *testing.T, tlsServer bool, log *hitLog, rest http.HandlerFunc) *httptest.Server {
	t.Helper()
	return blobRegistryFor(t, tlsServer, log, clientTestDigest, rest)
}

// blobRegistryFor is blobRegistry for a registry whose manifest names digest.
func blobRegistryFor(t *testing.T, tlsServer bool, log *hitLog, digest string, rest http.HandlerFunc) *httptest.Server {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		if isManifestRequest(r) {
			_, _ = fmt.Fprint(w, blobManifestFor(digest))
			return
		}
		rest(w, r)
	})
	return startServer(t, tlsServer, h)
}

// archiveHost starts a host that records every request in log and serves archive
// to all of them.
func archiveHost(t *testing.T, tlsServer bool, log *hitLog, archive []byte) *httptest.Server {
	t.Helper()
	return startServer(t, tlsServer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		_, _ = w.Write(archive)
	}))
}

func startServer(t *testing.T, tlsServer bool, h http.Handler) *httptest.Server {
	t.Helper()
	var s *httptest.Server
	if tlsServer {
		s = httptest.NewTLSServer(h)
	} else {
		s = httptest.NewServer(h)
	}
	t.Cleanup(s.Close)
	return s
}

// withTransport sends both of the client's HTTP clients through rt, keeping the
// CheckRedirect each of them carries in production.
func withTransport(c *Client, rt http.RoundTripper) *Client {
	c.httpClient.Transport = rt
	c.blobClient.Transport = rt
	return c
}

// redirectTo answers every request with a 307 to loc.
func redirectTo(loc string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, loc, http.StatusTemporaryRedirect)
	}
}

// wantFixtureEntries checks that the download returned the one file of
// clientTarGz.
func wantFixtureEntries(t *testing.T, tpl *Template, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("DownloadTemplate: %v", err)
	}
	if tpl == nil || len(tpl.Entries) != 1 || tpl.Entries[0].Path != "Dockerfile" ||
		string(tpl.Entries[0].Content) != "FROM scratch\n" {
		t.Fatalf("template = %+v, want the single Dockerfile of the fixture archive", tpl)
	}
}

// wantNoLeak fails when err mentions any of the strings a signed URL, a path or
// a credential would leave behind.
func wantNoLeak(t *testing.T, err error, leaks ...string) {
	t.Helper()
	for _, leak := range append(leaks, "SECRET", "sig=", clientTestPass) {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q leaks %q", err, leak)
		}
	}
}

// 1. The base case: the blob goes to another origin (same host name, other
// port) and gets no Authorization. net/http alone would copy the header there.
func TestBlobRedirectOffOriginDropsCredentials(t *testing.T) {
	var aHits, bHits hitLog
	b := archiveHost(t, false, &bHits, clientTarGz(t))
	a := blobRegistry(t, false, &aHits, redirectTo(b.URL+"/blob?"+blobSignedQuery))

	tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

	wantFixtureEntries(t, tpl, err)
	if n := bHits.count(); n != 1 {
		t.Fatalf("the other origin received %d requests, want 1", n)
	}
	if got := bHits.anyAuth(); got != "" {
		t.Errorf("the other origin saw Authorization %q, want none", got)
	}
	if got := authsOf(&aHits, "/manifests/"); len(got) != 1 || got[0] != basicHeader() {
		t.Errorf("manifest Authorization = %q, want one Basic header", got)
	}
	if got := authsOf(&aHits, "/blobs/"); len(got) != 1 || got[0] != basicHeader() {
		t.Errorf("registry blob Authorization = %q, want one Basic header", got)
	}
}

// 2. A subdomain of the registry host is another origin: net/http copies
// sensitive headers to it, the policy must not.
func TestBlobRedirectToSubdomainDropsCredentials(t *testing.T) {
	var regHits, cdnHits hitLog
	archive := clientTarGz(t)
	var s *httptest.Server
	s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// One listener answers both names; the Host tells them apart.
		if strings.HasPrefix(r.Host, "cdn.") {
			cdnHits.record(r)
			_, _ = w.Write(archive)
			return
		}
		regHits.record(r)
		if isManifestRequest(r) {
			_, _ = fmt.Fprint(w, blobManifest())
			return
		}
		http.Redirect(w, r, "http://cdn.registry.test:"+portOfServer(s)+"/blob?"+blobSignedQuery, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(s.Close)

	c := withTransport(NewClient("http://registry.test:"+portOfServer(s), clientTestUser, clientTestPass), dialTo(s.Listener.Addr().String()))
	tpl, err := c.DownloadTemplate("tpl", "v1")

	wantFixtureEntries(t, tpl, err)
	if n := cdnHits.count(); n != 1 {
		t.Fatalf("the subdomain received %d requests, want 1", n)
	}
	if got := cdnHits.anyAuth(); got != "" {
		t.Errorf("the subdomain saw Authorization %q, want none", got)
	}
	if got := authsOf(&regHits, "/blobs/"); len(got) != 1 || got[0] != basicHeader() {
		t.Errorf("registry blob Authorization = %q, want one Basic header", got)
	}
}

// 3. Once the chain left the origin, coming back to it does not bring the
// credentials back: A -> B -> A reaches A the second time anonymously.
func TestBlobRedirectCredentialsStayDroppedWhenChainReturns(t *testing.T) {
	var aHits, bHits hitLog
	archive := clientTarGz(t)
	var a, b *httptest.Server
	b = startServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.record(r)
		http.Redirect(w, r, a.URL+"/blob-back?"+blobSignedQuery, http.StatusTemporaryRedirect)
	}))
	a = blobRegistry(t, false, &aHits, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/blob-back") {
			_, _ = w.Write(archive)
			return
		}
		http.Redirect(w, r, b.URL+"/hop?"+blobSignedQuery, http.StatusTemporaryRedirect)
	})

	tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

	wantFixtureEntries(t, tpl, err)
	if got := authsOf(&aHits, "/v2/tpl/blobs/"); len(got) != 1 || got[0] != basicHeader() {
		t.Errorf("first registry hop Authorization = %q, want one Basic header", got)
	}
	if got := bHits.anyAuth(); got != "" {
		t.Errorf("the other origin saw Authorization %q, want none", got)
	}
	back := authsOf(&aHits, "/blob-back")
	if len(back) != 1 {
		t.Fatalf("registry saw %d return requests, want 1", len(back))
	}
	if back[0] != "" {
		t.Errorf("return to the registry carried Authorization %q, want none (never restored)", back[0])
	}
}

// 4. A redirect chain that stays on the registry keeps its credentials on every
// hop.
func TestBlobRedirectSameOriginChainKeepsCredentials(t *testing.T) {
	var hits hitLog
	archive := clientTarGz(t)
	a := blobRegistry(t, false, &hits, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/blob-final"):
			_, _ = w.Write(archive)
		case strings.HasPrefix(r.URL.Path, "/blob-moved"):
			http.Redirect(w, r, "/blob-final", http.StatusTemporaryRedirect)
		default:
			http.Redirect(w, r, "/blob-moved", http.StatusTemporaryRedirect)
		}
	})

	tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

	wantFixtureEntries(t, tpl, err)
	for _, part := range []string{"/v2/tpl/blobs/", "/blob-moved", "/blob-final"} {
		if got := authsOf(&hits, part); len(got) != 1 || got[0] != basicHeader() {
			t.Errorf("Authorization on %s = %q, want one Basic header", part, got)
		}
	}
}

// 5. A blob never goes from https to http, and may go from https to https.
func TestBlobRedirectDowngrade(t *testing.T) {
	t.Run("https to http is refused", func(t *testing.T) {
		var aHits, bHits hitLog
		b := archiveHost(t, false, &bHits, clientTarGz(t))
		a := blobRegistry(t, true, &aHits, redirectTo(b.URL+blobPrivatePath+"?"+blobSignedQuery))

		c := withTransport(NewClient(a.URL, clientTestUser, clientTestPass), a.Client().Transport)
		tpl, err := c.DownloadTemplate("tpl", "v1")

		if !errors.Is(err, errInsecureRedirect) {
			t.Fatalf("err = %v, want errors.Is errInsecureRedirect", err)
		}
		if tpl != nil {
			t.Errorf("template = %+v, want nil", tpl)
		}
		if n := bHits.count(); n != 0 {
			t.Errorf("the http target received %d requests, want 0", n)
		}
		wantNoLeak(t, err, blobPrivatePath)
	})

	t.Run("https to https is followed without credentials", func(t *testing.T) {
		var aHits, bHits hitLog
		b := archiveHost(t, true, &bHits, clientTarGz(t))
		a := blobRegistry(t, true, &aHits, redirectTo(b.URL+"/blob?"+blobSignedQuery))

		// Both TLS test servers share one certificate.
		c := withTransport(NewClient(a.URL, clientTestUser, clientTestPass), a.Client().Transport)
		tpl, err := c.DownloadTemplate("tpl", "v1")

		wantFixtureEntries(t, tpl, err)
		if n := bHits.count(); n != 1 {
			t.Fatalf("the https target received %d requests, want 1", n)
		}
		if got := bHits.anyAuth(); got != "" {
			t.Errorf("the https target saw Authorization %q, want none", got)
		}
	})

	t.Run("http to https is followed", func(t *testing.T) {
		var aHits, bHits hitLog
		b := archiveHost(t, true, &bHits, clientTarGz(t))
		a := blobRegistry(t, false, &aHits, redirectTo(b.URL+"/blob?"+blobSignedQuery))

		c := withTransport(NewClient(a.URL, clientTestUser, clientTestPass), b.Client().Transport)
		tpl, err := c.DownloadTemplate("tpl", "v1")

		wantFixtureEntries(t, tpl, err)
		if got := bHits.anyAuth(); got != "" {
			t.Errorf("the https target saw Authorization %q, want none", got)
		}
	})
}

// 6. The chain is bounded: A and B keep redirecting to each other and the
// download stops after MaxRedirects hops, without any hop's URL in the error.
func TestBlobRedirectChainIsBounded(t *testing.T) {
	var aHits, bHits, aHops hitLog
	var a, b *httptest.Server
	// blobRegistry already records every request of A in aHits; aHops only
	// numbers A's hops, so A's blob requests are not counted twice.
	hop := func(other func() string, log *hitLog) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			log.record(r)
			n := log.count()
			http.Redirect(w, r, fmt.Sprintf("%s/hop%d-SECRET?%s", other(), n, blobSignedQuery), http.StatusTemporaryRedirect)
		}
	}
	b = startServer(t, false, hop(func() string { return a.URL }, &bHits))
	a = blobRegistry(t, false, &aHits, hop(func() string { return b.URL }, &aHops))

	tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("err = %v, want errors.Is ErrTooManyRedirects", err)
	}
	if tpl != nil {
		t.Errorf("template = %+v, want nil", tpl)
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Errorf("error %q lacks the cap message", err)
	}
	// The manifest is one request of A; the rest is the blob chain.
	if n := aHits.count() - 1 + bHits.count(); n != MaxRedirects {
		t.Errorf("blob chain made %d requests, want %d", n, MaxRedirects)
	}
	wantNoLeak(t, err, "/hop")
}

// 7. A transport failure on the other host names that host at most: not the
// path, not the signed query.
func TestBlobTransportErrorCarriesNoSignedURL(t *testing.T) {
	var aHits, bHits hitLog
	b := startServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.record(r)
		// Drop the connection with no response at all.
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
	a := blobRegistry(t, false, &aHits, redirectTo(b.URL+blobPrivatePath+"?"+blobSignedQuery))

	tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

	if err == nil {
		t.Fatalf("template = %+v, want an error", tpl)
	}
	if bHits.count() == 0 {
		t.Fatal("the other host was never reached, the test checks nothing")
	}
	wantNoLeak(t, err, blobPrivatePath, "http://")
}

// paddedTarGz builds a small archive of three tiny files, each preceded by a
// PAX header carrying 500 KiB of seeded random (incompressible) padding. The
// padding is neither file content nor counted by Limits, so only a cap on the
// bytes taken off the network sees it: about 1.5 MiB in all.
func paddedTarGz(t *testing.T) []byte {
	t.Helper()
	const (
		files   = 3
		padSize = 500 << 10
	)
	rng := rand.New(rand.NewSource(412))
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for i := range files {
		pad := make([]byte, padSize)
		_, _ = rng.Read(pad)
		hdr := &tar.Header{
			Name:       fmt.Sprintf("f%d.txt", i),
			Mode:       0o644,
			Size:       2,
			Typeflag:   tar.TypeReg,
			Format:     tar.FormatPAX,
			PAXRecords: map[string]string{"DEVDESK.pad": string(pad)},
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing header %d: %v", i, err)
		}
		if _, err := tw.Write([]byte("hi")); err != nil {
			t.Fatalf("writing body %d: %v", i, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("closing gzip: %v", err)
	}
	if buf.Len() > 2<<20 {
		t.Fatalf("fixture is %d bytes, keep it under 2 MiB", buf.Len())
	}
	return buf.Bytes()
}

// 8. The compressed bytes taken off the network are capped, even when what the
// archive declares as content stays within Limits.
func TestBlobByteCap(t *testing.T) {
	archive := paddedTarGz(t)
	lim := Limits{Files: 10, Bytes: 1 << 10}
	if int64(len(archive)) <= blobCap(lim) {
		t.Fatalf("fixture (%d bytes) is not over blobCap (%d), the test checks nothing", len(archive), blobCap(lim))
	}

	t.Run("blobCap formula", func(t *testing.T) {
		if got, want := blobCap(lim), int64(1<<10+10*2048+1<<20); got != want {
			t.Errorf("blobCap(%+v) = %d, want %d", lim, got, want)
		}
		if got := blobCap(Limits{}); got != 0 {
			t.Errorf("blobCap(zero) = %d, want 0 (no cap)", got)
		}
	})

	t.Run("redirected blob over the cap", func(t *testing.T) {
		var aHits, bHits hitLog
		b := archiveHost(t, false, &bHits, archive)
		// The manifest names the digest of the archive served, so only the
		// cap can refuse it.
		a := blobRegistryFor(t, false, &aHits, sha256Digest(archive), redirectTo(b.URL+"/blob?"+blobSignedQuery))

		c := NewClient(a.URL, clientTestUser, clientTestPass)
		c.Limits = lim
		tpl, err := c.DownloadTemplate("tpl", "v1")

		if !errors.Is(err, ErrArchiveTooLarge) {
			t.Fatalf("err = %v, want errors.Is ErrArchiveTooLarge", err)
		}
		if tpl != nil {
			t.Errorf("template = %+v, want nil", tpl)
		}
	})

	t.Run("same archive without limits is read", func(t *testing.T) {
		var aHits, bHits hitLog
		b := archiveHost(t, false, &bHits, archive)
		a := blobRegistryFor(t, false, &aHits, sha256Digest(archive), redirectTo(b.URL+"/blob?"+blobSignedQuery))

		tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")

		if err != nil {
			t.Fatalf("DownloadTemplate without Limits: %v", err)
		}
		if len(tpl.Entries) != 3 {
			t.Errorf("entries = %d, want 3", len(tpl.Entries))
		}
	})
}

// 9. Only the blob follows a redirect: the manifest and the tags of oci.Client
// and the calls of the registry browser still refuse another origin.
func TestOtherCallsStillRefuseOffOriginRedirect(t *testing.T) {
	var aHits, bHits hitLog
	b := recordingServer(t, &bHits)
	a := startServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aHits.record(r)
		http.Redirect(w, r, b.URL+"/x?"+blobSignedQuery, http.StatusTemporaryRedirect)
	}))

	for _, op := range clientOps() {
		t.Run("client/"+op.name, func(t *testing.T) {
			err := op.run(NewClient(a.URL, clientTestUser, clientTestPass))
			if !errors.Is(err, ErrOffOriginRedirect) {
				t.Fatalf("err = %v, want errors.Is ErrOffOriginRedirect", err)
			}
			wantNoLeak(t, err, "/x")
		})
	}
	t.Run("registry tags", func(t *testing.T) {
		_, err := ListRegistryTagsContext(context.Background(), a.URL, "tpl", clientTestUser, clientTestPass)
		if !errors.Is(err, ErrOffOriginRedirect) {
			t.Fatalf("err = %v, want errors.Is ErrOffOriginRedirect", err)
		}
	})
	t.Run("catalog", func(t *testing.T) {
		_, _, err := ListCatalogContext(context.Background(), a.URL, clientTestUser, clientTestPass, 10, 1000)
		if !errors.Is(err, ErrOffOriginRedirect) {
			t.Fatalf("err = %v, want errors.Is ErrOffOriginRedirect", err)
		}
	})
	if n := bHits.count(); n != 0 {
		t.Errorf("the other origin received %d requests, want 0", n)
	}
}
