package nexusbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	// destroyPollStart and destroyPollMax bound the interval between two reads
	// of a namespace that is still Terminating: it doubles from the first to
	// the second.
	destroyPollStart = time.Second
	destroyPollMax   = 10 * time.Second
)

// Destroy deletes the bench namespace, waits until it is gone, then reports the
// PersistentVolumes left behind and removes the stored secrets. Deleting a
// namespace cannot be undone, so the order is rigid:
//
//  1. RunPreflight(OpDown): the parameters (which refuse the namespaces the
//     bench must never touch), the cluster, the permission, and the label of
//     the namespace. A namespace without our label is refused here.
//  2. A namespace that is absent is "nothing to delete": success, and the
//     stored secrets of a bench that no longer exists are removed.
//  3. One write, `delete namespace <ns> --wait=false`, issued once and never
//     retried. Nothing else is ever written: a namespace stuck in Terminating
//     is reported, not forced.
//  4. The namespace is read until NotFound, or until p.DeleteTimeout.
//  5. One read of the PersistentVolumes: one that still points at a claim of
//     the namespace is a warning, not a failure (the data stays on the node).
//  6. The stored secrets are removed, last, and only when the namespace is gone.
//
// Every failure is a *BenchError, except a cancelled context, which is
// wrapped so that errors.Is(err, context.Canceled) holds. Nothing written to
// out or to an error carries a secret.
func Destroy(ctx context.Context, r Runner, clk Clock, store SecretStore, p Params, out io.Writer) error {
	if out == nil {
		out = io.Discard
	}
	res, err := RunPreflight(ctx, r, p, OpDown)
	if err != nil {
		return err
	}
	d := destroyer{r: r, clk: clk, p: p, out: out}
	if !res.NamespaceExists {
		d.say("nothing to delete: namespace %s does not exist", p.Namespace)
		return removeSecrets(store)
	}
	if err := d.deleteNamespace(ctx); err != nil {
		return err
	}
	if err := d.waitGone(ctx); err != nil {
		return err
	}
	d.warnAboutVolumes(ctx)
	d.say("namespace %s deleted", p.Namespace)
	return removeSecrets(store)
}

type destroyer struct {
	r   Runner
	clk Clock
	p   Params
	out io.Writer
}

func (d destroyer) say(format string, args ...any) {
	_, _ = fmt.Fprintf(d.out, "down: "+format+"\n", args...)
}

// command is the kubectl command line a user can run to repeat a call.
func (d destroyer) command(args ...string) string {
	return "kubectl --context " + d.p.Context + " " + strings.Join(args, " ")
}

// removeSecrets drops the stored secrets of the bench. The error of the store
// can name a path but never a secret.
func removeSecrets(store SecretStore) error {
	if err := store.Remove(); err != nil {
		return &BenchError{
			Kind:  Unclassified,
			Cause: "the namespace is gone but the stored secrets could not be removed",
			Check: "fix the permissions of the secrets file, then run `mise run nexus-bench:down` again (it finds nothing to delete and retries the removal)",
			Err:   err,
		}
	}
	return nil
}

// deleteNamespace issues the one write of Destroy. It is not retried.
func (d destroyer) deleteNamespace(ctx context.Context) error {
	ns := d.p.Namespace
	args := []string{"delete", "namespace", ns, "--wait=false", requestTimeout}
	_, errOut, err := d.r.Run(ctx, nil, args...)
	if err == nil || isNotFound(string(errOut)) {
		d.say("deleting namespace %s", ns)
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("delete namespace %s: %w", ns, ctx.Err())
	}
	kind := classifyKubectl(string(errOut))
	cause := fmt.Sprintf("could not delete namespace %q: kubectl failed", ns)
	if kind == Forbidden {
		cause = fmt.Sprintf("could not delete namespace %q: the API server refused the request (forbidden)", ns)
	}
	return &BenchError{Kind: kind, Cause: cause, Check: d.command("auth", "can-i", "delete", "namespaces"), Err: err}
}

