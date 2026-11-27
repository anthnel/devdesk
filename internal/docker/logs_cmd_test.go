package docker

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-84 (DEVDESK-801, requirement S1). The log pager used to be handed to
// `sh -c "<engine> logs ... | ${PAGER:-less} -R"`. The `logs` command is now
// built here, as an argv, from a validated ID, and it is never started by this
// package: the caller wires its output to the pager's stdin with a Go pipe
// (internal/ui/teaexec.Pipe).
//
// LogsCmd and ErrInvalidContainerID are the API these tests expect:
//
//	var ErrInvalidContainerID = errors.New("invalid container ID")
//	func LogsCmd(id string, tail int) (*exec.Cmd, error)

// A valid ID gives `<engine> logs --tail N ID`, one argv element per word, on
// the binary of the engine in use, under both engines. The real runner is used
// against the shared fake CLI, so the argv that reaches the process is the one
// read back from its journal, not the one the code claims to have built.
func TestLogsCmdIsAnArgvOnTheEnginesBinary(t *testing.T) {
	fullID := strings.Repeat("ab", 32)
	tests := []struct {
		name string
		id   string
		tail int
		want []string
	}{
		{"short id, the tail the pane uses", "0123456789ab", 500, []string{"logs", "--tail", "500", "0123456789ab"}},
		{"full-length id", fullID, 500, []string{"logs", "--tail", "500", fullID}},
		{"another tail", "0123456789ab", 20, []string{"logs", "--tail", "20", "0123456789ab"}},
	}

	var scenario fakecli.Scenario
	for _, tool := range engine.Names() {
		for _, tt := range tests {
			scenario = append(scenario, fakecli.Exchange{
				Tool: tool, Args: tt.want,
				Reply: fakecli.Reply{Stdout: "line one\nline two\n"},
			})
		}
	}
	fake := fakecli.Install(t, scenario, engine.Names()...)

	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stub(t, &stubRunner{}) // restores the runner and the engine
				runner = cliRunner{}
				useEngine(t, shape.Name)

				cmd, err := LogsCmd(tt.id, tt.tail)
				if err != nil {
					t.Fatalf("LogsCmd(%q, %d) error = %v", tt.id, tt.tail, err)
				}

				if got := filepath.Base(cmd.Args[0]); got != shape.Binary {
					t.Errorf("LogsCmd runs %q, want the engine's binary %q", got, shape.Binary)
				}
				if !slices.Equal(cmd.Args[1:], tt.want) {
					t.Errorf("LogsCmd argv = %q, want %q", cmd.Args[1:], tt.want)
				}
				// The caller wires the streams to the pager; a stream already set
				// here would be one it cannot take over.
				if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
					t.Errorf("LogsCmd set a stream (stdin %v, stdout %v, stderr %v); the pipe is the caller's", cmd.Stdin, cmd.Stdout, cmd.Stderr)
				}

				before := len(fake.Invocations())
				out, err := cmd.Output()
				if err != nil {
					t.Fatalf("running the command: %v", err)
				}
				if string(out) != "line one\nline two\n" {
					t.Errorf("output = %q, want the fake's log", out)
				}
				ran := fake.Invocations()[before:]
				if len(ran) != 1 || ran[0].Tool != shape.Binary || !slices.Equal(ran[0].Args, tt.want) {
					t.Errorf("the process received %+v, want exactly one %s %q", ran, shape.Binary, tt.want)
				}
			})
		}
	})
}

// A malformed ID is refused before anything is built: no command, an error that
// names the problem, and the runner is never asked for a thing (a strict stub
// with no declared argv fails the test on any Build or Run).
func TestLogsCmdRefusesAMalformedID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"shell metacharacters", "abc; rm -rf ~"},
		{"command substitution", "$(touch /tmp/pwned)"},
		{"backticks", "`id`0123456789"},
		{"a pipe", "0123456789ab | sh"},
		{"leading dash, a flag for the engine", "-0123456789ab"},
		{"an option", "--follow"},
		{"a short option", "-f"},
		{"empty", ""},
		{"uppercase hex", "0123456789AB"},
		{"too short", "0123456789a"},
		{"too long", strings.Repeat("a", 65)},
		{"trailing newline", "0123456789ab\n"},
		{"embedded space", "0123456789ab cd"},
		{"a name, not an ID", "web"},
	}

	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stubStrict(t) // no argv declared: any Build or Run fails the test

				cmd, err := LogsCmd(tt.id, 500)

				if cmd != nil {
					t.Errorf("LogsCmd(%q) built %q, want no command", tt.id, cmd.Args)
				}
				if !errors.Is(err, ErrInvalidContainerID) {
					t.Fatalf("LogsCmd(%q) error = %v, want ErrInvalidContainerID", tt.id, err)
				}
				// The footer shows this text, so it has to say what was refused.
				if !strings.Contains(err.Error(), "container ID") {
					t.Errorf("error %q does not say it is about the container ID", err)
				}
			})
		}
	})
}
