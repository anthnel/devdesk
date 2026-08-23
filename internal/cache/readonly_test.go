package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The MCP server reads these caches while the TUI may be writing them, and
// ~/.devdesk has no lock. So the guarantee is not "writes rarely" but "does not
// write", and these tests check the file on disk rather than the value returned.

func TestReadingAnImageCacheDoesNotWriteIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	// A context-scoped file is the case that writes: opening it folds the
	// contexts together and saves (§3.39).
	writeJSON(t, path, scanCacheFile[ImageScanEntry]{
		Version:  1,
		Contexts: map[string]map[string]ImageScanEntry{"work": {"nginx:latest": {Critical: 3}}},
	})
	before := stamp(t, path)

	entries, err := readImageScanEntriesAt(path)
	if err != nil {
		t.Fatalf("readImageScanEntriesAt: %v", err)
	}

	if entries["nginx:latest"].Critical != 3 {
		t.Errorf("entries = %+v, want the folded entry", entries)
	}
	assertUntouched(t, path, before)
}

func TestReadingAWorkspaceCacheDoesNotWriteIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-scans.json")
	// A pre-context file is the case that writes: opening it attributes the
	// entries to the opening context and saves.
	writeJSON(t, path, map[string]WorkspaceScanEntry{"/home/u/repo": {Critical: 1}})
	before := stamp(t, path)

	entries, err := readWorkspaceScanEntriesAt(path, "work")
	if err != nil {
		t.Fatalf("readWorkspaceScanEntriesAt: %v", err)
	}

	if entries["/home/u/repo"].Critical != 1 {
		t.Errorf("entries = %+v, want the legacy entry", entries)
	}
	assertUntouched(t, path, before)
}

// The read serves a legacy file's entries without claiming them. Reading on one
// context and then another must give the same answer — an attribution would
// make the second read empty.
func TestALegacyFileIsServedToWhicheverContextAsks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-scans.json")
	writeJSON(t, path, map[string]WorkspaceScanEntry{"/home/u/repo": {Critical: 1}})

	work, err := readWorkspaceScanEntriesAt(path, "work")
	if err != nil {
		t.Fatalf("read for work: %v", err)
	}
	perso, err := readWorkspaceScanEntriesAt(path, "perso")
	if err != nil {
		t.Fatalf("read for perso: %v", err)
	}

	if len(work) != 1 || len(perso) != 1 {
		t.Errorf("the legacy entries were claimed by the first reader: work=%v perso=%v", work, perso)
	}
}

// A context that has scanned nothing reads as empty, not as another context's
// entries — the scoping still holds on the read path.
func TestTheWorkspaceReadStaysScopedToItsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace-scans.json")
	writeJSON(t, path, scanCacheFile[WorkspaceScanEntry]{
		Version:  1,
		Contexts: map[string]map[string]WorkspaceScanEntry{"work": {"/home/u/repo": {Critical: 5}}},
	})

	got, err := readWorkspaceScanEntriesAt(path, "perso")
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("context perso reads context work's entries: %v", got)
	}
}

// A cache that does not exist yet is empty, and reading it must not create it —
// that is the MkdirAll the constructors do and this path must not.
func TestReadingACacheThatDoesNotExistCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "image-scans.json")

	entries, err := readImageScanEntriesAt(path)
	if err != nil {
		t.Fatalf("readImageScanEntriesAt: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("entries = %v, want empty", entries)
	}
	if _, err := os.Stat(filepath.Join(dir, "nested")); !os.IsNotExist(err) {
		t.Errorf("the read created the cache directory")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

type fileStamp struct {
	modTime time.Time
	size    int64
}

func stamp(t *testing.T, path string) fileStamp {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fileStamp{modTime: info.ModTime(), size: info.Size()}
}

func assertUntouched(t *testing.T, path string, before fileStamp) {
	t.Helper()
	after := stamp(t, path)
	if !after.modTime.Equal(before.modTime) || after.size != before.size {
		t.Errorf("the read wrote to %s: mtime %v → %v, size %d → %d",
			filepath.Base(path), before.modTime, after.modTime, before.size, after.size)
	}
}

// The read-only path names a result file itself rather than going through the
// writing helper, so the two spellings of "SHA256 of the target" have to agree.
// If they drift, every read comes back "not found" for results that are there.
func TestTheReadOnlyResultPathMatchesTheWrittenOne(t *testing.T) {
	for _, target := range []string{"nginx:latest", "/home/u/repo", "registry.example.com/team/app:1.0"} {
		written, err := imageResultFilePath(target)
		if err != nil {
			t.Fatalf("imageResultFilePath(%q): %v", target, err)
		}
		if got, want := hashedResultName(target), filepath.Base(written); got != want {
			t.Errorf("hashedResultName(%q) = %s, the writer uses %s", target, got, want)
		}

		written, err = resultFilePath(target)
		if err != nil {
			t.Fatalf("resultFilePath(%q): %v", target, err)
		}
		if got, want := hashedResultName(target), filepath.Base(written); got != want {
			t.Errorf("hashedResultName(%q) = %s, the workspace writer uses %s", target, got, want)
		}
	}
}
