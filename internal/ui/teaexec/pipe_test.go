package teaexec

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// OC-84 (DEVDESK-801, requirement S1). The log pager is `<engine> logs | pager`
// made of two commands and a Go pipe, with no shell between them. This file
// holds what the pipeline owes: the producer's output (stdout and stderr, which
// is what `2>&1` did) reaches the consumer's stdin, the terminal's input does
// not, and when the consumer ends (the user pressed q) the producer is killed
// and reaped, never left running.
//
// The API these tests expect:
//
//	func NewPipe(producer, consumer *exec.Cmd) tea.ExecCommand
//	func Pipe(producer, consumer *exec.Cmd, fn tea.ExecCallback) tea.Cmd
//
// NewPipe's Run starts the producer with its stdout and stderr on one pipe and
// the consumer with that pipe as stdin and the stdout and stderr given by
// SetStdout and SetStderr. SetStdin is ignored: the consumer reads the pipe
// (less reads its keys from the terminal itself). Run returns the consumer's
// error; the producer's exit status, killed or not, is never the result. A
// command that cannot start fails Run, and whatever was started is ended. Pipe
// is NewPipe through Command, so the settle delay applies to it too.

const (
	helperEnv  = "TEAEXEC_PIPE_HELPER"
	helperCode = "TEAEXEC_PIPE_CODE"
	runLimit   = 10 * time.Second
)

// TestPipeHelperProcess is not a test: it is the child the tests below start,
// by re-running the test binary. It does nothing unless helperEnv is set.
func TestPipeHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		return
	}
	code := 0
	if v := os.Getenv(helperCode); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &code)
	}
	switch mode {
	case "emit": // some lines on both streams, then exit with the given code
		_, _ = fmt.Fprintln(os.Stdout, "out-1")
		_, _ = fmt.Fprintln(os.Stderr, "err-1")
		_, _ = fmt.Fprintln(os.Stdout, "out-2")
	case "hang": // one line, then nothing for a minute
		fmt.Fprintln(os.Stdout, "first line")
		time.Sleep(time.Minute)
	case "hang-silent": // nothing at all for a minute
		time.Sleep(time.Minute)
	case "flood": // lines until something stops it
		for {
			if _, err := fmt.Fprintln(os.Stdout, "flood flood flood flood flood"); err != nil {
				break
			}
		}
	case "cat": // stdin to stdout
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "one-line": // read one line, then leave, as a user pressing q
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	case "exit": // leave at once
	}
	os.Exit(code)
}

func helper(mode string, code int) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPipeHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"="+mode, fmt.Sprintf("%s=%d", helperCode, code))
	return cmd
}

// run drives the pipeline like Bubble Tea does and gives up after runLimit, so a
// producer that is never killed fails the test instead of hanging it.
func run(t *testing.T, c tea.ExecCommand) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.Run() }()
	select {
	case err := <-done:
		return err
	case <-time.After(runLimit):
		t.Fatalf("Run did not return within %s: the producer is still running, or the pipe never closed", runLimit)
		return nil
	}
}

// reap is called with the producer once Run returned: the process must have been
// waited for (ProcessState set), which is what "no orphan" means, and it must
// not have ended cleanly, since it was told to hang or flood.
func reap(t *testing.T, producer *exec.Cmd) {
	t.Helper()
	t.Cleanup(func() {
		if producer.Process != nil {
			_ = producer.Process.Kill() // the test's own child, if the code under test failed to
		}
	})
	if producer.Process == nil {
		t.Fatal("the producer was never started")
	}
	if producer.ProcessState == nil {
		t.Fatal("the producer was not waited for: it is still running or a zombie")
	}
	if producer.ProcessState.Success() {
		t.Error("the producer ended cleanly; it was meant to be killed or broken off")
	}
}

