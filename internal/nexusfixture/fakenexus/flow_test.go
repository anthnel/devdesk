package fakenexus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-580 step 8: the token route, Bearer identification, HEAD without a
// body, the badcred identity and the per-exchange duplicate check. Every fixture
// here is SYNTHETIC (built in the test, never a Nexus answer, none embedded).

const (
	catalogTarget = "/v2/_catalog?n=1000"
	// tokenTarget is the token request oci builds from the challenge below
	// (url.Values.Encode sorts the keys), written with the recorded host.
	tokenTarget = "/v2/token?scope=registry%3Acatalog%3A%2A&service=http%3A%2F%2F127.0.0.1%3A30482%2Fv2%2Ftoken"
	challenge   = `Bearer realm="http://127.0.0.1:30482/v2/token",service="http://127.0.0.1:30482/v2/token",scope="registry:catalog:*"`
	jsonType    = "application/json"
)

func auth(value string) http.Header {
	if value == "" {
		return http.Header{}
	}
	return http.Header{"Authorization": {value}}
}

func exchange(method, target string, req http.Header, status int, resp http.Header, body string) nexusfixture.Exchange {
	ex := nexusfixture.Exchange{
		Method: method, Target: target, Host: hostGroup,
		RequestHeader: req, Status: status, Header: resp, Body: []byte(body),
	}
	if body == "" {
		ex.Stub = "<empty body>"
		ex.Body = nil
	}
	return ex
}

// bearerFlow is the three exchanges an anonymous client records against a
// registry that challenges it: the 401, the token, the retry with the token.
func bearerFlow(name string, id nexusfixture.Identity) nexusfixture.Fixture {
	first := auth("")
	if id != nexusfixture.Anonymous {
		first = auth("Basic <redacted>")
	}
	return nexusfixture.Fixture{
		Campaign: "synthetic", Name: name, Scenario: strings.TrimSuffix(name, "--"+string(id)), Identity: id,
		Meta: nexusfixture.Meta{Flow: nexusfixture.FlowBearer},
		Exchanges: []nexusfixture.Exchange{
			exchange("GET", catalogTarget, first, 401,
				http.Header{"Www-Authenticate": {challenge}, "Content-Type": {jsonType}}, `{"errors":[{"code":"UNAUTHORIZED"}]}`),
			exchange("GET", tokenTarget, first, 200,
				http.Header{"Content-Type": {jsonType}}, `{"token":"<redacted>","expires_in":300}`),
			exchange("GET", catalogTarget, auth("Bearer <redacted>"), 200,
				http.Header{"Content-Type": {jsonType}}, `{"repositories":["team/api","team/web","shared/base"]}`),
		},
	}
}

// bearerOnly is a one-exchange fixture that answers a request carrying the token
// of its identity.
func bearerOnly(name, target string, id nexusfixture.Identity) nexusfixture.Fixture {
	return nexusfixture.Fixture{
		Campaign: "synthetic", Name: name, Scenario: name, Identity: id,
		Exchanges: []nexusfixture.Exchange{
			exchange("GET", target, auth("Bearer <redacted>"), 200, http.Header{"Content-Type": {jsonType}}, `{"ok":true}`),
		},
	}
}

func bearerGet(t *testing.T, base, target, token string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return do(t, req)
}

func TestACompleteBearerFlowIsReplayedForAnAnonymousCatalogWalk(t *testing.T) {
	rec := &recorder{TB: t}
	s := fakenexus.New(rec, bearerFlow("flow--anon", nexusfixture.Anonymous))
	if errs := rec.failures(); len(errs) != 0 {
		t.Fatalf("New refused the three exchanges of one flow: %q", errs)
	}
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()

	names, truncated, err := oci.ListCatalogContext(ctx, s.URL(hostGroup), "", "", 5, 1000)
	if err != nil {
		t.Fatalf("ListCatalogContext: %v", err)
	}
	if want := []string{"team/api", "team/web", "shared/base"}; strings.Join(names, ",") != strings.Join(want, ",") || truncated {
		t.Errorf("names, truncated = %v, %v, want %v, false", names, truncated, want)
	}
	if got := s.Hits("flow--anon"); got != 3 {
		t.Errorf("Hits = %d, want 3 (401, token, retry)", got)
	}
	if errs := rec.failures(); len(errs) != 0 {
		t.Errorf("the fake reported unrecorded requests: %q", errs)
	}
}

