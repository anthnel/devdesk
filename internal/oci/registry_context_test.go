package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests pin that the context-aware variants of the registry client honor a deadline and a
// cancellation, and that the context-free functions keep their behavior.
// Handlers synchronize through channels and r.Context(), never through sleeps.

const ctxTestBound = 2 * time.Second

// bearerRegistry serves a Bearer-protected tag list. tokenHandler answers the
// token endpoint; tagsHandler answers an authorized /tags/list request.
func bearerRegistry(t *testing.T, tokenHandler, tagsHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenHandler(w, r)
		case r.Header.Get("Authorization") == "Bearer tok":
			tagsHandler(w, r)
		default:
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="reg"`, srv.URL))
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func okToken(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"token":"tok"}`))
}

func assertDeadline(t *testing.T, err error, elapsed time.Duration) {
	t.Helper()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v (%T), want errors.Is(context.DeadlineExceeded)", err, err)
	}
	var se *StatusError
	if errors.As(err, &se) {
		t.Errorf("err is a *StatusError (%v), a deadline is not a status", se)
	}
	if elapsed >= ctxTestBound {
		t.Errorf("returned after %v, want well under %v", elapsed, ctxTestBound)
	}
}

func TestListRegistryTagsContextDeadlineMidPagination(t *testing.T) {
	var srv *httptest.Server
	page2 := make(chan struct{})
	var page2Once sync.Once
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			page2Once.Do(func() { close(page2) })
			<-r.Context().Done() // the slow page: held until the client gives up
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/v2/x/tags/list?page=2>; rel="next"`, srv.URL))
		_, _ = w.Write([]byte(`{"tags":["a","b"]}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	tags, err := ListRegistryTagsContext(ctx, srv.URL, "x", "", "")
	assertDeadline(t, err, time.Since(start))
	select {
	case <-page2:
	default:
		t.Error("page 2 was never requested: the deadline hit before the pagination reached it")
	}
	if tags != nil {
		t.Errorf("tags = %v, want nil on an interrupted walk", tags)
	}
}

func TestListRegistryTagsContextDeadlineOnTokenEndpoint(t *testing.T) {
	srv := bearerRegistry(t,
		func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
		func(http.ResponseWriter, *http.Request) { t.Error("tags requested although the token never came") },
	)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ListRegistryTagsContext(ctx, srv.URL, "x", "", "")
	assertDeadline(t, err, time.Since(start))
}

func TestListRegistryTagsContextClosedServerIsNotADeadline(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), ctxTestBound)
	defer cancel()
	_, err := ListRegistryTagsContext(ctx, url, "x", "", "")
	if err == nil {
		t.Fatal("err = nil, want a network error")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, a refused connection must not look like a deadline or a cancel", err)
	}
}

func TestListRegistryTagsContextCancelClosesConnection(t *testing.T) {
	started := make(chan struct{})
	closed := make(chan struct{})
	var startedOnce, closedOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		<-r.Context().Done()
		closedOnce.Do(func() { close(closed) })
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, err := ListRegistryTagsContext(ctx, srv.URL, "x", "", "")
		errc <- err
	}()

	select {
	case <-started:
	case <-time.After(ctxTestBound):
		t.Fatal("the registry never saw the request")
	}
	cancel()

	select {
	case <-closed:
	case <-time.After(ctxTestBound):
		t.Fatal("the registry did not see the connection close after cancel()")
	}
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want errors.Is(context.Canceled)", err)
		}
		var se *StatusError
		if errors.As(err, &se) {
			t.Errorf("err is a *StatusError (%v)", se)
		}
	case <-time.After(ctxTestBound):
		t.Fatal("ListRegistryTagsContext did not return after cancel()")
	}
}

func TestListRegistryTagsContextAlreadyCancelledSendsNothing(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"tags":[]}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ListRegistryTagsContext(ctx, srv.URL, "x", "", "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want errors.Is(context.Canceled)", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("registry received %d request(s), want 0", n)
	}
}

// The lower-level variants take the context too; each one stops on a
// cancelled context without reaching the server.
func TestContextVariantsRefuseACancelledContext(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := map[string]func() error{
		"registryGETContext": func() error {
			_, _, _, err := registryGETContext(ctx, srv.URL+"/v2/x/tags/list", "", "", "")
			return err
		},
		"registryDoContext": func() error {
			_, _, err := registryDoContext(ctx, http.MethodGet, srv.URL, "", "", "", "")
			return err
		},
		"doRequestContext": func() error {
			_, err := doRequestContext(ctx, http.MethodGet, srv.URL, "", "", "", "")
			return err
		},
		"exchangeBearerTokenContext": func() error {
			_, err := exchangeBearerTokenContext(ctx, `realm="`+srv.URL+`/token"`, "", "")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want errors.Is(context.Canceled)", err)
			}
		})
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server received %d request(s), want 0", n)
	}
}

// Criterion 5: the context-free functions still compile and behave as before.
func TestContextFreeFunctionsKeepTheirBehavior(t *testing.T) {
	srv := bearerRegistry(t, okToken, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tags":["1","2"]}`))
	})

	tags, err := ListRegistryTags(srv.URL, "x", "", "")
	if err != nil || len(tags) != 2 {
		t.Fatalf("ListRegistryTags = %v, %v; want 2 tags", tags, err)
	}
	body, _, token, err := registryGET(srv.URL+"/v2/x/tags/list", "", "", "")
	if err != nil || token != "tok" || len(body) == 0 {
		t.Errorf("registryGET = %q, token %q, %v", body, token, err)
	}
	resp, token, err := registryDo(http.MethodGet, srv.URL+"/v2/x/tags/list", "", "", "", "")
	if err != nil || token != "tok" {
		t.Fatalf("registryDo token %q, %v", token, err)
	}
	_ = resp.Body.Close()
	resp, err = doRequest(http.MethodGet, srv.URL+"/v2/x/tags/list", "", "", "", "tok")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("doRequest = %v, %v", resp, err)
	}
	_ = resp.Body.Close()
	if tok, err := exchangeBearerToken(`realm="`+srv.URL+`/token"`, "", ""); err != nil || tok != "tok" {
		t.Errorf("exchangeBearerToken = %q, %v", tok, err)
	}
}
