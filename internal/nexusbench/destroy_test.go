package nexusbench

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests of Destroy (DEVDESK-504, step 7, risk R1: deleting a namespace is
// irreversible). Destroy is called as
//
//	Destroy(ctx, runner, clock, store, p, out) error
//
// against the scripted kubectl of preflight_test.go, wrapped by destroyKubectl
// below, which adds the two things Destroy needs and the preflight does not:
// the one write the bench may issue, and the reads that follow it. No real
// kubectl, no real wait: the Clock is fakeClock (clock_test.go).
//
// The kubectl calls the tests assume (arguments after "kubectl", without
// --context; "--request-timeout=<d>" may sit anywhere and is ignored):
//
//	config get-contexts -o name        \  RunPreflight(OpDown), first, always:
//	get --raw /readyz                   |  Destroy goes through it and nothing is
//	auth can-i delete namespaces        |  written when it fails
//	get namespace <ns> -o json         /
//	delete namespace <ns> --wait=false    the only write; at most once per Destroy
//	get namespace <ns> -o json            polled, with Clock.Sleep between polls,
//	                                      until NotFound or p.DeleteTimeout
//	get pv -o json                        read once, after the namespace is gone
//
// Anything else fails the test as unscripted. In particular a second write
// (a finalizer patch, a force delete, a delete of anything but the namespace)
// is caught by the "exactly one write" checks.
//
// Hypotheses about what Destroy says, to settle with the code:
//   - an absent namespace prints a line containing "nothing to delete";
//   - a PV left bound to the namespace prints a line containing "warning" and
//     the PV name (case-insensitive for "warning"); without one, no "warning";
//   - the Deadline message names the remaining finalizers of spec.finalizers
//     and, for status.conditions, either the type or the message of a condition;
//   - the namespace is polled at least once per second at most (an interval of
//     1 s or more) and at most every 30 s, so the wait ends within 30 s of
//     p.DeleteTimeout;
//   - a failure to remove the secrets and an unreadable PV list are not tested.

// ---- doubles ----------------------------------------------------------------

// fakePV is one PersistentVolume of the cluster.
type fakePV struct {
	name, claimNamespace, claimName, phase string
}

// destroyKubectl wraps the scripted kubectl: reads go to it until the namespace
// is deleted; the delete itself and what follows are answered here.
type destroyKubectl struct {
	*fakeKubectl
	log *orderLog

	// terminatingPolls is how many "get namespace" after the delete answer
	// Terminating before NotFound; -1 means it never goes away.
	terminatingPolls int
	finalizers       []string
	conditions       []map[string]any

	pvs     []fakePV
	pvReply *fakeReply // when set, answers "get pv -o json"

	deleteReply *fakeReply // when set, answers the delete instead of accepting it

	deleted        bool
	namespacePolls int // "get namespace" calls after the delete
	pvReads        int
	lines          []string // every call, normalized, in order
	writes         []string // every call whose sub-command writes
}

var _ Runner = (*destroyKubectl)(nil)

func newDestroyKubectl(t *testing.T, p Params, log *orderLog) *destroyKubectl {
	t.Helper()
	return &destroyKubectl{
		fakeKubectl:      newFakeKubectl(t, p),
		log:              log,
		terminatingPolls: 2,
		finalizers:       []string{"kubernetes"},
		conditions: []map[string]any{{
			"type":    "NamespaceContentRemaining",
			"status":  "True",
			"reason":  "SomeResourcesRemain",
			"message": "Some resources are remaining: persistentvolumeclaims. has 1 resource instances",
		}},
	}
}

func (d *destroyKubectl) Run(ctx context.Context, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	norm := normalizeArgs(args)
	joined := strings.Join(norm, " ")
	d.lines = append(d.lines, joined)
	d.log.add("KUBECTL " + joined)
	ns := d.p.Namespace

	switch verb := verbOf(args); {
	case verb == "delete":
		d.writes = append(d.writes, joined)
		if joined != "delete namespace "+ns+" --wait=false" {
			d.t.Errorf("unexpected delete call %q, the only write allowed is `delete namespace %s --wait=false`", args, ns)
			return nil, []byte("the fake refuses this delete"), errFakeExit1
		}
		if d.deleteReply != nil {
			return d.deleteReply.reply()
		}
		d.deleted = true
		return []byte("namespace \"" + ns + "\" deleted\n"), nil, nil
	case isWriteVerb(verb):
		d.writes = append(d.writes, joined)
		d.t.Errorf("unexpected write call %q", args)
		return nil, []byte("the fake refuses a write: " + verb), errFakeExit1
	case joined == "get namespace "+ns+" -o json" && d.deleted:
		return d.answerAfterDelete()
	case joined == "get pv -o json":
		d.pvReads++
		return d.answerPVs()
	}
	return d.fakeKubectl.Run(ctx, stdin, args...)
}

func (d *destroyKubectl) answerAfterDelete() ([]byte, []byte, error) {
	d.namespacePolls++
	if d.terminatingPolls >= 0 && d.namespacePolls > d.terminatingPolls {
		return nil, []byte("Error from server (NotFound): namespaces \"" + d.p.Namespace + "\" not found\n"), errFakeExit1
	}
	return d.mustJSON(map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":              d.p.Namespace,
			"labels":            map[string]string{managedByKV: benchOwner, "kubernetes.io/metadata.name": d.p.Namespace},
			"deletionTimestamp": "2026-11-16T12:00:00Z",
		},
		"spec":   map[string]any{"finalizers": d.finalizers},
		"status": map[string]any{"phase": "Terminating", "conditions": d.conditions},
	}), nil, nil
}

