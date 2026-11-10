package teaexec

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fakeChild is a tea.ExecCommand whose Run advances a fake clock.
type fakeChild struct {
	clock *time.Time
	took  time.Duration
	err   error
}

func (f *fakeChild) Run() error {
	*f.clock = f.clock.Add(f.took)
	return f.err
}
func (f *fakeChild) SetStdin(_ io.Reader)  {}
func (f *fakeChild) SetStdout(_ io.Writer) {}
func (f *fakeChild) SetStderr(_ io.Writer) {}

var errX = errors.New("child failed")

func TestSettledRunHoldsTheTerminalForTheDelay(t *testing.T) {
	const delay = 50 * time.Millisecond
	tests := []struct {
		name      string
		took      time.Duration
		err       error
		wantSleep []time.Duration
	}{
		{"instant child", 0, nil, []time.Duration{50 * time.Millisecond}},
		{"30ms child", 30 * time.Millisecond, nil, []time.Duration{20 * time.Millisecond}},
		{"child lasting exactly the delay", 50 * time.Millisecond, nil, nil},
		{"child lasting longer", 200 * time.Millisecond, nil, nil},
		{"instant failing child", 0, errX, []time.Duration{50 * time.Millisecond}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := time.Unix(1000, 0)
			var slept []time.Duration
			s := settled{
				ExecCommand: &fakeChild{clock: &clock, took: tt.took, err: tt.err},
				delay:       delay,
				now:         func() time.Time { return clock },
				sleep: func(d time.Duration) {
					slept = append(slept, d)
					clock = clock.Add(d)
				},
			}
			err := s.Run()
			if !errors.Is(err, tt.err) {
				t.Errorf("Run() = %v, want %v", err, tt.err)
			}
			if len(slept) != len(tt.wantSleep) {
				t.Fatalf("sleep calls = %v, want %v", slept, tt.wantSleep)
			}
			for i := range slept {
				if slept[i] != tt.wantSleep[i] {
					t.Errorf("sleep[%d] = %v, want %v", i, slept[i], tt.wantSleep[i])
				}
			}
		})
	}
}

func TestOSCommandKeepsStreamsAlreadySet(t *testing.T) {
	in, out, errw := strings.NewReader("in"), &bytes.Buffer{}, &bytes.Buffer{}
	cmd := exec.Command("true")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errw

	c := osCommand{cmd}
	c.SetStdin(strings.NewReader("other"))
	c.SetStdout(&bytes.Buffer{})
	c.SetStderr(&bytes.Buffer{})

	if cmd.Stdin != in || cmd.Stdout != out || cmd.Stderr != errw {
		t.Error("osCommand overwrote a stream that was already set")
	}
}

func TestOSCommandFillsNilStreams(t *testing.T) {
	in, out, errw := strings.NewReader("in"), &bytes.Buffer{}, &bytes.Buffer{}
	cmd := exec.Command("true")

	c := osCommand{cmd}
	c.SetStdin(in)
	c.SetStdout(out)
	c.SetStderr(errw)

	if cmd.Stdin != in || cmd.Stdout != out || cmd.Stderr != errw {
		t.Errorf("nil streams not filled: stdin=%v stdout=%v stderr=%v", cmd.Stdin, cmd.Stdout, cmd.Stderr)
	}
}

func TestEntryPointsAcceptANilCallback(t *testing.T) {
	if Command(&fakeChild{clock: &time.Time{}}, nil) == nil {
		t.Error("Command(c, nil) returned a nil tea.Cmd")
	}
	if Process(exec.Command("true"), nil) == nil {
		t.Error("Process(cmd, nil) returned a nil tea.Cmd")
	}
}

func TestProcessReturnsTheExecMessageNotTheCallbackOne(t *testing.T) {
	type cbMsg struct{}
	msg := Process(exec.Command("true"), func(error) tea.Msg { return cbMsg{} })()
	if msg == nil {
		t.Fatal("Process(...)() returned a nil message")
	}
	if _, isCB := msg.(cbMsg); isCB {
		t.Error("the command ran the callback instead of handing the exec message to the program")
	}
}

func TestSettleDelayIs50ms(t *testing.T) {
	if settleDelay != 50*time.Millisecond {
		t.Errorf("settleDelay = %v, want 50ms (H1 of DEVDESK-290)", settleDelay)
	}
}
