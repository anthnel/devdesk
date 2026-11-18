package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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

// ── DEVDESK-510: a refused listing is told apart from a settled one ─────────

// refusedGroupEnv configures one Nexus group per slug and returns the env.
func refusedGroupEnv(slugs ...string) *Env {
	env := testEnv(nil)
	for _, slug := range slugs {
		env.Config.Registry.Registries = append(env.Config.Registry.Registries,
			config.RegistryItem{Slug: slug, URL: slug + ".example.com", Kind: "group", Provider: "nexus"})
	}
	return env
}

func storeGroupRefusal(t *testing.T, slug string, r cache.RegistryGroupRefusal) {
	t.Helper()
	c, err := cache.NewRegistryGroupCache()
	if err != nil {
		t.Fatalf("open group cache: %v", err)
	}
	if err := c.SetRefusal(slug, r); err != nil {
		t.Fatalf("store group refusal: %v", err)
	}
}

func listedBySlug(t *testing.T, env *Env) map[string]registryEntry {
	t.Helper()
	var out registriesListOut
	callTool(t, connect(t, env), "registries_list", nil, &out)
	bySlug := map[string]registryEntry{}
	for _, entry := range out.Registries {
		bySlug[entry.Slug] = entry
	}
	return bySlug
}

// rawEntries decodes the tool's raw JSON into one generic object per slug, so
// the wire shape is what is checked and not the Go struct that decodes it.
func rawEntries(t *testing.T, env *Env) map[string]map[string]any {
	t.Helper()
	var out struct {
		Structured struct {
			Registries []map[string]any `json:"registries"`
		} `json:"structuredContent"`
	}
	raw := callToolRaw(t, connect(t, env), "registries_list", nil)
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	bySlug := map[string]map[string]any{}
	for _, entry := range out.Structured.Registries {
		slug, _ := entry["slug"].(string)
		bySlug[slug] = entry
	}
	if len(bySlug) == 0 {
		t.Fatalf("no registries decoded from the structured reply: %s", raw)
	}
	return bySlug
}

// A group whose manager refused the listing has no settled listing: discovery
// keeps its meaning and stays absent, and members_refused says why.
func TestARefusedGroupReportsMembersRefusedAndNoDiscovery(t *testing.T) {
	fakeCacheHome(t)
	storeGroupRefusal(t, "nexus", cache.RegistryGroupRefusal{
		At: time.Now().Add(-time.Hour), Status: 403, Anonymous: true,
	})

	got := listedBySlug(t, refusedGroupEnv("nexus"))["nexus"]

	if got.Discovery != nil {
		t.Errorf("discovery = %+v, want none: a refusal is not a listing", got.Discovery)
	}
	if got.MembersRefused == nil {
		t.Fatal("members_refused is absent though the manager refused the listing")
	}
	if got.MembersRefused.Status != 403 {
		t.Errorf("members_refused.status = %d, want 403", got.MembersRefused.Status)
	}
	if !got.MembersRefused.Anonymous {
		t.Error("members_refused.anonymous = false, want the probe's own value (true)")
	}
	if got.MembersRefused.AgeSeconds < 3500 {
		t.Errorf("members_refused.age_seconds = %d, want roughly one hour", got.MembersRefused.AgeSeconds)
	}
	if got.MembersRefused.At.IsZero() {
		t.Error("members_refused.at is zero, want the time of the refusal")
	}
}

// The members last known stay reported next to the refusal; discovery is just
// older than it, and says so through its own age.
func TestARefusedGroupWithAnOlderListingReportsBoth(t *testing.T) {
	fakeCacheHome(t)
	storeGroupDiscovery(t, "nexus", cache.RegistryGroupEntry{
		DiscoveredAt: time.Now().Add(-48 * time.Hour),
		Members:      []cache.RegistryGroupMember{{Alias: "hosted", URL: "nexus.example.com", RepoPrefix: "docker-hosted"}},
	})
	storeGroupRefusal(t, "nexus", cache.RegistryGroupRefusal{At: time.Now().Add(-time.Minute), Status: 401})

	got := listedBySlug(t, refusedGroupEnv("nexus"))["nexus"]

	if got.Discovery == nil || len(got.Discovery.Members) != 1 {
		t.Fatalf("discovery = %+v, want the older listing kept beside the refusal", got.Discovery)
	}
	if got.Discovery.Members[0].Address != "nexus.example.com/docker-hosted" {
		t.Errorf("member address = %q", got.Discovery.Members[0].Address)
	}
	if got.MembersRefused == nil || got.MembersRefused.Status != 401 {
		t.Fatalf("members_refused = %+v, want status 401 beside the listing", got.MembersRefused)
	}
	if got.MembersRefused.AgeSeconds >= got.Discovery.AgeSeconds {
		t.Errorf("refusal age %d >= listing age %d, want the refusal to read as the newer of the two",
			got.MembersRefused.AgeSeconds, got.Discovery.AgeSeconds)
	}
}

