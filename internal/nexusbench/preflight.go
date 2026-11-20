package nexusbench

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Op is what a run of the bench is about to do; it decides which checks the
// preflight makes.
type Op int

const (
	OpUp Op = iota
	OpProvision
	OpDown
	OpStatus
	// OpSeed is appended after OpStatus: no earlier value moves.
	OpSeed
	// OpCut, OpRestore and OpReset follow OpSeed, in this order.
	OpCut
	OpRestore
	OpReset
	// OpRecord follows OpReset: the recorder reads the upstream to check the state
	// of the bench (DEVDESK-580, PR2).
	OpRecord
)

// readsNode is true for the operations that return the node address: down and
// the ones that go through the loopback (seed, cut, restore, reset) do not.
func (op Op) readsNode() bool {
	return op == OpUp || op == OpProvision || op == OpStatus
}

// PreflightResult is what a successful preflight learned about the cluster.
type PreflightResult struct {
	// NamespaceExists is true when the bench namespace is there.
	NamespaceExists bool
	// NamespaceOurs is true when it carries our managed-by label.
	NamespaceOurs bool
	// NodeIP is the InternalIP of the first node that reports one; empty when
	// none does, which is not an error (the other hosts still work). It is not
	// read for OpDown.
	NodeIP string
}

const (
	// managedByLabel and managedByValue are the label 00-namespace.yaml.tmpl
	// puts on the namespace; down deletes a namespace only when it carries it.
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "devdesk-nexus-bench"

	// nexusServiceName is the Service 30-service.yaml.tmpl creates.
	nexusServiceName = "nexus"

	// requestTimeout bounds every call that reaches the API server.
	requestTimeout = "--request-timeout=10s"
)

// permissionScope says where a can-i question applies.
type permissionScope int

const (
	scopeCluster   permissionScope = iota // no flag: cluster-scoped resources
	scopeNamespace                        // -n <namespace>
	scopeAll                              // -A
)

// permission is one "kubectl auth can-i" question the preflight asks.
type permission struct {
	verb, resource string
	// subresource is asked as --subresource=<sub>: kubectl reads "pods/exec" as
	// the object "exec" of the pods, not as the exec subresource.
	subresource string
	scope       permissionScope
}

// name is the resource as a message shows it: "pods/exec".
func (perm permission) name() string {
	if perm.subresource == "" {
		return perm.resource
	}
	return perm.resource + "/" + perm.subresource
}

// seedPermissions are the questions of seed, which restore asks too and reset
// asks first. Seed reads the Deployment and the pods of the upstream, scales it
// back when it is cut, and reaches it through a port-forward, which gets the
// Service, then the pod it picks, before it opens the tunnel. EnsureUpstream
// reads the claim on every turn of its wait, so the two claim questions come
// before the first write.
var seedPermissions = []permission{
	{"get", "deployments", "", scopeNamespace},
	{"list", "pods", "", scopeNamespace},
	{"get", "pods", "", scopeNamespace},
	{"get", "services", "", scopeNamespace},
	{"get", "persistentvolumeclaims", "", scopeNamespace},
	{"list", "persistentvolumeclaims", "", scopeNamespace},
	{"create", "pods", "portforward", scopeNamespace},
	{"patch", "deployments", "scale", scopeNamespace},
}

