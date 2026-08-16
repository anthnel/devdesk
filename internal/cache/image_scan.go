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

// ImageScanCache manages the image scan results cache file.
//
// An instance is bound to one configuration context and only ever reads and
// writes that context's entries, so Get, Set, GetAll and Delete keep working on
// plain image keys. The file underneath holds every context.
type ImageScanCache struct {
	mu       sync.RWMutex
	path     string
	context  string
	contexts map[string]map[string]ImageScanEntry // context → "repo:tag" → entry
}

// NewImageScanCache creates or loads the cache from
// ~/.devdesk/cache/image-scans.json, scoped to a configuration context.
func NewImageScanCache(context string) (*ImageScanCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	return newImageScanCacheAt(filepath.Join(cacheDir, "image-scans.json"), context)
}

// newImageScanCacheAt is NewImageScanCache without the home-directory lookup,
// so tests can point at a temp file.
func newImageScanCacheAt(path, context string) (*ImageScanCache, error) {
	c := &ImageScanCache{
		path:     path,
		context:  context,
		contexts: make(map[string]map[string]ImageScanEntry),
	}
	c.load()
	return c, nil
}

// load reads the cache file from disk
func (c *ImageScanCache) load() {
	contexts, err := readScanCacheFile[ImageScanEntry](c.path, c.context)
	if err != nil {
		log.Printf("ERROR [cache/image_scan] read cache file %s: %v", c.path, err)
		return
	}
	c.contexts = contexts
}

// entries returns this context's map, creating it on first write.
// Must be called with the lock held.
func (c *ImageScanCache) entries() map[string]ImageScanEntry {
	return entriesFor(c.contexts, c.context)
}

// save writes the cache to disk
func (c *ImageScanCache) save() error {
	return writeScanCacheFile(c.path, c.contexts)
}

// Get returns the cached scan entry for an image, or nil if not found
func (c *ImageScanCache) Get(imageKey string) *ImageScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if entry, ok := c.contexts[c.context][imageKey]; ok {
		return &entry
	}
	return nil
}

// Set stores a scan entry and persists to disk
func (c *ImageScanCache) Set(imageKey string, entry ImageScanEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries()[imageKey] = entry
	return c.save()
}

// GetAll returns all cached entries for this context
func (c *ImageScanCache) GetAll() map[string]ImageScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	own := c.contexts[c.context]
	result := make(map[string]ImageScanEntry, len(own))
	for k, v := range own {
		result[k] = v
	}
	return result
}

// Delete removes one entry from this context and persists to disk
func (c *ImageScanCache) Delete(imageKey string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.contexts[c.context], imageKey)
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
