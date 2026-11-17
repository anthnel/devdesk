package nexusbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// resetHalfWayAdvice ends the Check of every error a reset returns after its first
// deletion: what was deleted stays deleted, and running the reset again starts over.
const resetHalfWayAdvice = "the reset stopped half-way: run `mise run nexus-bench:reset` again, it starts over"

// errRepositoriesNeverEmpty marks the failure of waitRepositoriesEmpty that is not
// cured by running the reset again.
var errRepositoriesNeverEmpty = errors.New("the repositories never emptied")

// storedPasswordOnly is the InitialPassword of a reset: it never reads the pod, so
// Provision uses the stored admin password and changes no password.
type storedPasswordOnly struct{}

// Read reports that there is no initial password.
func (storedPasswordOnly) Read(context.Context) (string, bool, error) { return "", false, nil }

// Reset brings the data set of the bench back to its initial state without
// recreating the instance (CA7): no namespace, Nexus pod or nexus-data write. It
// runs RunPreflight(OpReset) itself, as Destroy does, because what it deletes
// cannot be undone. It never reads the initial password of the pod and never
// changes a password: a bench whose stored admin password is refused, or whose
// claim of the upstream the bench did not create, is left untouched.
//
// The order is: preflight, custom upstream, loopback origin, stored credentials
// (checked by one admin read), label of the claim, then the deletion of
// docker-group, docker-proxy and docker-hosted, the recycling of the upstream
// storage, Provision, the wait for the two repositories to list nothing (H-REPODEL)
// and Seed. A failure after the first deletion carries resetHalfWayAdvice, but the
// repositories that never empty. Every failure is a *BenchError, but a cancelled
// context, which is returned as is. A nil out discards the progress lines.
func Reset(ctx context.Context, d SeedDeps, baseURL string, p Params, out io.Writer) (ProvisionResult, SeedResult, error) {
	var none ProvisionResult
	if out == nil {
		out = io.Discard
	}
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(out, "reset: "+format+"\n", args...) }
	if err := ctx.Err(); err != nil {
		return none, SeedResult{}, err
	}
	if _, err := RunPreflight(ctx, d.Runner, p, OpReset); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return none, SeedResult{}, cerr
		}
		return none, SeedResult{}, err
	}
	if !p.UpstreamIsBench() {
		return none, SeedResult{}, customUpstreamError(p)
	}
	if err := requireLoopbackBase(baseURL); err != nil {
		return none, SeedResult{}, err
	}
	_, _, c, err := seedCredentials(ctx, d.Store, baseURL)
	if err != nil {
		return none, SeedResult{}, err
	}
	defer c.closeIdle()
	if err := c.requireAdmin(ctx); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return none, SeedResult{}, cerr
		}
		return none, SeedResult{}, asBenchError(err, p, baseURL)
	}
	dep := &deployer{pf: preflight{r: d.Runner, p: p}, w: upstreamWorkload(), tag: "upstream", clk: d.Clock, out: out}
	if _, _, err := dep.checkedUpstreamClaim(ctx); err != nil {
		return none, SeedResult{}, err
	}

	// halfWay returns err with the advice to run the reset again, once something
	// was deleted.
	halfWay := func(err error) (ProvisionResult, SeedResult, error) {
		if cerr := ctx.Err(); cerr != nil {
			return none, SeedResult{}, cerr
		}
		var be *BenchError
		if !errors.As(err, &be) {
			return none, SeedResult{}, err
		}
		half := *be
		half.Check = be.Check + ". " + resetHalfWayAdvice
		return none, SeedResult{}, &half
	}

	say("deleting the repositories %s", strings.Join(benchRepositories, ", "))
	for _, name := range benchRepositories {
		if _, err := c.deleteRepository(ctx, name); err != nil {
			return halfWay(asBenchError(err, p, baseURL))
		}
	}
	say("replacing the storage of the upstream")
	if err := recycleUpstreamStorage(ctx, d.Runner, d.Clock, p, out); err != nil {
		return halfWay(err)
	}
	prov, err := Provision(ctx, baseURL, storedPasswordOnly{}, d.Store, p, out)
	if err != nil {
		return halfWay(err)
	}
	say("waiting for %s and %s to list nothing", dockerHostedRepository, dockerProxyRepository)
	if err := waitRepositoriesEmpty(ctx, c, d.Clock, dockerHostedRepository, dockerProxyRepository); err != nil {
		if errors.Is(err, errRepositoriesNeverEmpty) {
			return none, SeedResult{}, err
		}
		return halfWay(asBenchError(err, p, baseURL))
	}
	seed, err := Seed(ctx, d, baseURL, p, out)
	if err != nil {
		return halfWay(err)
	}
	say("the bench is back to its initial state")
	return prov, seed, nil
}

