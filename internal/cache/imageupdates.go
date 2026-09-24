package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ImageUpdateEntry is what a registry said about one image reference (§3.88):
// the digest its tag points to now, and a newer patch tag on the same line if
// one exists.
//
// Only the registry's side is kept. Whether an update is available is decided
// against the local image when it is shown, so a pull clears the arrow at once,
// without asking the registry again.
type ImageUpdateEntry struct {
	Digest     string    `json:"digest,omitempty"`
	NewerPatch string    `json:"newer_patch,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
	// Failed is a check the registry did not answer — unreachable, unknown
	// repository, denied. It is kept so the reference is not asked again on
	// every refresh, and it shows nothing.
	Failed bool `json:"failed,omitempty"`
}

const imageUpdateCacheVersion = 1

type imageUpdateCacheFile struct {
	Version int                         `json:"version"`
	Entries map[string]ImageUpdateEntry `json:"entries"`
}

// imageUpdateMu serializes every read-modify-write of the file: three views
// check references, each on its own Cmd.
var imageUpdateMu sync.Mutex

func imageUpdateCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".devdesk", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "image-updates.json"), nil
}

// ReadImageUpdates returns every stored check, keyed by image reference. A file
// that does not exist yet is an empty cache, not an error.
func ReadImageUpdates() (map[string]ImageUpdateEntry, error) {
	path, err := imageUpdateCachePath()
	if err != nil {
		return nil, err
	}
	imageUpdateMu.Lock()
	defer imageUpdateMu.Unlock()
	return readImageUpdateFile(path)
}

// SetImageUpdates stores several checks at once, keeping the others.
func SetImageUpdates(entries map[string]ImageUpdateEntry) error {
	path, err := imageUpdateCachePath()
	if err != nil {
		return err
	}
	return setImageUpdatesAt(path, entries)
}

func setImageUpdatesAt(path string, add map[string]ImageUpdateEntry) error {
	imageUpdateMu.Lock()
	defer imageUpdateMu.Unlock()
	entries, err := readImageUpdateFile(path)
	if err != nil {
		return err
	}
	for ref, e := range add {
		entries[ref] = e
	}
	data, err := json.MarshalIndent(imageUpdateCacheFile{Version: imageUpdateCacheVersion, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func readImageUpdateFile(path string) (map[string]ImageUpdateEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]ImageUpdateEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file imageUpdateCacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if file.Entries == nil {
		file.Entries = map[string]ImageUpdateEntry{}
	}
	return file.Entries, nil
}
