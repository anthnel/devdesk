package containers

import (
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
)

// OC-84 (DEVDESK-801, requirement S1). `V` on a container log builds the `logs`
// command in internal/docker (docker.LogsCmd: an argv, the ID validated) and
// the viewer pipes it into the pager. logsSource no longer carries PagerCmd,
// which built a string for `sh -c`; it carries
//
//	func (s logsSource) PagerFeed() (*exec.Cmd, error)
//
// which is docker.LogsCmd(s.ID, logsTail) and nothing more. The command is built,
// never started here, and PATH is emptied so that nothing could be found if it
// were: no test of this file reaches a real engine.

func noEngineOnPath(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
}

func TestPagerFeedIsTheLogsArgvOfTheEngineInUse(t *testing.T) {
	const id = "aaaa111122223333"
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			noEngineOnPath(t)
			shape := engine.ShapeFor(name)
			engine.SetCurrent(shape)

			cmd, err := logsSource{ID: id, Container: "web"}.PagerFeed()
			if err != nil {
				t.Fatalf("PagerFeed() error = %v", err)
			}

			if got := filepath.Base(cmd.Args[0]); got != shape.Binary {
				t.Errorf("PagerFeed runs %q, want the engine's binary %q", got, shape.Binary)
			}
			want := []string{"logs", "--tail", strconv.Itoa(logsTail), id}
			if !slices.Equal(cmd.Args[1:], want) {
				t.Errorf("PagerFeed argv = %q, want %q", cmd.Args[1:], want)
			}
			if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
				t.Error("PagerFeed set a stream; the pipe to the pager is the caller's")
			}
		})
	}
}

// The container ID comes from `ps`, but it is validated where it is used: a
// refused ID launches nothing, and the error is the one the footer reports.
func TestPagerFeedRefusesAMalformedID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"shell metacharacters", "abc; rm -rf ~"},
		{"command substitution", "$(touch /tmp/pwned)"},
		{"leading dash", "-0123456789ab"},
		{"an engine flag", "--follow"},
		{"empty", ""},
		{"too short, the fixture that used to be accepted", "abc123"},
	}
	for _, name := range engine.Names() {
		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				noEngineOnPath(t)
				engine.SetCurrent(engine.ShapeFor(name))

				cmd, err := logsSource{ID: tt.id, Container: "web"}.PagerFeed()

				if cmd != nil {
					t.Errorf("PagerFeed built %q for the ID %q, want no command", cmd.Args, tt.id)
				}
				if !errors.Is(err, docker.ErrInvalidContainerID) {
					t.Errorf("PagerFeed error = %v, want docker.ErrInvalidContainerID", err)
				}
			})
		}
	}
}

// The command string for a shell is gone, not hidden behind the new method:
// anything still offering PagerCmd would be a second way to page.
func TestTheLogSourceNoLongerBuildsAShellCommand(t *testing.T) {
	if _, ok := any(logsSource{}).(interface{ PagerCmd() *exec.Cmd }); ok {
		t.Error("logsSource still has PagerCmd, which builds a command for a shell")
	}
}
