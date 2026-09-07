package metrics

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// Size walks a directory and adds up what its files occupy.
//
// **This is the one call in the package whose cost grows with the user's
// data**: it reads every entry of every directory. So it runs on the slow
// clock, on a Cmd's goroutine, and never two at once — see
// Model.measuringSize, which exists for that and nothing else.
//
// Symlinks are not followed: WalkDir reads the link itself, not its
// target, which avoids both cycles and double-counting a directory already
// in the tree in one move.
//
// Nothing is excluded from the walk, `.git` included: a cloned repository
// costs its history as much as its working tree, and it is often the
// history that weighs the most. The question asked is "how much does this
// directory take", not "how much code does it contain".
func Size(path string) TreeSize {
	out := TreeSize{Path: path}
	if path == "" {
		return out
	}

	// The root is checked separately: without this, an unreadable path
	// would yield a "successful" measurement of zero bytes, which reads as
	// an empty directory.
	if _, err := os.Stat(path); err != nil {
		log.Printf("ERROR [metrics/tree] stat %q: %v", path, err)
		return out
	}

	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory refused or gone partway through does not stop the
			// measurement: it makes it partial.
			out.Partial = true
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Only regular files carry bytes. A directory returns nil so the walk
		// descends into it; a symlink, on the other hand, must not count
		// either way: what WalkDir measures on a link is the length of the
		// path it designates, so the tree's size would move with a rename
		// elsewhere. Its target is not counted either — that is the whole
		// point of not following it.
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			out.Partial = true
			return nil
		}
		if size := info.Size(); size > 0 {
			out.Bytes += uint64(size)
		}
		return nil
	})
	if err != nil {
		log.Printf("ERROR [metrics/tree] walking %q: %v", path, err)
		return TreeSize{Path: path}
	}

	out.OK = true
	return out
}
