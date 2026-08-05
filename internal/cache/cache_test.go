package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/scan"
)

// testContext is the context these caches are opened under. Scoping is covered
// in scan_context_test.go; every test here is about one context's behaviour.
const testContext = "default"

// newTestImageCache creates an ImageScanCache backed by a temp dir.
func newTestImageCache(t *testing.T) *ImageScanCache {
	t.Helper()
	return openImageCache(t, filepath.Join(t.TempDir(), "image-scans.json"), testContext)
}

// newTestWorkspaceCache creates a WorkspaceScanCache backed by a temp dir.
func newTestWorkspaceCache(t *testing.T) *WorkspaceScanCache {
	t.Helper()
	return openWorkspaceCache(t, filepath.Join(t.TempDir(), "workspace-scans.json"), testContext)
}

// ── ImageScanCache ──────────────────────────────────────────────────────────

func TestImageCache_GetMiss(t *testing.T) {
	c := newTestImageCache(t)
	if got := c.Get("nginx:latest"); got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}
}

func TestImageCache_SetAndGet(t *testing.T) {
	c := newTestImageCache(t)
	entry := ImageScanEntry{
		ImageID:   "nginx:latest",
		Critical:  2,
		High:      5,
		Medium:    3,
		Low:       1,
		ScannedAt: time.Now(),
	}
	if err := c.Set("nginx:latest", entry); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	got := c.Get("nginx:latest")
	if got == nil {
		t.Fatal("Get() returned nil after Set()")
	}
	if got.Critical != 2 || got.High != 5 {
		t.Errorf("unexpected entry values: %+v", got)
	}
}

