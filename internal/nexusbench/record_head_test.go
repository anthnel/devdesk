package nexusbench

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
)

// Step 7a of DEVDESK-580 (PR2): the closed list of the recorder gets three
// additions, each behind a guard.
//
//   - A HEAD of a manifest. Through docker-hosted (host 30481, path
//     /repository/docker-hosted/...) it changes nothing and is accepted. Through
//     docker-proxy, docker-group or the group connector (30482) it can cache an
//     image in the proxy: it is sent only when the fixture says "effect: caches"
//     AND the run says --allow-cache-effects (RecordOptions.AllowCacheEffects),
//     and the report says which fixtures were such, so that the command can ask
//     for a reset. A HEAD answer has no body: it is written "<empty body>".
//   - A GET of a blob, only on docker-hosted and only for a fixture that says
//     "flow: redirects"; a GET of a manifest stays refused whatever the
//     fixture says. The token paths are not in this list at all: only a flow
//     sends them (record_flow_test.go).
//   - The identity "badcred": Basic with the user admin and the public wrong
//     password fakenexus.BadPassword, no stored password needed.
//
// Everything is checked before the first request, so a refusal leaves the bench
// and the files alone (e.refuse, e.requireNoRequest).

const (
	recDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	recHostedManifest    = "/repository/docker-hosted/v2/team/api/manifests/1.0.0"
	recProxyManifest     = "/repository/docker-proxy/v2/mirror/uncached/manifests/1.0"
	recGroupManifest     = "/repository/docker-group/v2/mirror/uncached/manifests/1.0"
	recConnectorManifest = "/v2/mirror/uncached/manifests/1.0"
	recHostedBlob        = "/repository/docker-hosted/v2/team/api/blobs/" + recDigest
	recProxyBlob         = "/repository/docker-proxy/v2/mirror/cached/blobs/" + recDigest

	recMetaCaches    = "# effect: caches"
	recMetaRedirects = "# flow: redirects"
	recMetaBearer    = "# flow: bearer"
)

// recMetaRequest is a request-only fixture text with extra header lines (flow,
// effect, state) before "# record: pending".
func recMetaRequest(meta []string, method, host, target string, auth bool, extra ...string) string {
	text := recRequest(method, host, target, auth, extra...)
	if len(meta) == 0 {
		return text
	}
	return strings.Replace(text, "# record: pending\n", strings.Join(meta, "\n")+"\n# record: pending\n", 1)
}

// addMeta adds a request-only fixture of the catalog campaign with extra header
// lines; the identity is the suffix of the name.
func (e *recEnv) addMeta(name string, meta []string, method, host, target string, extra ...string) string {
	e.t.Helper()
	return e.add("catalog", name, recMetaRequest(meta, method, host, target, !strings.HasSuffix(name, "--anon"), extra...))
}

// headAnswer is what a HEAD of a manifest gets: the digest and the length, no body.
func headAnswer() recAnswer {
	return recAnswer{
		contentType: "application/vnd.docker.distribution.manifest.v2+json",
		header:      http.Header{"Docker-Content-Digest": {recDigest}, "Content-Length": {"1234"}},
	}
}

// ---- HEAD of a manifest on docker-hosted -----------------------------------------------

