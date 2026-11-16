package nexusbench

import (
	"bytes"
	"context"
	"os/exec"
)

// Runner runs one kubectl command. args are the arguments after "kubectl" and
// carry no --context: the runner adds it. stdin is sent to the command when it
// is not empty. Both streams are returned even on failure, because the stderr
// of a failed call is what classifyKubectl reads.
type Runner interface {
	Run(ctx context.Context, stdin []byte, args ...string) (stdout, stderr []byte, err error)
}

// execRunner runs the kubectl found on PATH against one context.
type execRunner struct {
	path    string
	context string
}

// NewExecRunner finds kubectl on PATH and returns a Runner that passes
// "--context <kubeContext>" to every call. It fails with a KubectlMissing
// BenchError when kubectl is not installed. This is the one place of the
// package that starts a process.
func NewExecRunner(kubeContext string) (Runner, error) {
	path, err := exec.LookPath("kubectl")
	if err != nil {
		return nil, &BenchError{
			Kind:  KubectlMissing,
			Cause: "kubectl was not found on PATH",
			Check: "run `mise install` to get kubectl, then `kubectl version --client`",
			Err:   err,
		}
	}
	return &execRunner{path: path, context: kubeContext}, nil
}

// Run implements Runner.
func (r *execRunner) Run(ctx context.Context, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	full := append([]string{"--context", r.context}, args...)
	cmd := exec.CommandContext(ctx, r.path, full...) //nolint:gosec // the binary is the kubectl found on PATH and the arguments are built by this package from validated parameters
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}
