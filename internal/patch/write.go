package patch

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrChanged is returned by WriteIfUnchanged when the file no longer holds what
// the edit was computed from.
var ErrChanged = errors.New("the file changed since it was read")

// WriteIfUnchanged replaces the file at path with updated, provided it still
// holds exactly expected — the content the change was computed from, and the one
// the user was shown and agreed to. Anything else is ErrChanged and the file is
// left alone: the user confirmed *that* diff, not another.
//
// The write goes through a temporary file in the same directory, then a rename,
// so a reader never sees half a file and a failure leaves the original in
// place. The file's permission bits are kept; a symbolic link is followed and
// its target replaced, not the link. The owner is not preserved when the process
// cannot chown, which is the ordinary case.
func WriteIfUnchanged(path string, expected, updated []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return ErrChanged
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".devdesk-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := tmp.Write(updated); err != nil {
		return fail(fmt.Errorf("write %s: %w", name, err))
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
