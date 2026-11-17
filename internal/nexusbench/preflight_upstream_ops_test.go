package nexusbench

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The preflights of the upstream operations (DEVDESK-534, NX-02b, step 1):
// OpCut (cut-upstream), OpRestore (restore-upstream) and OpReset (reset).
// Against the scripted kubectl of preflight_test.go, which records every
// "auth can-i" question in order; asked, inNamespace, seedQuestions and
// requireRepeatsTheQuestion come from preflight_seed_test.go.
//
// What the preflights ask, as the code does it:
//   - OpCut, OpRestore and OpReset are appended after OpSeed, in this order; no
//     earlier value moves.
//   - OpCut asks, in this order, all in the bench namespace: get deployments,
//     list pods, patch deployments --subresource=scale. It reads the upstream
//     Deployment, waits until no pod is left and scales it to zero: it needs no
//     port-forward, no Service and no claim.
//   - OpRestore asks exactly what OpSeed asks, in the same order (a port-forward,
//     EnsureUpstream and its claim reads, the scale). If one list changes, the
//     test that compares them says so.
//   - OpReset asks what OpRestore asks, then delete, create and patch on
//     persistentvolumeclaims (the apply --server-side of an absent claim needs
//     create). It asks no pods/exec: a reset never changes the admin password.
//   - None of the three reads the nodes: the REST calls and the images go through
//     the loopback, so PreflightResult.NodeIP is empty. readsNode is true for
//     OpUp, OpProvision and OpStatus only.
//   - All three refuse an absent namespace (NamespaceMissing) and a foreign one
//     (NamespaceForeign), like seed.
//   - A refused question ends the preflight there; nothing is ever written.

// upstreamOpName names the three operations in sub-test titles and messages
// (testOpName of preflight_test.go does not know them).
var upstreamOpName = map[Op]string{OpCut: "cut", OpRestore: "restore", OpReset: "reset"}

// upstreamOps are the three operations, in the order of the constants.
var upstreamOps = []Op{OpCut, OpRestore, OpReset}

// claimWrites are the three questions only OpReset adds to those of OpRestore.
var claimWrites = []string{
	"delete persistentvolumeclaims",
	"create persistentvolumeclaims",
	"patch persistentvolumeclaims",
}

// cutQuestions are the questions of OpCut, in the order the tests expect.
var cutQuestions = []string{
	"get deployments", "list pods", "patch deployments --subresource=scale",
}

// questionsOf are the questions of an upstream operation, in order.
func questionsOf(op Op) []string {
	switch op {
	case OpCut:
		return cutQuestions
	case OpRestore:
		return seedQuestions
	case OpReset:
		return slices.Concat(seedQuestions, claimWrites)
	}
	panic(fmt.Sprintf("no questions scripted for op %d", op))
}

// denialOf turns "create pods --subresource=portforward" into the pair deny
// takes, ("create", "pods/portforward").
func denialOf(question string) (verb, resource string) {
	fields := strings.Fields(question)
	verb, resource = fields[0], fields[1]
	if len(fields) > 2 {
		resource += "/" + strings.TrimPrefix(fields[2], "--subresource=")
	}
	return verb, resource
}

func TestRunPreflightUpstreamOpsAreAppended(t *testing.T) {
	// The earlier values are pinned by TestRunPreflightOpSeedIsAppended; here the
	// three new ones follow OpSeed, one after the other.
	if OpCut != OpSeed+1 || OpRestore != OpCut+1 || OpReset != OpRestore+1 {
		t.Errorf("OpSeed, OpCut, OpRestore, OpReset = %d, %d, %d, %d: want four consecutive values in this order",
			OpSeed, OpCut, OpRestore, OpReset)
	}
	for v, want := range []Op{OpUp, OpProvision, OpDown, OpStatus, OpSeed} {
		if want != Op(v) {
			t.Errorf("operation #%d = %d: operations are appended, never renumbered", v, want)
		}
	}
}

func TestRunPreflightUpstreamOpsAskTheirQuestionsInOrder(t *testing.T) {
	for _, op := range upstreamOps {
		t.Run(upstreamOpName[op], func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).ours()
			res, err := RunPreflight(context.Background(), f, p, op)
			if err != nil {
				t.Fatalf("RunPreflight: %v", err)
			}
			if got, want := asked(f), inNamespace(p, questionsOf(op)...); !slices.Equal(got, want) {
				t.Errorf("questions =\n  %q\nwant\n  %q", got, want)
			}
			if want := (PreflightResult{NamespaceExists: true, NamespaceOurs: true}); res != want {
				t.Errorf("result = %+v, want %+v", res, want)
			}
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightUpstreamOpsAskNothingOutsideTheNamespace(t *testing.T) {
	// The three operations act on one namespace: no cluster-wide question and no
	// -A (the NodePort scan is up's), and no pods/exec (no admin password change).
	for _, op := range upstreamOps {
		t.Run(upstreamOpName[op], func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).ours()
			if _, err := RunPreflight(context.Background(), f, p, op); err != nil {
				t.Fatalf("RunPreflight: %v", err)
			}
			for _, q := range f.asked {
				if q.allNamespaces || q.namespace != p.Namespace {
					t.Errorf("question %q is not scoped to the namespace of the run", q)
				}
				if q.subresource == "exec" {
					t.Errorf("question %q: an upstream operation never execs into a pod", q)
				}
			}
			for _, line := range f.callLines() {
				if strings.HasPrefix(line, "get services") {
					t.Errorf("%s read the Services of the cluster (%q): the NodePort check is up's", upstreamOpName[op], line)
				}
			}
		})
	}
}