func TestImageCache_Overwrite(t *testing.T) {
	c := newTestImageCache(t)
	if err := c.Set("img:v1", ImageScanEntry{Critical: 1}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}
	if err := c.Set("img:v1", ImageScanEntry{Critical: 9}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	got := c.Get("img:v1")
	if got == nil || got.Critical != 9 {
		t.Errorf("expected overwritten value 9, got %+v", got)
	}
}

func TestImageCache_GetAll_Empty(t *testing.T) {
	c := newTestImageCache(t)
	all := c.GetAll()
	if len(all) != 0 {
		t.Errorf("expected empty map, got %d entries", len(all))
	}
}

func TestImageCache_GetAll_Multiple(t *testing.T) {
	c := newTestImageCache(t)
	if err := c.Set("a:1", ImageScanEntry{Critical: 1}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}
	if err := c.Set("b:2", ImageScanEntry{Critical: 2}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	all := c.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}
	// Verify copy semantics — mutating result does not affect cache
	all["a:1"] = ImageScanEntry{Critical: 99}
	if got := c.Get("a:1"); got == nil || got.Critical != 1 {
		t.Error("GetAll() should return a copy, not a reference")
	}
}

func TestImageCache_Delete(t *testing.T) {
	c := newTestImageCache(t)
	if err := c.Set("x:1", ImageScanEntry{Critical: 3}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	if err := c.Delete("x:1"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if got := c.Get("x:1"); got != nil {
		t.Error("expected nil after Delete()")
	}
}

func TestImageCache_DeleteNonExistent(t *testing.T) {
	c := newTestImageCache(t)
	// Deleting a key that doesn't exist should not error
	if err := c.Delete("ghost:image"); err != nil {
		t.Errorf("Delete() on missing key returned error: %v", err)
	}
}

func TestImageCache_Persist(t *testing.T) {
	c := newTestImageCache(t)
	if err := c.Set("persist:test", ImageScanEntry{High: 7}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	// Create a new cache pointing at the same file
	c2 := openImageCache(t, c.path, testContext)

	got := c2.Get("persist:test")
	if got == nil || got.High != 7 {
		t.Errorf("expected persisted entry with High=7, got %+v", got)
	}
}

func TestImageCache_Reload(t *testing.T) {
	c := newTestImageCache(t)
	if err := c.Set("before:reload", ImageScanEntry{Low: 4}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	// Simulate external modification: write directly to the file
	c2 := openImageCache(t, c.path, testContext)
	if err := c2.Set("after:reload", ImageScanEntry{Medium: 11}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	// Reload the first cache from the updated file
	c.Reload()
	if got := c.Get("after:reload"); got == nil || got.Medium != 11 {
		t.Error("Reload() did not pick up external changes")
	}
}

func TestImageCache_LoadMissingFile(t *testing.T) {
	// Use a path inside a temp dir that does not exist yet — fully isolated.
	c := newTestImageCache(t)
	// path points to an unused file in the temp dir; it was never written to.
	// Reload() (which calls load() under the lock) should silently succeed.
	c.Reload()
	if len(c.GetAll()) != 0 {
		t.Errorf("expected empty entries after loading missing file, got %d", len(c.GetAll()))
	}
}

// ── SaveImageScanResult / LoadImageScanResult ───────────────────────────────

func TestSaveAndLoadImageScanResult(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	result := &scan.Result{
		Target: "nginx:latest",
	}

	if err := SaveImageScanResult("nginx:latest", result); err != nil {
		t.Fatalf("SaveImageScanResult() error: %v", err)
	}

	loaded, err := LoadImageScanResult("nginx:latest")
	if err != nil {
		t.Fatalf("LoadImageScanResult() error: %v", err)
	}
	if loaded.Target != "nginx:latest" {
		t.Errorf("expected target 'nginx:latest', got %q", loaded.Target)
	}
}

func TestLoadImageScanResult_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := LoadImageScanResult("does-not-exist:latest")
	if err == nil {
		t.Error("expected error for missing scan result file, got nil")
	}
}

func TestSaveImageScanResult_DifferentNames_DifferentFiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	r1 := &scan.Result{Target: "image-a:1"}
	r2 := &scan.Result{Target: "image-b:2"}

	if err := SaveImageScanResult("image-a:1", r1); err != nil {
		t.Fatalf("SaveImageScanResult(a) error: %v", err)
	}
	if err := SaveImageScanResult("image-b:2", r2); err != nil {
		t.Fatalf("SaveImageScanResult(b) error: %v", err)
	}

	la, err := LoadImageScanResult("image-a:1")
	if err != nil || la.Target != "image-a:1" {
		t.Errorf("unexpected result for a: %+v err=%v", la, err)
	}
	lb, err := LoadImageScanResult("image-b:2")
	if err != nil || lb.Target != "image-b:2" {
		t.Errorf("unexpected result for b: %+v err=%v", lb, err)
	}
}

func TestImageResultDir_Created(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir, err := imageScanResultDir()
	if err != nil {
		t.Fatalf("imageScanResultDir() error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Errorf("expected directory to be created at %s", dir)
	}
}

// ── WorkspaceScanCache ──────────────────────────────────────────────────────

func TestWorkspaceCache_GetMiss(t *testing.T) {
	c := newTestWorkspaceCache(t)
	if got := c.Get("/nonexistent/repo"); got != nil {
		t.Errorf("expected nil for missing key, got %+v", got)
	}
}

func TestWorkspaceCache_SetAndGet(t *testing.T) {
	c := newTestWorkspaceCache(t)
	entry := WorkspaceScanEntry{
		RepoPath:  "/home/user/project",
		Critical:  1,
		Sensitive: true,
		ScannedAt: time.Now(),
	}
	if err := c.Set("/home/user/project", entry); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	got := c.Get("/home/user/project")
	if got == nil {
		t.Fatal("Get() returned nil after Set()")
	}
	if !got.Sensitive || got.Critical != 1 {
		t.Errorf("unexpected entry values: %+v", got)
	}
}

func TestWorkspaceCache_GetAll_Empty(t *testing.T) {
	c := newTestWorkspaceCache(t)
	all := c.GetAll()
	if len(all) != 0 {
		t.Errorf("expected empty map, got %d entries", len(all))
	}
}

func TestWorkspaceCache_GetAll_Multiple(t *testing.T) {
	c := newTestWorkspaceCache(t)
	if err := c.Set("/repo/a", WorkspaceScanEntry{High: 1}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}
	if err := c.Set("/repo/b", WorkspaceScanEntry{High: 2}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	all := c.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}
}

func TestWorkspaceCache_Delete(t *testing.T) {
	c := newTestWorkspaceCache(t)
	if err := c.Set("/repo/del", WorkspaceScanEntry{Medium: 5}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	if err := c.Delete("/repo/del"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if got := c.Get("/repo/del"); got != nil {
		t.Error("expected nil after Delete()")
	}
}

func TestWorkspaceCache_DeleteNonExistent(t *testing.T) {
	c := newTestWorkspaceCache(t)
	if err := c.Delete("/repo/ghost"); err != nil {
		t.Errorf("Delete() on missing key error: %v", err)
	}
}

func TestWorkspaceCache_Persist(t *testing.T) {
	c := newTestWorkspaceCache(t)
	if err := c.Set("/repo/persist", WorkspaceScanEntry{Low: 3}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	c2 := openWorkspaceCache(t, c.path, testContext)

	got := c2.Get("/repo/persist")
	if got == nil || got.Low != 3 {
		t.Errorf("expected persisted entry Low=3, got %+v", got)
	}
}

func TestWorkspaceCache_Reload(t *testing.T) {
	c := newTestWorkspaceCache(t)
	if err := c.Set("/repo/old", WorkspaceScanEntry{Critical: 0}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	c2 := openWorkspaceCache(t, c.path, testContext)
	if err := c2.Set("/repo/new", WorkspaceScanEntry{Critical: 2}); err != nil {
		t.Fatalf("Set() setup error: %v", err)
	}

	c.Reload()
	if got := c.Get("/repo/new"); got == nil || got.Critical != 2 {
		t.Error("Reload() did not pick up external changes")
	}
}

// ── SaveWorkspaceScanResult / LoadWorkspaceScanResult / Delete ──────────────

func TestSaveAndLoadWorkspaceScanResult(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	result := &scan.Result{Target: "/home/user/repo"}

	if err := SaveWorkspaceScanResult("/home/user/repo", result); err != nil {
		t.Fatalf("SaveWorkspaceScanResult() error: %v", err)
	}

	loaded, err := LoadWorkspaceScanResult("/home/user/repo")
	if err != nil {
		t.Fatalf("LoadWorkspaceScanResult() error: %v", err)
	}
	if loaded.Target != "/home/user/repo" {
		t.Errorf("expected target '/home/user/repo', got %q", loaded.Target)
	}
}

func TestLoadWorkspaceScanResult_Missing(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := LoadWorkspaceScanResult("/nonexistent/path")
	if err == nil {
		t.Error("expected error for missing workspace scan result, got nil")
	}
}

func TestDeleteWorkspaceScanResult(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	result := &scan.Result{Target: "/repo/delete-me"}
	if err := SaveWorkspaceScanResult("/repo/delete-me", result); err != nil {
		t.Fatalf("SaveWorkspaceScanResult() error: %v", err)
	}

	if err := DeleteWorkspaceScanResult("/repo/delete-me"); err != nil {
		t.Fatalf("DeleteWorkspaceScanResult() error: %v", err)
	}

	// Loading should now fail
	if _, err := LoadWorkspaceScanResult("/repo/delete-me"); err == nil {
		t.Error("expected error after deletion, got nil")
	}
}

func TestDeleteWorkspaceScanResult_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Should not error for a file that was never created
	if err := DeleteWorkspaceScanResult("/repo/never-existed"); err != nil {
		t.Errorf("DeleteWorkspaceScanResult() on missing file error: %v", err)
	}
}

func TestWorkspaceResultDir_Created(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir, err := workspaceScanResultDir()
	if err != nil {
		t.Fatalf("workspaceScanResultDir() error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Errorf("expected directory to be created at %s", dir)
	}
}
