package viewer

import (
	"os/exec"
	"path/filepath"
	"time"
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

// Followable is a source worth re-reading on a clock. It unlocks `F`.
//
// It returns an interval rather than a command, and that is the whole of the
// fix. Following used to hand the terminal to `docker logs -f` through
// tea.ExecProcess, and there is no way out of that process but ctrl+c — which
// the suspended TUI does not intercept, so it killed DevDesk outright and left
// the terminal in whatever mode the child had put it in. Keys stopped answering
// afterwards. Observed, not theorised.
//
// A follow is therefore a re-read on a timer, inside the viewport, which is
// exactly what the ports tab already does with `ss`. The source names the
// cadence because only it knows what a read costs: a `docker logs --tail 500`
// is not a file stat.
//
// The trade is stated rather than discovered: this polls, it does not stream.
// A line can wait up to one interval, and a burst longer than the source's tail
// is missed between two reads. `V` still hands the whole thing to the pager,
// which does stream — and which the user leaves with `q` rather than ctrl+c.
type Followable interface {
	Source
	FollowInterval() time.Duration
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

// Pathed is a source backed by a real file on disk. It unlocks `O`, handing
// the path to the configured IDE — an inspect or a container log has nothing
// on disk an editor could open, so FileSource is the only implementer.
type Pathed interface {
	Source
	Path() string
}

// FileSource reads a file from disk. It is the workspaces view's source, and
// the only one in this package: the others know docker, and this package must
// not.
type FileSource struct {
	path string
}

// NewFileSource builds a source for a path on disk.
func NewFileSource(path string) FileSource {
	return FileSource{path: path}
}

func (s FileSource) Name() string { return filepath.Base(s.path) }

// Kind is KindAuto: a file browser does not know what it is opening, so the
// extension and then the content decide.
func (s FileSource) Kind() Kind { return KindAuto }

func (s FileSource) Load() ([]byte, error) { return ReadFile(s.path) }

// Path satisfies Pathed.
func (s FileSource) Path() string { return s.path }
