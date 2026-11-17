package main

// Tests of the NX-02b side of the nexus-bench command (DEVDESK-534, plan step 7,
// DEVDESK-554), written before the code. They pin down, on the `deps` of main.go
// and the harness of main_test.go, which gains four fakes (wireUpstreamOps below):
//
//	cutUpstream     func(ctx, r Runner, clk Clock, p Params, out io.Writer) error
//	restoreUpstream func(ctx, d SeedDeps, baseURL string, p Params, out io.Writer) error
//	reset           func(ctx, d SeedDeps, baseURL string, p Params, out io.Writer) (ProvisionResult, SeedResult, error)
//	unblockProxy    func(ctx, store SecretStore, baseURL string, p Params, out io.Writer) error
//
// and the subcommands cut-upstream, restore-upstream and reset, plus the change
// of `up` (D-Q: the proxy is unblocked when the upstream was found cut).
//
// Interpretations the tests fix (the report says them again):
//
//   - cut-upstream: preflight(OpCut), then cutUpstream. It builds no forwarder
//     and no copier (no skopeo needed), takes the common flags only (no
//     --rest-host, no --accept-eula) and never writes the descriptor.
//   - restore-upstream: doors first (a host without skopeo is refused before the
//     cluster is touched), preflight(OpRestore), then restoreUpstream under the
//     deadline Params.Timeout. --rest-host is limited to the loopback, "localhost"
//     included, because the library refuses a name (requireLoopbackBase). It never
//     writes the descriptor and does not need one.
//   - reset: doors, then the descriptor of the previous run (as seed: none, schema
//     1 or another bench is refused before any library call), then reset under the
//     deadline Params.Timeout. Reset runs RunPreflight(OpReset) itself (plan, D-P),
//     which the fakes cannot see: the command may or may not ask for a preflight of
//     its own, but if it does it asks for OpReset and before reset. The descriptor
//     is written only after a reset that succeeded, with the hosts of the previous
//     descriptor and what reset returned. No InitialPassword is built (no exec in
//     the pod): newInitial and the deps provision/seed are never called by it.
//   - up, D-Q: after Deploy, and before EnsureUpstream (which brings the upstream
//     back to one replica and so erases the evidence), `up` reads the upstream.
//     Found cut, it calls unblockProxy after Provision and before the seed. A
//     failed read stops up. Not read at all for an upstream the bench did not
//     deploy.
//   - up, found cut, with a --rest-host that is not a literal loopback address:
//     UnblockProxy refuses such a host (requireLoopbackBase), and a refusal after
//     EnsureUpstream would leave a proxy that stays blocked for ever, because a
//     second `up` would find the upstream ready. So `up` stops BEFORE
//     EnsureUpstream, says to run restore-upstream here, and leaves the upstream
//     cut. With the upstream found ready or starting the flag keeps working as
//     before.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// ---- fakes ------------------------------------------------------------------------

// opsRecorder holds what the fakes of cut-upstream, restore-upstream, reset and
// unblockProxy were given, and the knobs a test sets before h.run. The results
// of reset are the harness's provisionRes and seedRes.
type opsRecorder struct {
	cutRunner nexusbench.Runner
	cutClock  nexusbench.Clock
	cutParams nexusbench.Params

	restoreDeps         nexusbench.SeedDeps
	restoreURL          string
	restoreParams       nexusbench.Params
	restoreHadDeadline  bool
	restoreRemaining    time.Duration
	resetDeps           nexusbench.SeedDeps
	resetURL            string
	resetParams         nexusbench.Params
	resetHadDeadline    bool
	resetRemaining      time.Duration
	descriptorSeenAtRst bool // the descriptor still existed when the fake reset ran

	unblockStore  nexusbench.SecretStore
	unblockURL    string
	unblockParams nexusbench.Params

	cutErr, restoreErr, resetErr, unblockErr error
	blockRestore, blockReset                 bool // the fake waits for its context to end
	onCut, onRestore, onReset                func()
}

// wireUpstreamOps adds the four fakes to d; harness.deps calls it.
func (h *harness) wireUpstreamOps(d *deps) {
	deadlineOf := func(ctx context.Context) (bool, time.Duration) {
		if deadline, ok := ctx.Deadline(); ok {
			return true, time.Until(deadline)
		}
		return false, 0
	}
	d.cutUpstream = func(ctx context.Context, r nexusbench.Runner, clk nexusbench.Clock, p nexusbench.Params, out io.Writer) error {
		h.log("cutUpstream")
		h.ops.cutRunner, h.ops.cutClock, h.ops.cutParams = r, clk, p
		_, _ = fmt.Fprintln(out, "upstream: "+progressMarker+" cut")
		if h.ops.onCut != nil {
			h.ops.onCut()
		}
		if h.respectContext && ctx.Err() != nil {
			return fmt.Errorf("cut: %w", ctx.Err())
		}
		return h.ops.cutErr
	}
	d.restoreUpstream = func(ctx context.Context, sd nexusbench.SeedDeps, baseURL string, p nexusbench.Params, out io.Writer) error {
		h.log("restoreUpstream")
		h.ops.restoreDeps, h.ops.restoreURL, h.ops.restoreParams = sd, baseURL, p
		h.ops.restoreHadDeadline, h.ops.restoreRemaining = deadlineOf(ctx)
		_, _ = fmt.Fprintln(out, "restore: "+progressMarker)
		if h.ops.onRestore != nil {
			h.ops.onRestore()
		}
		if h.ops.blockRestore {
			<-ctx.Done()
			return fmt.Errorf("restore: %w", ctx.Err())
		}
		if h.respectContext && ctx.Err() != nil {
			return fmt.Errorf("restore: %w", ctx.Err())
		}
		return h.ops.restoreErr
	}
	d.reset = func(ctx context.Context, sd nexusbench.SeedDeps, baseURL string, p nexusbench.Params, out io.Writer) (nexusbench.ProvisionResult, nexusbench.SeedResult, error) {
		h.log("reset")
		h.ops.resetDeps, h.ops.resetURL, h.ops.resetParams = sd, baseURL, p
		h.ops.resetHadDeadline, h.ops.resetRemaining = deadlineOf(ctx)
		if _, err := os.Stat(h.descriptorPathOf(p)); err == nil {
			h.ops.descriptorSeenAtRst = true
		}
		_, _ = fmt.Fprintln(out, "reset: "+progressMarker)
		if h.ops.onReset != nil {
			h.ops.onReset()
		}
		if h.ops.blockReset {
			<-ctx.Done()
			return nexusbench.ProvisionResult{}, nexusbench.SeedResult{}, fmt.Errorf("reset: %w", ctx.Err())
		}
		if h.respectContext && ctx.Err() != nil {
			return nexusbench.ProvisionResult{}, nexusbench.SeedResult{}, fmt.Errorf("reset: %w", ctx.Err())
		}
		if h.ops.resetErr != nil {
			return nexusbench.ProvisionResult{}, nexusbench.SeedResult{}, h.ops.resetErr
		}
		return h.provisionRes, h.seedRes, nil
	}
	d.unblockProxy = func(_ context.Context, store nexusbench.SecretStore, baseURL string, p nexusbench.Params, out io.Writer) error {
		h.log("unblockProxy")
		h.ops.unblockStore, h.ops.unblockURL, h.ops.unblockParams = store, baseURL, p
		_, _ = fmt.Fprintln(out, "restore: "+progressMarker+" unblockProxy")
		return h.ops.unblockErr
	}
}

