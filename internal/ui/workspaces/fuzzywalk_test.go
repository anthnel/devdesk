package workspaces

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func dirSet(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths {
		out[p] = true
	}
	return out
}

func TestCollectDirsFindsPlainDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("creating the tree: %v", err)
	}

	found := collectDirs(root, false)
	dirs := dirSet(found.Dirs)
	for _, want := range []string{root, filepath.Join(root, "a"), nested} {
		if !dirs[want] {
			t.Errorf("collectDirs(%q) = %v, want %q present", root, found.Dirs, want)
		}
	}
}

// A repository's own tree is not a fuzzy-find target, mirroring
// detectSubRepos's boundary: what stops the walk is finding a repository,
// not a depth limit.
func TestCollectDirsStopsAtRepoBoundary(t *testing.T) {
	root := t.TempDir()
	repo := mkRepoAt(t, root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatalf("creating the repo's own tree: %v", err)
	}

	found := collectDirs(root, false)
	dirs := dirSet(found.Dirs)
	if !dirs[repo] {
		t.Errorf("collectDirs(%q) = %v, want the repository itself present", root, found.Dirs)
	}
	if dirs[filepath.Join(repo, "src")] {
		t.Errorf("collectDirs(%q) = %v, walked inside the repository", root, found.Dirs)
	}
}

func TestCollectDirsRespectsShowHidden(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, ".cache")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatalf("creating the hidden directory: %v", err)
	}

	if dirs := dirSet(collectDirs(root, false).Dirs); dirs[hidden] {
		t.Errorf("collectDirs with showHidden=false included %q", hidden)
	}
	if dirs := dirSet(collectDirs(root, true).Dirs); !dirs[hidden] {
		t.Errorf("collectDirs with showHidden=true left out %q", hidden)
	}
}

// The cycle guard walkSubRepos already relies on (mayFollow) is shared by
// this walk, not reimplemented — this is what confirms it actually is.
func TestCollectDirsDoesNotLoopThroughALink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatalf("creating the tree: %v", err)
	}
	linkDir(t, root, filepath.Join(root, "a", "loop"))

	found := collectDirs(root, false)
	for _, p := range found.Dirs {
		if filepath.Base(p) == "loop" {
			t.Fatalf("the walk went through the loop and reported %q", p)
		}
	}
}

func TestCollectDirsCountsUnreadableDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not restrict directory reads on Windows")
	}
	root := t.TempDir()
	closed := filepath.Join(root, "closed")
	if err := os.MkdirAll(closed, 0o755); err != nil {
		t.Fatalf("creating the directory: %v", err)
	}
	if err := os.Chmod(closed, 0o000); err != nil {
		t.Fatalf("closing the directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })

	found := collectDirs(root, false)
	if found.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", found.Skipped)
	}
}
