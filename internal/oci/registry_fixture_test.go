package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
)

// DEVDESK-580 (NX-08b, PR4, NX-05): how the registry client authenticates and
// whether it ever meets a redirect, replayed from the answers MEASURED on Nexus
// 3.98.1-01 (the `access` campaign, with a few of `catalog`), through fakenexus.
// Every expectation is what a fixture says or what the client does with it; a
// measurement that contradicts a criterion of NX-08b or the code is pinned as
// measured, never fixed here (docs/measurements/2026-11-20-nexus-access.md).
//
// This is an internal test (package oci) on purpose: the realm of the X-Forwarded
// probe is https://realm-probe.invalid, which the client WOULD resolve, so the
// transport of registryHTTPClient is swapped (useClientTransport) to log every
// request and to answer that one host without touching the network.
//
// The verdict "refused" that a 401 turns into is imagesearch.Classify's: the
// package imagesearch imports this one, so it cannot be named here. That half is in
// internal/imagesearch/access_fixture_test.go.

const (
	nxPathHost      = "127.0.0.1:30481" // the HTTP port: repositories by path
	nxConnectorHost = "127.0.0.1:30482" // the group's Docker connector
	// nxBasicChallenge is what Nexus answers to a wrong password, whatever the
	// state of the bench (nominal, DockerToken realm removed, anonymous cut): a Basic
	// challenge, upper case, never a Bearer one.
	nxBasicChallenge = `BASIC realm="Sonatype Nexus Repository Manager"`
	// nxAnonymousToken is the token fakenexus hands the anonymous identity (its
	// own "fixture-token-<identity>"; the real one is never recorded).
	nxAnonymousToken = "fixture-token-anon"
)

// nxCaptureTB lets a test expect the complaints of a fake (a request no fixture
// answers) instead of failing on them. Everything else is the real T.
type nxCaptureTB struct {
	testing.TB
	mu   sync.Mutex
	errs []string
}

func (c *nxCaptureTB) Helper() {}

func (c *nxCaptureTB) Errorf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, fmt.Sprintf(format, args...))
}

func (c *nxCaptureTB) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.errs)
}

// nxPick returns the named fixtures of a campaign, in the order given. A name that
// is not embedded fails the test, so a removed fixture cannot leave it vacuous.
func nxPick(t testing.TB, campaign string, names ...string) []nexusfixture.Fixture {
	t.Helper()
	all, err := nexusfixture.Load(campaign)
	if err != nil {
		t.Fatalf("Load(%s): %v", campaign, err)
	}
	var out []nexusfixture.Fixture
	for _, name := range names {
		i := slices.IndexFunc(all, func(f nexusfixture.Fixture) bool { return f.Name == name })
		if i < 0 {
			t.Fatalf("the %s campaign has no fixture %q", campaign, name)
		}
		out = append(out, all[i])
	}
	return out
}

// nxWant pins one recorded exchange of a fixture: its request and its status. A
// fixture regenerated with another answer fails here, naming it, before any replay
// can hide the change.
func nxWant(t testing.TB, f nexusfixture.Fixture, exchange int, method, target string, status int) nexusfixture.Exchange {
	t.Helper()
	if exchange >= len(f.Exchanges) {
		t.Fatalf("%s: %d exchanges, this test reads exchange %d: the measurement changed", f.Name, len(f.Exchanges), exchange)
	}
	ex := f.Exchanges[exchange]
	if ex.Method != method || ex.Target != target || ex.Status != status {
		t.Fatalf("%s exchange %d recorded %s %s -> %d, this test was written for %s %s -> %d: the measurement changed",
			f.Name, exchange, ex.Method, ex.Target, ex.Status, method, target, status)
	}
	return ex
}

// nxReq is one request the client put on the wire.
type nxReq struct {
	Method, Host, URI, Auth, Accept string
}

// nxWire is the transport of the client under test: it logs every request, answers
// the hosts named in intercept without any network, and lets the rest go to the
// real (loopback) transport.
type nxWire struct {
	mu        sync.Mutex
	reqs      []nxReq
	intercept map[string]func(*http.Request) *http.Response
}

