package cache

import (
	"encoding/json"
	"os"
)

// scanCacheVersion marks a file whose entries are grouped by configuration
// context. Version 0 is the shape that predates contexts: a bare key → entry
// map, which is what `Contexts == nil` after a successful unmarshal means.
const scanCacheVersion = 1

// scanCacheFile is the on-disk shape shared by the image and workspace scan
// caches. Both are keyed by context because the configuration is: two contexts
// legitimately point at different workspace roots and different registries, so
// one flat namespace made them share results.
type scanCacheFile[T any] struct {
	Version  int                     `json:"version"`
	Contexts map[string]map[string]T `json:"contexts"`
}

// readScanCacheFile loads every context's entries, upgrading the file in place
// when it predates contexts.
//
// A legacy file is a bare key → entry map. Unmarshalled into the struct it
// leaves Version at 0 and Contexts nil — no field matches — which identifies it
// without having to guess from the data.
//
// The upgrade is written back immediately rather than left until the first Set.
// Deferring it would let every context that opens the file claim the legacy
// entries in turn, so what the user saw would depend on which context happened
// to write first. Writing once, on the first open, makes the owner the context
// that was current at upgrade time and nothing else.
//
// A file that cannot be read is empty, not an error: that is a cache which does
// not exist yet.
func readScanCacheFile[T any](path, context string) (map[string]map[string]T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]map[string]T{}, nil
	}

	var file scanCacheFile[T]
	if err := json.Unmarshal(data, &file); err == nil && file.Contexts != nil {
		return file.Contexts, nil
	}

	var flat map[string]T
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil, err
	}
	if len(flat) == 0 {
		return map[string]map[string]T{}, nil
	}

	contexts := map[string]map[string]T{context: flat}
	if err := writeScanCacheFile(path, contexts); err != nil {
		return nil, err
	}
	return contexts, nil
}

// writeScanCacheFile persists every context, not just the one that changed —
// the caller holds them all, and dropping the others would make a write from
// one context erase the rest.
func writeScanCacheFile[T any](path string, contexts map[string]map[string]T) error {
	data, err := json.MarshalIndent(scanCacheFile[T]{
		Version:  scanCacheVersion,
		Contexts: contexts,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// entriesFor returns a context's map, creating it if this is the first write.
func entriesFor[T any](contexts map[string]map[string]T, context string) map[string]T {
	if entries, ok := contexts[context]; ok {
		return entries
	}
	entries := make(map[string]T)
	contexts[context] = entries
	return entries
}