// permissionsByOp is the list F2 of the plan: what each operation needs. An
// operation absent from the map (status) writes nothing and asks nothing.
var permissionsByOp = map[Op][]permission{
	OpUp: {
		{"create", "namespaces", "", scopeCluster},
		{"create", "persistentvolumeclaims", "", scopeNamespace},
		{"create", "services", "", scopeNamespace},
		{"create", "deployments", "", scopeNamespace},
		{"create", "pods", "exec", scopeNamespace},
		{"list", "services", "", scopeAll},
		// Server-side apply is a PATCH, also on a second run (the namespace
		// included), and Deploy reads the pods and the claims of the namespace
		// while it waits.
		{"patch", "namespaces", "", scopeCluster},
		{"patch", "deployments", "", scopeNamespace},
		{"patch", "services", "", scopeNamespace},
		{"patch", "persistentvolumeclaims", "", scopeNamespace},
		{"get", "pods", "", scopeNamespace},
		{"list", "pods", "", scopeNamespace},
		{"get", "persistentvolumeclaims", "", scopeNamespace},
		{"list", "persistentvolumeclaims", "", scopeNamespace},
		// The upstream: EnsureUpstream scales it, and the seed that follows up
		// reaches it through a port-forward, which reads the Service first. Asked
		// last, so the fourteen above stay where they were.
		{"patch", "deployments", "scale", scopeNamespace},
		{"create", "pods", "portforward", scopeNamespace},
		{"get", "services", "", scopeNamespace},
	},
	// Provision waits for the Deployment and reads the pods before it execs.
	OpProvision: {
		{"create", "pods", "exec", scopeNamespace},
		{"get", "deployments", "", scopeNamespace},
		{"list", "pods", "", scopeNamespace},
	},
	OpDown:    {{"delete", "namespaces", "", scopeCluster}},
	OpSeed:    seedPermissions,
	OpRestore: seedPermissions,
	// Cut reads the upstream Deployment, waits until no pod is left and scales
	// it to zero: no tunnel, no Service, no claim.
	OpCut: {
		{"get", "deployments", "", scopeNamespace},
		{"list", "pods", "", scopeNamespace},
		{"patch", "deployments", "scale", scopeNamespace},
	},
	// Reset recycles the claim: what restore asks, then the writes on the claim
	// (a server-side apply of an absent claim needs create). It never execs: the
	// admin password is not changed.
	OpReset: slices.Concat(seedPermissions, []permission{
		{"delete", "persistentvolumeclaims", "", scopeNamespace},
		{"create", "persistentvolumeclaims", "", scopeNamespace},
		{"patch", "persistentvolumeclaims", "", scopeNamespace},
	}),
	// Record reads the upstream Deployment and its pods to learn the state of the
	// bench (ReadUpstream) and nothing else: no write, no tunnel, no node.
	OpRecord: {
		{"get", "deployments", "", scopeNamespace},
		{"list", "pods", "", scopeNamespace},
	},
}

// RunPreflight checks, with read-only kubectl calls, that op can go ahead, and
// returns what it learned. Nothing is written to the cluster, whatever the
// outcome: a failure here means no write was ever issued. The checks run in a
// fixed order and stop at the first failure, so nothing is asked of a cluster
// the context does not reach:
//
//  1. the parameters (no call at all)
//  2. the context is in the kubeconfig
//  3. the API server answers /readyz
//  4. the can-i questions of the operation
//  5. the namespace: absent, ours, or foreign
//  6. up only: no other Service holds one of our NodePorts
//  7. up, provision and status only (Op.readsNode): the InternalIP of the node
//
// Every failure is a *BenchError.
func RunPreflight(ctx context.Context, r Runner, p Params, op Op) (PreflightResult, error) {
	var res PreflightResult
	if err := p.Validate(); err != nil {
		return res, err
	}
	pf := preflight{r: r, p: p}
	if err := pf.checkContext(ctx); err != nil {
		return res, err
	}
	if err := pf.checkReachable(ctx); err != nil {
		return res, err
	}
	for _, perm := range permissionsByOp[op] {
		if err := pf.checkPermission(ctx, perm); err != nil {
			return res, err
		}
	}
	res, err := pf.checkNamespace(ctx, op)
	if err != nil {
		return res, err
	}
	if op == OpUp {
		if err := pf.checkNodePorts(ctx); err != nil {
			return res, err
		}
	}
	// The operations that reach the registries through a port-forward on the
	// loopback have no use for the node address, and no can-i question covers
	// get nodes.
	if op.readsNode() {
		if res.NodeIP, err = pf.nodeIP(ctx); err != nil {
			return res, err
		}
	}
	return res, nil
}

type preflight struct {
	r Runner
	p Params
}

// local runs a call that does not reach the API server.
func (pf preflight) local(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	out, errOut, err := pf.r.Run(ctx, nil, args...)
	return string(out), string(errOut), err
}

