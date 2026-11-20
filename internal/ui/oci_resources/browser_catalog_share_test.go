package ociresources

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
)

// DEVDESK-311, step 2: the browser's lot shares catalog walks. Entries of one
// batch that reach the same catalog with the same identity cost one request.
//
// The entries here are generic registries: several of them behind one host that
// serves ONE root /v2/_catalog whose names carry each entry's prefix (D140, R1).
// Nexus members are not in this file: a Nexus member's catalog is served under
// its own path, never at the root, and the browser walks one catalog per member
// (browser_nexus_member_path_test.go, DEVDESK-579).

// sharedPrefixes are the prefixes of three generic entries on one host.
var sharedPrefixes = []string{"hosted", "proxy", "group"}

// genericEntry is an anonymous standalone registry at url with a repo prefix. It
// declares no parent and no provider: nothing makes it a Nexus member.
func genericEntry(base, slug, prefix string) config.RegistryItem {
	return config.RegistryItem{
		URL: base, Alias: slug, Slug: slug, Kind: config.KindRegistry,
		RepoPrefix: prefix, AuthMode: config.AuthAnonymous,
	}
}

// sharedHostModel opens the browser on three generic entries that all point at
// base, each with its own repo prefix.
func sharedHostModel(t *testing.T, base string) Model {
	t.Helper()
	var items []config.RegistryItem
	for _, p := range sharedPrefixes {
		items = append(items, genericEntry(base, p, p))
	}
	return registryModel(t, items...)
}

// runLot submits text, runs the whole lot and feeds every answer back.
func runLot(t *testing.T, m Model, text string, n int) Model {
	t.Helper()
	m, cmd := submitRaw(t, m, text)
	if cmd == nil || m.registryBrowser.state != browserStateImages {
		t.Fatalf("the search did not start (state %d)", m.registryBrowser.state)
	}
	results := runCmds(t, cmd)
	for range n {
		m = feed(t, m, recvResult(t, results))
	}
	return m
}

