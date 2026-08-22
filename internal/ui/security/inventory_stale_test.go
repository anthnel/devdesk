package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
)

// The inventory is the two scan caches, and a cache outlives what it describes:
// D on an image, P, D on a repository, and any docker rmi or rm -rf outside the
// application all leave an entry naming something that is gone. The load
// reconciles rather than each of those cascading — one rule against four
// callers, one of which (the outside world) cannot call anything.

func TestAnImageStillPulledIsKept(t *testing.T) {
	images := map[string]struct{}{"nginx:1.27": {}}

	if !stillPulled("nginx:1.27", images, true) {
		t.Error("an image the daemon lists was dropped")
	}
}

func TestAnImageThatIsGoneIsDropped(t *testing.T) {
	images := map[string]struct{}{"nginx:1.27": {}}

	if stillPulled("vsc-discours-f6f52be:latest", images, true) {
		t.Error("an image the daemon does not list survived the reconciliation")
	}
}

// The guard the whole design rests on: "Docker is not running" and "the image is
// gone" are the same silence, and reading the first as the second would empty
// the inventory of every image the moment the daemon stops.
func TestNothingIsDroppedWhenTheImagesCannotBeListed(t *testing.T) {
	if !stillPulled("anything:1.0", nil, false) {
		t.Error("a failed enumeration dropped an image, so a stopped daemon reads as a deletion")
	}
}

// An empty listing is an answer, not a failure: a machine with no images has no
// image targets.
func TestAnEmptyListingDropsEveryImage(t *testing.T) {
	if stillPulled("nginx:1.27", map[string]struct{}{}, true) {
		t.Error("an image survived a listing that succeeded and named nothing")
	}
}

func TestARepositoryThatIsGoneIsDropped(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "deleted-repo")

	if !isGone(missing) {
		t.Errorf("%q does not exist and was not reported gone", missing)
	}
}

func TestAnExistingRepositoryIsKept(t *testing.T) {
	dir := t.TempDir()

	if isGone(dir) {
		t.Errorf("%q exists and was reported gone", dir)
	}
}

// A file that cannot be read is not a file that is absent. Only os.IsNotExist
// removes a row; anything else keeps it, because "I could not look" is not an
// answer about what is there.
func TestAPathThatCannotBeReadIsKept(t *testing.T) {
	// A path under a *file* answers ENOTDIR rather than ENOENT on Unix, and
	// ERROR_DIRECTORY on Windows — either way, not a definite absence.
	file := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(file, "child")

	if _, err := os.Stat(under); err == nil {
		t.Skip("this platform resolves a path under a file")
	} else if os.IsNotExist(err) {
		t.Skip("this platform reports a path under a file as absent")
	}

	if isGone(under) {
		t.Errorf("%q could not be read and was reported gone", under)
	}
}

// The reported case, end to end: an image deleted after its scan keeps its
// cache entry, and the inventory listed it forever.
func TestAnImageDeletedSinceItsScanLeavesTheInventory(t *testing.T) {
	const deleted = "vsc-discours-f6f52be-uid:latest"
	images, err := cache.NewImageScanCache(config.CurrentContextName())
	if err != nil {
		t.Fatalf("open image cache: %v", err)
	}
	if err := images.Set(deleted, cache.ImageScanEntry{Critical: 1}); err != nil {
		t.Fatalf("cache an image scan: %v", err)
	}
	t.Cleanup(func() { _ = images.Delete(deleted) })
	pulled(t, "nginx:1.27")

	for _, target := range loadedTargets(t) {
		if target.Name == deleted {
			t.Fatalf("the inventory still lists %q, which no longer exists", deleted)
		}
	}

	// Hidden, not deleted. Dropping the entry would make a transient answer —
	// a daemon that came back with a shorter list, a share not yet mounted —
	// destroy a scan result nobody asked to purge. `A` only rescans the rows
	// that are there, so a hidden entry costs nothing while it waits.
	images.Reload()
	if entry := images.Get(deleted); entry == nil {
		t.Error("the cache entry was deleted; hiding a row must not destroy what it described")
	}
}

// The same entry, with the daemon unreachable: it comes back, because "I could
// not ask" is not "it is gone".
func TestAStoppedDaemonHidesNothing(t *testing.T) {
	const scanned = "registry.test/still-there:1"
	images, err := cache.NewImageScanCache(config.CurrentContextName())
	if err != nil {
		t.Fatalf("open image cache: %v", err)
	}
	if err := images.Set(scanned, cache.ImageScanEntry{Critical: 1}); err != nil {
		t.Fatalf("cache an image scan: %v", err)
	}
	t.Cleanup(func() { _ = images.Delete(scanned) })

	previous := listImages
	listImages = func() ([]docker.Image, error) { return nil, errors.New("docker daemon is not running") }
	t.Cleanup(func() { listImages = previous })

	found := false
	for _, target := range loadedTargets(t) {
		if target.Name == scanned {
			found = true
		}
	}
	if !found {
		t.Errorf("%q vanished because the daemon could not be reached", scanned)
	}
}