// remote runs a call that reaches the API server, under a request timeout.
func (pf preflight) remote(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	return pf.local(ctx, slices.Concat(args, []string{requestTimeout})...)
}

// command is the kubectl command line a user can run to repeat a call.
func (pf preflight) command(args ...string) string {
	return "kubectl --context " + pf.p.Context + " " + strings.Join(args, " ")
}

// failure turns a failed kubectl call into a BenchError whose kind comes from
// the stderr. Neither the stderr nor the sub-process error reaches the message.
func (pf preflight) failure(what string, args []string, stderr string, err error) *BenchError {
	kind := classifyKubectl(stderr)
	reason := "kubectl failed"
	switch kind {
	case Forbidden:
		reason = "the API server refused the request (forbidden)"
	case ClusterUnreachable:
		reason = "the cluster did not answer"
	}
	return &BenchError{
		Kind:  kind,
		Cause: "could not " + what + ": " + reason,
		Check: pf.command(args...),
		Err:   err,
	}
}

func (pf preflight) checkContext(ctx context.Context) error {
	args := []string{"config", "get-contexts", "-o", "name"}
	out, errOut, err := pf.local(ctx, args...)
	if err != nil {
		return pf.failure("list the kubeconfig contexts", args, errOut, err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == pf.p.Context {
			return nil
		}
	}
	return &BenchError{
		Kind:  ContextNotFound,
		Cause: fmt.Sprintf("no kubeconfig context is named %q", pf.p.Context),
		Check: "kubectl config get-contexts -o name, then set NEXUS_BENCH_CONTEXT to one of them",
	}
}

func (pf preflight) checkReachable(ctx context.Context) error {
	args := []string{"get", "--raw", "/readyz"}
	_, errOut, err := pf.remote(ctx, args...)
	if err == nil {
		return nil
	}
	// A refusal (an expired token, a 403) is not an absent cluster: say so.
	// Anything else, classified or not, means the server did not answer.
	if classifyKubectl(errOut) == Forbidden {
		return pf.failure("reach the API server", args, errOut, err)
	}
	return &BenchError{
		Kind:  ClusterUnreachable,
		Cause: fmt.Sprintf("the API server of context %q does not answer /readyz", pf.p.Context),
		Check: pf.command(args...),
		Err:   err,
	}
}

// checkPermission asks "kubectl auth can-i". A "no" arrives on stdout with a
// non-zero exit, so the answer is read before the error.
func (pf preflight) checkPermission(ctx context.Context, perm permission) error {
	args := []string{"auth", "can-i", perm.verb, perm.resource}
	if perm.subresource != "" {
		args = append(args, "--subresource="+perm.subresource)
	}
	where := "cluster-wide"
	switch perm.scope {
	case scopeNamespace:
		args = append(args, "-n", pf.p.Namespace)
		where = fmt.Sprintf("in namespace %q", pf.p.Namespace)
	case scopeAll:
		args = append(args, "-A")
		where = "in all namespaces"
	}
	out, errOut, err := pf.remote(ctx, args...)
	answer := strings.TrimSpace(out)
	switch {
	case strings.HasPrefix(answer, "no"):
		return &BenchError{
			Kind:  Forbidden,
			Cause: fmt.Sprintf("the current user is not allowed to %s %s %s", perm.verb, perm.name(), where),
			Check: pf.command(args...),
			Err:   err,
		}
	case err == nil && strings.HasPrefix(answer, "yes"):
		return nil
	}
	return pf.failure(fmt.Sprintf("check the permission to %s %s", perm.verb, perm.name()), args, errOut, err)
}

// namespaceObject is the part of a Namespace the preflight reads.
type namespaceObject struct {
	Metadata struct {
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
}

func (pf preflight) checkNamespace(ctx context.Context, op Op) (PreflightResult, error) {
	var res PreflightResult
	ns := pf.p.Namespace
	args := []string{"get", "namespace", ns, "-o", "json"}
	out, errOut, err := pf.remote(ctx, args...)
	switch {
	case err != nil && isNotFound(errOut):
		// Absent is fine for up and down only.
		if op != OpUp && op != OpDown {
			return res, &BenchError{
				Kind:  NamespaceMissing,
				Cause: fmt.Sprintf("namespace %q does not exist", ns),
				Check: pf.command(args[:3]...) + ", then run `mise run nexus-bench:up`",
				Err:   err,
			}
		}
		return res, nil
	case err != nil:
		return res, pf.failure(fmt.Sprintf("read the namespace %q", ns), args, errOut, err)
	}

	var obj namespaceObject
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return res, &BenchError{
			Kind:  Unclassified,
			Cause: fmt.Sprintf("kubectl answered with something that is not a Namespace for %q", ns),
			Check: pf.command(args...),
			Err:   err,
		}
	}
	res.NamespaceExists = true
	res.NamespaceOurs = obj.Metadata.Labels[managedByLabel] == managedByValue
	if !res.NamespaceOurs {
		return res, &BenchError{
			Kind:  NamespaceForeign,
			Cause: fmt.Sprintf("namespace %q exists but does not carry the label %s=%s, so the bench did not create it", ns, managedByLabel, managedByValue),
			Check: pf.command("get", "namespace", ns, "--show-labels") + ", or choose another NEXUS_BENCH_NAMESPACE",
		}
	}
	return res, nil
}

// serviceList is the part of a Service list the preflight reads.
type serviceList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec struct {
			Ports []struct {
				NodePort int `json:"nodePort"`
			} `json:"ports"`
		} `json:"spec"`
	} `json:"items"`
}

