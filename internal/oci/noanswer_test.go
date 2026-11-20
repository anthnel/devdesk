package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// DEVDESK-629 step 4 (F5, DEVDESK-633): a registry that does not answer before the
// client's delay ends the request with a *NoAnswerError whose text is "no answer
// within N s", N read from RequestTimeout. The delay itself is unchanged (15 s).
//
// None of these tests waits for it: a context already past its deadline, or a client
// swapped for one of tens of milliseconds against a server that blocks on its own
// request context, stands in for the real delay.
//
// This file names no symbol of the step (it detects the type by name), so it fails on
// an assertion before the code exists. The assertions that need the symbols
// (RequestTimeout, NoAnswerError) are in noanswer_symbols_test.go.

// productionTimeout is the delay of the client as shipped, read before any test swaps it.
var productionTimeout = registryHTTPClient.Timeout

// noAnswerText is the text a no-answer error must carry: the production delay, in whole
// seconds, never a literal.
func noAnswerText() string {
	return fmt.Sprintf("no answer within %d s", int(productionTimeout/time.Second))
}

// noAnswerIn reports whether err is, or wraps, a *NoAnswerError, and returns its text.
func noAnswerIn(err error) (string, bool) {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if reflect.TypeOf(e).String() == "*oci.NoAnswerError" {
			return e.Error(), true
		}
	}
	return "", false
}

// withClientTimeout swaps registryHTTPClient for a copy with another timeout, so the
// production CheckRedirect stays in force.
func withClientTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := registryHTTPClient
	c := *old
	c.Timeout = d
	registryHTTPClient = &c
	t.Cleanup(func() { registryHTTPClient = old })
}

const (
	shortClientTimeout = 20 * time.Millisecond
	slowHandlerCap     = time.Second
)

// blockUntilGone holds a handler until the client has given up, with a cap so a missing
// timeout cannot hang the suite.
func blockUntilGone(r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(slowHandlerCap):
	}
}

// countingServer serves h and counts the requests it receives.
func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// registryCall is one way the client reads a registry.
type registryCall struct {
	name string
	run  func(ctx context.Context, apiURL string) error
}

func registryCalls() []registryCall {
	return []registryCall{
		{"tags", func(ctx context.Context, u string) error {
			_, err := ListRegistryTagsContext(ctx, u, "library/nginx", "", "")
			return err
		}},
		{"catalog", func(ctx context.Context, u string) error {
			_, _, err := ListCatalogContext(ctx, u, "", "", 3, 100)
			return err
		}},
		{"catalog under a prefix", func(ctx context.Context, u string) error {
			_, _, err := ListCatalogUnderContext(ctx, u, "docker-proxy", "", "", 3, 100)
			return err
		}},
	}
}

func expiredContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	return ctx
}

func wantNoAnswer(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want a no-answer error")
	}
	text, ok := noAnswerIn(err)
	if !ok {
		t.Fatalf("err = %v (%T), want a *NoAnswerError", err, err)
	}
	if want := noAnswerText(); text != want {
		t.Errorf("no-answer text = %q, want %q", text, want)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false for %v", err)
	}
	var se *StatusError
	if errors.As(err, &se) {
		t.Errorf("err carries a *StatusError (%v): no answer is not a status", se)
	}
}

func TestExpiredContextIsNoAnswer(t *testing.T) {
	for _, c := range registryCalls() {
		t.Run(c.name, func(t *testing.T) {
			srv, hits := countingServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

			wantNoAnswer(t, c.run(expiredContext(t), srv.URL))

			if n := hits.Load(); n != 0 {
				t.Errorf("the registry received %d requests, want 0 for an expired context", n)
			}
		})
	}
}