// terminatingNamespace is the part of a Namespace the wait reads and reports.
type terminatingNamespace struct {
	Spec struct {
		Finalizers []string `json:"finalizers"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

// describe lists what holds the namespace: its finalizers and its conditions.
func (n terminatingNamespace) describe() string {
	finalizers := "none"
	if len(n.Spec.Finalizers) > 0 {
		finalizers = strings.Join(n.Spec.Finalizers, ", ")
	}
	var conds []string
	for _, c := range n.Status.Conditions {
		conds = append(conds, fmt.Sprintf("%s=%s (%s)", c.Type, c.Status, c.Message))
	}
	if len(conds) == 0 {
		conds = []string{"none reported"}
	}
	return fmt.Sprintf("spec.finalizers: %s; status.conditions: %s", finalizers, strings.Join(conds, "; "))
}

// waitGone reads the namespace until it is NotFound or p.DeleteTimeout passes.
func (d destroyer) waitGone(ctx context.Context) error {
	ns := d.p.Namespace
	args := []string{"get", "namespace", ns, "-o", "json"}
	deadline := d.clk.Now().Add(d.p.DeleteTimeout)
	interval := destroyPollStart
	var last terminatingNamespace
	for {
		out, errOut, err := d.r.Run(ctx, nil, append(args, requestTimeout)...)
		switch {
		case err != nil && isNotFound(string(errOut)):
			return nil
		case err != nil && ctx.Err() != nil:
			return fmt.Errorf("wait for namespace %s to go: %w", ns, ctx.Err())
		case err != nil:
			return &BenchError{
				Kind:  classifyKubectl(string(errOut)),
				Cause: fmt.Sprintf("could not read namespace %q while it terminates: kubectl failed", ns),
				Check: d.command(args...),
				Err:   err,
			}
		}
		// An unreadable answer keeps the last description: the wait goes on.
		var seen terminatingNamespace
		if json.Unmarshal(out, &seen) == nil {
			last = seen
		}
		remaining := deadline.Sub(d.clk.Now())
		if remaining <= 0 {
			return &BenchError{
				Kind:  Deadline,
				Cause: fmt.Sprintf("namespace %q is still Terminating after %s (%s)", ns, d.p.DeleteTimeout, last.describe()),
				Check: d.command(args...) + ", then clear what holds it; the bench never removes finalizers",
			}
		}
		if err := d.clk.Sleep(ctx, min(interval, remaining)); err != nil {
			return fmt.Errorf("wait for namespace %s to go: %w", ns, err)
		}
		interval = min(interval*2, destroyPollMax)
	}
}

// volumeList is the part of a PersistentVolume list the check reads.
type volumeList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			ClaimRef *struct {
				Namespace string `json:"namespace"`
			} `json:"claimRef"`
		} `json:"spec"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	} `json:"items"`
}

// warnAboutVolumes says which PersistentVolumes still point at a claim of the
// deleted namespace. It never fails the run: the namespace is already gone, and
// a volume that outlives it (reclaim policy Retain) is not the bench's to
// delete.
func (d destroyer) warnAboutVolumes(ctx context.Context) {
	args := []string{"get", "pv", "-o", "json"}
	out, _, err := d.r.Run(ctx, nil, append(args, requestTimeout)...)
	var list volumeList
	if err == nil {
		err = json.Unmarshal(out, &list)
	}
	if err != nil {
		if !errors.Is(ctx.Err(), context.Canceled) {
			d.say("warning: could not check for PersistentVolumes left behind. Check: %s", d.command(args...))
		}
		return
	}
	for _, pv := range list.Items {
		if pv.Spec.ClaimRef != nil && pv.Spec.ClaimRef.Namespace == d.p.Namespace {
			d.say("warning: PersistentVolume %s still exists (phase %s) and points at a claim of namespace %s; its reclaim policy decides whether it goes. Check: %s",
				pv.Metadata.Name, pv.Status.Phase, d.p.Namespace, d.command("get", "pv", pv.Metadata.Name))
		}
	}
}