func TestTheTokenIsServedWithItsRedactionReplacedByTheIdentity(t *testing.T) {
	tests := []struct {
		id   nexusfixture.Identity
		want string
	}{
		{nexusfixture.Anonymous, "fixture-token-anon"},
		{nexusfixture.Pull, "fixture-token-pull"},
	}
	for _, tt := range tests {
		t.Run(string(tt.id), func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, bearerFlow("flow--"+string(tt.id), tt.id))
			base := s.URL(hostGroup)
			// The token request, as oci sends it: the fake's address in the service.
			target := "/v2/token?scope=registry%3Acatalog%3A%2A&service=" + url.QueryEscape(base+"/v2/token")
			resp, body := get(t, base, target, tt.id)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200 (errors %q)", resp.StatusCode, rec.failures())
			}
			var got struct {
				Token     string `json:"token"`
				ExpiresIn int    `json:"expires_in"`
			}
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("body %q: %v", body, err)
			}
			if got.Token != tt.want || got.ExpiresIn != 300 {
				t.Errorf("token, expires_in = %q, %d, want %q, 300 (only <redacted> changes)", got.Token, got.ExpiresIn, tt.want)
			}
			if strings.Contains(string(body), "<redacted>") {
				t.Errorf("the placeholder is served as is: %s", body)
			}
		})
	}
}

// A Bearer request is identified by its token and answered with the exchanges
// recorded as "Bearer <redacted>" for that identity, never another's.
func TestABearerTokenOfAnotherIdentityIsNotAnswered(t *testing.T) {
	rec := &recorder{TB: t}
	s := fakenexus.New(rec,
		bearerOnly("only-anon--anon", "/v2/anon-side", nexusfixture.Anonymous),
		bearerOnly("only-pull--pull", "/v2/pull-side", nexusfixture.Pull),
	)
	base := s.URL(hostGroup)

	if resp, _ := bearerGet(t, base, "/v2/anon-side", "fixture-token-anon"); resp.StatusCode != http.StatusOK {
		t.Fatalf("the anonymous token on the anonymous exchange: status %d, want 200 (errors %q)", resp.StatusCode, rec.failures())
	}
	if resp, _ := bearerGet(t, base, "/v2/pull-side", "fixture-token-pull"); resp.StatusCode != http.StatusOK {
		t.Fatalf("the pull token on the pull exchange: status %d, want 200 (errors %q)", resp.StatusCode, rec.failures())
	}
	if errs := rec.failures(); len(errs) != 0 {
		t.Fatalf("matched requests reported errors: %q", errs)
	}

	resp, _ := bearerGet(t, base, "/v2/pull-side", "fixture-token-anon")
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("the anonymous token on the pull exchange: status %d, want 501", resp.StatusCode)
	}
	errs := rec.failures()
	if len(errs) != 1 || !strings.Contains(errs[0], "no recorded answer") || !strings.Contains(errs[0], "anon") || !strings.Contains(errs[0], "/v2/pull-side") {
		t.Errorf("errors = %q, want one \"no recorded answer\" naming the path and the anon identity", errs)
	}
	if got := s.Hits("only-pull--pull"); got != 1 {
		t.Errorf("Hits(only-pull--pull) = %d, want 1 (the refused request counts for nothing)", got)
	}
}

