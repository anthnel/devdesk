package k8s

import (
	"os"
	"path/filepath"
	"testing"
)

// EscapingReference finds the directory an overlay pulls in, directly or
// through a base, that lies outside a given root: what a container mounted on
// that root cannot see (DEVDESK-742). Only an existing directory counts: a
// missing one is a broken overlay, and a file outside the kustomization's
// directory is refused by kustomize wherever it runs.
func TestEscapingReferenceFindsWhatLiesOutsideTheRoot(t *testing.T) {
	tree := writeTree(t, map[string]string{
		"shared/kustomization.yaml":                  "resources:\n- cm.yaml\n",
		"shared/cm.yaml":                             deployment,
		"repo/base/kustomization.yaml":               "resources:\n- ../../shared\n",
		"repo/inner/kustomization.yaml":              "resources:\n- cm.yaml\n",
		"repo/inner/cm.yaml":                         deployment,
		"repo/overlays/direct/kustomization.yaml":    "resources:\n- ../../../shared\n",
		"repo/overlays/viabase/kustomization.yaml":   "bases:\n- ../../base\n",
		"repo/overlays/inside/kustomization.yaml":    "resources:\n- ../../inner\n- https://example.com/x.yaml\n",
		"repo/overlays/missing/kustomization.yaml":   "resources:\n- ../../../nowhere\n",
		"repo/overlays/file/kustomization.yaml":      "resources:\n- ../../../shared/cm.yaml\n",
		"repo/overlays/component/kustomization.yaml": "components:\n- ../../../shared\n",
		"repo/overlays/loop/kustomization.yaml":      "resources:\n- ../loop2\n",
		"repo/overlays/loop2/kustomization.yaml":     "resources:\n- ../loop\n",
	})
	root := filepath.Join(tree, "repo")
	if err := os.Symlink(filepath.Join(tree, "shared"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "overlays", "symlinked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "overlays", "symlinked", "kustomization.yaml"),
		[]byte("resources:\n- ../../link\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		overlay  string
		wantFrom string // relative to root, "" when nothing escapes
		wantRef  string
	}{
		{"direct", "overlays/direct", "../../../shared"},
		{"viabase", "base", "../../shared"},
		{"component", "overlays/component", "../../../shared"},
		{"symlinked", "overlays/symlinked", "../../link"},
		{"inside", "", ""},
		{"missing", "", ""},
		{"file", "", ""},
		{"loop", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.overlay, func(t *testing.T) {
			from, ref, ok := EscapingReference(root, filepath.Join(root, "overlays", tt.overlay))
			if ok != (tt.wantRef != "") {
				t.Fatalf("escapes = %v (%q from %q), want %v", ok, ref, from, tt.wantRef != "")
			}
			if !ok {
				return
			}
			rel, err := filepath.Rel(root, from)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.ToSlash(rel) != tt.wantFrom || ref != tt.wantRef {
				t.Errorf("got %q from %q, want %q from %q", ref, filepath.ToSlash(rel), tt.wantRef, tt.wantFrom)
			}
		})
	}
}
