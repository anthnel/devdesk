package agentusage

import (
	"errors"
	"os"
)

// errNotPlain is returned when a path is not what Lstat said it was a moment
// ago (swapped for a link or another file). It is fixed so it never carries
// a path or content.
var errNotPlain = errors.New("agentusage: path changed while being opened")

// errNotRegular is returned when a path is a symbolic link or not a regular
// file as seen by Lstat.
var errNotRegular = errors.New("agentusage: not a regular file")

// openPlain opens path only if it is not a symbolic link and its mode, as
// reported by Lstat, matches wantDir. The opened handle is compared with the
// Lstat result (os.SameFile), which closes the window between checking and
// opening. On Windows the file identity is read lazily, at the SameFile call.
// A missing path yields an error satisfying os.ErrNotExist; a path of the
// wrong kind yields (nil, nil).
//
// Known limit, accepted: a regular file swapped for a FIFO between Lstat and
// Open makes os.Open block until a writer appears, before the SameFile check
// can run. Closing it would need O_NONBLOCK, which the os package does not
// offer portably. The window is tiny and the attacker must already control
// the transcripts directory.
func openPlain(path string, wantDir bool) (*os.File, error) {
	li, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if li.IsDir() != wantDir || (!wantDir && !li.Mode().IsRegular()) {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil || !os.SameFile(li, fi) {
		_ = f.Close()
		if err == nil {
			err = errNotPlain
		}
		return nil, err
	}
	return f, nil
}
