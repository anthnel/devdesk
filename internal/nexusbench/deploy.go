package nexusbench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// fieldManager owns the fields the bench applies; a second apply under the
	// same manager is a no-op, which is what makes a second up idempotent.
	fieldManager = "devdesk-nexus-bench"

	// podSelector selects the Nexus pod, the label 20-deployment.yaml.tmpl sets.
	podSelector = "app.kubernetes.io/name=nexus"

	// pollInterval separates two rounds of the wait.
	pollInterval = 5 * time.Second

	// pendingGrace is how long a "cannot be scheduled" or "claim not bound"
	// reading must last before it is reported: the scheduler and the volume
	// provisioner settle within seconds on a healthy cluster.
	pendingGrace = 30 * time.Second

	// applyTimeout bounds one apply, which may go through admission webhooks.
	applyTimeout = "--request-timeout=30s"

	// Reasons of the container states the wait reacts to.
	reasonErrImagePull     = "ErrImagePull"
	reasonImagePullBackOff = "ImagePullBackOff"
	reasonCrashLoop        = "CrashLoopBackOff"
	reasonOOMKilled        = "OOMKilled"
)

// Deploy applies the manifests of the bench and waits until Nexus answers on a
// NodePort. The order is fixed:
//
//  1. RunPreflight(OpUp): nothing is written before it succeeds
//  2. the context is looked at, then the Namespace is applied alone (server-side)
//  3. the whole stream of Render(p) is applied, the Namespace again included
//  4. a wait polls the pods and the claims of the namespace, diagnoses them
//     (PodPending, ImagePull, Crash), then asks the Prober about every host
//
// It returns as soon as one host answers, with the hosts that answered in that
// round; a host that stays mute is a warning on out. The deadline is
// Params.Timeout. Every failure is a *BenchError, but a cancelled context, which
// is returned as is. Neither the stderr of kubectl nor the error of the prober
// reaches a message; the Err of a BenchError keeps the former for errors.Is.
func Deploy(ctx context.Context, r Runner, pr Prober, clk Clock, p Params, out io.Writer) (DeployResult, error) {
	if out == nil {
		out = io.Discard
	}
	pre, err := RunPreflight(ctx, r, p, OpUp)
	if err != nil {
		return DeployResult{}, err
	}
	// The runner of a test may ignore ctx; the first write must not depend on it.
	if err := ctx.Err(); err != nil {
		return DeployResult{}, err
	}
	nsDoc, err := RenderNamespace(p)
	if err != nil {
		return DeployResult{}, fmt.Errorf("render the Namespace: %w", err)
	}
	fullDoc, err := Render(p)
	if err != nil {
		return DeployResult{}, fmt.Errorf("render the manifests: %w", err)
	}

	d := &deployer{pf: preflight{r: r, p: p}, pr: pr, clk: clk, out: out, hosts: deployHosts(p, pre.NodeIP)}
	d.say("applying the Namespace %s", p.Namespace)
	if err := d.apply(ctx, "the Namespace", nsDoc); err != nil {
		return DeployResult{}, err
	}
	d.say("applying the manifests")
	if err := d.apply(ctx, "the manifests", fullDoc); err != nil {
		return DeployResult{}, err
	}
	reached, err := d.wait(ctx)
	if err != nil {
		return DeployResult{}, err
	}
	return DeployResult{ReachableHosts: reached, NodeIP: pre.NodeIP}, nil
}

// deployHosts are the hosts the NodePorts are tried on: Params.Hosts when set,
// else the loopback address and the node's InternalIP when there is one.
func deployHosts(p Params, nodeIP string) []string {
	if len(p.Hosts) > 0 {
		return slices.Clone(p.Hosts)
	}
	hosts := []string{loopbackHost}
	if nodeIP != "" {
		hosts = append(hosts, nodeIP)
	}
	return hosts
}

// deployer is the state of one Deploy run.
type deployer struct {
	pf    preflight // the runner and the parameters, and its error builders
	pr    Prober
	clk   Clock
	out   io.Writer
	hosts []string

	lastSaid     string      // the last state line, so a line is not repeated
	pendingKey   string      // Cause of the pending reading being timed
	pendingSince time.Time   // when that reading was first seen
	candidate    *BenchError // the pending reading of the last round, if any
}

func (d *deployer) say(format string, args ...any) {
	_, _ = fmt.Fprintf(d.out, "deploy: "+format+"\n", args...)
}

// ---- apply ------------------------------------------------------------------

