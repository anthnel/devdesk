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
	ImageID   string    `json:"image_id"`
	Critical  int       `json:"critical"`
	High      int       `json:"high"`
	Medium    int       `json:"medium"`
	Low       int       `json:"low"`
	ScannedAt time.Time `json:"scanned_at"`
}

// ImageScanCache manages the image scan results cache file
type ImageScanCache struct {
	mu      sync.RWMutex
	path    string
	entries map[string]ImageScanEntry // keyed by "repo:tag"
}

// NewImageScanCache creates or loads the cache from ~/.devdesk/cache/image-scans.json
func NewImageScanCache() (*ImageScanCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	c := &ImageScanCache{
		path:    filepath.Join(cacheDir, "image-scans.json"),
		entries: make(map[string]ImageScanEntry),
	}

	c.load()
	return c, nil
}

// load reads the cache file from disk
func (c *ImageScanCache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		log.Printf("ERROR [cache/image_scan] unmarshal cache file %s: %v", c.path, err)
	}
}

// save writes the cache to disk
func (c *ImageScanCache) save() error {
	data, err := json.MarshalIndent(c.entries, "", "  ")
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

// GetAll returns all cached entries
func (c *ImageScanCache) GetAll() map[string]ImageScanEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]ImageScanEntry, len(c.entries))
	for k, v := range c.entries {
		result[k] = v
	}
	return result
}

// Delete removes one entry from the cache and persists to disk
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
