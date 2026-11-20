package nexusbench

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// DEVDESK-580, PR4 (DEVDESK-628, phase B): the `access` campaign holds 43
// fixtures, and the recorder refused 8 of them with "the path is not in the
// closed list". Four path shapes, all GET only, join the closed list:
//
//	GET /v2                                                  (no trailing slash)
//	GET /repository/<repo>/v2                                (no trailing slash)
//	GET /v2/<repo>                                           (exactly the repository, nothing after it)
//	GET /repository/<repo>/v2/<name>/tags/list               (the tags of an image, by the repository path)
//
// <repo> is one of the three repositories of the bench (docker-hosted,
// docker-proxy, docker-group), the same set the HEAD of a manifest knows
// (benchRepos): these four shapes widen the list as little as the fixtures need,
// and a repository the bench does not hold is outside it (a choice, flagged in
// the report of the task). Nothing else changes: no escape, no dot-dot, no
// double slash, and no query beyond n and last.

func TestRecordClosedListAcceptsTheFourAccessShapes(t *testing.T) {
	tests := []struct {
		target  string
		refused bool
	}{
		// The eight fixtures that were pending.
		{target: "/v2"},
		{target: "/repository/docker-hosted/v2"},
		{target: "/v2/docker-hosted"},
		{target: "/repository/docker-hosted/v2/api/tags/list"},
		// The same shapes on the other repositories of the bench.
		{target: "/repository/docker-proxy/v2"},
		{target: "/repository/docker-group/v2"},
		{target: "/v2/docker-proxy"},
		{target: "/v2/docker-group"},
		{target: "/repository/docker-proxy/v2/mirror/cached/tags/list"},
		{target: "/repository/docker-group/v2/team/api/tags/list"},
		{target: "/repository/docker-hosted/v2/team/api/tags/list?n=1"},
		{target: "/repository/docker-hosted/v2/team/api/tags/list?n=1&last=api"},
		// What the list already held.
		{target: "/v2/"},
		{target: "/v2/_catalog"},
		{target: "/v2/team/api/tags/list"},
		{target: "/v2/docker-hosted/_catalog"},
		{target: "/repository/docker-hosted/v2/_catalog"},

		// A repository the bench does not hold.
		{target: "/repository/maven-central/v2", refused: true},
		{target: "/v2/maven-central", refused: true},
		{target: "/v2/api", refused: true},
		{target: "/repository/maven-central/v2/api/tags/list", refused: true},
		// Nothing may follow the exact forms.
		{target: "/v2/docker-hosted/", refused: true},
		{target: "/v2/docker-hosted/extra", refused: true},
		{target: "/v2/docker-hosted/a/b", refused: true},
		{target: "/repository/docker-hosted/v2/", refused: true},
		{target: "/repository/docker-hosted/v2/x", refused: true},
		{target: "/repository/docker-hosted", refused: true},
		{target: "/repository/docker-hosted/", refused: true},
		{target: "/repository/v2", refused: true},
		{target: "/v2docker-hosted", refused: true},
		{target: "/v22", refused: true},
		// The tags of nothing, and what is not the tags.
		{target: "/repository/docker-hosted/v2/tags/list", refused: true},
		{target: "/repository/docker-hosted/v2/api/tags/list/extra", refused: true},
		{target: "/repository/docker-hosted/v2/api/tags/lists", refused: true},
		{target: "/repository/docker-hosted/v2/api/tags", refused: true},
		{target: "/repository/docker-hosted/v2/api/tags/list/", refused: true},
		// A manifest or a blob read can cache an image, whatever the shape.
		{target: "/repository/docker-hosted/v2/api/manifests/1.0.0", refused: true},
		{target: "/repository/docker-proxy/v2/mirror/cached/blobs/sha256:" + strings.Repeat("a", 64), refused: true},
		{target: "/v2/docker-hosted/manifests/1.0.0", refused: true},
		// Dot-dot, in every position.
		{target: "/v2/..", refused: true},
		{target: "/v2/../service/rest/v1/security/users", refused: true},
		{target: "/v2/docker-hosted/..", refused: true},
		{target: "/repository/docker-hosted/v2/..", refused: true},
		{target: "/repository/docker-hosted/..", refused: true},
		{target: "/repository/../v2", refused: true},
		{target: "/repository/docker-hosted/v2/../v2/api/tags/list", refused: true},
		{target: "/repository/docker-hosted/v2/api/../tags/list", refused: true},
		{target: "/repository/docker-hosted/v2/api/tags/list/..", refused: true},
		// A double slash.
		{target: "/v2//x", refused: true},
		{target: "//v2", refused: true},
		{target: "/v2//docker-hosted", refused: true},
		{target: "/repository//v2", refused: true},
		{target: "/repository/docker-hosted//v2", refused: true},
		{target: "/repository/docker-hosted/v2//api/tags/list", refused: true},
		{target: "/repository/docker-hosted/v2/api//tags/list", refused: true},
		// An invalid image name.
		{target: "/repository/docker-hosted/v2/Api/tags/list", refused: true},
		{target: "/repository/docker-hosted/v2/ap i/tags/list", refused: true},
		{target: "/repository/docker-hosted/v2/api:1/tags/list", refused: true},
		{target: "/repository/docker-hosted/v2/api@sha256/tags/list", refused: true},
		{target: "/repository/Docker-Hosted/v2", refused: true},
		{target: "/v2/Docker-Hosted", refused: true},
		// An escape or a fragment.
		{target: "/repository/docker-hosted/v2/a%2fb/tags/list", refused: true},
		{target: "/repository/docker-hosted/v2/%2e%2e/tags/list", refused: true},
		{target: "/repository/docker%2Dhosted/v2", refused: true},
		{target: "/v2%2Fdocker-hosted", refused: true},
		{target: "/v2/docker%2Dhosted", refused: true},
		{target: "/v2%2F", refused: true},
		{target: "/v2#x", refused: true},
		{target: "/v2/docker-hosted#x", refused: true},
		{target: "/repository/docker-hosted/v2#x", refused: true},
		// A query beyond n and last.
		{target: "/repository/docker-hosted/v2/api/tags/list?token=abc", refused: true},
		{target: "/repository/docker-hosted/v2/api/tags/list?n=1&foo=1", refused: true},
		{target: "/v2?token=abc", refused: true},
		{target: "/v2/docker-hosted?token=abc", refused: true},
		{target: "/repository/docker-hosted/v2?service=x&scope=y", refused: true},
		{target: "/v2/docker-hosted?last=a%20b", refused: true},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := refusedRequest("GET", tt.target)
			switch {
			case tt.refused && got == "":
				t.Errorf("refusedRequest(GET, %q) = \"\", want a refusal", tt.target)
			case !tt.refused && got != "":
				t.Errorf("refusedRequest(GET, %q) = %q, want it accepted", tt.target, got)
			}
		})
	}
}

