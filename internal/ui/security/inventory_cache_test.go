package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// These are the round trips through the real cache files: TestMain points HOME
// at a temporary directory, so the loaders and the writers can be run for what
// they actually do. Everything else about the inventory is driven by messages.

// findTarget returns the row for a name, or fails.
func findTarget(t *testing.T, targets []scanTarget, name string) scanTarget {
	t.Helper()
	for _, target := range targets {
		if target.Name == name {
			return target
		}
	}
	t.Fatalf("%q is not in the inventory", name)
	return scanTarget{}
}

// pulled makes the daemon answer with exactly these image names for the length
// of one test. The inventory drops an image nothing lists, and a test cannot
// pull one — without this, every image assertion below would pass or fail for
// the wrong reason.
func pulled(t *testing.T, names ...string) {
	t.Helper()
	previous := listImages
	listImages = func() ([]docker.Image, error) {
		images := make([]docker.Image, 0, len(names))
		for _, name := range names {
			repo, tag, found := strings.Cut(name, ":")
			if !found {
				tag = ""
			}
			images = append(images, docker.Image{Repository: repo, Tag: tag})
		}
		return images, nil
	}
	t.Cleanup(func() { listImages = previous })
}

// existingRepo is a real directory, because the loader now drops a repository
// path that is not one. A fixture under /srv never existed on the machine
// running the test.
func existingRepo(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("create the repository fixture: %v", err)
	}
	return path
}

func loadedTargets(t *testing.T) []scanTarget {
	t.Helper()
	msg, ok := testutil.MsgOf[InventoryLoadedMsg](loadInventoryCmd(""))
	if !ok {
		t.Fatal("loadInventoryCmd() produced no inventory")
	}
	return msg.Targets
}

// One table over two caches: an image and a repository are told apart by Kind,
// which is what decides how `enter` loads a row and how ctrl+s rescans it.
func TestTheInventoryReadsBothCaches(t *testing.T) {
	contextName := config.CurrentContextName()
	scannedAt := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	pulled(t, "registry.test/both:1")
	repoPath := existingRepo(t, "both-repo")

	images, err := cache.NewImageScanCache()
	if err != nil {
		t.Fatalf("open image cache: %v", err)
	}
	if err := images.Set("registry.test/both:1", cache.ImageScanEntry{Critical: 5, High: 2, ScannedAt: scannedAt}); err != nil {
		t.Fatalf("cache an image scan: %v", err)
	}
	repos, err := cache.NewWorkspaceScanCache(contextName)
	if err != nil {
		t.Fatalf("open workspace cache: %v", err)
	}
	if err := repos.Set(repoPath, cache.WorkspaceScanEntry{RepoPath: repoPath, High: 4, ScannedAt: scannedAt}); err != nil {
		t.Fatalf("cache a workspace scan: %v", err)
	}
	t.Cleanup(func() {
		_ = images.Delete("registry.test/both:1")
		_ = repos.Delete(repoPath)
	})

	targets := loadedTargets(t)

	image := findTarget(t, targets, "registry.test/both:1")
	if image.Kind != kindImage || !image.Scanned || image.Counts.Critical != 5 {
		t.Errorf("the image row = %+v, want a scanned image carrying its counts", image)
	}
	if !image.ScannedAt.Equal(scannedAt) {
		t.Errorf("ScannedAt = %v, want the cached time %v", image.ScannedAt, scannedAt)
	}
	repo := findTarget(t, targets, repoPath)
	if repo.Kind != kindRepo || repo.Counts.High != 4 {
		t.Errorf("the repository row = %+v, want a repository carrying its counts", repo)
	}
}