func TestAnUnknownBearerTokenIsUnrecognizedCredentials(t *testing.T) {
	for _, token := range []string{"garbage", "fixture-token-", "fixture-token-root", "<redacted>"} {
		t.Run(token, func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, bearerOnly("only-anon--anon", "/v2/anon-side", nexusfixture.Anonymous))
			resp, _ := bearerGet(t, s.URL(hostGroup), "/v2/anon-side", token)
			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("status = %d, want 501", resp.StatusCode)
			}
			if errs := rec.failures(); len(errs) != 1 || !strings.Contains(errs[0], "no recorded answer") {
				t.Errorf("errors = %q, want one \"no recorded answer\"", errs)
			}
		})
	}
}

// A request with no token never gets the answer recorded for the token, and the
// other way round: one target, two exchanges, two answers.
func TestTheBearerExchangeAndTheUnauthenticatedOneDoNotShadowEachOther(t *testing.T) {
	rec := &recorder{TB: t}
	s := fakenexus.New(rec, bearerFlow("flow--anon", nexusfixture.Anonymous))
	base := s.URL(hostGroup)

	resp, _ := get(t, base, catalogTarget, nexusfixture.Anonymous)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without a token: status %d, want 401", resp.StatusCode)
	}
	resp, body := bearerGet(t, base, catalogTarget, "fixture-token-anon")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "shared/base") {
		t.Errorf("with the token: status %d body %q, want 200 and the catalog", resp.StatusCode, body)
	}
	if errs := rec.failures(); len(errs) != 0 {
		t.Errorf("errors = %q, want none", errs)
	}
}

// The token request is matched on its path, then on its decoded query, with the
// recorded host of the service rewritten to the fake's URL.
func TestTheTokenRequestIsMatchedOnItsDecodedQuery(t *testing.T) {
	other := synthetic("other--anon", hostHTTP, "/other", http.StatusOK, http.Header{"Content-Type": {"text/plain"}}, "x")
	tests := []struct {
		name   string
		query  func(group, httpPort string) string
		wantOK bool
	}{
		{"as oci sends it", func(g, _ string) string {
			return "scope=registry%3Acatalog%3A%2A&service=" + url.QueryEscape(g+"/v2/token")
		}, true},
		{"keys in another order", func(g, _ string) string {
			return "service=" + url.QueryEscape(g+"/v2/token") + "&scope=registry%3Acatalog%3A%2A"
		}, true},
		{"unencoded values", func(g, _ string) string { return "scope=registry:catalog:*&service=" + g + "/v2/token" }, true},
		{"service of another recorded host", func(_, h string) string {
			return "scope=registry%3Acatalog%3A%2A&service=" + url.QueryEscape(h+"/v2/token")
		}, false},
		{"service still holding the recorded address", func(string, string) string {
			return "scope=registry%3Acatalog%3A%2A&service=" + url.QueryEscape("http://127.0.0.1:30482/v2/token")
		}, false},
		{"another scope", func(g, _ string) string {
			return "scope=registry%3Aother%3A%2A&service=" + url.QueryEscape(g+"/v2/token")
		}, false},
		{"an extra parameter", func(g, _ string) string {
			return "scope=registry%3Acatalog%3A%2A&service=" + url.QueryEscape(g+"/v2/token") + "&account=admin"
		}, false},
		{"a missing service", func(string, string) string { return "scope=registry%3Acatalog%3A%2A" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{TB: t}
			s := fakenexus.New(rec, bearerFlow("flow--anon", nexusfixture.Anonymous), other)
			group, httpPort := s.URL(hostGroup), s.URL(hostHTTP)
			resp, _ := get(t, group, "/v2/token?"+tt.query(group, httpPort), nexusfixture.Anonymous)
			errs := rec.failures()
			if tt.wantOK {
				if resp.StatusCode != http.StatusOK || len(errs) != 0 {
					t.Errorf("status %d, errors %q, want 200 and none", resp.StatusCode, errs)
				}
				return
			}
			if resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("status %d, want 501 (not matched)", resp.StatusCode)
			}
			if len(errs) != 1 || !strings.Contains(errs[0], "no recorded answer") || !strings.Contains(errs[0], "/v2/token") {
				t.Errorf("errors = %q, want one \"no recorded answer\" naming /v2/token", errs)
			}
			if got := s.Hits("flow--anon"); got != 0 {
				t.Errorf("Hits = %d, want 0 for an unmatched token request", got)
			}
		})
	}
}

