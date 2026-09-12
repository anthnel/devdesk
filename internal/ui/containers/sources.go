package containers

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/viewer"
)

// The viewer's sources for this view. They live here rather than in
// internal/viewer because they know docker, and that package must not: it
// parses documents, and a domain package that reached for a container runtime
// would be answering a question nobody asked it.

// logsTail is how much of a container's log is fetched. It is the number the
// pane has always used, and it is a fetch bound rather than a display one: the
// viewer's size cap applies to files, whose size nothing else limits.
const logsTail = 500

// inspectSource is `docker inspect` for one container.
type inspectSource struct {
	ID        string
	Container string
}

func (s inspectSource) Name() string { return "inspect · " + s.Container }

func (s inspectSource) Kind() viewer.Kind { return viewer.KindJSON }

func (s inspectSource) Load() ([]byte, error) { return docker.InspectContainer(s.ID) }

// logsSource is `docker logs` for one container.
//
// It implements all three optional capabilities, and is the only source that
// does — which is what the interfaces exist to express: `t`, `ctrl+f` and `e`
// appear for this document and for no other.
type logsSource struct {
	ID         string
	Container  string
	Timestamps bool
}

func (s logsSource) Name() string { return "logs · " + s.Container }

func (s logsSource) Kind() viewer.Kind { return viewer.KindLog }

func (s logsSource) Load() ([]byte, error) {
	out, err := docker.GetContainerLogs(s.ID, logsTail, s.Timestamps)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// WithTimestamps returns a source rather than mutating this one: a command may
// already be in flight against the old value, and handing it a struct that
// changes under it is the race Rule 110 is about.
func (s logsSource) WithTimestamps(on bool) viewer.Source {
	s.Timestamps = on
	return s
}

// followInterval is how often a followed log is re-read.
//
// Two seconds, the ports tab's cadence, and for the same reason: it is about
// the slowest a live pane can refresh and still read as live. A re-read is one
// `docker logs --tail 500` — the very exec `ctrl+r` already runs — so following
// costs one process every two seconds while the user is watching it, and
// nothing at all once they stop.
const followInterval = 2 * time.Second

// FollowInterval makes a container log followable.
//
// This was FollowCmd, handing the terminal to `docker logs -f` through
// tea.ExecProcess. The reasoning was that following is what docker already
// does — true, and beside the point: the only way out of `docker logs -f` is
// ctrl+c, the suspended TUI never sees it, so it killed DevDesk outright and
// gave the terminal back in whatever mode the child had left it, with keys no
// longer answering. A capability whose only exit kills the application is not
// one.
func (s logsSource) FollowInterval() time.Duration { return followInterval }

// PagerCmd hands the log to the system pager.
//
// This is the one pager path the viewer keeps, and it is kept for a reason the
// viewport cannot answer: `less` handles a gigabyte and follows it, while the
// viewport holds the whole document in memory.
//
// **No quotes, on either branch, and that is load-bearing on Windows.** The
// Windows branch used to write to a temp file and page that:
//
//	docker logs --tail 500 ID > "%TEMP%\devdesk-logs.txt" 2>&1 && more "%TEMP%\devdesk-logs.txt"
//
// It never worked. Go's exec.Command escapes an argument's inner quotes as `\"`
// when it builds the Windows command line, and cmd.exe does not understand that
// escaping — it sees the backslashes as part of the path. The result was
// `C:\C:\Users\...\devdesk-logs.txt\`, cmd answering "the filename, directory
// name or volume label syntax is incorrect", and the pager returning to DevDesk
// instantly with an exit status nobody rendered. Measured, by running the exact
// string through exec.Command.
//
// The temp file went with it, because the reason given for it was not true:
// `more` reads a pipe perfectly well — `dir | more` is its canonical use — so
// the two branches now differ only in the shell and the pager's name. Nothing
// is written to disk, and no path needs quoting.
//
// The container ID is interpolated into a shell string, which is only safe
// because it comes from the engine's own `ps`. Do not extend this to a value
// the user types.
//
// The engine binary is interpolated too, and deliberately **not** quoted:
// TestThePagerCommandCarriesNoQuote forbids a quote anywhere in this command,
// because exec.Command escapes it and cmd.exe does not understand that — a
// debugging session's worth of reason, and not one to undo for this.
//
// The stated cost: an app.container_engine set to a path containing a space
// breaks `V` here, and only here — every other invocation goes through
// exec.Command, which needs no quoting at all. A name (docker, podman) and any
// ordinary path are unaffected.
func (s logsSource) PagerCmd() *exec.Cmd {
	bin := engine.Current().Binary
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c",
			fmt.Sprintf("%s logs --tail %d %s 2>&1 | more", bin, logsTail, s.ID))
	}
	return exec.Command("sh", "-c",
		fmt.Sprintf("%s logs --tail %d %s 2>&1 | ${PAGER:-less} -R", bin, logsTail, s.ID))
}
