package oci

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The tag lister talks HTTP and takes the registry's base URL as an argument, so
// an httptest server standing in for a registry covers it without a seam.
// These tests moved from the OCI resources view with the code they cover.

func baseOf(r *http.Request) string { return "http://" + r.Host }

// The challenge is a comma-separated list of quoted pairs, and a scope value
// contains colons and slashes of its own — splitting on the first `=` is what
// keeps those intact.
func TestParseBearerChallenge(t *testing.T) {
	params := parseBearerChallenge(`realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`)

	if params["realm"] != "https://auth.docker.io/token" {
		t.Errorf("realm = %q", params["realm"])
	}
	if params["service"] != "registry.docker.io" {
		t.Errorf("service = %q", params["service"])
	}
	if params["scope"] != "repository:library/nginx:pull" {
		t.Errorf("scope = %q, want the colons kept", params["scope"])
	}
}

func TestAMalformedBearerChallengeYieldsNoParameters(t *testing.T) {
	if params := parseBearerChallenge("Bearer-with-no-pairs"); len(params) != 0 {
		t.Errorf("params = %v, want nothing from a challenge with no pairs", params)
	}
}

// ── Fetching tags ────────────────────────────────────────────────────────────

func TestTagsAreReadFromTheRegistry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/library/nginx/tags/list" {
			t.Errorf("requested %q, want the v2 tags endpoint", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"name":"library/nginx","tags":["1.25","latest"]}`))
	}))
	defer srv.Close()

	tags, err := ListRegistryTags(srv.URL, "library/nginx", "", "")
	if err != nil {
		t.Fatalf("ListRegistryTags: %v", err)
	}
	if strings.Join(tags, ",") != "1.25,latest" {
		t.Errorf("tags = %v, want both", tags)
	}
}

// Credentials configured for a registry have to reach it, or a private
// repository looks empty rather than forbidden.
func TestCredentialsAreSentAsBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		_, _ = w.Write([]byte(`{"tags":["v1"]}`))
	}))
	defer srv.Close()

	if _, err := ListRegistryTags(srv.URL, "api", "anthnel", "s3cret"); err != nil {
		t.Fatalf("ListRegistryTags: %v", err)
	}
	if gotUser != "anthnel" || gotPass != "s3cret" {
		t.Errorf("credentials sent = %q/%q, want the configured ones", gotUser, gotPass)
	}
}

// This is the flow Docker Hub uses: the first request is refused with a
// challenge naming a token endpoint, and the request is replayed with the token
// it hands back.
func TestA401IsAnsweredWithABearerTokenAndTheRequestReplayed(t *testing.T) {
	var authorised int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if user, _, _ := r.BasicAuth(); user != "anthnel" {
				t.Errorf("the token endpoint was called as %q, want the configured user", user)
			}
			if r.URL.Query().Get("service") != "registry.example.com" {
				t.Errorf("service = %q, want it carried from the challenge", r.URL.Query().Get("service"))
			}
			_, _ = w.Write([]byte(`{"token":"issued-token"}`))
		case r.Header.Get("Authorization") == "Bearer issued-token":
			authorised++
			_, _ = w.Write([]byte(`{"tags":["v1"]}`))
		default:
			w.Header().Set("Www-Authenticate", `Bearer realm="`+baseOf(r)+`/token",service="registry.example.com"`)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	tags, err := ListRegistryTags(srv.URL, "api", "anthnel", "s3cret")
	if err != nil {
		t.Fatalf("the bearer exchange failed: %v", err)
	}
	if authorised != 1 {
		t.Errorf("the request was replayed %d times, want exactly one", authorised)
	}
	if strings.Join(tags, ",") != "v1" {
		t.Errorf("tags = %v", tags)
	}
}

// Some registries answer with access_token rather than token; both name the
// same thing and a client that reads only one silently fails to authenticate.
func TestAnAccessTokenIsAcceptedInPlaceOfAToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_, _ = w.Write([]byte(`{"access_token":"issued-token"}`))
		case r.Header.Get("Authorization") == "Bearer issued-token":
			_, _ = w.Write([]byte(`{"tags":["v1"]}`))
		default:
			w.Header().Set("Www-Authenticate", `Bearer realm="`+baseOf(r)+`/token"`)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	if _, err := ListRegistryTags(srv.URL, "api", "", ""); err != nil {
		t.Fatalf("an access_token response was rejected: %v", err)
	}
}

func TestAChallengeThatIsNotBearerIsReportedRatherThanRetried(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Www-Authenticate", `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListRegistryTags(srv.URL, "api", "", "")
	if err == nil {
		t.Fatal("a Basic challenge was treated as a successful fetch")
	}
	if !strings.Contains(err.Error(), "unexpected auth challenge") {
		t.Errorf("err = %v, want the challenge quoted", err)
	}
}

func TestABearerChallengeWithNoRealmIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Www-Authenticate", `Bearer service="registry.example.com"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "missing realm") {
		t.Errorf("err = %v, want it to name the missing realm", err)
	}
}

func TestATokenEndpointThatRefusesIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Www-Authenticate", `Bearer realm="`+baseOf(r)+`/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v, want the token endpoint's status", err)
	}
}

// A token response with neither field is not an authentication: continuing with
// an empty bearer would produce a second, more confusing 401.
func TestATokenResponseWithNoTokenIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"expires_in":300}`))
			return
		}
		w.Header().Set("Www-Authenticate", `Bearer realm="`+baseOf(r)+`/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "no token field") {
		t.Errorf("err = %v, want it to say the response carried no token", err)
	}
}

func TestARegistryErrorStatusIsReportedWithItsCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := ListRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want the status code", err)
	}
}

func TestATagListThatIsNotJSONIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>proxy error</html>"))
	}))
	defer srv.Close()

	_, err := ListRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "decode tags response") {
		t.Errorf("err = %v, want it to name the decode step", err)
	}
}

// ── Pagination ───────────────────────────────────────────────────────────────

// A registry that pages answers a page at a time and says where the next one
// is. Docker Hub does not page unless asked (9 125 tags for library/node in one
// response), but others cap a response, and a truncated list would silently
// hide the newest tags — which are the ones a bump wants.
func TestALinkedTagListIsFollowedToItsEnd(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.RequestURI())
		switch r.URL.Query().Get("last") {
		case "":
			w.Header().Set("Link", `</v2/api/tags/list?last=b&n=2>; rel="next"`)
			_, _ = w.Write([]byte(`{"tags":["a","b"]}`))
		case "b":
			w.Header().Set("Link", `</v2/api/tags/list?last=d&n=2>; rel="next"`)
			_, _ = w.Write([]byte(`{"tags":["c","d"]}`))
		default:
			_, _ = w.Write([]byte(`{"tags":["e"]}`))
		}
	}))
	defer srv.Close()

	tags, err := ListRegistryTags(srv.URL, "api", "", "")
	if err != nil {
		t.Fatalf("ListRegistryTags: %v", err)
	}
	if got := strings.Join(tags, ","); got != "a,b,c,d,e" {
		t.Errorf("tags = %s, want every page in order", got)
	}
	if len(requested) != 3 {
		t.Errorf("made %d requests, want one per page: %v", len(requested), requested)
	}
}

