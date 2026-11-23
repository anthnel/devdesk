package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// countResolutions wraps the walk's symlink resolution; after is called with
// the running count after each call.
func countResolutions(t *testing.T, after func(n int)) *int {
	t.Helper()
	n := 0
	previous := evalSymlinks
	evalSymlinks = func(p string) (string, error) {
		n++
		if after != nil {
			after(n)
		}
		return previous(p)
	}
	t.Cleanup(func() { evalSymlinks = previous })
	return &n
}

// hostileTree is the security audit's repository at a smaller scale (N1 of
// DEVDESK-742): one kustomization listing n sibling directories and then
// "- ." up to size bytes, and n directories whose kustomization.yaml is a
// symlink to it. Every directory reaches every other, and every one repeats
// the same reference thousands of times.
func hostileTree(t *testing.T, n, size int) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("resources:\n")
	for i := range n {
		fmt.Fprintf(&b, "- ../c%d\n", i)
	}
	for b.Len() < size {
		b.WriteString("- .\n")
	}
	if err := os.WriteFile(filepath.Join(root, "big.yaml"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		d := filepath.Join(root, fmt.Sprintf("c%d", i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../big.yaml", filepath.Join(d, "kustomization.yaml")); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// A hostile repository costs the walk a bounded number of resolutions, and
// little time: references are deduplicated, and a global budget stops it.
func TestEscapingReferenceResistsAHostileRepository(t *testing.T) {
	root := hostileTree(t, 70, 128<<10)
	n := countResolutions(t, nil)

	start := time.Now()
	finishes(t, 20*time.Second, func() {
		if _, ref, ok := EscapingReference(t.Context(), root, filepath.Join(root, "c0")); ok {
			t.Errorf("%q escapes, want nothing", ref)
		}
	})

	if *n > maxEscapeResolutions+1 {
		t.Errorf("%d resolutions, want at most %d", *n, maxEscapeResolutions+1)
	}
	limit := 2 * time.Second
	if raceEnabled {
		limit = 15 * time.Second
	}
	if d := time.Since(start); d > limit {
		t.Errorf("took %v, want under %v", d, limit)
	}
}

// A context cancelled in the middle of a kustomization's references stops the
// walk at the next one, not at the end of the list.
func TestEscapingReferenceStopsMidLoopWhenCancelled(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("resources:\n")
	for i := range 300 {
		if err := os.MkdirAll(filepath.Join(root, "ov", fmt.Sprintf("s%d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "- s%d\n", i)
	}
	if err := os.WriteFile(filepath.Join(root, "ov", "kustomization.yaml"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	n := countResolutions(t, func(n int) {
		if n == 10 {
			cancel()
		}
	})

	EscapingReference(ctx, root, filepath.Join(root, "ov"))

	if *n > 11 {
		t.Errorf("%d resolutions after a cancel at the 10th, want the walk stopped", *n)
	}
}

// What the walk reads of one kustomization is bounded: its size, the number of
// references, each counted once. Past a bound the walk reports nothing, which
// keeps the HIGH finding and kustomize's message.
func TestEscapingReferenceBoundsWhatItReads(t *testing.T) {
	tree := writeTree(t, map[string]string{"shared/kustomization.yaml": "resources: []\n"})
	write := func(name, content string) string {
		t.Helper()
		dir := filepath.Join(tree, "repo", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	root := filepath.Join(tree, "repo")

	var many strings.Builder
	many.WriteString("resources:\n")
	for i := range maxEscapeRefs {
		fmt.Fprintf(&many, "- missing%d\n", i)
	}
	many.WriteString("- ../../shared\n")
	if _, ref, ok := EscapingReference(t.Context(), root, write("many", many.String())); ok {
		t.Errorf("read past %d references to %q", maxEscapeRefs, ref)
	}

	big := "resources:\n- ../../shared\n" + strings.Repeat("# padding\n", maxKustomizationSize/10+1)
	if _, ref, ok := EscapingReference(t.Context(), root, write("big", big)); ok {
		t.Errorf("read a kustomization over %d bytes, found %q", maxKustomizationSize, ref)
	}

	n := countResolutions(t, nil)
	repeated := "resources:\n" + strings.Repeat("- .\n- ./\n", 5000) + "- ../../shared\n"
	if _, _, ok := EscapingReference(t.Context(), root, write("repeated", repeated)); !ok {
		t.Error("the reference after the repeated ones was not found")
	}
	if *n > 3 {
		t.Errorf("%d resolutions for two distinct references, want them counted once", *n)
	}
}

// What the whole walk reads is bounded too: many large kustomizations, each
// under the per-file limit, stop it before the reference that escapes.
func TestEscapingReferenceBoundsWhatTheWholeWalkReads(t *testing.T) {
	tree := writeTree(t, map[string]string{"shared/kustomization.yaml": "resources: []\n"})
	root := filepath.Join(tree, "repo")
	padding := strings.Repeat("# padding\n", (maxKustomizationSize-1024)/10)
	var overlay strings.Builder
	overlay.WriteString("resources:\n")
	files := maxEscapeBytes/len(padding) + 2
	for i := range files {
		dir := filepath.Join(root, fmt.Sprintf("d%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("resources: []\n"+padding), 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&overlay, "- ../d%d\n", i)
	}
	overlay.WriteString("- ../../shared\n")
	ov := filepath.Join(root, "ov")
	if err := os.MkdirAll(ov, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ov, "kustomization.yaml"), []byte(overlay.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, ref, ok := EscapingReference(t.Context(), root, ov); ok {
		t.Errorf("read %d kustomizations of %d bytes, past %d in all, to %q", files, len(padding), maxEscapeBytes, ref)
	}
}
