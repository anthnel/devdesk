// Package k8s reads Kubernetes manifests as files — which of a repository's
// YAML files are manifests, which directories are Helm charts or Kustomize
// roots, and where inside a manifest a JSON pointer lands (§3.80).
//
// It never talks to a cluster and runs no tool: it is pure file reading, so
// the scan stage that drives kubeconform, helm and kustomize can be tested
// without any of them.
package k8s

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxManifestSize bounds what is read to decide whether a file is a manifest.
// A generated bundle can be large, but a YAML file of several megabytes in a
// repository is data, not something anyone applies by hand.
const maxManifestSize = 4 << 20

// kustomizationFiles are the names kustomize accepts for a root, in the order
// it looks for them.
var kustomizationFiles = []string{"kustomization.yaml", "kustomization.yml", "Kustomization"}

// skippedDirs are never descended into: dependencies and tool state, not the
// repository's own manifests.
var skippedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
}

// Layout is what a repository holds in the way of Kubernetes configuration.
// Every path is relative to the root, with forward slashes.
type Layout struct {
	// Manifests are the plain YAML files that carry at least one document
	// with an apiVersion and a kind, outside any chart or Kustomize root.
	Manifests []string
	// Charts are the directories holding a Chart.yaml that is not a subchart
	// of another one — a subchart is rendered by its parent.
	Charts []string
	// Kustomizations are the Kustomize roots nothing else references: the
	// overlays. A base is rendered through the overlays that use it, and
	// rendering it alone would validate a configuration nobody deploys.
	Kustomizations []string
}

// Discover walks root and sorts what it finds into a Layout.
//
// A file inside a chart or under a Kustomize root is never a plain manifest:
// a template is not YAML until helm renders it, and a Kustomize patch is a
// fragment that fails schema validation on its own — it is only a resource
// once kustomize has merged it. Both are validated after rendering, or not at
// all when the tool that renders them is absent.
//
// Which YAML files are manifests is decided by content, not by name: a
// document with an apiVersion and a kind. That is what Trivy does too, and
// what keeps a CI configuration or a docker-compose file out.
func Discover(root string) (Layout, error) {
	var charts, kustomizeRoots, candidates []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable entry costs itself, not the walk; only the root
			// failing means there is nothing to discover.
			if path == root {
				return err
			}
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || skippedDirs[name]) {
				return filepath.SkipDir
			}
			if fileExists(filepath.Join(path, "Chart.yaml")) {
				charts = append(charts, path)
			}
			if kustomizationIn(path) != "" {
				kustomizeRoots = append(kustomizeRoots, path)
			}
			return nil
		}
		if isYAMLName(d.Name()) {
			candidates = append(candidates, path)
		}
		return nil
	})
	if err != nil {
		return Layout{}, err
	}

	var layout Layout
	for _, c := range outermost(charts) {
		layout.Charts = append(layout.Charts, rel(root, c))
	}
	for _, k := range leaves(kustomizeRoots) {
		layout.Kustomizations = append(layout.Kustomizations, rel(root, k))
	}
	excluded := append(append([]string{}, charts...), kustomizeRoots...)
	for _, path := range candidates {
		if under(path, excluded) {
			continue
		}
		if content, err := readBounded(path); err == nil && IsManifest(content) {
			layout.Manifests = append(layout.Manifests, rel(root, path))
		}
	}
	sort.Strings(layout.Manifests)
	sort.Strings(layout.Charts)
	sort.Strings(layout.Kustomizations)
	return layout, nil
}

// IsManifest reports whether any document of a YAML file has an apiVersion
// and a kind at its top level. A file that does not parse is not one: it
// cannot be applied either, but saying so is a linter's job, and the file may
// well not be meant for Kubernetes at all.
func IsManifest(content []byte) bool {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			return false
		}
		if _, ok := doc["apiVersion"].(string); !ok {
			continue
		}
		if _, ok := doc["kind"].(string); ok {
			return true
		}
	}
}

// KustomizationFile returns the name of dir's Kustomize file, or "" when dir
// is not a Kustomize root.
func KustomizationFile(dir string) string {
	if p := kustomizationIn(dir); p != "" {
		return filepath.Base(p)
	}
	return ""
}

// kustomizationIn returns the path of dir's Kustomize file, or "".
func kustomizationIn(dir string) string {
	for _, name := range kustomizationFiles {
		p := filepath.Join(dir, name)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// kustomizationRefs are the fields through which one root pulls in another.
type kustomizationRefs struct {
	Resources  []string `yaml:"resources"`
	Bases      []string `yaml:"bases"`
	Components []string `yaml:"components"`
}

// leaves drops every Kustomize root another one references. A reference that
// names a file rather than a directory, or a remote URL, references no root
// and is ignored; a kustomization that does not parse references nothing and
// stays a leaf, so kustomize itself gets to say what is wrong with it.
func leaves(roots []string) []string {
	referenced := map[string]bool{}
	for _, r := range roots {
		content, err := readBounded(kustomizationIn(r))
		if err != nil {
			continue
		}
		var refs kustomizationRefs
		if yaml.Unmarshal(content, &refs) != nil {
			continue
		}
		for _, list := range [][]string{refs.Resources, refs.Bases, refs.Components} {
			for _, ref := range list {
				if strings.Contains(ref, "://") {
					continue
				}
				referenced[filepath.Clean(filepath.Join(r, ref))] = true
			}
		}
	}
	var out []string
	for _, r := range roots {
		if !referenced[filepath.Clean(r)] {
			out = append(out, r)
		}
	}
	return out
}

// outermost drops every chart nested in another: helm renders a subchart as
// part of its parent, with the parent's values.
func outermost(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		nested := false
		for _, other := range dirs {
			if other != d && isUnder(d, other) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, d)
		}
	}
	return out
}

func under(path string, dirs []string) bool {
	for _, d := range dirs {
		if isUnder(path, d) {
			return true
		}
	}
	return false
}

// isUnder reports whether path lies inside dir, dir itself excluded.
func isUnder(path, dir string) bool {
	r, err := filepath.Rel(dir, path)
	return err == nil && r != "." && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(r)
}

func isYAMLName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// readBounded reads a file unless it is larger than maxManifestSize.
func readBounded(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxManifestSize {
		return nil, errors.New("too large to be a hand-written manifest")
	}
	return os.ReadFile(path) //nolint:gosec // a file of the repository being scanned
}
