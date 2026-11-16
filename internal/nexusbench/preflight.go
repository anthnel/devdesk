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
)

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
	scope          permissionScope
}

// permissionsByOp is the list F2 of the plan: what each operation needs. An
// operation absent from the map (status) writes nothing and asks nothing.
var permissionsByOp = map[Op][]permission{
	OpUp: {
		{"create", "namespaces", scopeCluster},
		{"create", "persistentvolumeclaims", scopeNamespace},
		{"create", "services", scopeNamespace},
		{"create", "deployments", scopeNamespace},
		{"create", "pods/exec", scopeNamespace},
		{"list", "services", scopeAll},
		// Server-side apply is a PATCH, also on a second run (the namespace
		// included), and Deploy reads the pods and the claims of the namespace
		// while it waits.
		{"patch", "namespaces", scopeCluster},
		{"patch", "deployments", scopeNamespace},
		{"patch", "services", scopeNamespace},
		{"patch", "persistentvolumeclaims", scopeNamespace},
		{"get", "pods", scopeNamespace},
		{"list", "pods", scopeNamespace},
		{"get", "persistentvolumeclaims", scopeNamespace},
		{"list", "persistentvolumeclaims", scopeNamespace},
	},
	// Provision waits for the Deployment and reads the pods before it execs.
	OpProvision: {
		{"create", "pods/exec", scopeNamespace},
		{"get", "deployments", scopeNamespace},
		{"list", "pods", scopeNamespace},
	},
	OpDown: {{"delete", "namespaces", scopeCluster}},
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
//  7. all but down: the InternalIP of the node
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
	if op != OpDown {
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
			Cause: fmt.Sprintf("the current user is not allowed to %s %s %s", perm.verb, perm.resource, where),
			Check: pf.command(args...),
			Err:   err,
		}
	case err == nil && strings.HasPrefix(answer, "yes"):
		return nil
	}
	return pf.failure(fmt.Sprintf("check the permission to %s %s", perm.verb, perm.resource), args, errOut, err)
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
		// Absent: only provision and status need it.
		if op == OpProvision || op == OpStatus {
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