func (d *destroyKubectl) answerPVs() ([]byte, []byte, error) {
	if d.pvReply != nil {
		return d.pvReply.reply()
	}
	items := make([]any, 0, len(d.pvs))
	for _, pv := range d.pvs {
		spec := map[string]any{
			"capacity":                      map[string]string{"storage": "5Gi"},
			"accessModes":                   []string{"ReadWriteOnce"},
			"persistentVolumeReclaimPolicy": "Retain",
			"storageClassName":              "local-path",
		}
		if pv.claimNamespace != "" {
			spec["claimRef"] = map[string]any{"kind": "PersistentVolumeClaim", "namespace": pv.claimNamespace, "name": pv.claimName}
		}
		items = append(items, map[string]any{
			"metadata": map[string]any{"name": pv.name},
			"spec":     spec,
			"status":   map[string]any{"phase": pv.phase},
		})
	}
	return d.mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": items}), nil, nil
}

// destroyStore is the SecretStore of the Destroy tests. It journals Remove next
// to the kubectl calls, so a test can prove the secrets go last; Save fails the
// test (Destroy never writes a secret).
type destroyStore struct {
	t   *testing.T
	log *orderLog

	removes   int
	removeErr error
}

var _ SecretStore = (*destroyStore)(nil)

func (s *destroyStore) Load() (Secrets, bool, error) {
	return Secrets{AdminPassword: secretMarker + "-admin", PullPassword: secretMarker + "-pull"}, true, nil
}

func (s *destroyStore) Save(Secrets) error {
	s.t.Error("Destroy saved a secret")
	return nil
}

func (s *destroyStore) Remove() error {
	s.removes++
	s.log.add("STORE Remove")
	return s.removeErr
}

// destroyEnv is one Destroy run: its cluster, clock, store and output.
type destroyEnv struct {
	t     *testing.T
	p     Params
	log   *orderLog
	kube  *destroyKubectl
	clk   *fakeClock
	store *destroyStore
	out   bytes.Buffer
}

// newDestroyEnv is a healthy cluster where the bench namespace is absent.
func newDestroyEnv(t *testing.T, p Params) *destroyEnv {
	t.Helper()
	log := &orderLog{}
	return &destroyEnv{
		t:     t,
		p:     p,
		log:   log,
		kube:  newDestroyKubectl(t, p, log),
		clk:   newFakeClock(t),
		store: &destroyStore{t: t, log: log},
	}
}

// ownedEnv is newDestroyEnv with the namespace there and carrying our label.
func ownedEnv(t *testing.T, p Params) *destroyEnv {
	t.Helper()
	env := newDestroyEnv(t, p)
	env.kube.ours()
	return env
}

func (e *destroyEnv) destroy(ctx context.Context) error {
	e.t.Helper()
	return Destroy(ctx, e.kube, e.clk, e.store, e.p, &e.out)
}

