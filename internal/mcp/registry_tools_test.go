package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
)

func TestRegistriesListReportsTheConfiguredEntriesBySlug(t *testing.T) {
	fakeCacheHome(t)
	env := testEnv(nil)
	env.Config.Registry.Registries = []config.RegistryItem{
		{Slug: "zulu", URL: "zulu.example.com", Kind: "registry", AuthMode: "anonymous"},
		{Slug: "alpha", URL: "alpha.example.com", Kind: "registry", AuthMode: "credentials"},
	}

	var out registriesListOut
	callTool(t, connect(t, env), "registries_list", nil, &out)

	if len(out.Registries) != 2 {
		t.Fatalf("listed %+v, want both entries", out.Registries)
	}
	if out.Registries[0].Slug != "alpha" || out.Registries[1].Slug != "zulu" {
		t.Errorf("listed %s then %s, want them ordered by slug — the config order is one the user typed, not one to depend on",
			out.Registries[0].Slug, out.Registries[1].Slug)
	}
	if out.Registries[0].AuthMode != "credentials" {
		t.Errorf("auth_mode = %q, want the declared one", out.Registries[0].AuthMode)
	}
}

// §3.18: an entry is an address, not a URL. The pair is joined into the head of
// the pull reference exactly, so browse and pull cannot mean two repositories.
func TestAnEntryReportsItsAddressAndNotJustItsURL(t *testing.T) {
	fakeCacheHome(t)
	env := testEnv(nil)
	env.Config.Registry.Registries = []config.RegistryItem{
		{Slug: "nx", URL: "nexus.example.com", RepoPrefix: "docker-hosted", Kind: "registry"},
		{Slug: "plain", URL: "plain.example.com", Kind: "registry"},
	}

	var out registriesListOut
	callTool(t, connect(t, env), "registries_list", nil, &out)

	byslug := map[string]registryEntry{}
	for _, entry := range out.Registries {
		byslug[entry.Slug] = entry
	}
	if got := byslug["nx"].Address; got != "nexus.example.com/docker-hosted" {
		t.Errorf("address = %q, want the url and the prefix joined", got)
	}
	if got := byslug["plain"].Address; got != "plain.example.com" {
		t.Errorf("address = %q, want the url alone when there is no prefix", got)
	}
}

func TestAGroupCarriesTheMembersDiscoveryFound(t *testing.T) {
	fakeCacheHome(t)
	discoveredAt := time.Now().Add(-2 * time.Hour)
	storeGroupDiscovery(t, "nexus", cache.RegistryGroupEntry{
		DiscoveredAt: discoveredAt,
		Members: []cache.RegistryGroupMember{
			{Alias: "hosted", URL: "nexus.example.com", RepoPrefix: "docker-hosted"},
			{Alias: "proxy", URL: "nexus.example.com", RepoPrefix: "docker-proxy"},
		},
	})

	env := testEnv(nil)
	env.Config.Registry.Registries = []config.RegistryItem{
		{Slug: "nexus", URL: "nexus.example.com", Kind: "group", Provider: "nexus"},
	}

	var out registriesListOut
	callTool(t, connect(t, env), "registries_list", nil, &out)

	if len(out.Registries) != 1 {
		t.Fatalf("listed %+v", out.Registries)
	}
	got := out.Registries[0]
	if got.Discovery == nil {
		t.Fatal("the group carries no discovery, though one is cached")
	}
	if len(got.Discovery.Members) != 2 {
		t.Fatalf("discovery holds %+v, want both members", got.Discovery.Members)
	}
	if got.Discovery.Members[0].Address != "nexus.example.com/docker-hosted" {
		t.Errorf("member address = %q, want the url and the prefix joined", got.Discovery.Members[0].Address)
	}
	if got.Discovery.AgeSeconds < 3000 {
		t.Errorf("age_seconds = %d, want roughly two hours — a cache with no visible age looks current whatever it holds", got.Discovery.AgeSeconds)
	}
}

