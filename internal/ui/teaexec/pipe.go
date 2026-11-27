package teaexec

import (
	"io"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// Pipe hands the terminal to consumer, whose stdin is the output of producer:
// `producer | consumer` as two commands and a Go pipe, with no shell between
// them (OC-84, DEVDESK-801). It goes through Command, so the settle delay applies.
func Pipe(producer, consumer *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return Command(NewPipe(producer, consumer), fn)
}

// NewPipe builds the tea.ExecCommand behind Pipe.
func NewPipe(producer, consumer *exec.Cmd) tea.ExecCommand {
	return &pipeCommand{producer: producer, consumer: consumer}
}

// pipeCommand runs a producer into a consumer. The consumer is the program the
// user sees; the producer only feeds it.
type pipeCommand struct {
	producer, consumer *exec.Cmd
	stdout, stderr     io.Writer
}

// SetStdin is ignored: the consumer reads the pipe, and a pager such as less
// reads its keys from the terminal by itself.
func (c *pipeCommand) SetStdin(io.Reader) {}

func (c *pipeCommand) SetStdout(w io.Writer) { c.stdout = w }

func (c *pipeCommand) SetStderr(w io.Writer) { c.stderr = w }

// Run starts the producer, with its stdout and stderr on one pipe (what `2>&1`
// did), then the consumer with the pipe as its stdin. When the consumer ends,
// the producer is killed and waited for, so it never outlives the pager. Only
// the consumer's error is returned: a killed producer is not the pager's
// failure, and a producer that fails (an unknown container) has already said
// why on the pipe. A command that cannot start fails Run, and whatever was
// started is ended.
func (c *pipeCommand) Run() error {
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	c.producer.Stdout = w
	c.producer.Stderr = w
	err = c.producer.Start()
	// The producer holds its own copy of the write end; ours must go, or the
	// consumer would never see the end of the stream.
	_ = w.Close()
	if err != nil {
		return err
	}
	defer func() {
		_ = c.producer.Process.Kill()
		_ = c.producer.Wait()
	}()

	c.consumer.Stdin = r
	// As osCommand does, a stream already set on the command is kept.
	if c.consumer.Stdout == nil {
		c.consumer.Stdout = c.stdout
	}
	if c.consumer.Stderr == nil {
		c.consumer.Stderr = c.stderr
	}
	return c.consumer.Run()
}