// preflightLines are the calls RunPreflight(OpDown) makes when the namespace exists or not.
func (e *destroyEnv) preflightLines() []string {
	return []string{
		"config get-contexts -o name",
		"get --raw /readyz",
		"auth can-i delete namespaces",
		"get namespace " + e.p.Namespace + " -o json",
	}
}

func (e *destroyEnv) deleteLine() string {
	return "delete namespace " + e.p.Namespace + " --wait=false"
}

func (e *destroyEnv) requireNoWrite() {
	e.t.Helper()
	if len(e.kube.writes) != 0 {
		e.t.Errorf("Destroy wrote to the cluster: %q", e.kube.writes)
	}
}

func (e *destroyEnv) requireOneDelete() {
	e.t.Helper()
	if !slices.Equal(e.kube.writes, []string{e.deleteLine()}) {
		e.t.Errorf("writes = %q, want exactly one: %q", e.kube.writes, e.deleteLine())
	}
}

func (e *destroyEnv) requireStoreUntouched() {
	e.t.Helper()
	if e.store.removes != 0 {
		e.t.Errorf("the secrets were removed %d time(s) although Destroy did not succeed", e.store.removes)
	}
}

// requireNoSecret checks that nothing Destroy said carries a password or the marker.
func (e *destroyEnv) requireNoSecret(err error) {
	e.t.Helper()
	said := e.out.String()
	if err != nil {
		said += "\n" + err.Error()
	}
	for _, banned := range []string{"password", strings.ToLower(secretMarker), strings.ToLower(fakeSecret)} {
		if strings.Contains(strings.ToLower(said), banned) {
			e.t.Errorf("Destroy said %q, which contains %q", said, banned)
		}
	}
}

// requireBenchError checks the failure is a clean *BenchError of the given kind.
func requireBenchKind(t *testing.T, err error, kind ErrKind) *BenchError {
	t.Helper()
	if err == nil {
		t.Fatalf("Destroy succeeded, want a BenchError of kind %d", kind)
	}
	be := requireCleanError(t, err)
	if be.Kind != kind {
		t.Errorf("Kind = %d, want %d; message: %v", be.Kind, kind, err)
	}
	return be
}

// ---- tests ------------------------------------------------------------------

func TestDestroyNamespaceAbsent(t *testing.T) {
	env := newDestroyEnv(t, Defaults())

	err := env.destroy(context.Background())
	if err != nil {
		t.Fatalf("Destroy of an absent namespace = %v, want success", err)
	}
	if !strings.Contains(strings.ToLower(env.out.String()), "nothing to delete") {
		t.Errorf("output %q does not say there is nothing to delete", env.out.String())
	}
	env.requireNoWrite()
	if got := env.kube.lines; !slices.Equal(got, env.preflightLines()) {
		t.Errorf("calls = %q, want only the preflight %q", got, env.preflightLines())
	}
	if len(env.clk.sleeps) != 0 {
		t.Errorf("Destroy slept %d times with nothing to wait for", len(env.clk.sleeps))
	}
	env.requireNoSecret(err)
}

func TestDestroyRefusesAForeignNamespace(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{"no label at all", map[string]string{"kubernetes.io/metadata.name": "nexus-bench"}},
		{"no labels field", nil},
		{"managed by someone else", map[string]string{managedByKV: "helm", "kubernetes.io/metadata.name": "nexus-bench"}},
		{"our value under another key", map[string]string{"app.kubernetes.io/part-of": benchOwner}},
		{"our key with a near value", map[string]string{managedByKV: benchOwner + "-2"}},
		{"only part of the label is right", map[string]string{managedByKV: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newDestroyEnv(t, Defaults())
			env.kube.withNamespace(tt.labels)

			err := env.destroy(context.Background())

			be := requireBenchKind(t, err, NamespaceForeign)
			if !strings.Contains(be.Error(), env.p.Namespace) {
				t.Errorf("message %q does not name the namespace", be.Error())
			}
			env.requireNoWrite()
			if slices.ContainsFunc(env.kube.lines, func(l string) bool { return strings.HasPrefix(l, "delete") }) {
				t.Errorf("a delete was attempted on a namespace that is not ours: %q", env.kube.lines)
			}
			env.requireStoreUntouched()
			env.requireNoSecret(err)
		})
	}
}

