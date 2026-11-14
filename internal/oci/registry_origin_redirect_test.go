package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DEVDESK-410 review round: refused redirects must error without echoing the
// target's path or query, the redirect cap and the round trip are pinned, and
// the token bound is exact.

// assertNoTargetLeak checks err is non-nil and carries nothing past the host.
func assertNoTargetLeak(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want a refused redirect error (silent follow regression)")
	}
	for _, leak := range []string{"SECRET", "sig=", "/blob"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q leaks %q from the redirect target", err, leak)
		}
	}
}

// signedTarget returns a Location on another host with a secret query.
func signedTarget(reg *httptest.Server) string {
	return "http://other.example:" + portOfServer(reg) + "/blob?sig=SECRET"
}

func TestRegistryRedirectRefusalDoesNotLeakTarget(t *testing.T) {
	setup := func(t *testing.T) (apiURL string, target *hitLog) {
		var hits hitLog
		var reg *httptest.Server
		reg = redirectingRegistry(t, false, func(string) string { return signedTarget(reg) })
		wrapped := reg.Config.Handler
		reg.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.Host, "other.") {
				hits.record(r)
				serveBoth(w, r)
				return
			}
			wrapped.ServeHTTP(w, r)
		})
		useClientTransport(t, dialTo(reg.Listener.Addr().String()))
		return "http://registry.test:" + portOfServer(reg), &hits
	}

	t.Run("tags", func(t *testing.T) {
		api, target := setup(t)
		_, err := ListRegistryTagsContext(context.Background(), api, "x", "user", "pass")
		assertNoTargetLeak(t, err)
		if !errors.Is(err, errOffOriginRedirect) {
			t.Errorf("err = %v, want errOffOriginRedirect", err)
		}
		if target.count() != 0 {
			t.Errorf("target hit %d times, want 0", target.count())
		}
	})
	t.Run("catalog", func(t *testing.T) {
		api, target := setup(t)
		_, _, err := ListCatalogContext(context.Background(), api, "user", "pass", 10, 1000)
		assertNoTargetLeak(t, err)
		if !errors.Is(err, errOffOriginRedirect) {
			t.Errorf("err = %v, want errOffOriginRedirect", err)
		}
		if target.count() != 0 {
			t.Errorf("target hit %d times, want 0", target.count())
		}
	})
	t.Run("token", func(t *testing.T) {
		tokenAPI, target := setup(t) // its every answer redirects off-origin
		reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Www-Authenticate", `Bearer realm="`+tokenAPI+`/token",service="s"`)
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer reg.Close()
		_, err := ListRegistryTagsContext(context.Background(), reg.URL, "x", "user", "pass")
		assertNoTargetLeak(t, err)
		if !errors.Is(err, errOffOriginRedirect) {
			t.Errorf("err = %v, want errOffOriginRedirect", err)
		}
		if target.count() != 0 {
			t.Errorf("target hit %d times, want 0", target.count())
		}
	})
}

// Same-origin redirects stop at the 10-redirect cap (net/http's default).
func TestRegistryRedirectLimit(t *testing.T) {
	hops := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		http.Redirect(w, r, fmt.Sprintf("/hop%d", hops), http.StatusFound)
	}))
	defer s.Close()
	_, err := ListRegistryTagsContext(context.Background(), s.URL, "x", "user", "pass")
	if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("err = %v, want \"stopped after 10 redirects\"", err)
	}
	// Same count as net/http's default policy: the original request plus nine
	// followed redirects; the tenth redirect is the one refused.
	if hops != 10 {
		t.Errorf("server saw %d requests, want 10", hops)
	}
}

// A -> A/x -> B -> A is refused at B, and B never sees a request.
func TestRegistryRedirectRoundTripRefused(t *testing.T) {
	var b hitLog
	var a, bs *httptest.Server
	bs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.record(r)
		http.Redirect(w, r, a.URL+"/back", http.StatusFound)
	}))
	defer bs.Close()
	a = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mid" {
			http.Redirect(w, r, bs.URL+"/away", http.StatusFound)
			return
		}
		if r.URL.Path == "/back" {
			serveBoth(w, r)
			return
		}
		http.Redirect(w, r, "/mid", http.StatusFound)
	}))
	defer a.Close()
	_, err := ListRegistryTagsContext(context.Background(), a.URL, "x", "user", "pass")
	if err == nil {
		t.Fatal("err = nil, want the round trip through another origin refused")
	}
	if b.count() != 0 || b.anyAuth() != "" {
		t.Errorf("other origin saw %d requests, auth %q; want none", b.count(), b.anyAuth())
	}
}

// tokenRegistry serves a token endpoint answering tokenBody.
func tokenRegistry(t *testing.T, tokenBody string) string {
	t.Helper()
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, tokenBody)
	}))
	t.Cleanup(tokenSrv.Close)
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer tok" {
			_, _ = fmt.Fprint(w, `{"tags":["a"]}`)
			return
		}
		w.Header().Set("Www-Authenticate", `Bearer realm="`+tokenSrv.URL+`/token",service="s"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(reg.Close)
	return reg.URL
}

// A token body of exactly 1 MiB is accepted; one byte more is refused.
func TestExchangeBearerTokenExactBound(t *testing.T) {
	const bound = 1 << 20
	pad := func(total int) string {
		const head, tail = `{"token":"tok","pad":"`, `"}`
		return head + strings.Repeat("a", total-len(head)-len(tail)) + tail
	}
	atBound, overBound := pad(bound), pad(bound+1)
	if len(atBound) != bound || len(overBound) != bound+1 {
		t.Fatalf("fixture sizes %d, %d", len(atBound), len(overBound))
	}
	t.Run("exactly 1 MiB is accepted", func(t *testing.T) {
		got, err := ListRegistryTagsContext(context.Background(), tokenRegistry(t, atBound), "x", "user", "pass")
		if err != nil || strings.Join(got, ",") != "a" {
			t.Fatalf("tags = %v, err = %v, want [a]", got, err)
		}
	})
	t.Run("1 MiB plus one byte is refused", func(t *testing.T) {
		if _, err := ListRegistryTagsContext(context.Background(), tokenRegistry(t, overBound), "x", "user", "pass"); err == nil {
			t.Fatal("err = nil, want an error over the bound")
		}
	})
}
