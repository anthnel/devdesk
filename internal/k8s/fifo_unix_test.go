//go:build unix

package k8s

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO named kustomization.yaml would block a read until a writer comes,
// which never does: Discover refuses anything but a regular file (security
// audit of DEVDESK-742).
func TestAFIFOKustomizationDoesNotBlock(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(root, "ov")
	if err := os.MkdirAll(overlay, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(overlay, "kustomization.yaml"), 0o600); err != nil {
		t.Skipf("no FIFO here: %v", err)
	}

	done := make(chan struct{})
	go func() {
		_, _ = Discover(root)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Discover still reading a FIFO after 3s")
	}
}
