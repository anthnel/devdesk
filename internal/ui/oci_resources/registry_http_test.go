package ociresources

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// The registry side of commands.go talks HTTP, and every entry point takes the
// base URL as an argument — so an httptest server standing in for a registry
// covers it without a seam. The one exception is the Docker Hub metadata call,
// which hardcodes hub.docker.com; redirectHTTP points the package's own client
// at the test server instead.

// ── URL shapes ───────────────────────────────────────────────────────────────

// A user types a registry the way they type it into `docker login`: usually a
// bare host, sometimes a full URL. Docker Hub is the case that cannot be
// derived, because the name it is pulled by is not the host that answers the
// API.
func TestRegistryAPIURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"registry.example.com", "https://registry.example.com"},
		{"registry.example.com/", "https://registry.example.com"},
		{"https://registry.example.com", "https://registry.example.com"},
		{"http://localhost:5000", "http://localhost:5000"},
		{"docker.io", "https://registry-1.docker.io"},
		{"Docker.IO", "https://registry-1.docker.io"},
		{"registry-1.docker.io", "https://registry-1.docker.io"},
	}
	for _, tc := range cases {
		if got := registryAPIURL(tc.in); got != tc.want {
			t.Errorf("registryAPIURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

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

	tags, err := fetchRegistryTags(srv.URL, "library/nginx", "", "")
	if err != nil {
		t.Fatalf("fetchRegistryTags: %v", err)
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

	if _, err := fetchRegistryTags(srv.URL, "api", "anthnel", "s3cret"); err != nil {
		t.Fatalf("fetchRegistryTags: %v", err)
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

	tags, err := fetchRegistryTags(srv.URL, "api", "anthnel", "s3cret")
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

	if _, err := fetchRegistryTags(srv.URL, "api", "", ""); err != nil {
		t.Fatalf("an access_token response was rejected: %v", err)
	}
}

func TestAChallengeThatIsNotBearerIsReportedRatherThanRetried(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Www-Authenticate", `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := fetchRegistryTags(srv.URL, "api", "", "")
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

	_, err := fetchRegistryTags(srv.URL, "api", "", "")
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

	_, err := fetchRegistryTags(srv.URL, "api", "", "")
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

	_, err := fetchRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "no token field") {
		t.Errorf("err = %v, want it to say the response carried no token", err)
	}
}

func TestARegistryErrorStatusIsReportedWithItsCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want the status code", err)
	}
}

func TestATagListThatIsNotJSONIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>proxy error</html>"))
	}))
	defer srv.Close()

	_, err := fetchRegistryTags(srv.URL, "api", "", "")
	if err == nil || !strings.Contains(err.Error(), "decode tags response") {
		t.Errorf("err = %v, want it to name the decode step", err)
	}
}

// The search runs against several registries at once and the answers arrive out
// of order, so each one has to say which registry it came from — and under the
// alias, since that is what the results table groups by.
func TestATagSearchNamesTheRegistryItAnswersFor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tags":["v1","v2"]}`))
	}))
	defer srv.Close()

	msg := run(t, searchRegistryTagsCmd("registry.example.com", "prod", srv.URL, "api", "", "")).(MultiRegistryTagsLoadedMsg)

	if msg.RegistryURL != "registry.example.com" || msg.Alias != "prod" || msg.Repo != "api" {
		t.Errorf("msg = %+v, want the registry, alias and repo it was asked about", msg)
	}
	if len(msg.Tags) != 2 || msg.Err != nil {
		t.Errorf("tags = %v, err = %v", msg.Tags, msg.Err)
	}
}

func TestAFailedTagSearchStillNamesItsRegistry(t *testing.T) {
	msg := run(t, searchRegistryTagsCmd("registry.example.com", "prod", "http://127.0.0.1:1", "api", "", "")).(MultiRegistryTagsLoadedMsg)

	if msg.Err == nil {
		t.Fatal("an unreachable registry was reported as having no tags")
	}
	if msg.RegistryURL != "registry.example.com" || msg.Alias != "prod" {
		t.Errorf("msg = %+v, want it named even on failure", msg)
	}
}

// ── Docker Hub metadata ──────────────────────────────────────────────────────

// Only Docker Hub has an API for this. Every other registry answers with no
// metadata and no error: the Updated column is simply empty, which is not a
// failure worth showing the user.
func TestOnlyDockerHubIsAskedForTagMetadata(t *testing.T) {
	msg := run(t, loadMultiRegistryTagsMetaCmd("registry.example.com", "api")).(MultiRegistryTagsMetaMsg)

	if msg.Err != nil || msg.Meta != nil {
		t.Errorf("msg = %+v, want a private registry to answer with nothing and no error", msg)
	}
}

// The Hub API is addressed as namespace/repository, so a bare name has no
// endpoint to call — and it is not an error either.
func TestAHubRepoWithNoNamespaceIsNotAsked(t *testing.T) {
	msg := run(t, loadMultiRegistryTagsMetaCmd("docker.io", "nginx")).(MultiRegistryTagsMetaMsg)

	if msg.Err != nil || msg.Meta != nil {
		t.Errorf("msg = %+v, want nothing asked for an unqualified repo", msg)
	}
}

func TestHubTagMetadataIsKeyedByTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v2/repositories/library/nginx/tags") {
			t.Errorf("requested %q, want the hub tags endpoint", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[
			{"name":"latest","last_updated":"2026-07-01T10:00:00.123456Z"},
			{"name":"broken","last_updated":"not a date"}
		]}`))
	}))
	defer srv.Close()
	redirectHTTP(t, srv.URL)

	msg := run(t, loadMultiRegistryTagsMetaCmd("docker.io", "library/nginx")).(MultiRegistryTagsMetaMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if _, ok := msg.Meta["latest"]; !ok {
		t.Errorf("Meta = %v, want the tag that carried a readable date", msg.Meta)
	}
	// An unparseable date drops that tag rather than the whole response: the
	// column is an enrichment, and one bad row must not blank the rest.
	if _, ok := msg.Meta["broken"]; ok {
		t.Error("a tag with an unreadable date was kept")
	}
}

func TestAHubErrorIsCarriedWithoutMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	redirectHTTP(t, srv.URL)

	msg := run(t, loadMultiRegistryTagsMetaCmd("docker.io", "library/nginx")).(MultiRegistryTagsMetaMsg)

	if msg.Err == nil {
		t.Fatal("a rate-limited hub was reported as having no metadata")
	}
	if msg.Meta != nil {
		t.Errorf("Meta = %v, want nothing alongside the error", msg.Meta)
	}
}

// ── Group detection ──────────────────────────────────────────────────────────

func TestAGroupsMembersAreDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/service/rest/v1/repositories/docker-group") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"type":"group","format":"docker",
			"attributes":{"group":{"memberNames":["dhi-proxy","internal-hosted"]}}}`))
	}))
	defer srv.Close()

	reg := config.RegistryItem{URL: srv.URL + "/repository/docker-group", Alias: "grp", Provider: config.ProviderNexus}
	msg := run(t, detectRegistryGroupCmd(reg, "")).(RegistryGroupDetectedMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if msg.RegistryURL != reg.URL {
		t.Errorf("RegistryURL = %q, want the registry that was probed", msg.RegistryURL)
	}
	if len(msg.Members) != 2 {
		t.Fatalf("Members = %+v, want both", msg.Members)
	}
	// The alias is what the browser shows, and the -proxy suffix is Nexus
	// bookkeeping rather than something a user typed.
	if msg.Members[0].Alias != "dhi" {
		t.Errorf("Alias = %q, want the suffix stripped", msg.Members[0].Alias)
	}
}