func TestRunPreflightUpstreamOpsHonorTheNamespaceParameter(t *testing.T) {
	for _, op := range upstreamOps {
		t.Run(upstreamOpName[op], func(t *testing.T) {
			p := Defaults()
			p.Namespace = "nexus-bench-2"
			p.UpstreamURL = upstreamFor(p.Namespace)
			f := newFakeKubectl(t, p).ours()
			if _, err := RunPreflight(context.Background(), f, p, op); err != nil {
				t.Fatalf("RunPreflight: %v", err)
			}
			got := asked(f)
			if len(got) != len(questionsOf(op)) {
				t.Errorf("asked %d questions, want %d: %q", len(got), len(questionsOf(op)), got)
			}
			for _, q := range got {
				if !strings.HasSuffix(q, " -n nexus-bench-2") {
					t.Errorf("question %q is not scoped to the namespace of the run", q)
				}
			}
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightUpstreamOpsStopAtTheFirstRefusedQuestion(t *testing.T) {
	for _, op := range upstreamOps {
		all := inNamespace(Defaults(), questionsOf(op)...)
		for i, q := range questionsOf(op) {
			verb, resource := denialOf(q)
			t.Run(upstreamOpName[op]+"/"+verb+" "+resource, func(t *testing.T) {
				p := Defaults()
				f := newFakeKubectl(t, p).ours().deny(verb, resource)
				_, err := RunPreflight(context.Background(), f, p, op)
				be := requireDeployKind(t, err, Forbidden)
				requireMentions(t, err, append([]string{verb}, strings.Split(resource, "/")...), nil)
				requireRepeatsTheQuestion(t, be.Check, verb, resource)
				if got, want := asked(f), all[:i+1]; !slices.Equal(got, want) {
					t.Errorf("questions = %q, want them to stop at the refused one: %q", got, want)
				}
				requireReadOnly(t, f)
			})
		}
	}
}

func TestRunPreflightRestoreAsksWhatSeedAsks(t *testing.T) {
	// restore needs exactly the rights of seed (the port-forward, EnsureUpstream
	// with its claim reads, the scale), in the same order. The lists are compared
	// so that a change on one side fails here, not in production.
	if got, want := permissionsByOp[OpRestore], permissionsByOp[OpSeed]; len(got) == 0 || !slices.Equal(got, want) {
		t.Errorf("permissionsByOp[OpRestore] =\n  %+v\nwant the list of OpSeed\n  %+v", got, want)
	}
	p := Defaults()
	seed := newFakeKubectl(t, p).ours()
	restore := newFakeKubectl(t, p).ours()
	for op, f := range map[Op]*fakeKubectl{OpSeed: seed, OpRestore: restore} {
		if _, err := RunPreflight(context.Background(), f, p, op); err != nil {
			t.Fatalf("RunPreflight(%d): %v", op, err)
		}
	}
	if !slices.Equal(asked(seed), asked(restore)) {
		t.Errorf("restore asked\n  %q\nseed asked\n  %q", asked(restore), asked(seed))
	}
}

func TestRunPreflightResetAsksWhatRestoreAsksThenTheClaimWrites(t *testing.T) {
	restore, reset := permissionsByOp[OpRestore], permissionsByOp[OpReset]
	if len(restore) == 0 || len(reset) != len(restore)+len(claimWrites) || !slices.Equal(reset[:len(restore)], restore) {
		t.Fatalf("permissionsByOp[OpReset] =\n  %+v\nwant the list of OpRestore, then %q", reset, claimWrites)
	}
	for i, q := range claimWrites {
		verb, resource := denialOf(q)
		want := permission{verb, resource, "", scopeNamespace}
		if got := reset[len(restore)+i]; got != want {
			t.Errorf("permissionsByOp[OpReset][%d] = %+v, want %+v", len(restore)+i, got, want)
		}
	}
}

func TestRunPreflightOnlyResetAsksToWriteClaims(t *testing.T) {
	for _, op := range []Op{OpCut, OpRestore} {
		t.Run(upstreamOpName[op], func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).ours()
			if _, err := RunPreflight(context.Background(), f, p, op); err != nil {
				t.Fatalf("RunPreflight: %v", err)
			}
			for _, q := range claimWrites {
				if slices.Contains(asked(f), q+" -n "+p.Namespace) {
					t.Errorf("%s asked %q: only reset recycles the claim", upstreamOpName[op], q)
				}
			}
		})
	}
	// cut touches only the Deployment of the upstream: no claim, no tunnel, no Service.
	p := Defaults()
	f := newFakeKubectl(t, p).ours()
	if _, err := RunPreflight(context.Background(), f, p, OpCut); err != nil {
		t.Fatalf("RunPreflight(OpCut): %v", err)
	}
	for _, q := range asked(f) {
		for _, banned := range []string{"persistentvolumeclaims", "portforward", "services", "namespaces", "secrets"} {
			if strings.Contains(q, banned) {
				t.Errorf("cut asked %q: it needs none of %s", q, banned)
			}
		}
	}
}

func TestRunPreflightUpstreamOpsDoNotReadTheNodes(t *testing.T) {
	for _, op := range upstreamOps {
		t.Run(upstreamOpName[op], func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).ours()
			res, err := RunPreflight(context.Background(), f, p, op)
			if err != nil {
				t.Fatalf("RunPreflight: %v", err)
			}
			for _, line := range f.callLines() {
				if strings.Contains(line, "nodes") {
					t.Errorf("%s issued %q: it must not read the nodes", upstreamOpName[op], line)
				}
			}
			if res.NodeIP != "" {
				t.Errorf("NodeIP = %q, want it empty", res.NodeIP)
			}
		})
	}
}

