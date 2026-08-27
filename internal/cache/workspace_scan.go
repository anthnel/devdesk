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

	"github.com/anthnel/devdesk/internal/scan"
)

// WorkspaceScanEntry holds cached CVE counts for a scanned workspace git repo
type WorkspaceScanEntry struct {
	RepoPath string `json:"repo_path"`
	Critical int    `json:"critical"`
	High     int    `json:"high"`
	Medium   int    `json:"medium"`
	Low      int    `json:"low"`
	// Sensitive is the secret verdict — same three values as on ImageScanEntry,
	// écrites par le même `scan.Result.SecretVerdict()`.
	//
	// Le passage du booléen au pointeur ne perd rien : l'ancien champ s'écrivait
	// **toujours** (`json:"sensitive"`, sans omitempty), donc un fichier écrit
	// par une version précédente décode en pointeur non nul, verdict compris.
	// Ce que le pointeur ajoute est le cas qui manquait : une étape secrets
	// coupée par l'option ou par un outil absent rendait `false`, c'est-à-dire
	// « propre », d'un scan qui n'avait pas regardé.
	Sensitive *bool `json:"sensitive,omitempty"`
	// CIScore is the pipeline grade, written by scan.Result.CIVerdict(), and it
	// is a pointer for exactly Sensitive's reason: nil means nobody graded this
	// repository — the option is off, plumber is absent, the repository is not
	// this context's forge, or the run was withheld. A letter is a claim, and
	// an empty string in its place would be a fifth state nothing means.
	//
	// The field has never been written, so an existing cache file decodes to
	// nil, which is the truth about it. ImageScanEntry gains nothing: an image
	// has no pipeline.
	CIScore   *string   `json:"ci_score,omitempty"`
	ScannedAt time.Time `json:"scanned_at"`
}

// WorkspaceScanCache manages workspace scan results cache.
//
// Bound to one configuration context, like ImageScanCache: workspaces_dir is
// per context, so two contexts legitimately hold different roots.
type WorkspaceScanCache struct {
	mu       sync.RWMutex
	path     string
	context  string
	contexts map[string]map[string]WorkspaceScanEntry // context → repo path → entry
}

// NewWorkspaceScanCache creates or loads the cache from
// ~/.devdesk/cache/workspace-scans.json, scoped to a configuration context.
func NewWorkspaceScanCache(context string) (*WorkspaceScanCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	return newWorkspaceScanCacheAt(filepath.Join(cacheDir, "workspace-scans.json"), context)
}

// newWorkspaceScanCacheAt is NewWorkspaceScanCache without the home-directory
// lookup, so tests can point at a temp file.
func newWorkspaceScanCacheAt(path, context string) (*WorkspaceScanCache, error) {
	c := &WorkspaceScanCache{
		path:     path,
		context:  context,
		contexts: make(map[string]map[string]WorkspaceScanEntry),
	}
	c.load()
	return c, nil
}

// load reads the cache file from disk
func (c *WorkspaceScanCache) load() {
	contexts, err := readScanCacheFile[WorkspaceScanEntry](c.path, c.context)
	if err != nil {
		log.Printf("ERROR [cache/workspace_scan] read cache file %s: %v", c.path, err)
		return
	}
	c.contexts = contexts
}

// entries returns this context's map, creating it on first write.
// Must be called with the lock held.
func (c *WorkspaceScanCache) entries() map[string]WorkspaceScanEntry {
	return entriesFor(c.contexts, c.context)
}

// save writes the cache to disk (must be called with lock held)
func (c *WorkspaceScanCache) save() error {
	return writeScanCacheFile(c.path, c.contexts)
}

// Get returns the cached scan entry for a repo path, or nil if not found
func (c *WorkspaceScanCache) Get(repoPath string) *WorkspaceScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.contexts[c.context][repoPath]; ok {
		return &entry
	}
	return nil
}

// Set stores a scan entry and persists to disk
func (c *WorkspaceScanCache) Set(repoPath string, entry WorkspaceScanEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries()[repoPath] = entry
	return c.save()
}

// GetAll returns all cached entries for this context
func (c *WorkspaceScanCache) GetAll() map[string]WorkspaceScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	own := c.contexts[c.context]
	result := make(map[string]WorkspaceScanEntry, len(own))
	for k, v := range own {
		result[k] = v
	}
	return result
}

// Delete removes one entry from this context and persists to disk
func (c *WorkspaceScanCache) Delete(repoPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.contexts[c.context], repoPath)
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
