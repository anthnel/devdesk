package k8s

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// The walk's bounds: a repository is untrusted input (N1 of the DEVDESK-742
// security review), and a real kustomize graph stays far below each of them.
// Reaching any one stops the whole walk with "no escape found", which keeps
// the HIGH finding and kustomize's own message.
const (
	maxEscapeDepth  = 64
	maxEscapeVisits = 1000
	// maxEscapeResolutions is the budget of symlink resolutions, each with
	// its Stat, for the whole walk.
	maxEscapeResolutions = 10000
	// maxEscapeRefs is the most distinct references one kustomization may
	// list; more and the walk stops.
	maxEscapeRefs = 1000
	// maxKustomizationSize is what is read of one kustomization file for the
	// walk, far below maxManifestSize: a kustomization is a list of paths.
	maxKustomizationSize = 256 << 10
	// maxEscapeBytes is what the whole walk may read of kustomization files.
	maxEscapeBytes = 4 << 20
)

// evalSymlinks resolves a path; tests count the calls.
var evalSymlinks = filepath.EvalSymlinks

// EscapingReference follows the kustomization in dir through its resources,
// bases and components, and returns the first reference that names an existing
// directory outside root: the real path of the kustomization's directory that
// holds it, and the reference as written there.
//
// It answers what a container mounted on root cannot see (DEVDESK-742). A
// reference to a missing path is not one: that overlay is broken wherever it
// is built. Nor is a file: under kustomize's default load restrictor a file
// outside its kustomization's directory fails wherever it runs. A remote
// reference is skipped, and so are patches and generators, which name files.
//
// Every directory is visited once, by its real path, and each kustomization's
// references are counted once, so symlinks to "." or to a parent do not
// multiply the work. The walk stops, answering that nothing escapes, when ctx
// is done (checked before every resolution) or at any bound above.
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
// stopped is set once a bound is reached or ctx is done; nothing is reported
// after that.
type escapeWalk struct {
	ctx         context.Context
	root        string
	seen        map[string]bool
	resolutions int
	bytes       int
	stopped     bool
}

// halt reports whether the walk must stop now, and records it.
func (w *escapeWalk) halt() bool {
	if w.ctx.Err() != nil || w.resolutions >= maxEscapeResolutions {
		w.stopped = true
	}
	return w.stopped
}

// walk visits dir, a real path, depth references away from the overlay.
func (w *escapeWalk) walk(dir string, depth int) (string, string, bool) {
	if w.seen[dir] {
		return "", "", false
	}
	if depth > maxEscapeDepth || len(w.seen) >= maxEscapeVisits {
		w.stopped = true
	}
	if w.halt() {
		return "", "", false
	}
	w.seen[dir] = true
	refs, read, ok := directoryRefs(dir, maxEscapeBytes-w.bytes)
	w.bytes += read
	if !ok {
		w.stopped = true
		return "", "", false
	}
	for _, r := range refs {
		if w.halt() {
			return "", "", false
		}
		w.resolutions++
		real, err := evalSymlinks(filepath.Join(dir, r))
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

// directoryRefs lists the distinct local references of dir's kustomization,
// in the order kustomize reads them, reading at most budget bytes, and returns
// how many it read. The last result is false when the file is larger than
// maxKustomizationSize or the budget, or lists more than maxEscapeRefs
// distinct references; a missing or unparseable kustomization lists nothing.
func directoryRefs(dir string, budget int) ([]string, int, bool) {
	file := kustomizationIn(dir)
	if file == "" {
		return nil, 0, true
	}
	content, err := readLimited(file, int64(min(maxKustomizationSize, budget)))
	if errors.Is(err, errTooLarge) {
		return nil, 0, false
	}
	if err != nil {
		return nil, 0, true
	}
	var refs kustomizationRefs
	if yaml.Unmarshal(content, &refs) != nil {
		return nil, len(content), true
	}
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{refs.Resources, refs.Bases, refs.Components} {
		for _, r := range list {
			key := filepath.Clean(r)
			if strings.Contains(r, "://") || seen[key] {
				continue
			}
			if len(out) == maxEscapeRefs {
				return nil, len(content), false
			}
			seen[key] = true
			out = append(out, r)
		}
	}
	return out, len(content), true
}