// §3.39 — the inventory is the view where the asymmetry shows, and it is the
// inverse of what this test used to assert for images.
//
// A **repository** scanned under another context stays hidden: a path is reached
// through `workspaces_dir`, which is per context, so the same path may be
// different work. An **image** does not: the daemon lists it whichever context
// is current, so hiding its counts threw away a scan somebody had paid for and
// made a context switch look like it had deleted something.
func TestTheInventoryHidesAnotherContextsRepositoriesButNotItsImages(t *testing.T) {
	elsewhereRepo := existingRepo(t, "elsewhere-repo")
	repos, err := cache.NewWorkspaceScanCache("some-other-context")
	if err != nil {
		t.Fatalf("open the other context's workspace cache: %v", err)
	}
	if err := repos.Set(elsewhereRepo, cache.WorkspaceScanEntry{RepoPath: elsewhereRepo, Critical: 1}); err != nil {
		t.Fatalf("cache a workspace scan under another context: %v", err)
	}
	t.Cleanup(func() { _ = repos.Delete(elsewhereRepo) })

	images, err := cache.NewImageScanCache()
	if err != nil {
		t.Fatalf("open the image cache: %v", err)
	}
	if err := images.Set("elsewhere/api:9", cache.ImageScanEntry{Critical: 1}); err != nil {
		t.Fatalf("cache an image scan: %v", err)
	}
	t.Cleanup(func() { _ = images.Delete("elsewhere/api:9") })
	// The image is pulled, so its presence or absence is the cache's doing only.
	pulled(t, "elsewhere/api:9")

	targets := loadedTargets(t)

	for _, target := range targets {
		if target.Name == elsewhereRepo {
			t.Errorf("the inventory listed %q, scanned under another context", target.Name)
		}
	}
	image := findTarget(t, targets, "elsewhere/api:9")
	if !image.Scanned || image.Counts.Critical != 1 {
		t.Errorf("the image row = %+v, want the counts it was scanned with", image)
	}
}

// Rule 126: ctrl+a purges. Both the counts entry and the stored result go, or
// `enter` would open findings the purge was meant to invalidate.
func TestPurgingRemovesTheEntryAndItsStoredResult(t *testing.T) {
	contextName := config.CurrentContextName()
	repos, err := cache.NewWorkspaceScanCache(contextName)
	if err != nil {
		t.Fatalf("open workspace cache: %v", err)
	}
	if err := repos.Set("/srv/purge-me", cache.WorkspaceScanEntry{RepoPath: "/srv/purge-me", Critical: 3}); err != nil {
		t.Fatalf("cache a workspace scan: %v", err)
	}
	if err := cache.SaveWorkspaceScanResult("/srv/purge-me", resultFixture()); err != nil {
		t.Fatalf("save the full result: %v", err)
	}

	testutil.Msgs(purgeInventoryCmd([]inventoryScanJob{{Kind: kindRepo, Name: "/srv/purge-me"}}))

	repos.Reload()
	if entry := repos.Get("/srv/purge-me"); entry != nil {
		t.Errorf("the cache entry survived the purge: %+v", entry)
	}
	if _, err := cache.LoadWorkspaceScanResult("/srv/purge-me"); err == nil {
		t.Error("the stored result survived the purge — enter would still open it")
	}
}

// A finished rescan has to land in both places, or the row and `enter` disagree:
// the counts come from the metadata entry, the findings from the stored result.
// Both kinds, because they go to different caches through different writers.
func TestAStoredRescanIsReadableByBothTheRowAndEnter(t *testing.T) {
	for _, tc := range []struct {
		kind targetKind
		name string
		// exists makes the target real for the length of the subtest and
		// returns the name to store it under: the loader now drops a target
		// with nothing behind it.
		exists func(t *testing.T, name string) string
		load   func(string) (*scan.Result, error)
	}{
		{
			kind: kindImage, name: "registry.test/stored:2",
			exists: func(t *testing.T, name string) string { pulled(t, name); return name },
			load:   cache.LoadImageScanResult,
		},
		{
			kind: kindRepo, name: "stored-repo",
			exists: existingRepo,
			load:   cache.LoadWorkspaceScanResult,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := resultFixture()
			result.Counts = scan.SeverityCounts{Critical: 2, High: 1}
			job := inventoryScanJob{Kind: tc.kind, Name: tc.exists(t, tc.name)}

			storeRescan(job, result)
			t.Cleanup(func() {
				testutil.Msgs(purgeInventoryCmd([]inventoryScanJob{job}))
			})

			entry := findTarget(t, loadedTargets(t), job.Name)
			if entry.Kind != tc.kind || entry.Counts.Critical != 2 || !entry.ScannedAt.Equal(result.EndTime) {
				t.Errorf("the row = %+v, want the rescan's kind, counts and end time", entry)
			}
			stored, err := tc.load(job.Name)
			if err != nil {
				t.Fatalf("enter could not read the result the rescan stored: %v", err)
			}
			if len(stored.Findings) != len(result.Findings) {
				t.Errorf("%d findings stored, want %d", len(stored.Findings), len(result.Findings))
			}
		})
	}
}