func (w *nxWire) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	w.reqs = append(w.reqs, nxReq{r.Method, r.URL.Host, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Accept")})
	answer := w.intercept[r.URL.Host]
	w.mu.Unlock()
	if answer != nil {
		resp := answer(r)
		resp.Request = r
		return resp, nil
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (w *nxWire) requests() []nxReq {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.reqs)
}

// useNxWire swaps the client's transport for a logging one (restored with t).
func useNxWire(t *testing.T, intercept map[string]func(*http.Request) *http.Response) *nxWire {
	t.Helper()
	w := &nxWire{intercept: intercept}
	useClientTransport(t, w)
	return w
}

// nxHits fails the test unless the fake answered each named fixture exactly the
// given number of times.
func nxHits(t *testing.T, s *fakenexus.Server, want map[string]int) {
	t.Helper()
	for name, n := range want {
		if got := s.Hits(name); got != n {
			t.Errorf("Hits(%s) = %d, want %d", name, got, n)
		}
	}
}

// nxNoToken fails when any logged request went to a token endpoint.
func nxNoToken(t *testing.T, reqs []nxReq) {
	t.Helper()
	for _, r := range reqs {
		if strings.Contains(r.URI, "/token") {
			t.Errorf("the client went to a token endpoint: %s %s%s", r.Method, r.Host, r.URI)
		}
	}
}

// Valid credentials never reach the Bearer flow on this Nexus. The client sends Basic
// at once and Nexus accepts it (200) on the group connector, on the HTTP port by
// path and by /repository/<name>, for the admin and the pull account, nominal, with
// the DockerToken realm removed (W5) and with the anonymous access cut (W6). So the
// request count is exactly 1 and none goes to /v2/token: the flow of
// TestBearerFlowOnMeasuredNexusAnswers is reached only by an answer Nexus gives to
// a wrong password or to an anonymous caller.
func TestValidCredentialsNeverEnterTheBearerFlowOnNexus(t *testing.T) {
	type read struct {
		name     string
		campaign string
		fixture  string
		host     string
		suffix   string // appended to the fake's URL: the entry's own path, if any
		list     func(ctx context.Context, base, user, pass string) ([]string, error)
		recorded string // the request the fixture holds
		target   string // the request the client must send
		want     []string
	}
	catalog := func(prefix string) func(context.Context, string, string, string) ([]string, error) {
		return func(ctx context.Context, base, user, pass string) ([]string, error) {
			names, truncated, err := ListCatalogUnderContext(ctx, base, prefix, user, pass, 10, 1000)
			if err == nil && truncated {
				err = errors.New("the walk was truncated")
			}
			return names, err
		}
	}
	tags := func(repo string) func(context.Context, string, string, string) ([]string, error) {
		return func(ctx context.Context, base, user, pass string) ([]string, error) {
			return ListRegistryTagsContext(ctx, base, repo, user, pass)
		}
	}
	hosted := []string{"api", "shared/base", "team/api", "tools/cli"}
	all := []string{"api", "mirror/cached", "mirror/uncached", "shared/base", "team/api", "tools/cli"}
	apiTags := []string{"1.0.0", "1.1.0"}
	const (
		connectorTarget = "/v2/_catalog?n=1000"
		hostedTarget    = "/v2/docker-hosted/_catalog?n=1000"
	)

	var cases []read
	for _, id := range []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull} {
		connectorFixture, connectorRecorded := "page-group-n1000--admin", connectorTarget
		if id == nexusfixture.Pull {
			// Only the n-less recording exists for pull: the fake serves it for n=1000
			// (the tolerance of fakenexus, an inference for this identity).
			connectorFixture, connectorRecorded = "catalog-group-connector-30482--pull", "/v2/_catalog"
		}
		cases = append(cases,
			read{"catalog, group connector, " + string(id), "catalog", connectorFixture, nxConnectorHost, "", catalog(""), connectorRecorded, connectorTarget, all},
			read{"catalog, hosted by path, " + string(id), "catalog", "catalog-hosted-path-n1000--" + string(id), nxPathHost, "", catalog("docker-hosted"), hostedTarget, hostedTarget, hosted},
			read{"tags, hosted by path, " + string(id), "access", "routing-tags-hosted-path--" + string(id), nxPathHost, "", tags("docker-hosted/api"),
				"/v2/docker-hosted/api/tags/list", "/v2/docker-hosted/api/tags/list", apiTags},
			read{"tags, hosted by /repository/<name>, " + string(id), "access", "routing-tags-hosted-repository--" + string(id), nxPathHost, "/repository/docker-hosted", tags("api"),
				"/repository/docker-hosted/v2/api/tags/list", "/repository/docker-hosted/v2/api/tags/list", apiTags},
			read{"tags, group connector, " + string(id), "access", "routing-tags-group-connector-30482--" + string(id), nxConnectorHost, "", tags("api"),
				"/v2/api/tags/list", "/v2/api/tags/list", apiTags},
		)
	}
	// The same catalogs with the DockerToken realm removed and with the anonymous
	// access cut: the admin still gets a 200 to Basic.
	for _, state := range []string{"w5", "w6"} {
		cases = append(cases,
			read{"catalog, group connector, admin, " + state, "access", state + "-catalog-group-connector-30482--admin", nxConnectorHost, "", catalog(""), connectorTarget, connectorTarget, all},
			read{"catalog, hosted by path, admin, " + state, "access", state + "-catalog-hosted-path--admin", nxPathHost, "", catalog("docker-hosted"), hostedTarget, hostedTarget, hosted},
		)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := nxPick(t, tc.campaign, tc.fixture)[0]
			nxWant(t, f, 0, "GET", tc.recorded, 200)
			if len(f.Exchanges) != 1 || f.Exchanges[0].Header.Get("Www-Authenticate") != "" {
				t.Fatalf("%s: %d exchanges, challenge %q: Nexus now challenges a valid account, the measurement changed",
					f.Name, len(f.Exchanges), f.Exchanges[0].Header.Get("Www-Authenticate"))
			}
			s := fakenexus.New(t, f)
			wire := useNxWire(t, nil)

			got, err := tc.list(context.Background(), s.URL(tc.host)+tc.suffix, fakenexus.User(f.Identity), fakenexus.Password)

			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("names = %v, want %v", got, tc.want)
			}
			reqs := wire.requests()
			if len(reqs) != 1 {
				t.Fatalf("%d requests on the wire, want 1: %+v", len(reqs), reqs)
			}
			if reqs[0].URI != tc.target {
				t.Errorf("request = %s, want %s", reqs[0].URI, tc.target)
			}
			if !strings.HasPrefix(reqs[0].Auth, "Basic ") {
				t.Errorf("Authorization = %q, want Basic sent at once", reqs[0].Auth)
			}
			nxNoToken(t, reqs)
			nxHits(t, s, map[string]int{f.Name: 1})
		})
	}
}

