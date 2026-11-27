package docker

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// A process that never started has no exit code. The log still says why in a
// fixed phrase, without the error text (it can name an argument).
func TestALaunchThatCouldNotStartLogsAFixedCause(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		stub(t, &stubRunner{err: map[string]error{
			"run": &exec.Error{Name: shape.Binary, Err: errors.New("permission denied for canary-arg")},
		}})

		_, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Env: []string{"A=canary"}})

		want := shape.Name + " run failed (could not start the process)"
		if got := LaunchLogText(err); got != want {
			t.Errorf("LaunchLogText() = %q, want %q", got, want)
		}
		if strings.Contains(LaunchLogText(err), "canary") {
			t.Error("the log text holds what the error said")
		}
	})
}
