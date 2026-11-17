package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// render prints the manifests. It builds no dependency: it contacts nothing.
func (s *session) render() int {
	out, err := nexusbench.Render(s.p)
	if err != nil {
		return s.fail(err)
	}
	if _, err := s.stdout.Write(out); err != nil {
		return s.fail(err)
	}
	return exitOK
}

// open resolves the secrets path and builds the runner and the store, in that
// order, before anything touches the cluster. Both are refused here when they
// cannot work: kubectl missing, a secrets path that git does not ignore.
func (s *session) open() (nexusbench.Runner, nexusbench.SecretStore, error) {
	if s.p.SecretsPath == "" {
		path, err := nexusbench.DefaultSecretsPath(s.p.Context, s.p.Namespace, s.getenv)
		if err != nil {
			return nil, nil, err
		}
		s.p.SecretsPath = path
	}
	runner, err := s.d.newRunner(s.p.Context)
	if err != nil {
		return nil, nil, err
	}
	store, err := s.d.newStore(s.p.SecretsPath)
	if err != nil {
		return nil, nil, err
	}
	return runner, store, nil
}

// doors are the two doors the seed goes through besides kubectl: the
// port-forward to the upstream and the image copier.
type doors struct {
	forwarder nexusbench.PortForwarder
	copier    nexusbench.ImageCopier
}

// openDoors builds the doors of the seed, the forwarder first. They are built
// before anything touches the cluster: a host without skopeo is refused here,
// not after the deployment.
func (s *session) openDoors() (doors, error) {
	forwarder, err := s.d.newForwarder(s.p.Context)
	if err != nil {
		return doors{}, err
	}
	copier, err := s.d.newCopier()
	if err != nil {
		return doors{}, err
	}
	return doors{forwarder: forwarder, copier: copier}, nil
}

// bounded runs fn with a context that ends after Params.Timeout, counted from
// now, so that a call over a port-forward that hangs does not hang the command.
// A call that outlasts it is a failure with its own message; a cancelled run keeps
// the plain cancellation. what names the call ("the seed"), and advice is what to
// run next.
func (s *session) bounded(what, advice string, fn func(ctx context.Context) error) error {
	ctx, cancel := s.ctx, context.CancelFunc(func() {})
	if s.p.Timeout > 0 {
		ctx, cancel = context.WithTimeout(s.ctx, s.p.Timeout)
	}
	defer cancel()
	err := fn(ctx)
	if err != nil && ctx.Err() != nil && s.ctx.Err() == nil {
		return &nexusbench.BenchError{
			Kind:  nexusbench.Deadline,
			Cause: fmt.Sprintf("%s did not finish within %s", what, s.p.Timeout),
			Check: "raise --timeout (NEXUS_BENCH_TIMEOUT) and " + advice,
			Err:   err,
		}
	}
	return err
}

// seedDeps are what the library calls that copy or read through the doors need.
func (s *session) seedDeps(runner nexusbench.Runner, store nexusbench.SecretStore, d doors) nexusbench.SeedDeps {
	return nexusbench.SeedDeps{Runner: runner, Forwarder: d.forwarder, Copier: d.copier, Clock: s.d.clock, Store: store}
}

// runSeed runs Seed under the timeout.
func (s *session) runSeed(runner nexusbench.Runner, store nexusbench.SecretStore, d doors) (nexusbench.SeedResult, error) {
	var res nexusbench.SeedResult
	err := s.bounded("the seed", "run nexus-bench seed again: the images already copied are kept", func(ctx context.Context) (err error) {
		res, err = s.d.seed(ctx, s.seedDeps(runner, store, d), s.restURL(), s.p, s.stdout)
		return err
	})
	if err != nil {
		return nexusbench.SeedResult{}, err
	}
	return res, nil
}

// cutUpstream scales the upstream to zero. It needs no store and no door: the
// preflight of a cut asks for what a scale needs.
func (s *session) cutUpstream() int {
	runner, _, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	if _, err := s.d.preflight(s.ctx, runner, s.p, nexusbench.OpCut); err != nil {
		return s.fail(err)
	}
	if err := s.d.cutUpstream(s.ctx, runner, s.d.clock, s.p, s.stdout); err != nil {
		return s.fail(err)
	}
	return exitOK
}

