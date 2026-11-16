package nexusbench

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

// This file holds the scripted kubectl the preflight tests run against, and the
// preflight tests. The fake is the contract of the kubectl calls the code under
// test may issue; the Deploy and Destroy tests reuse it and extend it.
//
// The Runner receives the arguments WITHOUT "--context <ctx>": injecting it is
// the exec runner's job, and the fake fails the test if a call carries one.
// "--request-timeout=<d>" may sit anywhere in the arguments; the fake ignores
// it, and "--all-namespaces" is read as "-A".
//
// Calls the fake answers (anything else fails the test as unscripted):
//
//	config get-contexts -o name
//	get --raw /readyz
//	auth can-i <verb> <resource> [-n <ns> | -A]
//	get namespace <ns> -o json
//	get services -A -o json
//	get nodes -o json
//
// "auth can-i" answers like kubectl: "yes" with exit 0, "no" on stdout with exit
// 1 (so the Runner returns a non-nil error that is NOT a failure of the call:
// the answer is on stdout). "get namespace" of an absent namespace fails with
// the kubectl NotFound text on stderr, which means "absent", not an error.

const (
	fakeNodeIP  = "192.168.1.31"
	fakeSecret  = "hunter2" // must never appear in an error message
	managedByKV = "app.kubernetes.io/managed-by"
	benchOwner  = "devdesk-nexus-bench"
)

var errFakeExit1 = errors.New("exit status 1")

var _ Runner = (*fakeKubectl)(nil)

type fakeReply struct {
	stdout, stderr string
	err            error
}

type fakeCall struct {
	args  []string
	stdin []byte
}

// canIQuery is one "auth can-i" call as the fake parsed it.
type canIQuery struct {
	verb, resource, namespace string
	allNamespaces             bool
}

func (q canIQuery) String() string { return q.verb + " " + q.resource }

type fakeService struct {
	namespace, name string
	nodePorts       []int // a ClusterIP Service has none
}

type fakeKubectl struct {
	t *testing.T
	p Params

	contexts string    // stdout of "config get-contexts -o name"
	readyz   fakeReply // answer of "get --raw /readyz"
	denied   map[string]bool

	nsPresent bool
	nsLabels  map[string]string
	nsReply   *fakeReply // when set, answers "get namespace" instead of the state above

	services      []fakeService
	servicesReply *fakeReply // when set, answers "get services -A -o json"
	nodeAddresses [][2]string

	calls []fakeCall
	asked []canIQuery
}

// newFakeKubectl is a healthy cluster for p: its context is known, the API
// answers, every can-i is "yes", the namespace does not exist, and the Services
// are those of the real cluster on 2026-11-16 (F1 of the plan).
func newFakeKubectl(t *testing.T, p Params) *fakeKubectl {
	t.Helper()
	return &fakeKubectl{
		t:        t,
		p:        p,
		contexts: "k0s\n" + p.Context + "\nminikube\n",
		readyz:   fakeReply{stdout: "ok"},
		denied:   map[string]bool{},
		services: []fakeService{
			{"default", "kubernetes", nil},
			{"traefik", "traefik", []int{30080}},
			{"observability", "grafana", []int{30300}},
			{"plane", "plane-web", []int{30890}},
			{"plane", "plane-api", []int{30891, 30892}},
			{"kube-system", "kube-dns", nil},
		},
		nodeAddresses: [][2]string{{"Hostname", "datomir"}, {"InternalIP", fakeNodeIP}},
	}
}

// withNamespace makes the bench namespace exist, with the given labels.
func (f *fakeKubectl) withNamespace(labels map[string]string) *fakeKubectl {
	f.nsPresent = true
	f.nsLabels = labels
	return f
}

// ours is the namespace as the bench creates it, plus the label every
// namespace gets from the API server.
func (f *fakeKubectl) ours() *fakeKubectl {
	return f.withNamespace(map[string]string{
		managedByKV:                   benchOwner,
		"app.kubernetes.io/part-of":   benchOwner,
		"kubernetes.io/metadata.name": f.p.Namespace,
	})
}

func (f *fakeKubectl) deny(verb, resource string) *fakeKubectl {
	f.denied[verb+" "+resource] = true
	return f
}