// The Bearer flow on the answers Nexus really gave. Two ways in, both measured:
//
//   - A wrong password. Nexus answers 401 with a BASIC challenge, never a Bearer
//     one, so the client does NOT enter the flow: it returns a typed
//     *StatusError{401, SourceRegistry} that carries the challenge. No token request
//     goes out. imagesearch.Classify turns it into "refused" (access_fixture_test.go).
//     Identical on the nominal bench, with the DockerToken realm removed (W5) and
//     with the anonymous access cut (W6).
//   - An anonymous GET /v2/ (the Docker client's first request). Nexus answers 401
//     with a Bearer challenge whose realm is on the registry's own origin; the
//     client fetches a token there (200) and retries with it. Nexus answers 401
//     AGAIN to that retry: the flow does not succeed on this bench, and the client
//     reports the plain 401 of that second answer (it never answers a second
//     challenge, and the realm token is not what authorizes a catalog read).
//
// The Bearer flow therefore never yields a useful read on this Nexus; what the
// client does at each step is frozen here.
func TestBearerFlowOnMeasuredNexusAnswers(t *testing.T) {
	user := fakenexus.User(nexusfixture.BadCredentials)

	t.Run("a wrong password is refused without a Bearer flow", func(t *testing.T) {
		cases := []struct {
			fixture string
			host    string
			prefix  string
			target  string
		}{
			{"bearer-catalog-group-connector-30482--badcred", nxConnectorHost, "", "/v2/_catalog?n=1000"},
			{"bearer-catalog-hosted-path--badcred", nxPathHost, "docker-hosted", "/v2/docker-hosted/_catalog?n=1000"},
			{"w5-catalog-group-connector-30482--badcred", nxConnectorHost, "", "/v2/_catalog?n=1000"},
			{"w5-catalog-hosted-path--badcred", nxPathHost, "docker-hosted", "/v2/docker-hosted/_catalog?n=1000"},
			{"w6-catalog-group-connector-30482--badcred", nxConnectorHost, "", "/v2/_catalog?n=1000"},
			{"w6-catalog-hosted-path--badcred", nxPathHost, "docker-hosted", "/v2/docker-hosted/_catalog?n=1000"},
		}
		for _, tc := range cases {
			t.Run(tc.fixture, func(t *testing.T) {
				f := nxPick(t, "access", tc.fixture)[0]
				ex := nxWant(t, f, 0, "GET", tc.target, 401)
				if got := ex.Header.Get("Www-Authenticate"); got != nxBasicChallenge {
					t.Fatalf("%s: challenge %q, want %q: the measurement changed", f.Name, got, nxBasicChallenge)
				}
				s := fakenexus.New(t, f)
				wire := useNxWire(t, nil)

				names, _, err := ListCatalogUnderContext(context.Background(), s.URL(tc.host), tc.prefix, user, fakenexus.BadPassword, 10, 1000)

				if len(names) != 0 {
					t.Errorf("names = %v, want none", names)
				}
				var se *StatusError
				if !errors.As(err, &se) {
					t.Fatalf("err = %v (%T), want a *StatusError", err, err)
				}
				if se.Code != 401 || se.Source != SourceRegistry {
					t.Errorf("StatusError = {%d, source %v}, want {401, SourceRegistry}: the registry refused, not a token endpoint", se.Code, se.Source)
				}
				if se.Challenge != nxBasicChallenge || !se.unexpectedChallenge {
					t.Errorf("Challenge = %q (unexpected = %v), want the Basic challenge carried as an unexpected one", se.Challenge, se.unexpectedChallenge)
				}
				if want := "unexpected auth challenge: " + nxBasicChallenge; err.Error() != want {
					t.Errorf("err.Error() = %q, want %q", err, want)
				}
				reqs := wire.requests()
				if len(reqs) != 1 || !strings.HasPrefix(reqs[0].Auth, "Basic ") {
					t.Errorf("wire = %+v, want one request, Basic", reqs)
				}
				nxNoToken(t, reqs)
				nxHits(t, s, map[string]int{f.Name: 1})
			})
		}
	})

	// 401 -> token -> retry, the three exchanges of the fixture. State "nominal" is the
	// nominal bench; "realm-off" has the DockerToken realm removed (W5): the
	// challenge is unchanged, the token is still handed out, and only the answer to
	// the retry changes (a Basic 401 instead of a Bearer one), which the client
	// cannot tell apart: it reports the same plain 401.
	t.Run("an anonymous client follows 401, token, retry and is refused again", func(t *testing.T) {
		cases := []struct {
			fixture   string
			host      string
			tokenPath string
			state     string
			retryAuth string // the challenge of the answer to the retry, as recorded
		}{
			{"bearer-ping-30481--anon", nxPathHost, "/repository/docker-hosted/v2/token", "nominal", "Bearer "},
			{"bearer-ping-30482--anon", nxConnectorHost, "/v2/token", "nominal", "Bearer "},
			{"w5-bearer-ping-30481--anon", nxPathHost, "/repository/docker-hosted/v2/token", "realm-off", "BASIC "},
			{"w5-bearer-ping-30482--anon", nxConnectorHost, "/v2/token", "realm-off", "BASIC "},
		}
		for _, tc := range cases {
			t.Run(tc.fixture, func(t *testing.T) {
				f := nxPick(t, "access", tc.fixture)[0]
				if f.Meta.Flow != nexusfixture.FlowBearer || f.Meta.State != tc.state || len(f.Exchanges) != 3 {
					t.Fatalf("%s: flow %q state %q, %d exchanges, want a 3-exchange bearer flow in state %q: the measurement changed",
						f.Name, f.Meta.Flow, f.Meta.State, len(f.Exchanges), tc.state)
				}
				first := nxWant(t, f, 0, "GET", "/v2/", 401)
				if !strings.HasPrefix(first.Header.Get("Www-Authenticate"), "Bearer ") {
					t.Fatalf("%s: first challenge %q, want Bearer", f.Name, first.Header.Get("Www-Authenticate"))
				}
				nxWant(t, f, 1, "GET", tc.tokenPath+"?service="+url.QueryEscape("http://"+tc.host+tc.tokenPath), 200)
				retry := nxWant(t, f, 2, "GET", "/v2/", 401)
				if got := retry.Header.Get("Www-Authenticate"); !strings.HasPrefix(got, tc.retryAuth) {
					t.Fatalf("%s: the retry is answered with challenge %q, want one starting %q: the measurement changed", f.Name, got, tc.retryAuth)
				}
				s := fakenexus.New(t, f)
				wire := useNxWire(t, nil)
				base := s.URL(tc.host)

				body, _, token, err := registryGETContext(context.Background(), base+"/v2/", "", "", "")

				if len(body) != 0 {
					t.Errorf("body = %q, want none", body)
				}
				var se *StatusError
				if !errors.As(err, &se) || se.Code != 401 || se.Source != SourceRegistry || se.Challenge != "" || se.unexpectedChallenge {
					t.Errorf("err = %v (%T), want the plain *StatusError{401, SourceRegistry} of the retry", err, err)
				}
				if err != nil && err.Error() != "registry returned 401" {
					t.Errorf("err.Error() = %q, want %q", err, "registry returned 401")
				}
				if token != nxAnonymousToken {
					t.Errorf("token kept = %q, want the one the fake handed out", token)
				}
				reqs := wire.requests()
				if len(reqs) != 3 {
					t.Fatalf("%d requests on the wire, want 3 (challenge, token, retry): %+v", len(reqs), reqs)
				}
				if reqs[0].URI != "/v2/" || reqs[0].Auth != "" {
					t.Errorf("request 1 = %+v, want GET /v2/ with no credentials", reqs[0])
				}
				tokenReq, err := url.Parse(reqs[1].URI)
				if err != nil || tokenReq.Path != tc.tokenPath || reqs[1].Host != strings.TrimPrefix(base, "http://") || reqs[1].Auth != "" {
					t.Errorf("request 2 = %+v, want the realm of the challenge, on the registry's own origin, with no credentials", reqs[1])
				}
				if got, want := tokenReq.Query().Get("service"), base+tc.tokenPath; got != want {
					t.Errorf("token request service = %q, want %q (the challenge's own)", got, want)
				}
				if reqs[2].URI != "/v2/" || reqs[2].Auth != "Bearer "+nxAnonymousToken {
					t.Errorf("request 3 = %+v, want the retry with the token", reqs[2])
				}
				nxHits(t, s, map[string]int{f.Name: 3})
			})
		}
	})

	// With the anonymous access cut (W6) an anonymous catalog read is answered 401
	// with a Bearer challenge that carries a scope. The recorder had no Bearer flow
	// for these fixtures: what Nexus answers at the token endpoint of that realm was
	// NOT measured. What is measured, and replayed, is the challenge and what the
	// client does next: it goes to the realm (same origin) with the scope and the
	// service of the challenge, with no credentials. How that ends is not asserted:
	// the fake has nothing to answer it with (it reports the request, expected here).
	t.Run("an anonymous catalog read with the anonymous access cut goes to the realm", func(t *testing.T) {
		cases := []struct {
			fixture   string
			host      string
			prefix    string
			target    string
			tokenPath string
		}{
			{"w6-catalog-group-connector-30482--anon", nxConnectorHost, "", "/v2/_catalog?n=1000", "/v2/token"},
			// The realm of a repository read by path is NOT the one of /v2/: it is under /v2/<repo>/.
			{"w6-catalog-hosted-path--anon", nxPathHost, "docker-hosted", "/v2/docker-hosted/_catalog?n=1000", "/v2/docker-hosted/token"},
		}
		for _, tc := range cases {
			t.Run(tc.fixture, func(t *testing.T) {
				f := nxPick(t, "access", tc.fixture)[0]
				ex := nxWant(t, f, 0, "GET", tc.target, 401)
				challenge := ex.Header.Get("Www-Authenticate")
				params := parseBearerChallenge(strings.TrimPrefix(challenge, "Bearer "))
				if !strings.HasPrefix(challenge, "Bearer ") || params["scope"] != "repository:*:pull" ||
					params["realm"] != "http://"+tc.host+tc.tokenPath {
					t.Fatalf("%s: challenge %q: the measurement changed", f.Name, challenge)
				}
				captured := &nxCaptureTB{TB: t}
				s := fakenexus.New(captured, f)
				wire := useNxWire(t, nil)

				_, _, err := ListCatalogUnderContext(context.Background(), s.URL(tc.host), tc.prefix, "", "", 10, 1000)

				var se *StatusError
				if !errors.As(err, &se) || se.Source != SourceToken {
					t.Fatalf("err = %v (%T), want the failure of the token endpoint (*StatusError, SourceToken)", err, err)
				}
				reqs := wire.requests()
				if len(reqs) != 2 {
					t.Fatalf("%d requests on the wire, want 2 (the read, the token): %+v", len(reqs), reqs)
				}
				tokenReq, perr := url.Parse(reqs[1].URI)
				if perr != nil || tokenReq.Path != tc.tokenPath || tokenReq.Query().Get("scope") != "repository:*:pull" || reqs[1].Auth != "" {
					t.Errorf("request 2 = %+v, want the realm of the challenge with its scope and no credentials", reqs[1])
				}
				if got := captured.all(); len(got) != 1 || !strings.Contains(got[0], "no recorded answer for GET "+tc.tokenPath) {
					t.Errorf("the fake reported %q, want exactly the unmeasured token request", got)
				}
				nxHits(t, s, map[string]int{f.Name: 1})
			})
		}
	})
}

