package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anthnel/devdesk/internal/scan"
)

// ImageScanEntry holds cached CVE counts for a scanned image
type ImageScanEntry struct {
	ImageID  string `json:"image_id"`
	Critical int    `json:"critical"`
	High     int    `json:"high"`
	Medium   int    `json:"medium"`
	Low      int    `json:"low"`
	// Sensitive is the secret verdict, and it is a pointer because it has three
	// values: absent quand personne n'a cherché, false quand une étape a cherché
	// sans rien trouver, true sinon. C'est `scan.Result.SecretVerdict()` qui
	// l'écrit.
	//
	// Une image n'avait aucun champ de secrets jusqu'ici, donc **toute entrée
	// déjà sur le disque décode à nil** — ce qui est la vérité : elle a été
	// écrite par un scan d'image qui n'avait pas d'étape secrets du tout.
	Sensitive *bool     `json:"sensitive,omitempty"`
	ScannedAt time.Time `json:"scanned_at"`
}

// imageScanCacheVersion marks a file whose entries are one flat map again.
// Version 1 is the shape that grouped them by configuration context, version 0
// the one that predates contexts — and version 0 is already this shape, which is
// why reading it has nothing to do (§3.39).
const imageScanCacheVersion = 2

// imageScanCacheFile is the on-disk shape of the image scan index.
//
// It carries no context, and that is the whole of §3.39: the key is a local
// Docker reference, and `docker image ls` answers for the machine rather than
// for a configuration. Two contexts looking at `nginx:1.25` are looking at the
// same bytes, so scoping the index made a context switch drop counts for images
// that had not moved — while the full results beside it, content-addressed by
// image name, were never scoped at all.
//
// The workspace cache stays scoped, and the asymmetry is the point: a workspace
// path is reached through `workspaces_dir`, which *is* per context, so two
// contexts holding the same path may legitimately mean different work.
type imageScanCacheFile struct {
	Version int                       `json:"version"`
	Entries map[string]ImageScanEntry `json:"entries"`
}

// ImageScanCache manages the image scan results cache file.
//
// It is not bound to a configuration context: see imageScanCacheFile.
type ImageScanCache struct {
	mu      sync.RWMutex
	path    string
	entries map[string]ImageScanEntry // "repo:tag" → entry
}

// NewImageScanCache creates or loads the cache from
// ~/.devdesk/cache/image-scans.json.
func NewImageScanCache() (*ImageScanCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	return newImageScanCacheAt(filepath.Join(cacheDir, "image-scans.json"))
}

// newImageScanCacheAt is NewImageScanCache without the home-directory lookup,
// so tests can point at a temp file.
func newImageScanCacheAt(path string) (*ImageScanCache, error) {
	c := &ImageScanCache{
		path:    path,
		entries: make(map[string]ImageScanEntry),
	}
	c.load()
	return c, nil
}

// load reads the cache file from disk
func (c *ImageScanCache) load() {
	entries, folded, err := readImageScanCacheFile(c.path)
	if err != nil {
		log.Printf("ERROR [cache/image_scan] read cache file %s: %v", c.path, err)
		return
	}
	c.entries = entries
	if !folded {
		return
	}
	// Written back on the first open rather than deferred to the first Set, for
	// the reason the context migration was: a file left in the old shape is
	// folded again on every open, so what it holds would depend on when it was
	// last read.
	if err := c.save(); err != nil {
		log.Printf("ERROR [cache/image_scan] write folded cache file %s: %v", c.path, err)
	}
}

// readImageScanCacheFile loads the index, folding a context-scoped file back
// into one map. It reports whether it had to.
//
// A file that cannot be read is empty, not an error: that is a cache which does
// not exist yet.
func readImageScanCacheFile(path string) (entries map[string]ImageScanEntry, folded bool, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return map[string]ImageScanEntry{}, false, nil
	}

	var current imageScanCacheFile
	if err := json.Unmarshal(data, &current); err == nil && current.Entries != nil {
		return current.Entries, false, nil
	}

	var scoped scanCacheFile[ImageScanEntry]
	if err := json.Unmarshal(data, &scoped); err == nil && scoped.Contexts != nil {
		return foldContexts(scoped.Contexts), true, nil
	}

	// The oldest shape is a bare key → entry map, which is the shape the file
	// has again — so there is nothing to fold, only a version to stamp on.
	var flat map[string]ImageScanEntry
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil, false, err
	}
	if flat == nil {
		flat = map[string]ImageScanEntry{}
	}
	return flat, true, nil
}

// foldContexts merges every context's entries into one map.
//
// Two contexts holding the same image is the ordinary case — it is one image,
// and both of them saw it — so the collision has to be settled rather than
// reported: the most recent scan wins. The image did not change between them,
// but the vulnerability database did.
func foldContexts(contexts map[string]map[string]ImageScanEntry) map[string]ImageScanEntry {
	entries := make(map[string]ImageScanEntry)
	for _, own := range contexts {
		for key, entry := range own {
			if kept, ok := entries[key]; ok && kept.ScannedAt.After(entry.ScannedAt) {
				continue
			}
			entries[key] = entry
		}
	}
	return entries
}

// save writes the cache to disk
func (c *ImageScanCache) save() error {
	data, err := json.MarshalIndent(imageScanCacheFile{
		Version: imageScanCacheVersion,
		Entries: c.entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// Get returns the cached scan entry for an image, or nil if not found
func (c *ImageScanCache) Get(imageKey string) *ImageScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.entries[imageKey]; ok {
		return &entry
	}
	return nil
}

// Set stores a scan entry and persists to disk
func (c *ImageScanCache) Set(imageKey string, entry ImageScanEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[imageKey] = entry
	return c.save()
}

// GetAll returns every cached entry
func (c *ImageScanCache) GetAll() map[string]ImageScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]ImageScanEntry, len(c.entries))
	for k, v := range c.entries {
		result[k] = v
	}
	return result
}

// Delete removes one entry and persists to disk
func (c *ImageScanCache) Delete(imageKey string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, imageKey)
	return c.save()
}

// Reload re-reads the cache from disk
func (c *ImageScanCache) Reload() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
}

// imageScanResultDir returns the directory for full image scan results, creating it if needed
func imageScanResultDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(homeDir, ".devdesk", "cache", "image-results")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// imageResultFilePath returns the JSON file path for the full scan result of an image.
// Uses SHA256 of the image name to avoid invalid filename characters.
func imageResultFilePath(imageName string) (string, error) {
	dir, err := imageScanResultDir()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(imageName))
	filename := fmt.Sprintf("%x.json", hash)
	return filepath.Join(dir, filename), nil
}

// SaveImageScanResult persists a full scan result to disk for later retrieval.
func SaveImageScanResult(imageName string, result *scan.Result) error {
	filePath, err := imageResultFilePath(imageName)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0600)
}

// LoadImageScanResult loads a full scan result from disk
func LoadImageScanResult(imageName string) (*scan.Result, error) {
	filePath, err := imageResultFilePath(imageName)
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