func TestBadPasswordWithAdminIsTheBadCredentialsIdentity(t *testing.T) {
	if fakenexus.BadPassword != "fixture-wrong-password" {
		t.Errorf("BadPassword = %q, want fixture-wrong-password", fakenexus.BadPassword)
	}
	if fakenexus.BadPassword == fakenexus.Password {
		t.Error("BadPassword equals Password")
	}
	if got := fakenexus.User(nexusfixture.BadCredentials); got != "admin" {
		t.Errorf("User(BadCredentials) = %q, want admin", got)
	}

	refused := nexusfixture.Fixture{
		Campaign: "synthetic", Name: "login-refused--badcred", Scenario: "login-refused", Identity: nexusfixture.BadCredentials,
		Exchanges: []nexusfixture.Exchange{
			exchange("GET", "/v2/_catalog", auth("Basic <redacted>"), 401,
				http.Header{"Www-Authenticate": {challenge}, "Content-Type": {jsonType}}, `{"errors":[{"code":"UNAUTHORIZED"}]}`),
			exchange("GET", tokenTarget, auth("Basic <redacted>"), 401,
				http.Header{"Content-Type": {jsonType}}, `{"errors":[{"code":"UNAUTHORIZED"}]}`),
		},
	}
	rec := &recorder{TB: t}
	s := fakenexus.New(rec, refused)
	base := s.URL(hostGroup)
	basic := func(user, pass, target string) *http.Response {
		req, err := http.NewRequest(http.MethodGet, base+target, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth(user, pass)
		resp, _ := do(t, req)
		return resp
	}

	if resp := basic("admin", fakenexus.BadPassword, "/v2/_catalog"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("admin with the bad password: status %d, want the recorded 401 (errors %q)", resp.StatusCode, rec.failures())
	}
	target := "/v2/token?scope=registry%3Acatalog%3A%2A&service=" + url.QueryEscape(base+"/v2/token")
	if resp := basic("admin", fakenexus.BadPassword, target); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token request with the bad password: status %d, want the recorded 401 (errors %q)", resp.StatusCode, rec.failures())
	}
	if got := s.Hits("login-refused--badcred"); got != 2 {
		t.Errorf("Hits = %d, want 2", got)
	}
	if errs := rec.failures(); len(errs) != 0 {
		t.Fatalf("matched requests reported errors: %q", errs)
	}

	// Only admin has a wrong-password identity; the real password is the admin
	// identity, which has no recording here; any other pair is unrecognized.
	for _, c := range []struct{ name, user, pass, label string }{
		{"admin with the real password", "admin", fakenexus.Password, "admin"},
		{"bench-pull with the bad password", "bench-pull", fakenexus.BadPassword, "unrecognized credentials"},
		{"admin with another wrong password", "admin", "zz-other-wrong", "unrecognized credentials"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := len(rec.failures())
			if resp := basic(c.user, c.pass, "/v2/_catalog"); resp.StatusCode != http.StatusNotImplemented {
				t.Errorf("status %d, want 501", resp.StatusCode)
			}
			errs := rec.failures()[before:]
			if len(errs) != 1 || !strings.Contains(errs[0], "no recorded answer") || !strings.Contains(errs[0], c.label) {
				t.Errorf("errors = %q, want one \"no recorded answer\" as %q", errs, c.label)
			}
		})
	}
}