// restoreUpstream brings a cut upstream back. It writes no descriptor.
func (s *session) restoreUpstream() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	d, err := s.openDoors()
	if err != nil {
		return s.fail(err)
	}
	if _, err := s.d.preflight(s.ctx, runner, s.p, nexusbench.OpRestore); err != nil {
		return s.fail(err)
	}
	err = s.bounded("the restore", "run `mise run nexus-bench:restore-upstream` again", func(ctx context.Context) error {
		return s.d.restoreUpstream(ctx, s.seedDeps(runner, store, d), s.restURL(), s.p, s.stdout)
	})
	if err != nil {
		return s.fail(err)
	}
	return exitOK
}

// reset puts the data set back to its initial state. Reset runs its own
// preflight; the descriptor of the previous run is read before anything is
// deleted and written again after a reset that succeeded, with the hosts of the
// previous one (the preflight of a reset reads no node).
func (s *session) reset() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	d, err := s.openDoors()
	if err != nil {
		return s.fail(err)
	}
	prev, err := s.previousDescriptor()
	if err != nil {
		return s.fail(err)
	}
	var provisioned nexusbench.ProvisionResult
	var seeded nexusbench.SeedResult
	err = s.bounded("the reset", "run `mise run nexus-bench:reset` again: it starts over", func(ctx context.Context) (err error) {
		provisioned, seeded, err = s.d.reset(ctx, s.seedDeps(runner, store, d), s.restURL(), s.p, s.stdout)
		return err
	})
	if errors.Is(err, context.Canceled) {
		// The library returns the cancellation as is, and a reset stopped after its
		// first deletion leaves a half-deleted data set.
		_, _ = fmt.Fprintln(s.stderr, "cancelled; the data set may be half deleted, run `mise run nexus-bench:reset` again: it starts over")
		return exitFailure
	}
	if err != nil {
		return s.fail(err)
	}
	return s.writeDescriptor(nexusbench.DeployResult{ReachableHosts: prev.Hosts}, provisioned, seeded)
}

// up deploys the bench and the upstream, provisions Nexus, seeds the data set
// and only then writes the descriptor (D-J). Deploy runs the preflight of up
// itself. An upstream URL the bench did not deploy is left alone: nothing is
// scaled or seeded, and the data set is described as empty.
func (s *session) up() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	seeds := s.p.UpstreamIsBench()
	var d doors
	if seeds {
		if d, err = s.openDoors(); err != nil {
			return s.fail(err)
		}
	}
	s.warnIfClearText()
	deployed, err := s.d.deploy(s.ctx, runner, s.d.newProber(), s.d.clock, s.p, s.stdout)
	if err != nil {
		return s.fail(err)
	}
	cut := false
	if seeds {
		if cut, err = s.foundCut(runner); err != nil {
			return s.fail(err)
		}
		if err := s.d.ensureUpstream(s.ctx, runner, s.d.clock, s.p, s.stdout); err != nil {
			return s.fail(err)
		}
	}
	provisioned, err := s.d.provision(s.ctx, s.restURL(), s.d.newInitial(runner, s.p.Namespace), store, s.p, s.stdout)
	if err != nil {
		return s.fail(err)
	}
	if cut {
		if err := s.d.unblockProxy(s.ctx, store, s.restURL(), s.p, s.stdout); err != nil {
			return s.fail(retryRestore(err))
		}
	}
	var seeded nexusbench.SeedResult
	if seeds {
		if seeded, err = s.runSeed(runner, store, d); err != nil {
			return s.fail(err)
		}
	} else {
		_, _ = fmt.Fprintln(s.stdout, "seed: skipped, NEXUS_BENCH_UPSTREAM_URL points at an upstream the bench did not deploy, so there is nothing to fill")
	}
	return s.writeDescriptor(deployed, provisioned, seeded)
}