// The token is earned once and sent with every following page; asking the
// token endpoint again for each would be a request per page for nothing.
func TestTheBearerTokenIsKeptAcrossPages(t *testing.T) {
	var tokenCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			tokenCalls++
			_, _ = w.Write([]byte(`{"token":"issued"}`))
		case r.Header.Get("Authorization") != "Bearer issued":
			w.Header().Set("Www-Authenticate", `Bearer realm="`+baseOf(r)+`/token"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Query().Get("last") == "":
			w.Header().Set("Link", `<`+baseOf(r)+`/v2/api/tags/list?last=a>; rel="next"`)
			_, _ = w.Write([]byte(`{"tags":["a"]}`))
		default:
			_, _ = w.Write([]byte(`{"tags":["b"]}`))
		}
	}))
	defer srv.Close()

	tags, err := ListRegistryTags(srv.URL, "api", "u", "p")
	if err != nil {
		t.Fatalf("ListRegistryTags: %v", err)
	}
	if got := strings.Join(tags, ","); got != "a,b" {
		t.Errorf("tags = %s", got)
	}
	if tokenCalls != 1 {
		t.Errorf("the token endpoint was called %d times, want once", tokenCalls)
	}
}

// A Link that points back at the page just read must end the walk, not be
// followed forever.
func TestALinkToTheSamePageEndsTheWalk(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Link", `<`+r.URL.RequestURI()+`>; rel="next"`)
		_, _ = w.Write([]byte(`{"tags":["a"]}`))
	}))
	defer srv.Close()

	tags, err := ListRegistryTags(srv.URL, "api", "", "")
	if err != nil {
		t.Fatalf("ListRegistryTags: %v", err)
	}
	if calls != 1 || len(tags) != 1 {
		t.Errorf("%d requests, tags %v; want one page", calls, tags)
	}
}

// A registry that always has a next page is bounded, not trusted.
func TestAnEndlessListIsCutOff(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Link", `</v2/api/tags/list?n=1&last=`+strings.Repeat("x", calls)+`>; rel="next"`)
		_, _ = w.Write([]byte(`{"tags":["a"]}`))
	}))
	defer srv.Close()

	_, err := ListRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "pages") {
		t.Errorf("err = %v, want the page bound named", err)
	}
	if calls > maxTagPages {
		t.Errorf("followed %d pages, bound is %d", calls, maxTagPages)
	}
}

func TestResolveNext(t *testing.T) {
	tests := []struct {
		name, link, want string
	}{
		{"none", "", ""},
		{"a path", `</v2/x/tags/list?last=b&n=100>; rel="next"`, "https://r.example/v2/x/tags/list?last=b&n=100"},
		{"absolute", `<https://other.example/v2/x?last=b>; rel="next"`, "https://other.example/v2/x?last=b"},
		{"not next", `</v2/x?last=b>; rel="prev"`, ""},
		{"several", `</v2/x?p=1>; rel="prev", </v2/x?p=3>; rel="next"`, "https://r.example/v2/x?p=3"},
	}
	for _, tt := range tests {
		if got := resolveNext("https://r.example/v2/x/tags/list", tt.link); got != tt.want {
			t.Errorf("%s: resolveNext = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// ── A tag's digest (§3.88) ───────────────────────────────────────────────────

// The digest is read with a HEAD that accepts an index: a multi-platform tag's
// digest is the index's, the one `docker pull` records.
func TestManifestDigestIsReadFromAHeadThatAcceptsAnIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer t" {
			w.Header().Set("Www-Authenticate", `Bearer realm="`+baseOf(r)+`/token",service="s"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodHead || r.URL.Path != "/v2/library/alpine/manifests/latest" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") {
			t.Errorf("Accept = %q, want the OCI index among them", r.Header.Get("Accept"))
		}
		w.Header().Set("Docker-Content-Digest", "sha256:abc")
	}))
	defer srv.Close()

	got, err := ManifestDigest(srv.URL, "library/alpine", "latest", "", "")
	if err != nil || got != "sha256:abc" {
		t.Errorf("ManifestDigest = %q, %v", got, err)
	}
}

func TestAnUnknownTagHasNoDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := ManifestDigest(srv.URL, "library/alpine", "nope", "", ""); err == nil {
		t.Error("a 404 was read as a digest")
	}
}

func TestRegistryAPIBase(t *testing.T) {
	for registry, want := range map[string]string{
		"":               "https://registry-1.docker.io",
		"ghcr.io":        "https://ghcr.io",
		"localhost:5000": "http://localhost:5000",
	} {
		if got := RegistryAPIBase(registry); got != want {
			t.Errorf("RegistryAPIBase(%q) = %q, want %q", registry, got, want)
		}
	}
}
