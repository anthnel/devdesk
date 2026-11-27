package enginefixture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

var errSourceTree = errors.New("enginefixture: cannot locate the source tree")

// SourceDir is the fixtures directory in the source tree, where the importer writes
// (the embedded copy is read-only). It never answers with a path that is not there:
// under -trimpath runtime.Caller gives a module-relative path, so the answer is then
// an empty path and an error that names the source tree.
func SourceDir() (string, error) {
	here, err := sourceDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(here, fixturesDir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: %s is not a directory", errSourceTree, dir)
	}
	return dir, nil
}

// VersionsSourceFile is tools/engine-vms/versions.env in the source tree, the one
// place that pins the engine versions (see ParseVersions). Like SourceDir it answers
// an empty path and an error when the source tree cannot be located.
func VersionsSourceFile() (string, error) {
	here, err := sourceDir()
	if err != nil {
		return "", err
	}
	file := filepath.Join(here, "..", "..", "tools", "engine-vms", "versions.env")
	if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a file", errSourceTree, file)
	}
	return filepath.Clean(file), nil
}

// sourceDir is the directory of this package in the source tree.
func sourceDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		return "", errSourceTree
	}
	return filepath.Dir(file), nil
}