// The distinction the group cache exists to keep (§3.8, decision 3): nobody
// asked, against somebody asked and it is not a group. Both would come back as
// an empty member list if the discovery were not a pointer.
func TestAGroupNobodyProbedIsToldApartFromOneWithNoMembers(t *testing.T) {
	fakeCacheHome(t)
	storeGroupDiscovery(t, "asked", cache.RegistryGroupEntry{
		DiscoveredAt: time.Now().Add(-time.Hour),
		Members:      nil,
	})

	env := testEnv(nil)
	env.Config.Registry.Registries = []config.RegistryItem{
		{Slug: "asked", URL: "asked.example.com", Kind: "group", Provider: "nexus"},
		{Slug: "unasked", URL: "unasked.example.com", Kind: "group", Provider: "nexus"},
	}

	var out registriesListOut
	callTool(t, connect(t, env), "registries_list", nil, &out)

	byslug := map[string]registryEntry{}
	for _, entry := range out.Registries {
		byslug[entry.Slug] = entry
	}
	if byslug["unasked"].Discovery != nil {
		t.Error("a group nobody probed reports a discovery — this server must not probe, and must not pretend one happened")
	}
	asked := byslug["asked"].Discovery
	if asked == nil {
		t.Fatal("a group that was probed and found not to be one reports no discovery — that is an answer, not an absence")
	}
	if len(asked.Members) != 0 {
		t.Errorf("members = %+v, want the empty list that was cached", asked.Members)
	}
}

// It reads the cache and never the network, so listing must leave the cache file
// exactly as it found it — and must not create one when there is none.
func TestListingRegistriesDoesNotTouchTheGroupCache(t *testing.T) {
	home := fakeCacheHome(t)
	storeGroupDiscovery(t, "nexus", cache.RegistryGroupEntry{DiscoveredAt: time.Now()})
	path := filepath.Join(home, ".devdesk", "cache", "registry-groups.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture: %v", err)
	}

	env := testEnv(nil)
	env.Config.Registry.Registries = []config.RegistryItem{
		{Slug: "nexus", URL: "nexus.example.com", Kind: "group", Provider: "nexus"},
		{Slug: "other", URL: "other.example.com", Kind: "group", Provider: "nexus"},
	}
	var out registriesListOut
	callTool(t, connect(t, env), "registries_list", nil, &out)

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after listing: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Errorf("listing wrote to the group cache: mtime %v → %v, size %d → %d",
			before.ModTime(), after.ModTime(), before.Size(), after.Size())
	}
}

func TestAContextWithNoRegistriesListsNone(t *testing.T) {
	fakeCacheHome(t)
	env := testEnv(nil)
	env.Config.Registry.Registries = nil

	var out registriesListOut
	callTool(t, connect(t, env), "registries_list", nil, &out)

	if len(out.Registries) != 0 {
		t.Errorf("listed %+v, want none", out.Registries)
	}
}

// context_get used to carry a thinner copy of the registries. Two tools
// answering one question is the shape D12, D24 and D25 each turned out to be,
// and an agent with two sources also has to guess which is authoritative.
func TestOnlyOneToolAnswersForRegistries(t *testing.T) {
	fakeCacheHome(t)
	env := testEnv(nil)
	env.Config.Registry.Registries = []config.RegistryItem{
		{Slug: "nx", URL: "nexus.example.com", Kind: "registry"},
	}

	raw := callToolRaw(t, connect(t, env), "context_get", nil)

	if strings.Contains(raw, "nexus.example.com") {
		t.Errorf("context_get answers for registries as well as registries_list:\n%s", raw)
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

func storeGroupDiscovery(t *testing.T, slug string, entry cache.RegistryGroupEntry) {
	t.Helper()
	c, err := cache.NewRegistryGroupCache()
	if err != nil {
		t.Fatalf("open group cache: %v", err)
	}
	if err := c.Set(slug, entry); err != nil {
		t.Fatalf("store group discovery: %v", err)
	}
}
