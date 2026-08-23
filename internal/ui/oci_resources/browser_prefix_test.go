package ociresources

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// D39, §3.18. A registry member is an address — the pair (url, repo_prefix) —
// and not a URL. Both directions derive from that one pair, which is the whole
// point: the browse and the pull cannot disagree about which repository they
// mean, the disagreement being exactly what D39 is.
//
// What it replaced put the member's name into the URL's *path*
// (`host/repository/<name>`). The Docker client builds its reference the other
// way round — `/v2/` first, the whole repository path after it — so the tags
// table filled correctly and every `G` on it 404'd.

// prefixedBrowser opens the browser on one registry declared as a bare host plus
// a prefix, which is what one line per proxy looks like (§3.18, "what it
// unlocks"), and submits a search for repo.
func prefixedBrowser(t *testing.T, host, prefix, repo string) (Model, tea.Cmd) {
	t.Helper()

	cfg := config.Default()
	cfg.Registry.Registries = []config.RegistryItem{{
		URL: host, Slug: "dhi", Alias: "dhi",
		AuthMode: config.AuthAnonymous, RepoPrefix: prefix,
	}}

	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30}, ImagesListMsg{Images: imageFixtures()})
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil {
		t.Fatal("the browser did not open")
	}
	m = typeInto(t, m, repo)
	return step(t, m, testutil.Key("enter"))
}

// The pair D39 fails: the browse asks `<host>/v2/<prefix>/<repo>/tags/list` and
// the pull reference is `<host>/<prefix>/<repo>:<tag>`. They are the same
// repository, which is what the one field buys.
func TestAPrefixedRegistryBrowsesAndPullsTheSameRepository(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		_, _ = w.Write([]byte(`{"tags":["21"]}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	m, cmd := prefixedBrowser(t, srv.URL, "dhi-io-proxy", "eclipse-temurin")
	loaded, ok := testutil.MsgOf[MultiRegistryTagsLoadedMsg](cmd)
	if !ok {
		t.Fatal("the search produced no result message")
	}
	if loaded.Err != nil {
		t.Fatalf("the search failed: %v", loaded.Err)
	}

	if want := "/v2/dhi-io-proxy/eclipse-temurin/tags/list"; asked != want {
		t.Errorf("browsed %q, want %q", asked, want)
	}

	m = feed(t, m, loaded)
	if len(m.registryBrowser.tags) != 1 {
		t.Fatalf("tags = %d, want the one the registry answered with", len(m.registryBrowser.tags))
	}
	got := m.registryBrowser.selectedImageName()
	if want := host + "/dhi-io-proxy/eclipse-temurin:21"; got != want {
		t.Errorf("pull reference = %q, want %q", got, want)
	}
}

// The prefix is a path segment, so it joins with a single slash whatever the
// repository looks like — including a repository that already carries one.
func TestThePrefixJoinsTheRepositoryWithOneSlash(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		_, _ = w.Write([]byte(`{"tags":["1.25"]}`))
	}))
	defer srv.Close()

	if _, cmd := prefixedBrowser(t, srv.URL, "docker-io-proxy", "library/nginx"); cmd != nil {
		testutil.Msgs(cmd)
	}
	if want := "/v2/docker-io-proxy/library/nginx/tags/list"; asked != want {
		t.Errorf("browsed %q, want %q", asked, want)
	}
}

// The ordinary registry is the one that declares no prefix, and it has to go on
// producing byte-identical requests: this field is an addition, not a change of
// address for everything that already worked.
func TestARegistryWithNoPrefixIsUnchanged(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		_, _ = w.Write([]byte(`{"tags":["v1"]}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	m, cmd := prefixedBrowser(t, srv.URL, "", "api")
	loaded, ok := testutil.MsgOf[MultiRegistryTagsLoadedMsg](cmd)
	if !ok {
		t.Fatal("the search produced no result message")
	}
	if want := "/v2/api/tags/list"; asked != want {
		t.Errorf("browsed %q, want %q", asked, want)
	}

	m = feed(t, m, loaded)
	if got, want := m.registryBrowser.selectedImageName(), host+"/api:v1"; got != want {
		t.Errorf("pull reference = %q, want %q", got, want)
	}
}

// Two proxies on one host are the ordinary case now, and the URL is no longer
// what tells them apart. Anything keyed on it — the checkbox, the remembered
// exclusions, the result filter — has to follow the entry instead (D40).
func TestTwoPrefixesOnOneHostAreTwoEntries(t *testing.T) {
	cfg := config.Default()
	cfg.Registry.Registries = []config.RegistryItem{
		{URL: "nexus.example.com", Slug: "dhi", Alias: "dhi", AuthMode: config.AuthAnonymous, RepoPrefix: "dhi-io-proxy"},
		{URL: "nexus.example.com", Slug: "quay", Alias: "quay", AuthMode: config.AuthAnonymous, RepoPrefix: "quay-io-proxy"},
	}
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30}, ImagesListMsg{Images: imageFixtures()})
	m = feed(t, m, testutil.Key(keymap.Browser))
	b := m.registryBrowser

	if len(b.entries) != 2 {
		t.Fatalf("entries = %d, want one per declared proxy", len(b.entries))
	}
	if b.entries[0].key == b.entries[1].key {
		t.Fatalf("both proxies answer to the key %q, so one checkbox covers both", b.entries[0].key)
	}

	// Unchecking one must leave the other alone — the defect the shared URL had.
	m = feed(t, m, testutil.Key("down"), testutil.Key(" "))
	if b.selected(b.entries[0]) {
		t.Error("the first proxy stayed checked")
	}
	if !b.selected(b.entries[1]) {
		t.Error("unchecking one proxy unchecked the other")
	}
}
