package security

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
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

func loadedTargets(t *testing.T) []scanTarget {
	t.Helper()
	msg, ok := testutil.MsgOf[InventoryLoadedMsg](loadInventoryCmd())
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

	images, err := cache.NewImageScanCache(contextName)
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
	if err := repos.Set("/srv/both-repo", cache.WorkspaceScanEntry{RepoPath: "/srv/both-repo", High: 4, ScannedAt: scannedAt}); err != nil {
		t.Fatalf("cache a workspace scan: %v", err)
	}
	t.Cleanup(func() {
		_ = images.Delete("registry.test/both:1")
		_ = repos.Delete("/srv/both-repo")
	})

	targets := loadedTargets(t)

	image := findTarget(t, targets, "registry.test/both:1")
	if image.Kind != kindImage || !image.Scanned || image.Counts.Critical != 5 {
		t.Errorf("the image row = %+v, want a scanned image carrying its counts", image)
	}
	if !image.ScannedAt.Equal(scannedAt) {
		t.Errorf("ScannedAt = %v, want the cached time %v", image.ScannedAt, scannedAt)
	}
	repo := findTarget(t, targets, "/srv/both-repo")
	if repo.Kind != kindRepo || repo.Counts.High != 4 {
		t.Errorf("the repository row = %+v, want a repository carrying its counts", repo)
	}
}

// §0b: the caches are scoped to a configuration context, and the inventory is
// the view that would put another context's findings on screen. It reads the
// current context and only that one.
func TestTheInventoryListsOnlyTheCurrentContext(t *testing.T) {
	other, err := cache.NewImageScanCache("some-other-context")
	if err != nil {
		t.Fatalf("open the other context's cache: %v", err)
	}
	if err := other.Set("elsewhere/api:9", cache.ImageScanEntry{Critical: 1}); err != nil {
		t.Fatalf("cache under another context: %v", err)
	}
	t.Cleanup(func() { _ = other.Delete("elsewhere/api:9") })

	for _, target := range loadedTargets(t) {
		if target.Name == "elsewhere/api:9" {
			t.Fatalf("the inventory listed %q, scanned under another context", target.Name)
		}
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
		load func(string) (*scan.Result, error)
	}{
		{kindImage, "registry.test/stored:2", cache.LoadImageScanResult},
		{kindRepo, "/srv/stored-repo", cache.LoadWorkspaceScanResult},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := resultFixture()
			result.Counts = scan.SeverityCounts{Critical: 2, High: 1}
			job := inventoryScanJob{Kind: tc.kind, Name: tc.name}

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