// checkNodePorts reads every Service of the cluster: the list of taken
// NodePorts is not a constant (F1). Our own Service, in our namespace, may hold
// them: that is a second up.
func (pf preflight) checkNodePorts(ctx context.Context) error {
	args := []string{"get", "services", "-A", "-o", "json"}
	out, errOut, err := pf.remote(ctx, args...)
	if err != nil {
		return pf.failure("list the Services of the cluster", args, errOut, err)
	}
	var list serviceList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return &BenchError{
			Kind:  Unclassified,
			Cause: "kubectl answered with something that is not a Service list",
			Check: pf.command(args...),
			Err:   err,
		}
	}
	var taken []string
	for _, svc := range list.Items {
		if svc.Metadata.Namespace == pf.p.Namespace && svc.Metadata.Name == nexusServiceName {
			continue
		}
		for _, port := range svc.Spec.Ports {
			if port.NodePort == pf.p.HTTPNodePort || port.NodePort == pf.p.DockerNodePort {
				taken = append(taken, fmt.Sprintf("%d is held by Service %s/%s", port.NodePort, svc.Metadata.Namespace, svc.Metadata.Name))
			}
		}
	}
	if len(taken) > 0 {
		return &BenchError{
			Kind:  NodePortTaken,
			Cause: "NodePort " + strings.Join(taken, ", "),
			Check: pf.command(args...) + ", then set NEXUS_BENCH_HTTP_NODEPORT and NEXUS_BENCH_DOCKER_NODEPORT to free ports",
		}
	}
	return nil
}

// nodeList is the part of a Node list the preflight reads.
type nodeList struct {
	Items []struct {
		Status struct {
			Addresses []struct {
				Type    string `json:"type"`
				Address string `json:"address"`
			} `json:"addresses"`
		} `json:"status"`
	} `json:"items"`
}

// nodeIP returns the first InternalIP any node reports, or "" when none does.
func (pf preflight) nodeIP(ctx context.Context) (string, error) {
	args := []string{"get", "nodes", "-o", "json"}
	out, errOut, err := pf.remote(ctx, args...)
	if err != nil {
		return "", pf.failure("read the nodes", args, errOut, err)
	}
	var nodes nodeList
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		return "", &BenchError{
			Kind:  Unclassified,
			Cause: "kubectl answered with something that is not a Node list",
			Check: pf.command(args...),
			Err:   err,
		}
	}
	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == "InternalIP" {
				return addr.Address, nil
			}
		}
	}
	return "", nil
}
