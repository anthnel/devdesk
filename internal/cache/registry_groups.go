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

// RegistryGroupCache manages the discovered-members cache file.
type RegistryGroupCache struct {
	mu      sync.RWMutex
	path    string
	entries map[string]RegistryGroupEntry // keyed by group slug
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
		path:    filepath.Join(cacheDir, "registry-groups.json"),
		entries: make(map[string]RegistryGroupEntry),
	}

	c.load()
	return c, nil
}

// load reads the cache file from disk
func (c *RegistryGroupCache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		log.Printf("ERROR [cache/registry_groups] unmarshal cache file %s: %v", c.path, err)
	}
}

// save writes the cache to disk
func (c *RegistryGroupCache) save() error {
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
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

// Set stores a discovery and persists to disk.
func (c *RegistryGroupCache) Set(slug string, entry RegistryGroupEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[slug] = entry
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

// Delete removes one group's discovery and persists to disk.
func (c *RegistryGroupCache) Delete(slug string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, slug)
	return c.save()
}

// Reload re-reads the cache from disk.
func (c *RegistryGroupCache) Reload() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
}
