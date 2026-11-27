package cache

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/anthnel/devdesk/internal/docker"
)

// PortMappingEntry holds the cached host port and enabled state for a single container port.
type PortMappingEntry struct {
	HostPort string `json:"host_port"`
	Enabled  bool   `json:"enabled"`
}

// LaunchOptionsEntry holds all cached form fields for a single image launch.
// The container name is intentionally excluded (must be unique per container).
// Environment variables are cached by name only: a value is a secret in the
// making and never reaches this file (OC-17, requirement S9).
type LaunchOptionsEntry struct {
	Entrypoint   string                      `json:"entrypoint"`
	ExtraPorts   string                      `json:"extra_ports"`
	EnvNames     []string                    `json:"env_names,omitempty"`
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

// maxLaunchCacheBytes bounds the read of the cache file; a larger one is
// treated as corrupt.
const maxLaunchCacheBytes = 1 << 20

// legacyLaunchEntry is an entry as it is read from disk: the current shape plus
// the "env" field of the builds before OC-17, which held the whole Environment
// field, values included. It exists to read that field once and drop it.
type legacyLaunchEntry struct {
	LaunchOptionsEntry
	LegacyEnv string `json:"env"`
}

// EnvNames returns the variable names of NAME=VALUE entries and nothing of their
// values, in order and without duplicates. An entry with no "=" is dropped, and so
// is one with no name or one whose value is only "=" characters: none has a
// name to keep, and what is typed there is the secret itself (a token pasted
// into the field, `abcdef==`). A name is kept as typed, valid or not: refusing
// it is the form's job.
func EnvNames(entries []string) []string {
	var names []string
	for _, e := range entries {
		name, value, ok := strings.Cut(strings.TrimSpace(e), "=")
		if !ok || value != "" && strings.Trim(value, "=") == "" {
			continue
		}
		names = appendName(names, name)
	}
	return names
}

// maxMigratedNameLen bounds a name kept from an older cache.
const maxMigratedNameLen = 128

// tokenPrefixes are the starts of well-known credentials (GitHub, GitLab, Slack,
// AWS). A name that starts with one is a token pasted in the name position, not a
// variable name, and the migration drops it.
var tokenPrefixes = []string{"ghp_", "gho_", "ghu_", "ghs_", "github_pat_", "glpat-", "xoxb-", "xoxp-", "AKIA"}

// legacyEnvNames reads the "env" field of an older cache: the Environment field
// as it was typed, values included. It keeps the names a launch could have used
// (docker.ValidEnvName, 128 bytes at most, no token prefix; a reserved name stays,
// the form refuses it at the submit) and drops the rest. Limit: the field is split
// on commas, so the piece after a comma inside a value (`TOKEN=a,b=c` gives `b=c`)
// cannot be told from an entry and its name stays.
func legacyEnvNames(env string) []string {
	var names []string
	for _, name := range EnvNames(strings.Split(env, ",")) {
		if len(name) > maxMigratedNameLen || !docker.ValidEnvName(name) {
			continue
		}
		if slices.ContainsFunc(tokenPrefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
			continue
		}
		names = append(names, name)
	}
	return names
}

// appendName adds name unless it is blank or already there.
func appendName(names []string, name string) []string {
	if name = strings.TrimSpace(name); name == "" || slices.Contains(names, name) {
		return names
	}
	return append(names, name)
}

// nameOnly keeps what precedes the first "=" of each name: whatever a caller
// puts in EnvNames, a value cannot be written.
func nameOnly(names []string) []string {
	var out []string
	for _, n := range names {
		name, _, _ := strings.Cut(n, "=")
		out = appendName(out, name)
	}
	return out
}

// NewLaunchOptionsCache creates or loads the cache from ~/.devdesk/cache/launch-options.json
func NewLaunchOptionsCache() (*LaunchOptionsCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	// 0700: a directory DevDesk creates here is private (an existing one is left as it is).
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, err
	}

	c := &LaunchOptionsCache{
		path:    filepath.Join(cacheDir, "launch-options.json"),
		entries: make(map[string]LaunchOptionsEntry),
	}
	if c.load() {
		if err := c.save(); err != nil {
			log.Printf("ERROR [cache/launch_options] rewrite migrated cache file: %v", err)
		}
	}
	return c, nil
}

// load reads the cache file from disk and reports whether it read values left
// by an earlier build: they are dropped here and the caller writes the file back
// without them. A file that is not valid JSON, or larger than
// maxLaunchCacheBytes, is removed: the cache is regenerable and the file may hold
// values an older build wrote. The log is a fixed line: never the content, the
// path or the parser's error, which quotes the byte it stopped on.
func (c *LaunchOptionsCache) load() (migrated bool) {
	f, err := os.Open(c.path)
	if err != nil {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxLaunchCacheBytes+1))
	_ = f.Close()
	if err != nil {
		return false
	}
	var onDisk map[string]legacyLaunchEntry
	if len(data) > maxLaunchCacheBytes || json.Unmarshal(data, &onDisk) != nil {
		log.Printf("ERROR [cache/launch_options] cache file is not valid JSON or too large, removed")
		if err := os.Remove(c.path); err != nil {
			log.Printf("ERROR [cache/launch_options] remove the unreadable cache file: %v", err)
		}
		return false
	}
	for key, e := range onDisk {
		entry := e.LaunchOptionsEntry
		names := nameOnly(entry.EnvNames)
		if !slices.Equal(names, entry.EnvNames) {
			migrated = true // a value written by hand in env_names
		}
		if e.LegacyEnv != "" {
			migrated = true
			names = nameOnly(append(names, legacyEnvNames(e.LegacyEnv)...))
		}
		entry.EnvNames = names
		c.entries[key] = entry
	}
	return migrated
}

// save writes the cache to disk, always as a 0600 file (an earlier file with a
// wider mode is replaced, not reused).
func (c *LaunchOptionsCache) save() error {
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(c.path, data)
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
	entry.EnvNames = nameOnly(entry.EnvNames)
	c.entries[imageKey] = entry
	return c.save()
}
