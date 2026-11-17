package nexusbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
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
	path, err := lookKubectl()
	if err != nil {
		return nil, err
	}
	return &execRunner{path: path, context: kubeContext}, nil
}

// lookKubectl finds kubectl on PATH, or returns a KubectlMissing BenchError.
func lookKubectl() (string, error) {
	path, err := exec.LookPath("kubectl")
	if err != nil {
		return "", &BenchError{
			Kind:  KubectlMissing,
			Cause: "kubectl was not found on PATH",
			Check: "run `mise install` to get kubectl, then `kubectl version --client`",
			Err:   err,
		}
	}
	return path, nil
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

// portForwardTimeout is how long Forward waits for kubectl to print the line
// that names the local port. A variable so a test can shorten it.
var portForwardTimeout = 15 * time.Second

const (
	// forwardKillDelay bounds the wait for kubectl's output pipes once it is
	// killed, in case a child it started still holds them.
	forwardKillDelay = 2 * time.Second
	// forwardStderrMax is how much of the stderr of kubectl port-forward is kept
	// for the classifier.
	forwardStderrMax = 16 << 10
	// forwardLineMax bounds a line kubectl prints without a line end.
	forwardLineMax = 4 << 10
	// forwardLoopback is the only address Forward binds and accepts.
	forwardLoopback = "127.0.0.1"
)

// resourceName is what a namespace or a Service name must look like before it
// reaches an argument of kubectl.
var resourceName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// PortForwarder forwards a local loopback port to a port of a Service.
type PortForwarder interface {
	Forward(ctx context.Context, namespace, service string, port int) (local string, stop func() error, err error)
}

// execPortForwarder runs `kubectl port-forward` from the kubectl on PATH.
type execPortForwarder struct {
	path    string
	context string
}

// NewExecPortForwarder finds kubectl on PATH and returns a PortForwarder that
// passes "--context <kubeContext>" to every call. It fails with a KubectlMissing
// BenchError when kubectl is not installed. With NewExecRunner, it is one of the
// two doors of the package to kubectl.
func NewExecPortForwarder(kubeContext string) (PortForwarder, error) {
	path, err := lookKubectl()
	if err != nil {
		return nil, err
	}
	return &execPortForwarder{path: path, context: kubeContext}, nil
}

// Forward starts `kubectl port-forward svc/<service> :<port> --address
// 127.0.0.1` and returns the local "127.0.0.1:<port>" kubectl chose. The
// forward ends when stop is called or ctx is done; stop kills kubectl and waits
// for it, and may be called any number of times. On an error no process is left
// running.
func (f *execPortForwarder) Forward(ctx context.Context, namespace, service string, port int) (string, func() error, error) {
	if !resourceName.MatchString(namespace) || !resourceName.MatchString(service) || port < 1 || port > 65535 {
		return "", nil, &BenchError{
			Kind:  Unclassified,
			Cause: "the port-forward target is not valid",
			Check: "give a namespace and a Service name made of lower case letters, digits, '-' and '.', and a port from 1 to 65535",
		}
	}
	if err := ctx.Err(); err != nil {
		return "", nil, forwardInterrupted(err)
	}

	lines := &forwardLineWriter{found: make(chan string, 1)}
	stderr := newCapWriter(forwardStderrMax)
	args := []string{"--context", f.context, "-n", namespace, "port-forward", "svc/" + service, ":" + strconv.Itoa(port), "--address", forwardLoopback}
	cmd := exec.CommandContext(ctx, f.path, args...) //nolint:gosec // the binary is the kubectl found on PATH and the arguments are validated above
	cmd.Stdout, cmd.Stderr = lines, stderr
	cmd.WaitDelay = forwardKillDelay
	if err := cmd.Start(); err != nil {
		return "", nil, &BenchError{
			Kind:  Unclassified,
			Cause: "kubectl port-forward could not be started",
			Check: "run `kubectl version --client` to see whether kubectl runs",
			Err:   err,
		}
	}
	pf := &portForward{cmd: cmd, done: make(chan struct{})}
	// The one goroutine that waits: it reaps kubectl whoever ends it (stop, the
	// context, kubectl itself) and publishes the end through done.
	go func() {
		pf.waitErr = cmd.Wait()
		close(pf.done)
	}()

	timer := time.NewTimer(portForwardTimeout)
	defer timer.Stop()
	select {
	case local := <-lines.found:
		return local, pf.stop, nil
	case <-pf.done:
		// The copy of the output ends before Wait returns, so a line printed
		// just before the exit is already in found.
		select {
		case local := <-lines.found:
			return local, pf.stop, nil
		default:
		}
		return "", nil, forwardExited(namespace, service, stderr.buf.String(), pf.waitErr)
	case <-timer.C:
		_ = pf.stop()
		return "", nil, &BenchError{
			Kind:  Deadline,
			Cause: fmt.Sprintf("kubectl did not report a local port within %s", portForwardTimeout),
			Check: fmt.Sprintf("run `kubectl --context %s -n %s port-forward svc/%s :%d` by hand and see whether the Service has a ready pod", f.context, namespace, service, port),
		}
	case <-ctx.Done():
		_ = pf.stop()
		return "", nil, forwardInterrupted(ctx.Err())
	}
}

// forwardInterrupted is the error of a forward the context ended.
func forwardInterrupted(err error) error {
	kind := Unclassified
	if errors.Is(err, context.DeadlineExceeded) {
		kind = Deadline
	}
	return &BenchError{
		Kind:  kind,
		Cause: "the port-forward was interrupted before it was ready",
		Check: "run the command again",
		Err:   err,
	}
}

// forwardExited is the error of a kubectl that ended before naming a port. The
// stderr only picks the kind: it never reaches the message, because it can carry
// a token or a server address.
func forwardExited(namespace, service, stderr string, runErr error) error {
	return &BenchError{
		Kind:  classifyKubectl(stderr),
		Cause: "kubectl port-forward ended before it reported a local port",
		Check: fmt.Sprintf("run `kubectl -n %s port-forward svc/%s` by hand to see why", namespace, service),
		Err:   runErr,
	}
}

// portForward is one running `kubectl port-forward`.
type portForward struct {
	cmd     *exec.Cmd
	done    chan struct{} // closed once cmd.Wait has returned
	waitErr error         // set before done is closed
	once    sync.Once
}

// stop kills kubectl and waits until it is gone. It is idempotent and always
// returns nil: a process that already ended is the goal, not a failure.
func (p *portForward) stop() error {
	p.once.Do(func() { _ = p.cmd.Process.Kill() })
	<-p.done
	return nil
}

// forwardLineWriter reads what kubectl prints, line by line, and publishes the
// first address parseForwardingLine accepts; after that it drops the output
// ("Handling connection for ..." goes on for as long as the forward lives).
type forwardLineWriter struct {
	found   chan string // capacity 1
	pending []byte
	done    bool
}

// Write implements io.Writer. os/exec calls it from one goroutine.
func (w *forwardLineWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.done {
		return n, nil
	}
	w.pending = append(w.pending, p...)
	for {
		end := bytes.IndexByte(w.pending, '\n')
		if end < 0 {
			break
		}
		line := string(w.pending[:end])
		w.pending = w.pending[end+1:]
		if local, ok := parseForwardingLine(line); ok {
			w.found <- local
			w.done, w.pending = true, nil
			return n, nil
		}
	}
	if len(w.pending) > forwardLineMax {
		w.pending = nil
	}
	return n, nil
}

// parseForwardingLine reads the line kubectl prints once a port is bound,
// "Forwarding from 127.0.0.1:41234 -> 5000". Only an IPv4 loopback address with
// a port from 1 to 65535 is accepted; an IPv6 line (kubectl prints both for
// "localhost") and anything else is ignored.
func parseForwardingLine(line string) (hostPort string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), "Forwarding from ")
	if !found {
		return "", false
	}
	addr, _, found := strings.Cut(rest, " -> ")
	if !found {
		return "", false
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil || host != forwardLoopback {
		return "", false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != strings.TrimLeft(portText, "0") {
		return "", false
	}
	return net.JoinHostPort(forwardLoopback, strconv.Itoa(port)), true
}