func (f *fakeKubectl) addService(namespace, name string, nodePorts ...int) *fakeKubectl {
	f.services = append(f.services, fakeService{namespace, name, nodePorts})
	return f
}

// Run implements Runner.
func (f *fakeKubectl) Run(_ context.Context, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	f.calls = append(f.calls, fakeCall{args: slices.Clone(args), stdin: slices.Clone(stdin)})
	if slices.Contains(args, "--context") {
		f.t.Errorf("kubectl call %q passes --context itself: the runner injects it", args)
	}
	if verb := verbOf(args); isWriteVerb(verb) {
		return nil, []byte("the fake refuses a write: " + verb), errFakeExit1
	}

	norm := normalizeArgs(args)
	switch joined := strings.Join(norm, " "); {
	case joined == "config get-contexts -o name":
		return []byte(f.contexts), nil, nil
	case joined == "get --raw /readyz":
		return f.readyz.reply()
	case strings.HasPrefix(joined, "auth can-i "):
		return f.answerCanI(norm[2:])
	case joined == "get namespace "+f.p.Namespace+" -o json":
		return f.answerNamespace()
	case joined == "get services -A -o json":
		return f.answerServices()
	case joined == "get nodes -o json":
		return f.answerNodes()
	default:
		f.t.Errorf("unscripted kubectl call: %q", args)
		return nil, []byte("unscripted call"), errFakeExit1
	}
}

func (r fakeReply) reply() ([]byte, []byte, error) {
	return []byte(r.stdout), []byte(r.stderr), r.err
}

func (f *fakeKubectl) answerCanI(args []string) ([]byte, []byte, error) {
	var q canIQuery
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n":
			if i+1 < len(args) {
				i++
				q.namespace = args[i]
			}
		case "-A":
			q.allNamespaces = true
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 {
		f.t.Errorf("auth can-i wants a verb and a resource, got %q", args)
		return nil, []byte("bad can-i"), errFakeExit1
	}
	q.verb, q.resource = positional[0], positional[1]
	if q.namespace != "" && q.namespace != f.p.Namespace {
		f.t.Errorf("auth can-i %q asks about namespace %q, the run is about %q", args, q.namespace, f.p.Namespace)
	}
	f.asked = append(f.asked, q)
	if f.denied[q.String()] {
		return []byte("no\n"), nil, errFakeExit1
	}
	return []byte("yes\n"), nil, nil
}

func (f *fakeKubectl) answerNamespace() ([]byte, []byte, error) {
	if f.nsReply != nil {
		return f.nsReply.reply()
	}
	if !f.nsPresent {
		return nil, []byte("Error from server (NotFound): namespaces \"" + f.p.Namespace + "\" not found\n"), errFakeExit1
	}
	meta := map[string]any{"name": f.p.Namespace, "uid": "6b1f0f8e-0000-4000-8000-000000000001"}
	if f.nsLabels != nil {
		meta["labels"] = f.nsLabels
	}
	return f.mustJSON(map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   meta,
		"status":     map[string]any{"phase": "Active"},
	}), nil, nil
}

func (f *fakeKubectl) answerServices() ([]byte, []byte, error) {
	if f.servicesReply != nil {
		return f.servicesReply.reply()
	}
	items := make([]any, 0, len(f.services))
	for _, s := range f.services {
		ports := make([]any, 0, len(s.nodePorts))
		for i, np := range s.nodePorts {
			ports = append(ports, map[string]any{"name": "p" + string(rune('0'+i)), "port": 80 + i, "targetPort": 8080 + i, "protocol": "TCP", "nodePort": np})
		}
		if len(ports) == 0 {
			ports = append(ports, map[string]any{"port": 443, "protocol": "TCP", "targetPort": 6443})
		}
		kind := "ClusterIP"
		if len(s.nodePorts) > 0 {
			kind = "NodePort"
		}
		items = append(items, map[string]any{
			"metadata": map[string]any{"name": s.name, "namespace": s.namespace},
			"spec":     map[string]any{"type": kind, "ports": ports},
		})
	}
	return f.mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": items}), nil, nil
}