// wwwAuthenticateRealm returns the realm of a Bearer challenge.
func wwwAuthenticateRealm(t *testing.T, challenge string) *url.URL {
	t.Helper()
	rest, ok := strings.CutPrefix(challenge, "Bearer ")
	if !ok {
		t.Fatalf("challenge %q is not a Bearer one", challenge)
	}
	realm, ok := parseBearerChallenge(rest)["realm"]
	if !ok {
		t.Fatalf("challenge %q names no realm", challenge)
	}
	u, err := url.Parse(realm)
	if err != nil {
		t.Fatalf("realm %q: %v", realm, err)
	}
	return u
}

// Where the realm points. Measured on the bench, every Bearer challenge names a realm
// on the registry's own origin (same scheme and host:port as the request), by the
// path of the connector or of the repository, and the client accepts it.
//
// The X-Forwarded-Host and X-Forwarded-Proto probe shows that Nexus builds the
// realm from the host the request ANNOUNCES: behind a reverse proxy that sets them
// (the enterprise case, which the bench does not reproduce) the realm is on
// whatever origin the proxy says. The probe's answer is replayed here, and what
// DevDesk does with such a realm is the fact pinned: it FOLLOWS it. checkTokenRealm
// refuses a realm by scheme only (not http behind https, not a scheme other than
// http or https); a realm on another origin over https is taken (Docker Hub's own
// case, deliberate), and, when credentials are set, they go to it as Basic.
//
// That contradicts criterion 3 of NX-08b ("the realms off the origin are refused as
// measured"): nothing measured shows a refusal, and the code has none. The
// contradiction is pinned, not corrected (docs/measurements/2026-11-20-nexus-access.md).
func TestMeasuredRealmStaysOnTheRegistryOrigin(t *testing.T) {
	t.Run("every measured realm is on the origin of its request", func(t *testing.T) {
		cases := []struct {
			fixture string
			host    string
			target  string
		}{
			{"bearer-ping-30481--anon", nxPathHost, "/v2/"},
			{"bearer-ping-30482--anon", nxConnectorHost, "/v2/"},
			{"redirects-v2-no-slash-30481--anon", nxPathHost, "/v2"},
			{"w5-ping-30481--anon", nxPathHost, "/v2/"},
			{"w5-ping-30482--anon", nxConnectorHost, "/v2/"},
			{"w6-ping-30481--anon", nxPathHost, "/v2/"},
			{"w6-ping-30482--anon", nxConnectorHost, "/v2/"},
			{"w6-catalog-group-connector-30482--anon", nxConnectorHost, "/v2/_catalog?n=1000"},
			{"w6-catalog-hosted-path--anon", nxPathHost, "/v2/docker-hosted/_catalog?n=1000"},
		}
		for _, tc := range cases {
			t.Run(tc.fixture, func(t *testing.T) {
				f := nxPick(t, "access", tc.fixture)[0]
				ex := nxWant(t, f, 0, "GET", tc.target, 401)
				recorded := wwwAuthenticateRealm(t, ex.Header.Get("Www-Authenticate"))
				if recorded.Scheme != "http" || recorded.Host != tc.host {
					t.Fatalf("%s: realm %s, want it on http://%s: the measurement changed", f.Name, recorded, tc.host)
				}
				s := fakenexus.New(t, f)
				useNxWire(t, nil)
				base := s.URL(tc.host)

				resp, err := doRequestContext(context.Background(), http.MethodGet, base+tc.target, "", "", "", "")
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()

				challenge := resp.Header.Get("Www-Authenticate")
				realm := wwwAuthenticateRealm(t, challenge)
				if want := strings.TrimPrefix(base, "http://"); realm.Scheme != "http" || realm.Host != want {
					t.Errorf("replayed realm = %s, want it on the origin of the request (http://%s)", realm, want)
				}
				if err := checkTokenRealm(base, challenge[len("Bearer "):]); err != nil {
					t.Errorf("checkTokenRealm refuses the measured realm: %v", err)
				}
			})
		}
	})

	t.Run("a realm announced off the origin is followed", func(t *testing.T) {
		const offOrigin = "realm-probe.invalid"
		cases := []struct {
			fixture   string
			host      string
			tokenPath string
		}{
			{"realm-probe-ping-30481--anon", nxPathHost, "/repository/docker-hosted/v2/token"},
			{"realm-probe-ping-30482--anon", nxConnectorHost, "/v2/token"},
		}
		for _, tc := range cases {
			f := nxPick(t, "access", tc.fixture)[0]
			ex := nxWant(t, f, 0, "GET", "/v2/", 401)
			if got := ex.RequestHeader.Get("X-Forwarded-Host"); got != offOrigin {
				t.Fatalf("%s: X-Forwarded-Host = %q: the probe changed", f.Name, got)
			}
			realm := wwwAuthenticateRealm(t, ex.Header.Get("Www-Authenticate"))
			if realm.Scheme != "https" || realm.Host != offOrigin || realm.Path != tc.tokenPath {
				t.Fatalf("%s: realm %s, want https://%s%s: the measurement changed", f.Name, realm, offOrigin, tc.tokenPath)
			}

			// What answers at the off-origin realm is the test's own stand-in (no network
			// is touched): a 403, so that the flow ends there.
			deny := func(*http.Request) *http.Response {
				return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
			}
			intercept := map[string]func(*http.Request) *http.Response{offOrigin: deny}

			t.Run(tc.fixture+", anonymous", func(t *testing.T) {
				s := fakenexus.New(t, f)
				wire := useNxWire(t, intercept)

				_, _, _, err := registryGETContext(context.Background(), s.URL(tc.host)+"/v2/", "", "", "")

				var se *StatusError
				if !errors.As(err, &se) || se.Source != SourceToken || se.Code != 403 {
					t.Fatalf("err = %v (%T), want the 403 of the off-origin token endpoint: the client went there", err, err)
				}
				reqs := wire.requests()
				if len(reqs) != 2 || reqs[1].Host != offOrigin || reqs[1].Auth != "" {
					t.Fatalf("wire = %+v, want the read then ONE token request to %s, with no credentials", reqs, offOrigin)
				}
				tokenReq, _ := url.Parse(reqs[1].URI)
				if tokenReq.Path != tc.tokenPath || tokenReq.Query().Get("service") != realm.String() {
					t.Errorf("token request = %s, want the realm's path %s and its service %s", reqs[1].URI, tc.tokenPath, realm)
				}
				nxHits(t, s, map[string]int{f.Name: 1})
			})

			// The credentials of a configured registry: the client sends Basic at once (to
			// the registry, as Nexus accepts it), and, on the challenge, again to the realm.
			// The fixture is the probe's anonymous recording, so the registry here is a
			// handler that serves its recorded 401 to ANY caller: it is the only place this
			// pin is not a measured exchange, only the measured header.
			t.Run(tc.fixture+", with credentials", func(t *testing.T) {
				reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					for name, values := range ex.Header {
						for _, v := range values {
							w.Header().Add(name, v)
						}
					}
					w.WriteHeader(ex.Status)
					_, _ = w.Write(ex.Body)
				}))
				t.Cleanup(reg.Close)
				wire := useNxWire(t, intercept)

				_, _, _, err := registryGETContext(context.Background(), reg.URL+"/v2/", "admin", "S3CRETPW", "")

				var se *StatusError
				if !errors.As(err, &se) || se.Source != SourceToken || se.Code != 403 {
					t.Fatalf("err = %v (%T), want the 403 of the off-origin token endpoint", err, err)
				}
				reqs := wire.requests()
				if len(reqs) != 2 || reqs[1].Host != offOrigin {
					t.Fatalf("wire = %+v, want the read then one token request to %s", reqs, offOrigin)
				}
				if !strings.HasPrefix(reqs[1].Auth, "Basic ") {
					t.Errorf("Authorization of the token request = %q: the credentials stopped at the registry, this pin is out of date", reqs[1].Auth)
				}
			})
		}
	})
}