func TestDestroyDeletesOurNamespace(t *testing.T) {
	tests := []struct {
		name             string
		terminatingPolls int
	}{
		{"gone at the first poll", 0},
		{"terminating for a few polls", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := ownedEnv(t, Defaults())
			env.kube.terminatingPolls = tt.terminatingPolls

			err := env.destroy(context.Background())
			if err != nil {
				t.Fatalf("Destroy = %v", err)
			}

			// One write, the delete of the namespace, never waited for by kubectl.
			env.requireOneDelete()

			// The exact sequence: preflight, delete, polls until NotFound, PVs.
			want := slices.Clone(env.preflightLines())
			want = append(want, env.deleteLine())
			for range tt.terminatingPolls + 1 {
				want = append(want, "get namespace "+env.p.Namespace+" -o json")
			}
			want = append(want, "get pv -o json")
			if !slices.Equal(env.kube.lines, want) {
				t.Errorf("calls =\n  %q\nwant\n  %q", env.kube.lines, want)
			}

			// The wait goes through the clock: no polling without a sleep between.
			if got, lo, hi := len(env.clk.sleeps), tt.terminatingPolls, tt.terminatingPolls+1; got < lo || got > hi {
				t.Errorf("sleeps = %d, want between %d and %d for %d Terminating answers", got, lo, hi, tt.terminatingPolls)
			}
			if tt.terminatingPolls > 0 && env.clk.elapsed() <= 0 {
				t.Error("the fake clock did not advance")
			}
			if env.kube.pvReads != 1 {
				t.Errorf("PV list read %d times, want once", env.kube.pvReads)
			}

			// The secrets go last: after the namespace is gone and the PVs checked.
			if env.store.removes != 1 {
				t.Errorf("secrets removed %d times, want once", env.store.removes)
			}
			rm, pv := env.log.index("STORE Remove"), env.log.index("KUBECTL get pv -o json")
			del := env.log.index("KUBECTL " + env.deleteLine())
			if del < 0 || pv < del || rm < pv {
				t.Errorf("order wrong: delete at %d, PV read at %d, secrets removed at %d; journal: %q", del, pv, rm, env.log.snapshot())
			}

			if strings.Contains(strings.ToLower(env.out.String()), "warning") {
				t.Errorf("output %q warns although no volume is left", env.out.String())
			}
			env.requireNoSecret(err)
		})
	}
}

// A nil writer is replaced, as Deploy and Provision do.
func TestDestroyAcceptsANilWriter(t *testing.T) {
	env := ownedEnv(t, Defaults())
	env.kube.pvs = []fakePV{{"pvc-1111", "nexus-bench", "nexus-data", "Released"}}
	if err := Destroy(context.Background(), env.kube, env.clk, env.store, env.p, nil); err != nil {
		t.Fatalf("Destroy with a nil writer = %v", err)
	}
	env.requireOneDelete()
}

func TestDestroyWarnsAboutAVolumeLeftBehind(t *testing.T) {
	tests := []struct {
		name      string
		pvs       []fakePV
		warnAbout []string // names the warning must carry
		silentOn  []string // names the output must not carry
		phases    []string // phases the warning must give
	}{
		{
			name:      "a released volume still points at the namespace",
			pvs:       []fakePV{{"pvc-1111", "nexus-bench", "nexus-data", "Released"}},
			warnAbout: []string{"pvc-1111"},
			phases:    []string{"Released"},
		},
		{
			name: "only the volumes of the namespace count",
			pvs: []fakePV{
				{"pvc-2222", "nexus-bench", "nexus-data", "Released"},
				{"pvc-other", "plane", "data", "Bound"},
				{"pvc-same-name", "nexus-bench-2", "nexus-data", "Bound"},
				{"pv-free", "", "", "Available"},
			},
			warnAbout: []string{"pvc-2222"},
			silentOn:  []string{"pvc-other", "pvc-same-name", "pv-free"},
		},
		{
			name: "two volumes are both named",
			pvs: []fakePV{
				{"pvc-3333", "nexus-bench", "a", "Released"},
				{"pvc-4444", "nexus-bench", "b", "Failed"},
			},
			warnAbout: []string{"pvc-3333", "pvc-4444"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := ownedEnv(t, Defaults())
			env.kube.pvs = tt.pvs

			err := env.destroy(context.Background())
			if err != nil {
				t.Fatalf("a leftover volume must warn, not fail; Destroy = %v", err)
			}
			said := env.out.String()
			if !strings.Contains(strings.ToLower(said), "warning") {
				t.Errorf("output %q has no warning", said)
			}
			for _, name := range tt.warnAbout {
				if !strings.Contains(said, name) {
					t.Errorf("output %q does not name the volume %q", said, name)
				}
			}
			// A Delete reclaim policy may remove the volume seconds later: the
			// line says it still exists, never that it was not deleted.
			if !strings.Contains(said, "still exists") {
				t.Errorf("output %q does not say the volume still exists", said)
			}
			if strings.Contains(said, "not deleted") {
				t.Errorf("output %q says the volume was not deleted, which is not known", said)
			}
			for _, phase := range tt.phases {
				if !strings.Contains(said, phase) {
					t.Errorf("output %q does not give the phase %q", said, phase)
				}
			}
			for _, name := range tt.silentOn {
				if strings.Contains(said, name) {
					t.Errorf("output %q names %q, a volume that is not bound to the namespace", said, name)
				}
			}
			env.requireOneDelete()
			// The namespace is gone, so the secrets of it go too.
			if env.store.removes != 1 {
				t.Errorf("secrets removed %d times, want once", env.store.removes)
			}
			env.requireNoSecret(err)
		})
	}
}