// apply sends doc to "kubectl apply --server-side". what names it in messages.
func (d *deployer) apply(ctx context.Context, what string, doc []byte) error {
	args := []string{"apply", "--server-side", "--field-manager=" + fieldManager, "-f", "-", applyTimeout}
	_, errOut, err := d.pf.r.Run(ctx, doc, args...)
	if err == nil {
		return nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return d.applyFailure(what, string(errOut), err)
}

// applyFailure classifies a failed apply from its stderr; the stderr itself is
// dropped from the message.
func (d *deployer) applyFailure(what, stderr string, err error) *BenchError {
	p := d.pf.p
	be := &BenchError{Kind: classifyKubectl(stderr), Err: err}
	switch be.Kind {
	case NodePortTaken:
		be.Cause = "could not apply " + what + ": a NodePort is already allocated"
		be.Check = d.pf.command("get", "services", "-A", "-o", "json") + ", then set " + envHTTPNodePort + " and " + envDockerNodePort + " to free ports"
	case AdmissionDenied:
		be.Cause = "could not apply " + what + ": an admission webhook denied the request"
		be.Check = d.pf.command("get", "events", "-A", "--sort-by=.lastTimestamp") + ", then read the policy that denied it"
	case Forbidden:
		be.Cause = "could not apply " + what + ": the API server refused the request (forbidden)"
		be.Check = d.pf.command("auth", "can-i", "--list", "-n", p.Namespace)
	case ClusterUnreachable:
		be.Cause = "could not apply " + what + ": the cluster did not answer"
		be.Check = d.pf.command("get", "--raw", "/readyz")
	default:
		be.Kind = Unclassified
		be.Cause = "kubectl failed to apply " + what
		be.Check = "mise run nexus-bench:render, then " + d.pf.command("apply", "--server-side", "--dry-run=server", "-f", "-")
	}
	return be
}

// ---- the wait ---------------------------------------------------------------

// wait polls until a host answers, a diagnosis fails the run, or the deadline
// passes. A round reads, diagnoses, then probes; the deadline is looked at after
// the round, so the cluster is read once more at the very end.
func (d *deployer) wait(ctx context.Context) ([]string, error) {
	deadline := d.clk.Now().Add(d.pf.p.Timeout)
	for {
		obs, err := d.observe(ctx)
		if err != nil {
			return nil, err
		}
		failure, pending := diagnose(d.pf.p, obs)
		if failure != nil {
			return nil, failure
		}
		d.candidate = pending
		if err := d.trackPending(); err != nil {
			return nil, err
		}
		d.report(obs)
		reached, silent := d.probeHosts(ctx)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(reached) > 0 {
			d.warnSilent(reached, silent)
			return reached, nil
		}
		if !d.clk.Now().Before(deadline) {
			return nil, d.deadlineError(obs)
		}
		if err := d.clk.Sleep(ctx, pollInterval); err != nil {
			return nil, err
		}
	}
}

// trackPending returns the pending reading of the round once it has lasted for
// pendingGrace without a break; a reading that changes or goes away restarts the
// count, so a claim that binds when the pod is scheduled is not a failure.
func (d *deployer) trackPending() error {
	if d.candidate == nil {
		d.pendingKey = ""
		return nil
	}
	if d.candidate.Cause != d.pendingKey {
		d.pendingKey, d.pendingSince = d.candidate.Cause, d.clk.Now()
	}
	if d.clk.Now().Sub(d.pendingSince) >= pendingGrace {
		return d.candidate
	}
	return nil
}

// deadlineError is the end of a wait that ran out of time. A pending reading
// that was still there says more than the deadline does.
func (d *deployer) deadlineError(obs observation) error {
	p := d.pf.p
	if d.candidate != nil {
		return d.candidate
	}
	return &BenchError{
		Kind: Deadline,
		Cause: fmt.Sprintf("Nexus did not answer on NodePort %d (tried %s) within %s; last state: %s",
			p.HTTPNodePort, strings.Join(d.hosts, ", "), p.Timeout, obs.describe()),
		Check: describePodCommand(p) + ", then raise " + envTimeout + " if the image pull or the start is only slow; `mise run nexus-bench:down` removes the bench",
	}
}

// report prints the state of the cluster when it changed since the last line.
func (d *deployer) report(obs observation) {
	if line := obs.describe(); line != d.lastSaid {
		d.lastSaid = line
		d.say("%s", line)
	}
}

// probeHosts asks the Prober about every host at once and splits them into the
// ones that answered and the ones that did not, each in the order of d.hosts.
// The error of the prober is dropped: it names the URL and the cause is enough
// in the final message.
func (d *deployer) probeHosts(ctx context.Context) (reached, silent []string) {
	urls := hostURLs(d.hosts, d.pf.p.HTTPNodePort)
	ok := make([]bool, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Go(func() { ok[i] = d.pr.Ready(ctx, u) == nil })
	}
	wg.Wait()
	for i, host := range d.hosts {
		if ok[i] {
			reached = append(reached, host)
		} else {
			silent = append(silent, host)
		}
	}
	return reached, silent
}

func (d *deployer) warnSilent(reached, silent []string) {
	for _, host := range silent {
		d.say("warning: %s did not answer on NodePort %d, although %s did; it will not be listed (check that it is reachable from this machine)",
			host, d.pf.p.HTTPNodePort, strings.Join(reached, ", "))
	}
}

// ---- reading the cluster ----------------------------------------------------

// observation is what one round read: the pods of Nexus and its claim.
type observation struct {
	pods []podObject
	pvc  *claimObject
}

type podObject struct {
	Metadata struct {
		Name              string  `json:"name"`
		DeletionTimestamp *string `json:"deletionTimestamp"`
	} `json:"metadata"`
	Status struct {
		Phase      string `json:"phase"`
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
		ContainerStatuses []containerStatus `json:"containerStatuses"`
	} `json:"status"`
}

type containerStatus struct {
	Ready        bool `json:"ready"`
	RestartCount int  `json:"restartCount"`
	State        struct {
		Waiting    *struct{ Reason string } `json:"waiting"`
		Running    *struct{}                `json:"running"`
		Terminated *struct{ Reason string } `json:"terminated"`
	} `json:"state"`
	LastState struct {
		Terminated *struct{ Reason string } `json:"terminated"`
	} `json:"lastState"`
}

type claimObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// observe reads the pods of Nexus and the claims of the namespace.
func (d *deployer) observe(ctx context.Context) (observation, error) {
	var obs observation
	ns := d.pf.p.Namespace
	var pods struct {
		Items []podObject `json:"items"`
	}
	if err := d.read(ctx, "read the Nexus pod", []string{"get", "pods", "-n", ns, "-l", podSelector, "-o", "json"}, &pods); err != nil {
		return obs, err
	}
	var claims struct {
		Items []claimObject `json:"items"`
	}
	if err := d.read(ctx, "read the volume claim", []string{"get", "pvc", "-n", ns, "-o", "json"}, &claims); err != nil {
		return obs, err
	}
	obs.pods = pods.Items
	if len(claims.Items) > 0 {
		obs.pvc = &claims.Items[0]
	}
	return obs, nil
}

// read runs a read-only kubectl call and decodes its JSON answer into into.
func (d *deployer) read(ctx context.Context, what string, args []string, into any) error {
	out, errOut, err := d.pf.remote(ctx, args...)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return d.pf.failure("could not "+what, args, string(errOut), err)
	}
	if err := json.Unmarshal([]byte(out), into); err != nil {
		return &BenchError{
			Kind:  Unclassified,
			Cause: "kubectl answered with something that is not the answer to \"" + what + "\"",
			Check: d.pf.command(args...),
			Err:   err,
		}
	}
	return nil
}