func TestRecordHeadOfAHostedManifestIsSentWithoutAFlag(t *testing.T) {
	e := newRecEnv(t)
	e.rest.answer("admin", recHostedManifest, headAnswer())
	path := e.addMeta("head-hosted--admin", nil, "HEAD", recRESTHost, recHostedManifest)

	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/head-hosted--admin"}; !slices.Equal(rep.Written, want) {
		t.Errorf("Written = %v, want %v", rep.Written, want)
	}
	if len(rep.CacheEffects) != 0 {
		t.Errorf("CacheEffects = %v, want none: a read through docker-hosted caches nothing", rep.CacheEffects)
	}
	ex := requireRecorded(t, path, "2026-11-18").Exchanges[0]
	if ex.Method != "HEAD" || ex.Target != recHostedManifest || ex.Host != recRESTHost {
		t.Errorf("request = %s %s on %s, want the HEAD of the file", ex.Method, ex.Target, ex.Host)
	}
	if ex.Status != 200 || ex.Stub != "<empty body>" || len(ex.Body) != 0 {
		t.Errorf("answer = %d, stub %q, body %q, want 200 and <empty body>", ex.Status, ex.Stub, ex.Body)
	}
	if got := ex.Header.Get("Docker-Content-Digest"); got != recDigest {
		t.Errorf("Docker-Content-Digest = %q, want %q kept: it is what a HEAD is for", got, recDigest)
	}
	if got := ex.Header.Get("Content-Length"); got != "1234" {
		t.Errorf("Content-Length = %q, want the 1234 the bench sent, not the length of an empty body", got)
	}
	seen := e.rest.fixtureRequests()
	if len(seen) != 1 || seen[0].method != "HEAD" || seen[0].uri != recHostedManifest || seen[0].user != "admin" {
		t.Errorf("the bench saw %+v, want one HEAD as admin", seen)
	}
}

func TestRecordHeadAnswerIsWrittenAsAnEmptyBodyWhateverTheIdentity(t *testing.T) {
	for _, id := range []string{"admin", "pull", "anon"} {
		t.Run(id, func(t *testing.T) {
			e := newRecEnv(t)
			user := map[string]string{"admin": "admin", "pull": "bench-pull", "anon": ""}[id]
			e.rest.answer(user, recHostedManifest, headAnswer())
			path := e.addMeta("head-hosted--"+id, nil, "HEAD", recRESTHost, recHostedManifest)
			if _, err := e.record(); err != nil {
				t.Fatalf("RecordFixtures: %v", err)
			}
			ex := requireRecorded(t, path, "2026-11-18").Exchanges[0]
			if ex.Stub != "<empty body>" {
				t.Errorf("stub = %q, want <empty body>", ex.Stub)
			}
		})
	}
}

// ---- HEAD that could cache an image --------------------------------------------------------

func TestRecordRefusesAHeadThatCouldCacheAnImage(t *testing.T) {
	tests := []struct {
		name, host, target string
		meta               []string
		want               string
	}{
		{"proxy, no effect", recRESTHost, recProxyManifest, nil, "can change the bench"},
		{"group, no effect", recRESTHost, recGroupManifest, nil, "can change the bench"},
		{"group connector, no effect", recDockerHost, recConnectorManifest, nil, "can change the bench"},
		{"proxy, effect and no flag", recRESTHost, recProxyManifest, []string{recMetaCaches}, "--allow-cache-effects"},
		{"group, effect and no flag", recRESTHost, recGroupManifest, []string{recMetaCaches}, "--allow-cache-effects"},
		{"group connector, effect and no flag", recDockerHost, recConnectorManifest, []string{recMetaCaches}, "--allow-cache-effects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.addGood()
			e.addMeta("z-bad--admin", tt.meta, "HEAD", tt.host, tt.target)
			be := e.refuse("z-bad--admin")
			if !strings.Contains(be.Error(), tt.want) {
				t.Errorf("the refusal %q does not say %q", be.Error(), tt.want)
			}
			e.requireNoRequest()
		})
	}
}

func TestRecordRefusalForTheFlagNamesTheFlagAndTheRunLeavesNothingBehind(t *testing.T) {
	e := newRecEnv(t)
	e.addMeta("z-bad--admin", []string{recMetaCaches}, "HEAD", recRESTHost, recProxyManifest)
	be := e.refuse("z-bad--admin")
	if !strings.Contains(be.Cause+" "+be.Check, "--allow-cache-effects") {
		t.Errorf("neither the cause %q nor the check %q names --allow-cache-effects", be.Cause, be.Check)
	}
	e.requireNoRequest()
}