// waitRepositoriesEmpty polls the repositories until none lists a component, for at
// most seedListWait (H-REPODEL): the deletion of a repository empties its storage
// in the background. Like the other waits, it looks at the deadline after a
// reading. The repositories that still list something are named in a SeedFailed
// error that points at the log of the pod, as running the reset again would wait
// for the same thing.
func waitRepositoriesEmpty(ctx context.Context, c *client, clk Clock, repos ...string) error {
	deadline := clk.Now().Add(seedListWait)
	for {
		var left []string
		for _, repo := range repos {
			comps, err := c.listComponents(ctx, repo)
			if err != nil {
				return err
			}
			for _, comp := range comps {
				left = append(left, repo+": "+comp.Name+":"+comp.Version)
			}
		}
		if len(left) == 0 {
			return nil
		}
		if !clk.Now().Before(deadline) {
			return &BenchError{
				Kind:  SeedFailed,
				Cause: fmt.Sprintf("the repositories still list components after %s: %s", seedListWait, shownNames(left)),
				Check: "read the log of the Nexus pod (kubectl logs deploy/nexus): the deletion of a repository did not empty its storage",
				Err:   errRepositoriesNeverEmpty,
			}
		}
		if err := clk.Sleep(ctx, seedListInterval); err != nil {
			return err
		}
	}
}

// recycleUpstreamStorage empties the upstream registry by replacing its claim:
// the upstream is cut, the claim bench-upstream-data (and nothing else) is
// deleted once its label is checked, recreated from its manifest alone, and the
// upstream is brought back to one ready replica. The delete is sent once and never
// retried; the wait for the claim to be gone lasts until Params.DeleteTimeout.
// Like CutUpstream, it runs no preflight (the caller runs OpReset), refuses a
// custom upstream before any call, returns a cancelled context as is and accepts
// a nil out. A claim that is already gone is not deleted again, so a run that
// stopped between the delete and the apply starts again where it stopped.
func recycleUpstreamStorage(ctx context.Context, r Runner, clk Clock, p Params, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	if err := validateNamespace(p.Namespace); err != nil {
		return err
	}
	if err := CutUpstream(ctx, r, clk, p, out); err != nil {
		return err
	}
	d := &deployer{pf: preflight{r: r, p: p}, w: upstreamWorkload(), tag: "upstream", clk: clk, out: out}
	if err := d.deleteUpstreamClaim(ctx); err != nil {
		return err
	}
	claim, err := renderUpstreamClaim(p)
	if err != nil {
		return err
	}
	d.say("creating the claim %s again", upstreamClaimName)
	if err := d.apply(ctx, "the claim "+upstreamClaimName, claim); err != nil {
		return err
	}
	return EnsureUpstream(ctx, r, clk, p, out)
}

