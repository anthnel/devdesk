// Command nexus-bench deploys, provisions, inspects and destroys the disposable
// Nexus Community bench of DEVDESK-504 on a k0s cluster. It is a development
// tool driven by the mise tasks nexus-bench:*, outside mise run ci; see
// docs/architecture/nexus-bench.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// Exit codes.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// deps is everything the command needs that reaches a cluster or the network,
// so that the tests run it with fakes. The zero value is enough for render,
// which uses none of it.
type deps struct {
	newRunner  func(kubeContext string) (nexusbench.Runner, error)
	newProber  func() nexusbench.Prober
	clock      nexusbench.Clock
	newStore   func(path string) (nexusbench.SecretStore, error)
	newInitial func(r nexusbench.Runner, namespace string) nexusbench.InitialPassword
	preflight  func(ctx context.Context, r nexusbench.Runner, p nexusbench.Params, op nexusbench.Op) (nexusbench.PreflightResult, error)
	deploy     func(ctx context.Context, r nexusbench.Runner, pr nexusbench.Prober, clk nexusbench.Clock, p nexusbench.Params, out io.Writer) (nexusbench.DeployResult, error)
	provision  func(ctx context.Context, baseURL string, ip nexusbench.InitialPassword, store nexusbench.SecretStore, p nexusbench.Params, out io.Writer) (nexusbench.ProvisionResult, error)
	destroy    func(ctx context.Context, r nexusbench.Runner, clk nexusbench.Clock, store nexusbench.SecretStore, p nexusbench.Params, out io.Writer) error
}

// defaultDeps wires the real kubectl, HTTP client, clock and files.
func defaultDeps() deps {
	return deps{
		newRunner:  nexusbench.NewExecRunner,
		newProber:  nexusbench.NewProber,
		clock:      nexusbench.NewClock(),
		newStore:   nexusbench.NewFileStore,
		newInitial: nexusbench.NewKubectlInitialPassword,
		preflight:  nexusbench.RunPreflight,
		deploy:     nexusbench.Deploy,
		provision:  nexusbench.Provision,
		destroy:    nexusbench.Destroy,
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal cancels ctx, hand the signals back to the default
	// behavior so that a second Ctrl-C kills a run that is slow to wind down.
	context.AfterFunc(ctx, stop)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr, defaultDeps())
	stop()
	os.Exit(code)
}

// session is one subcommand being run: its context, its resolved parameters and
// its streams.
type session struct {
	ctx    context.Context
	p      nexusbench.Params
	getenv func(string) string
	stdout io.Writer
	stderr io.Writer
	d      deps
	// restHost is where the REST API is reached; explicit tells whether the
	// operator chose it.
	restHost string
	explicit bool
}

// run executes one command line and returns the exit code: 0 for success, 1
// for a failure of the bench, 2 for a mistake in the command line.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer, d deps) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitUsage
	}
	name := args[0]
	switch name {
	case "-h", "--help", "help":
		printUsage(stdout)
		return exitOK
	case "up", "provision", "down", "status", "render":
	default:
		_, _ = fmt.Fprintf(stderr, "nexus-bench: unknown subcommand %q\n\n", name)
		printUsage(stderr)
		return exitUsage
	}

	provisions := name == "up" || name == "provision"
	flags := newFlagSet(name, provisions)
	if err := flags.parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printSubcommandHelp(stdout, name, flags)
			return exitOK
		}
		_, _ = fmt.Fprintf(stderr, "nexus-bench %s: %v\nRun nexus-bench %s -h for the flags.\n", name, err, name)
		return exitUsage
	}

	s := &session{ctx: ctx, getenv: getenv, stdout: stdout, stderr: stderr, d: d, restHost: loopbackHost}
	p, err := flags.resolveParams(getenv)
	if err != nil {
		return s.fail(err)
	}
	s.p = p
	if host, given := flags.restHostGiven(); given {
		if err := validateRESTHost(host); err != nil {
			return s.fail(err)
		}
		s.restHost, s.explicit = host, true
	}

	switch name {
	case "up":
		return s.up()
	case "provision":
		return s.provision()
	case "down":
		return s.down()
	case "status":
		return s.status()
	default:
		return s.render()
	}
}

// fail prints err on stderr and returns the failure code. A BenchError (also
// when wrapped or joined) is printed as its own message and nothing else: the
// text around it and the error it wraps can carry what a sub-process printed,
// a credential included. A cancelled run says so.
func (s *session) fail(err error) int {
	var be *nexusbench.BenchError
	switch {
	case errors.As(err, &be):
		_, _ = fmt.Fprintln(s.stderr, be.Error())
	case errors.Is(err, context.Canceled):
		_, _ = fmt.Fprintln(s.stderr, "cancelled; the cluster keeps what was already done, run nexus-bench status to see it")
	default:
		_, _ = fmt.Fprintln(s.stderr, err.Error())
	}
	return exitFailure
}

// warn prints a warning on stderr.
func (s *session) warn(format string, args ...any) {
	_, _ = fmt.Fprintf(s.stderr, "warning: "+format+"\n", args...)
}

// restURL is the REST origin the credentials are sent to.
func (s *session) restURL() string {
	return nexusbench.RESTBaseURL(s.restHost, s.p)
}

// warnIfClearText says so when the admin credentials leave this machine.
func (s *session) warnIfClearText() {
	if s.explicit && !isLoopbackHost(s.restHost) {
		s.warn("%s is plain HTTP: the credentials travel in clear text to %s", s.restURL(), s.restHost)
	}
}