func TestRecordSendsAHeadThroughTheProxyWhenTheFixtureAndTheRunBothSaidSo(t *testing.T) {
	tests := []struct {
		name, host, target string
		bench              func(e *recEnv) *recBench
	}{
		{"proxy", recRESTHost, recProxyManifest, func(e *recEnv) *recBench { return e.rest }},
		{"group", recRESTHost, recGroupManifest, func(e *recEnv) *recBench { return e.rest }},
		{"group connector", recDockerHost, recConnectorManifest, func(e *recEnv) *recBench { return e.docker }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.opts.AllowCacheEffects = true
			bench := tt.bench(e)
			bench.answer("admin", tt.target, headAnswer())
			e.rest.answer("admin", recHostedManifest, headAnswer())
			// The hosted one sorts first and is no cache effect.
			e.addMeta("a-hosted--admin", nil, "HEAD", recRESTHost, recHostedManifest)
			path := e.addMeta("head-cache--admin", []string{recMetaCaches}, "HEAD", tt.host, tt.target)

			rep, err := e.record()
			if err != nil {
				t.Fatalf("RecordFixtures: %v", err)
			}
			if want := []string{"catalog/a-hosted--admin", "catalog/head-cache--admin"}; !slices.Equal(rep.Written, want) {
				t.Errorf("Written = %v, want %v", rep.Written, want)
			}
			if want := []string{"catalog/head-cache--admin"}; !slices.Equal(rep.CacheEffects, want) {
				t.Errorf("CacheEffects = %v, want %v: the report says which fixtures can have cached an image", rep.CacheEffects, want)
			}
			f := requireRecorded(t, path, "2026-11-18")
			if f.Meta.Effect != nexusfixture.EffectCaches {
				t.Errorf("effect = %q, want %q kept in the file", f.Meta.Effect, nexusfixture.EffectCaches)
			}
			if ex := f.Exchanges[0]; ex.Method != "HEAD" || ex.Stub != "<empty body>" || ex.Header.Get("Docker-Content-Digest") != recDigest {
				t.Errorf("exchange = %s, stub %q, digest %q, want a HEAD with its digest and no body", ex.Method, ex.Stub, ex.Header.Get("Docker-Content-Digest"))
			}
			var heads int
			for _, s := range bench.fixtureRequests() {
				if s.method == "HEAD" && s.uri == tt.target {
					heads++
				}
			}
			if heads != 1 {
				t.Errorf("the bench saw %d HEAD of %s, want 1", heads, tt.target)
			}
		})
	}
}

func TestRecordKeepsAnEmptyCacheEffectListWhenNothingWasSent(t *testing.T) {
	e := newRecEnv(t)
	e.opts.AllowCacheEffects = true
	e.rest.answer("admin", recGoodTarget, recAnswer{contentType: "application/json", body: `[]`})
	e.pending("a-good--admin", recRESTHost, recGoodTarget)
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if len(rep.CacheEffects) != 0 {
		t.Errorf("CacheEffects = %v, want none: the flag alone sends nothing that caches", rep.CacheEffects)
	}
}

// The flag opens nothing by itself: a HEAD with no "effect: caches" through the
// proxy stays refused, and so does anything outside the manifest shape.
func TestRecordFlagAloneDoesNotOpenTheClosedList(t *testing.T) {
	e := newRecEnv(t)
	e.opts.AllowCacheEffects = true
	e.addGood()
	e.addMeta("z-bad--admin", nil, "HEAD", recRESTHost, recProxyManifest)
	be := e.refuse("z-bad--admin")
	if !strings.Contains(be.Error(), "can change the bench") {
		t.Errorf("the refusal %q does not say the request can change the bench", be.Error())
	}
	e.requireNoRequest()
}

