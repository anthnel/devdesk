package registrymgr

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-412 step 6: the Nexus detector keeps its credentials on the origin they
// were meant for (a redirect to another origin is refused, the anonymous retry
// included) and bounds every response it reads. Server A is Nexus. The other
// origin is either server B (another port of the same host) or a sub-domain of
// A's host name; it answers with a group that has a member, so a redirect that
// was followed is visible in the result as well as in its log.

const (
	nexusTestUser = "user"
	nexusTestPass = "S3CRETPW"
	// The paths of the two Nexus endpoints the detector asks for.
	nexusMetaPath   = "/service/rest/v1/repositories/docker-group"
	nexusFormatPath = "/service/rest/v1/repositories/docker/group/docker-group"
	// nexusLeakPath is where a redirect points; it must appear in no error.
	nexusLeakPath = "/leak-target"
	nexusSigned   = "sig=SECRET"
	// nexusGroupNoMembers is the generic endpoint's answer for a group that
	// does not list its members (the format endpoint is asked next).
	nexusGroupNoMembers = `{"type":"group","format":"docker"}`
	nexusGroupMembers   = `{"type":"group","format":"docker","group":{"memberNames":["a-proxy","b-hosted"]}}`
	nexusLeakedMembers  = `{"type":"group","format":"docker","group":{"memberNames":["leaked"]}}`
)

// nexusHits records every request a test server receives.
type nexusHits struct {
	mu    sync.Mutex
	paths []string
	auths []string
}

func (h *nexusHits) record(r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paths = append(h.paths, r.URL.RequestURI())
	h.auths = append(h.auths, r.Header.Get("Authorization"))
}

func (h *nexusHits) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.paths)
}

// authsFor returns the Authorization header of every request whose URI contains part.
func (h *nexusHits) authsFor(part string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for i, p := range h.paths {
		if strings.Contains(p, part) {
			out = append(out, h.auths[i])
		}
	}
	return out
}

func (h *nexusHits) anyAuth() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, a := range h.auths {
		if a != "" {
			return a
		}
	}
	return ""
}

// nexusBasic is the Authorization header the detector sends for the test account.
func nexusBasic(t *testing.T) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://x", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(nexusTestUser, nexusTestPass)
	return req.Header.Get("Authorization")
}

// nexusLog sends the standard logger to a buffer for the test and restores it.
func nexusLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

// dialTo sends every connection to addr whatever host name was asked for.
func dialTo(addr string) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

// useNexusTransport swaps nexusHTTPClient for a copy of itself with another
// transport, so the redirect policy and the timeout of the production client
// stay in force.
func useNexusTransport(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	old := nexusHTTPClient
	c := *old
	c.Transport = rt
	nexusHTTPClient = &c
	t.Cleanup(func() { nexusHTTPClient = old })
}

// nexusEnv is one test setup: the registry's address, the logs of both origins
// and where a redirect has to point to leave the registry's origin.
type nexusEnv struct {
	infoURL string // RegistryInfo.URL
	other   string // base URL of the other origin
	a, b    *nexusHits
}

func (e *nexusEnv) info() RegistryInfo {
	return RegistryInfo{Provider: ProviderNexus, URL: e.infoURL, Username: nexusTestUser, Password: nexusTestPass}
}

// otherOrigins are the three ways a redirect leaves the origin: another port,
// another host name, another scheme (same host and port, https instead of http).
var otherOrigins = []string{"other port", "sub-domain", "other scheme"}

// redirectStatuses are the redirect statuses every off-origin test runs under:
// 302 and 307 differ in how net/http rebuilds the request, not in the origin rule.
var redirectStatuses = []int{http.StatusFound, http.StatusTemporaryRedirect}