// GET only: a verb other than GET is refused on each of the four shapes, both by
// the closed list and by the check of the first request of a fixture.
func TestRecordClosedListRefusesAnotherVerbOnTheAccessShapes(t *testing.T) {
	targets := []string{
		"/v2",
		"/repository/docker-hosted/v2",
		"/v2/docker-hosted",
		"/repository/docker-hosted/v2/api/tags/list",
	}
	for _, target := range targets {
		for _, method := range []string{"HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
			t.Run(method+" "+target, func(t *testing.T) {
				if got := refusedRequest(method, target); got == "" {
					t.Errorf("refusedRequest(%s, %s) accepted", method, target)
				}
				ex := nexusfixture.Exchange{Method: method, Target: target, Host: recRESTHost}
				for _, meta := range []nexusfixture.Meta{{}, {Flow: nexusfixture.FlowRedirects}, {Effect: nexusfixture.EffectCaches}} {
					if reason, check := refusedFixtureRequest(ex, meta, recRESTHost, true); reason == "" || check == "" {
						t.Errorf("refusedFixtureRequest(%s %s, meta %+v) = (%q, %q), want a refusal with what to do", method, target, meta, reason, check)
					}
				}
			})
		}
	}
}

// The check of the first request of a fixture lets a GET of the four shapes
// through, with or without a redirects flow (the ones of the campaign say
// "flow: redirects"), on either port.
func TestRecordFirstRequestOfAFixtureMayBeAnAccessShape(t *testing.T) {
	tests := []struct {
		host, target string
		meta         nexusfixture.Meta
	}{
		{recRESTHost, "/v2", nexusfixture.Meta{Flow: nexusfixture.FlowRedirects}},
		{recDockerHost, "/v2", nexusfixture.Meta{Flow: nexusfixture.FlowRedirects}},
		{recRESTHost, "/repository/docker-hosted/v2", nexusfixture.Meta{Flow: nexusfixture.FlowRedirects}},
		{recRESTHost, "/v2/docker-hosted", nexusfixture.Meta{Flow: nexusfixture.FlowRedirects}},
		{recRESTHost, "/repository/docker-hosted/v2/api/tags/list", nexusfixture.Meta{}},
		{recRESTHost, "/repository/docker-hosted/v2/api/tags/list?n=1", nexusfixture.Meta{}},
	}
	for _, tt := range tests {
		t.Run(tt.host+tt.target, func(t *testing.T) {
			ex := nexusfixture.Exchange{Method: "GET", Target: tt.target, Host: tt.host}
			if reason, check := refusedFixtureRequest(ex, tt.meta, recRESTHost, false); reason != "" {
				t.Errorf("refusedFixtureRequest refused GET %s: %s (%s)", tt.target, reason, check)
			}
		})
	}
}

