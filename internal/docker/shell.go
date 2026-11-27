package docker

import (
	"context"
	"os/exec"
	"time"
)

// The two shells an interactive session can open in a running container.
const (
	shellBash = "/bin/bash"
	shellSh   = "/bin/sh"
)

// shellDetectTimeout bounds the detection: a daemon that does not answer must
// not keep the footer on "Opening shell…" for good. A variable so a test can
// shorten it; production code never assigns it.
var shellDetectTimeout = 8 * time.Second

// DetectShell returns "/bin/bash" when the container has bash, else "/bin/sh".
//
// It runs `<engine> exec <id> which bash` and therefore waits on the daemon:
// call it from a tea.Cmd, never from Update (Rule 110). Any failure, an absent
// bash as much as an unreachable engine or no answer within shellDetectTimeout,
// answers "/bin/sh": the session that follows reports the real problem in its
// own terminal.
func DetectShell(id string) string {
	ctx, cancel := context.WithTimeout(context.Background(), shellDetectTimeout)
	defer cancel()
	if _, err := runner.Run(dockerCmd{Args: []string{"exec", id, "which", "bash"}, Ctx: ctx}); err != nil {
		return shellSh
	}
	return shellBash
}

// ShellCmd returns the ready-to-run `<engine> exec -it <id> <shell>` without
// starting it, for a caller that hands the process to a terminal. Its Args are
// the full argv (the engine binary first), which is what a terminal emulator
// launched in a new window is given.
func ShellCmd(id, shell string) *exec.Cmd {
	return runner.Build("exec", "-it", id, shell)
}
