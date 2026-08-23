package cache

import (
	"encoding/json"
	"os"
)

// scanCacheVersion marks a file whose entries are grouped by configuration
// context. Version 0 is the shape that predates contexts: a bare key → entry
// map, which is what `Contexts == nil` after a successful unmarshal means.
const scanCacheVersion = 1

// scanCacheFile is the on-disk shape of the workspace scan cache, and of the
// image scan cache as it was written between the arrival of contexts and §3.39.
//
// A workspace path is reached through `workspaces_dir`, which is per context, so
// two contexts holding the same path may legitimately mean different work — one
// flat namespace made them share results. An image key is a local Docker
// reference and answers for the machine, which is why that cache is flat again;
// the image side still reads this shape to fold it back (readImageScanCacheFile).
type scanCacheFile[T any] struct {
	Version  int                     `json:"version"`
	Contexts map[string]map[string]T `json:"contexts"`
}

// readScanCacheFile loads every context's entries, upgrading the file in place
// when it predates contexts. parseScanCacheFile is the same read without the
// upgrade.
//
// The upgrade is written back immediately rather than left until the first Set.
// Deferring it would let every context that opens the file claim the legacy
// entries in turn, so what the user saw would depend on which context happened
// to write first. Writing once, on the first open, makes the owner the context
// that was current at upgrade time and nothing else.
func readScanCacheFile[T any](path, context string) (map[string]map[string]T, error) {
	contexts, legacy, err := parseScanCacheFile[T](path)
	if err != nil {
		return nil, err
	}
	if len(legacy) == 0 {
		return contexts, nil
	}

	contexts[context] = legacy
	if err := writeScanCacheFile(path, contexts); err != nil {
		return nil, err
	}
	return contexts, nil
}

// parseScanCacheFile reads the file and upgrades nothing.
//
// It returns the per-context entries, and *separately* the entries of a legacy
// pre-context file — which belong to no context yet. Deciding whose they are is
// a write, and this function does not make it: that is the whole reason it
// exists beside readScanCacheFile, which does.
//
// The read-only MCP server is what needs the distinction (§3.38). A server
// opened on context X would otherwise claim for X entries nobody attributed to
// it — a read path that writes, and decides. It serves them and leaves the
// decision to the TUI, where somebody is present to see it.
//
// A legacy file is a bare key → entry map. Unmarshalled into the struct it
// leaves Version at 0 and Contexts nil — no field matches — which identifies it
// without having to guess from the data.
//
// A file that cannot be read is empty, not an error: that is a cache which does
// not exist yet.
func parseScanCacheFile[T any](path string) (contexts map[string]map[string]T, legacy map[string]T, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return map[string]map[string]T{}, nil, nil
	}

	var file scanCacheFile[T]
	if err := json.Unmarshal(data, &file); err == nil && file.Contexts != nil {
		return file.Contexts, nil, nil
	}

	var flat map[string]T
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil, nil, err
	}
	if len(flat) == 0 {
		return map[string]map[string]T{}, nil, nil
	}
	return map[string]map[string]T{}, flat, nil
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
