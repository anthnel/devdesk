package workspaces

import (
	"github.com/anthnel/devdesk/internal/ui/terminal"
)

// detectTerminalCmd delegates to the shared terminal package.
func detectTerminalCmd(path string) (string, []string, bool) {
	return terminal.ForDir(path)
}

// detectShell delegates to the shared terminal package.
func detectShell() string {
	return terminal.Shell()
}
