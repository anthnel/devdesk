package ociresources

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/imagesearch"
)

// DEVDESK-306: a private registry is searched through its catalog, inside the
// browser, with the production routing (Hub and catalog) and no real registry.

// wiredLikeProduction is the production routing with Docker Hub pointed at a
// fake; the catalog searcher is the real one, walking the entry's own server.
func wiredLikeProduction(hub *httptest.Server) imagesearch.Searcher {
	return imagesearch.ByRegistry(
		imagesearch.HubSearcher{Endpoint: hub.URL, Client: hub.Client()},
		imagesearch.CatalogSearcher{})
}

// A one-character text skips the private entry without a request; the Hub
// entry is still asked.
func TestAOneCharacterQuerySkipsThePrivateEntryAndAsksHub(t *testing.T) {
	hub, hubRequests := fakeHub(t)
	priv, privRequests := catalogServer(t, "api", "alpha")
	withImageSearcher(t, wiredLikeProduction(hub))

	m := registryModel(t,
		anonymousRegistry("docker.io", "hub"),
		anonymousRegistry(priv.URL, "private"))
	m, cmd := submitRaw(t, m, "a")
	if cmd == nil || m.registryBrowser.state != browserStateImages {
		t.Fatalf("the search did not start (state %d)", m.registryBrowser.state)
	}
	results := runCmds(t, cmd)
	for range 2 {
		m = feed(t, m, recvResult(t, results))
	}

	got := m.RegistryBrowserOutcomes()
	if want := (imagesearch.Outcome{Verdict: imagesearch.VerdictNoResult, Skipped: true}); got["private"] != want {
		t.Errorf("private outcome = %+v, want %+v", got["private"], want)
	}
	if got["hub"].Verdict != imagesearch.VerdictOK {
		t.Errorf("hub outcome = %+v, want ok", got["hub"])
	}
	if n := privRequests.Load(); n != 0 {
		t.Errorf("the private catalog got %d requests, want 0", n)
	}
	if n := hubRequests.Load(); n != 1 {
		t.Errorf("the fake Hub got %d requests, want 1", n)
	}
}

// R1 end to end: an entry whose prefix is absent from the catalog is refused,
// adds no row, and the journal says so at INFO without the typed text.
func TestAPrefixAbsentFromTheCatalogIsRefusedAndLogged(t *testing.T) {
	hub, _ := fakeHub(t)
	priv, privRequests := catalogServer(t, "other/nginx", "team/nginx-base")
	withImageSearcher(t, wiredLikeProduction(hub))
	logs := captureLog(t)

	m := registryModel(t, anonymousRegistry(priv.URL, "private"))
	for i := range m.registryBrowser.entries {
		m.registryBrowser.entries[i].repoPrefix = "docker-hosted"
	}
	m, cmd := submitRaw(t, m, "nginx")
	res, ok := cmd().(ImageSearchResultMsg)
	if !ok {
		t.Fatal("the search did not answer with an ImageSearchResultMsg")
	}
	m = feed(t, m, res)

	if privRequests.Load() == 0 {
		t.Error("the catalog was never walked")
	}
	wantVerdict(t, m, "private", imagesearch.VerdictRefused)
	if got := rowsOf(m); len(got) != 0 {
		t.Errorf("rows = %v, want none", got)
	}
	out := logs.String()
	const line = "INFO [oci_resources] image search on private: unsupported or refused: no catalog name carries the entry's prefix"
	if !strings.Contains(out, line) {
		t.Errorf("journal = %q, want it to contain %q", out, line)
	}
	if strings.Contains(out, "ERROR") {
		t.Errorf("a refusal was logged as an error: %q", out)
	}
	for _, leak := range []string{"nginx", "Authorization", "Bearer"} {
		if strings.Contains(out, leak) {
			t.Errorf("the journal holds %q: %q", leak, out)
		}
	}
}

// A catalog that never answers fails at the deadline, adds no row, and the
// rows another entry delivered before stay.
func TestACatalogThatNeverAnswersFailsAtTheDeadlineAndKeepsOtherRows(t *testing.T) {
	withSearchDeadline(t, 150*time.Millisecond)
	hub, _ := fakeHub(t)
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) }) // runs first: frees the handler before Close
	withImageSearcher(t, wiredLikeProduction(hub))

	m := registryModel(t,
		anonymousRegistry("docker.io", "hub"),
		anonymousRegistry(slow.URL, "private"))
	m, cmd := submitRaw(t, m, "nginx")
	results := runCmds(t, cmd)

	first := recvResult(t, results)
	if first.EntryKey != "hub" {
		t.Fatalf("the first answer is %q, want hub", first.EntryKey)
	}
	m = feed(t, m, first)
	before := rowsOf(m)
	if len(before) != 3 {
		t.Fatalf("rows after the Hub answer = %v, want 3", before)
	}

	m = feed(t, m, recvResult(t, results))
	wantVerdict(t, m, "private", imagesearch.VerdictFailed)
	wantVerdict(t, m, "hub", imagesearch.VerdictOK)
	if got := rowsOf(m); !equal(got, before) {
		t.Errorf("rows = %v after the deadline, want the Hub's %v", got, before)
	}
}
