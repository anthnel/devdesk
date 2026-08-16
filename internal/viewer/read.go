package viewer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
)

// MaxSize is the largest file the viewer will open. Everything above it is
// refused by name rather than loaded and then survived.
const MaxSize = 5 << 20 // 5 MiB

// sniffSize is how much of a file decides whether it is text.
const sniffSize = 8 << 10

// ErrTooLarge and ErrBinary are the two refusals. They are values rather than
// formatted strings so the caller can report them without matching on prose.
var (
	ErrTooLarge = errors.New("file is too large to view")
	ErrBinary   = errors.New("not a text file")
)

// ReadFile loads a file for the viewer, refusing the two things it cannot show.
//
// The cap is a *file* concern and lives here rather than in a producer: a
// source that fetches — `docker logs --tail 500` — is bounded by its own
// command, and applying a byte ceiling to it would be answering a question
// nobody asked.
func ReadFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > MaxSize {
		return nil, ErrTooLarge
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if IsBinary(data) {
		return nil, ErrBinary
	}
	return data, nil
}

// IsBinary reports whether data looks like something a text pane must not try
// to render.
//
// Two signals, and the first one catches nearly everything: a NUL byte, which
// no text encoding this application will meet uses, and which every executable,
// archive and image carries early. The second is a density of other control
// bytes, for the formats that manage to avoid NUL in their first pages.
func IsBinary(data []byte) bool {
	head := data
	if len(head) > sniffSize {
		head = head[:sniffSize]
	}
	if len(head) == 0 {
		return false
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}

	control := 0
	for _, b := range head {
		if b < 0x20 && b != '\n' && b != '\t' && b != '\r' && b != 0x1b {
			control++
		}
	}
	return control*100/len(head) > 30
}