func TestDestroyDeadlineWhenTheNamespaceStaysTerminating(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"default delete timeout", Defaults().DeleteTimeout},
		{"a short delete timeout is honored", 45 * time.Second},
	}
	const slack = 30 * time.Second
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			p.DeleteTimeout = tt.timeout
			env := ownedEnv(t, p)
			env.kube.terminatingPolls = -1
			env.kube.finalizers = []string{"kubernetes", "example.com/bench-finalizer"}

			err := env.destroy(context.Background())

			be := requireBenchKind(t, err, Deadline)
			msg := err.Error()
			if !strings.Contains(msg, "example.com/bench-finalizer") {
				t.Errorf("message %q does not cite the remaining finalizer", msg)
			}
			if !strings.Contains(msg, "NamespaceContentRemaining") && !strings.Contains(msg, "persistentvolumeclaims") {
				t.Errorf("message %q cites neither the type nor the message of the remaining condition", msg)
			}
			if !strings.Contains(msg, env.p.Namespace) {
				t.Errorf("message %q does not name the namespace", msg)
			}
			if be.Check == "" {
				t.Error("the Deadline error has no Check")
			}

			// It waited the whole delete timeout, and no longer than that plus one interval.
			if el := env.clk.elapsed(); el < tt.timeout || el > tt.timeout+slack {
				t.Errorf("waited %v on the fake clock, want between %v and %v", el, tt.timeout, tt.timeout+slack)
			}
			// The number of polls is bounded: one per second at the very most.
			if limit := int(tt.timeout/time.Second) + 2; env.kube.namespacePolls > limit || env.kube.namespacePolls < 2 {
				t.Errorf("polled %d times, want between 2 and %d", env.kube.namespacePolls, limit)
			}

			// Stuck is not a reason to force it: the one delete, no finalizer patch.
			env.requireOneDelete()
			env.requireStoreUntouched()
			env.requireNoSecret(err)
		})
	}
}

func TestDestroyForbiddenOnDelete(t *testing.T) {
	env := ownedEnv(t, Defaults())
	env.kube.deleteReply = &fakeReply{
		stderr: "Error from server (Forbidden): namespaces \"nexus-bench\" is forbidden: User \"bench\" cannot delete resource \"namespaces\" in API group \"\" in the namespace \"nexus-bench\"\n",
		err:    errFakeExit1,
	}

	err := env.destroy(context.Background())

	_ = requireBenchKind(t, err, Forbidden)
	env.requireOneDelete() // attempted once, not retried
	env.requireStoreUntouched()
	if len(env.clk.sleeps) != 0 {
		t.Errorf("Destroy waited %d times for a namespace that was never deleted", len(env.clk.sleeps))
	}
	env.requireNoSecret(err)
}

