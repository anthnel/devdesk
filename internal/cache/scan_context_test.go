package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The scan caches are per context because the configuration is. Two contexts
// legitimately point at different workspace roots and different registries, so
// one flat namespace made them share results.
//
// Nothing showed it while the caches were only ever queried — you ask about the
// image in front of you. The inventory view lists everything the cache holds,
// which is what turns this into something the user can see.

func TestImageEntriesAreScopedToTheirContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")

	work := openImageCache(t, path, "work")
	if err := work.Set("nginx:latest", ImageScanEntry{Critical: 3}); err != nil {
		t.Fatalf("Set in context work: %v", err)
	}

	perso := openImageCache(t, path, "perso")
	if got := perso.Get("nginx:latest"); got != nil {
		t.Errorf("context perso can see context work's entry: %+v", got)
	}
	if got := perso.GetAll(); len(got) != 0 {
		t.Errorf("GetAll in context perso returned %d entries, want none", len(got))
	}

	// And the write did not cost the other context its own view of the key.
	if got := openImageCache(t, path, "work").Get("nginx:latest"); got == nil || got.Critical != 3 {
		t.Errorf("context work lost its own entry: %+v", got)
	}
}

func TestWorkspaceEntriesAreScopedToTheirContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-scans.json")

	work := openWorkspaceCache(t, path, "work")
	if err := work.Set("/home/u/repo", WorkspaceScanEntry{Critical: 5}); err != nil {
		t.Fatalf("Set in context work: %v", err)
	}

	perso := openWorkspaceCache(t, path, "perso")
	if got := perso.Get("/home/u/repo"); got != nil {
		t.Errorf("context perso can see context work's entry: %+v", got)
	}
	if got := openWorkspaceCache(t, path, "work").Get("/home/u/repo"); got == nil || got.Critical != 5 {
		t.Errorf("context work lost its own entry: %+v", got)
	}
}

// A cache written before contexts existed is a bare key → entry map. It belongs
// to whichever context is current when it is first opened: that is the context
// the scans were run under, since there was only one namespace.
func TestAFlatImageCacheMigratesIntoTheOpeningContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	writeJSON(t, path, map[string]ImageScanEntry{
		"nginx:latest": {Critical: 2, High: 7, ScannedAt: time.Now().UTC().Truncate(time.Second)},
	})

	c := openImageCache(t, path, "work")

	got := c.Get("nginx:latest")
	if got == nil {
		t.Fatal("the legacy entry was dropped instead of migrated")
	}
	if got.Critical != 2 || got.High != 7 {
		t.Errorf("migrated entry = %+v, want Critical 2 / High 7", got)
	}
	if other := openImageCache(t, path, "perso").Get("nginx:latest"); other != nil {
		t.Errorf("the migrated entry landed in every context, not just the opening one: %+v", other)
	}
}

func TestAFlatWorkspaceCacheMigratesIntoTheOpeningContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-scans.json")
	writeJSON(t, path, map[string]WorkspaceScanEntry{
		"/home/u/repo": {Critical: 1, Sensitive: true},
	})

	c := openWorkspaceCache(t, path, "work")

	got := c.Get("/home/u/repo")
	if got == nil {
		t.Fatal("the legacy entry was dropped instead of migrated")
	}
	if got.Critical != 1 || !got.Sensitive {
		t.Errorf("migrated entry = %+v, want Critical 1 / Sensitive true", got)
	}
}

// The migration must not run twice. Once a context has been written, the file
// is in the new shape and a second open must read it as such — re-running the
// legacy branch would fold every context back into the one being opened.
func TestOpeningAMigratedFileDoesNotMigrateAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	writeJSON(t, path, map[string]ImageScanEntry{"nginx:latest": {Critical: 2}})

	// First open migrates into "work" and writes the new shape.
	first := openImageCache(t, path, "work")
	if err := first.Set("redis:7", ImageScanEntry{Critical: 1}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A different context opens the same file and writes its own entry.
	perso := openImageCache(t, path, "perso")
	if err := perso.Set("alpine:3", ImageScanEntry{Low: 4}); err != nil {
		t.Fatalf("Set in context perso: %v", err)
	}

	// Neither context may have acquired the other's keys.
	if got := openImageCache(t, path, "work").GetAll(); len(got) != 2 {
		t.Errorf("context work holds %d entries (%v), want 2", len(got), keysOf(got))
	}
	if got := openImageCache(t, path, "perso").GetAll(); len(got) != 1 {
		t.Errorf("context perso holds %d entries (%v), want 1", len(got), keysOf(got))
	}
}

// Deleting is scoped too: purging a context's cache before a rescan (Rule 126)
// must not reach into another context's entries.
func TestDeleteOnlyTouchesTheOwnContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")

	work := openImageCache(t, path, "work")
	_ = work.Set("shared:tag", ImageScanEntry{Critical: 1})
	perso := openImageCache(t, path, "perso")
	_ = perso.Set("shared:tag", ImageScanEntry{Critical: 9})

	if err := perso.Delete("shared:tag"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if got := openImageCache(t, path, "work").Get("shared:tag"); got == nil {
		t.Error("deleting in context perso removed context work's entry")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func openImageCache(t *testing.T, path, context string) *ImageScanCache {
	t.Helper()
	c, err := newImageScanCacheAt(path, context)
	if err != nil {
		t.Fatalf("open image cache for context %q: %v", context, err)
	}
	return c
}

func openWorkspaceCache(t *testing.T, path, context string) *WorkspaceScanCache {
	t.Helper()
	c, err := newWorkspaceScanCacheAt(path, context)
	if err != nil {
		t.Fatalf("open workspace cache for context %q: %v", context, err)
	}
	return c
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func keysOf[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
