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

// RemediationEntry is what a scan of a candidate base image found: the CVE
// counts, and when.
//
// Only the counts are kept. A candidate is measured to be compared with the
// image it would replace, and what is compared is how many CRITICAL and HIGH
// findings each has — the findings themselves describe an image the user does
// not own.
type RemediationEntry struct {
	Critical  int       `json:"critical"`
	High      int       `json:"high"`
	Medium    int       `json:"medium"`
	Low       int       `json:"low"`
	ScannedAt time.Time `json:"scanned_at"`
}

// remediationCacheVersion is the shape of the file, for the day it changes.
const remediationCacheVersion = 1

type remediationCacheFile struct {
	Version int                         `json:"version"`
	Entries map[string]RemediationEntry `json:"entries"`
}

// remediationMu serializes every read-modify-write of the file. Scans of
// several candidates finish concurrently, each on its own Cmd, and each would
// otherwise read the file, add its entry and write it back over the others'.
var remediationMu sync.Mutex

// The candidates' results live in a file of their own, not in the image scan
// cache, for a reason measured in §3.2: the inventory only lists an image the
// engine still has (cache.ImageGone), and a candidate is read from its registry
// and never pulled. Its entry would sit in the shared file, invisible to the
// inventory and counted by whatever reads the cache without that filter.
func remediationCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".devdesk", "cache")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "remediation-scans.json"), nil
}

// ReadRemediationResults returns every stored candidate result, keyed by image
// reference. A file that does not exist yet is an empty cache, not an error.
func ReadRemediationResults() (map[string]RemediationEntry, error) {
	path, err := remediationCachePath()
	if err != nil {
		return nil, err
	}
	remediationMu.Lock()
	defer remediationMu.Unlock()
	return readRemediationFile(path)
}

// SetRemediationResult stores one candidate's result.
func SetRemediationResult(ref string, entry RemediationEntry) error {
	path, err := remediationCachePath()
	if err != nil {
		return err
	}
	return setRemediationResultAt(path, ref, entry)
}

func setRemediationResultAt(path, ref string, entry RemediationEntry) error {
	remediationMu.Lock()
	defer remediationMu.Unlock()
	entries, err := readRemediationFile(path)
	if err != nil {
		return err
	}
	entries[ref] = entry
	data, err := json.MarshalIndent(remediationCacheFile{Version: remediationCacheVersion, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func readRemediationFile(path string) (map[string]RemediationEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]RemediationEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file remediationCacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if file.Entries == nil {
		file.Entries = map[string]RemediationEntry{}
	}
	return file.Entries, nil
}

// writeFileAtomic writes through a temporary file in the same directory and
// renames it over the target, so a reader never sees half a file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