func (f *fakeKubectl) answerNodes() ([]byte, []byte, error) {
	addresses := make([]any, 0, len(f.nodeAddresses))
	for _, a := range f.nodeAddresses {
		addresses = append(addresses, map[string]any{"type": a[0], "address": a[1]})
	}
	return f.mustJSON(map[string]any{
		"apiVersion": "v1",
		"kind":       "List",
		"items": []any{map[string]any{
			"metadata": map[string]any{"name": "datomir"},
			"status":   map[string]any{"addresses": addresses},
		}},
	}), nil, nil
}

func (f *fakeKubectl) mustJSON(v any) []byte {
	f.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		f.t.Fatalf("encoding a fake answer: %v", err)
	}
	return raw
}

// normalizeArgs drops the request timeout and spells --all-namespaces as -A.
func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--request-timeout"):
		case a == "--all-namespaces":
			out = append(out, "-A")
		default:
			out = append(out, a)
		}
	}
	return out
}

// flagsWithValue are the kubectl flags that take their value as the next argument.
var flagsWithValue = map[string]bool{
	"-n": true, "--namespace": true, "-o": true, "--output": true,
	"--context": true, "-f": true, "-l": true, "--selector": true, "-c": true,
}

// verbOf returns the kubectl sub-command of args: the first positional argument.
// "auth can-i create pods" is therefore "auth", not "create".
func verbOf(args []string) string {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case flagsWithValue[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a
		}
	}
	return ""
}

var writeVerbs = []string{
	"apply", "create", "delete", "patch", "exec", "label", "annotate", "replace",
	"edit", "scale", "set", "rollout", "cp", "run", "expose", "taint", "drain",
	"cordon", "uncordon", "autoscale", "debug", "attach",
}

func isWriteVerb(verb string) bool { return slices.Contains(writeVerbs, verb) }

// recordedWrites lists the calls the fake saw whose sub-command writes.
func (f *fakeKubectl) recordedWrites() []string {
	var out []string
	for _, c := range f.calls {
		if isWriteVerb(verbOf(c.args)) {
			out = append(out, strings.Join(c.args, " "))
		}
	}
	return out
}

func (f *fakeKubectl) callLines() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, strings.Join(normalizeArgs(c.args), " "))
	}
	return out
}

// requireReadOnly fails when the preflight wrote anything, or sent a manifest on stdin.
func requireReadOnly(t *testing.T, f *fakeKubectl) {
	t.Helper()
	if writes := f.recordedWrites(); len(writes) > 0 {
		t.Errorf("the preflight issued write calls: %q", writes)
	}
	for _, c := range f.calls {
		if len(c.stdin) > 0 {
			t.Errorf("the preflight sent stdin to %q", c.args)
		}
	}
}

// requireCleanError checks what every failure of the bench promises: a
// *BenchError with a cause and a check, no password and no raw sub-process text.
func requireCleanError(t *testing.T, err error) *BenchError {
	t.Helper()
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Cause == "" {
		t.Error("the error has no Cause")
	}
	if !strings.Contains(be.Check, "kubectl") && !strings.Contains(be.Check, "mise") {
		t.Errorf("Check %q names neither a kubectl command nor a mise task", be.Check)
	}
	msg := strings.ToLower(err.Error())
	for _, banned := range []string{"password", fakeSecret} {
		if strings.Contains(msg, banned) {
			t.Errorf("message %q contains %q", err.Error(), banned)
		}
	}
	return be
}

// testOpName names the operations in sub-test titles and messages.
var testOpName = map[Op]string{OpUp: "up", OpProvision: "provision", OpDown: "down", OpStatus: "status"}

// requiredCheck is a can-i the preflight must ask for an operation.
type requiredCheck struct {
	verb, resource string
	scope          string // "namespace" (-n <ns>), "cluster" (no -A) or "all" (-A)
}

