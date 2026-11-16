package oci

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DEVDESK-412 step 8 (group A): an apiURL written with its password
// (https://user:pass@host) never puts that password in an error. net/http masks
// the userinfo of the *url.Error it returns, but the prefixes added by this
// package ("GET <url>: ...", "GET (retry) <url>: ...", "build request: ...") used
// to repeat the raw URL.

// hangUp closes the connection with no response: a deterministic transport
// failure, with no closed port to reuse.
func hangUp(w http.ResponseWriter, _ *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

// withPassword inserts alice:S3CRETPW@ in the userinfo of an http URL.
func withPassword(serverURL string) string {
	return strings.Replace(serverURL, "http://", "http://alice:"+realmTestPassword+"@", 1)
}

func wantNoPassword(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want a failure (the test would check nothing)")
	}
	if strings.Contains(err.Error(), realmTestPassword) {
		t.Errorf("error %q holds the password", err)
	}
}

// The request fails after the connection is made: the prefix of the error names
// the URL.
func TestRegistryErrorPrefixHidesThePasswordWhenTheServerHangsUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(hangUp))
	t.Cleanup(srv.Close)

	for _, op := range registryOps() {
		t.Run(op.name, func(t *testing.T) {
			wantNoPassword(t, op.run(withPassword(srv.URL), "", ""))
		})
	}
}

// The request fails before anything is sent (a context already done).
func TestRegistryErrorPrefixHidesThePasswordOnACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := withPassword("http://registry.example:5000")

	t.Run("doRequestContext", func(t *testing.T) {
		_, err := doRequestContext(ctx, http.MethodGet, api+"/v2/x/tags/list", "", "", "", "")
		wantNoPassword(t, err)
	})
	t.Run("registryDoContext", func(t *testing.T) {
		_, _, err := registryDoContext(ctx, http.MethodGet, api+"/v2/x/tags/list", "", "", "", "")
		wantNoPassword(t, err)
	})
	t.Run("ListRegistryTagsContext", func(t *testing.T) {
		_, err := ListRegistryTagsContext(ctx, api, "x", "", "")
		wantNoPassword(t, err)
	})
	t.Run("ListCatalogContext", func(t *testing.T) {
		_, _, err := ListCatalogContext(ctx, api, "", "", 10, 1000)
		wantNoPassword(t, err)
	})
}

// The "(retry)" prefix: the first answer is a Bearer challenge, the token is
// given, and the second request fails.
func TestRegistryRetryPrefixHidesThePassword(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = fmt.Fprint(w, `{"token":"tok"}`)
		case r.Header.Get("Authorization") == "Bearer tok":
			hangUp(w, r)
		default:
			w.Header().Set("Www-Authenticate", `Bearer realm="`+srv.URL+`/token"`)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)

	t.Run("registryDoContext", func(t *testing.T) {
		_, _, err := registryDoContext(context.Background(), http.MethodGet, withPassword(srv.URL)+"/v2/x/tags/list", "", "", "", "")
		wantNoPassword(t, err)
		if !strings.Contains(err.Error(), "(retry)") {
			t.Errorf("error %q does not come from the retry path, the test checks nothing", err)
		}
	})
	t.Run("ListRegistryTagsContext", func(t *testing.T) {
		_, err := ListRegistryTagsContext(context.Background(), withPassword(srv.URL), "x", "", "")
		wantNoPassword(t, err)
	})
}

// A URL that cannot be read fails when the request is built, and that error
// used to hold the whole raw URL. Nothing is sent: the parse fails first.
func TestRegistryBuildRequestErrorHidesThePassword(t *testing.T) {
	const unreadable = "http://alice:" + realmTestPassword + "@registry.example:5000/%zz"

	t.Run("doRequestContext", func(t *testing.T) {
		_, err := doRequestContext(context.Background(), http.MethodGet, unreadable, "", "", "", "")
		wantNoPassword(t, err)
	})
	t.Run("registryDoContext", func(t *testing.T) {
		_, _, err := registryDoContext(context.Background(), http.MethodGet, unreadable, "", "", "", "")
		wantNoPassword(t, err)
	})
	for _, op := range registryOps() {
		t.Run(op.name, func(t *testing.T) {
			wantNoPassword(t, op.run("http://alice:"+realmTestPassword+"@registry.example:5000/%zz", "", ""))
		})
	}
}
