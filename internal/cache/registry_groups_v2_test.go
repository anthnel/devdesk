package cache

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// DEVDESK-510 (NX-07): the group cache file moves to a versioned envelope and
// records a refusal beside the listings, never inside one. An entry means "a
// settled answer" for every reader; a refusal is not an answer about the members.

const legacyGroupFile = `{
  "a": {"members": [{"alias": "hosted", "url": "https://nexus.example.com/repository/hosted"}], "discovered_at": "2026-01-02T03:04:05Z"},
  "b": {"members": [], "discovered_at": "2026-01-02T03:04:05Z"},
  "c": {"members": null, "discovered_at": "2026-01-02T03:04:05Z"}
}`

func refusalAt(at time.Time, status int, anonymous bool) RegistryGroupRefusal {
	return RegistryGroupRefusal{At: at, Status: status, Anonymous: anonymous}
}

// reopenGroupCache opens a second cache on the same file, the way a new session does.
func reopenGroupCache(c *RegistryGroupCache) *RegistryGroupCache {
	reopened := &RegistryGroupCache{path: c.path, entries: make(map[string]RegistryGroupEntry)}
	reopened.load()
	return reopened
}

func readRaw(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// parseEnvelope reads the file as generic JSON, so the format is pinned by what
// is on disk and not by the struct that writes it.
func parseEnvelope(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(readRaw(t, path), &raw); err != nil {
		t.Fatalf("the cache file is not a JSON object: %v", err)
	}
	return raw
}

// --- the decoder shared by the cache and the read-only reader ---

func TestDecodeRegistryGroupFile_LegacyKeepsListedAndDropsEmpty(t *testing.T) {
	groups, refusals, dropped, err := decodeRegistryGroupFile([]byte(legacyGroupFile))
	if err != nil {
		t.Fatalf("decodeRegistryGroupFile: %v", err)
	}

	a, ok := groups["a"]
	if !ok || len(a.Members) != 1 || a.Members[0].Alias != "hosted" {
		t.Errorf("groups[a] = %+v, want the entry with a member kept", a)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !a.DiscoveredAt.Equal(want) {
		t.Errorf("groups[a].DiscoveredAt = %v, want %v", a.DiscoveredAt, want)
	}
	// An empty legacy entry may be a swallowed refusal: it goes back to "never".
	for _, slug := range []string{"b", "c"} {
		if e, present := groups[slug]; present {
			t.Errorf("groups[%s] = %+v, want the member-less legacy entry dropped (CA6)", slug, e)
		}
	}
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
	if len(refusals) != 0 {
		t.Errorf("refusals = %+v, a legacy file has none", refusals)
	}
}

// The discriminator is the version, not the presence of a "groups" key: a legacy
// map whose slugs are literally "groups" and "version" is still a legacy map.
func TestDecodeRegistryGroupFile_LegacySlugsNamedLikeEnvelopeKeys(t *testing.T) {
	data := []byte(`{
  "groups":  {"members": [{"alias": "x", "url": "https://nexus.example.com/repository/x"}], "discovered_at": "2026-01-02T03:04:05Z"},
  "version": {"members": [], "discovered_at": "2026-01-02T03:04:05Z"}
}`)

	groups, _, dropped, err := decodeRegistryGroupFile(data)
	if err != nil {
		t.Fatalf("decodeRegistryGroupFile: %v", err)
	}

	if e, ok := groups["groups"]; !ok || len(e.Members) != 1 {
		t.Errorf("groups[%q] = %+v, want the legacy entry with a member kept", "groups", e)
	}
	if _, ok := groups["version"]; ok {
		t.Error(`the member-less legacy entry named "version" was kept`)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
}

func TestDecodeRegistryGroupFile_V2(t *testing.T) {
	data := []byte(`{
  "version": 2,
  "groups": {
    "full":  {"members": [{"alias": "m", "url": "https://nexus.example.com/repository/m"}], "discovered_at": "2026-01-02T03:04:05Z"},
    "empty": {"members": [], "discovered_at": "2026-01-02T03:04:05Z"}
  },
  "refusals": {
    "full":    {"at": "2026-02-03T04:05:06Z", "status": 403},
    "refused": {"at": "2026-02-03T04:05:06Z", "status": 401, "anonymous": true}
  }
}`)

	groups, refusals, dropped, err := decodeRegistryGroupFile(data)
	if err != nil {
		t.Fatalf("decodeRegistryGroupFile: %v", err)
	}

	// In v2 an entry with no member is a settled "0 members" (CA2), not junk.
	if e, ok := groups["empty"]; !ok || len(e.Members) != 0 {
		t.Errorf("groups[empty] = %+v, present %v, want the v2 empty entry kept", e, ok)
	}
	if e, ok := groups["full"]; !ok || len(e.Members) != 1 {
		t.Errorf("groups[full] = %+v, want kept", e)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0: nothing is set aside in a v2 file", dropped)
	}
	if r, ok := refusals["full"]; !ok || r.Status != 403 || r.Anonymous {
		t.Errorf("refusals[full] = %+v, want status 403 account refusal", r)
	}
	if r, ok := refusals["refused"]; !ok || r.Status != 401 || !r.Anonymous {
		t.Errorf("refusals[refused] = %+v, want status 401 anonymous", r)
	}
	if _, ok := groups["refused"]; ok {
		t.Error("a refusal produced a listing entry")
	}
}

// A file from a newer build is read for what decodes; fields this build does not
// know are ignored.
func TestDecodeRegistryGroupFile_FutureVersionIsReadForWhatDecodes(t *testing.T) {
	data := []byte(`{
  "version": 3,
  "groups": {"g": {"members": [{"alias": "m", "url": "https://nexus.example.com/repository/m"}], "discovered_at": "2026-01-02T03:04:05Z", "etag": "x"}},
  "refusals": {"h": {"at": "2026-02-03T04:05:06Z", "status": 403, "reason": "new"}},
  "something_new": {"k": 1}
}`)

	groups, refusals, _, err := decodeRegistryGroupFile(data)
	if err != nil {
		t.Fatalf("decodeRegistryGroupFile: %v", err)
	}
	if e, ok := groups["g"]; !ok || len(e.Members) != 1 {
		t.Errorf("groups[g] = %+v, want the version 3 listing read", e)
	}
	if r, ok := refusals["h"]; !ok || r.Status != 403 {
		t.Errorf("refusals[h] = %+v, want the version 3 refusal read", r)
	}
}

// Whatever an older or unknown shape means, a member-less entry must not come
// back as a settled "0 members": that is the false answer this story removes.
func TestDecodeRegistryGroupFile_AnOlderVersionNeverYieldsAFalseEmptyGroup(t *testing.T) {
	data := []byte(`{"version": 1, "groups": {"a": {"members": [], "discovered_at": "2026-01-02T03:04:05Z"}}}`)

	groups, _, _, _ := decodeRegistryGroupFile(data) // an error is acceptable, a false answer is not

	if e, ok := groups["a"]; ok {
		t.Errorf("groups[a] = %+v, want no settled empty group out of a version below 2", e)
	}
}

func TestDecodeRegistryGroupFile_InvalidJSON(t *testing.T) {
	for name, data := range map[string]string{
		"not json":  "{not json",
		"truncated": `{"version": 2, "groups": {"a": `,
		"empty":     "",
	} {
		t.Run(name, func(t *testing.T) {
			groups, refusals, _, err := decodeRegistryGroupFile([]byte(data))
			if err == nil {
				t.Error("err = nil, want the decode error returned to the caller")
			}
			if len(groups) != 0 || len(refusals) != 0 {
				t.Errorf("groups = %+v, refusals = %+v, want nothing out of a broken file", groups, refusals)
			}
		})
	}
}

// --- the cache ---

func TestGroupCache_WritesTheVersion2Envelope(t *testing.T) {
	c := newTestGroupCache(t)
	at := time.Now().Truncate(time.Second)
	if err := c.Set("prod", groupEntry(at, "hosted")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw := parseEnvelope(t, c.path)

	var version int
	if err := json.Unmarshal(raw["version"], &version); err != nil || version != 2 {
		t.Errorf(`"version" = %s (err %v), want 2`, raw["version"], err)
	}
	var groups map[string]RegistryGroupEntry
	if err := json.Unmarshal(raw["groups"], &groups); err != nil {
		t.Fatalf(`"groups" is not a map of entries: %v`, err)
	}
	if e, ok := groups["prod"]; !ok || len(e.Members) != 1 || !e.DiscoveredAt.Equal(at) {
		t.Errorf(`"groups" = %+v, want prod with its member and time`, groups)
	}
	if _, bare := raw["prod"]; bare {
		t.Error("the slug sits at the top level: the legacy bare map was written")
	}
}

func TestGroupCache_RoundTripsListingsAndRefusals(t *testing.T) {
	c := newTestGroupCache(t)
	at := time.Now().Truncate(time.Second)
	refusedAt := at.Add(time.Minute)

	if err := c.Set("listed", groupEntry(at, "one", "two")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Set("empty", RegistryGroupEntry{DiscoveredAt: at}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.SetRefusal("refused", refusalAt(refusedAt, 403, false)); err != nil {
		t.Fatalf("SetRefusal: %v", err)
	}
	if err := c.SetRefusal("anon", refusalAt(refusedAt, 401, true)); err != nil {
		t.Fatalf("SetRefusal: %v", err)
	}

	reopened := reopenGroupCache(c)

	if e := reopened.Get("listed"); e == nil || len(e.Members) != 2 || !e.DiscoveredAt.Equal(at) {
		t.Errorf("Get(listed) = %+v, want both members and the time", e)
	}
	// A v2 empty entry is a settled answer and survives the round trip (CA2).
	if e := reopened.Get("empty"); e == nil || len(e.Members) != 0 || e.DiscoveredAt.IsZero() {
		t.Errorf("Get(empty) = %+v, want the settled empty entry", e)
	}
	if e := reopened.Get("refused"); e != nil {
		t.Errorf("Get(refused) = %+v, a refusal must not become a listing", e)
	}
	r := reopened.Refusal("refused")
	if r == nil || r.Status != 403 || r.Anonymous || !r.At.Equal(refusedAt) {
		t.Errorf("Refusal(refused) = %+v, want 403 account refusal at %v", r, refusedAt)
	}
	a := reopened.Refusal("anon")
	if a == nil || a.Status != 401 || !a.Anonymous {
		t.Errorf("Refusal(anon) = %+v, want 401 anonymous", a)
	}
}

// Legacy empty entries are dropped in memory at load; the file is only rewritten
// (in v2) by the next write, and a read never writes (CA6, no manual step).
func TestGroupCache_LegacyFileIsDroppedOnLoadAndRewrittenOnNextSet(t *testing.T) {
	c := newTestGroupCache(t)
	if err := os.WriteFile(c.path, []byte(legacyGroupFile), 0o600); err != nil {
		t.Fatalf("writing the legacy file: %v", err)
	}
	before := stamp(t, c.path)

	c.load()

	if e := c.Get("a"); e == nil || len(e.Members) != 1 {
		t.Errorf("Get(a) = %+v, want the legacy entry with a member kept", e)
	}
	if !c.Get("a").DiscoveredAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("Get(a).DiscoveredAt = %v, want the legacy time kept", c.Get("a").DiscoveredAt)
	}
	for _, slug := range []string{"b", "c"} {
		if e := c.Get(slug); e != nil {
			t.Errorf("Get(%s) = %+v, want unknown (\"never\") after the legacy drop", slug, e)
		}
	}
	assertUntouched(t, c.path, before)
	if !bytes.Equal(readRaw(t, c.path), []byte(legacyGroupFile)) {
		t.Error("load rewrote the legacy file")
	}

	if err := c.Set("d", groupEntry(time.Now(), "x")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw := parseEnvelope(t, c.path)
	if string(raw["version"]) != "2" {
		t.Errorf(`after the next write "version" = %s, want 2`, raw["version"])
	}
	reopened := reopenGroupCache(c)
	if reopened.Get("a") == nil || reopened.Get("d") == nil {
		t.Error("the rewrite lost a legacy listing or the new one")
	}
	if reopened.Get("b") != nil || reopened.Get("c") != nil {
		t.Error("a dropped legacy entry came back after the rewrite")
	}
}

// D23: a refusal is not a listing, so it leaves the members last known alone.
func TestGroupCache_ARefusalNeverTouchesTheKnownMembers(t *testing.T) {
	c := newTestGroupCache(t)
	at := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := c.Set("g", groupEntry(at, "one", "two", "three")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	r := refusalAt(time.Now().Truncate(time.Second), 403, false)
	if err := c.SetRefusal("g", r); err != nil {
		t.Fatalf("SetRefusal: %v", err)
	}

	for name, cc := range map[string]*RegistryGroupCache{"in memory": c, "after a reopen": reopenGroupCache(c)} {
		e := cc.Get("g")
		if e == nil || len(e.Members) != 3 {
			t.Fatalf("%s: Get(g) = %+v, want the 3 known members", name, e)
		}
		if !e.DiscoveredAt.Equal(at) {
			t.Errorf("%s: DiscoveredAt = %v, want %v: the refusal moved the listing's time", name, e.DiscoveredAt, at)
		}
		got := cc.Refusal("g")
		if got == nil || got.Status != 403 || !got.At.Equal(r.At) {
			t.Errorf("%s: Refusal(g) = %+v, want %+v", name, got, r)
		}
	}
}

func TestGroupCache_ARefusalAloneCreatesNoListing(t *testing.T) {
	c := newTestGroupCache(t)

	if err := c.SetRefusal("g", refusalAt(time.Now(), 403, false)); err != nil {
		t.Fatalf("SetRefusal: %v", err)
	}

	if e := c.Get("g"); e != nil {
		t.Errorf("Get(g) = %+v, want nil: a refused group is unknown, not an empty group", e)
	}
	if len(c.GetAll()) != 0 {
		t.Errorf("GetAll = %+v, want no listing", c.GetAll())
	}
}

func TestGroupCache_ASuccessfulListingClearsTheRefusal(t *testing.T) {
	c := newTestGroupCache(t)
	if err := c.SetRefusal("g", refusalAt(time.Now(), 403, false)); err != nil {
		t.Fatalf("SetRefusal: %v", err)
	}

	if err := c.Set("g", groupEntry(time.Now(), "one")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if r := c.Refusal("g"); r != nil {
		t.Errorf("Refusal(g) = %+v after a listing, want none", r)
	}
	if r := reopenGroupCache(c).Refusal("g"); r != nil {
		t.Errorf("Refusal(g) = %+v after a reopen, the cleared refusal came back from disk", r)
	}
}

// A settled "0 members" clears the refusal too: it is a listing like any other.
func TestGroupCache_AnEmptyListingAlsoClearsTheRefusal(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.SetRefusal("g", refusalAt(time.Now(), 403, false))

	if err := c.Set("g", RegistryGroupEntry{DiscoveredAt: time.Now()}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if c.Refusal("g") != nil {
		t.Error("the refusal survived a settled empty listing")
	}
}

func TestGroupCache_ARefusalForOneSlugLeavesTheOthersAlone(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.Set("a", groupEntry(time.Now(), "one"))
	_ = c.SetRefusal("b", refusalAt(time.Now(), 403, false))
	_ = c.SetRefusal("c", refusalAt(time.Now(), 401, true))

	_ = c.Set("b", groupEntry(time.Now(), "two"))

	if c.Refusal("b") != nil {
		t.Error("Refusal(b) survived its own listing")
	}
	if r := c.Refusal("c"); r == nil || r.Status != 401 {
		t.Errorf("Refusal(c) = %+v, want it untouched by a listing of b", r)
	}
	if c.Get("a") == nil {
		t.Error("Get(a) lost by a refusal on another slug")
	}
}

func TestGroupCache_DeleteRemovesTheListingAndTheRefusal(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.Set("g", groupEntry(time.Now(), "one"))
	_ = c.SetRefusal("g", refusalAt(time.Now(), 403, false))
	_ = c.SetRefusal("only-refused", refusalAt(time.Now(), 403, false))

	if err := c.Delete("g"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := c.Delete("only-refused"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	reopened := reopenGroupCache(c)
	for name, cc := range map[string]*RegistryGroupCache{"in memory": c, "after a reopen": reopened} {
		if cc.Get("g") != nil || cc.Refusal("g") != nil {
			t.Errorf("%s: g survived Delete (listing %+v, refusal %+v)", name, cc.Get("g"), cc.Refusal("g"))
		}
		if cc.Refusal("only-refused") != nil {
			t.Errorf("%s: a refusal with no listing survived Delete", name)
		}
	}
}

func TestGroupCache_RefusalMiss(t *testing.T) {
	if r := newTestGroupCache(t).Refusal("absent"); r != nil {
		t.Errorf("Refusal on an empty cache returned %+v", r)
	}
}

func TestGroupCache_RefusalsCopies(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.SetRefusal("a", refusalAt(time.Now(), 403, false))
	_ = c.SetRefusal("b", refusalAt(time.Now(), 401, true))

	all := c.Refusals()
	if len(all) != 2 {
		t.Fatalf("Refusals returned %d entries, want 2", len(all))
	}
	delete(all, "a")
	all["b"] = RegistryGroupRefusal{Status: 500}

	if c.Refusal("a") == nil {
		t.Error("mutating the returned map reached the cache")
	}
	if r := c.Refusal("b"); r == nil || r.Status != 401 {
		t.Errorf("Refusal(b) = %+v, want it unchanged by a write to the copy", r)
	}
}

func TestGroupCache_RefusalReturnsACopy(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.SetRefusal("a", refusalAt(time.Now(), 403, false))

	got := c.Refusal("a")
	if got == nil {
		t.Fatal("Refusal(a) = nil right after SetRefusal")
	}
	got.Status = 500

	if r := c.Refusal("a"); r == nil || r.Status != 403 {
		t.Errorf("Refusal(a) = %+v, want the cache's own value untouched by the caller", r)
	}
}

func TestGroupCache_ReloadPicksUpAnotherWritersRefusal(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.Set("prod", groupEntry(time.Now(), "one"))

	other := reopenGroupCache(c)
	_ = other.SetRefusal("prod", refusalAt(time.Now(), 403, false))

	c.Reload()

	if c.Refusal("prod") == nil {
		t.Error("Reload did not pick up a refusal another writer added")
	}
	if e := c.Get("prod"); e == nil || len(e.Members) != 1 {
		t.Errorf("Get(prod) = %+v, want the listing still there after Reload", e)
	}
}

func TestGroupCache_BrokenFileLoadsNothingAndNoRefusal(t *testing.T) {
	c := newTestGroupCache(t)
	if err := os.WriteFile(c.path, []byte(`{"version": 2, "groups": {"a": `), 0o600); err != nil {
		t.Fatalf("writing the broken file: %v", err)
	}

	c.load()

	if len(c.GetAll()) != 0 || len(c.Refusals()) != 0 {
		t.Errorf("a broken cache file yielded groups %+v and refusals %+v", c.GetAll(), c.Refusals())
	}
	// A broken file is an empty cache, not a cache that refuses to be written.
	if err := c.Set("g", groupEntry(time.Now(), "one")); err != nil {
		t.Errorf("Set after a broken load: %v", err)
	}
}

// The real constructor, through HOME, like the browser opens it. HOME is shared
// by the test process (DEVDESK-36), so it is isolated here.
func TestNewRegistryGroupCache_ReadsALegacyFileAndWritesV2(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".devdesk", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "registry-groups.json")
	if err := os.WriteFile(path, []byte(legacyGroupFile), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := NewRegistryGroupCache()
	if err != nil {
		t.Fatalf("NewRegistryGroupCache: %v", err)
	}
	if c.Get("a") == nil || c.Get("b") != nil || c.Get("c") != nil {
		t.Errorf("Get(a/b/c) = %+v / %+v / %+v, want only a known", c.Get("a"), c.Get("b"), c.Get("c"))
	}
	if err := c.SetRefusal("b", refusalAt(time.Now(), 403, false)); err != nil {
		t.Fatalf("SetRefusal: %v", err)
	}

	again, err := NewRegistryGroupCache()
	if err != nil {
		t.Fatalf("NewRegistryGroupCache: %v", err)
	}
	if again.Refusal("b") == nil || again.Get("b") != nil || again.Get("a") == nil {
		t.Errorf("after a reopen: Refusal(b) %+v, Get(b) %+v, Get(a) %+v", again.Refusal("b"), again.Get("b"), again.Get("a"))
	}
	if string(parseEnvelope(t, path)["version"]) != "2" {
		t.Error("the file was not rewritten as version 2")
	}
}

// --- the read-only reader (MCP) ---

func TestReadRegistryGroupsAt_LegacyFileDropsEmptyEntriesAndDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry-groups.json")
	if err := os.WriteFile(path, []byte(legacyGroupFile), 0o600); err != nil {
		t.Fatal(err)
	}
	before := stamp(t, path)

	snap, err := readRegistryGroupsAt(path)
	if err != nil {
		t.Fatalf("readRegistryGroupsAt: %v", err)
	}

	if e, ok := snap.Groups["a"]; !ok || len(e.Members) != 1 {
		t.Errorf("Groups[a] = %+v, want kept", e)
	}
	for _, slug := range []string{"b", "c"} {
		if e, ok := snap.Groups[slug]; ok {
			t.Errorf("Groups[%s] = %+v, want the legacy empty entry dropped (CA6 through the read-only path)", slug, e)
		}
	}
	if len(snap.Refusals) != 0 {
		t.Errorf("Refusals = %+v, a legacy file has none", snap.Refusals)
	}
	assertUntouched(t, path, before)
	if !bytes.Equal(readRaw(t, path), []byte(legacyGroupFile)) {
		t.Error("the read-only reader rewrote the legacy file")
	}
}

// The writer and the read-only reader share one decoder: what the cache writes,
// the reader sees, listings and refusals apart.
func TestReadRegistryGroupsAt_SeesWhatTheCacheWrites(t *testing.T) {
	c := newTestGroupCache(t)
	at := time.Now().Truncate(time.Second)
	refusedAt := at.Add(time.Minute)
	_ = c.Set("listed", groupEntry(at, "one"))
	_ = c.Set("empty", RegistryGroupEntry{DiscoveredAt: at})
	_ = c.SetRefusal("refused", refusalAt(refusedAt, 403, false))
	_ = c.SetRefusal("listed", refusalAt(refusedAt, 401, true))
	before := stamp(t, c.path)

	snap, err := readRegistryGroupsAt(c.path)
	if err != nil {
		t.Fatalf("readRegistryGroupsAt: %v", err)
	}

	if e, ok := snap.Groups["listed"]; !ok || len(e.Members) != 1 || !e.DiscoveredAt.Equal(at) {
		t.Errorf("Groups[listed] = %+v, want the listing kept next to its refusal", e)
	}
	if e, ok := snap.Groups["empty"]; !ok || len(e.Members) != 0 {
		t.Errorf("Groups[empty] = %+v, present %v, want the settled empty entry (CA2)", e, ok)
	}
	if _, ok := snap.Groups["refused"]; ok {
		t.Error("a refused-only group shows up as a listing")
	}
	if r, ok := snap.Refusals["refused"]; !ok || r.Status != 403 || r.Anonymous || !r.At.Equal(refusedAt) {
		t.Errorf("Refusals[refused] = %+v, want the 403 account refusal", r)
	}
	if r, ok := snap.Refusals["listed"]; !ok || r.Status != 401 || !r.Anonymous {
		t.Errorf("Refusals[listed] = %+v, want the 401 anonymous refusal", r)
	}
	assertUntouched(t, c.path, before)
}

func TestReadRegistryGroupsAt_FutureVersionIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry-groups.json")
	data := `{"version": 3, "groups": {"g": {"members": [{"alias": "m", "url": "u"}], "discovered_at": "2026-01-02T03:04:05Z"}}, "refusals": {"h": {"at": "2026-02-03T04:05:06Z", "status": 403}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, err := readRegistryGroupsAt(path)
	if err != nil {
		t.Fatalf("readRegistryGroupsAt: %v", err)
	}
	if e, ok := snap.Groups["g"]; !ok || len(e.Members) != 1 {
		t.Errorf("Groups[g] = %+v, want read", e)
	}
	if r, ok := snap.Refusals["h"]; !ok || r.Status != 403 {
		t.Errorf("Refusals[h] = %+v, want read", r)
	}
}

func TestReadRegistryGroupsAt_AbsentFileIsEmptyNotAnError(t *testing.T) {
	snap, err := readRegistryGroupsAt(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("an absent cache returned %v, want an empty snapshot", err)
	}
	if len(snap.Groups) != 0 || len(snap.Refusals) != 0 {
		t.Errorf("snapshot = %+v, want empty", snap)
	}
}

func TestReadRegistryGroupsAt_BrokenFileReturnsTheError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry-groups.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, err := readRegistryGroupsAt(path)

	if err == nil {
		t.Error("err = nil, want the decode error of a broken file (as before)")
	}
	if len(snap.Groups) != 0 || len(snap.Refusals) != 0 {
		t.Errorf("snapshot = %+v, want nothing out of a broken file", snap)
	}
}

// The exported reader, through HOME, is the one the MCP server calls.
func TestReadRegistryGroups_ReadsTheFileUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".devdesk", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "registry-groups.json")
	c := &RegistryGroupCache{path: path, entries: make(map[string]RegistryGroupEntry)}
	_ = c.Set("g", groupEntry(time.Now(), "one"))
	_ = c.SetRefusal("h", refusalAt(time.Now(), 403, false))

	snap, err := ReadRegistryGroups()
	if err != nil {
		t.Fatalf("ReadRegistryGroups: %v", err)
	}

	if snap.Groups["g"].Members == nil || len(snap.Groups["g"].Members) != 1 {
		t.Errorf("Groups[g] = %+v, want the listing", snap.Groups["g"])
	}
	if r, ok := snap.Refusals["h"]; !ok || r.Status != 403 {
		t.Errorf("Refusals[h] = %+v, want the refusal", r)
	}
}

// The cache file is derived data a refusal's status and time are written into:
// it is private to the user, whatever mode an older build left on it, and the
// atomic write leaves nothing but the file itself in the directory.
func TestGroupCache_EveryWriterLeavesA0600FileAndNoTemporaryFile(t *testing.T) {
	writers := map[string]func(c *RegistryGroupCache) error{
		"Set": func(c *RegistryGroupCache) error { return c.Set("g", groupEntry(time.Now(), "one")) },
		"SetRefusal": func(c *RegistryGroupCache) error {
			return c.SetRefusal("g", refusalAt(time.Now(), 403, false))
		},
		"Delete": func(c *RegistryGroupCache) error { return c.Delete("g") },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			c := newTestGroupCache(t)
			// An older build, or the user, left the file world-readable.
			if err := os.WriteFile(c.path, []byte(legacyGroupFile), 0o644); err != nil {
				t.Fatalf("seeding the old file: %v", err)
			}
			if err := os.Chmod(c.path, 0o644); err != nil {
				t.Fatalf("chmod the old file: %v", err)
			}

			if err := write(c); err != nil {
				t.Fatalf("%s: %v", name, err)
			}

			if runtime.GOOS != "windows" {
				info, err := os.Stat(c.path)
				if err != nil {
					t.Fatalf("stat the cache file: %v", err)
				}
				if mode := info.Mode().Perm(); mode != 0o600 {
					t.Errorf("after %s the cache file has mode %o, want 600", name, mode)
				}
			}
			entries, err := os.ReadDir(filepath.Dir(c.path))
			if err != nil {
				t.Fatalf("read the cache directory: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != filepath.Base(c.path) {
				names := make([]string, 0, len(entries))
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Errorf("after %s the cache directory holds %v, want only the cache file (no temporary file left)", name, names)
			}
		})
	}
}

// Reload takes the file as the truth. It replaces the maps rather than merging
// into them, so what another writer deleted or replaced is gone here too, and a
// refusal another writer cleared does not linger.
func TestGroupCache_ReloadReplacesTheMapsInsteadOfMergingThem(t *testing.T) {
	now := time.Now()
	c := newTestGroupCache(t)
	for slug, aliases := range map[string][]string{"gone": {"one"}, "kept": {"one"}, "changed": {"old"}, "cleared": {"one"}} {
		if err := c.Set(slug, groupEntry(now, aliases...)); err != nil {
			t.Fatalf("Set(%s): %v", slug, err)
		}
	}
	for _, slug := range []string{"refused-stays", "refused-dropped"} {
		if err := c.SetRefusal(slug, refusalAt(now, 403, false)); err != nil {
			t.Fatalf("SetRefusal(%s): %v", slug, err)
		}
	}

	// Another writer, on the same file: removes an entry, replaces another,
	// drops a refusal through Delete, and clears one by listing successfully.
	other := reopenGroupCache(c)
	if err := other.Delete("gone"); err != nil {
		t.Fatalf("other.Delete(gone): %v", err)
	}
	if err := other.Set("changed", groupEntry(now.Add(time.Hour), "new-a", "new-b")); err != nil {
		t.Fatalf("other.Set(changed): %v", err)
	}
	if err := other.Delete("refused-dropped"); err != nil {
		t.Fatalf("other.Delete(refused-dropped): %v", err)
	}
	if err := other.SetRefusal("cleared", refusalAt(now, 401, true)); err != nil {
		t.Fatalf("other.SetRefusal(cleared): %v", err)
	}
	if err := other.Set("cleared", groupEntry(now, "one")); err != nil { // a listing clears the refusal
		t.Fatalf("other.Set(cleared): %v", err)
	}

	c.Reload()

	if e := c.Get("gone"); e != nil {
		t.Errorf("Get(gone) = %+v after Reload, want it gone: Reload merged instead of replacing", e)
	}
	if e := c.Get("kept"); e == nil {
		t.Error("Get(kept) is missing after Reload, want the untouched entry kept")
	}
	if e := c.Get("changed"); e == nil || len(e.Members) != 2 {
		t.Errorf("Get(changed) = %+v after Reload, want the other writer's two members", e)
	}
	if r := c.Refusal("refused-dropped"); r != nil {
		t.Errorf("Refusal(refused-dropped) = %+v after Reload, want it gone: Reload merged instead of replacing", r)
	}
	if r := c.Refusal("cleared"); r != nil {
		t.Errorf("Refusal(cleared) = %+v after Reload, want it cleared by the other writer's listing", r)
	}
	if r := c.Refusal("refused-stays"); r == nil {
		t.Error("Refusal(refused-stays) is missing after Reload, want the untouched refusal kept")
	}
	if got, want := len(c.GetAll()), 3; got != want {
		t.Errorf("GetAll holds %d entries after Reload, want %d", got, want)
	}
}