// opsEvents keeps what touches the cluster, in order, the NX-02b calls included.
func (h *harness) opsEvents() []string {
	var out []string
	for _, e := range h.eventList() {
		switch {
		case strings.HasPrefix(e, "preflight:"), e == "deploy", e == "readUpstream", e == "ensureUpstream",
			e == "provision", e == "seed", e == "destroy", e == "cutUpstream", e == "restoreUpstream",
			e == "reset", e == "unblockProxy":
			out = append(out, e)
		}
	}
	return out
}

// requireNotCalled fails when one of the events was logged.
func (h *harness) requireNotCalled(events ...string) {
	h.t.Helper()
	for _, e := range events {
		if h.has(e) {
			h.t.Errorf("%s must not run here, events: %v", e, h.eventList())
		}
	}
}

// prepare writes what a subcommand needs on disk before it runs: reset needs the
// descriptor of a provisioned bench (like seed), the others need nothing. It
// returns the bytes of the descriptor, nil when there is none.
func (h *harness) prepare(sub string) []byte {
	h.t.Helper()
	if sub == "reset" {
		return h.writePreviousDescriptor()
	}
	return nil
}

// upstreamOps are the three subcommands of NX-02b.
var upstreamOps = []string{"cut-upstream", "restore-upstream", "reset"}

