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

// DEVDESK-412 step 1: an https registry that announces a Bearer realm which is
// not https is refused before any request goes to that realm, with or without
// credentials (decision Q3). The error names the realm's host at most: no
// password, no realm path or query.

const realmTestPassword = "S3CRETPW"

// registryOp is one public entry point that walks the Bearer flow.
type registryOp struct {
	name string
	run  func(apiURL, user, pass string) error
}

func registryOps() []registryOp {
	return []registryOp{
		{"ListRegistryTagsContext", func(api, user, pass string) error {
			_, err := ListRegistryTagsContext(context.Background(), api, "x", user, pass)
			return err
		}},
		{"ListCatalogContext", func(api, user, pass string) error {
			_, _, err := ListCatalogContext(context.Background(), api, user, pass, 10, 1000)
			return err
		}},
		{"ManifestDigest", func(api, user, pass string) error {
			_, err := ManifestDigest(api, "x", "latest", user, pass)
			return err
		}},
	}
}

// realmCreds are the two credential states every refusal must hold in.
var realmCreds = []struct{ name, user, pass string }{
	{"with credentials", "user", realmTestPassword},
	{"without credentials", "", ""},
}

// tokenHandler answers every request with a valid token and records it.
func tokenHandler(hits *hitLog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hits.record(r)
		_, _ = fmt.Fprint(w, `{"token":"tok"}`)
	}
}

// bearerFixture is a registry that demands a Bearer token from its realm.
type bearerFixture struct {
	apiURL    string
	regHits   *hitLog // requests the registry received (token path excluded)
	realmHits *hitLog // requests the token endpoints received
}

