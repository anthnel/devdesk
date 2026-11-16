package nexusbench

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// execReplyRunner stands in for kubectl: it records the arguments and answers with a
// fixed reply, the password on stdout included when the test wants it there.
type execReplyRunner struct {
	stdout, stderr string
	err            error
	args           []string
	stdin          []byte
}

var _ Runner = (*execReplyRunner)(nil)

func (r *execReplyRunner) Run(_ context.Context, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	r.args, r.stdin = slices.Clone(args), stdin
	return []byte(r.stdout), []byte(r.stderr), r.err
}

func TestKubectlInitialPasswordReadsTheFile(t *testing.T) {
	r := &execReplyRunner{stdout: fakeInitialAdmin + "\n"}
	pw, ok, err := NewKubectlInitialPassword(r, "nexus-bench").Read(context.Background())
	if err != nil || !ok || pw != fakeInitialAdmin {
		t.Fatalf("Read = %q, %v, %v; want the password without its newline", pw, ok, err)
	}
	want := []string{"-n", "nexus-bench", "exec", "deploy/nexus", "--", "cat", "/nexus-data/admin.password"}
	if !slices.Equal(r.args, want) {
		t.Errorf("args = %v, want %v", r.args, want)
	}
	if slices.Contains(r.args, "--context") {
		t.Error("the runner adds --context, the reader must not")
	}
	if len(r.stdin) != 0 {
		t.Error("nothing is sent on stdin")
	}
}

func TestKubectlInitialPasswordMissingFile(t *testing.T) {
	r := &execReplyRunner{
		stderr: "cat: /nexus-data/admin.password: No such file or directory\ncommand terminated with exit code 1",
		err:    errors.New("exit status 1"),
	}
	pw, ok, err := NewKubectlInitialPassword(r, "nexus-bench").Read(context.Background())
	if err != nil || ok || pw != "" {
		t.Errorf("Read = %q, %v, %v; a missing file is (empty, false, nil)", pw, ok, err)
	}
}

func TestKubectlInitialPasswordEmptyFile(t *testing.T) {
	r := &execReplyRunner{stdout: "\n"}
	if pw, ok, err := NewKubectlInitialPassword(r, "nexus-bench").Read(context.Background()); err != nil || ok || pw != "" {
		t.Errorf("Read = %q, %v, %v; an empty file is not a password", pw, ok, err)
	}
}

func TestKubectlInitialPasswordFailuresHoldNoPassword(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   ErrKind
	}{
		{"forbidden", `Error from server (Forbidden): pods "x" is forbidden: cannot create resource "pods/exec"`, Forbidden},
		{"unreachable", "Unable to connect to the server: dial tcp 10.0.0.1:6443: connect: connection refused", ClusterUnreachable},
		{"no pod", "error: unable to upgrade connection: pod does not exist", ProvisionFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The password is on both streams, as a command that failed halfway
			// might leave it: neither may reach the error.
			r := &execReplyRunner{stdout: secretMarker, stderr: tt.stderr + " " + secretMarker, err: errors.New("exit status 1")}
			_, ok, err := NewKubectlInitialPassword(r, "nexus-bench").Read(context.Background())
			if ok || err == nil {
				t.Fatalf("Read = ok %v, err %v; want an error", ok, err)
			}
			var be *BenchError
			if !errors.As(err, &be) || be.Kind != tt.want {
				t.Fatalf("error = %#v, want a *BenchError of kind %d", err, tt.want)
			}
			if strings.Contains(err.Error(), secretMarker) {
				t.Errorf("the error carries the password: %q", err)
			}
		})
	}
}
