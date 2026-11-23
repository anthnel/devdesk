package k8s

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// EscapingReference follows the kustomization in dir through its resources,
// bases and components, and returns the first reference that names an existing
// directory outside root: the directory of the kustomization that holds it, and
// the reference as written there. A symlink is followed before judging.
//
// It answers what a container mounted on root cannot see (DEVDESK-742). A
// reference to a missing path is not one: that overlay is broken wherever it
// is built. Nor is a file: kustomize refuses a file outside its
// kustomization's directory wherever it runs. A remote reference is skipped.
func EscapingReference(root, dir string) (from, ref string, ok bool) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", false
	}
	seen := map[string]bool{}
	var walk func(d string) (string, string, bool)
	walk = func(d string) (string, string, bool) {
		d = filepath.Clean(d)
		if seen[d] {
			return "", "", false
		}
		seen[d] = true
		for _, r := range directoryRefs(d) {
			real, err := filepath.EvalSymlinks(filepath.Join(d, r))
			if err != nil {
				continue
			}
			if info, err := os.Stat(real); err != nil || !info.IsDir() {
				continue
			}
			if real != realRoot && !isUnder(real, realRoot) {
				return d, r, true
			}
			if f, escaped, ok := walk(filepath.Join(d, r)); ok {
				return f, escaped, true
			}
		}
		return "", "", false
	}
	return walk(dir)
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
