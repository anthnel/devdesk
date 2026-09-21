package git

import (
	"path/filepath"
	"strings"
)

// FileState is what git knows about one file: whether an edit to it can be
// undone with git, and whether that undoing would also take the user's own
// uncommitted work.
type FileState int

const (
	// FileOutsideRepo: the file is not in a git working copy. Nothing records
	// what it held before.
	FileOutsideRepo FileState = iota
	// FileUntracked: in a working copy, but git has never been told about it (or
	// ignores it). No history holds its previous content either.
	FileUntracked
	// FileModified: tracked, with changes not yet committed. `git checkout` would
	// undo an edit and the user's own changes together.
	FileModified
	// FileClean: tracked and identical to HEAD. Any edit can be seen with
	// `git diff` and undone with `git checkout`.
	FileClean
)

// StateOf reports what git knows about the file at path.
//
// A directory that is not a working copy is an answer (FileOutsideRepo), not an
// error; anything else git refuses — a repository it will not open, git missing
// — is returned, because it is not the same fact and reading it as "outside a
// repository" would tell the user their file has no safety net when it might.
func StateOf(path string) (FileState, error) {
	dir, name := filepath.Split(path)
	if dir == "" {
		dir = "."
	}

	if _, err := run(dir, "", "rev-parse", "--git-dir"); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
			return FileOutsideRepo, nil
		}
		return FileOutsideRepo, err
	}
	if _, err := run(dir, "", "ls-files", "--error-unmatch", "--", name); err != nil {
		// A path git does not track is a failure of the command, by design.
		return FileUntracked, nil
	}
	out, err := run(dir, "", "status", "--porcelain", "--", name)
	if err != nil {
		return FileClean, err
	}
	if strings.TrimSpace(out) != "" {
		return FileModified, nil
	}
	return FileClean, nil
}