// requiredChecks is the list of F2 the plan fixes. The fake answers "yes" to any
// other can-i, so the code may ask for more; it may not ask for less.
var requiredChecks = map[Op][]requiredCheck{
	OpUp: {
		{"create", "namespaces", "cluster"},
		{"create", "persistentvolumeclaims", "namespace"},
		{"create", "services", "namespace"},
		{"create", "deployments", "namespace"},
		{"create", "pods/exec", "namespace"},
		{"list", "services", "all"},
		// The namespace is labeled by server-side apply, also on a second run.
		{"patch", "namespaces", "cluster"},
		// Server-side apply is a PATCH, also on a second run; Deploy reads the
		// pods and the claims of the namespace while it waits.
		{"patch", "deployments", "namespace"},
		{"patch", "services", "namespace"},
		{"patch", "persistentvolumeclaims", "namespace"},
		{"get", "pods", "namespace"},
		{"list", "pods", "namespace"},
		{"get", "persistentvolumeclaims", "namespace"},
		{"list", "persistentvolumeclaims", "namespace"},
	},
	OpProvision: {
		{"create", "pods/exec", "namespace"},
		{"get", "deployments", "namespace"},
		{"list", "pods", "namespace"},
	},
	OpDown: {{"delete", "namespaces", "cluster"}},
}

func (c requiredCheck) matches(q canIQuery, p Params) bool {
	if q.verb != c.verb || q.resource != c.resource {
		return false
	}
	switch c.scope {
	case "namespace":
		return q.namespace == p.Namespace && !q.allNamespaces
	case "all":
		return q.allNamespaces
	default:
		return !q.allNamespaces
	}
}

func TestWriteVerbDetector(t *testing.T) {
	tests := []struct {
		args  []string
		write bool
	}{
		{[]string{"apply", "--server-side", "-f", "-"}, true},
		{[]string{"create", "namespace", "x"}, true},
		{[]string{"delete", "namespace", "x", "--wait=false"}, true},
		{[]string{"-n", "ns", "exec", "deploy/nexus", "--", "cat", "x"}, true},
		{[]string{"patch", "deployment", "nexus"}, true},
		{[]string{"label", "namespace", "x", "a=b"}, true},
		{[]string{"annotate", "namespace", "x", "a=b"}, true},
		{[]string{"auth", "can-i", "create", "pods"}, false},
		{[]string{"auth", "can-i", "delete", "namespaces"}, false},
		{[]string{"--request-timeout=10s", "get", "--raw", "/readyz"}, false},
		{[]string{"get", "services", "-A", "-o", "json"}, false},
		{[]string{"config", "get-contexts", "-o", "name"}, false},
	}
	for _, tt := range tests {
		if got := isWriteVerb(verbOf(tt.args)); got != tt.write {
			t.Errorf("write(%q) = %v, want %v", tt.args, got, tt.write)
		}
	}
}

