package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The **workspace** cache is per context because the configuration is: two
// contexts legitimately point at different workspace roots, so one flat
// namespace made them share results for paths that are not the same work.
//
// The **image** cache is not, and scoping it was the mistake (§3.39). Its key is
// a local Docker reference, and `docker image ls` answers for the machine rather
// than for a context — so switching context dropped the counts for images that
// had not moved. The full results were never scoped at all: they are
// content-addressed by image name, with no context anywhere, so the index and
// the blobs beside it disagreed.

func TestImageEntriesAreSharedBetweenContexts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")

	work := openImageCache(t, path)
	if err := work.Set("nginx:latest", ImageScanEntry{Critical: 3}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// The same file, opened again — which is what a context switch does.
	perso := openImageCache(t, path)
	got := perso.Get("nginx:latest")
	if got == nil {
		t.Fatal("the scan was lost across a context switch")
	}
	if got.Critical != 3 {
		t.Errorf("entry = %+v, want Critical 3", got)
	}
	if all := perso.GetAll(); len(all) != 1 {
		t.Errorf("GetAll returned %d entries (%v), want the one", len(all), keysOf(all))
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

// The oldest image cache is a bare key → entry map, which is the shape the cache
// has again — so it is read as it stands, with nothing to fold.
func TestAFlatImageCacheIsReadAsItStands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	writeJSON(t, path, map[string]ImageScanEntry{
		"nginx:latest": {Critical: 2, High: 7, ScannedAt: time.Now().UTC().Truncate(time.Second)},
	})

	got := openImageCache(t, path).Get("nginx:latest")

	if got == nil {
		t.Fatal("the legacy entry was dropped")
	}
	if got.Critical != 2 || got.High != 7 {
		t.Errorf("entry = %+v, want Critical 2 / High 7", got)
	}
}

// A file written while the image cache was scoped holds one map per context. The
// contexts are folded back into one, because the key was never context-specific
// in the first place.
func TestAScopedImageCacheIsFoldedBackIntoOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	writeJSON(t, path, scanCacheFile[ImageScanEntry]{
		Version: 1,
		Contexts: map[string]map[string]ImageScanEntry{
			"work":  {"nginx:latest": {Critical: 3}, "redis:7": {Low: 1}},
			"perso": {"alpine:3": {Medium: 2}},
		},
	})

	c := openImageCache(t, path)

	if got := c.GetAll(); len(got) != 3 {
		t.Errorf("the fold kept %d entries (%v), want all three", len(got), keysOf(got))
	}
	for _, key := range []string{"nginx:latest", "redis:7", "alpine:3"} {
		if c.Get(key) == nil {
			t.Errorf("%s was dropped by the fold", key)
		}
	}
}

// Two contexts having scanned the same image is the ordinary case — it is one
// image, and both of them saw it. The most recent scan wins: the image did not
// change, but the vulnerability database did.
func TestTheNewerScanWinsWhenTwoContextsHoldTheSameImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	older := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	writeJSON(t, path, scanCacheFile[ImageScanEntry]{
		Version: 1,
		Contexts: map[string]map[string]ImageScanEntry{
			"work":  {"nginx:latest": {Critical: 3, ScannedAt: newer}},
			"perso": {"nginx:latest": {Critical: 9, ScannedAt: older}},
		},
	})

	got := openImageCache(t, path).Get("nginx:latest")

	if got == nil {
		t.Fatal("the entry was dropped by the fold")
	}
	if got.Critical != 3 || !got.ScannedAt.Equal(newer) {
		t.Errorf("entry = %+v, want the more recent scan (Critical 3)", got)
	}
}

// The fold is written back on the first open rather than deferred to the first
// Set, for the reason the context migration was: a file left in the old shape is
// folded again on every open, so what it holds depends on when it was last read.
func TestTheFoldIsWrittenBackOnTheFirstOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	writeJSON(t, path, scanCacheFile[ImageScanEntry]{
		Version:  1,
		Contexts: map[string]map[string]ImageScanEntry{"work": {"nginx:latest": {Critical: 3}}},
	})

	openImageCache(t, path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var file imageScanCacheFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if file.Version != imageScanCacheVersion {
		t.Errorf("version = %d, want %d", file.Version, imageScanCacheVersion)
	}
	if _, ok := file.Entries["nginx:latest"]; !ok {
		t.Errorf("the folded file holds %v, want the entry at the top level", keysOf(file.Entries))
	}
}

func TestAFlatWorkspaceCacheMigratesIntoTheOpeningContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-scans.json")
	writeJSON(t, path, map[string]WorkspaceScanEntry{
		"/home/u/repo": {Critical: 1, Sensitive: secretsFound()},
	})

	c := openWorkspaceCache(t, path, "work")

	got := c.Get("/home/u/repo")
	if got == nil {
		t.Fatal("the legacy entry was dropped instead of migrated")
	}
	if got.Critical != 1 || got.Sensitive == nil || !*got.Sensitive {
		t.Errorf("migrated entry = %+v, want Critical 1 / Sensitive true", got)
	}
}

// Writes accumulate rather than replacing the file: open, write, open again,
// write again, and both entries are there. Running the fold twice is what could
// break it.
func TestSuccessiveWritesAccumulate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	writeJSON(t, path, map[string]ImageScanEntry{"nginx:latest": {Critical: 2}})

	first := openImageCache(t, path)
	if err := first.Set("redis:7", ImageScanEntry{Critical: 1}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	second := openImageCache(t, path)
	if err := second.Set("alpine:3", ImageScanEntry{Low: 4}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got := openImageCache(t, path).GetAll(); len(got) != 3 {
		t.Errorf("the cache holds %d entries (%v), want all three", len(got), keysOf(got))
	}
}

// Deleting is what a purge does before a rescan (Rule 126). With one namespace
// there is nothing to scope it to, and it removes the entry outright.
func TestDeleteRemovesTheEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")

	c := openImageCache(t, path)
	_ = c.Set("shared:tag", ImageScanEntry{Critical: 1})

	if err := c.Delete("shared:tag"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if got := openImageCache(t, path).Get("shared:tag"); got != nil {
		t.Errorf("the entry survived the delete: %+v", got)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func openImageCache(t *testing.T, path string) *ImageScanCache {
	t.Helper()
	c, err := newImageScanCacheAt(path)
	if err != nil {
		t.Fatalf("open image cache: %v", err)
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
