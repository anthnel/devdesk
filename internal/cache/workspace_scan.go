package cache

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gitlab.com/anthnell/devsecops/devdesk/internal/scan"
)

// WorkspaceScanEntry holds cached CVE counts for a scanned workspace git repo
type WorkspaceScanEntry struct {
	RepoPath  string    `json:"repo_path"`
	Critical  int       `json:"critical"`
	High      int       `json:"high"`
	Medium    int       `json:"medium"`
	Low       int       `json:"low"`
	Sensitive bool      `json:"sensitive"` // true if secrets detected
	ScannedAt time.Time `json:"scanned_at"`
}

// WorkspaceScanCache manages workspace scan results cache
type WorkspaceScanCache struct {
	mu      sync.RWMutex
	path    string
	entries map[string]WorkspaceScanEntry // keyed by absolute repo path
}

// NewWorkspaceScanCache creates or loads the cache from ~/.devdesk/cache/workspace-scans.json
func NewWorkspaceScanCache() (*WorkspaceScanCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	c := &WorkspaceScanCache{
		path:    filepath.Join(cacheDir, "workspace-scans.json"),
		entries: make(map[string]WorkspaceScanEntry),
	}

	c.load()
	return c, nil
}

// load reads the cache file from disk
func (c *WorkspaceScanCache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		log.Printf("ERROR [cache/workspace_scan] unmarshal cache file %s: %v", c.path, err)
	}
}

// save writes the cache to disk (must be called with lock held)
func (c *WorkspaceScanCache) save() error {
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// Get returns the cached scan entry for a repo path, or nil if not found
func (c *WorkspaceScanCache) Get(repoPath string) *WorkspaceScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.entries[repoPath]; ok {
		return &entry
	}
	return nil
}

// Set stores a scan entry and persists to disk
func (c *WorkspaceScanCache) Set(repoPath string, entry WorkspaceScanEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[repoPath] = entry
	return c.save()
}

// GetAll returns all cached entries
func (c *WorkspaceScanCache) GetAll() map[string]WorkspaceScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]WorkspaceScanEntry, len(c.entries))
	for k, v := range c.entries {
		result[k] = v
	}
	return result
}

// Delete removes one entry from the cache and persists to disk
func (c *WorkspaceScanCache) Delete(repoPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, repoPath)
	return c.save()
}

// Reload re-reads the cache from disk
func (c *WorkspaceScanCache) Reload() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
}

// workspaceScanResultDir returns the directory for full scan results, creating it if needed
func workspaceScanResultDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(homeDir, ".devdesk", "cache", "workspace-results")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// resultFilePath returns the JSON file path for the full scan result of a repo path.
// Uses SHA256 of the path to avoid invalid filename characters.
func resultFilePath(repoPath string) (string, error) {
	dir, err := workspaceScanResultDir()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(repoPath))
	filename := fmt.Sprintf("%x.json", hash)
	return filepath.Join(dir, filename), nil
}

// SaveWorkspaceScanResult persists a full scan result to disk.
// Designed to be called in a goroutine: go SaveWorkspaceScanResult(path, result)
func SaveWorkspaceScanResult(repoPath string, result *scan.Result) error {
	filePath, err := resultFilePath(repoPath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0600)
}

// DeleteWorkspaceScanResult removes the full scan result file from disk.
// Used by purgeScanCacheCmd before re-scanning to avoid stale results.
func DeleteWorkspaceScanResult(repoPath string) error {
	filePath, err := resultFilePath(repoPath)
	if err != nil {
		return err
	}
	err = os.Remove(filePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // already gone, not an error
	}
	return err
}

// LoadWorkspaceScanResult loads a full scan result from disk
func LoadWorkspaceScanResult(repoPath string) (*scan.Result, error) {
	filePath, err := resultFilePath(repoPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var result scan.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