// retryRestore tells, in the check of a BenchError, that the proxy is still
// blocked and that a second up will not unblock it: the upstream is back by now,
// so the second up finds it ready. Another error is returned as it is.
func retryRestore(err error) error {
	var be *nexusbench.BenchError
	if !errors.As(err, &be) {
		return err
	}
	return &nexusbench.BenchError{
		Kind:  be.Kind,
		Cause: be.Cause,
		Check: be.Check + "; the upstream is back but docker-proxy is still blocked, and a second up will not retry that: run `mise run nexus-bench:restore-upstream`",
		Err:   be.Err,
	}
}

// foundCut reads the upstream before EnsureUpstream brings it back, and tells
// whether it was cut: the proxy then needs to be unblocked (D-Q). The proxy can
// only be unblocked through the loopback; with another REST host, up stops here,
// before it brings the upstream back, because a refusal later would leave a proxy
// that stays blocked (a second up finds the upstream ready).
func (s *session) foundCut(runner nexusbench.Runner) (bool, error) {
	state, err := s.d.readUpstream(s.ctx, runner, s.p)
	if err != nil {
		return false, err
	}
	if state != nexusbench.UpstreamCut {
		return false, nil
	}
	if !nexusbench.IsLoopbackRESTHost(s.restHost) {
		return false, &nexusbench.BenchError{
			Kind:  nexusbench.Unclassified,
			Cause: fmt.Sprintf("the upstream is cut and its proxy can only be put back in service through 127.0.0.1, not through %s", s.restHost),
			Check: "run `mise run nexus-bench:restore-upstream` on the machine that runs the bench, then up again",
		}
	}
	return true, nil
}

// provision provisions a bench that is already running.
func (s *session) provision() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	s.warnIfClearText()
	pre, err := s.d.preflight(s.ctx, runner, s.p, nexusbench.OpProvision)
	if err != nil {
		return s.fail(err)
	}
	provisioned, err := s.d.provision(s.ctx, s.restURL(), s.d.newInitial(runner, s.p.Namespace), store, s.p, s.stdout)
	if err != nil {
		return s.fail(err)
	}
	return s.writeDescriptor(nexusbench.DeployResult{NodeIP: pre.NodeIP}, provisioned, s.keptDataset())
}

// keptDataset returns the data set of the code when the descriptor on disk is the
// descriptor of the same bench (namespace, context and upstream URL unchanged) and
// lists exactly that data set: the data set is fixed in the code, so what the file
// holds is compared to it and never copied. A file that disagrees, or is of another
// bench, counts as no data set, and the command says so. A descriptor that cannot
// be read (none, or schema 1), or that lists no image, has nothing to keep.
func (s *session) keptDataset() nexusbench.SeedResult {
	prev, err := nexusbench.LoadDescriptor(s.descriptorPath())
	if err != nil || len(prev.Dataset) == 0 {
		return nexusbench.SeedResult{}
	}
	code := nexusbench.SeedResult{Images: nexusbench.Dataset()}
	want := nexusbench.BuildDescriptor(s.p, nexusbench.DeployResult{}, nexusbench.ProvisionResult{}, code).Dataset
	if prev.Namespace == s.p.Namespace && prev.KubeContext == s.p.Context && prev.Upstream.URL == s.p.UpstreamURL &&
		reflect.DeepEqual(prev.Dataset, want) {
		return code
	}
	line := "provision: the descriptor now lists no data set"
	if s.p.UpstreamIsBench() {
		line += "; run mise run nexus-bench:seed"
	}
	_, _ = fmt.Fprintln(s.stdout, line)
	return nexusbench.SeedResult{}
}

// seed puts the data set in a bench that up already provisioned, then writes the
// descriptor again. Seed has no data of its own about Nexus: the version, the
// edition, the EULA consent and the hosts (and so the URLs that depend on them)
// come from the descriptor of the previous run, which is read before anything is
// copied. The preflight of a seed does not read the node, so it has no address to
// give.
func (s *session) seed() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	d, err := s.openDoors()
	if err != nil {
		return s.fail(err)
	}
	s.warnIfClearText()
	prev, err := s.previousDescriptor()
	if err != nil {
		return s.fail(err)
	}
	if _, err := s.d.preflight(s.ctx, runner, s.p, nexusbench.OpSeed); err != nil {
		return s.fail(err)
	}
	seeded, err := s.runSeed(runner, store, d)
	if err != nil {
		return s.fail(err)
	}
	provisioned := nexusbench.ProvisionResult{NexusVersion: prev.NexusVersion, Edition: prev.Edition, EULAAccepted: prev.EULAAccepted}
	return s.writeDescriptor(nexusbench.DeployResult{ReachableHosts: prev.Hosts}, provisioned, seeded)
}

