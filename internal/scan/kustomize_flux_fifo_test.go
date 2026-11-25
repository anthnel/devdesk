//go:build unix

package scan

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A file that fluxOverlay must not open is a named pipe: opening one for reading
// blocks until something writes to it, so a call that opens it does not return.
// Each case is a pipe that holds a Flux document nobody can write: a call that
// returns false, in time, did not open it.
func TestFluxOverlayDoesNotOpenAFileItMustNotRead(t *testing.T) {
	tests := []struct {
		name string
		// listing is the kustomization, pipe the path of the pipe: relative to the
		// repository root, with the link (if any) to make in overlay.
		listing func(root, base string) string
		pipe    func(root, base string) string
		link    bool
	}{
		{"a pipe in the overlay: it is not a regular file",
			func(_, _ string) string { return "resources:\n  - pipe\n" },
			func(root, _ string) string { return filepath.Join(root, "overlay", "pipe") }, false},
		{"a pipe outside the root",
			func(_, _ string) string { return "resources:\n  - ../../outside/pipe\n" },
			func(_, base string) string { return filepath.Join(base, "outside", "pipe") }, false},
		{"an absolute path to a pipe in the overlay",
			func(root, _ string) string {
				return "resources:\n  - " + q(filepath.Join(root, "overlay", "pipe")) + "\n"
			},
			func(root, _ string) string { return filepath.Join(root, "overlay", "pipe") }, false},
		{"a link to a pipe outside the root",
			func(_, _ string) string { return "resources:\n  - link\n" },
			func(_, base string) string { return filepath.Join(base, "outside", "pipe") }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "repo")
			writeFile(t, filepath.Join(root, "overlay", "kustomization.yaml"), tt.listing(root, base))
			pipe := tt.pipe(root, base)
			mkdirs(t, filepath.Dir(pipe))
			if err := syscall.Mkfifo(pipe, 0o600); err != nil {
				t.Skipf("no named pipe here: %v", err)
			}
			if tt.link {
				if err := os.Symlink(pipe, filepath.Join(root, "overlay", "link")); err != nil {
					t.Skipf("no symbolic link here: %v", err)
				}
			}
			answer := make(chan bool, 1)
			go func() { answer <- fluxOverlay(root, filepath.Join(root, "overlay"), defaultGateLimits) }()

			select {
			case got := <-answer:
				if got {
					t.Error("a pipe was a Flux file")
				}
			case <-time.After(5 * time.Second):
				// Let the blocked open go, so that the goroutine does not outlive the test.
				if f, err := os.OpenFile(pipe, os.O_RDWR, 0); err == nil {
					_ = f.Close()
				}
				t.Fatal("fluxOverlay did not return: it opened a file it must not read")
			}
		})
	}
}