// startNexus starts Nexus (handler gets the base URL of the other origin) and the
// other origin, which answers every request with nexusLeakedMembers.
func startNexus(t *testing.T, kind string, handler func(w http.ResponseWriter, r *http.Request, other string)) *nexusEnv {
	t.Helper()
	env := &nexusEnv{a: &nexusHits{}, b: &nexusHits{}}
	serveOther := func(w http.ResponseWriter, r *http.Request) {
		env.b.record(r)
		_, _ = io.WriteString(w, nexusLeakedMembers)
	}

	switch kind {
	case "other port":
		b := httptest.NewServer(http.HandlerFunc(serveOther))
		t.Cleanup(b.Close)
		a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			env.a.record(r)
			handler(w, r, b.URL)
		}))
		t.Cleanup(a.Close)
		env.infoURL, env.other = a.URL+"/repository/docker-group", b.URL
	case "sub-domain":
		// One listener answers both names; the Host tells them apart. They are
		// two origins for net/http (and it would copy Authorization to the
		// sub-domain).
		var s *httptest.Server
		s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.Host, "cdn.") {
				serveOther(w, r)
				return
			}
			env.a.record(r)
			_, port, _ := net.SplitHostPort(s.Listener.Addr().String())
			handler(w, r, "http://cdn.nexus.test:"+port)
		}))
		t.Cleanup(s.Close)
		_, port, _ := net.SplitHostPort(s.Listener.Addr().String())
		useNexusTransport(t, dialTo(s.Listener.Addr().String()))
		env.infoURL, env.other = "http://nexus.test:"+port+"/repository/docker-group", "http://cdn.nexus.test:"+port
	case "other scheme":
		// Nexus speaks plain http on port P; the redirect points to https on the
		// same host and port. Dials are split by scheme: http goes to Nexus, https
		// goes to a TLS server B whose certificate the client trusts, so a redirect
		// that was followed would reach B and show in its log. The refusal has to
		// come from the origin check, before any dial.
		b := httptest.NewTLSServer(http.HandlerFunc(serveOther))
		t.Cleanup(b.Close)
		var a *httptest.Server
		a = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			env.a.record(r)
			_, port, _ := net.SplitHostPort(a.Listener.Addr().String())
			handler(w, r, "https://nexus.test:"+port)
		}))
		t.Cleanup(a.Close)
		_, port, _ := net.SplitHostPort(a.Listener.Addr().String())
		pool := x509.NewCertPool()
		pool.AddCert(b.Certificate())
		useNexusTransport(t, &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, a.Listener.Addr().String())
			},
			DialTLSContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				d := tls.Dialer{Config: &tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS12}}
				return d.DialContext(ctx, network, b.Listener.Addr().String())
			},
		})
		env.infoURL, env.other = "http://nexus.test:"+port+"/repository/docker-group", "https://nexus.test:"+port
	default:
		t.Fatalf("unknown origin kind %q", kind)
	}
	return env
}

// wantNoSecret fails when s (an error or a log) holds the password, the signed
// query or the path a redirect pointed to.
func wantNoSecret(t *testing.T, what, s string) {
	t.Helper()
	for _, leak := range []string{nexusTestPass, "SECRET", nexusSigned, nexusLeakPath} {
		if strings.Contains(s, leak) {
			t.Errorf("%s leaks %q: %s", what, leak, s)
		}
	}
}

// forEachOffOriginRedirect runs fn as a subtest for every other origin under
// every redirect status.
func forEachOffOriginRedirect(t *testing.T, fn func(t *testing.T, kind string, status int)) {
	t.Helper()
	for _, kind := range otherOrigins {
		for _, status := range redirectStatuses {
			t.Run(fmt.Sprintf("%s/%d", kind, status), func(t *testing.T) { fn(t, kind, status) })
		}
	}
}

// 1. The generic endpoint redirected to another origin: refused, with the shared
// error of the other registry clients, and the credentials go nowhere.
func TestNexusMetaRedirectOffOriginIsRefused(t *testing.T) {
	forEachOffOriginRedirect(t, func(t *testing.T, kind string, status int) {
		logs := nexusLog(t)
		env := startNexus(t, kind, func(w http.ResponseWriter, r *http.Request, other string) {
			http.Redirect(w, r, other+nexusLeakPath+"?"+nexusSigned, status)
		})

		members, err := (&NexusDetector{}).DetectGroup(context.Background(), env.info())

		if !errors.Is(err, oci.ErrOffOriginRedirect) {
			t.Fatalf("err = %v, want errors.Is oci.ErrOffOriginRedirect (the manager could not be asked)", err)
		}
		if len(members) != 0 {
			t.Errorf("members = %v, want none", members)
		}
		if n := env.b.count(); n != 0 {
			t.Errorf("the other origin received %d requests, want 0", n)
		}
		if got := env.b.anyAuth(); got != "" {
			t.Errorf("the other origin saw Authorization %q", got)
		}
		if got := env.a.authsFor(nexusMetaPath); len(got) != 1 || got[0] != nexusBasic(t) {
			t.Errorf("Nexus Authorization headers = %q, want one Basic header", got)
		}
		wantNoSecret(t, "error", err.Error())
		wantNoSecret(t, "log", logs.String())
	})
}

