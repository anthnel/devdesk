package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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
			from, ref, ok := EscapingReference(t.Context(), root, filepath.Join(root, "overlays", tt.overlay))
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

// finishes runs f and fails the test if it does not return within d: a walk
// that does not end must fail the test, not hang the suite.
func finishes(t *testing.T, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("still running after %v", d)
	}
}

// Symlinks to "." are versioned by git, so a cloned repository can hold them.
// Two in one kustomization made the walk exponential (security audit of
// DEVDESK-742); it ends because a directory is visited once, by its real path.
func TestEscapingReferenceEndsOnSymlinkLoops(t *testing.T) {
	for _, n := range []int{1, 2, 3} {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		overlay := filepath.Join(root, "ov")
		if err := os.MkdirAll(overlay, 0o755); err != nil {
			t.Fatal(err)
		}
		refs := "resources:\n"
		for i := range n {
			name := string(rune('a' + i))
			if err := os.Symlink(".", filepath.Join(overlay, name)); err != nil {
				t.Fatal(err)
			}
			refs += "- " + name + "\n"
		}
		if err := os.WriteFile(filepath.Join(overlay, "kustomization.yaml"), []byte(refs), 0o600); err != nil {
			t.Fatal(err)
		}

		finishes(t, 5*time.Second, func() {
			if _, ref, ok := EscapingReference(t.Context(), root, overlay); ok {
				t.Errorf("%d links: %q escapes, want nothing", n, ref)
			}
		})
	}
}

// The walk is bounded in depth: a chain of bases deeper than that is not
// followed to its end, and a cancelled context stops it at once.
func TestEscapingReferenceIsBounded(t *testing.T) {
	files := map[string]string{"shared/kustomization.yaml": "resources: []\n"}
	const depth = maxEscapeDepth + 5
	for i := range depth {
		files[fmt.Sprintf("repo/d%d/kustomization.yaml", i)] = fmt.Sprintf("resources:\n- ../d%d\n", i+1)
	}
	files[fmt.Sprintf("repo/d%d/kustomization.yaml", depth)] = "resources:\n- ../../shared\n"
	tree := writeTree(t, files)
	root := filepath.Join(tree, "repo")

	if _, ref, ok := EscapingReference(t.Context(), root, filepath.Join(root, "d0")); ok {
		t.Errorf("followed a chain of %d bases to %q, want it cut at %d", depth, ref, maxEscapeDepth)
	}
	if _, _, ok := EscapingReference(t.Context(), root, filepath.Join(root, fmt.Sprintf("d%d", depth-3))); !ok {
		t.Error("a short chain to the outside was not followed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, ok := EscapingReference(ctx, root, filepath.Join(root, fmt.Sprintf("d%d", depth))); ok {
		t.Error("a cancelled walk still answered")
	}
}