func TestRecordRefusesAHeadThatIsNotAManifestOfALegitimateRepository(t *testing.T) {
	tests := []struct {
		name, host, target string
	}{
		{"the tags of hosted", recRESTHost, "/repository/docker-hosted/v2/team/api/tags/list"},
		{"the catalog of hosted", recRESTHost, "/repository/docker-hosted/v2/_catalog"},
		{"a blob of hosted", recRESTHost, recHostedBlob},
		{"a REST path", recRESTHost, "/service/rest/v1/repositories"},
		{"a manifest with a query", recRESTHost, recHostedManifest + "?x=1"},
		{"a dot-dot", recRESTHost, "/repository/docker-hosted/v2/../x/manifests/1.0"},
		{"an escaped slash", recRESTHost, "/repository/docker-hosted/v2/team%2Fapi/manifests/1.0"},
		{"an upper-case image name", recRESTHost, "/repository/docker-hosted/v2/Team/api/manifests/1.0"},
		{"a repository that is none of the bench", recRESTHost, "/repository/other-repo/v2/team/api/manifests/1.0"},
		{"hosted on the group connector host", recDockerHost, recHostedManifest},
		{"a manifest with no reference", recRESTHost, "/repository/docker-hosted/v2/team/api/manifests/"},
		{"an absolute URL", recRESTHost, "http://evil.example" + recHostedManifest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			// Even with the effect and the flag: nothing here is a manifest that may be sent.
			e.opts.AllowCacheEffects = true
			e.addGood()
			e.addMeta("z-bad--admin", []string{recMetaCaches}, "HEAD", tt.host, tt.target)
			_ = e.refuse("z-bad--admin")
			e.requireNoRequest()
		})
	}
}

// ---- GET of a manifest or a blob ------------------------------------------------------------

func TestRecordStillRefusesAGetOfAManifestOrABlob(t *testing.T) {
	tests := []struct {
		name, host, target string
		meta               []string
	}{
		{"a hosted manifest", recRESTHost, recHostedManifest, nil},
		{"a hosted manifest, with every opt-in", recRESTHost, recHostedManifest, []string{recMetaCaches, recMetaRedirects}},
		{"a proxy manifest, with every opt-in", recRESTHost, recProxyManifest, []string{recMetaCaches, recMetaRedirects}},
		{"a connector manifest, with every opt-in", recDockerHost, recConnectorManifest, []string{recMetaCaches, recMetaRedirects}},
		{"a hosted blob, with no flow", recRESTHost, recHostedBlob, nil},
		{"a hosted blob, with the bearer flow", recRESTHost, recHostedBlob, []string{recMetaBearer}},
		{"a hosted blob, with the cache effect", recRESTHost, recHostedBlob, []string{recMetaCaches}},
		{"a proxy blob, with the redirects flow", recRESTHost, recProxyBlob, []string{recMetaRedirects}},
		{"a group blob, with the redirects flow", recRESTHost, "/repository/docker-group/v2/mirror/cached/blobs/" + recDigest, []string{recMetaRedirects}},
		{"a connector blob, with the redirects flow", recDockerHost, "/v2/mirror/cached/blobs/" + recDigest, []string{recMetaRedirects}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.opts.AllowCacheEffects = true
			e.addGood()
			e.addMeta("z-bad--admin", tt.meta, "GET", tt.host, tt.target)
			be := e.refuse("z-bad--admin")
			if !strings.Contains(be.Error(), "can change the bench") {
				t.Errorf("the refusal %q does not say the read can change the bench", be.Error())
			}
			e.requireNoRequest()
		})
	}
}

func TestRecordGetsABlobOfTheHostedRepositoryForARedirectsFixture(t *testing.T) {
	e := newRecEnv(t)
	e.rest.answer("admin", recHostedBlob, recAnswer{contentType: "application/octet-stream", body: "layer-bytes"})
	path := e.addMeta("blob-hosted--admin", []string{recMetaRedirects}, "GET", recRESTHost, recHostedBlob)
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/blob-hosted--admin"}; !slices.Equal(rep.Written, want) || len(rep.CacheEffects) != 0 {
		t.Errorf("report = %+v, want %v written and no cache effect", rep, want)
	}
	f := requireRecorded(t, path, "2026-11-18")
	if f.Meta.Flow != nexusfixture.FlowRedirects {
		t.Errorf("flow = %q, want %q kept in the file", f.Meta.Flow, nexusfixture.FlowRedirects)
	}
	ex := f.Exchanges[0]
	if ex.Status != 200 || len(ex.Body) != 0 || !strings.HasPrefix(ex.Stub, "<body omitted:") {
		t.Errorf("answer = %d, body %q, stub %q, want 200 and the summary of the body, never the bytes", ex.Status, ex.Body, ex.Stub)
	}
	if strings.Contains(string(mustRead(t, path)), "layer-bytes") {
		t.Error("the file holds the bytes of the blob")
	}
}