// A registry that accepts the request and never answers, read through the real client
// delay (shrunk), for every call that reaches a registry, the manifest included.
func TestSlowRegistryIsNoAnswer(t *testing.T) {
	calls := append(registryCalls(), registryCall{"manifest digest", func(_ context.Context, u string) error {
		_, err := ManifestDigest(u, "library/nginx", "latest", "", "")
		return err
	}})
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			withClientTimeout(t, shortClientTimeout)
			srv, hits := countingServer(t, func(_ http.ResponseWriter, r *http.Request) { blockUntilGone(r) })

			start := time.Now()
			err := c.run(context.Background(), srv.URL)

			wantNoAnswer(t, err)
			if n := hits.Load(); n == 0 {
				t.Error("the registry received no request: the test did not exercise the client's delay")
			}
			if took := time.Since(start); took >= slowHandlerCap {
				t.Errorf("returned after %v, want the client's own delay (%v)", took, shortClientTimeout)
			}
		})
	}
}

// The answer starts and stalls: the delay then ends the body read, not the request.
func TestSlowBodyIsNoAnswer(t *testing.T) {
	stall := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte(`{"repositories":[`))
		w.(http.Flusher).Flush()
		blockUntilGone(r)
	}
	for _, c := range registryCalls() {
		t.Run(c.name, func(t *testing.T) {
			withClientTimeout(t, shortClientTimeout)
			srv, _ := countingServer(t, stall)

			wantNoAnswer(t, c.run(context.Background(), srv.URL))
		})
	}
}

// The token endpoint is a registry request too: one that never answers, or answers
// headers and stalls, is no answer.
func TestSlowTokenEndpointIsNoAnswer(t *testing.T) {
	for name, token := range map[string]http.HandlerFunc{
		"no answer": func(_ http.ResponseWriter, r *http.Request) { blockUntilGone(r) },
		"stalled answer": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte(`{"token":`))
			w.(http.Flusher).Flush()
			blockUntilGone(r)
		},
	} {
		t.Run(name, func(t *testing.T) {
			withClientTimeout(t, shortClientTimeout)
			srv := bearerRegistry(t, token, func(http.ResponseWriter, *http.Request) {})

			_, err := ListRegistryTagsContext(context.Background(), srv.URL, "library/nginx", "", "")

			wantNoAnswer(t, err)
		})
	}
}

// A 502 that arrives in time is a status, not a lack of answer; its text says nothing
// about a delay, so the two cases cannot be confused.
func TestTimelyBadGatewayIsNotNoAnswer(t *testing.T) {
	calls := append(registryCalls(), registryCall{"manifest digest", func(_ context.Context, u string) error {
		_, err := ManifestDigest(u, "library/nginx", "latest", "", "")
		return err
	}})
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })

			err := c.run(context.Background(), srv.URL)

			var se *StatusError
			if !errors.As(err, &se) || se.Code != http.StatusBadGateway {
				t.Fatalf("err = %v (%T), want a *StatusError{502}", err, err)
			}
			if _, ok := noAnswerIn(err); ok {
				t.Errorf("a 502 received in time is a no-answer error: %v", err)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("a 502 received in time satisfies errors.Is(DeadlineExceeded): %v", err)
			}
			if strings.Contains(err.Error(), "no answer") {
				t.Errorf("err text = %q, must not say no answer", err.Error())
			}
		})
	}
}

// The no-answer error is not a wrapper of the request: no URL, user, password or page
// size comes back with it.
func TestNoAnswerCarriesNoURL(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	withCreds := strings.Replace(srv.URL, "http://", "http://alice:s3cr3t-pw@", 1)
	host := strings.TrimPrefix(srv.URL, "http://")

	for _, c := range registryCalls() {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(expiredContext(t), withCreds)

			wantNoAnswer(t, err)
			for _, leak := range []string{"@", "alice", "s3cr3t", "http://", host, "n=", "/v2/"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("err = %q carries %q", err.Error(), leak)
				}
			}
		})
	}
}

// A cancellation is the caller's choice, not a lack of answer: it stays as it is.
func TestCancelledContextIsNotNoAnswer(t *testing.T) {
	for _, c := range registryCalls() {
		t.Run(c.name, func(t *testing.T) {
			srv, _ := countingServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := c.run(ctx, srv.URL)

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want errors.Is(context.Canceled)", err)
			}
			if _, ok := noAnswerIn(err); ok {
				t.Errorf("a cancellation became a no-answer error: %v", err)
			}
		})
	}
}
