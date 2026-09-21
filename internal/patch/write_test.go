package patch

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteIfUnchangedReplacesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Dockerfile")
	writeFile(t, path, "FROM alpine:3.18\n", 0o644)

	if err := WriteIfUnchanged(path, []byte("FROM alpine:3.18\n"), []byte("FROM alpine:3.21\n")); err != nil {
		t.Fatalf("WriteIfUnchanged: %v", err)
	}
	if got := readFile(t, path); got != "FROM alpine:3.21\n" {
		t.Errorf("file holds %q", got)
	}
}

// The user confirmed one diff. A file edited since is not that diff.
func TestWriteIfUnchangedRefusesAFileThatChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Dockerfile")
	writeFile(t, path, "FROM alpine:3.19\n", 0o644)

	err := WriteIfUnchanged(path, []byte("FROM alpine:3.18\n"), []byte("FROM alpine:3.21\n"))
	if !errors.Is(err, ErrChanged) {
		t.Fatalf("err = %v, want ErrChanged", err)
	}
	if got := readFile(t, path); got != "FROM alpine:3.19\n" {
		t.Errorf("a refused write touched the file: %q", got)
	}
}

func TestWriteIfUnchangedLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	writeFile(t, path, "a\n", 0o644)

	if err := WriteIfUnchanged(path, []byte("a\n"), []byte("b\n")); err != nil {
		t.Fatalf("WriteIfUnchanged: %v", err)
	}
	// And after a refusal.
	_ = WriteIfUnchanged(path, []byte("stale\n"), []byte("c\n"))

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only the Dockerfile", len(entries))
	}
}

func TestWriteIfUnchangedKeepsThePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "Dockerfile")
	writeFile(t, path, "a\n", 0o640)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	if err := WriteIfUnchanged(path, []byte("a\n"), []byte("b\n")); err != nil {
		t.Fatalf("WriteIfUnchanged: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640 kept", info.Mode().Perm())
	}
}

// Replacing the link with a file would leave the real Dockerfile as it was.
func TestWriteIfUnchangedFollowsASymbolicLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real.Dockerfile")
	link := filepath.Join(dir, "Dockerfile")
	writeFile(t, real, "a\n", 0o644)
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteIfUnchanged(link, []byte("a\n"), []byte("b\n")); err != nil {
		t.Fatalf("WriteIfUnchanged: %v", err)
	}
	if got := readFile(t, real); got != "b\n" {
		t.Errorf("the target holds %q, want it written", got)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced by a file: %v %v", info, err)
	}
}

func TestWriteIfUnchangedOfAMissingFile(t *testing.T) {
	if err := WriteIfUnchanged(filepath.Join(t.TempDir(), "absent"), nil, []byte("x")); err == nil {
		t.Error("a missing file was written")
	}
}
