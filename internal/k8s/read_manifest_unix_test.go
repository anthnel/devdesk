//go:build unix

package k8s

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO has no size and no end: reading it would hang until a writer shows
// up. It is refused, and quickly.
func TestReadManifestInRefusesAFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.yaml"), 0o600); err != nil {
		t.Skipf("cannot create a FIFO: %v", err)
	}
	root := openRoot(t, dir)

	done := make(chan error, 1)
	go func() {
		_, err := ReadManifestIn(root, "pipe.yaml")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a FIFO was read")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadManifestIn blocked on a FIFO")
	}
}
