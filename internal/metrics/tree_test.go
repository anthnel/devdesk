package metrics

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// write creates a file of n bytes, parent directories included.
func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// The sum covers the whole depth, and `.git` is part of it: a cloned
// repository costs its history as much as its working tree, and it is
// often the history that weighs the most. The question is "how much does
// this directory take".
func TestSizeAddsUpTheWholeTreeIncludingGit(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "readme.md"), 100)
	write(t, filepath.Join(root, "repo", "main.go"), 250)
	write(t, filepath.Join(root, "repo", ".git", "objects", "pack", "p.pack"), 4000)

	got := Size(root)
	if !got.OK {
		t.Fatal("the walk reported a failure on a readable tree")
	}
	if got.Partial {
		t.Error("the walk reported itself partial with nothing to skip")
	}
	if got.Bytes != 4350 {
		t.Errorf("Size = %d bytes, want 4350 — every file at every depth", got.Bytes)
	}
}

func TestAnEmptyTreeIsMeasuredAtZero(t *testing.T) {
	got := Size(t.TempDir())
	if !got.OK {
		t.Fatal("an empty directory was reported unreadable")
	}
	if got.Bytes != 0 {
		t.Errorf("Size = %d bytes on an empty directory", got.Bytes)
	}
}

// An unreadable path does not yield a "successful" measurement of zero
// bytes: that would read as an empty directory, and that is what the root
// check in Size exists to avoid.
func TestAMissingPathIsNotAnEmptyTree(t *testing.T) {
	for _, path := range []string{filepath.Join(t.TempDir(), "nope"), ""} {
		if got := Size(path); got.OK {
			t.Errorf("Size(%q) reported a successful measurement of %d bytes", path, got.Bytes)
		}
	}
}

// A symlink counts for nothing, and that is two things rather than one.
// Its target is not followed — it would be counted twice if already in the
// tree, and a cycle would never terminate. But its own entry does not
// count either: WalkDir reports the length of the path it designates, so
// the tree's size would move with a rename elsewhere. That is the second
// half that was missing, and it could not show under Windows, where this
// test is skipped.
func TestSizeDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "data", "big.bin"), 5000)

	if err := os.Symlink(filepath.Join(root, "data"), filepath.Join(root, "link")); err != nil {
		if runtime.GOOS == "windows" {
			// Creating a link needs a privilege the test account may not have
			// under Windows.
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatalf("creating the symlink: %v", err)
	}

	if got := Size(root); got.Bytes != 5000 {
		t.Errorf("Size = %d bytes, want 5000 — the link weighed, by its target or by itself", got.Bytes)
	}
}
