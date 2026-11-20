package nexusbench

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// DEVDESK-579: the catalog of a Nexus repository is read under its path,
// "GET /v2/<repository>/_catalog". The recorder writes all or nothing, so a
// request of the embedded campaign that the closed list refuses would make
// `mise run nexus-bench:record` refuse the whole campaign.

func TestRecordClosedListAcceptsTheCatalogOfARepositoryPath(t *testing.T) {
	tests := []struct {
		target  string
		refused bool
	}{
		// What the new fixtures ask for, and the query DevDesk adds.
		{target: "/v2/docker-hosted/_catalog"},
		{target: "/v2/docker-proxy/_catalog"},
		{target: "/v2/docker-group/_catalog"},
		{target: "/v2/docker-hosted/_catalog?n=1000"},
		{target: "/v2/docker-hosted/_catalog?n=2&last=shared/base"},
		{target: "/v2/a/b/_catalog"},
		// Unchanged shapes.
		{target: "/v2/_catalog"},
		{target: "/v2/_catalog?n=1000"},
		{target: "/v2/team/api/tags/list"},

		// Still refused: nothing but a catalog may follow the repository path.
		{target: "/v2/docker-hosted/_catalog/extra", refused: true},
		{target: "/v2/docker-hosted/_catalogs", refused: true},
		{target: "/v2/docker-hosted/_catalog?token=abc", refused: true},
		{target: "/v2/docker-hosted/_catalog?n=1000&foo=1", refused: true},
		{target: "/v2/docker-hosted/manifests/_catalog", refused: true},
		{target: "/v2/docker-hosted/blobs/_catalog", refused: true},
		{target: "/v2/docker-hosted/_catalog/../../service/rest/v1/security/users", refused: true},
		{target: "/v2/../service/rest/v1/security/users/_catalog", refused: true},
		{target: "/v2/a/../b/_catalog", refused: true},
		{target: "/v2/a%2fb/_catalog", refused: true},
		{target: "/v2/%2e%2e/_catalog", refused: true},
		{target: "/v2//_catalog", refused: true},
		{target: "/v2/a//b/_catalog", refused: true},
		{target: "/v2/Docker-Hosted/_catalog", refused: true},
		{target: "/v2/docker hosted/_catalog", refused: true},
		{target: "/v2/docker-hosted/x/manifests/1.0.0", refused: true},
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

// A refused method stays refused on the new shape.
func TestRecordClosedListRefusesANonGETOnACatalogPath(t *testing.T) {
	for _, method := range []string{"HEAD", "POST", "DELETE"} {
		if got := refusedRequest(method, "/v2/docker-hosted/_catalog"); got == "" {
			t.Errorf("refusedRequest(%s, /v2/docker-hosted/_catalog) accepted", method)
		}
	}
}

// Every request of the embedded campaign must be one the recorder may send:
// otherwise a fixture added by hand (a copy of a measurement) turns the next
// `nexus-bench record` into a refusal of the whole campaign. This holds for the
// fixtures of today and for the twelve of DEVDESK-579.
func TestRecordMaySendEveryRequestOfTheEmbeddedCampaign(t *testing.T) {
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	for _, f := range fixtures {
		for _, ex := range f.Exchanges {
			if why := refusedRequest(ex.Method, ex.Target); why != "" {
				t.Errorf("%s: the recorder would refuse %s %s (%s), and refuse the whole campaign with it", f.Name, ex.Method, ex.Target, why)
			}
		}
	}
}

// End to end on the bench stand-in: a request-only fixture of the new shape is
// requested as the file wrote it, answered, redacted and written, and nothing
// else is sent.
func TestRecordFillsAPendingCatalogOfARepositoryPath(t *testing.T) {
	e := newRecEnv(t)
	const target = "/v2/docker-hosted/_catalog"
	body := `{"repositories":["api","shared/base","team/api","tools/cli"]}`
	for _, user := range []string{"admin", "bench-pull", ""} {
		e.docker.answer(user, target, recAnswer{contentType: "application/json", body: body})
	}
	for _, id := range []string{"admin", "pull", "anon"} {
		e.pending("catalog-hosted-path--"+id, recDockerHost, target)
	}
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures refused a catalog by path: %v", err)
	}
	if len(rep.Written) != 3 {
		t.Errorf("%d fixtures written, want 3: %v", len(rep.Written), rep.Written)
	}
	for _, s := range e.docker.requests() {
		if s.method != "GET" || s.uri != target {
			t.Errorf("the bench received %s %s, want GET %s only", s.method, s.uri, target)
		}
	}
	got := recSnapshot(t, e.dir)
	var found int
	for path, file := range got {
		if strings.Contains(path, "catalog-hosted-path--") {
			found++
			if text := string(file.data); !strings.Contains(text, body) || !strings.Contains(text, "recorded-by: "+recRecordedBy) {
				t.Errorf("%s does not hold the recorded answer and its provenance:\n%s", path, text)
			}
		}
	}
	if found != 3 {
		t.Errorf("%d recorded files found, want 3", found)
	}
}