func TestRunPreflightSucceeds(t *testing.T) {
	custom := func() Params {
		p := Defaults()
		p.Namespace = "nexus-bench-2"
		p.UpstreamURL = upstreamFor(p.Namespace)
		p.HTTPNodePort, p.DockerNodePort = 30490, 30491
		return p
	}
	tests := []struct {
		name       string
		op         Op
		params     func() Params // nil means Defaults
		setup      func(f *fakeKubectl)
		want       PreflightResult
		skipNodeIP bool // the plan does not say whether down reads the node
	}{
		{
			name: "up on a cluster without the namespace",
			op:   OpUp, setup: func(f *fakeKubectl) {},
			want: PreflightResult{NamespaceExists: false, NamespaceOurs: false, NodeIP: fakeNodeIP},
		},
		{
			name: "up on our own namespace",
			op:   OpUp, setup: func(f *fakeKubectl) { f.ours() },
			want: PreflightResult{NamespaceExists: true, NamespaceOurs: true, NodeIP: fakeNodeIP},
		},
		{
			name: "up again: our own Service already holds both NodePorts",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.ours().addService("nexus-bench", "nexus", 30481, 30482)
			},
			want: PreflightResult{NamespaceExists: true, NamespaceOurs: true, NodeIP: fakeNodeIP},
		},
		{
			name:   "up with other ports and namespace, own Service on them",
			op:     OpUp,
			params: custom,
			setup: func(f *fakeKubectl) {
				f.ours().addService("nexus-bench-2", "nexus", 30490, 30491)
				// The default bench exists next door and holds the default ports.
				f.addService("nexus-bench", "nexus", 30481, 30482)
			},
			want: PreflightResult{NamespaceExists: true, NamespaceOurs: true, NodeIP: fakeNodeIP},
		},
		{
			name: "up reads the node address of the InternalIP entry only",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.nodeAddresses = [][2]string{{"ExternalIP", "203.0.113.9"}, {"Hostname", "datomir"}, {"InternalIP", fakeNodeIP}}
			},
			want: PreflightResult{NodeIP: fakeNodeIP},
		},
		{
			name: "up on a node that reports no InternalIP leaves the address empty",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.nodeAddresses = [][2]string{{"Hostname", "datomir"}}
			},
			want: PreflightResult{NodeIP: ""},
		},
		{
			name: "up on a Windows kubectl whose context list ends lines with CRLF",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.contexts = "k0s\r\ndatomir\r\n"
			},
			want: PreflightResult{NodeIP: fakeNodeIP},
		},
		{
			name: "provision on our namespace",
			op:   OpProvision, setup: func(f *fakeKubectl) { f.ours() },
			want: PreflightResult{NamespaceExists: true, NamespaceOurs: true, NodeIP: fakeNodeIP},
		},
		{
			name: "status on our namespace",
			op:   OpStatus, setup: func(f *fakeKubectl) { f.ours() },
			want: PreflightResult{NamespaceExists: true, NamespaceOurs: true, NodeIP: fakeNodeIP},
		},
		{
			name: "down on our namespace",
			op:   OpDown, setup: func(f *fakeKubectl) { f.ours() },
			want:       PreflightResult{NamespaceExists: true, NamespaceOurs: true},
			skipNodeIP: true,
		},
		{
			name: "down without the namespace: nothing to delete is not an error",
			op:   OpDown, setup: func(f *fakeKubectl) {},
			want:       PreflightResult{NamespaceExists: false, NamespaceOurs: false},
			skipNodeIP: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			if tt.params != nil {
				p = tt.params()
			}
			f := newFakeKubectl(t, p)
			tt.setup(f)

			got, err := RunPreflight(context.Background(), f, p, tt.op)
			if err != nil {
				t.Fatalf("RunPreflight: %v", err)
			}
			if tt.skipNodeIP {
				got.NodeIP = ""
			}
			if got != tt.want {
				t.Errorf("result = %+v, want %+v", got, tt.want)
			}
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightAsksTheRequiredChecks(t *testing.T) {
	for op, checks := range requiredChecks {
		t.Run(testOpName[op], func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p)
			if op != OpUp {
				f.ours()
			}
			if _, err := RunPreflight(context.Background(), f, p, op); err != nil {
				t.Fatalf("RunPreflight: %v", err)
			}
			for _, c := range checks {
				if !slices.ContainsFunc(f.asked, func(q canIQuery) bool { return c.matches(q, p) }) {
					t.Errorf("never asked: auth can-i %s %s (scope %s); asked %v", c.verb, c.resource, c.scope, f.asked)
				}
			}
			// No manifest creates a Secret, so no one asks to.
			for _, q := range f.asked {
				if q.resource == "secrets" {
					t.Errorf("asked about secrets (%v), no manifest creates one", q)
				}
			}
			requireReadOnly(t, f)
		})
	}
}

// failCase is one way the preflight refuses to go on.
type failCase struct {
	name      string
	op        Op
	params    func() Params // nil means Defaults
	setup     func(f *fakeKubectl)
	kind      ErrKind
	mention   []string // each must appear in the error message
	unplanned bool     // the kind is none of the named ones
	onlyCalls []string // when set, the exact calls the fake may have seen
}

func foreignNamespace(labels map[string]string) func(f *fakeKubectl) {
	return func(f *fakeKubectl) { f.withNamespace(labels) }
}

