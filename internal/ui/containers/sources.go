package containers

import (
	"fmt"
	"os/exec"
	"runtime"

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

// FollowCmd streams live output. It suspends the TUI (tea.ExecProcess) rather
// than tailing into the viewport: following is what `docker logs -f` already
// does, and re-implementing it against a viewport would be re-implementing
// `less +F` badly.
func (s logsSource) FollowCmd() *exec.Cmd {
	args := []string{"logs", "-f", "--tail", fmt.Sprint(logsTail)}
	if s.Timestamps {
		args = append(args, "--timestamps")
	}
	return exec.Command("docker", append(args, s.ID)...)
}

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