// previousDescriptor reads the descriptor of the previous run. Without a readable
// descriptor of schema 2 (missing, schema 1, not JSON), or with the descriptor of
// another bench (namespace or context), the bench was not provisioned by this
// build for these parameters, and the answer is to run up.
func (s *session) previousDescriptor() (nexusbench.Descriptor, error) {
	path := s.descriptorPath()
	prev, err := nexusbench.LoadDescriptor(path)
	var be *nexusbench.BenchError
	switch {
	case err == nil:
	case errors.As(err, &be):
		return prev, err
	default:
		return prev, &nexusbench.BenchError{
			Kind:  nexusbench.Unclassified,
			Cause: "there is no readable descriptor of a provisioned bench (" + err.Error() + ")",
			Check: "run `mise run nexus-bench:up` first: the seed completes the descriptor that up writes",
			Err:   err,
		}
	}
	if prev.Namespace != s.p.Namespace || prev.KubeContext != s.p.Context {
		return prev, &nexusbench.BenchError{
			Kind:  nexusbench.Unclassified,
			Cause: fmt.Sprintf("the descriptor %s describes namespace %q on context %q, not namespace %q on context %q", path, prev.Namespace, prev.KubeContext, s.p.Namespace, s.p.Context),
			Check: "run `mise run nexus-bench:up` for this bench first: the seed completes the descriptor that up writes",
		}
	}
	if err := nexusbench.ValidateDescriptorHosts(prev.Hosts); err != nil {
		return prev, &nexusbench.BenchError{
			Kind:  nexusbench.Unclassified,
			Cause: fmt.Sprintf("the descriptor %s cannot be trusted: %s", path, err),
			Check: "run `mise run nexus-bench:up` for this bench first: it writes the descriptor again",
		}
	}
	return prev, nil
}

// down deletes the bench namespace. The descriptor is left alone: it is the
// versioned description of the bench, not part of one instance.
func (s *session) down() int {
	runner, store, err := s.open()
	if err != nil {
		return s.fail(err)
	}
	if err := s.d.destroy(s.ctx, runner, s.d.clock, store, s.p, s.stdout); err != nil {
		return s.fail(err)
	}
	return exitOK
}

// descriptorPath is where the descriptor is read and written.
func (s *session) descriptorPath() string {
	if s.p.DescriptorPath != "" {
		return s.p.DescriptorPath
	}
	return nexusbench.DescriptorPath()
}

// writeDescriptor writes the descriptor of a bench that was provisioned and, for
// up and seed, seeded. It is called after a step that succeeded only: a partial
// result is never described.
func (s *session) writeDescriptor(deployed nexusbench.DeployResult, provisioned nexusbench.ProvisionResult, seeded nexusbench.SeedResult) int {
	path := s.descriptorPath()
	if err := nexusbench.WriteDescriptor(path, nexusbench.BuildDescriptor(s.p, deployed, provisioned, seeded)); err != nil {
		return s.fail(err)
	}
	_, _ = fmt.Fprintf(s.stdout, "nexus-bench: descriptor written to %s\n", path)
	if !describesDefaultBench(s.p) {
		s.warn("descriptor.json now describes a non-default bench; do not commit it")
	}
	return exitOK
}

// describesDefaultBench tells whether p is the bench the versioned descriptor
// describes. The timeouts, the paths and the EULA consent do not change what
// the bench is, so they are left out.
func describesDefaultBench(p nexusbench.Params) bool {
	normalize := func(p nexusbench.Params) nexusbench.Params {
		p.Timeout, p.DeleteTimeout = 0, 0
		p.SecretsPath, p.DescriptorPath = "", ""
		p.AcceptEULA = false
		if len(p.Hosts) == 0 {
			p.Hosts = nil
		}
		return p
	}
	return reflect.DeepEqual(normalize(p), normalize(nexusbench.Defaults()))
}