// requireOpsEvents fails when the cluster events of h are not exactly want, or when
// reset (which preflights inside the library) shows a preflight of another kind.
func requireOpsEvents(t *testing.T, h *harness, sub string, want []string) {
	t.Helper()
	got := h.opsEvents()
	if sub == "reset" {
		// Reset runs RunPreflight(OpReset) itself: a preflight of the command is
		// tolerated, as long as it is the one of reset and comes first.
		if len(got) > 0 && got[0] == "preflight:OpReset" {
			got = got[1:]
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s: cluster events = %v, want %v (all: %v)", sub, got, want, h.eventList())
	}
}

// ---- the three subcommands: order, what they are given ------------------------------

func TestCutUpstreamRunsItsPreflightThenCuts(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("cut-upstream", "--context", "ctx-cut", "--namespace", "nb-cut", "--http-nodeport", "31003")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if got, want := h.opsEvents(), []string{"preflight:OpCut", "cutUpstream"}; !slices.Equal(got, want) {
		t.Fatalf("cluster events = %v, want %v (all: %v)", got, want, h.eventList())
	}
	// CutUpstream runs no preflight of its own, so the command asks for OpCut and
	// for nothing else.
	if len(h.preflightOps) != 1 || h.preflightOps[0] != nexusbench.OpCut {
		t.Errorf("preflight ops = %v, want [OpCut]", h.preflightOps)
	}
	if h.preflightRunner != nexusbench.Runner(h.runner) || h.ops.cutRunner != nexusbench.Runner(h.runner) {
		t.Error("the preflight and CutUpstream were not given the runner of the deps")
	}
	if h.ops.cutClock != nexusbench.Clock(h.clock) {
		t.Error("CutUpstream was not given the clock of the deps")
	}
	for _, p := range []nexusbench.Params{h.preflightParams[0], h.ops.cutParams} {
		if p.Context != "ctx-cut" || p.Namespace != "nb-cut" || p.HTTPNodePort != 31003 {
			t.Errorf("params = %+v, want the flags", p)
		}
	}
	if !reflectEqualParams(h.preflightParams[0], h.ops.cutParams) {
		t.Errorf("the preflight and CutUpstream were given different parameters:\n%+v\n%+v", h.preflightParams[0], h.ops.cutParams)
	}
	// No REST, no skopeo: neither door is built, nothing is provisioned or seeded.
	h.requireNotCalled("newForwarder", "newCopier", "newInitial", "deploy", "provision", "ensureUpstream", "seed",
		"restoreUpstream", "reset", "unblockProxy", "destroy", "readImages:docker-hosted", "readImages:docker-proxy")
	if !h.before("newRunner", "preflight:OpCut") {
		t.Errorf("the runner must be built before the preflight, events: %v", h.eventList())
	}
	if !strings.Contains(stdout, "upstream: "+progressMarker+" cut") {
		t.Errorf("stdout lost the progress lines of CutUpstream:\n%s", stdout)
	}
	if h.descriptorExists() {
		t.Error("cut-upstream wrote the descriptor")
	}
	requireClean(t, stdout, stderr)
}

func TestRestoreUpstreamRunsItsPreflightThenRestores(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("restore-upstream", "--context", "ctx-rs", "--namespace", "nb-rs", "--http-nodeport", "31002")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if got, want := h.opsEvents(), []string{"preflight:OpRestore", "restoreUpstream"}; !slices.Equal(got, want) {
		t.Fatalf("cluster events = %v, want %v (all: %v)", got, want, h.eventList())
	}
	if len(h.preflightOps) != 1 || h.preflightOps[0] != nexusbench.OpRestore {
		t.Errorf("preflight ops = %v, want [OpRestore]", h.preflightOps)
	}
	// RestoreUpstream copies nothing but port-forwards and reads digests through
	// skopeo: both doors exist before the cluster is touched.
	for _, e := range []string{"newRunner", "newStore", "newForwarder", "newCopier"} {
		if !h.before(e, "preflight:OpRestore") {
			t.Errorf("%s must happen before the preflight, events: %v", e, h.eventList())
		}
	}
	sd := h.ops.restoreDeps
	if sd.Runner != nexusbench.Runner(h.runner) || sd.Forwarder != nexusbench.PortForwarder(h.forward) ||
		sd.Copier != nexusbench.ImageCopier(h.copier) || sd.Clock != nexusbench.Clock(h.clock) || sd.Store != nexusbench.SecretStore(h.store) {
		t.Errorf("SeedDeps = %+v, want the runner, forwarder, copier, clock and store of the deps", sd)
	}
	if want := "http://127.0.0.1:31002"; h.ops.restoreURL != want {
		t.Errorf("RestoreUpstream base URL = %q, want %q", h.ops.restoreURL, want)
	}
	if p := h.ops.restoreParams; p.Context != "ctx-rs" || p.Namespace != "nb-rs" || p.HTTPNodePort != 31002 {
		t.Errorf("RestoreUpstream params = %+v, want the flags", p)
	}
	if !reflectEqualParams(h.preflightParams[0], h.ops.restoreParams) {
		t.Error("the preflight and RestoreUpstream were given different parameters")
	}
	h.requireNotCalled("newInitial", "deploy", "provision", "ensureUpstream", "seed", "cutUpstream", "reset",
		"unblockProxy", "destroy")
	if !strings.Contains(stdout, "restore: "+progressMarker) {
		t.Errorf("stdout lost the progress lines of RestoreUpstream:\n%s", stdout)
	}
	if h.descriptorExists() {
		t.Error("restore-upstream wrote the descriptor, and has no data about the bench to describe")
	}
	requireClean(t, stdout, stderr)
}

func TestResetRunsTheLibraryResetAndWritesTheDescriptorAfterSuccess(t *testing.T) {
	h := newHarness(t)
	resetParams := func(p *nexusbench.Params) { p.Context, p.Namespace, p.HTTPNodePort = "ctx-rst", "nb-rst", 31004 }
	prev := h.writePreviousDescriptorFor(resetParams)
	code, stdout, stderr := h.run("reset", "--context", "ctx-rst", "--namespace", "nb-rst", "--http-nodeport", "31004")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	requireOpsEvents(t, h, "reset", []string{"reset"})
	for _, op := range h.preflightOps {
		if op != nexusbench.OpReset {
			t.Errorf("reset ran a preflight for %s, want OpReset only", opName(op))
		}
	}
	for _, e := range []string{"newRunner", "newStore", "newForwarder", "newCopier"} {
		if !h.before(e, "reset") {
			t.Errorf("%s must happen before reset, events: %v", e, h.eventList())
		}
	}
	// What Reset needs and the command does not touch: the stored secrets are
	// read by the library with no exec in the pod, so no InitialPassword is built
	// and neither Provision nor Seed is called by the command.
	h.requireNotCalled("newInitial", "deploy", "provision", "ensureUpstream", "seed", "cutUpstream",
		"restoreUpstream", "unblockProxy", "destroy")
	if calls := h.runner.calls(); len(calls) != 0 {
		t.Errorf("the command itself ran kubectl: %v", calls)
	}
	sd := h.ops.resetDeps
	if sd.Runner != nexusbench.Runner(h.runner) || sd.Forwarder != nexusbench.PortForwarder(h.forward) ||
		sd.Copier != nexusbench.ImageCopier(h.copier) || sd.Clock != nexusbench.Clock(h.clock) || sd.Store != nexusbench.SecretStore(h.store) {
		t.Errorf("SeedDeps = %+v, want the runner, forwarder, copier, clock and store of the deps", sd)
	}
	if want := "http://127.0.0.1:31004"; h.ops.resetURL != want {
		t.Errorf("Reset base URL = %q, want %q", h.ops.resetURL, want)
	}
	if p := h.ops.resetParams; p.Context != "ctx-rst" || p.Namespace != "nb-rst" || p.HTTPNodePort != 31004 {
		t.Errorf("Reset params = %+v, want the flags", p)
	}
	if !h.ops.descriptorSeenAtRst {
		t.Error("the descriptor of the previous run should still be there while Reset runs")
	}

	// The descriptor: the hosts of the previous descriptor (the node is not read),
	// what Reset returned, as seed does.
	p := nexusbench.Defaults()
	resetParams(&p)
	p.UpstreamURL = "http://bench-upstream.nb-rst.svc.cluster.local:5000"
	got, err := os.ReadFile(h.descriptor)
	if err != nil {
		t.Fatalf("no descriptor: %v", err)
	}
	want := wantDescriptorBytesWith(t, p, previousDeployment, h.provisionRes, h.seedRes)
	if !bytes.Equal(got, want) {
		t.Errorf("descriptor bytes differ from BuildDescriptor with the previous hosts and the result of Reset:\n got %s\nwant %s", got, want)
	}
	before, after := mustParseDescriptor(t, prev), mustParseDescriptor(t, got)
	if !slices.Equal(after.Hosts, before.Hosts) {
		t.Errorf("hosts after the reset = %v, want those of the previous descriptor %v", after.Hosts, before.Hosts)
	}
	if !strings.Contains(stdout, "reset: "+progressMarker) || !strings.Contains(stdout, h.descriptor) {
		t.Errorf("stdout lacks the lines of Reset or the descriptor path:\n%s", stdout)
	}
	requireClean(t, stdout, stderr)
}

// "Idempotent, byte for byte": on a bench that is as seed leaves it, reset
// writes the very file seed writes.
func TestResetWritesTheBytesSeedWritesOnTheSameBench(t *testing.T) {
	seeded := newHarness(t)
	seeded.provisionRes = previousProvisioning
	seeded.writePreviousDescriptor()
	if code, stdout, stderr := seeded.run("seed"); code != 0 {
		t.Fatalf("seed: exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	want, err := os.ReadFile(seeded.descriptor)
	if err != nil {
		t.Fatal(err)
	}

	reset := newHarness(t)
	reset.provisionRes = previousProvisioning // what Provision reports of the same instance
	reset.writePreviousDescriptor()
	if code, stdout, stderr := reset.run("reset"); code != 0 {
		t.Fatalf("reset: exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	got, err := os.ReadFile(reset.descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("reset and seed write different bytes on the same bench:\nreset %s\nseed  %s", got, want)
	}
}

// ---- the descriptor: cut and restore never write it, reset only after success -----------

func TestCutAndRestoreNeverTouchTheDescriptor(t *testing.T) {
	for _, sub := range []string{"cut-upstream", "restore-upstream"} {
		for _, existing := range []bool{false, true} {
			name := sub + " / no descriptor"
			if existing {
				name = sub + " / a descriptor exists"
			}
			t.Run(name, func(t *testing.T) {
				h := newHarness(t)
				var before []byte
				if existing {
					before = h.writePreviousDescriptor()
				}
				if code, stdout, stderr := h.run(sub); code != 0 {
					t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
				}
				after, err := os.ReadFile(h.descriptor)
				switch {
				case existing && (err != nil || !bytes.Equal(before, after)):
					t.Errorf("the descriptor was changed (err=%v)", err)
				case !existing && err == nil:
					t.Errorf("a descriptor was written: %s", after)
				}
			})
		}
	}
}

func TestResetNeedsTheDescriptorOfAProvisionedBench(t *testing.T) {
	v1 := []byte(`{"schemaVersion": 1, "kubeContext": "datomir", "namespace": "nexus-bench"}` + "\n")
	for _, tt := range []struct {
		name  string
		setup func(h *harness) []byte
	}{
		{"no descriptor", func(*harness) []byte { return nil }},
		{"a descriptor of schema 1", func(h *harness) []byte {
			if err := os.MkdirAll(filepath.Dir(h.descriptor), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(h.descriptor, v1, 0o644); err != nil {
				t.Fatal(err)
			}
			return v1
		}},
		{"another namespace", func(h *harness) []byte {
			return h.writePreviousDescriptorFor(func(p *nexusbench.Params) { p.Namespace = "other-bench" })
		}},
		{"another context", func(h *harness) []byte {
			return h.writePreviousDescriptorFor(func(p *nexusbench.Params) { p.Context = "other-context" })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			before := tt.setup(h)
			code, stdout, stderr := h.run("reset")
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, "nexus-bench:up") {
				t.Errorf("stderr does not say to run nexus-bench:up:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			// Refused before any library call: nothing was deleted.
			if got := h.opsEvents(); len(got) != 0 {
				t.Errorf("a library call or a preflight ran without a descriptor to complete: %v", got)
			}
			if before == nil {
				if h.descriptorExists() {
					t.Error("a descriptor was written")
				}
			} else if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
				t.Errorf("the descriptor was rewritten (err=%v)", err)
			}
		})
	}
}

// ---- failures ---------------------------------------------------------------------------

// A failure at any step stops the command, is printed as its message and nothing
// else (the cause can carry a secret), exits 1 and leaves the descriptor alone.
func TestUpstreamOpsFailuresExitWithOneAndKeepTheDescriptor(t *testing.T) {
	be := benchError()
	skopeoMissing := &nexusbench.BenchError{Kind: nexusbench.SkopeoMissing, Cause: "skopeo was not found on PATH", Check: "install skopeo", Err: errors.New("exec: " + secretMarker)}
	type row struct {
		name      string
		subs      []string
		setup     func(h *harness)
		notCalled []string
	}
	rows := []row{
		{"the library call fails", upstreamOps, func(h *harness) {
			h.ops.cutErr, h.ops.restoreErr, h.ops.resetErr = be, be, be
			// Reset returns what it had put in place together with the error: it is never described.
			h.seedRes = nexusbench.SeedResult{Images: nexusbench.Dataset()[:2]}
		}, nil},
		{"the library call fails with an error that is not a BenchError", upstreamOps, func(h *harness) {
			e := errors.New("the call was interrupted")
			h.ops.cutErr, h.ops.restoreErr, h.ops.resetErr = e, e, e
		}, nil},
		{"the preflight refuses", []string{"cut-upstream", "restore-upstream"}, func(h *harness) { h.preflightErr = be }, []string{"cutUpstream", "restoreUpstream"}},
		{"kubectl is missing", upstreamOps, func(h *harness) {
			h.newRunnerErr = &nexusbench.BenchError{Kind: nexusbench.KubectlMissing, Cause: "kubectl was not found on PATH", Check: "run `mise install`"}
		}, []string{"preflight:OpCut", "preflight:OpRestore", "cutUpstream", "restoreUpstream", "reset"}},
		{"the secrets path is refused", upstreamOps, func(h *harness) {
			h.newStoreErr = &nexusbench.BenchError{Cause: "the secrets path is in a repository and not ignored", Check: "set NEXUS_BENCH_SECRETS"}
		}, []string{"preflight:OpCut", "preflight:OpRestore", "cutUpstream", "restoreUpstream", "reset"}},
		// skopeo is a prerequisite of restore and reset, not of cut: it is refused
		// before the cluster is touched.
		{"skopeo is missing", []string{"restore-upstream", "reset"}, func(h *harness) { h.newCopierErr = skopeoMissing }, []string{"preflight:OpRestore", "restoreUpstream", "reset"}},
		{"the forwarder cannot be built", []string{"restore-upstream", "reset"}, func(h *harness) { h.newForwarderErr = be }, []string{"preflight:OpRestore", "restoreUpstream", "reset"}},
	}
	for _, tt := range rows {
		for _, sub := range tt.subs {
			t.Run(sub+" / "+tt.name, func(t *testing.T) {
				h := newHarness(t)
				tt.setup(h)
				before := h.prepare(sub)
				code, stdout, stderr := h.run(sub)
				if code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
				want := firstErrorText(h, sub)
				if got := strings.TrimSpace(stderr); got != want {
					t.Errorf("stderr = %q, want exactly %q", got, want)
				}
				requireClean(t, stdout, stderr)
				h.requireNotCalled(tt.notCalled...)
				if before == nil {
					if h.descriptorExists() {
						t.Error("a descriptor was written although the run failed")
					}
				} else if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
					t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
				}
			})
		}
	}
}

// cut-upstream has no use of skopeo: a host without it can still cut.
func TestCutUpstreamDoesNotNeedSkopeo(t *testing.T) {
	h := newHarness(t)
	h.newCopierErr = &nexusbench.BenchError{Kind: nexusbench.SkopeoMissing, Cause: "skopeo was not found on PATH", Check: "install skopeo"}
	h.newForwarderErr = benchError()
	if code, stdout, stderr := h.run("cut-upstream"); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	h.requireNotCalled("newCopier", "newForwarder")
}

// firstErrorText is the message the command must print for the error the harness
// was programmed with: the text of its BenchError, or the text of the plain error.
func firstErrorText(h *harness, sub string) string {
	var errs []error
	switch sub {
	case "cut-upstream":
		errs = []error{h.newRunnerErr, h.newStoreErr, h.preflightErr, h.ops.cutErr}
	case "restore-upstream":
		errs = []error{h.newRunnerErr, h.newStoreErr, h.newForwarderErr, h.newCopierErr, h.preflightErr, h.ops.restoreErr}
	default:
		errs = []error{h.newRunnerErr, h.newStoreErr, h.newForwarderErr, h.newCopierErr, h.ops.resetErr}
	}
	for _, err := range errs {
		if err == nil {
			continue
		}
		var be *nexusbench.BenchError
		if errors.As(err, &be) {
			return be.Error()
		}
		return err.Error()
	}
	h.t.Fatal("test setup: no error programmed")
	return ""
}

// ---- --rest-host: the loopback only, for restore-upstream and reset --------------------

func TestRestoreAndResetAcceptOnlyALoopbackRestHost(t *testing.T) {
	for _, sub := range []string{"restore-upstream", "reset"} {
		for _, tt := range []struct{ name, host, wantURL string }{
			{"the default", "", "http://127.0.0.1:30481"},
			{"the loopback", "127.0.0.1", "http://127.0.0.1:30481"},
			{"the IPv6 loopback", "::1", "http://[::1]:30481"},
		} {
			t.Run(sub+" accepts "+tt.name, func(t *testing.T) {
				h := newHarness(t)
				h.prepare(sub)
				args := []string{sub}
				if tt.host != "" {
					args = append(args, "--rest-host", tt.host)
				}
				code, stdout, stderr := h.run(args...)
				if code != 0 {
					t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
				}
				got := h.ops.restoreURL
				if sub == "reset" {
					got = h.ops.resetURL
				}
				if got != tt.wantURL {
					t.Errorf("base URL = %q, want %q", got, tt.wantURL)
				}
				if stderr != "" {
					t.Errorf("stderr = %q, want empty on the loopback", stderr)
				}
				requireClean(t, stdout, stderr)
			})
		}
		// "localhost" is a name: the library refuses it (requireLoopbackBase), so the
		// command refuses it too, before any call, rather than after the preflight.
		// 127.0.0.2, ::ffff:127.0.0.1 and 0:0:0:0:0:0:0:1 are loopback addresses
		// the library refuses (it takes 127.0.0.1 and ::1 only): one rule for both.
		for _, host := range []string{"192.168.1.31", "fd00::1", "nexus.lan", "10.0.0.1", "localhost", "127.0.0.2", "::ffff:127.0.0.1", "0:0:0:0:0:0:0:1"} {
			t.Run(sub+" refuses "+host, func(t *testing.T) {
				h := newHarness(t)
				before := h.prepare(sub)
				code, stdout, stderr := h.run(sub, "--rest-host", host)
				if code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
				lower := strings.ToLower(stderr)
				if !strings.Contains(lower, "127.0.0.1") || !strings.Contains(lower, sub) {
					t.Errorf("stderr does not say that %s always goes through 127.0.0.1:\n%s", sub, stderr)
				}
				if strings.Contains(stderr, "credentials travel in clear text") {
					t.Errorf("%s warned instead of refusing:\n%s", sub, stderr)
				}
				requireClean(t, stdout, stderr)
				h.requireNoCalls()
				if before != nil {
					if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
						t.Errorf("the descriptor was changed (err=%v)", err)
					}
				}
			})
		}
		for _, host := range []string{"a/b", "user@host", "bad host", "http://127.0.0.1", "127.0.0.1:30481"} {
			t.Run(sub+" not a host "+host, func(t *testing.T) {
				h := newHarness(t)
				h.prepare(sub)
				code, stdout, stderr := h.run(sub, "--rest-host="+host)
				if (code != 1 && code != 2) || strings.TrimSpace(stderr) == "" {
					t.Errorf("exit code = %d, stderr %q", code, stderr)
				}
				requireClean(t, stdout, stderr)
				h.requireNoCalls()
			})
		}
	}
}

// ---- parameters: flag, variable, default -------------------------------------------------

func TestUpstreamOpsReadTheirParametersFromTheEnvironmentAndTheFlags(t *testing.T) {
	for _, sub := range upstreamOps {
		t.Run(sub+" / the variables", func(t *testing.T) {
			h := newHarness(t)
			h.env["NEXUS_BENCH_NAMESPACE"] = "nb-env"
			h.env["NEXUS_BENCH_CONTEXT"] = "ctx-env"
			h.env["NEXUS_BENCH_HTTP_NODEPORT"] = "31010"
			if sub == "reset" {
				h.writePreviousDescriptorFor(func(p *nexusbench.Params) { p.Namespace, p.Context = "nb-env", "ctx-env" })
			}
			if code, stdout, stderr := h.run(sub); code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			p := opParams(h, sub)
			if p.Namespace != "nb-env" || p.Context != "ctx-env" || p.HTTPNodePort != 31010 {
				t.Errorf("params = %+v, want the variables", p)
			}
		})
		t.Run(sub+" / a flag beats its variable", func(t *testing.T) {
			h := newHarness(t)
			h.env["NEXUS_BENCH_NAMESPACE"] = "nb-env"
			if sub == "reset" {
				h.writePreviousDescriptorFor(func(p *nexusbench.Params) { p.Namespace = "nb-flag" })
			}
			if code, stdout, stderr := h.run(sub, "--namespace", "nb-flag"); code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if p := opParams(h, sub); p.Namespace != "nb-flag" {
				t.Errorf("namespace = %q, want nb-flag", p.Namespace)
			}
		})
	}
}

// opParams are the parameters the fake of a subcommand was given.
func opParams(h *harness, sub string) nexusbench.Params {
	switch sub {
	case "cut-upstream":
		return h.ops.cutParams
	case "restore-upstream":
		return h.ops.restoreParams
	}
	return h.ops.resetParams
}

// reflectEqualParams compares two Params by value.
func reflectEqualParams(a, b nexusbench.Params) bool { return reflect.DeepEqual(a, b) }

// ---- the deadline and the cancellation --------------------------------------------------

func TestRestoreAndResetAreBoundedByTheTimeout(t *testing.T) {
	for _, sub := range []string{"restore-upstream", "reset"} {
		for _, tt := range []struct {
			name string
			args []string
			want time.Duration
		}{
			{"the flag", []string{"--timeout", "7m"}, 7 * time.Minute},
			{"the variable of the harness", nil, 30 * time.Second},
		} {
			t.Run(sub+" "+tt.name, func(t *testing.T) {
				h := newHarness(t)
				h.prepare(sub)
				code, stdout, stderr := h.run(append([]string{sub}, tt.args...)...)
				if code != 0 {
					t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
				}
				had, left := h.ops.restoreHadDeadline, h.ops.restoreRemaining
				if sub == "reset" {
					had, left = h.ops.resetHadDeadline, h.ops.resetRemaining
				}
				if !had {
					t.Fatal("the context of the library call has no deadline")
				}
				if left > tt.want || left < tt.want-10*time.Second {
					t.Errorf("time left on the context = %s, want just under %s", left, tt.want)
				}
			})
		}
	}
}

// A call that outlasts the timeout is a failure with its own message, which names
// the timeout and the way back (a reset or a restore that stopped half-way is run
// again); it writes no descriptor.
func TestARestoreOrResetThatOutlastsTheTimeoutFailsWithoutADescriptor(t *testing.T) {
	for _, sub := range []string{"restore-upstream", "reset"} {
		t.Run(sub, func(t *testing.T) {
			h := newHarness(t)
			h.ops.blockRestore, h.ops.blockReset = true, true
			before := h.prepare(sub)
			code, stdout, stderr := h.run(sub, "--timeout", "60ms")
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			for _, want := range []string{"60ms", "NEXUS_BENCH_TIMEOUT", "nexus-bench:" + sub} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr)
				}
			}
			requireClean(t, stdout, stderr)
			if before == nil {
				if h.descriptorExists() {
					t.Error("a descriptor was written")
				}
			} else if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
				t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
			}
		})
	}
}

