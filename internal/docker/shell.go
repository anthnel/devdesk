package docker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// The two shells an interactive session can open in a running container.
const (
	shellBash = "/bin/bash"
	shellSh   = "/bin/sh"
)

// ErrInvalidShell is wrapped by ShellCmd when the shell is neither of the two
// above: the session only ever starts what DetectShell answers.
var ErrInvalidShell = errors.New("invalid shell")

// shellDetectTimeout bounds the detection: a daemon that does not answer must
// not keep the footer on "Opening shell…" for good. A variable so a test can
// shorten it; production code never assigns it.
var shellDetectTimeout = 8 * time.Second

// DetectShell returns "/bin/bash" when the container has bash, else "/bin/sh".
//
// A malformed container ID is refused first (ErrInvalidContainerID, an empty
// shell): nothing is built or run for it. A bash that is missing is not an
// error, it is the "/bin/sh" fallback.
//
// It runs `<engine> exec <id> which bash` and therefore waits on the daemon:
// call it from a tea.Cmd, never from Update (Rule 110). Any failure of that
// probe, an absent bash as much as an unreachable engine or no answer within
// shellDetectTimeout, answers "/bin/sh": the session that follows reports the
// real problem in its own terminal.
func DetectShell(id string) (string, error) {
	if err := requireContainerID("exec", id); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), shellDetectTimeout)
	defer cancel()
	if _, err := runner.Run(dockerCmd{Args: []string{"exec", id, "which", "bash"}, Ctx: ctx}); err != nil {
		return shellSh, nil
	}
	return shellBash, nil
}

// ShellCmd returns the ready-to-run `<engine> exec -it <id> <shell>` without
// starting it, for a caller that hands the process to a terminal. Its Args are
// the full argv (the engine binary first), which is what a terminal emulator
// launched in a new window is given: the session is an argv, never an
// interpreter's command line. A malformed container ID (ErrInvalidContainerID)
// or a shell other than /bin/bash and /bin/sh (ErrInvalidShell) builds nothing.
func ShellCmd(id, shell string) (*exec.Cmd, error) {
	if err := requireContainerID("exec", id); err != nil {
		return nil, err
	}
	if shell != shellBash && shell != shellSh {
		return nil, fmt.Errorf("%s: %q is not a shell DevDesk opens: %w", cmdLabel("exec"), shell, ErrInvalidShell)
	}
	return runner.Build("exec", "-it", id, shell), nil
}