// Every fixture of the embedded `access` campaign must be one the recorder may
// send, pending or recorded: it is the invariant of
// TestRecordMaySendEveryRequestOfTheEmbeddedCampaign (catalog), held for the
// campaign of PR4. Only the first exchange of a fixture is sent (a flow builds
// the others), so that is the one checked, with the check of the recorder
// itself, the one that also knows the HEAD of a manifest and the blob of a
// redirects flow. The fixtures are read from the source tree, not from the
// embedded copy, because nexusfixture.Load refuses a pending one.
func TestRecordMaySendEveryFirstRequestOfTheAccessCampaign(t *testing.T) {
	dir, err := nexusfixture.SourceDir()
	if err != nil {
		t.Fatalf("SourceDir: %v", err)
	}
	campaign := filepath.Join(dir, "access")
	entries, err := os.ReadDir(campaign)
	if err != nil {
		t.Fatalf("read the access campaign: %v", err)
	}
	restHost, _ := restOriginKey(recDescriptor())
	var seen int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".http") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(campaign, e.Name())) //nolint:gosec // a fixture of the source tree
		if err != nil {
			t.Fatal(err)
		}
		f, err := nexusfixture.Parse("access", e.Name(), data)
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		seen++
		if strings.HasPrefix(f.Meta.Record, "skip") {
			continue
		}
		ex := f.Exchanges[0]
		if reason, check := refusedFixtureRequest(ex, f.Meta, restHost, true); reason != "" {
			t.Errorf("%s: the recorder would refuse %s %s (%s; %s), and refuse the whole campaign with it", f.Name, ex.Method, ex.Target, reason, check)
		}
	}
	if seen != 43 {
		t.Errorf("the access campaign holds %d fixtures, want 43", seen)
	}
}

// End to end on the bench stand-in: the four shapes, as request-only fixtures of
// the `access` campaign, are asked for as the files wrote them, answered, written,
// and nothing else is sent.
func TestRecordFillsAPendingFixtureOfEachAccessShape(t *testing.T) {
	e := newRecEnv(t)
	shapes := []struct {
		name, host, target string
		meta               []string
		bench              *recBench
	}{
		{"redirects-v2-no-slash-30482--admin", recDockerHost, "/v2", []string{recMetaRedirects}, e.docker},
		{"redirects-repository-v2-no-slash--admin", recRESTHost, "/repository/docker-hosted/v2", []string{recMetaRedirects}, e.rest},
		{"redirects-hosted-path-root--admin", recRESTHost, "/v2/docker-hosted", []string{recMetaRedirects}, e.rest},
		{"routing-tags-hosted-repository--admin", recRESTHost, "/repository/docker-hosted/v2/api/tags/list", nil, e.rest},
	}
	want := map[string]string{}
	for _, s := range shapes {
		body := `{"shape":"` + s.name + `"}`
		s.bench.answer("admin", s.target, recAnswer{contentType: "application/json", body: body})
		e.add("access", s.name, recMetaRequest(s.meta, "GET", s.host, s.target, true))
		want[s.name] = body
	}
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures refused a shape of the access campaign: %v", err)
	}
	if len(rep.Written) != len(shapes) {
		t.Errorf("%d fixtures written, want %d: %v", len(rep.Written), len(shapes), rep.Written)
	}
	var sent []string
	for _, s := range append(e.rest.fixtureRequests(), e.docker.requests()...) {
		if s.method != "GET" {
			t.Errorf("a %s %s request was sent", s.method, s.uri)
		}
		sent = append(sent, s.uri)
	}
	slices.Sort(sent)
	wantSent := []string{"/repository/docker-hosted/v2", "/repository/docker-hosted/v2/api/tags/list", "/v2", "/v2/docker-hosted"}
	if !slices.Equal(sent, wantSent) {
		t.Errorf("the bench received %v, want exactly %v", sent, wantSent)
	}
	got := recSnapshot(t, e.dir)
	for name, body := range want {
		file, ok := got[filepath.Join(e.dir, "access", name+".http")]
		if !ok {
			t.Errorf("%s was not written", name)
			continue
		}
		if text := string(file.data); !strings.Contains(text, body) || !strings.Contains(text, "recorded-by: "+recRecordedBy) {
			t.Errorf("%s does not hold the recorded answer and its provenance:\n%s", name, text)
		}
	}
}

// A refused shape stops the run before anything is sent, even beside a fixture
// of an accepted shape (all or nothing).
func TestRecordRefusesAnAccessLookalikeBeforeSendingAnything(t *testing.T) {
	for _, target := range []string{
		"/repository/maven-central/v2",
		"/v2/api",
		"/repository/docker-hosted/v2/../v2",
		"/repository/docker-hosted/v2/Api/tags/list",
		"/v2//x",
	} {
		t.Run(target, func(t *testing.T) {
			e := newRecEnv(t)
			e.addGood()
			e.add("access", "z-bad--admin", recMetaRequest(nil, "GET", recRESTHost, target, true))
			_ = e.refuse("z-bad--admin")
			e.requireNoRequest()
		})
	}
}