// ---- the token paths are a flow's, never a fixture's -----------------------------------------

func TestRecordRefusesATokenRequestAsAFixtureOfItsOwn(t *testing.T) {
	token := "/v2/token?service=http://127.0.0.1:30482/v2/token&scope=registry:catalog:*"
	for _, meta := range [][]string{nil, {recMetaBearer}, {recMetaRedirects}} {
		for _, target := range []string{token, "/repository/docker-hosted/v2/token?service=http://127.0.0.1:30482/v2/token"} {
			t.Run(strings.Join(meta, "")+target, func(t *testing.T) {
				e := newRecEnv(t)
				e.addGood()
				e.addMeta("z-bad--admin", meta, "GET", recDockerHost, target)
				_ = e.refuse("z-bad--admin")
				e.requireNoRequest()
			})
		}
	}
}

// A flow opens no path: the first request of a flow goes through the closed list
// like any other.
func TestRecordFlowFixtureStillGoesThroughTheClosedList(t *testing.T) {
	for _, meta := range [][]string{{recMetaBearer}, {recMetaRedirects}} {
		for _, target := range []string{recHostedManifest, "/service/rest/v1/security/users", "/v2/_catalog?token=abc"} {
			t.Run(strings.Join(meta, "")+target, func(t *testing.T) {
				e := newRecEnv(t)
				e.addGood()
				e.addMeta("z-bad--admin", meta, "GET", recRESTHost, target)
				_ = e.refuse("z-bad--admin")
				e.requireNoRequest()
			})
		}
	}
}

// ---- the badcred identity -------------------------------------------------------------------

func TestRecordBadCredentialsSendsAdminWithTheWrongPublicPassword(t *testing.T) {
	e := newRecEnv(t)
	path := e.pending("login-refused--badcred", recDockerHost, "/v2/_catalog")
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/login-refused--badcred"}; !slices.Equal(rep.Written, want) {
		t.Errorf("Written = %v, want %v", rep.Written, want)
	}
	seen := e.docker.requests()
	if len(seen) != 1 {
		t.Fatalf("the bench saw %d request(s), want 1", len(seen))
	}
	want := "Basic " + recBasic("admin", fakenexus.BadPassword)
	if got := seen[0].header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want Basic with the user admin and %q: neither the real password nor the placeholder", got, fakenexus.BadPassword)
	}
	f := requireRecorded(t, path, "2026-11-18")
	if f.Identity != nexusfixture.BadCredentials {
		t.Errorf("identity = %q, want %q", f.Identity, nexusfixture.BadCredentials)
	}
	ex := f.Exchanges[0]
	if ex.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want the 401 of a refused login", ex.Status)
	}
	if got := ex.RequestHeader.Get("Authorization"); got != "Basic <redacted>" {
		t.Errorf("request credentials = %q, want the placeholder: the wrong password is public, the file says nothing of it", got)
	}
}

// The password of badcred is public and is no secret of the bench, so the run
// needs no stored password for it.
func TestRecordBadCredentialsNeedsNoStoredPassword(t *testing.T) {
	e := newRecEnv(t)
	e.opts.Secrets = Secrets{}
	e.pending("login-refused--badcred", recDockerHost, "/v2/_catalog")
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures with no stored password: %v", err)
	}
	if got := len(e.docker.requests()); got != 1 {
		t.Errorf("the bench saw %d request(s), want 1", got)
	}
}

// ---- header order ---------------------------------------------------------------------------

// A recorded file keeps its flow and effect headers where Format puts them.
func TestRecordKeepsTheFlowAndEffectHeadersOfTheFixture(t *testing.T) {
	e := newRecEnv(t)
	e.opts.AllowCacheEffects = true
	e.rest.answer("admin", recProxyManifest, headAnswer())
	path := e.addMeta("head-cache--admin", []string{recMetaCaches}, "HEAD", recRESTHost, recProxyManifest)
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	data := string(mustRead(t, path))
	if !strings.Contains(data, "# effect: caches\n# recorded-by: nexus-bench record\n") {
		t.Errorf("the file does not carry the effect header before recorded-by:\n%s", data)
	}
}