// describe is the one-line state of the round, for the progress and the deadline.
func (o observation) describe() string {
	var parts []string
	if len(o.pods) == 0 {
		parts = append(parts, "no pod matches "+podSelector+" yet")
	}
	for _, pod := range o.pods {
		parts = append(parts, pod.describe())
	}
	if o.pvc == nil {
		parts = append(parts, "no volume claim yet")
	} else {
		parts = append(parts, fmt.Sprintf("claim %s is %s", o.pvc.Metadata.Name, o.pvc.Status.Phase))
	}
	return strings.Join(parts, "; ")
}

func (pod podObject) describe() string {
	s := fmt.Sprintf("pod %s: phase %s", pod.Metadata.Name, pod.Status.Phase)
	if len(pod.Status.ContainerStatuses) == 0 {
		if _, ok := pod.unschedulable(); ok {
			return s + ", cannot be scheduled"
		}
		return s + ", not started"
	}
	cs := pod.Status.ContainerStatuses[0]
	switch {
	case cs.State.Waiting != nil:
		return s + ", container waiting (" + cs.State.Waiting.Reason + ")"
	case cs.State.Running != nil && cs.Ready:
		return s + ", container running and ready"
	case cs.State.Running != nil:
		return s + ", container running, not ready"
	}
	return s + ", container not running"
}

// ---- diagnosis --------------------------------------------------------------

// diagnose reads a round. failure is a state that ends the wait at once;
// pending is a state that does (when it lasts) but may also be passing.
func diagnose(p Params, obs observation) (failure, pending *BenchError) {
	for _, pod := range obs.pods {
		if pod.Metadata.DeletionTimestamp != nil {
			continue
		}
		if f := podFailure(p, pod); f != nil {
			return f, nil
		}
	}
	return nil, pendingReading(p, obs)
}