// Every parameter that names a namespace the bench must never touch is refused
// by Validate, before the first kubectl call.
func TestDestroyRefusesInvalidParamsBeforeAnyCall(t *testing.T) {
	for _, ns := range []string{"default", "kube-system", "kube-public", "plane", "traefik"} {
		t.Run(ns, func(t *testing.T) {
			p := Defaults()
			p.Namespace = ns
			env := newDestroyEnv(t, p)
			env.kube.ours() // even labeled, the name alone forbids it

			err := env.destroy(context.Background())

			if err == nil {
				t.Fatalf("Destroy accepted the namespace %q", ns)
			}
			if len(env.kube.lines) != 0 {
				t.Errorf("kubectl was called with invalid parameters: %q", env.kube.lines)
			}
			env.requireStoreUntouched()
			if len(env.clk.sleeps) != 0 {
				t.Errorf("Destroy slept with invalid parameters")
			}
		})
	}
}

// Destroy goes through RunPreflight(OpDown): when it fails, nothing is written.
func TestDestroyStopsWhenThePreflightFails(t *testing.T) {
	tests := []struct {
		name  string
		setup func(k *destroyKubectl)
		kind  ErrKind
		calls []string // the calls up to the failure, or nil to skip the check
	}{
		{
			name:  "not allowed to delete namespaces",
			setup: func(k *destroyKubectl) { k.deny("delete", "namespaces") },
			kind:  Forbidden,
			calls: []string{"config get-contexts -o name", "get --raw /readyz", "auth can-i delete namespaces"},
		},
		{
			name:  "unknown context",
			setup: func(k *destroyKubectl) { k.contexts = "k0s\nminikube\n" },
			kind:  ContextNotFound,
			calls: []string{"config get-contexts -o name"},
		},
		{
			name: "cluster unreachable",
			setup: func(k *destroyKubectl) {
				k.readyz = fakeReply{stderr: "Unable to connect to the server: dial tcp 192.168.1.31:6443: connect: connection refused\n", err: errFakeExit1}
			},
			kind:  ClusterUnreachable,
			calls: []string{"config get-contexts -o name", "get --raw /readyz"},
		},
		{
			name: "namespace of someone else",
			setup: func(k *destroyKubectl) {
				k.withNamespace(map[string]string{managedByKV: "helm"})
			},
			kind: NamespaceForeign,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := ownedEnv(t, Defaults())
			tt.setup(env.kube)

			err := env.destroy(context.Background())

			_ = requireBenchKind(t, err, tt.kind)
			env.requireNoWrite()
			env.requireStoreUntouched()
			if tt.calls != nil && !slices.Equal(env.kube.lines, tt.calls) {
				t.Errorf("calls = %q, want %q", env.kube.lines, tt.calls)
			}
			env.requireNoSecret(err)
		})
	}
}

func TestDestroyStopsWhenTheContextIsCancelledWhileWaiting(t *testing.T) {
	env := ownedEnv(t, Defaults())
	env.kube.terminatingPolls = -1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env.clk.onSleep = func(n int) {
		if n == 2 {
			cancel()
		}
	}

	err := env.destroy(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Destroy = %v, want an error wrapping context.Canceled", err)
	}
	if len(env.clk.sleeps) != 2 {
		t.Errorf("slept %d times, want to stop at the cancelled second one", len(env.clk.sleeps))
	}
	env.requireOneDelete()
	env.requireStoreUntouched()
	env.requireNoSecret(err)
}

// A second down finds nothing to delete and says so; nothing fails.
func TestDestroyIsIdempotent(t *testing.T) {
	env := ownedEnv(t, Defaults())

	if err := env.destroy(context.Background()); err != nil {
		t.Fatalf("first Destroy = %v", err)
	}
	env.out.Reset()
	writesAfterFirst := len(env.kube.writes)
	callsAfterFirst := len(env.kube.lines)

	if err := env.destroy(context.Background()); err != nil {
		t.Fatalf("second Destroy = %v, want success", err)
	}
	if !strings.Contains(strings.ToLower(env.out.String()), "nothing to delete") {
		t.Errorf("second Destroy said %q, want it to say there is nothing to delete", env.out.String())
	}
	if len(env.kube.writes) != writesAfterFirst {
		t.Errorf("the second Destroy wrote: %q", env.kube.writes[writesAfterFirst:])
	}
	if got, want := env.kube.lines[callsAfterFirst:], env.preflightLines(); !slices.Equal(got, want) {
		t.Errorf("second Destroy calls = %q, want only the preflight %q", got, want)
	}
	env.requireOneDelete() // one delete in all, across both runs
	env.requireNoSecret(nil)
}