func TestOpReadsNode(t *testing.T) {
	// up, provision and status read the node address; down and the four
	// operations that go through the loopback do not.
	want := map[Op]bool{
		OpUp: true, OpProvision: true, OpStatus: true,
		OpDown: false, OpSeed: false, OpCut: false, OpRestore: false, OpReset: false,
	}
	for op, reads := range want {
		if got := op.readsNode(); got != reads {
			t.Errorf("Op(%d).readsNode() = %v, want %v", op, got, reads)
		}
	}
	// And the preflight follows: status, up and provision still return the address.
	for _, op := range []Op{OpUp, OpProvision, OpStatus} {
		p := Defaults()
		f := newFakeKubectl(t, p).ours()
		res, err := RunPreflight(context.Background(), f, p, op)
		if err != nil {
			t.Fatalf("RunPreflight(%s): %v", testOpName[op], err)
		}
		if res.NodeIP != fakeNodeIP {
			t.Errorf("%s: NodeIP = %q, want %q", testOpName[op], res.NodeIP, fakeNodeIP)
		}
	}
}

func TestRunPreflightUpstreamOpsRefuseTheNamespace(t *testing.T) {
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
	for _, op := range upstreamOps {
		for _, tt := range tests {
			t.Run(upstreamOpName[op]+"/"+tt.name, func(t *testing.T) {
				p := Defaults()
				f := newFakeKubectl(t, p)
				tt.setup(f)
				_, err := RunPreflight(context.Background(), f, p, op)
				_ = requireDeployKind(t, err, tt.kind)
				requireMentions(t, err, tt.mention, nil)
				requireReadOnly(t, f)
			})
		}
	}
}

func TestRunPreflightUpstreamOpsCheckThePermissionsBeforeTheNamespace(t *testing.T) {
	// A user who may do nothing is told so, not that the namespace is absent.
	for _, op := range upstreamOps {
		t.Run(upstreamOpName[op], func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).deny("get", "deployments")
			_, err := RunPreflight(context.Background(), f, p, op)
			_ = requireDeployKind(t, err, Forbidden)
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightUpstreamOpsStopBeforeTheQuestionsOnABadCluster(t *testing.T) {
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
	for _, op := range upstreamOps {
		for _, tt := range tests {
			t.Run(upstreamOpName[op]+"/"+tt.name, func(t *testing.T) {
				p := Defaults()
				f := newFakeKubectl(t, p).ours()
				tt.setup(f)
				_, err := RunPreflight(context.Background(), f, p, op)
				_ = requireDeployKind(t, err, tt.kind)
				if len(f.asked) != 0 {
					t.Errorf("asked %s of a cluster that cannot be reached", fmt.Sprint(asked(f)))
				}
				requireReadOnly(t, f)
			})
		}
	}
}

func TestRunPreflightUpstreamOpsValidateBeforeAnyCall(t *testing.T) {
	for _, op := range upstreamOps {
		t.Run(upstreamOpName[op], func(t *testing.T) {
			p := Defaults()
			p.Namespace = "Not A Namespace"
			f := newFakeKubectl(t, p).ours()
			if _, err := RunPreflight(context.Background(), f, p, op); err == nil {
				t.Fatal("RunPreflight accepted an invalid namespace")
			}
			if len(f.calls) != 0 {
				t.Errorf("kubectl was called %d times before the parameters were validated: %q", len(f.calls), f.callLines())
			}
		})
	}
}