func TestACancelledContextEndsTheUpstreamOpsCleanly(t *testing.T) {
	for _, sub := range upstreamOps {
		t.Run(sub+", cancelled before the run", func(t *testing.T) {
			h := newHarness(t)
			h.respectContext = true
			before := h.prepare(sub)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			h.ctx = ctx
			code, stdout, stderr := h.run(sub)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(strings.ToLower(stderr), "cancel") || strings.Count(strings.TrimSpace(stderr), "\n") != 0 {
				t.Errorf("stderr should say in one line that the run was cancelled:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			if before != nil {
				if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
					t.Errorf("the descriptor changed (err=%v)", err)
				}
			}
		})
		t.Run(sub+", cancelled during the call", func(t *testing.T) {
			h := newHarness(t)
			h.respectContext = true
			before := h.prepare(sub)
			ctx, cancel := context.WithCancel(context.Background())
			h.ctx = ctx
			h.ops.onCut, h.ops.onRestore, h.ops.onReset = cancel, cancel, cancel
			code, stdout, stderr := h.run(sub)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(strings.ToLower(stderr), "cancel") {
				t.Errorf("stderr does not say the run was cancelled:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			if before == nil {
				if h.descriptorExists() {
					t.Error("a descriptor was written after the cancellation")
				}
			} else if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
				t.Errorf("a descriptor was written after the cancellation (err=%v)", err)
			}
		})
	}
}

