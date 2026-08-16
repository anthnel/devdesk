package viewer

import (
	"os/exec"
	"path/filepath"
)

// Source is where a document came from, and it is carried rather than the bytes
// alone.
//
// The reason is everything the container logs pane could do that a byte slice
// cannot: reload, follow, re-fetch with timestamps. Those are properties of the
// *origin*, not of the pane — and a file gains from it too, since a document
// re-read after an external edit is the same question as a log re-fetched.
type Source interface {
	// Name is what the header shows.
	Name() string
	// Kind is what the producer declares. KindAuto lets the name and the
	// content decide, which is what a file browser passes.
	Kind() Kind
	// Load fetches the content. It runs on a Cmd's goroutine, never in Update
	// (Rule 110).
	Load() ([]byte, error)
}

// The three optional capabilities below are probed with a type assertion, the
// same way the router probes FooterView and FramelessView.
//
// Each is a *single* method on purpose. CLAUDE.md's warning about HeaderView is
// that a view supplying two of its four methods satisfies none of it and fails
// in silence; an interface with one method has no half-satisfied state to fall
// into. That is why these are three interfaces and not one with three methods.

// Timestamped is a source that can re-fetch with or without timestamps. It
// unlocks `t`.
type Timestamped interface {
	Source
	WithTimestamps(on bool) Source
}

// Followable is a source that can stream live output. It unlocks `ctrl+f`.
//
// It returns a command rather than a stream because following suspends the TUI
// and hands the terminal over (tea.ExecProcess): a viewport that tried to tail
// a stream would be re-implementing `less +F` badly.
type Followable interface {
	Source
	FollowCmd() *exec.Cmd
}

// Pageable is a source that can be handed to the system pager. It unlocks `e`.
//
// It survives for logs alone, and deliberately: `less` handles a gigabyte and
// follows it, which a viewport holding the whole document in memory will never
// do. Every other pager path in the application is deleted by this view.
type Pageable interface {
	Source
	PagerCmd() *exec.Cmd
}

// FileSource reads a file from disk. It is the workspaces view's source, and
// the only one in this package: the others know docker, and this package must
// not.
type FileSource struct {
	Path string
}

// NewFileSource builds a source for a path on disk.
func NewFileSource(path string) FileSource {
	return FileSource{Path: path}
}

func (s FileSource) Name() string { return filepath.Base(s.Path) }

// Kind is KindAuto: a file browser does not know what it is opening, so the
// extension and then the content decide.
func (s FileSource) Kind() Kind { return KindAuto }

func (s FileSource) Load() ([]byte, error) { return ReadFile(s.Path) }