// 2. The format endpoint redirected to another origin: the refusal ends the
// detection. There is no anonymous retry after a refused redirect (the story puts
// a silent anonymous retry out of scope): Nexus sees one request with the
// credentials, the other origin none, and the log names the refusal without any
// signed path.
func TestNexusFormatEndpointRedirectOffOriginIsRefused(t *testing.T) {
	forEachOffOriginRedirect(t, func(t *testing.T, kind string, status int) {
		logs := nexusLog(t)
		env := startNexus(t, kind, func(w http.ResponseWriter, r *http.Request, other string) {
			if !strings.Contains(r.URL.Path, "/group/") {
				_, _ = io.WriteString(w, nexusGroupNoMembers)
				return
			}
			http.Redirect(w, r, other+nexusLeakPath+"?"+nexusSigned, status)
		})

		members, err := (&NexusDetector{}).DetectGroup(context.Background(), env.info())

		if len(members) != 0 {
			t.Errorf("members = %v, want none", members)
		}
		if err != nil && !errors.Is(err, oci.ErrOffOriginRedirect) {
			t.Errorf("err = %v, want nil or errors.Is oci.ErrOffOriginRedirect", err)
		}
		if n := env.b.count(); n != 0 {
			t.Errorf("the other origin received %d requests, want 0", n)
		}
		if got := env.a.authsFor("/group/"); len(got) != 1 || got[0] != nexusBasic(t) {
			t.Errorf("Nexus saw Authorization %q on the format endpoint, want [Basic] only (no anonymous retry)", got)
		}
		if !strings.Contains(logs.String(), oci.ErrOffOriginRedirect.Error()) {
			t.Errorf("the log does not mention the origin refusal: %s", logs.String())
		}
		wantNoSecret(t, "log", logs.String())
		if err != nil {
			wantNoSecret(t, "error", err.Error())
		}
	})
}

// 2b. The anonymous retry stays for its own case: Nexus itself refuses the
// account (403) and answers the anonymous request.
func TestNexusAnonymousRetryAfterARefusedAccountStays(t *testing.T) {
	env := startNexus(t, "other port", func(w http.ResponseWriter, r *http.Request, _ string) {
		switch {
		case !strings.Contains(r.URL.Path, "/group/"):
			_, _ = io.WriteString(w, nexusGroupNoMembers)
		case r.Header.Get("Authorization") != "":
			w.WriteHeader(http.StatusForbidden)
		default:
			_, _ = io.WriteString(w, nexusGroupMembers)
		}
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), env.info())

	if err != nil {
		t.Fatalf("DetectGroup: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("members = %v, want the 2 of the anonymous answer", members)
	}
	got := env.a.authsFor("/group/")
	if len(got) != 2 || got[0] != nexusBasic(t) || got[1] != "" {
		t.Errorf("Nexus saw Authorization %q on the format endpoint, want [Basic, none]", got)
	}
	if n := env.b.count(); n != 0 {
		t.Errorf("the other origin received %d requests, want 0", n)
	}
}

// 3. A redirect that stays on Nexus is followed and keeps the credentials.
func TestNexusSameOriginRedirectIsFollowedWithCredentials(t *testing.T) {
	cases := []struct {
		name string
		// generic is the generic endpoint's answer when it is not redirected:
		// no members means the format endpoint is the one that gets redirected.
		generic string
		moved   string // path part of the second hop that must carry Authorization
	}{
		{"generic endpoint", nexusGroupMembers, "/moved-meta"},
		{"format endpoint", nexusGroupNoMembers, "/moved-format"},
	}
	for _, tc := range cases {
		for _, status := range redirectStatuses {
			t.Run(fmt.Sprintf("%s/%d", tc.name, status), func(t *testing.T) {
				var s *httptest.Server
				a := &nexusHits{}
				s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					a.record(r)
					switch {
					case strings.HasPrefix(r.URL.Path, "/moved-meta"):
						_, _ = io.WriteString(w, nexusGroupMembers)
					case strings.HasPrefix(r.URL.Path, "/moved-format"):
						_, _ = io.WriteString(w, nexusGroupMembers)
					case strings.Contains(r.URL.Path, "/group/"):
						http.Redirect(w, r, "/moved-format", status)
					default:
						if tc.generic == nexusGroupMembers {
							http.Redirect(w, r, "/moved-meta", status)
							return
						}
						_, _ = io.WriteString(w, tc.generic)
					}
				}))
				defer s.Close()
				info := RegistryInfo{Provider: ProviderNexus, URL: s.URL + "/repository/docker-group", Username: nexusTestUser, Password: nexusTestPass}

				members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

				if err != nil {
					t.Fatalf("DetectGroup: %v", err)
				}
				if len(members) != 2 {
					t.Fatalf("members = %v, want the 2 of the redirected answer", members)
				}
				if got := a.authsFor(tc.moved); len(got) != 1 || got[0] != nexusBasic(t) {
					t.Errorf("Authorization on %s = %q, want one Basic header", tc.moved, got)
				}
			})
		}
	}
}

