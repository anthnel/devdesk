package cache

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Discovered group members are derived data with a server as their source of
// truth, and config.yaml is what the user declares — so they live here, keyed
// by group slug, refreshed explicitly rather than on every browser open
// (§3.8, decision 3).
//
// The member type is this package's own rather than registrymgr's: this is a
// file format, and it should not move because a domain type did.

// RegistryGroupMember is one repository fronted by a group.
//
// The pair (URL, RepoPrefix) is the member's address, and both halves are needed
// because the prefix is what tells two members of one group apart once they no
// longer each carry a synthesised URL of their own (§3.18). An entry written
// before the field existed decodes to an empty prefix, which is what it meant.
type RegistryGroupMember struct {
	Alias      string `json:"alias"`
	URL        string `json:"url"`
	RepoPrefix string `json:"repo_prefix,omitempty"`
}

// RegistryGroupEntry is the result of one discovery. An entry with no members
// is a real answer — "asked, and it is not a group" — and is what keeps a
// non-group from being probed forever.
type RegistryGroupEntry struct {
	Members      []RegistryGroupMember `json:"members"`
	DiscoveredAt time.Time             `json:"discovered_at"`
}

// RegistryGroupRefusal records that a group's repository manager refused to
// list its members to the account that asked. It is not a listing: it never
// replaces the members last known, and a later listing clears it.
type RegistryGroupRefusal struct {
	At        time.Time `json:"at"`
	Status    int       `json:"status"`
	Anonymous bool      `json:"anonymous,omitempty"`
}

// registryGroupCacheVersion is the version of the envelope written to disk;
// version 1 is the legacy bare map, which carries no version key.
const registryGroupCacheVersion = 2

// registryGroupFile is the on-disk envelope (version 2).
type registryGroupFile struct {
	Version  int                             `json:"version"`
	Groups   map[string]RegistryGroupEntry   `json:"groups"`
	Refusals map[string]RegistryGroupRefusal `json:"refusals,omitempty"`
}

// decodeRegistryGroupFile reads either file shape; the cache and the read-only
// reader both go through it. dropped counts the legacy entries set aside (CA6).
//
// A version 2 (or newer) envelope is taken as it is: there an entry with no
// member is a settled "0 members". Anything else is read as the legacy bare map
// and keeps only the entries with at least one member: a non-empty entry can
// only come from a successful listing, while an empty one may be a refusal the
// old code swallowed, indistinguishable from a real "not a group", so it becomes
// unknown again. An explicit version below 2 gets the same treatment, so no
// shape yields a false "0 members".
//
// The envelope is recognized by a numeric "version" key, not by the presence of
// a "groups" key: a legacy slug may be named "groups" or "version", and its
// value is an object.
func decodeRegistryGroupFile(data []byte) (groups map[string]RegistryGroupEntry,
	refusals map[string]RegistryGroupRefusal, dropped int, err error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, 0, err
	}

	var version int
	hasVersion := false
	if v, ok := raw["version"]; ok {
		hasVersion = json.Unmarshal(v, &version) == nil
	}

	switch {
	case hasVersion && version >= registryGroupCacheVersion:
		var file registryGroupFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, nil, 0, err
		}
		return nonNilGroups(file.Groups), nonNilRefusals(file.Refusals), 0, nil
	case hasVersion:
		// An envelope older than this build knows: only its listings, and only
		// the ones that prove a successful read.
		var file registryGroupFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, nil, 0, err
		}
		groups, dropped = keepListed(file.Groups)
		return groups, map[string]RegistryGroupRefusal{}, dropped, nil
	default:
		var legacy map[string]RegistryGroupEntry
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, nil, 0, err
		}
		groups, dropped = keepListed(legacy)
		return groups, map[string]RegistryGroupRefusal{}, dropped, nil
	}
}

// keepListed returns the entries that hold at least one member and how many
// were set aside.
func keepListed(entries map[string]RegistryGroupEntry) (kept map[string]RegistryGroupEntry, dropped int) {
	kept = make(map[string]RegistryGroupEntry, len(entries))
	for slug, e := range entries {
		if len(e.Members) == 0 {
			dropped++
			continue
		}
		kept[slug] = e
	}
	return kept, dropped
}