func failCases() []failCase {
	custom := func() Params {
		p := Defaults()
		p.Namespace = "nexus-bench-2"
		p.UpstreamURL = upstreamFor(p.Namespace)
		p.HTTPNodePort, p.DockerNodePort = 30490, 30491
		return p
	}
	unlabeled := map[string]string{"kubernetes.io/metadata.name": "nexus-bench"}
	cases := []failCase{
		{
			name: "context missing from the list",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.contexts = "k0s\nminikube\n"
			},
			kind:      ContextNotFound,
			mention:   []string{"datomir"},
			onlyCalls: []string{"config get-contexts -o name"},
		},
		{
			name: "a context whose name only starts like ours is not ours",
			op:   OpDown,
			setup: func(f *fakeKubectl) {
				f.contexts = "datomir-old\nold-datomir\n"
			},
			kind:      ContextNotFound,
			mention:   []string{"datomir"},
			onlyCalls: []string{"config get-contexts -o name"},
		},
		{
			name: "API server refuses the connection",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.readyz = fakeReply{
					stderr: "Unable to connect to the server: dial tcp 192.168.1.31:6443: connect: connection refused\n",
					err:    errFakeExit1,
				}
			},
			kind:      ClusterUnreachable,
			onlyCalls: []string{"config get-contexts -o name", "get --raw /readyz"},
		},
		{
			name: "API server times out",
			op:   OpStatus,
			setup: func(f *fakeKubectl) {
				f.readyz = fakeReply{
					stderr: "Unable to connect to the server: net/http: TLS handshake timeout\n",
					err:    errFakeExit1,
				}
			},
			kind:      ClusterUnreachable,
			onlyCalls: []string{"config get-contexts -o name", "get --raw /readyz"},
		},
		{
			name: "readyz is forbidden: the token is refused, not the cluster missing",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.readyz = fakeReply{
					stderr: "Error from server (Forbidden): forbidden: User \"system:anonymous\" cannot get path \"/readyz\"\n",
					err:    errFakeExit1,
				}
			},
			kind:      Forbidden,
			onlyCalls: []string{"config get-contexts -o name", "get --raw /readyz"},
		},
		{
			name: "readyz fails in a way nobody planned: still reported as an unreachable cluster",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.readyz = fakeReply{stderr: "something odd happened\n", err: errFakeExit1}
			},
			kind:      ClusterUnreachable,
			onlyCalls: []string{"config get-contexts -o name", "get --raw /readyz"},
		},
		{
			name: "unreachable cluster whose stderr carries a secret",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.readyz = fakeReply{
					stderr: "Unable to connect to the server: proxyconnect tcp: dial tcp user:password=" + fakeSecret + "@proxy:3128: connection refused\n",
					err:    errFakeExit1,
				}
			},
			kind: ClusterUnreachable,
		},
		{
			name: "namespace of ours is not ours: no labels at all but the API server's",
			op:   OpUp, setup: foreignNamespace(unlabeled),
			kind: NamespaceForeign, mention: []string{"nexus-bench"},
		},
		{
			name: "down refuses a namespace without our label",
			op:   OpDown, setup: foreignNamespace(unlabeled),
			kind: NamespaceForeign, mention: []string{"nexus-bench"},
		},
		{
			name: "up refuses a namespace managed by someone else",
			op:   OpUp, setup: foreignNamespace(map[string]string{managedByKV: "Helm"}),
			kind: NamespaceForeign, mention: []string{"nexus-bench"},
		},
		{
			name: "down refuses a namespace managed by someone else",
			op:   OpDown, setup: foreignNamespace(map[string]string{managedByKV: "Helm"}),
			kind: NamespaceForeign, mention: []string{"nexus-bench"},
		},
		{
			name: "foreign namespace with no label map at all",
			op:   OpUp, setup: foreignNamespace(nil),
			kind: NamespaceForeign, mention: []string{"nexus-bench"},
		},
		{
			name: "provision without a namespace",
			op:   OpProvision, setup: func(f *fakeKubectl) {},
			kind: NamespaceMissing, mention: []string{"nexus-bench"},
		},
		{
			name: "status without a namespace",
			op:   OpStatus, setup: func(f *fakeKubectl) {},
			kind: NamespaceMissing, mention: []string{"nexus-bench"},
		},
		{
			name:  "Service plane/x holds the REST NodePort",
			op:    OpUp,
			setup: func(f *fakeKubectl) { f.addService("plane", "x", 30481) },
			kind:  NodePortTaken, mention: []string{"plane/x", "30481"},
		},
		{
			name:  "Service plane/x holds the Docker NodePort",
			op:    OpUp,
			setup: func(f *fakeKubectl) { f.addService("plane", "x", 30482) },
			kind:  NodePortTaken, mention: []string{"plane/x", "30482"},
		},
		{
			name:  "the taken NodePort is the second port of a Service",
			op:    OpUp,
			setup: func(f *fakeKubectl) { f.addService("plane", "x", 30900, 30481) },
			kind:  NodePortTaken, mention: []string{"plane/x", "30481"},
		},
		{
			name:  "a namespace that only starts like ours is foreign",
			op:    OpUp,
			setup: func(f *fakeKubectl) { f.ours().addService("nexus-bench-2", "x", 30481) },
			kind:  NodePortTaken, mention: []string{"nexus-bench-2/x", "30481"},
		},
		{
			name:   "NodePorts of a non-default run",
			op:     OpUp,
			params: custom,
			setup:  func(f *fakeKubectl) { f.addService("plane", "x", 30491) },
			kind:   NodePortTaken, mention: []string{"plane/x", "30491"},
		},
		{
			name: "our namespace exists but another Service holds a port",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.ours().addService("nexus-bench", "nexus", 30481, 30482).addService("traefik", "rogue", 30482)
			},
			kind: NodePortTaken, mention: []string{"traefik/rogue", "30482"},
		},
		{
			name: "get namespace is forbidden although can-i said yes",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.nsReply = &fakeReply{
					stderr: "Error from server (Forbidden): namespaces \"nexus-bench\" is forbidden: User \"anthoni\" cannot get resource \"namespaces\" in API group \"\" in the namespace \"nexus-bench\"\n",
					err:    errFakeExit1,
				}
			},
			kind: Forbidden,
		},
		{
			name: "get services is forbidden although can-i said yes",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.servicesReply = &fakeReply{
					stderr: "Error from server (Forbidden): services is forbidden: User \"anthoni\" cannot list resource \"services\" in API group \"\" at the cluster scope\n",
					err:    errFakeExit1,
				}
			},
			kind: Forbidden,
		},
		{
			name: "get namespace fails in a way nobody planned, and the stderr holds a secret",
			op:   OpUp,
			setup: func(f *fakeKubectl) {
				f.nsReply = &fakeReply{
					stderr: "Error from server (InternalError): Internal error occurred: token=" + fakeSecret + " rejected\n",
					err:    errFakeExit1,
				}
			},
			unplanned: true,
		},
	}
	// Every verb and resource of the required list, denied one at a time.
	for _, op := range []Op{OpUp, OpProvision, OpDown} {
		for _, c := range requiredChecks[op] {
			cases = append(cases, failCase{
				name: "can-i says no to " + c.verb + " " + c.resource + " (" + testOpName[op] + ")",
				op:   op,
				setup: func(f *fakeKubectl) {
					// The namespace is there for provision and down, so that the
					// order of the checks does not matter to the outcome.
					if op != OpUp {
						f.ours()
					}
					f.deny(c.verb, c.resource)
				},
				kind:    Forbidden,
				mention: []string{c.verb, c.resource},
			})
		}
	}
	return cases
}

