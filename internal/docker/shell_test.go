package docker

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// OC-04 (DEVDESK-800): the shell detection answers bash, else sh, and never waits
// on a daemon that does not answer.

func TestDetectShellPrefersBash(t *testing.T) {
	s := stub(t, &stubRunner{})

	if got := DetectShell("abc123"); got != "/bin/bash" {
		t.Errorf("DetectShell() = %q, want /bin/bash", got)
	}
	if got := strings.Join(s.lastArgs(), " "); got != "exec abc123 which bash" {
		t.Errorf("argv = %q, want %q", got, "exec abc123 which bash")
	}
}

func TestDetectShellFallsBackToShWhenBashIsMissing(t *testing.T) {
	stub(t, &stubRunner{err: map[string]error{"exec": errors.New("exit status 1")}})

	if got := DetectShell("abc123"); got != "/bin/sh" {
		t.Errorf("DetectShell() = %q, want /bin/sh", got)
	}
}

// hangingRunner is a daemon that never answers: Run returns only when the
// context of the call ends, so the test needs no sleep longer than the timeout
// it sets.
type hangingRunner struct{ stubRunner }

func (h *hangingRunner) Run(dc dockerCmd) ([]byte, error) {
	if dc.Ctx == nil {
		return nil, errors.New("no context: this call could never be stopped")
	}
	<-dc.Ctx.Done()
	return nil, dc.Ctx.Err()
}

func TestDetectShellGivesUpOnADaemonThatDoesNotAnswer(t *testing.T) {
	stub(t, &stubRunner{}) // restores runner and engine on cleanup
	runner = &hangingRunner{}
	previous := shellDetectTimeout
	shellDetectTimeout = 20 * time.Millisecond
	t.Cleanup(func() { shellDetectTimeout = previous })

	done := make(chan string, 1)
	go func() { done <- DetectShell("abc123") }()

	select {
	case got := <-done:
		if got != "/bin/sh" {
			t.Errorf("DetectShell() = %q, want the /bin/sh fallback", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DetectShell did not return: the detection has no timeout")
	}
}

func TestShellCmdIsTheInteractiveExec(t *testing.T) {
	stub(t, &stubRunner{})

	cmd := ShellCmd("abc123", "/bin/sh")

	if got := strings.Join(cmd.Args[1:], " "); got != "exec -it abc123 /bin/sh" {
		t.Errorf("args = %q, want %q", got, "exec -it abc123 /bin/sh")
	}
}

// InspectContainer's failure message: the engine's stderr when the runner kept it
// on the ExitError, the error's own text otherwise (never an empty message).
func TestInspectContainerFailureCarriesStderrOrTheError(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"stderr kept", &exec.ExitError{Stderr: []byte("Error: no such object: abc\n")}, "no such object: abc"},
		{"no stderr", errors.New("exec: \"docker\": executable file not found"), "executable file not found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub(t, &stubRunner{err: map[string]error{"inspect": tt.err}})

			_, err := InspectContainer("aaaa111122223333")

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