// Redirects. NX-05 asked whether Nexus ever redirects; the answer measured is no: not
// one of the exchanges of the three campaigns carries a 3xx or a Location, whatever
// the candidate that was tried (GET /v2 without its slash, /repository/<r>/v2 without
// its slash, /v2/<repo>, the HEAD of a manifest and the GET of a blob on the hosted
// repository). The answers are direct: 200, 401 Bearer, or a 404 page. So there is no
// redirect to follow and none off the origin to refuse, and this test pins the direct
// answers (the client takes each as it is: one request, no hop).
//
// The refusal of a redirect or a Link off the origin is NOT measured (the bench has no
// reverse proxy and its blob store is a file one). It stays covered, and only, by the
// tests written by hand: TestRegistryRedirectOffOriginSendsNoCredentials,
// TestRegistryRedirectRoundTripRefused and TestRegistryRedirectRefusalDoesNotLeakTarget
// (registry_origin*_test.go), TestListRegistryTagsNextOffOriginIsAnError,
// TestClientRefusesOffOriginRedirect and TestBlobRedirectOffOriginDropsCredentials.
func TestRedirectsOnMeasuredNexusAnswers(t *testing.T) {
	t.Run("no measured exchange redirects", func(t *testing.T) {
		total := 0
		for _, campaign := range nexusfixture.Campaigns() {
			fixtures, err := nexusfixture.Load(campaign)
			if err != nil {
				t.Fatalf("Load(%s): %v", campaign, err)
			}
			for _, f := range fixtures {
				for i, ex := range f.Exchanges {
					total++
					if ex.Status >= 300 && ex.Status < 400 || ex.Header.Get("Location") != "" {
						t.Errorf("%s exchange %d: %s %s -> %d, Location %q: Nexus redirects now, so the direct answers pinned below and the claim of the measurement note are out of date",
							f.Name, i, ex.Method, ex.Target, ex.Status, ex.Header.Get("Location"))
					}
				}
			}
		}
		if total < 100 {
			t.Errorf("only %d exchanges read: a campaign went missing", total)
		}
	})

	t.Run("the direct answers", func(t *testing.T) {
		cases := []struct {
			fixture string
			host    string
			target  string
			status  int
			// answer is what the client reports for it: "" none, else the challenge kind.
			bearer bool
		}{
			{"redirects-v2-no-slash-30481--admin", nxPathHost, "/v2", 200, false},
			{"redirects-v2-no-slash-30481--anon", nxPathHost, "/v2", 401, true},
			{"redirects-v2-no-slash-30482--admin", nxConnectorHost, "/v2", 404, false},
			{"redirects-hosted-path-root--admin", nxPathHost, "/v2/docker-hosted", 404, false},
			{"redirects-repository-v2-no-slash--admin", nxPathHost, "/repository/docker-hosted/v2", 404, false},
		}
		for _, tc := range cases {
			t.Run(tc.fixture, func(t *testing.T) {
				f := nxPick(t, "access", tc.fixture)[0]
				nxWant(t, f, 0, "GET", tc.target, tc.status)
				if f.Meta.Flow != nexusfixture.FlowRedirects {
					t.Fatalf("%s: flow %q, want %q", f.Name, f.Meta.Flow, nexusfixture.FlowRedirects)
				}
				s := fakenexus.New(t, f)
				wire := useNxWire(t, nil)
				user, pass := "", ""
				if f.Identity != nexusfixture.Anonymous {
					user, pass = fakenexus.User(f.Identity), fakenexus.Password
				}

				resp, err := doRequestContext(context.Background(), http.MethodGet, s.URL(tc.host)+tc.target, "", user, pass, "")
				if err != nil {
					t.Fatalf("a direct answer is an error: %v", err)
				}
				_ = resp.Body.Close()

				if resp.StatusCode != tc.status || resp.Header.Get("Location") != "" {
					t.Errorf("answer = %d, Location %q, want %d with none", resp.StatusCode, resp.Header.Get("Location"), tc.status)
				}
				if got := strings.HasPrefix(resp.Header.Get("Www-Authenticate"), "Bearer "); got != tc.bearer {
					t.Errorf("Bearer challenge = %v, want %v", got, tc.bearer)
				}
				if reqs := wire.requests(); len(reqs) != 1 {
					t.Errorf("%d requests on the wire, want 1 (no hop): %+v", len(reqs), reqs)
				}
				nxHits(t, s, map[string]int{f.Name: 1})
			})
		}
	})

	// The one registry call DevDesk makes on a manifest is the HEAD of ManifestDigest;
	// Nexus answers it directly on the hosted repository, with the digest in the header.
	t.Run("the HEAD of a hosted manifest", func(t *testing.T) {
		f := nxPick(t, "access", "redirects-head-hosted-manifest-api--admin")[0]
		ex := nxWant(t, f, 0, "HEAD", "/v2/docker-hosted/api/manifests/1.0.0", 200)
		const digest = "sha256:241d539e25e7715ee530207ef552fe850bab8a59bc0b328359b8cffcfed49cb8"
		if got := ex.Header.Get("Docker-Content-Digest"); got != digest {
			t.Fatalf("%s: digest %q, want %q: the measurement changed", f.Name, got, digest)
		}
		s := fakenexus.New(t, f)
		wire := useNxWire(t, nil)

		got, err := ManifestDigest(s.URL(nxPathHost), "docker-hosted/api", "1.0.0", fakenexus.User(nexusfixture.Admin), fakenexus.Password)

		if err != nil || got != digest {
			t.Fatalf("ManifestDigest = %q, %v, want %q", got, err, digest)
		}
		reqs := wire.requests()
		if len(reqs) != 1 || reqs[0].Method != http.MethodHead || reqs[0].URI != "/v2/docker-hosted/api/manifests/1.0.0" {
			t.Fatalf("wire = %+v, want one HEAD of the manifest", reqs)
		}
		if want := ex.RequestHeader.Get("Accept"); reqs[0].Accept != want || want != manifestAccept {
			t.Errorf("Accept = %q, recorded %q, client's %q: all three must be the same list", reqs[0].Accept, want, manifestAccept)
		}
		nxHits(t, s, map[string]int{f.Name: 1})
	})

	// The GET of a layer is recorded as a stub (its 3072 bytes are not kept), which
	// fakenexus cannot serve faithfully: the fact is pinned from the file, with no replay.
	t.Run("the GET of a hosted blob answers directly (recorded fact, no replay)", func(t *testing.T) {
		f := nxPick(t, "access", "redirects-get-hosted-blob-api--admin")[0]
		const digest = "sha256:dac904bd7b8caac7cc9684397f88e72b8cd89b5cf221e992d5f41d00cb9dc23c"
		ex := nxWant(t, f, 0, "GET", "/v2/docker-hosted/api/blobs/"+digest, 200)
		if ex.Stub == "" || ex.Header.Get("Docker-Content-Digest") != digest || ex.Header.Get("Content-Length") != "3072" {
			t.Errorf("%s: digest %q, length %q, stub %q: the measurement changed",
				f.Name, ex.Header.Get("Docker-Content-Digest"), ex.Header.Get("Content-Length"), ex.Stub)
		}
	})
}