func TestRunPreflightFails(t *testing.T) {
	for _, tt := range failCases() {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			if tt.params != nil {
				p = tt.params()
			}
			f := newFakeKubectl(t, p)
			tt.setup(f)

			_, err := RunPreflight(context.Background(), f, p, tt.op)
			if err == nil {
				t.Fatal("RunPreflight succeeded, want a refusal")
			}
			be := requireCleanError(t, err)

			if tt.unplanned {
				for _, k := range namedKinds {
					if be.Kind == k.kind {
						t.Errorf("an unplanned failure was classified as %s", k.name)
					}
				}
			} else if be.Kind != tt.kind {
				t.Errorf("Kind = %d, want %d; message: %v", be.Kind, tt.kind, err)
			}
			for _, want := range tt.mention {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message %q does not mention %q", err.Error(), want)
				}
			}
			if tt.onlyCalls != nil {
				if got := f.callLines(); !slices.Equal(got, tt.onlyCalls) {
					t.Errorf("calls = %q, want %q (nothing is asked of a cluster the context does not reach)", got, tt.onlyCalls)
				}
			}
			requireReadOnly(t, f)
		})
	}
}

// Parameters are validated before the cluster is contacted.
func TestRunPreflightRefusesInvalidParamsBeforeAnyCall(t *testing.T) {
	p := Defaults()
	p.Namespace = "default"
	f := newFakeKubectl(t, p)

	if _, err := RunPreflight(context.Background(), f, p, OpUp); err == nil {
		t.Fatal("RunPreflight accepted the namespace default")
	}
	if len(f.calls) != 0 {
		t.Errorf("kubectl was called with invalid parameters: %q", f.callLines())
	}
}
