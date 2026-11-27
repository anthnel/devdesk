package docker

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-04 (DEVDESK-800): the shell detection answers bash, else sh, and never waits
// on a daemon that does not answer.
//
// OC-18 (DEVDESK-816, DEVDESK-939): DetectShell and ShellCmd refuse a malformed
// container ID with ErrInvalidContainerID before anything is built or run, the way
// LogsCmd and the lifecycle verbs do, and the shell session is never an
// interpreter's command line: the argv is `<engine> exec -it <id> <shell>`, one
// element each. The API these tests expect:
//
//	func DetectShell(id string) (string, error)
//	func ShellCmd(id, shell string) (*exec.Cmd, error)

// shellID is a well-formed container ID, the short form `docker ps` prints.
const shellID = "0123456789ab"

func TestDetectShellPrefersBash(t *testing.T) {
	s := stub(t, &stubRunner{})

	got, err := DetectShell(shellID)

	if err != nil {
		t.Fatalf("DetectShell(%q) error = %v", shellID, err)
	}
	if got != "/bin/bash" {
		t.Errorf("DetectShell() = %q, want /bin/bash", got)
	}
	if want := []string{"exec", shellID, "which", "bash"}; !slices.Equal(s.lastArgs(), want) {
		t.Errorf("argv = %q, want %q", s.lastArgs(), want)
	}
}

// Bash missing in the container is not an error: the shell is /bin/sh, as before
// OC-18 (criterion 4).
func TestDetectShellFallsBackToShWhenBashIsMissing(t *testing.T) {
	stub(t, &stubRunner{err: map[string]error{"exec": errors.New("exit status 1")}})

	got, err := DetectShell(shellID)

	if err != nil {
		t.Errorf("DetectShell() error = %v, want nil: a missing bash is the fallback, not a failure", err)
	}
	if got != "/bin/sh" {
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
	go func() {
		got, _ := DetectShell(shellID)
		done <- got
	}()

	select {
	case got := <-done:
		if got != "/bin/sh" {
			t.Errorf("DetectShell() = %q, want the /bin/sh fallback", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DetectShell did not return: the detection has no timeout")
	}
}

// OC-18 criterion 2: a malformed ID is refused before the engine is asked, so
// `exec <hostile text> which bash` is never an argv.
func TestDetectShellRefusesAMalformedContainerID(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, bad := range badIDs[kindContainer] {
			t.Run(bad.label, func(t *testing.T) {
				s := stubStrict(t) // no argv declared: any Run fails the test

				got, err := DetectShell(bad.id)

				if !errors.Is(err, ErrInvalidContainerID) {
					t.Errorf("DetectShell(%q) error = %v, want ErrInvalidContainerID", bad.id, err)
				}
				if got != "" {
					t.Errorf("DetectShell(%q) = %q, want no shell for a refused ID", bad.id, got)
				}
				if len(s.calls) != 0 {
					t.Errorf("DetectShell(%q) reached the runner with %q", bad.id, s.lastArgs())
				}
			})
		}
	})
}

// OC-18 criterion 1: the session is `<engine> exec -it <id> <shell>` as an argv
// array on both engines, for the two shells, and the ID is one element that
// IsContainerID accepts. No interpreter, no `-c`, no joined string.
func TestShellCmdIsTheInteractiveExec(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, id := range goodIDs[kindContainer] {
			for _, shell := range []string{"/bin/bash", "/bin/sh"} {
				t.Run(shell+"/"+id[:12], func(t *testing.T) {
					want := []string{"exec", "-it", id, shell}
					s := stubStrict(t, reply{argv: want})

					cmd, err := ShellCmd(id, shell)

					if err != nil {
						t.Fatalf("ShellCmd(%q, %q) error = %v", id, shell, err)
					}
					if cmd == nil {
						t.Fatalf("ShellCmd(%q, %q) built nothing", id, shell)
					}
					if !IsContainerID(id) {
						t.Fatalf("setup: %q is not a container ID", id)
					}
					// Args[0] is the engine binary; the rest is the argv, element by element.
					if got := cmd.Args[1:]; !slices.Equal(got, want) {
						t.Errorf("args = %q, want %q", got, want)
					}
					if slices.Contains(cmd.Args, "-c") {
						t.Errorf("args = %q: the session goes through an interpreter's -c", cmd.Args)
					}
					if len(s.calls) != 1 {
						t.Errorf("ShellCmd built %d command(s), want 1", len(s.calls))
					}
				})
			}
		}
	})
}

// OC-18 criterion 2 (DEVDESK-939): a malformed ID builds no argv at all. The
// error is what the footer shows, so it has to say what was refused.
func TestShellCmdRefusesAMalformedContainerID(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, bad := range badIDs[kindContainer] {
			t.Run(bad.label, func(t *testing.T) {
				s := stubStrict(t) // no argv declared: any Build or Run fails the test

				cmd, err := ShellCmd(bad.id, "/bin/sh")

				if cmd != nil {
					t.Errorf("ShellCmd(%q) built %q, want no command", bad.id, cmd.Args)
				}
				if !errors.Is(err, ErrInvalidContainerID) {
					t.Fatalf("ShellCmd(%q) error = %v, want ErrInvalidContainerID", bad.id, err)
				}
				if !strings.Contains(err.Error(), "container ID") {
					t.Errorf("error %q does not say it is about the container ID", err)
				}
				if len(s.calls) != 0 {
					t.Errorf("ShellCmd(%q) reached the runner with %q", bad.id, s.lastArgs())
				}
			})
		}
	})
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