// omitempty: a group nobody probed, one with a settled listing and one with a
// settled "0 members" all carry no members_refused key on the wire.
func TestAGroupThatWasNotRefusedHasNoMembersRefusedField(t *testing.T) {
	fakeCacheHome(t)
	storeGroupDiscovery(t, "listed", cache.RegistryGroupEntry{
		DiscoveredAt: time.Now().Add(-time.Hour),
		Members:      []cache.RegistryGroupMember{{Alias: "hosted", URL: "listed.example.com", RepoPrefix: "docker-hosted"}},
	})
	storeGroupDiscovery(t, "empty", cache.RegistryGroupEntry{DiscoveredAt: time.Now().Add(-time.Hour)})
	storeGroupRefusal(t, "refused", cache.RegistryGroupRefusal{At: time.Now(), Status: 403})

	raw := rawEntries(t, refusedGroupEnv("listed", "empty", "unasked", "refused"))

	for _, slug := range []string{"listed", "empty", "unasked"} {
		if _, present := raw[slug]["members_refused"]; present {
			t.Errorf("%s carries members_refused though it was not refused: %v", slug, raw[slug])
		}
	}
	if _, present := raw["refused"]["members_refused"]; !present {
		t.Errorf("refused does not carry members_refused on the wire: %v", raw["refused"])
	}
}

// A refusal for one slug must not leak onto the others, and a refusal recorded
// for a slug the config no longer has is not invented into an entry.
func TestARefusalBelongsToItsOwnSlugOnly(t *testing.T) {
	fakeCacheHome(t)
	storeGroupRefusal(t, "refused", cache.RegistryGroupRefusal{At: time.Now(), Status: 403})
	storeGroupRefusal(t, "gone", cache.RegistryGroupRefusal{At: time.Now(), Status: 403})

	var out registriesListOut
	callTool(t, connect(t, refusedGroupEnv("refused", "other")), "registries_list", nil, &out)

	if len(out.Registries) != 2 {
		t.Fatalf("listed %+v, want only the two configured entries", out.Registries)
	}
	for _, entry := range out.Registries {
		switch entry.Slug {
		case "refused":
			if entry.MembersRefused == nil {
				t.Error("refused lost its refusal")
			}
		case "other":
			if entry.MembersRefused != nil {
				t.Errorf("other carries the refusal of another slug: %+v", entry.MembersRefused)
			}
		}
	}
}

