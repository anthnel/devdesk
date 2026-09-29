// Package fsbrowse is the filesystem half of the file browser (§3.95): listing a
// directory, creating and removing an entry, and the few path rules the views
// that browse a tree must answer the same way.
//
// It holds no git knowledge on purpose. The workspaces view enriches its rows
// with repository state, and that walk is expensive; a file browser that lists
// /usr/lib has no use for it. What the two share is what a row *is* — hidden or
// not, directory or not — and that is what lives here.
package fsbrowse

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one row of a listing.
type Entry struct {
	Name    string
	Path    string
	IsDir   bool
	Size    int64
	ModTime time.Time
	Mode    fs.FileMode
}

// IsHidden decides what a listing leaves out.
//
// One rule, consulted by every listing and walk alike: the workspaces view
// relies on the rows it shows being exactly what S and F act on, and two rules
// for that question would let a repository be a visible row and an invisible
// target at once.
func IsHidden(name string, showHidden bool) bool {
	return !showHidden && strings.HasPrefix(name, ".")
}

// LeadsToDir reports whether a listing entry leads to a directory, following a
// symbolic link or a Windows junction to answer.
//
// DirEntry.IsDir() reports on the link itself, so it said false for a junction
// pointing at a directory full of repositories: everything behind it was
// invisible to S, F and A, and the row rendered as a file — neither browsable
// nor scannable, with nothing saying why (§1.3 D59, cause 2).
//
// The test is "not a plain file" rather than "is a symlink" on purpose: Go has
// reported a Windows junction as ModeSymlink and as ModeIrregular depending on
// the version, and os.Stat answers the same either way. A regular file costs no
// syscall, which is what keeps this affordable on a tree with no links in it.
func LeadsToDir(e os.DirEntry, path string) bool {
	if e.IsDir() {
		return true
	}
	if e.Type().IsRegular() {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// List reads dir, directories first, then by name without regard to case.
//
// An entry whose Info cannot be read (removed between ReadDir and Stat) is
// skipped rather than failing the listing: the directory was readable, and one
// racing file is not a reason to show nothing.
func List(dir string, showHidden bool) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(dirEntries))
	for _, d := range dirEntries {
		if IsHidden(d.Name(), showHidden) {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, d.Name())
		entries = append(entries, Entry{
			Name:    d.Name(),
			Path:    path,
			IsDir:   LeadsToDir(d, path),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Mode:    info.Mode(),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// Validation errors, written to be read in a footer.
var (
	ErrEmptyName     = errors.New("name is empty")
	ErrReservedName  = errors.New(`"." and ".." are not names`)
	ErrSeparatorName = errors.New("a name cannot contain a path separator")
	ErrInvalidChar   = errors.New("a name cannot contain a NUL character")
)

// ValidName checks a single path segment — what a user types to name a new
// entry inside the directory being browsed.
//
// A separator is refused, not interpreted: "a/b" would create a directory the
// user did not see being created, and "../x" would write outside the directory
// the screen says it writes into.
func ValidName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return ErrEmptyName
	case name == "." || name == "..":
		return ErrReservedName
	case strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator):
		return ErrSeparatorName
	case strings.ContainsRune(name, 0):
		return ErrInvalidChar
	}
	return nil
}

// Create makes an empty file or a directory named name inside dir, and returns
// its path. It never overwrites: an existing entry of either kind is an error.
func Create(dir, name string, isDir bool) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if isDir {
		// Mkdir, not MkdirAll: the parent is the directory on screen, and a
		// missing one means the listing is stale — better said than papered over.
		return path, os.Mkdir(path, 0o755)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return path, err
	}
	return path, f.Close()
}

// ErrProtectedPath is returned by Remove for a filesystem root or the home
// directory: no confirmation text is worth that outcome.
var ErrProtectedPath = errors.New("refusing to delete a root or the home directory")

// Remove deletes path, recursively for a directory.
func Remove(path string) error {
	if Protected(path) {
		return ErrProtectedPath
	}
	return os.RemoveAll(path)
}

// Count returns how many entries a directory holds directly, hidden included —
// what a deletion would take with it at the first level.
func Count(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// Protected reports whether Remove refuses path: a root or the home directory.
// A view greys its delete from the same answer (Rule 130).
func Protected(path string) bool {
	return IsRoot(path) || isHome(path)
}

// IsRoot reports whether path is a filesystem root: "/" or a volume ("C:\").
func IsRoot(path string) bool {
	clean := filepath.Clean(path)
	return filepath.Dir(clean) == clean
}

func isHome(path string) bool {
	home, err := os.UserHomeDir()
	return err == nil && home != "" && filepath.Clean(path) == filepath.Clean(home)
}

// ExpandHome turns a leading "~" or "~/" into the home directory. Anything else,
// including "~user", is returned unchanged.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	return filepath.Join(home, path[1:])
}

// NearestExisting returns path itself when it exists, otherwise its closest
// existing ancestor, and "" when path is empty or nothing along it exists.
func NearestExisting(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	p := filepath.Clean(ExpandHome(path))
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}

// Describe names what an entry is, for a confirmation title.
func Describe(isDir bool) string {
	if isDir {
		return "Directory"
	}
	return "File"
}

// HumanSize renders a byte count in the units ls -h uses.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