// A HEAD answer keeps its headers (Content-Length and Docker-Content-Digest are
// the point of a HEAD of a manifest) and has no body.
func TestAHeadAnswerReplaysItsHeadersAndNoBody(t *testing.T) {
	const digest = "sha256:00000000000000000000000000000000000000000000000000000000000000aa"
	head := nexusfixture.Fixture{
		Campaign: "synthetic", Name: "manifest-head--admin", Scenario: "manifest-head", Identity: nexusfixture.Admin,
		Exchanges: []nexusfixture.Exchange{{
			Method: http.MethodHead, Target: "/v2/team/api/manifests/latest", Host: hostGroup,
			RequestHeader: http.Header{"Authorization": {"Basic <redacted>"}, "Accept": {"application/vnd.oci.image.index.v1+json"}},
			Status:        http.StatusOK,
			Header: http.Header{
				"Content-Type": {"application/vnd.oci.image.index.v1+json"}, "Docker-Content-Digest": {digest},
				"Content-Length": {"1234"}, "Docker-Distribution-Api-Version": {"registry/2.0"},
			},
			Stub: "<empty body>",
		}},
	}
	rec := &recorder{TB: t}
	s := fakenexus.New(rec, head)
	req, err := http.NewRequest(http.MethodHead, s.URL(hostGroup)+"/v2/team/api/manifests/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(fakenexus.User(nexusfixture.Admin), fakenexus.Password)
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json")
	resp, body := do(t, req)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200 (errors %q)", resp.StatusCode, rec.failures())
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want none for a HEAD", body)
	}
	if got := resp.Header.Get("Docker-Content-Digest"); got != digest {
		t.Errorf("Docker-Content-Digest = %q, want %q", got, digest)
	}
	if resp.ContentLength != 1234 {
		t.Errorf("Content-Length = %d, want the recorded 1234 (a stub body must not turn it into 0)", resp.ContentLength)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/vnd.oci.image.index.v1+json" {
		t.Errorf("Content-Type = %q, want it replayed", got)
	}

	// The same target as a GET was never recorded.
	if resp, _ := get(t, s.URL(hostGroup), "/v2/team/api/manifests/latest", nexusfixture.Admin); resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("GET of a HEAD-only target: status %d, want 501", resp.StatusCode)
	}
	if errs := rec.failures(); len(errs) != 1 {
		t.Errorf("errors = %q, want exactly the one of the GET", errs)
	}
}

// A fixture of several exchanges is held to the duplicate check exchange by
// exchange: the same request twice with the same credentials is refused, naming
// the fixture; the same request with and without a token is not a duplicate.
func TestDuplicateCheckRunsPerExchangeOfAMultiExchangeFixture(t *testing.T) {
	t.Run("the same exchange twice in one fixture", func(t *testing.T) {
		f := bearerFlow("dup--anon", nexusfixture.Anonymous)
		f.Exchanges = append(f.Exchanges, f.Exchanges[2]) // a second retry with the token
		rec := &recorder{TB: t}
		fakenexus.New(rec, f)
		errs := rec.failures()
		if len(errs) != 1 || !strings.Contains(errs[0], "dup--anon") || !strings.Contains(errs[0], catalogTarget) {
			t.Errorf("errors = %q, want one naming dup--anon and %s", errs, catalogTarget)
		}
	})
	t.Run("the same unauthenticated exchange in two fixtures", func(t *testing.T) {
		rec := &recorder{TB: t}
		fakenexus.New(rec, bearerFlow("one--anon", nexusfixture.Anonymous), bearerFlow("two--anon", nexusfixture.Anonymous))
		if errs := rec.failures(); len(errs) == 0 || !strings.Contains(joined(errs), "one--anon") || !strings.Contains(joined(errs), "two--anon") {
			t.Errorf("errors = %q, want them to name both fixtures", errs)
		}
	})
	t.Run("one request with and without a token is not a duplicate", func(t *testing.T) {
		rec := &recorder{TB: t}
		fakenexus.New(rec, bearerFlow("flow--anon", nexusfixture.Anonymous), bearerFlow("flow--pull", nexusfixture.Pull))
		if errs := rec.failures(); len(errs) != 0 {
			t.Errorf("errors = %q, want none: the identities and the token differ", errs)
		}
	})
}
