package containers

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
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
// viewport holds the whole document in memory. The Windows branch writes to a
// temp file first because `more` cannot read a pipe the way `less` can — it is
// the shape the logs pane already used, and the only place it survives.
func (s logsSource) PagerCmd() *exec.Cmd {
	if runtime.GOOS == "windows" {
		script := fmt.Sprintf(
			`docker logs --tail %d %s > "%%TEMP%%\devdesk-logs.txt" 2>&1 && more "%%TEMP%%\devdesk-logs.txt"`,
			logsTail, s.ID)
		return exec.Command("cmd", "/c", script)
	}
	return exec.Command("sh", "-c",
		fmt.Sprintf("docker logs --tail %d %s 2>&1 | ${PAGER:-less} -R", logsTail, s.ID))
}
