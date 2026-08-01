package cache

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// PortMappingEntry holds the cached host port and enabled state for a single container port.
type PortMappingEntry struct {
	HostPort string `json:"host_port"`
	Enabled  bool   `json:"enabled"`
}

// LaunchOptionsEntry holds all cached form fields for a single image launch.
// The container name is intentionally excluded (must be unique per container).
type LaunchOptionsEntry struct {
	Entrypoint   string                      `json:"entrypoint"`
	ExtraPorts   string                      `json:"extra_ports"`
	Env          string                      `json:"env"`
	Volumes      string                      `json:"volumes"`
	User         string                      `json:"user"`
	Network      string                      `json:"network"`
	Remove       bool                        `json:"remove"`
	Detach       bool                        `json:"detach"`
	Interactive  bool                        `json:"interactive"`
	PortMappings map[string]PortMappingEntry `json:"port_mappings"` // containerPort -> {HostPort, Enabled}
}

// LaunchOptionsCache manages the launch options cache file.
type LaunchOptionsCache struct {
	mu      sync.RWMutex
	path    string
	entries map[string]LaunchOptionsEntry // keyed by "name:tag"
}

// NewLaunchOptionsCache creates or loads the cache from ~/.devdesk/cache/launch-options.json
func NewLaunchOptionsCache() (*LaunchOptionsCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	c := &LaunchOptionsCache{
		path:    filepath.Join(cacheDir, "launch-options.json"),
		entries: make(map[string]LaunchOptionsEntry),
	}
	c.load()
	return c, nil
}

// load reads the cache file from disk.
func (c *LaunchOptionsCache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		log.Printf("ERROR [cache/launch_options] unmarshal cache file %s: %v", c.path, err)
	}
}

// save writes the cache to disk.
func (c *LaunchOptionsCache) save() error {
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// Get returns the cached launch options for an image, or nil if not found.
func (c *LaunchOptionsCache) Get(imageKey string) *LaunchOptionsEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.entries[imageKey]; ok {
		return &entry
	}
	return nil
}

// Set stores launch options and persists to disk.
func (c *LaunchOptionsCache) Set(imageKey string, entry LaunchOptionsEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[imageKey] = entry
	return c.save()
}
