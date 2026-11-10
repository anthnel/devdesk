// Package teaexec hands the terminal to a child process through tea.Exec, and
// works around a renderer ticker race in Bubble Tea v2.1.1 (DEVDESK-290).
//
// Bubble Tea's exec path is: stopRenderer, ExecCommand.Run, startRenderer.
// stopRenderer sends on an unbuffered channel, which completes when the render
// goroutine has received the signal, not when it has finished: that goroutine
// then runs ticker.Stop() asynchronously. startRenderer reuses the same ticker
// (Reset). When the child returns at once (failed start, missing binary), Reset
// can run before the late Stop(), the ticker stays stopped and the screen never
// repaints again while the program keeps handling input.
//
// Nothing done in the ExecCallback can fix this, as it runs after the race is
// decided. Holding the terminal for a minimum time inside Run lets the old
// goroutine finish. Every exec goes through Process or Command; a source guard
// forbids calling tea.ExecProcess, tea.Exec and tea.Suspend elsewhere.
package teaexec

import (
	"io"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"
)

// settleDelay is the shortest time Run holds the terminal, child included.
const settleDelay = 50 * time.Millisecond

// Process is the drop-in replacement for tea.ExecProcess.
func Process(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return Command(osCommand{c}, fn)
}

// Command is the drop-in replacement for tea.Exec.
func Command(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
	return tea.Exec(settled{ExecCommand: c, delay: settleDelay, now: time.Now, sleep: time.Sleep}, fn)
}

// settled wraps an ExecCommand so that Run never returns before delay has
// passed since it started. The fields now and sleep are seams for tests.
type settled struct {
	tea.ExecCommand
	delay time.Duration
	now   func() time.Time
	sleep func(time.Duration)
}

// Run runs the child, then sleeps for whatever remains of the delay, also when
// the child failed (the most exposed path). The child's error is returned
// unchanged. Bubble Tea calls Run between stopRenderer and startRenderer, so
// the sleep gives the old render goroutine time to run its ticker.Stop() before
// startRenderer resets the ticker. If the sentinel test
// TestUpstreamTickerRaceStillExists fails, upstream fixed the race and this
// delay can be removed.
func (s settled) Run() error {
	start := s.now()
	err := s.ExecCommand.Run()
	if rest := s.delay - s.now().Sub(start); rest > 0 {
		s.sleep(rest)
	}
	return err
}

// osCommand adapts *exec.Cmd to tea.ExecCommand with the semantics of Bubble
// Tea's unexported osExecCommand: a stream is set only when it is nil.
type osCommand struct{ *exec.Cmd }

func (c osCommand) SetStdin(r io.Reader) {
	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c osCommand) SetStdout(w io.Writer) {
	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c osCommand) SetStderr(w io.Writer) {
	if c.Stderr == nil {
		c.Stderr = w
	}
}
