package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// DEVDESK-667 (S-04b, criterion c): Docker Hub answers 429 when a quota is spent. The
// client already reports it as a *StatusError, which is what lets the browser word it as a
// rate limit; these tests pin that shape (code, source) and that nothing retries, since a
// retry against a spent quota only spends more of it.

// rateLimitedRegistry answers the tag list with 429 (when tagsLimited) and the token
// endpoint with 429 (when tokenLimited), counting the requests of each.
func rateLimitedRegistry(t *testing.T, tagsLimited, tokenLimited bool) (srv *httptest.Server, tags, tokens *atomic.Int32) {
	t.Helper()
	tags, tokens = new(atomic.Int32), new(atomic.Int32)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokens.Add(1)
			if tokenLimited {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{"token":"tok"}`))
		case tagsLimited:
			tags.Add(1)
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			tags.Add(1)
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="reg"`, srv.URL))
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, tags, tokens
}

func TestARateLimitedTagListIsAStatusErrorAndIsNotRetried(t *testing.T) {
	srv, tags, tokens := rateLimitedRegistry(t, true, false)

	_, err := ListRegistryTagsContext(context.Background(), srv.URL, "library/nginx", "", "")

	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v (%T), want a *StatusError", err, err)
	}
	if se.Code != http.StatusTooManyRequests || se.Source != SourceRegistry {
		t.Errorf("StatusError = {Code %d, Source %v}, want {429, SourceRegistry}", se.Code, se.Source)
	}
	if got := tags.Load(); got != 1 {
		t.Errorf("tag list requests = %d, want 1 (no retry)", got)
	}
	if got := tokens.Load(); got != 0 {
		t.Errorf("token requests = %d, want 0", got)
	}
}

func TestARateLimitedTokenEndpointIsAStatusErrorFromTheToken(t *testing.T) {
	srv, tags, tokens := rateLimitedRegistry(t, false, true)

	_, err := ListRegistryTagsContext(context.Background(), srv.URL, "library/nginx", "", "")

	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v (%T), want a *StatusError", err, err)
	}
	if se.Code != http.StatusTooManyRequests || se.Source != SourceToken {
		t.Errorf("StatusError = {Code %d, Source %v}, want {429, SourceToken}", se.Code, se.Source)
	}
	if got := tokens.Load(); got != 1 {
		t.Errorf("token requests = %d, want 1 (no retry)", got)
	}
	if got := tags.Load(); got != 1 {
		t.Errorf("tag list requests = %d, want 1: the walk stops at the token", got)
	}
}