// 3 (anonymous retry). The authenticated request fails, the anonymous retry is
// redirected to another origin: that is refused too, and the retry itself carries
// no credential.
func TestNexusAnonymousRetryDoesNotFollowAnOffOriginRedirect(t *testing.T) {
	forEachOffOriginRedirect(t, func(t *testing.T, kind string, status int) {
		logs := nexusLog(t)
		env := startNexus(t, kind, func(w http.ResponseWriter, r *http.Request, other string) {
			switch {
			case !strings.Contains(r.URL.Path, "/group/"):
				_, _ = io.WriteString(w, nexusGroupNoMembers)
			case r.Header.Get("Authorization") != "":
				// The account lacks the privilege: the retry starts.
				w.WriteHeader(http.StatusForbidden)
			default:
				http.Redirect(w, r, other+nexusLeakPath+"?"+nexusSigned, status)
			}
		})

		members, err := (&NexusDetector{}).DetectGroup(context.Background(), env.info())

		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if len(members) != 0 {
			t.Errorf("members = %v, want none (the anonymous redirect was followed)", members)
		}
		if n := env.b.count(); n != 0 {
			t.Errorf("the other origin received %d requests, want 0", n)
		}
		got := env.a.authsFor("/group/")
		if len(got) != 2 || got[0] != nexusBasic(t) || got[1] != "" {
			t.Errorf("Nexus saw Authorization %q on the format endpoint, want [Basic, none]", got)
		}
		wantNoSecret(t, "log", logs.String())
	})
}

// padded returns body followed by spaces up to exactly size bytes (valid JSON).
func padded(t *testing.T, body string, size int) string {
	t.Helper()
	if len(body) > size {
		t.Fatalf("body (%d bytes) is longer than %d", len(body), size)
	}
	return body + strings.Repeat(" ", size-len(body))
}

// writeBounded writes at most limit bytes of s and stops: no test server ever
// serves more than the few MiB a test asks for.
func writeBounded(w http.ResponseWriter, s string, limit int) {
	if len(s) > limit {
		s = s[:limit]
	}
	_, _ = io.WriteString(w, s)
}

func TestNexusResponseCapIsOneMiB(t *testing.T) {
	if maxNexusResponse != 1<<20 {
		t.Errorf("maxNexusResponse = %d, want 1 MiB", maxNexusResponse)
	}
}

