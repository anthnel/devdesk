package dockerfile

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// maxFindDepth bounds how far below the root a Dockerfile is looked for:
	// a monorepo keeps them a few levels down, and a tree deeper than that is
	// vendored or generated.
	maxFindDepth = 4
	// maxFound bounds the answer, so a checkout that vendors hundreds of
	// example Dockerfiles cannot turn one keypress into hundreds of registry
	// requests.
	maxFound = 50
)

// skippedDirs are never entered: what a package manager or a VCS put there is
// not the project's own build.
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".worktrees": true,
	".venv": true, "venv": true, "target": true, "dist": true, "build": true,
}

// IsDockerfileName reports whether a file name is one a container build reads:
// Dockerfile or Containerfile, with a suffix (Dockerfile.dev) or a prefix
// (api.Dockerfile). It is by name and never by content, as the viewer's kind
// detection is.
func IsDockerfileName(name string) bool {
	lower := strings.ToLower(name)
	for _, base := range []string{"dockerfile", "containerfile"} {
		if lower == base || strings.HasPrefix(lower, base+".") || strings.HasSuffix(lower, "."+base) {
			return true
		}
	}
	return false
}

// Find lists the Dockerfiles under root, as slash-separated paths relative to
// it, in path order. It reports whether the answer was cut at the bound.
func Find(root string) (paths []string, truncated bool, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			return nil // an unreadable subdirectory is skipped, not fatal
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if d.IsDir() {
			if rel != "." && (skippedDirs[d.Name()] || strings.Count(rel, string(filepath.Separator)) >= maxFindDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if IsDockerfileName(d.Name()) {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	sort.Strings(paths)
	if len(paths) > maxFound {
		return paths[:maxFound], true, nil
	}
	return paths, false, nil
}
