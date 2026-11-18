package nexusbench

import (
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// The tests of client.exchange (DEVDESK-511, step 7; Plane DEVDESK-574): the
// one place the recorder sends a request, next to do in nexusapi.go, which is
// the only Basic sender of the bench (guard 3 of guard_test.go). Unlike do it
// keeps the status, the headers and the body of any answer, a 404 included, and
// it sends what the fixture says: the headers it is given, minus the
// credentials of the fixture (a placeholder), plus its own when it has any.
//
//	func (c *client) exchange(ctx context.Context, method, target string, header http.Header) (status int, h http.Header, body []byte, err error)
//
// The target is path and raw query, as a fixture records it, appended to the
// base of the client.

func TestExchangeKeepsTheAnswerOfAnErrorStatus(t *testing.T) {
	b := newRecBench(t)
	b.answer("admin", "/service/rest/v1/missing", recAnswer{
		status: http.StatusNotFound, contentType: "application/json",
		header: http.Header{"Docker-Distribution-Api-Version": {"registry/2.0"}},
		body:   `{"message":"Repository not found"}`,
	})
	c := newClient(b.srv.URL, "admin", recAdminPW)
	defer c.closeIdle()

	status, h, body, err := c.exchange(context.Background(), http.MethodGet, "/service/rest/v1/missing", http.Header{})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if status != http.StatusNotFound || string(body) != `{"message":"Repository not found"}` {
		t.Errorf("answer = %d %q, want the 404 and its body (do would have dropped it)", status, body)
	}
	if h.Get("Server") != recServer || h.Get("Docker-Distribution-Api-Version") != "registry/2.0" || h.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v, want the answer's headers", h)
	}
}

func TestExchangeFollowsNoRedirect(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	t.Cleanup(other.Close)
	b := newRecBench(t)
	b.answer("admin", "/v2/_catalog", recAnswer{status: http.StatusFound, header: http.Header{"Location": {other.URL + "/v2/_catalog"}}})
	c := newClient(b.srv.URL, "admin", recAdminPW)
	defer c.closeIdle()

	status, h, _, err := c.exchange(context.Background(), http.MethodGet, "/v2/_catalog", http.Header{})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if status != http.StatusFound || h.Get("Location") != other.URL+"/v2/_catalog" {
		t.Errorf("answer = %d, Location %q, want the 302 itself", status, h.Get("Location"))
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("the redirect was followed: %d request(s) reached the other origin", n)
	}
}

func TestExchangeSendsTheHeadersOfTheFixtureAndItsOwnCredentials(t *testing.T) {
	tests := []struct {
		name, user, password string
		wantUser             string
	}{
		{"an account", "bench-pull", recPullPW, "bench-pull"},
		{"anonymous", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newRecBench(t)
			c := newClient(b.srv.URL, tt.user, tt.password)
			defer c.closeIdle()
			// The fixture says Basic <redacted>: that is a placeholder, never a value to send.
			header := http.Header{"Authorization": {"Basic <redacted>"}, "Accept": {"application/json"}}
			if _, _, _, err := c.exchange(context.Background(), http.MethodGet, "/v2/_catalog?n=5&last=api", header); err != nil {
				t.Fatalf("exchange: %v", err)
			}
			got := b.requests()
			if len(got) != 1 {
				t.Fatalf("%d request(s), want 1", len(got))
			}
			s := got[0]
			if s.method != http.MethodGet || s.uri != "/v2/_catalog?n=5&last=api" {
				t.Errorf("request = %s %s, want the target as given", s.method, s.uri)
			}
			if s.header.Get("Accept") != "application/json" {
				t.Errorf("Accept = %q, want the header of the fixture", s.header.Get("Accept"))
			}
			if s.header.Get("Cookie") != "" {
				t.Errorf("Cookie = %q, want none", s.header.Get("Cookie"))
			}
			auth := s.header.Values("Authorization")
			switch {
			case tt.wantUser == "" && len(auth) != 0:
				t.Errorf("an anonymous exchange carried %v", auth)
			case tt.wantUser != "" && (len(auth) != 1 || s.user != tt.wantUser || strings.Contains(auth[0], "<redacted>")):
				t.Errorf("credentials = %v (user %q), want one Basic header of %q", auth, s.user, tt.wantUser)
			}
		})
	}
}

