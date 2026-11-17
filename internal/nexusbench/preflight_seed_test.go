package nexusbench

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The preflight of the seed and the three permissions `up` gains (DEVDESK-505,
// NX-02a, step 5). Against the scripted kubectl of preflight_test.go, which
// records every "auth can-i" question in order.
//
// Choices of the tests, to settle with the code:
//   - OpSeed is appended after OpStatus; no earlier value moves.
//   - A subresource is asked as kubectl wants it: `auth can-i create pods
//     --subresource=portforward -n <ns>`, never `create pods/portforward`, which
//     kubectl reads as the pod named "portforward". The fake fails the test on a
//     resource that holds a "/". The tests below write a question as
//     "<verb> <resource>[ --subresource=<sub>] -n <ns>" (see asked), and the
//     failure messages as "pods/portforward" or "pods --subresource=portforward"
//     alike: they only require each word (the verb, "pods", "portforward").
//     The Check of a refusal repeats the question: it contains
//     "auth can-i <verb> <resource>" and "--subresource=<sub>".
//   - The questions of seed, in this order, all in the bench namespace:
//     get deployments, list pods, get pods, get services, get
//     persistentvolumeclaims, list persistentvolumeclaims, create pods
//     --subresource=portforward, patch deployments --subresource=scale.
//     The port-forward to svc/bench-upstream reads the Service, then the pod,
//     before it POSTs to pods/portforward (get services, get pods, create
//     pods/portforward); EnsureUpstream observes the upstream's Deployment,
//     pods and claim on every turn of its wait (get deployments, list pods, get
//     and list persistentvolumeclaims) and scales it (patch deployments/scale).
//     Without the two claim questions a Forbidden would arrive after the scale
//     write. The order among them is the tests' choice.
//   - up keeps its fourteen questions where they are (they already hold get pods
//     and the claims) and asks the three new ones after them, in this order:
//     patch deployments --subresource=scale (EnsureUpstream), create pods
//     --subresource=portforward and get services (the port-forward the seed
//     opens). Its create pods/exec, inherited from NX-01, is asked the same way:
//     create pods --subresource=exec.
//   - seed refuses an absent or a foreign namespace, like provision and status,
//     does not read the Services of the cluster (that is up's NodePort check),
//     and does not read the nodes either: the seed reaches the registry through
//     a port-forward on the loopback and no question covers get nodes, so its
//     PreflightResult.NodeIP is empty. up and provision keep reading it.
//   - A refused question ends the preflight there: the later ones are not asked.
//   - Nothing is ever written.

// asked renders the can-i questions the fake saw: "verb resource", plus
// "--subresource=<sub>" when there is one, plus "-n <ns>" or "-A" when the
// question was scoped.
func asked(f *fakeKubectl) []string {
	out := make([]string, 0, len(f.asked))
	for _, q := range f.asked {
		s := q.verb + " " + q.resource
		if q.subresource != "" {
			s += " --subresource=" + q.subresource
		}
		switch {
		case q.allNamespaces:
			s += " -A"
		case q.namespace != "":
			s += " -n " + q.namespace
		}
		out = append(out, s)
	}
	return out
}

func inNamespace(p Params, questions ...string) []string {
	out := make([]string, 0, len(questions))
	for _, q := range questions {
		out = append(out, q+" -n "+p.Namespace)
	}
	return out
}

// seedQuestions are the questions of OpSeed, in the order the tests expect.
var seedQuestions = []string{
	"get deployments", "list pods", "get pods", "get services",
	"get persistentvolumeclaims", "list persistentvolumeclaims",
	"create pods --subresource=portforward", "patch deployments --subresource=scale",
}

// seedDenials are the same questions as deny takes them, "pods/portforward".
var seedDenials = []struct{ verb, resource string }{
	{"get", "deployments"},
	{"list", "pods"},
	{"get", "pods"},
	{"get", "services"},
	{"get", "persistentvolumeclaims"},
	{"list", "persistentvolumeclaims"},
	{"create", "pods/portforward"},
	{"patch", "deployments/scale"},
}

// requireRepeatsTheQuestion checks that a Check names the refused question the
// way kubectl takes it: the resource, and the subresource as a flag.
func requireRepeatsTheQuestion(t *testing.T, check, verb, resource string) {
	t.Helper()
	res, sub, hasSub := strings.Cut(resource, "/")
	if want := "auth can-i " + verb + " " + res; !strings.Contains(check, want) {
		t.Errorf("Check %q does not repeat the refused question %q", check, want)
	}
	if hasSub && !strings.Contains(check, "--subresource="+sub) {
		t.Errorf("Check %q does not carry --subresource=%s", check, sub)
	}
	if strings.Contains(check, res+"/"+sub) {
		t.Errorf("Check %q writes %s/%s, which kubectl reads as an object name", check, res, sub)
	}
}

func TestRunPreflightOpSeedIsAppended(t *testing.T) {
	want := map[string]Op{"OpUp": 0, "OpProvision": 1, "OpDown": 2, "OpStatus": 3, "OpSeed": 4}
	got := map[string]Op{"OpUp": OpUp, "OpProvision": OpProvision, "OpDown": OpDown, "OpStatus": OpStatus, "OpSeed": OpSeed}
	for name, v := range want {
		if got[name] != v {
			t.Errorf("%s = %d, want %d: operations are appended, never renumbered", name, got[name], v)
		}
	}
}

func TestRunPreflightSeedAsksItsQuestionsInOrder(t *testing.T) {
	p := Defaults()
	f := newFakeKubectl(t, p).ours()
	res, err := RunPreflight(context.Background(), f, p, OpSeed)
	if err != nil {
		t.Fatalf("RunPreflight(OpSeed): %v", err)
	}
	want := inNamespace(p, seedQuestions...)
	if got := asked(f); !slices.Equal(got, want) {
		t.Errorf("questions =\n  %q\nwant\n  %q", got, want)
	}
	if want := (PreflightResult{NamespaceExists: true, NamespaceOurs: true}); res != want {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	for _, line := range f.callLines() {
		if strings.HasPrefix(line, "get services") {
			t.Errorf("seed read the Services of the cluster (%q): the NodePort check is up's", line)
		}
	}
	requireReadOnly(t, f)
}

func TestRunPreflightSeedDoesNotReadTheNodes(t *testing.T) {
	// No can-i covers get nodes, and the seed has no use for the address: it
	// reaches the registries through a port-forward on the loopback.
	p := Defaults()
	f := newFakeKubectl(t, p).ours()
	res, err := RunPreflight(context.Background(), f, p, OpSeed)
	if err != nil {
		t.Fatalf("RunPreflight(OpSeed): %v", err)
	}
	for _, line := range f.callLines() {
		if strings.Contains(line, "nodes") {
			t.Errorf("seed issued %q: it must not read the nodes", line)
		}
	}
	if res.NodeIP != "" {
		t.Errorf("NodeIP = %q, want it empty for seed", res.NodeIP)
	}
	// up and provision are not affected: they still read it.
	for _, op := range []Op{OpUp, OpProvision} {
		f := newFakeKubectl(t, p).ours()
		res, err := RunPreflight(context.Background(), f, p, op)
		if err != nil {
			t.Fatalf("RunPreflight(%s): %v", testOpName[op], err)
		}
		if res.NodeIP != fakeNodeIP || !slices.Contains(f.callLines(), "get nodes -o json") {
			t.Errorf("%s: NodeIP = %q and calls %q, want it to read the nodes", testOpName[op], res.NodeIP, f.callLines())
		}
	}
}

func TestRunPreflightSeedHonorsTheNamespaceParameter(t *testing.T) {
	p := Defaults()
	p.Namespace = "nexus-bench-2"
	p.UpstreamURL = upstreamFor(p.Namespace)
	f := newFakeKubectl(t, p).ours()
	if _, err := RunPreflight(context.Background(), f, p, OpSeed); err != nil {
		t.Fatalf("RunPreflight(OpSeed): %v", err)
	}
	if got := asked(f); len(got) != len(seedQuestions) {
		t.Errorf("seed asked %d questions, want %d: %q", len(got), len(seedQuestions), got)
	}
	for _, q := range asked(f) {
		if !strings.HasSuffix(q, " -n nexus-bench-2") {
			t.Errorf("question %q is not scoped to the namespace of the run", q)
		}
	}
	requireReadOnly(t, f)
}

func TestRunPreflightUpAsksTheUpstreamQuestionsLast(t *testing.T) {
	p := Defaults()
	f := newFakeKubectl(t, p)
	if _, err := RunPreflight(context.Background(), f, p, OpUp); err != nil {
		t.Fatalf("RunPreflight(OpUp): %v", err)
	}
	got := asked(f)
	if len(got) != len(requiredChecks[OpUp])+3 {
		t.Fatalf("up asked %d questions, want the %d it asked before plus 3:\n%q", len(got), len(requiredChecks[OpUp]), got)
	}
	wantTail := inNamespace(p, "patch deployments --subresource=scale", "create pods --subresource=portforward", "get services")
	if tail := got[len(got)-3:]; !slices.Equal(tail, wantTail) {
		t.Errorf("the last three questions of up are %q, want %q", tail, wantTail)
	}
	// What up asked before is still asked, in the same places.
	for _, c := range requiredChecks[OpUp] {
		if !slices.ContainsFunc(f.asked[:len(f.asked)-3], func(q canIQuery) bool { return c.matches(q, p) }) {
			t.Errorf("up no longer asks auth can-i %s %s (%s) before the new questions", c.verb, c.resource, c.scope)
		}
	}
	requireReadOnly(t, f)
}

func TestRunPreflightSeedStopsAtTheFirstRefusedQuestion(t *testing.T) {
	p := Defaults()
	all := inNamespace(p, seedQuestions...)
	for i, d := range seedDenials {
		t.Run(d.verb+" "+d.resource, func(t *testing.T) {
			f := newFakeKubectl(t, p).ours().deny(d.verb, d.resource)
			_, err := RunPreflight(context.Background(), f, p, OpSeed)
			be := requireDeployKind(t, err, Forbidden)
			requireMentions(t, err, append([]string{d.verb}, strings.Split(d.resource, "/")...), nil)
			requireRepeatsTheQuestion(t, be.Check, d.verb, d.resource)
			if got, want := asked(f), all[:i+1]; !slices.Equal(got, want) {
				t.Errorf("questions = %q, want them to stop at the refused one: %q", got, want)
			}
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightUpRefusesWithoutTheUpstreamPermissions(t *testing.T) {
	for _, d := range []struct{ verb, resource string }{
		{"patch", "deployments/scale"},
		{"create", "pods/portforward"},
		{"get", "services"},
	} {
		t.Run(d.verb+" "+d.resource, func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).deny(d.verb, d.resource)
			_, err := RunPreflight(context.Background(), f, p, OpUp)
			be := requireDeployKind(t, err, Forbidden)
			requireMentions(t, err, append([]string{d.verb}, strings.Split(d.resource, "/")...), nil)
			requireRepeatsTheQuestion(t, be.Check, d.verb, d.resource)
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightAsksEverySubresourceAsAFlag(t *testing.T) {
	// up asks create pods/exec (NX-01), patch deployments/scale and create
	// pods/portforward; provision asks create pods/exec; seed asks the last two.
	want := map[Op][]string{
		OpUp:        {"create pods --subresource=exec", "patch deployments --subresource=scale", "create pods --subresource=portforward"},
		OpProvision: {"create pods --subresource=exec"},
		OpSeed:      {"create pods --subresource=portforward", "patch deployments --subresource=scale"},
	}
	for op, questions := range want {
		t.Run(testOpName[op], func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).ours()
			if _, err := RunPreflight(context.Background(), f, p, op); err != nil {
				t.Fatalf("RunPreflight: %v", err)
			}
			got := asked(f)
			for _, q := range questions {
				if !slices.Contains(got, q+" -n "+p.Namespace) {
					t.Errorf("never asked %q in the namespace; asked %q", q, got)
				}
			}
			for _, line := range f.callLines() {
				if strings.Contains(line, "auth can-i") && strings.Contains(strings.TrimPrefix(line, "auth can-i "), "/") {
					t.Errorf("%q puts a subresource in the resource", line)
				}
			}
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightSeedRefusesTheNamespace(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(f *fakeKubectl)
		kind    ErrKind
		mention []string
	}{
		{"absent", func(f *fakeKubectl) {}, NamespaceMissing, []string{"nexus-bench", "does not exist"}},
		{
			"foreign: no label of ours",
			func(f *fakeKubectl) { f.withNamespace(map[string]string{"kubernetes.io/metadata.name": f.p.Namespace}) },
			NamespaceForeign, []string{"nexus-bench", managedByKV},
		},
		{
			"foreign: managed by someone else",
			func(f *fakeKubectl) {
				f.withNamespace(map[string]string{managedByKV: "helm", "kubernetes.io/metadata.name": f.p.Namespace})
			},
			NamespaceForeign, []string{"nexus-bench", managedByKV},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p)
			tt.setup(f)
			_, err := RunPreflight(context.Background(), f, p, OpSeed)
			_ = requireDeployKind(t, err, tt.kind)
			requireMentions(t, err, tt.mention, nil)
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightSeedChecksThePermissionsBeforeTheNamespace(t *testing.T) {
	// The order of RunPreflight: the questions come before the namespace is read,
	// so a user who may do nothing is told so, not that the namespace is absent.
	p := Defaults()
	f := newFakeKubectl(t, p).deny("get", "deployments")
	_, err := RunPreflight(context.Background(), f, p, OpSeed)
	_ = requireDeployKind(t, err, Forbidden)
	requireReadOnly(t, f)
}

func TestRunPreflightSeedStopsBeforeTheQuestionsOnABadCluster(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fakeKubectl)
		kind  ErrKind
	}{
		{"unknown context", func(f *fakeKubectl) { f.contexts = "k0s\nminikube\n" }, ContextNotFound},
		{"API server down", func(f *fakeKubectl) {
			f.readyz = fakeReply{stderr: "Unable to connect to the server: dial tcp: i/o timeout", err: errFakeExit1}
		}, ClusterUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).ours()
			tt.setup(f)
			_, err := RunPreflight(context.Background(), f, p, OpSeed)
			_ = requireDeployKind(t, err, tt.kind)
			if len(f.asked) != 0 {
				t.Errorf("asked %s of a cluster that cannot be reached", fmt.Sprint(asked(f)))
			}
			requireReadOnly(t, f)
		})
	}
}