// A reset cancelled after its first deletion leaves a half-deleted data set: the
// library returns the cancellation as is, so the command is the one to say that
// running reset again starts over. The message stays on one line.
func TestACancelledResetSaysToRunItAgain(t *testing.T) {
	h := newHarness(t)
	h.respectContext = true
	h.prepare("reset")
	ctx, cancel := context.WithCancel(context.Background())
	h.ctx = ctx
	h.ops.onReset = cancel
	code, stdout, stderr := h.run("reset")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	for _, want := range []string{"cancel", "nexus-bench:reset", "again"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Count(strings.TrimSpace(stderr), "\n") != 0 {
		t.Errorf("stderr should stay on one line:\n%s", stderr)
	}
	requireClean(t, stdout, stderr)
}

// ---- a non-default bench ------------------------------------------------------------------

func TestResetOfANonDefaultBenchWarnsNotToCommitTheDescriptor(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		previous func(*nexusbench.Params)
		want     bool
	}{
		{"default bench, tuned timeout", []string{"--timeout", "5m"}, nil, false},
		{"namespace", []string{"--namespace", "nexus-bench-2"}, func(p *nexusbench.Params) { p.Namespace = "nexus-bench-2" }, true},
		{"http nodeport", []string{"--http-nodeport", "31001"}, func(p *nexusbench.Params) { p.HTTPNodePort = 31001 }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.writePreviousDescriptorFor(tt.previous)
			code, stdout, stderr := h.run(append([]string{"reset"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if got := strings.Contains(stderr, nonDefaultWarning); got != tt.want {
				t.Errorf("warning present = %v, want %v\nstderr: %s", got, tt.want, stderr)
			}
		})
	}
}

// ---- no password, ever ---------------------------------------------------------------------

// The store holds a password the fakes can see: nothing the command prints on any
// path may carry it, and no password is an argument of anything.
func TestUpstreamOpsPrintNoSecret(t *testing.T) {
	for _, sub := range upstreamOps {
		for _, failure := range []bool{false, true} {
			name := sub + " / success"
			if failure {
				name = sub + " / failure"
			}
			t.Run(name, func(t *testing.T) {
				h := newHarness(t)
				if failure {
					h.ops.cutErr, h.ops.restoreErr, h.ops.resetErr = benchError(), benchError(), benchError()
				}
				h.prepare(sub)
				_, stdout, stderr := h.run(sub)
				requireClean(t, stdout, stderr)
				for _, args := range h.runner.calls() {
					for _, a := range args {
						if strings.Contains(a, secretMarker) || strings.Contains(a, storeMarker) {
							t.Errorf("a secret reached kubectl's arguments: %v", args)
						}
					}
				}
			})
		}
	}
}

// ---- flags, help, usage -------------------------------------------------------------------

func TestUpstreamOpsHelpListsTheirFlags(t *testing.T) {
	common := []string{"namespace", "context", "http-nodeport", "timeout", "upstream-image", "upstream-url", "secrets", "descriptor"}
	for _, tt := range []struct {
		sub  string
		has  []string
		hasN []string // flags it must not list
	}{
		{"cut-upstream", common, []string{"rest-host", "accept-eula"}},
		{"restore-upstream", append([]string{"rest-host"}, common...), []string{"accept-eula"}},
		{"reset", append([]string{"rest-host"}, common...), []string{"accept-eula"}},
	} {
		for _, flag := range []string{"-h", "--help"} {
			t.Run(tt.sub+" "+flag, func(t *testing.T) {
				h := newHarness(t)
				code, stdout, stderr := h.run(tt.sub, flag)
				if code != 0 {
					t.Fatalf("exit code = %d", code)
				}
				if !strings.HasPrefix(stdout, "Usage: nexus-bench "+tt.sub+" [flags]") {
					t.Errorf("the help does not start with its usage line:\n%s", stdout)
				}
				for _, f := range tt.has {
					if !strings.Contains(stdout, f) {
						t.Errorf("the help of %s does not list %q:\n%s", tt.sub, f, stdout)
					}
				}
				for _, f := range tt.hasN {
					if strings.Contains(stdout, f) {
						t.Errorf("the help of %s lists %q, which it does not take:\n%s", tt.sub, f, stdout)
					}
				}
				if stderr != "" {
					t.Errorf("stderr = %q, want empty", stderr)
				}
				h.requireNoCalls()
			})
		}
	}
}

// The rest-host flag of restore-upstream and reset says it is a loopback address.
func TestRestAndResetRestHostHelpSaysLoopbackOnly(t *testing.T) {
	for _, sub := range []string{"restore-upstream", "reset"} {
		h := newHarness(t)
		_, stdout, _ := h.run(sub, "-h")
		_, flags, _ := strings.Cut(stdout, "Flags:")
		_, afterRest, found := strings.Cut(flags, "-rest-host")
		if !found {
			t.Fatalf("%s -h lists no --rest-host:\n%s", sub, stdout)
		}
		entry, _, _ := strings.Cut(afterRest, "\n  -")
		if !strings.Contains(entry, "127.0.0.1") || !strings.Contains(strings.ToLower(entry), "loopback") {
			t.Errorf("the --rest-host help of %s does not say it takes a loopback address:\n%s", sub, entry)
		}
		if strings.Contains(strings.ToLower(entry), "clear text") {
			t.Errorf("the --rest-host help of %s speaks of clear text, as if another host were allowed:\n%s", sub, entry)
		}
	}
}

// summaryOf is the text between the usage line and the flags of a subcommand help.
func summaryOf(t *testing.T, stdout string) string {
	t.Helper()
	_, rest, _ := strings.Cut(stdout, "\n\n")
	summary, _, found := strings.Cut(rest, "\nFlags:")
	if !found {
		t.Fatalf("the help has no Flags: section:\n%s", stdout)
	}
	return summary
}

func TestUpstreamOpsHelpSaysWhatTheyDoAndNeed(t *testing.T) {
	for _, tt := range []struct {
		sub  string
		want []string // lower-case words the summary must contain
	}{
		{"cut-upstream", []string{"upstream", "zero"}},
		{"restore-upstream", []string{"upstream", "skopeo"}},
		// reset says what it erases and what it keeps.
		{"reset", []string{"skopeo", "docker-hosted", "docker-proxy", "docker-group", "bench-upstream-data", "password", "namespace"}},
	} {
		t.Run(tt.sub, func(t *testing.T) {
			h := newHarness(t)
			_, stdout, _ := h.run(tt.sub, "-h")
			summary := strings.ToLower(summaryOf(t, stdout))
			for _, w := range tt.want {
				if !strings.Contains(summary, w) {
					t.Errorf("the summary of %s does not mention %q:\n%s", tt.sub, w, summary)
				}
			}
		})
	}
}

// entryOf returns an entry of the general help: its line and the lines indented
// more than it.
func entryOf(usage, name string) string {
	var out []string
	lines := strings.Split(usage, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "  "+name+" ") {
			continue
		}
		out = append(out, l)
		for _, next := range lines[i+1:] {
			if !strings.HasPrefix(next, "   ") {
				break
			}
			out = append(out, next)
		}
		break
	}
	return strings.Join(out, "\n")
}

func TestGeneralHelpListsTheUpstreamOps(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("help")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, sub := range upstreamOps {
		entry := entryOf(stdout, sub)
		if entry == "" {
			t.Errorf("the general help has no entry for %s:\n%s", sub, stdout)
			continue
		}
		lower := strings.ToLower(entry)
		if (sub == "restore-upstream" || sub == "reset") && !strings.Contains(lower, "skopeo") {
			t.Errorf("the entry of %s does not say it needs skopeo:\n%s", sub, entry)
		}
		if sub == "reset" && !strings.Contains(lower, "delete") && !strings.Contains(lower, "erase") {
			t.Errorf("the entry of reset does not say it deletes the data:\n%s", entry)
		}
	}
	// The entry of up still says it seeds.
	if up := entryOf(stdout, "up"); !strings.Contains(strings.ToLower(up), "seed") {
		t.Errorf("the entry of up does not say it seeds:\n%s", up)
	}
}

func TestUpstreamOpsUsageErrorsExitWithTwo(t *testing.T) {
	for _, tt := range []struct {
		sub  string
		args []string
	}{
		{"cut-upstream", []string{"--bogus"}},
		{"cut-upstream", []string{"extra"}},
		{"cut-upstream", []string{"--timeout", "soon"}},
		{"cut-upstream", []string{"--http-nodeport", "abc"}},
		{"cut-upstream", []string{"--namespace"}},
		{"cut-upstream", []string{"--accept-eula"}},
		{"cut-upstream", []string{"--rest-host", "127.0.0.1"}}, // no REST: no flag
		{"restore-upstream", []string{"--bogus"}},
		{"restore-upstream", []string{"extra"}},
		{"restore-upstream", []string{"--timeout", "soon"}},
		{"restore-upstream", []string{"--accept-eula"}},
		{"restore-upstream", []string{"--namespace"}},
		{"reset", []string{"--bogus"}},
		{"reset", []string{"extra"}},
		{"reset", []string{"--timeout", "soon"}},
		{"reset", []string{"--accept-eula"}},
		{"reset", []string{"--accept-eula=yes"}},
		{"reset", []string{"--namespace"}},
	} {
		t.Run(tt.sub+" "+strings.Join(tt.args, " "), func(t *testing.T) {
			h := newHarness(t)
			h.prepare(tt.sub)
			code, stdout, stderr := h.run(append([]string{tt.sub}, tt.args...)...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout != "" || strings.TrimSpace(stderr) == "" {
				t.Errorf("stdout %q, stderr %q: want an empty stdout and a message", stdout, stderr)
			}
			requireClean(t, stderr)
			h.requireNoCalls()
		})
	}
}

// ---- up, D-Q: the proxy is unblocked when the upstream was found cut --------------------

func TestUpUnblocksTheProxyWhenItFoundTheUpstreamCut(t *testing.T) {
	h := newHarness(t)
	h.upstreamState = nexusbench.UpstreamCut
	code, stdout, stderr := h.run("up", "--accept-eula")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	// Deploy leaves a cut upstream cut; it is read before EnsureUpstream brings it
	// back (afterwards nothing says it was cut), and the proxy is unblocked once
	// Provision has put its configuration in place and before the seed copies.
	got := h.opsEvents()
	if len(got) > 0 && got[0] == "preflight:OpUp" {
		got = got[1:]
	}
	if want := []string{"deploy", "readUpstream", "ensureUpstream", "provision", "unblockProxy", "seed"}; !slices.Equal(got, want) {
		t.Fatalf("cluster calls = %v, want %v (all: %v)", got, want, h.eventList())
	}
	if h.readUpstreamRunner != nexusbench.Runner(h.runner) {
		t.Error("the upstream was not read through the runner of the deps")
	}
	if h.ops.unblockStore != nexusbench.SecretStore(h.store) {
		t.Error("UnblockProxy was not given the store of the deps")
	}
	if h.ops.unblockURL != "http://127.0.0.1:30481" || h.ops.unblockURL != h.provisionURL {
		t.Errorf("UnblockProxy base URL = %q, Provision base URL = %q, want the same loopback origin", h.ops.unblockURL, h.provisionURL)
	}
	if !reflectEqualParams(h.ops.unblockParams, h.provisionParams) {
		t.Errorf("UnblockProxy was not given the parameters of the run:\n%+v\n%+v", h.ops.unblockParams, h.provisionParams)
	}
	last := -1
	for _, marker := range []string{progressMarker + " provision", progressMarker + " unblockProxy", "seed: " + progressMarker, h.descriptor} {
		i := strings.Index(stdout, marker)
		if i < 0 {
			t.Errorf("stdout lacks %q:\n%s", marker, stdout)
			continue
		}
		if i < last {
			t.Errorf("stdout shows %q out of order:\n%s", marker, stdout)
		}
		last = i
	}
	// The descriptor is what a plain up writes.
	data, err := os.ReadFile(h.descriptor)
	if err != nil {
		t.Fatalf("no descriptor: %v", err)
	}
	if want := wantDescriptorBytes(t, nexusbench.Defaults(), h.deployRes, h.provisionRes); !bytes.Equal(data, want) {
		t.Errorf("descriptor bytes differ from those of a plain up:\n got %s\nwant %s", data, want)
	}
	requireClean(t, stdout, stderr)
}

func TestUpDoesNotUnblockTheProxyOfAnUpstreamThatWasNotCut(t *testing.T) {
	for _, state := range []nexusbench.UpstreamState{nexusbench.UpstreamReady, nexusbench.UpstreamStarting, nexusbench.UpstreamMissing} {
		t.Run(fmt.Sprint("state ", int(state)), func(t *testing.T) {
			h := newHarness(t)
			h.upstreamState = state
			if code, stdout, stderr := h.run("up", "--accept-eula"); code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if !h.has("readUpstream") {
				t.Errorf("up did not read the upstream: %v", h.eventList())
			}
			h.requireNotCalled("unblockProxy")
		})
	}
}

// The state is read after Deploy and before EnsureUpstream, so a read that fails
// stops up there: it cannot tell whether the proxy needs unblocking.
func TestUpStopsWhenItCannotReadTheUpstream(t *testing.T) {
	h := newHarness(t)
	h.upstreamErr = benchError()
	before := h.writePreviousDescriptor()
	code, stdout, stderr := h.run("up", "--accept-eula")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if got := strings.TrimSpace(stderr); got != benchError().Error() {
		t.Errorf("stderr = %q, want exactly %q", got, benchError().Error())
	}
	requireClean(t, stdout, stderr)
	h.requireNotCalled("ensureUpstream", "provision", "unblockProxy", "seed")
	if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
		t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
	}
}

func TestUpStopsWhenTheProxyCannotBeUnblocked(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "no descriptor yet"
		if existing {
			name = "a descriptor exists"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.upstreamState = nexusbench.UpstreamCut
			h.ops.unblockErr = benchError()
			var before []byte
			if existing {
				before = h.writePreviousDescriptor()
			}
			code, stdout, stderr := h.run("up", "--accept-eula")
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			// The upstream is back by now, so a second up finds it ready and does not
			// unblock the proxy: the check says what to run instead.
			if got := strings.TrimSpace(stderr); !strings.HasPrefix(got, benchError().Error()) {
				t.Errorf("stderr = %q, want it to start with %q", got, benchError().Error())
			}
			if !strings.Contains(stderr, "mise run nexus-bench:restore-upstream") {
				t.Errorf("stderr does not tell to run mise run nexus-bench:restore-upstream:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			h.requireNotCalled("seed")
			if existing {
				if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
					t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
				}
			} else if h.descriptorExists() {
				t.Error("a descriptor was written although the run failed")
			}
		})
	}
}