// 4. A response over the cap is an error, never a silent cut; one exactly at the
// cap is read. The oversized bodies start with a complete JSON document, so a
// reader that stopped at the cap would succeed.
func TestNexusResponseOverTheCapIsAnError(t *testing.T) {
	const maxServed = 3 << 20 // no test server serves more than this

	t.Run("generic endpoint", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			size int
			want error // nil = read
		}{
			{"exactly the cap", maxNexusResponse, nil},
			{"one byte over", maxNexusResponse + 1, oci.ErrBodyTooLarge},
			{"far over (2.5 MiB)", 5 << 19, oci.ErrBodyTooLarge},
		} {
			t.Run(tc.name, func(t *testing.T) {
				body := padded(t, nexusGroupMembers, tc.size)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					writeBounded(w, body, maxServed)
				}))
				defer srv.Close()
				info := RegistryInfo{Provider: ProviderNexus, URL: srv.URL + "/repository/docker-group", Username: nexusTestUser, Password: nexusTestPass}

				members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

				if tc.want == nil {
					if err != nil || len(members) != 2 {
						t.Fatalf("DetectGroup = %v, %v, want 2 members and no error", members, err)
					}
					return
				}
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want errors.Is %v", err, tc.want)
				}
				if len(members) != 0 {
					t.Errorf("members = %v, want none (no silent cut)", members)
				}
				wantNoSecret(t, "error", err.Error())
			})
		}
	})

	t.Run("format endpoint", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			size int
			want int // members
		}{
			{"exactly the cap", maxNexusResponse, 2},
			{"one byte over", maxNexusResponse + 1, 0},
			{"far over (2.5 MiB)", 5 << 19, 0},
		} {
			t.Run(tc.name, func(t *testing.T) {
				logs := nexusLog(t)
				body := padded(t, nexusGroupMembers, tc.size)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !strings.Contains(r.URL.Path, "/group/") {
						_, _ = io.WriteString(w, nexusGroupNoMembers)
						return
					}
					writeBounded(w, body, maxServed)
				}))
				defer srv.Close()
				info := RegistryInfo{Provider: ProviderNexus, URL: srv.URL + "/repository/docker-group", Username: nexusTestUser, Password: nexusTestPass}

				members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

				if err != nil {
					t.Fatalf("err = %v, want nil (an unreadable format endpoint only means no members)", err)
				}
				if len(members) != tc.want {
					t.Errorf("members = %d, want %d", len(members), tc.want)
				}
				if tc.want == 0 && !strings.Contains(logs.String(), "members unreachable") {
					t.Errorf("an oversized answer left no trace in the log: %s", logs.String())
				}
				wantNoSecret(t, "log", logs.String())
			})
		}
	})
}

// 5. The usual answers are read as before, with or without credentials.
func TestNexusUsualAnswersAreUnchanged(t *testing.T) {
	for _, withAuth := range []bool{true, false} {
		t.Run(fmt.Sprintf("auth=%v", withAuth), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/group/"):
					_, _ = io.WriteString(w, nexusGroupMembers)
				default:
					_, _ = io.WriteString(w, nexusGroupNoMembers)
				}
			}))
			defer srv.Close()
			info := RegistryInfo{Provider: ProviderNexus, URL: srv.URL + "/repository/docker-group"}
			if withAuth {
				info.Username, info.Password = nexusTestUser, nexusTestPass
			}

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			if err != nil {
				t.Fatalf("DetectGroup: %v", err)
			}
			if len(members) != 2 || members[0].Alias != "a" || members[1].Alias != "b" {
				t.Errorf("members = %+v, want a and b", members)
			}
		})
	}
}

// 6. The redirect policy is the shared one of the other registry clients, and it
// is the client of the detector that carries it.
func TestNexusClientIsBounded(t *testing.T) {
	if nexusHTTPClient.CheckRedirect == nil {
		t.Fatal("nexusHTTPClient.CheckRedirect = nil, want the origin guard")
	}
	first, _ := http.NewRequest(http.MethodGet, "http://nexus.test/service/rest/v1/repositories/x", nil)
	other, _ := http.NewRequest(http.MethodGet, "http://cdn.test/x", nil)
	same, _ := http.NewRequest(http.MethodGet, "http://nexus.test/elsewhere", nil)
	if err := nexusHTTPClient.CheckRedirect(other, []*http.Request{first}); !errors.Is(err, oci.ErrOffOriginRedirect) {
		t.Errorf("CheckRedirect(other origin) = %v, want oci.ErrOffOriginRedirect", err)
	}
	if err := nexusHTTPClient.CheckRedirect(same, []*http.Request{first}); err != nil {
		t.Errorf("CheckRedirect(same origin) = %v, want nil", err)
	}
	if nexusHTTPClient.Timeout <= 0 {
		t.Errorf("nexusHTTPClient.Timeout = %v, want a finite timeout", nexusHTTPClient.Timeout)
	}
}