// A registry that is not a group is the ordinary case and must not look like a
// failure: the browser lists it flat.
func TestARegistryThatIsNotAGroupYieldsNoMembers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"hosted","format":"docker"}`))
	}))
	defer srv.Close()

	reg := config.RegistryItem{URL: srv.URL + "/repository/docker-hosted", Provider: config.ProviderNexus}
	msg := run(t, detectRegistryGroupCmd(reg, "")).(RegistryGroupDetectedMsg)

	if msg.Err != nil || msg.Members != nil {
		t.Errorf("msg = %+v, want no members and no error", msg)
	}
}

// D23, fixed. A manager that cannot be asked used to be reported exactly like
// one that answered "not a group" — `fetchRepoMeta` returned a bare ok=false and
// `DetectGroup` turned both into nil, nil. Harmless while the answer was thrown
// away on every open; not harmless once it is cached, since one unreachable
// minute would erase what was last known.
//
// Its sibling above pins the other half: a real "not a group" is still not an
// error, so this cannot pass by making every answer one.
func TestAManagerThatCannotBeAskedIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"the manager refuses", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}},
		{"the reply is not JSON", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>login</html>"))
		}},
	} {
		srv := httptest.NewServer(tc.handler)

		reg := config.RegistryItem{URL: srv.URL + "/repository/docker-group", Provider: config.ProviderNexus}
		msg := run(t, detectRegistryGroupCmd(reg, "")).(RegistryGroupDetectedMsg)
		srv.Close()

		if msg.Err == nil {
			t.Errorf("%s: reported as a settled 'not a group', which a cache would then store", tc.name)
		}
		if msg.Members != nil {
			t.Errorf("%s: Members = %+v, want none", tc.name, msg.Members)
		}
	}
}

// Docker stores credentials by hostname, and a repository manager's API may
// live on a different host than the registry. The lookup for the management URL
// therefore has to be made against its host alone, with the repository path
// stripped — otherwise nothing matches and the probe goes out anonymous.
func TestManagementCredentialsAreLookedUpByHostAlone(t *testing.T) {
	var gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, _, _ = r.BasicAuth()
		_, _ = w.Write([]byte(`{"type":"group","format":"docker",
			"attributes":{"group":{"memberNames":["dhi-proxy"]}}}`))
	}))
	defer srv.Close()

	host, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing the test server URL: %v", err)
	}
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{
			host.Scheme + "://" + host.Host: map[string]string{"auth": encodeAuth("nexus-admin", "s3cret")},
		},
	})

	reg := config.RegistryItem{
		URL:           "registry.example.com/repository/docker-group",
		ManagementURL: srv.URL + "/repository/docker-group",
		AuthMode:      config.AuthCredentials,
		Provider:      config.ProviderNexus,
	}
	msg := run(t, detectRegistryGroupCmd(reg, "")).(RegistryGroupDetectedMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if gotUser != "nexus-admin" {
		t.Errorf("the probe authenticated as %q, want the credentials stored for the management host", gotUser)
	}
	if len(msg.Members) != 1 {
		t.Errorf("Members = %+v, want the one member", msg.Members)
	}
}

// D12, fixed. Docker keys credentials by host, so one `docker login` against a
// Nexus instance made every repository it serves authenticate as that user —
// including the ones the user had marked as needing no authentication, because
// nothing on this path ever read the flag. The mode is now read before the
// lookup, and the probe goes out with nothing.
//
// Its sibling above proves the lookup still happens when the mode allows it, so
// this one cannot pass by breaking credentials outright.
func TestAnAnonymousRegistryIsProbedWithoutCredentials(t *testing.T) {
	var authenticated bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, authenticated = r.BasicAuth()
		_, _ = w.Write([]byte(`{"type":"group","format":"docker",
			"attributes":{"group":{"memberNames":["dhi-proxy"]}}}`))
	}))
	defer srv.Close()

	host, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing the test server URL: %v", err)
	}
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{
			host.Scheme + "://" + host.Host: map[string]string{"auth": encodeAuth("nexus-admin", "s3cret")},
		},
	})

	reg := config.RegistryItem{
		URL:           srv.URL + "/repository/docker-group",
		ManagementURL: srv.URL + "/repository/docker-group",
		AuthMode:      config.AuthAnonymous,
		Provider:      config.ProviderNexus,
	}
	msg := run(t, detectRegistryGroupCmd(reg, "")).(RegistryGroupDetectedMsg)

	if authenticated {
		t.Error("the probe sent the credentials stored for the host to a registry marked anonymous")
	}
	if msg.Err != nil {
		t.Fatalf("Err = %v — anonymous is not an error", msg.Err)
	}
	if len(msg.Members) != 1 {
		t.Errorf("Members = %+v, want the detection to have run anyway", msg.Members)
	}
}

// A username configured on an anonymous entry is not a way back in: the mode is
// the decision, and half a credential is still a credential.
func TestAnAnonymousRegistrySendsNoUsernameEither(t *testing.T) {
	var gotUser string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, _, _ = r.BasicAuth()
		_, _ = w.Write([]byte(`{"type":"group","format":"docker"}`))
	}))
	defer srv.Close()

	reg := config.RegistryItem{
		URL:      srv.URL + "/repository/docker-group",
		Username: "configured",
		AuthMode: config.AuthAnonymous,
		Provider: config.ProviderNexus,
	}
	run(t, detectRegistryGroupCmd(reg, ""))

	if gotUser != "" {
		t.Errorf("the probe authenticated as %q, want nothing sent", gotUser)
	}
}

// A password typed into the form outranks anything stored: it is the more
// recent statement of intent, and it is how a wrong stored credential is
// worked around.
func TestAnExplicitPasswordIsNotOverriddenByStoredCredentials(t *testing.T) {
	var gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotPass, _ = r.BasicAuth()
		_, _ = w.Write([]byte(`{"type":"group","format":"docker"}`))
	}))
	defer srv.Close()

	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{
			srv.URL + "/repository/docker-group": map[string]string{"auth": encodeAuth("stored", "stored-pass")},
		},
	})

	reg := config.RegistryItem{URL: srv.URL + "/repository/docker-group", Username: "typed", Provider: config.ProviderNexus}
	run(t, detectRegistryGroupCmd(reg, "typed-pass"))

	if gotPass != "typed-pass" {
		t.Errorf("the probe authenticated with %q, want the password from the form", gotPass)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// baseOf rebuilds the URL a handler is answering on, so a challenge can name a
// realm on the same test server.
func baseOf(r *http.Request) string {
	return "http://" + r.Host
}

// redirectHTTP points the package's HTTP client at target, whatever host a
// request names. fetchDockerHubTagsMeta builds a hub.docker.com URL itself, so
// this is the only way to stand in for it.
func redirectHTTP(t *testing.T, target string) {
	t.Helper()

	to, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parsing %q: %v", target, err)
	}
	previous := ociHTTPClient
	ociHTTPClient = &http.Client{Transport: rewriteHost{to: to}}
	t.Cleanup(func() { ociHTTPClient = previous })
}

type rewriteHost struct{ to *url.URL }

func (rw rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	routed := req.Clone(req.Context())
	routed.URL.Scheme = rw.to.Scheme
	routed.URL.Host = rw.to.Host
	routed.Host = rw.to.Host
	return http.DefaultTransport.RoundTrip(routed)
}
