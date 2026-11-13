package ociresources

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-311, step 2: the browser's lot shares catalog walks. Members of one
// batch that reach the same catalog with the same identity cost one request.

var shareMembers = []string{"hosted", "proxy", "group"}

// nexusGroupModel opens the browser on an anonymous Nexus group whose three
// discovered members all point at url, each with its own repo prefix.
func nexusGroupModel(t *testing.T, url string) Model {
	t.Helper()
	group := nexusGroup()
	group.AuthMode = config.AuthAnonymous
	group.URL = url
	cfg := testConfig()
	cfg.Registry.Registries = []config.RegistryItem{group}
	var members []cache.RegistryGroupMember
	for _, p := range shareMembers {
		members = append(members, cache.RegistryGroupMember{Alias: p, URL: url, RepoPrefix: p})
	}
	m := feed(t, New(cfg), testutil.Resize(200, 40),
		ImagesListMsg{Images: imageFixtures()},
		RegistryGroupCacheLoadedMsg{Entries: map[string]cache.RegistryGroupEntry{"nx": {Members: members}}},
	)
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("'b' did not open the registry browser")
	}
	return m
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

func TestThreeNexusMembersOnOneCatalogWalkItOnce(t *testing.T) {
	srv, walks := catalogRequests(t, http.StatusOK)
	withImageSearcher(t, wiredLikeProduction(srv))

	m := runLot(t, nexusGroupModel(t, srv.URL), "nginx", 3)

	if n := walks.Load(); n != 1 {
		t.Errorf("/v2/_catalog got %d requests, want 1", n)
	}
	got := m.RegistryBrowserOutcomes()
	for _, p := range shareMembers {
		key := memberKey("nx", srv.URL, p)
		if o, ok := got[key]; !ok || o.Verdict != imagesearch.VerdictOK {
			t.Errorf("outcome of %s = %+v (present %v), want ok", key, o, ok)
		}
	}
	rows := rowsOf(m)
	if len(rows) != 3 {
		t.Fatalf("rows = %v, want 3", rows)
	}
	for _, p := range shareMembers {
		want := memberKey("nx", srv.URL, p) + ":" + p + "/"
		if n := countPrefix(rows, want); n != 1 {
			t.Errorf("rows under %q = %d in %v, want 1 (own prefix only)", want, n, rows)
		}
	}
	for _, r := range rows {
		if strings.Contains(r, ":other/") {
			t.Errorf("row %q is outside every member prefix", r)
		}
	}
	_, vals := headerLines(m)
	want := "nx/hosted, nx/proxy, nx/group"
	if vals["Cached only"] != want {
		t.Errorf("Cached only = %q, want %q", vals["Cached only"], want)
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

func TestAFailedSharedWalkFailsEveryMemberWithOneRequest(t *testing.T) {
	srv, walks := catalogRequests(t, http.StatusServiceUnavailable)
	withImageSearcher(t, wiredLikeProduction(srv))

	m := runLot(t, nexusGroupModel(t, srv.URL), "nginx", 3)

	if n := walks.Load(); n != 1 {
		t.Errorf("/v2/_catalog got %d requests, want 1", n)
	}
	for _, p := range shareMembers {
		wantVerdict(t, m, memberKey("nx", srv.URL, p), imagesearch.VerdictFailed)
	}
	_, vals := headerLines(m)
	for _, p := range shareMembers {
		if !strings.Contains(vals["Failed"], "nx/"+p) {
			t.Errorf("Failed = %q, want it to list nx/%s", vals["Failed"], p)
		}
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

	m := runLot(t, nexusGroupModel(t, srv.URL), "nginx", 3)
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

	m, cmd := submitRaw(t, nexusGroupModel(t, srv.URL), "nginx")
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