// catalogRequests serves one shared /v2/_catalog whose names carry the entry
// prefix (hosted/nginx, proxy/nginx-x, group/nginx-y), the way a generic
// registry that fronts several repositories does.
func catalogRequests(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/_catalog" {
			http.NotFound(w, r)
			return
		}
		n.Add(1)
		if status != http.StatusOK {
			http.Error(w, "down", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"repositories":["hosted/nginx","proxy/nginx-x","group/nginx-y","other/nginx"]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// Three generic entries of one host and one identity read the root catalog once
// (D140): each keeps only the names under its own prefix (R1).
func TestThreeGenericEntriesOnOneCatalogWalkItOnce(t *testing.T) {
	srv, walks := catalogRequests(t, http.StatusOK)
	withImageSearcher(t, wiredLikeProduction(srv))

	m := runLot(t, sharedHostModel(t, srv.URL), "nginx", 3)

	if n := walks.Load(); n != 1 {
		t.Errorf("/v2/_catalog got %d requests, want 1", n)
	}
	for _, p := range sharedPrefixes {
		wantVerdict(t, m, p, imagesearch.VerdictOK)
	}
	rows := rowsOf(m)
	if len(rows) != 3 {
		t.Fatalf("rows = %v, want 3", rows)
	}
	for _, p := range sharedPrefixes {
		want := p + ":" + p + "/"
		if n := countPrefix(rows, want); n != 1 {
			t.Errorf("rows under %q = %d in %v, want 1 (own prefix only)", want, n, rows)
		}
	}
	for _, r := range rows {
		if strings.Contains(r, ":other/") {
			t.Errorf("row %q is outside every entry prefix", r)
		}
	}
	// Nothing here is a Nexus member: no "May omit images" line.
	if _, vals := headerLines(m); vals[mayOmitKey] != "" {
		t.Errorf("%s = %q, want none for generic entries", mayOmitKey, vals[mayOmitKey])
	}
}

func countPrefix(rows []string, prefix string) int {
	n := 0
	for _, r := range rows {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

// The D140 contract for registries that are not Nexus: one failed shared walk
// fails every entry that shared it, and costs one request.
func TestAFailedSharedWalkFailsEveryMemberWithOneRequest(t *testing.T) {
	srv, walks := catalogRequests(t, http.StatusServiceUnavailable)
	withImageSearcher(t, wiredLikeProduction(srv))

	m := runLot(t, sharedHostModel(t, srv.URL), "nginx", 3)

	if n := walks.Load(); n != 1 {
		t.Errorf("/v2/_catalog got %d requests, want 1", n)
	}
	for _, p := range sharedPrefixes {
		wantVerdict(t, m, p, imagesearch.VerdictFailed)
	}
	_, vals := headerLines(m)
	for _, p := range sharedPrefixes {
		if !strings.Contains(vals["Failed"], p) {
			t.Errorf("Failed = %q, want it to list %s", vals["Failed"], p)
		}
	}
}

// A standalone entry with a repo prefix keeps the root catalog and R1: only an
// entry whose group is declared `provider: nexus` is read under its prefix.
func TestAStandaloneEntryWithAPrefixKeepsTheRootCatalogAndR1(t *testing.T) {
	srv, walks := catalogRequests(t, http.StatusOK)
	withImageSearcher(t, wiredLikeProduction(srv))

	// One entry: tea.Batch of a single Cmd is that Cmd, so it is run directly.
	m, cmd := submitRaw(t, registryModel(t, genericEntry(srv.URL, "solo", "hosted")), "nginx")
	res, ok := cmd().(ImageSearchResultMsg)
	if !ok {
		t.Fatal("the search did not answer with an ImageSearchResultMsg")
	}
	m = feed(t, m, res)

	if n := walks.Load(); n != 1 {
		t.Errorf("/v2/_catalog got %d requests, want 1 (the root catalog)", n)
	}
	wantVerdict(t, m, "solo", imagesearch.VerdictOK)
	if got, want := rowsOf(m), []string{"solo:hosted/nginx"}; !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestTwoIdentitiesOnOneURLWalkTwice(t *testing.T) {
	srv, walks := catalogRequests(t, http.StatusOK)
	withImageSearcher(t, wiredLikeProduction(srv))
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{u.Host: map[string]string{"auth": encodeAuth("alice", "s3cret")}},
	})
	authed := config.RegistryItem{URL: srv.URL, Alias: "authed", Slug: "authed", AuthMode: config.AuthCredentials}

	m := registryModel(t, anonymousRegistry(srv.URL, "anon"), authed)
	m = runLot(t, m, "nginx", 2)

	if n := walks.Load(); n != 2 {
		t.Errorf("/v2/_catalog got %d requests, want 2 (one per identity)", n)
	}
	wantVerdict(t, m, "anon", imagesearch.VerdictOK)
	wantVerdict(t, m, "authed", imagesearch.VerdictOK)
}

func TestTwoSuccessiveSearchesWalkTwice(t *testing.T) {
	srv, walks := catalogRequests(t, http.StatusOK)
	withImageSearcher(t, wiredLikeProduction(srv))

	m := runLot(t, sharedHostModel(t, srv.URL), "nginx", 3)
	b := m.registryBrowser
	_, cmd := b.startImageSearch("nginx", slices.Clone(b.entries))
	results := runCmds(t, cmd)
	for range 3 {
		recvResult(t, results)
	}
	if n := walks.Load(); n != 2 {
		t.Errorf("/v2/_catalog got %d requests over two searches, want 2 (no cache between searches)", n)
	}
}

// A walk still in flight when a new lot replaces its lot gives the new lot no
// late result: the generation drops the old answer.
func TestALateWalkOfAReplacedLotGivesTheNewLotNothing(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		_, _ = w.Write([]byte(`{"repositories":["hosted/nginx"]}`))
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	withImageSearcher(t, wiredLikeProduction(srv))

	m, cmd := submitRaw(t, sharedHostModel(t, srv.URL), "nginx")
	old := runCmds(t, cmd)
	<-entered
	oldGen := m.registryBrowser.gen

	// The new lot replaces the old one (and cancels its context).
	b := m.registryBrowser
	b.startImageSearch("nginx", nil)
	if b.gen == oldGen {
		t.Fatal("the new lot kept the old generation")
	}
	late := recvResult(t, old)
	m = feed(t, m, late)
	if rows := rowsOf(m); len(rows) != 0 {
		t.Errorf("rows = %v, want none from the replaced lot", rows)
	}
	if got := m.RegistryBrowserOutcomes(); len(got) != 0 {
		t.Errorf("outcomes = %v, want none from the replaced lot", got)
	}
}
