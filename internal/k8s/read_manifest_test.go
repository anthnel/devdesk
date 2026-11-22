package k8s

import (
	"os"
	"strings"
	"testing"
)

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestReadManifestInReadsARegularFile(t *testing.T) {
	root := openRoot(t, writeTree(t, map[string]string{"sub/a.yaml": deployment}))
	got, err := ReadManifestIn(root, "sub/a.yaml")
	if err != nil || string(got) != deployment {
		t.Fatalf("ReadManifestIn = %q, %v; want the file", got, err)
	}
}

// The bound is applied to what is read, not only to what Stat announced, so a
// file that grows between the two cannot slip past it.
func TestReadManifestInRefusesAFileOverTheBound(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"exact.yaml": strings.Repeat("a", maxManifestSize),
		"big.yaml":   strings.Repeat("a", maxManifestSize+1),
	})
	root := openRoot(t, dir)
	if got, err := ReadManifestIn(root, "exact.yaml"); err != nil || len(got) != maxManifestSize {
		t.Errorf("a file of exactly the bound: len %d, err %v; want it read", len(got), err)
	}
	if _, err := ReadManifestIn(root, "big.yaml"); err == nil {
		t.Error("a file over the bound was read")
	}
}

func TestReadManifestInRefusesADirectory(t *testing.T) {
	root := openRoot(t, writeTree(t, map[string]string{"d/a.yaml": deployment}))
	if _, err := ReadManifestIn(root, "d"); err == nil {
		t.Error("a directory was read")
	}
}

func TestReadManifestInCannotLeaveTheRoot(t *testing.T) {
	outside := writeTree(t, map[string]string{"secret.yaml": deployment})
	dir := t.TempDir()
	if err := os.Symlink(outside+"/secret.yaml", dir+"/link.yaml"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root := openRoot(t, dir)
	for _, name := range []string{"link.yaml", "../secret.yaml"} {
		if _, err := ReadManifestIn(root, name); err == nil {
			t.Errorf("%s left the root", name)
		}
	}
}