// Tag pagination. DevDesk sends no n on tags/list, so Nexus sends no Link on the
// tag lists the client reads (routing-tags-*: none carries one) and the walk is one
// request. When n IS sent (tags-page-hosted-n1), Nexus honours it (one tag) and
// answers an ABSOLUTE Link rel="next" on the same origin, with last= the tag just
// given. The client follows a Link (maxTagPages), resolves it against the page it
// came with, and refuses one off the origin (ErrTagsNextOffOrigin, hand-written
// tests); the measured Link is what it would take. The second page was not recorded,
// so no walk is replayed; the catalog pagination (criterion 6) is PR1's
// TestCatalogWalk* (catalog_fixture_test.go).
func TestTagsLinkOnMeasuredNexusAnswers(t *testing.T) {
	t.Run("a page of one tag carries an absolute same-origin Link", func(t *testing.T) {
		f := nxPick(t, "access", "tags-page-hosted-n1--admin")[0]
		ex := nxWant(t, f, 0, "GET", "/v2/docker-hosted/api/tags/list?n=1", 200)
		if got, want := ex.Header.Get("Link"), `<http://127.0.0.1:30481/v2/docker-hosted/api/tags/list?n=1&last=1.0.0>; rel="next"`; got != want {
			t.Fatalf("%s: Link %q, want %q: the measurement changed", f.Name, got, want)
		}
		s := fakenexus.New(t, f)
		useNxWire(t, nil)
		base := s.URL(nxPathHost)
		current := base + "/v2/docker-hosted/api/tags/list?n=1"

		resp, err := doRequestContext(context.Background(), http.MethodGet, current, "", fakenexus.User(nexusfixture.Admin), fakenexus.Password, "")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if string(body) != `{"name":"api","tags":["1.0.0"]}` {
			t.Errorf("body = %s, want one tag (n=1 is honoured)", body)
		}
		next := resolveNext(current, resp.Header.Get("Link"))
		if want := base + "/v2/docker-hosted/api/tags/list?n=1&last=1.0.0"; next != want {
			t.Errorf("next = %q, want %q", next, want)
		}
		if !sameOrigin(current, next) {
			t.Errorf("next %q is not on the origin of %q: the client would stop on it", next, current)
		}
	})

	t.Run("the tag lists DevDesk reads carry no Link", func(t *testing.T) {
		var names []string
		for _, route := range []string{"hosted-path", "hosted-repository", "group-connector-30482"} {
			for _, id := range []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull, nexusfixture.Anonymous} {
				names = append(names, "routing-tags-"+route+"--"+string(id))
			}
		}
		for _, f := range nxPick(t, "access", names...) {
			if len(f.Exchanges) != 1 || f.Exchanges[0].Status != 200 || f.Exchanges[0].Header.Get("Link") != "" ||
				string(f.Exchanges[0].Body) != `{"name":"api","tags":["1.0.0","1.1.0"]}` {
				t.Errorf("%s: %+v: the measurement changed (a Link now, or another list)", f.Name, f.Exchanges)
			}
		}
	})
}