// newBearerRegistry starts a registry (TLS or plain) that answers 401 with a
// Bearer challenge until it gets "Bearer tok", then serves a tag list, a
// catalog page or a manifest digest. realm builds the challenge's realm URL
// from the registry's own URL. A request to the registry's own /token path is
// answered as a token endpoint (the same-origin realm case). When any server
// of a test is TLS, the client trusts httptest's shared certificate.
func newBearerRegistry(t *testing.T, tlsRegistry bool, realm func(self string) string, realmHits *hitLog, rt http.RoundTripper) bearerFixture {
	t.Helper()
	regHits := &hitLog{}
	var reg *httptest.Server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenHandler(realmHits)(w, r)
			return
		}
		regHits.record(r)
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.Header().Set("Www-Authenticate", `Bearer realm="`+realm(reg.URL)+`",service="s",scope="repository:x:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/_catalog"):
			_, _ = fmt.Fprint(w, catalogBody("a"))
		case strings.Contains(r.URL.Path, "/manifests/"):
			w.Header().Set("Docker-Content-Digest", "sha256:abc")
		default:
			_, _ = fmt.Fprint(w, `{"tags":["a"]}`)
		}
	})
	if tlsRegistry {
		reg = httptest.NewTLSServer(handler)
		rt = reg.Client().Transport
	} else {
		reg = httptest.NewServer(handler)
	}
	t.Cleanup(reg.Close)
	if rt != nil {
		useClientTransport(t, rt)
	}
	return bearerFixture{apiURL: reg.URL, regHits: regHits, realmHits: realmHits}
}

// newTokenServer starts a token endpoint (TLS or plain) and returns its URL,
// its log and, for a TLS one, a transport that trusts it.
func newTokenServer(t *testing.T, useTLS bool) (url string, hits *hitLog, rt http.RoundTripper) {
	t.Helper()
	hits = &hitLog{}
	var s *httptest.Server
	if useTLS {
		s = httptest.NewTLSServer(tokenHandler(hits))
		rt = s.Client().Transport
	} else {
		s = httptest.NewServer(tokenHandler(hits))
	}
	t.Cleanup(s.Close)
	return s.URL, hits, rt
}

// assertNoRealmLeak fails when err carries a password or the realm's path.
func assertNoRealmLeak(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	for _, leak := range []string{realmTestPassword, "/token"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q leaks %q", err, leak)
		}
	}
}

func TestInsecureBearerRealmRefusedBeforeAnyRequest(t *testing.T) {
	for _, op := range registryOps() {
		for _, cred := range realmCreds {
			t.Run(op.name+"/"+cred.name, func(t *testing.T) {
				realmURL, realmHits, _ := newTokenServer(t, false)
				fx := newBearerRegistry(t, true, func(string) string { return realmURL + "/token" }, realmHits, nil)

				err := op.run(fx.apiURL, cred.user, cred.pass)

				if !errors.Is(err, errInsecureTokenRealm) {
					t.Errorf("err = %v, want errInsecureTokenRealm (the request to the http realm went out)", err)
				}
				if n := realmHits.count(); n != 0 {
					t.Errorf("http realm received %d requests, want 0 (auth %q)", n, realmHits.anyAuth())
				}
				if n := fx.regHits.count(); n != 1 {
					t.Errorf("registry received %d requests, want only the first one", n)
				}
				assertNoRealmLeak(t, err)
			})
		}
	}
}

// A realm that is neither http nor https is refused too, whatever the
// registry's scheme; nothing is sent anywhere.
func TestNonHTTPBearerRealmRefused(t *testing.T) {
	for _, realm := range []string{"ftp://auth.example/token", "file:///token"} {
		for _, op := range registryOps() {
			for _, cred := range realmCreds {
				t.Run(realm+"/"+op.name+"/"+cred.name, func(t *testing.T) {
					realmHits := &hitLog{}
					fx := newBearerRegistry(t, true, func(string) string { return realm }, realmHits, nil)

					err := op.run(fx.apiURL, cred.user, cred.pass)

					if !errors.Is(err, errInsecureTokenRealm) {
						t.Errorf("err = %v, want errInsecureTokenRealm", err)
					}
					if n := fx.regHits.count(); n != 1 {
						t.Errorf("registry received %d requests, want only the first one", n)
					}
					assertNoRealmLeak(t, err)
				})
			}
		}
	}
}

// Realms that stay acceptable: https on the registry's origin or on another
// host, and anything over an http registry (the flow as before step 1).
func TestBearerRealmStillAccepted(t *testing.T) {
	for _, tt := range []struct {
		name        string
		tlsRegistry bool
		realm       string // "same", "tls", "plain"
	}{
		{"https registry, https realm on the same origin", true, "same"},
		{"https registry, https realm on another host", true, "tls"},
		{"http registry, https realm", false, "tls"},
		{"http registry, http realm", false, "plain"},
	} {
		for _, op := range registryOps() {
			for _, cred := range realmCreds {
				t.Run(tt.name+"/"+op.name+"/"+cred.name, func(t *testing.T) {
					var (
						realm func(self string) string
						hits  *hitLog
						rt    http.RoundTripper
					)
					switch tt.realm {
					case "same":
						hits = &hitLog{}
						realm = func(self string) string { return self + "/token" }
					default:
						var url string
						url, hits, rt = newTokenServer(t, tt.realm == "tls")
						realm = func(string) string { return url + "/token" }
					}
					fx := newBearerRegistry(t, tt.tlsRegistry, realm, hits, rt)

					if err := op.run(fx.apiURL, cred.user, cred.pass); err != nil {
						t.Fatalf("err = %v, want the walk to succeed", err)
					}
					if n := hits.count(); n != 1 {
						t.Errorf("realm received %d requests, want 1", n)
					}
					if cred.user != "" && !strings.HasPrefix(hits.anyAuth(), "Basic ") {
						t.Errorf("realm Authorization = %q, want the Basic credentials as before", hits.anyAuth())
					}
				})
			}
		}
	}
}

func TestCheckTokenRealm(t *testing.T) {
	const (
		httpsReg = "https://registry.example:5000"
		httpReg  = "http://registry.example:5000"
	)
	for _, tt := range []struct {
		name      string
		registry  string
		challenge string
		refused   bool
		hostShown string // host the error must name, when refused
	}{
		{"https registry, http realm", httpsReg, `realm="http://auth.example/token",service="s"`, true, "auth.example"},
		{"https registry, http realm on its own host", httpsReg, `realm="http://registry.example:5001/token"`, true, "registry.example:5001"},
		{"https registry, upper-case HTTP scheme", httpsReg, `realm="HTTP://auth.example/token"`, true, "auth.example"},
		{"https registry, http realm with userinfo", httpsReg, `realm="http://alice:` + realmTestPassword + `@auth.example/token"`, true, "auth.example"},
		{"https registry, http realm with a query", httpsReg, `realm="http://auth.example/token?sig=SECRET"`, true, "auth.example"},
		{"https registry, ftp realm", httpsReg, `realm="ftp://auth.example/token"`, true, "auth.example"},
		{"https registry, file realm", httpsReg, `realm="file:///token"`, true, ""},
		{"https registry, relative realm", httpsReg, `realm="/token"`, true, ""},
		{"https registry, scheme-relative realm", httpsReg, `realm="//auth.example/token"`, true, "auth.example"},
		{"https registry, realm without a scheme", httpsReg, `realm="auth.example/token"`, true, ""},
		{"http registry, relative realm", httpReg, `realm="/token"`, true, ""},
		{"http registry, scheme-relative realm", httpReg, `realm="//auth.example/token"`, true, "auth.example"},
		{"unreadable registry URL, http realm", "://bad", `realm="http://auth.example/token"`, true, "auth.example"},
		{"http registry, ftp realm", httpReg, `realm="ftp://auth.example/token"`, true, "auth.example"},
		{"https registry, same-origin https realm", httpsReg, `realm="https://registry.example:5000/token"`, false, ""},
		{"https registry, https realm on another host", httpsReg, `realm="https://auth.docker.io/token",service="registry.docker.io"`, false, ""},
		{"https registry, upper-case HTTPS scheme", httpsReg, `realm="HTTPS://auth.example/token"`, false, ""},
		{"http registry, http realm", httpReg, `realm="http://auth.example/token"`, false, ""},
		{"http registry, https realm", httpReg, `realm="https://auth.example/token"`, false, ""},
		// The existing errors of exchangeBearerTokenContext stay the ones
		// reported for these: the check passes them through.
		{"https registry, no realm", httpsReg, `service="s"`, false, ""},
		{"https registry, unparsable realm", httpsReg, `realm="%zz"`, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := checkTokenRealm(tt.registry, tt.challenge)
			if !tt.refused {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, errInsecureTokenRealm) {
				t.Fatalf("err = %v, want errInsecureTokenRealm", err)
			}
			if tt.hostShown != "" && !strings.Contains(err.Error(), tt.hostShown) {
				t.Errorf("err = %q, want the host %q named", err, tt.hostShown)
			}
			for _, leak := range []string{realmTestPassword, "alice", "/token", "sig=", "SECRET"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("err = %q leaks %q", err, leak)
				}
			}
		})
	}
}