func nonNilGroups(m map[string]RegistryGroupEntry) map[string]RegistryGroupEntry {
	if m == nil {
		return map[string]RegistryGroupEntry{}
	}
	return m
}

func nonNilRefusals(m map[string]RegistryGroupRefusal) map[string]RegistryGroupRefusal {
	if m == nil {
		return map[string]RegistryGroupRefusal{}
	}
	return m
}

// RegistryGroupCache manages the discovered-members cache file.
type RegistryGroupCache struct {
	mu       sync.RWMutex
	path     string
	entries  map[string]RegistryGroupEntry   // keyed by group slug
	refusals map[string]RegistryGroupRefusal // keyed by group slug
}

// NewRegistryGroupCache creates or loads the cache from
// ~/.devdesk/cache/registry-groups.json
func NewRegistryGroupCache() (*RegistryGroupCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	c := &RegistryGroupCache{
		path:     filepath.Join(cacheDir, "registry-groups.json"),
		entries:  make(map[string]RegistryGroupEntry),
		refusals: make(map[string]RegistryGroupRefusal),
	}

	c.load()
	return c, nil
}

// load reads the cache file from disk. It never writes: a legacy file is only
// rewritten in the current version by the next Set, SetRefusal or Delete. A file
// that cannot be decoded leaves the cache as it was.
func (c *RegistryGroupCache) load() {
	if c.entries == nil {
		c.entries = make(map[string]RegistryGroupEntry)
	}
	if c.refusals == nil {
		c.refusals = make(map[string]RegistryGroupRefusal)
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	groups, refusals, dropped, err := decodeRegistryGroupFile(data)
	if err != nil {
		log.Printf("ERROR [cache/registry_groups] unmarshal cache file %s: %v", c.path, err)
		return
	}
	if dropped > 0 {
		log.Printf("INFO [cache/registry_groups] set aside %d legacy entries with no member in %s", dropped, c.path)
	}
	c.entries = groups
	c.refusals = refusals
}

// save writes the cache to disk as the current envelope, atomically.
func (c *RegistryGroupCache) save() error {
	data, err := json.MarshalIndent(registryGroupFile{
		Version:  registryGroupCacheVersion,
		Groups:   nonNilGroups(c.entries),
		Refusals: c.refusals,
	}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(c.path, data)
}

// Get returns the cached discovery for a group slug, or nil when there is none.
func (c *RegistryGroupCache) Get(slug string) *RegistryGroupEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.entries[slug]; ok {
		return &entry
	}
	return nil
}

// Set stores a discovery and persists to disk. A listing that succeeded, even an
// empty one, supersedes any refusal recorded for the slug.
func (c *RegistryGroupCache) Set(slug string, entry RegistryGroupEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[slug] = entry
	delete(c.refusals, slug)
	return c.save()
}

// GetAll returns every cached discovery.
func (c *RegistryGroupCache) GetAll() map[string]RegistryGroupEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]RegistryGroupEntry, len(c.entries))
	for k, v := range c.entries {
		result[k] = v
	}
	return result
}

// Delete removes one group's discovery and refusal and persists to disk.
func (c *RegistryGroupCache) Delete(slug string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, slug)
	delete(c.refusals, slug)
	return c.save()
}

// Reload re-reads the cache from disk.
func (c *RegistryGroupCache) Reload() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
}

// SetRefusal stores a refusal beside the listing and persists to disk. It never
// touches the members last known, and creates no listing.
func (c *RegistryGroupCache) SetRefusal(slug string, r RegistryGroupRefusal) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refusals == nil {
		c.refusals = make(map[string]RegistryGroupRefusal)
	}
	c.refusals[slug] = r
	return c.save()
}

// Refusal returns the refusal recorded for a group slug, or nil when there is none.
func (c *RegistryGroupCache) Refusal(slug string) *RegistryGroupRefusal {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if r, ok := c.refusals[slug]; ok {
		return &r
	}
	return nil
}

// Refusals returns a copy of every recorded refusal.
func (c *RegistryGroupCache) Refusals() map[string]RegistryGroupRefusal {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]RegistryGroupRefusal, len(c.refusals))
	for k, v := range c.refusals {
		result[k] = v
	}
	return result
}