// podFailure is a pull that fails or a container that crashes.
func podFailure(p Params, pod podObject) *BenchError {
	for _, cs := range pod.Status.ContainerStatuses {
		waiting := ""
		if cs.State.Waiting != nil {
			waiting = cs.State.Waiting.Reason
		}
		last := ""
		healthy := cs.Ready && cs.State.Running != nil
		switch {
		case healthy:
			// An old termination stays in lastState while the pod lives: a
			// container that runs and is ready again is not crashing.
		case cs.LastState.Terminated != nil:
			last = cs.LastState.Terminated.Reason
		case cs.State.Terminated != nil:
			last = cs.State.Terminated.Reason
		}
		switch {
		case waiting == reasonErrImagePull || waiting == reasonImagePullBackOff:
			return &BenchError{
				Kind:  ImagePull,
				Cause: fmt.Sprintf("the image %s could not be pulled (%s)", p.ImageRef(), waiting),
				Check: describePodCommand(p) + ", then check " + envImage + ", " + envImageTag + " and the access of the cluster to the registry",
			}
		case waiting == reasonCrashLoop || last == reasonOOMKilled:
			return crashFailure(p, waiting, last)
		}
	}
	return nil
}

func crashFailure(p Params, waiting, last string) *BenchError {
	state := "restarted"
	if waiting != "" {
		state = waiting
	}
	if last == "" {
		last = "unknown"
	}
	return &BenchError{
		Kind:  Crash,
		Cause: fmt.Sprintf("the Nexus container keeps crashing (%s, last termination: %s) with a memory limit of %s", state, last, p.MemoryLimit),
		Check: describePodCommand(p) + " and `kubectl --context " + p.Context + " -n " + p.Namespace + " logs deploy/nexus --previous`; raise " + envMemoryLimit + " when it was " + reasonOOMKilled,
	}
}

// pendingReading is a pod that cannot be scheduled, or a claim that stays
// Pending although a pod is scheduled. The pod comes first: while it is not
// scheduled, a claim of a WaitForFirstConsumer class is Pending for that reason
// alone.
func pendingReading(p Params, obs observation) *BenchError {
	scheduled := false
	for _, pod := range obs.pods {
		if pod.Metadata.DeletionTimestamp != nil {
			continue
		}
		if msg, ok := pod.unschedulable(); ok {
			return unschedulableReading(p, msg)
		}
		scheduled = scheduled || pod.scheduled()
	}
	if scheduled && obs.pvc != nil && obs.pvc.Status.Phase == "Pending" {
		return &BenchError{
			Kind:  PodPending,
			Cause: fmt.Sprintf("the volume claim %s (%s of storage) is still Pending although the pod is scheduled", obs.pvc.Metadata.Name, p.StorageSize),
			Check: kubectlIn(p, "describe", "pvc", obs.pvc.Metadata.Name) + ", then lower " + envStorageSize + " or check the default StorageClass",
		}
	}
	return nil
}

func unschedulableReading(p Params, msg string) *BenchError {
	var lacks, envs []string
	if strings.Contains(msg, "Insufficient memory") {
		lacks = append(lacks, "memory (requested "+p.MemoryRequest+")")
		envs = append(envs, envMemoryRequest)
	}
	if strings.Contains(msg, "Insufficient cpu") {
		lacks = append(lacks, "cpu (requested "+p.CPURequest+")")
		envs = append(envs, envCPURequest)
	}
	be := &BenchError{Kind: PodPending, Cause: "the Nexus pod cannot be scheduled", Check: describePodCommand(p)}
	if len(lacks) > 0 {
		be.Cause += ": the node has not enough " + strings.Join(lacks, " and ") + " left"
		be.Check += ", then lower " + strings.Join(envs, " or ")
	}
	return be
}

// scheduled reports whether the PodScheduled condition is True.
func (pod podObject) scheduled() bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == "PodScheduled" && c.Status == "True" {
			return true
		}
	}
	return false
}

// unschedulable returns the message of the Unschedulable PodScheduled=False
// condition, and whether the pod has one.
func (pod podObject) unschedulable() (message string, ok bool) {
	for _, c := range pod.Status.Conditions {
		if c.Type == "PodScheduled" && c.Status == "False" && c.Reason == "Unschedulable" {
			return c.Message, true
		}
	}
	return "", false
}

// describePodCommand is the kubectl command that describes the Nexus pod.
func describePodCommand(p Params) string {
	return kubectlIn(p, "describe", "pod", "-l", podSelector)
}

// kubectlIn is a kubectl command line, in the namespace of the bench, a user can
// run to repeat a diagnosis.
func kubectlIn(p Params, args ...string) string {
	return "kubectl --context " + p.Context + " -n " + p.Namespace + " " + strings.Join(args, " ")
}