func TestExchangeReadsAtMostMaxResponseBytes(t *testing.T) {
	b := newRecBench(t)
	b.answer("", "/v2/exact", recAnswer{contentType: "text/plain", body: strings.Repeat("x", maxResponseBytes)})
	b.answer("", "/v2/over", recAnswer{contentType: "text/plain", body: strings.Repeat("x", maxResponseBytes+1)})
	b.answer("", "/v2/over-404", recAnswer{status: http.StatusNotFound, contentType: "text/plain", body: strings.Repeat("x", maxResponseBytes+1)})
	c := newClient(b.srv.URL, "", "")
	defer c.closeIdle()

	if _, _, body, err := c.exchange(context.Background(), http.MethodGet, "/v2/exact", nil); err != nil || len(body) != maxResponseBytes {
		t.Errorf("an answer of exactly %d bytes: %d bytes, error %v, want it whole", maxResponseBytes, len(body), err)
	}
	for _, target := range []string{"/v2/over", "/v2/over-404"} {
		_, _, body, err := c.exchange(context.Background(), http.MethodGet, target, nil)
		var tooLarge *tooLargeError
		if !errors.As(err, &tooLarge) {
			t.Errorf("%s: error %v, want a *tooLargeError", target, err)
		}
		if len(body) != 0 {
			t.Errorf("%s: %d bytes of an answer past the cap were returned", target, len(body))
		}
	}
}

func TestExchangeRefusesAMethodOtherThanGETBeforeSending(t *testing.T) {
	b := newRecBench(t)
	c := newClient(b.srv.URL, "admin", recAdminPW)
	defer c.closeIdle()
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, "get", ""} {
		if _, _, _, err := c.exchange(context.Background(), method, "/v2/_catalog", nil); err == nil {
			t.Errorf("method %q was accepted", method)
		}
	}
	if got := b.requests(); len(got) != 0 {
		t.Errorf("%d request(s) were sent, want none", len(got))
	}
}

func TestExchangeRefusesATargetThatIsNotAPathBeforeSending(t *testing.T) {
	b := newRecBench(t)
	c := newClient(b.srv.URL, "admin", recAdminPW)
	defer c.closeIdle()
	for _, target := range []string{"", "v2/_catalog", "http://evil.example/v2/_catalog", "//evil.example/v2/_catalog"} {
		if _, _, _, err := c.exchange(context.Background(), http.MethodGet, target, nil); err == nil {
			t.Errorf("target %q was accepted", target)
		}
	}
	if got := b.requests(); len(got) != 0 {
		t.Errorf("%d request(s) were sent, want none", len(got))
	}
}

// gzipBench is a Nexus that compresses an answer when it is asked to and sends
// it as is, with its Content-Length, when it is not.
type gzipBench struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []string
}

func newGzipBench(t *testing.T, body string) *gzipBench {
	t.Helper()
	b := &gzipBench{}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accepted := r.Header.Get("Accept-Encoding")
		b.mu.Lock()
		b.seen = append(b.seen, accepted)
		b.mu.Unlock()
		w.Header().Set("Server", recServer)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(strings.ToLower(accepted), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			_, _ = zw.Write([]byte(body))
			_ = zw.Close()
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(b.srv.Close)
	return b
}

// encodings is the Accept-Encoding header of every request, in order.
func (b *gzipBench) encodings() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.seen...)
}

// exchange reads the answer as the instance sends it: no gzip is asked for, so
// the Content-Length of the answer survives.
func TestExchangeAsksForNoCompression(t *testing.T) {
	b := newGzipBench(t, recCatalogBody)
	c := newClient(b.srv.URL, "", "")
	defer c.closeIdle()

	status, h, body, err := c.exchange(context.Background(), http.MethodGet, "/v2/_catalog", http.Header{"Accept": {"application/json"}})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if got := b.encodings(); len(got) != 1 || strings.Contains(strings.ToLower(got[0]), "gzip") {
		t.Errorf("Accept-Encoding = %q, want a request that does not ask for gzip", got)
	}
	if status != http.StatusOK || string(body) != recCatalogBody {
		t.Errorf("answer = %d %q, want the catalog as sent", status, body)
	}
	if h.Get("Content-Encoding") != "" || h.Get("Content-Length") != strconv.Itoa(len(recCatalogBody)) {
		t.Errorf("Content-Encoding %q, Content-Length %q, want none and %d", h.Get("Content-Encoding"), h.Get("Content-Length"), len(recCatalogBody))
	}
}

// Turning compression off belongs to the recorder only: every other call of the
// client keeps what the transport does by default, asking for gzip and decoding
// the answer.
func TestOtherCallsOfTheClientKeepTheTransportCompression(t *testing.T) {
	b := newGzipBench(t, recCatalogBody)
	c := newClient(b.srv.URL, "", "")
	defer c.closeIdle()

	resp, err := c.do(context.Background(), request{op: "read the catalog", method: http.MethodGet, path: "/v2/_catalog"})
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if got := b.encodings(); len(got) != 1 || !strings.Contains(strings.ToLower(got[0]), "gzip") {
		t.Errorf("Accept-Encoding = %q, want a request that asks for gzip", got)
	}
	if string(resp.body) != recCatalogBody {
		t.Errorf("body = %q, want it decoded", resp.body)
	}
}
