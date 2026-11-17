package nexusbench

import (
	"context"
	"fmt"
	"io"
	"slices"
	"time"
)

// upstreamPollInterval separates two rounds of the wait for the upstream. It is
// shorter than pollInterval: a registry is ready within seconds of its image
// being local, and the wait has no host to probe.
const upstreamPollInterval = 2 * time.Second

// UpstreamState is what the cluster says about the bench upstream registry.
type UpstreamState int

// The states of the upstream, from the least to the most alive.
const (
	// UpstreamMissing: the Deployment bench-upstream does not exist.
	UpstreamMissing UpstreamState = iota
	// UpstreamCut: the Deployment exists with zero replicas.
	UpstreamCut
	// UpstreamStarting: one replica is wanted and no pod of it is ready yet.
	UpstreamStarting
	// UpstreamReady: a pod of the upstream that is not terminating is ready.
	UpstreamReady
)

// ReadUpstream reads the state of the bench upstream with read-only kubectl
// calls: it writes nothing and waits for nothing. A pod that fails to pull is
// Starting, not an error: diagnosing is the job of the wait. It runs no
// preflight; the caller does. Every failure is a *BenchError, but a cancelled
// context, which is returned as is.
func ReadUpstream(ctx context.Context, r Runner, p Params) (UpstreamState, error) {
	d := &deployer{pf: preflight{r: r, p: p}, w: upstreamWorkload(), tag: "upstream", out: io.Discard}
	return d.readUpstream(ctx)
}

// EnsureUpstream brings the bench upstream to one ready replica, and returns
// when it is ready or fails with a diagnosis. It reads first and writes only
// when the upstream is cut: one scale to one replica, then the wait. A starting
// upstream is waited for without a write; a ready one is left alone. The
// Deployment itself is applied by Deploy, so a missing one is an error. A
// custom upstream (Params.UpstreamIsBench is false) is refused before any call:
// the bench scales what it deploys and nothing else. The wait polls the pods
// and the claim of the upstream only, until Params.Timeout. It runs no
// preflight; the caller does. Every failure is a *BenchError, but a cancelled
// context, which is returned as is. A nil out discards the progress lines.
func EnsureUpstream(ctx context.Context, r Runner, clk Clock, p Params, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	if !p.UpstreamIsBench() {
		return customUpstreamError(p)
	}
	// The runner of a test may ignore ctx; the first call must not depend on it.
	if err := ctx.Err(); err != nil {
		return err
	}
	d := &deployer{pf: preflight{r: r, p: p}, w: upstreamWorkload(), tag: "upstream", clk: clk, out: out}
	state, err := d.readUpstream(ctx)
	if err != nil {
		return err
	}
	switch state {
	case UpstreamMissing:
		return &BenchError{
			Kind:  Unclassified,
			Cause: fmt.Sprintf("the Deployment %s does not exist in namespace %q", upstreamDeployment, p.Namespace),
			Check: "run `mise run nexus-bench:up`, which applies it, or " + d.pf.command("get", "deployment", upstreamDeployment, "-n", p.Namespace),
		}
	case UpstreamReady:
		d.say("the upstream is ready")
		return nil
	case UpstreamCut:
		d.say("scaling the upstream to one replica")
		if err := d.scaleUpstream(ctx, 1); err != nil {
			return err
		}
	case UpstreamStarting:
		d.say("the upstream is starting")
	}
	return d.waitUpstream(ctx)
}

// customUpstreamError refuses an upstream the bench does not deploy.
func customUpstreamError(p Params) *BenchError {
	return &BenchError{
		Kind: Unclassified,
		Cause: fmt.Sprintf("%s points at an upstream the bench did not deploy in namespace %q, and the bench scales only its own upstream",
			envUpstreamURL, p.Namespace),
		Check: "unset " + envUpstreamURL + " to use the bench upstream, or start and stop your own upstream registry yourself",
	}
}

// readUpstream is ReadUpstream on a deployer.
func (d *deployer) readUpstream(ctx context.Context) (UpstreamState, error) {
	var dep struct {
		Spec struct {
			Replicas *int `json:"replicas"`
		} `json:"spec"`
	}
	args := []string{"get", "deployment", upstreamDeployment, "-n", d.pf.p.Namespace, "-o", "json"}
	found, err := d.fetch(ctx, "read the Deployment "+upstreamDeployment, args, &dep, true)
	if err != nil {
		return UpstreamMissing, err
	}
	if !found {
		return UpstreamMissing, nil
	}
	// The manifest leaves the count out, so the API server defaults it to one.
	if dep.Spec.Replicas != nil && *dep.Spec.Replicas == 0 {
		return UpstreamCut, nil
	}
	var pods struct {
		Items []podObject `json:"items"`
	}
	if err := d.read(ctx, "read the "+d.w.name+" pod", d.podsArgs(), &pods); err != nil {
		return UpstreamMissing, err
	}
	if slices.ContainsFunc(pods.Items, podObject.ready) {
		return UpstreamReady, nil
	}
	return UpstreamStarting, nil
}

// scaleUpstream is the one write of EnsureUpstream. A failed write is not
// retried: the scale is not what a second try would fix.
func (d *deployer) scaleUpstream(ctx context.Context, replicas int) error {
	args := []string{"scale", "deployment/" + upstreamDeployment, fmt.Sprintf("--replicas=%d", replicas), "-n", d.pf.p.Namespace, applyTimeout}
	_, errOut, err := d.pf.r.Run(ctx, nil, args...)
	if err == nil {
		return nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return d.pf.failure("scale the Deployment "+upstreamDeployment, args, string(errOut), err)
}

// waitUpstream polls until a pod of the upstream is ready, a diagnosis fails the
// run, or the deadline passes. Like the wait of Deploy, it looks at the deadline
// after a round, so the cluster is read once more at the very end.
func (d *deployer) waitUpstream(ctx context.Context) error {
	deadline := d.clk.Now().Add(d.pf.p.Timeout)
	for {
		obs, err := d.round(ctx)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if slices.ContainsFunc(obs.pods, podObject.ready) {
			d.say("the upstream is ready")
			return nil
		}
		if !d.clk.Now().Before(deadline) {
			return d.upstreamDeadline(obs)
		}
		if err := d.clk.Sleep(ctx, upstreamPollInterval); err != nil {
			return err
		}
	}
}

// upstreamDeadline is the end of a wait that ran out of time. A pending reading
// that was still there says more than the deadline does.
func (d *deployer) upstreamDeadline(obs observation) error {
	if d.candidate != nil {
		return d.candidate
	}
	p := d.pf.p
	return &BenchError{
		Kind:  Deadline,
		Cause: fmt.Sprintf("the upstream %s was not ready within %s; last state: %s", upstreamDeployment, p.Timeout, obs.describe()),
		Check: describePodCommand(p, d.w) + ", then raise " + envTimeout + " if the image pull or the start is only slow",
	}
}

// ready reports whether the pod is not terminating and its Ready condition is
// True.
func (pod podObject) ready() bool {
	if pod.Metadata.DeletionTimestamp != nil {
		return false
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == "Ready" {
			return c.Status == "True"
		}
	}
	return false
}