// The cache keeps the status and the time, but the reader may meet a file with
// more in it (a newer build, a hand edit). Nothing beyond the declared fields,
// no error text and no URL, reaches an agent.
func TestMembersRefusedExposesNoErrorTextNorURL(t *testing.T) {
	home := fakeCacheHome(t)
	dir := filepath.Join(home, ".devdesk", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := `{"version": 2, "groups": {}, "refusals": {"nexus": {
		"at": "2026-01-02T03:04:05Z", "status": 403, "anonymous": true,
		"error": "403 Forbidden: https://leaky.internal/service/rest/v1/repositories?token=s3cr3t",
		"url": "https://leaky.internal/service/rest/v1/repositories"}}}`
	if err := os.WriteFile(filepath.Join(dir, "registry-groups.json"), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	env := refusedGroupEnv("nexus")

	raw := rawEntries(t, env)["nexus"]
	refused, ok := raw["members_refused"].(map[string]any)
	if !ok {
		t.Fatalf("members_refused is not an object on the wire: %v", raw)
	}
	allowed := map[string]bool{"at": true, "age_seconds": true, "status": true, "anonymous": true}
	for key := range refused {
		if !allowed[key] {
			t.Errorf("members_refused exposes %q, want only at, age_seconds, status and anonymous", key)
		}
	}
	whole := callToolRaw(t, connect(t, env), "registries_list", nil)
	for _, leak := range []string{"leaky.internal", "s3cr3t", "Forbidden"} {
		if strings.Contains(whole, leak) {
			t.Errorf("the reply carries %q from the refusal record:\n%s", leak, whole)
		}
	}
}

// A refusal says nothing about the members, so it must not turn into a settled
// "0 members": no discovery on the wire, and no empty member list.
func TestARefusalWithNoKnownListingFabricatesNoZeroMembers(t *testing.T) {
	fakeCacheHome(t)
	storeGroupRefusal(t, "nexus", cache.RegistryGroupRefusal{At: time.Now(), Status: 403})

	raw := rawEntries(t, refusedGroupEnv("nexus"))["nexus"]

	if d, present := raw["discovery"]; present {
		t.Errorf("discovery = %v on the wire, want it absent: a refusal is not a listing of zero members", d)
	}
	encoded, _ := json.Marshal(raw)
	if strings.Contains(string(encoded), `"members":[]`) {
		t.Errorf("an empty member list was fabricated: %s", encoded)
	}
}

// A legacy file's member-less entry may be a swallowed refusal: it reads as
// "never probed" through MCP too, with no refusal invented for it either.
func TestALegacyEmptyEntryReadsAsNeverProbedThroughMCP(t *testing.T) {
	home := fakeCacheHome(t)
	dir := filepath.Join(home, ".devdesk", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"old": {"members": [], "discovered_at": "2026-01-02T03:04:05Z"},
	             "kept": {"members": [{"alias": "h", "url": "kept.example.com", "repo_prefix": "docker-hosted"}], "discovered_at": "2026-01-02T03:04:05Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "registry-groups.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	got := listedBySlug(t, refusedGroupEnv("old", "kept"))

	if got["old"].Discovery != nil {
		t.Errorf("a legacy empty entry reports discovery %+v, want none (it may be a swallowed refusal)", got["old"].Discovery)
	}
	if got["old"].MembersRefused != nil {
		t.Errorf("a legacy empty entry reports a refusal %+v, want none", got["old"].MembersRefused)
	}
	if got["kept"].Discovery == nil || len(got["kept"].Discovery.Members) != 1 {
		t.Errorf("a legacy listing with a member is lost: %+v", got["kept"].Discovery)
	}
}

// Read-only like the rest of the tool: reporting a refusal must not write the
// file, rewrite it into another version, or leave a temporary file behind.
func TestReportingARefusalDoesNotTouchTheGroupCache(t *testing.T) {
	home := fakeCacheHome(t)
	storeGroupDiscovery(t, "nexus", cache.RegistryGroupEntry{DiscoveredAt: time.Now().Add(-time.Hour)})
	storeGroupRefusal(t, "nexus", cache.RegistryGroupRefusal{At: time.Now(), Status: 403})
	dir := filepath.Join(home, ".devdesk", "cache")
	path := filepath.Join(dir, "registry-groups.json")
	bytesBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the fixture: %v", err)
	}
	namesBefore := dirNames(t, dir)

	got := listedBySlug(t, refusedGroupEnv("nexus"))["nexus"]
	if got.MembersRefused == nil {
		t.Fatal("the refusal is not reported, so this test would pass for the wrong reason")
	}

	bytesAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after listing: %v", err)
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after listing: %v", err)
	}
	if string(bytesBefore) != string(bytesAfter) || !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Error("listing a refused group rewrote the group cache file")
	}
	if namesAfter := dirNames(t, dir); strings.Join(namesBefore, ",") != strings.Join(namesAfter, ",") {
		t.Errorf("listing changed the cache directory: %v -> %v", namesBefore, namesAfter)
	}
}

// The tool description is what an agent reads first; it has to name the field,
// or the field is a surprise at the end of a reply.
func TestRegistriesListDescriptionNamesMembersRefused(t *testing.T) {
	if !strings.Contains(toolDescription("registries_list"), "members_refused") {
		t.Errorf("the registries_list description does not mention members_refused:\n%s", toolDescription("registries_list"))
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
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