// checkedUpstreamClaim reads the claim of the upstream and checks that its label
// proves the bench created it; found is false when the claim is not there. It
// returns the UID of the claim it read.
func (d *deployer) checkedUpstreamClaim(ctx context.Context) (uid string, found bool, err error) {
	uid, labels, found, err := d.readUpstreamClaim(ctx)
	if err != nil || !found {
		return "", found, err
	}
	if labels[managedByLabel] != managedByValue {
		return "", true, &BenchError{
			Kind:  Unclassified,
			Cause: fmt.Sprintf("the claim %s does not carry the label %s=%s, so the bench did not create it and will not delete it", upstreamClaimName, managedByLabel, managedByValue),
			Check: d.pf.command("get", "pvc", upstreamClaimName, "-n", d.pf.p.Namespace, "--show-labels"),
		}
	}
	return uid, true, nil
}

// deleteUpstreamClaim deletes the claim of the upstream, once its label proves
// the bench created it, and waits until it is gone. A claim that is not there is
// left as it is. The delete carries a precondition on the UID that was read, so a
// claim replaced since the read is not deleted: kubectl has no flag for it, hence
// the raw form with a DeleteOptions body. A claim read without a UID is refused:
// an empty precondition would protect nothing.
func (d *deployer) deleteUpstreamClaim(ctx context.Context) error {
	if err := validateNamespace(d.pf.p.Namespace); err != nil {
		return err
	}
	uid, found, err := d.checkedUpstreamClaim(ctx)
	if err != nil || !found {
		return err
	}
	if uid == "" {
		return &BenchError{
			Kind:  Unclassified,
			Cause: fmt.Sprintf("kubectl returned the claim %s without a UID, so the delete cannot carry its precondition and is not sent", upstreamClaimName),
			Check: d.pf.command("get", "pvc", upstreamClaimName, "-n", d.pf.p.Namespace, "-o", "jsonpath={.metadata.uid}"),
		}
	}
	body, err := json.Marshal(map[string]any{
		"kind":          "DeleteOptions",
		"apiVersion":    "v1",
		"preconditions": map[string]string{"uid": uid},
	})
	if err != nil {
		return err
	}
	d.say("deleting the claim %s", upstreamClaimName)
	path := "/api/v1/namespaces/" + d.pf.p.Namespace + "/persistentvolumeclaims/" + upstreamClaimName
	args := []string{"delete", "--raw", path, "-f", "-", applyTimeout}
	if _, errOut, err := d.pf.r.Run(ctx, body, args...); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return d.pf.failure("delete the claim "+upstreamClaimName, args, string(errOut), err)
	}
	return d.waitUpstreamClaimGone(ctx)
}

// readUpstreamClaim reads the claim of the upstream by name; found is false when
// it is not there.
func (d *deployer) readUpstreamClaim(ctx context.Context) (uid string, labels map[string]string, found bool, err error) {
	var claim struct {
		Metadata struct {
			UID    string            `json:"uid"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}
	args := []string{"get", "pvc", upstreamClaimName, "-n", d.pf.p.Namespace, "-o", "json"}
	found, err = d.fetch(ctx, "read the claim "+upstreamClaimName, args, &claim, true)
	return claim.Metadata.UID, claim.Metadata.Labels, found, err
}

// waitUpstreamClaimGone polls the claim until it is not there, or until
// Params.DeleteTimeout passes. Like the other waits, it looks at the deadline
// after a reading.
func (d *deployer) waitUpstreamClaimGone(ctx context.Context) error {
	p := d.pf.p
	deadline := d.clk.Now().Add(p.DeleteTimeout)
	for {
		_, _, found, err := d.readUpstreamClaim(ctx)
		if err != nil || !found {
			return err
		}
		if !d.clk.Now().Before(deadline) {
			return &BenchError{
				Kind:  Deadline,
				Cause: fmt.Sprintf("the claim %s is still terminating after %s: a finalizer (kubernetes.io/pvc-protection) may hold it", upstreamClaimName, p.DeleteTimeout),
				Check: d.pf.command("get", "pvc", upstreamClaimName, "-n", p.Namespace, "-o", "yaml"),
			}
		}
		if err := d.clk.Sleep(ctx, upstreamPollInterval); err != nil {
			return err
		}
	}
}
