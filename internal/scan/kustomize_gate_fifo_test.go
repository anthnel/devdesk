//go:build unix

package scan

import (
	"path/filepath"
	"syscall"
	"testing"
)

// DEVDESK-771: kustomize blocks on a named pipe it is told to read, so a
// reference that exists must be a regular file or a directory.
func TestVetOverlayRefusesAReferenceThatIsNeitherAFileNorADirectory(t *testing.T) {
	for name, kustomization := range map[string]string{
		"a resource":  "resources:\n  - pipe\n",
		"a patch":     "patches:\n  - path: pipe\n",
		"an env file": "configMapGenerator:\n  - name: c\n    env: pipe\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := writeRepo(t, ovl(kustomization))
			if err := syscall.Mkfifo(filepath.Join(root, "overlay", "pipe"), 0o600); err != nil {
				t.Skipf("no named pipe here: %v", err)
			}

			got := vetOverlay(t.Context(), root, filepath.Join(root, "overlay"), defaultGateLimits)

			checkVerdict(t, got, gateUnchecked, overlayFile, "", "pipe")
		})
	}
}
