package fakecli_test

import (
	"bytes"
	"errors"
	"os/exec"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-08a step 5: fakecli is a byte transport. A Reply is two Go strings, and a Go
// string holds any bytes; the scenario used to travel as JSON text, which
// rewrites an invalid UTF-8 byte as U+FFFD, so a recorded binary stream reached
// the code under test altered. The public API (Reply{Stdout, Stderr string},
// Exit) does not change; what the tool writes is exactly what the Reply holds.

// raw runs a tool and returns its streams as bytes.
func raw(t *testing.T, name string, args ...string) (stdout, stderr []byte, exit int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		exit = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("starting %s %q: %v", name, args, err)
	}
	return so.Bytes(), se.Bytes(), exit
}

func allBytes() string {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return string(b)
}

func TestARepliesBytesReachTheCodeUnderTestUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		reply  fakecli.Reply
		stdout string
		stderr string
	}{
		{"an invalid UTF-8 byte, a NUL and a CR in stdout", fakecli.Reply{Stdout: "\xff\x00a\r\n"}, "\xff\x00a\r\n", ""},
		{"the same on stderr", fakecli.Reply{Stderr: "\xfe\x80\x00z"}, "", "\xfe\x80\x00z"},
		{"both, with an exit code", fakecli.Reply{Stdout: "\xc3(", Stderr: "\xe2\x82", Exit: 125}, "\xc3(", "\xe2\x82"},
		{"every byte value", fakecli.Reply{Stdout: allBytes(), Stderr: allBytes()}, allBytes(), allBytes()},
		{"a literal U+FFFD is still U+FFFD", fakecli.Reply{Stdout: "\xef\xbf\xbd"}, "\xef\xbf\xbd", ""},
		{"no final newline", fakecli.Reply{Stdout: "6.2.1"}, "6.2.1", ""},
		{"nothing at all", fakecli.Reply{}, "", ""},
		{"a big stream", fakecli.Reply{Stdout: string(bytes.Repeat([]byte{0x80, 0xff, 0x00}, 50_000))},
			string(bytes.Repeat([]byte{0x80, 0xff, 0x00}, 50_000)), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakecli.Install(t, fakecli.Scenario{{Tool: "docker", Args: []string{"export", "x"}, Reply: tt.reply}}, "docker")

			stdout, stderr, exit := raw(t, "docker", "export", "x")

			if !bytes.Equal(stdout, []byte(tt.stdout)) {
				t.Errorf("stdout = %q, want %q", clip(stdout), clip([]byte(tt.stdout)))
			}
			if !bytes.Equal(stderr, []byte(tt.stderr)) {
				t.Errorf("stderr = %q, want %q", clip(stderr), clip([]byte(tt.stderr)))
			}
			if exit != tt.reply.Exit {
				t.Errorf("exit = %d, want %d", exit, tt.reply.Exit)
			}
		})
	}
}

// clip keeps a failure message readable for the big streams.
func clip(b []byte) []byte {
	if len(b) > 64 {
		return append(b[:64:64], "..."...)
	}
	return b
}

// A prefix exchange and the journal keep working with bytes in the reply: the
// transport changed under them, not their behaviour.
func TestTheByteTransportKeepsPrefixMatchingAndTheJournal(t *testing.T) {
	f := fakecli.Install(t, fakecli.Scenario{
		{Tool: "docker", Args: []string{"export"}, Prefix: true, Reply: fakecli.Reply{Stdout: "\xff"}},
	}, "docker")

	stdout, _, _ := raw(t, "docker", "export", "ddm-run")

	if !bytes.Equal(stdout, []byte{0xff}) {
		t.Errorf("stdout = %q, want ff", stdout)
	}
	inv := f.Invocations()
	if len(inv) != 1 || !inv[0].Matched || inv[0].Tool != "docker" || len(inv[0].Args) != 2 || inv[0].Args[1] != "ddm-run" {
		t.Errorf("journal = %+v, want one matched docker export ddm-run", inv)
	}
}
