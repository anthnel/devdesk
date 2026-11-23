package k8s

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// The walk's bounds: a repository is untrusted input, and kustomize itself
// refuses a deeper or larger graph long before these.
const (
	maxEscapeDepth  = 64
	maxEscapeVisits = 1000
)

// EscapingReference follows the kustomization in dir through its resources,
// bases and components, and returns the first reference that names an existing
// directory outside root: the real path of the kustomization's directory that
// holds it, and the reference as written there.
//
// It answers what a container mounted on root cannot see (DEVDESK-742). A
// reference to a missing path is not one: that overlay is broken wherever it
// is built. Nor is a file: kustomize refuses a file outside its
// kustomization's directory wherever it runs. A remote reference is skipped,
// and so are patches and generators, which name files.
//
// Every directory is visited once, by its real path, so symlinks to "." or to
// a parent end the walk instead of multiplying it. The walk also stops at
// maxEscapeDepth, at maxEscapeVisits kustomizations, and when ctx is done.
func EscapingReference(ctx context.Context, root, dir string) (from, ref string, ok bool) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", false
	}
	start, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", "", false
	}
	w := escapeWalk{ctx: ctx, root: realRoot, seen: map[string]bool{}}
	return w.walk(start, 0)
}

// escapeWalk is one EscapingReference: its bounds and what it has visited.
type escapeWalk struct {
	ctx  context.Context
	root string
	seen map[string]bool
}

// walk visits dir, a real path, depth references away from the overlay.
func (w *escapeWalk) walk(dir string, depth int) (string, string, bool) {
	if depth > maxEscapeDepth || len(w.seen) >= maxEscapeVisits || w.ctx.Err() != nil || w.seen[dir] {
		return "", "", false
	}
	w.seen[dir] = true
	for _, r := range directoryRefs(dir) {
		real, err := filepath.EvalSymlinks(filepath.Join(dir, r))
		if err != nil {
			continue
		}
		if info, err := os.Stat(real); err != nil || !info.IsDir() {
			continue
		}
		if real != w.root && !isUnder(real, w.root) {
			return dir, r, true
		}
		if f, escaped, ok := w.walk(real, depth+1); ok {
			return f, escaped, true
		}
	}
	return "", "", false
}

// directoryRefs lists the local references of dir's kustomization, in the
// order kustomize reads them. Nothing when dir holds none or it does not parse.
func directoryRefs(dir string) []string {
	file := kustomizationIn(dir)
	if file == "" {
		return nil
	}
	content, err := readBounded(file)
	if err != nil {
		return nil
	}
	var refs kustomizationRefs
	if yaml.Unmarshal(content, &refs) != nil {
		return nil
	}
	var out []string
	for _, list := range [][]string{refs.Resources, refs.Bases, refs.Components} {
		for _, r := range list {
			if !strings.Contains(r, "://") {
				out = append(out, r)
			}
		}
	}
	return out
}
