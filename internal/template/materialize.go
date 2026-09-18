package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScanDir is where a template is written to be scanned: one fixed directory per
// slug under ~/.devdesk/cache/template-scan.
//
// Fixed rather than temporary because the scan is cached by path — the result
// shows up in `:sec` under the directory it was made from, and a fresh temp
// directory each time would leave a dead entry behind for every scan.
func ScanDir(slug string) (string, error) {
	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("slug %q must be lowercase letters, digits and dashes", slug)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".devdesk", "cache", "template-scan", slug), nil
}

// Materialize writes a template's files to its ScanDir, replacing whatever a
// previous scan left there, and returns the directory.
//
// Only that directory is ever removed: it is computed here from a validated
// slug, never passed in, so nothing a caller does can point the removal
// elsewhere. A file whose path would land outside it is refused — the readers
// already refuse those, and this is the second lock on the door that matters
// most, the one that writes to disk.
func Materialize(slug string, files []File) (string, error) {
	dir, err := ScanDir(slug)
	if err != nil {
		return "", err
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	for _, f := range files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if rel, err := filepath.Rel(dir, target); err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("template file %q would be written outside its directory", f.Path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		mode := os.FileMode(0o600)
		if f.Executable {
			mode = 0o700
		}
		if err := os.WriteFile(target, f.Content, mode); err != nil {
			return "", err
		}
	}
	return dir, nil
}
