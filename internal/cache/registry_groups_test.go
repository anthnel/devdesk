package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestGroupCache creates a RegistryGroupCache backed by a temp dir.
func newTestGroupCache(t *testing.T) *RegistryGroupCache {
	t.Helper()
	return &RegistryGroupCache{
		path:    filepath.Join(t.TempDir(), "registry-groups.json"),
		entries: make(map[string]RegistryGroupEntry),
	}
}

func groupEntry(at time.Time, aliases ...string) RegistryGroupEntry {
	members := make([]RegistryGroupMember, 0, len(aliases))
	for _, a := range aliases {
		members = append(members, RegistryGroupMember{Alias: a, URL: "https://nexus.example.com/repository/" + a})
	}
	return RegistryGroupEntry{Members: members, DiscoveredAt: at}
}

func TestGroupCache_GetMiss(t *testing.T) {
	if entry := newTestGroupCache(t).Get("absent"); entry != nil {
		t.Errorf("Get on an empty cache returned %+v", entry)
	}
}

func TestGroupCache_SetAndGet(t *testing.T) {
	c := newTestGroupCache(t)
	at := time.Now().Truncate(time.Second)

	if err := c.Set("prod", groupEntry(at, "hosted", "dhi")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entry := c.Get("prod")
	if entry == nil {
		t.Fatal("Get returned nothing for a slug just set")
	}
	if len(entry.Members) != 2 {
		t.Errorf("Members = %+v, want both", entry.Members)
	}
	if entry.Members[0].Alias != "hosted" {
		t.Errorf("Members[0].Alias = %q, want the order preserved", entry.Members[0].Alias)
	}
	if !entry.DiscoveredAt.Equal(at) {
		t.Errorf("DiscoveredAt = %v, want %v — the column reads it", entry.DiscoveredAt, at)
	}
}

// "Asked, and it is not a group" is an answer. Without it a non-group would be
// probed on every open, which is the cost the cache exists to remove.
func TestGroupCache_AnEmptyDiscoveryIsStillAnAnswer(t *testing.T) {
	c := newTestGroupCache(t)
	at := time.Now()

	if err := c.Set("hosted", RegistryGroupEntry{DiscoveredAt: at}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entry := c.Get("hosted")
	if entry == nil {
		t.Fatal("a discovery that found no members was not recorded at all")
	}
	if len(entry.Members) != 0 {
		t.Errorf("Members = %+v, want none", entry.Members)
	}
	if entry.DiscoveredAt.IsZero() {
		t.Error("the entry carries no time, so nothing can say how stale it is")
	}
}

func TestGroupCache_Overwrite(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.Set("prod", groupEntry(time.Now().Add(-time.Hour), "old"))

	newer := time.Now()
	if err := c.Set("prod", groupEntry(newer, "a", "b", "c")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entry := c.Get("prod")
	if len(entry.Members) != 3 {
		t.Errorf("Members = %+v, want the refreshed list to replace the old one", entry.Members)
	}
	if !entry.DiscoveredAt.Equal(newer) {
		t.Errorf("DiscoveredAt = %v, want the refresh time", entry.DiscoveredAt)
	}
}

func TestGroupCache_GetAllCopies(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.Set("a", groupEntry(time.Now(), "one"))
	_ = c.Set("b", groupEntry(time.Now(), "two"))

	all := c.GetAll()
	if len(all) != 2 {
		t.Fatalf("GetAll returned %d entries, want 2", len(all))
	}
	delete(all, "a")
	if c.Get("a") == nil {
		t.Error("mutating the returned map reached the cache")
	}
}

func TestGroupCache_Delete(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.Set("prod", groupEntry(time.Now(), "one"))

	if err := c.Delete("prod"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if c.Get("prod") != nil {
		t.Error("the entry survived Delete")
	}
	if err := c.Delete("never-there"); err != nil {
		t.Errorf("deleting an absent slug returned %v", err)
	}
}

// The point of the cache is that the next session does not go to the network.
func TestGroupCache_SurvivesAReopen(t *testing.T) {
	c := newTestGroupCache(t)
	at := time.Now().Truncate(time.Second)
	if err := c.Set("prod", groupEntry(at, "hosted", "dhi")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	reopened := &RegistryGroupCache{path: c.path, entries: make(map[string]RegistryGroupEntry)}
	reopened.load()

	entry := reopened.Get("prod")
	if entry == nil {
		t.Fatal("nothing was on disk to reopen")
	}
	if len(entry.Members) != 2 || entry.Members[1].URL == "" {
		t.Errorf("Members = %+v, want both with their URLs", entry.Members)
	}
	if !entry.DiscoveredAt.Equal(at) {
		t.Errorf("DiscoveredAt = %v, want %v", entry.DiscoveredAt, at)
	}
}

func TestGroupCache_Reload(t *testing.T) {
	c := newTestGroupCache(t)
	_ = c.Set("prod", groupEntry(time.Now(), "one"))

	other := &RegistryGroupCache{path: c.path, entries: make(map[string]RegistryGroupEntry)}
	other.load()
	_ = other.Set("staging", groupEntry(time.Now(), "two"))

	c.Reload()

	if c.Get("staging") == nil {
		t.Error("Reload did not pick up what another writer added")
	}
}

// A cache file that cannot be parsed must not take the application down with
// it: an empty cache means one refresh, a crash means no application.
func TestGroupCache_LoadsNothingFromABrokenFile(t *testing.T) {
	c := newTestGroupCache(t)
	if err := os.WriteFile(c.path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing the broken file: %v", err)
	}

	c.load()

	if len(c.GetAll()) != 0 {
		t.Errorf("a broken cache file yielded %+v", c.GetAll())
	}
}

func TestGroupCache_LoadMissingFile(t *testing.T) {
	c := newTestGroupCache(t)

	c.load()

	if len(c.GetAll()) != 0 {
		t.Errorf("a missing cache file yielded %+v", c.GetAll())
	}
}