func TestPipeCarriesTheProducersOutputToTheConsumersStdin(t *testing.T) {
	tests := []struct {
		name         string
		producerCode int
		want         []string
		notWant      []string
	}{
		{"stdout and stderr, in order, as 2>&1 did", 0, []string{"out-1\n", "err-1\n", "out-2\n"}, nil},
		// `docker logs` of an unknown container exits non-zero and writes its
		// reason on stderr: the pager shows it, and the pipeline reports no error.
		{"a producer that fails still shows what it said", 1, []string{"out-1\n", "err-1\n", "out-2\n"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var screen, errs bytes.Buffer
			c := NewPipe(helper("emit", tt.producerCode), helper("cat", 0))
			c.SetStdin(strings.NewReader("KEYS-TYPED-AT-THE-TERMINAL\n"))
			c.SetStdout(&screen)
			c.SetStderr(&errs)

			if err := run(t, c); err != nil {
				t.Fatalf("Run() = %v, want nil: the consumer succeeded", err)
			}

			got := screen.String()
			if want := strings.Join(tt.want, ""); got != want {
				t.Errorf("the consumer's output = %q, want the producer's %q", got, want)
			}
			// The terminal's input is not the producer's output.
			if strings.Contains(got, "KEYS-TYPED") {
				t.Errorf("the consumer read the terminal's stdin: %q", got)
			}
		})
	}
}

// The user leaves the pager (q): the producer, still running, is killed and
// waited for. Without that, `docker logs` outlives the pager, as an orphan, and
// until it fills the pipe it holds a connection to the engine.
func TestPipeEndsTheProducerWhenTheConsumerExits(t *testing.T) {
	tests := []struct {
		name     string
		producer string
		consumer string
	}{
		{"the user quits after the first line, the producer is silent after", "hang", "one-line"},
		{"the user quits at once, the producer wrote nothing", "hang-silent", "exit"},
		{"the user quits while the producer floods", "flood", "one-line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			producer := helper(tt.producer, 0)
			c := NewPipe(producer, helper(tt.consumer, 0))
			c.SetStdout(io.Discard)
			c.SetStderr(io.Discard)

			if err := run(t, c); err != nil {
				t.Errorf("Run() = %v, want nil: a killed producer is not the pager's failure", err)
			}
			reap(t, producer)
		})
	}
}

func TestPipeReturnsTheConsumersFailure(t *testing.T) {
	producer := helper("hang-silent", 0)
	c := NewPipe(producer, helper("exit", 3))
	c.SetStdout(io.Discard)
	c.SetStderr(io.Discard)

	err := run(t, c)

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("Run() = %v, want the consumer's exit status 3", err)
	}
	reap(t, producer)
}

// A producer that cannot start (the engine is not installed) fails the run, and
// the pager is not opened on nothing.
func TestPipeDoesNotStartTheConsumerWhenTheProducerCannotStart(t *testing.T) {
	consumer := helper("cat", 0)
	c := NewPipe(exec.Command("/nonexistent/oc84-engine", "logs"), consumer)
	c.SetStdout(io.Discard)
	c.SetStderr(io.Discard)

	if err := run(t, c); err == nil {
		t.Fatal("Run() = nil, want the error of the producer that could not start")
	}
	if consumer.Process != nil {
		t.Error("the consumer was started although the producer could not be")
	}
}

// A pager that cannot start (PAGER names no program, or a shell metacharacter
// made the first word `less;`) fails the run, and the producer already started
// is not left behind.
func TestPipeEndsTheProducerWhenTheConsumerCannotStart(t *testing.T) {
	for _, pager := range []string{"/nonexistent/oc84-pager", "less;"} {
		t.Run(pager, func(t *testing.T) {
			producer := helper("hang-silent", 0)
			t.Cleanup(func() {
				if producer.Process != nil {
					_ = producer.Process.Kill()
				}
			})
			c := NewPipe(producer, exec.Command(pager, "touch", "x"))
			c.SetStdout(io.Discard)
			c.SetStderr(io.Discard)

			if err := run(t, c); err == nil {
				t.Fatal("Run() = nil, want the error of the pager that could not start")
			}
			if producer.Process != nil && producer.ProcessState == nil {
				t.Error("the producer was started and then left running")
			}
		})
	}
}

// Pipe is the Cmd the viewer returns: it goes through teaexec, like Process and
// Command (the source guard keeps tea.Exec out of everywhere else).
func TestPipeReturnsACommand(t *testing.T) {
	cmd := Pipe(helper("emit", 0), helper("cat", 0), func(err error) tea.Msg { return err })
	if cmd == nil {
		t.Fatal("Pipe returned no command")
	}
}