// An upstream the bench did not deploy is not the command's to read or unblock:
// the NX-01 behavior stays.
func TestUpWithACustomUpstreamNeitherReadsNorUnblocks(t *testing.T) {
	h := newHarness(t)
	h.upstreamState = nexusbench.UpstreamCut // would be unblocked on the bench's own upstream
	code, stdout, stderr := h.run("up", "--accept-eula", "--upstream-url", "http://registry.example.com:5000")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	h.requireNotCalled("readUpstream", "unblockProxy", "ensureUpstream", "seed")
}

// The case the review raised. UnblockProxy refuses any REST host that is not a
// literal loopback address, and up accepts --rest-host for any host. Found cut with
// such a host, up cannot unblock the proxy, and failing after EnsureUpstream would
// leave it blocked for good (a second up finds the upstream ready and does not
// unblock). So up stops before EnsureUpstream: the upstream stays cut, the next up
// or restore-upstream finds it cut again, and the message says what to run.
func TestUpFoundCutWithARestHostThatIsNotTheLoopbackStopsBeforeBringingTheUpstreamBack(t *testing.T) {
	for _, host := range []string{"192.168.1.31", "fd00::1", "nexus.lan", "localhost", "127.0.0.2", "::ffff:127.0.0.1", "0:0:0:0:0:0:0:1"} {
		t.Run(host, func(t *testing.T) {
			h := newHarness(t)
			h.upstreamState = nexusbench.UpstreamCut
			before := h.writePreviousDescriptor()
			code, stdout, stderr := h.run("up", "--accept-eula", "--rest-host", host)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			for _, want := range []string{"restore-upstream", "127.0.0.1"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr does not say %q:\n%s", want, stderr)
				}
			}
			requireClean(t, stdout, stderr)
			// Deploy and the read happened (they are idempotent and write no replica
			// count); nothing after them.
			if !h.before("deploy", "readUpstream") {
				t.Errorf("events: %v, want deploy then readUpstream", h.eventList())
			}
			h.requireNotCalled("ensureUpstream", "provision", "unblockProxy", "seed")
			if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
				t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
			}
		})
	}
}

// The same flag, with an upstream that does not need unblocking, works as before
// (the clear-text warning of the credentials included), and the IPv6 loopback
// reaches UnblockProxy.
func TestUpRestHostKeepsWorkingOutsideTheCutCase(t *testing.T) {
	t.Run("ready upstream, another host", func(t *testing.T) {
		h := newHarness(t)
		code, stdout, stderr := h.run("up", "--accept-eula", "--rest-host", "192.168.1.31")
		if code != 0 {
			t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "credentials travel in clear text") {
			t.Errorf("no clear-text warning:\n%s", stderr)
		}
		h.requireNotCalled("unblockProxy")
	})
	t.Run("cut upstream, the IPv6 loopback", func(t *testing.T) {
		h := newHarness(t)
		h.upstreamState = nexusbench.UpstreamCut
		if code, stdout, stderr := h.run("up", "--accept-eula", "--rest-host", "::1"); code != 0 {
			t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if h.ops.unblockURL != "http://[::1]:30481" {
			t.Errorf("UnblockProxy base URL = %q, want http://[::1]:30481", h.ops.unblockURL)
		}
	})
}
