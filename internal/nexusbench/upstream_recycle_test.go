package nexusbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests of the recycling of the upstream storage (DEVDESK-534, NX-02b, step 5,
// task DEVDESK-552):
//
//	recycleUpstreamStorage(ctx, runner, clock, p, out) error
//
// against the scripted kubectl of upstream_test.go (upstreamKubectl) extended below
// by recycleKubectl with the two verbs the recycling adds, delete and apply, and the
// state of the claim bench-upstream-data, and the fake clock of clock_test.go. No
// real kubectl, no real wait, nothing on a cluster.
//
// Like CutUpstream and EnsureUpstream, recycleUpstreamStorage runs no preflight: its
// caller (Reset, step 6) runs OpReset first. The calls the tests assume (arguments
// after "kubectl", without --context; "--request-timeout=<d>" is ignored):
//
//	get deployment bench-upstream -n <ns> -o json        (CutUpstream, EnsureUpstream)
//	get pods -n <ns> -l app.kubernetes.io/name=bench-upstream -o json
//	get pvc bench-upstream-data -n <ns> -o json          (the label check, then the wait
//	                                                      for NotFound, one read per round)
//	get pvc -n <ns> -o json                              (the wait of EnsureUpstream)
//	scale deployment/bench-upstream --replicas=0|1 -n <ns>
//	delete --raw /api/v1/namespaces/<ns>/persistentvolumeclaims/bench-upstream-data -f -
//	                                                     (stdin: DeleteOptions with the UID the read answered)
//	apply --server-side --field-manager=devdesk-nexus-bench -f - (stdin: the claim alone)
//
// Choices of the tests, to settle with the code (the plan gives the order and the
// verbs, not every argument):
//   - The order of the writes is exactly: scale to 0, delete of the claim, apply of
//     the claim, scale to 1. Nothing else is ever written: not the Deployment of
//     Nexus, not nexus-data, not the namespace, no pod, no rollout, no exec.
//   - The claim is read by name (get pvc bench-upstream-data -o json) before the
//     delete: absent means no delete (the apply and the scale still run); a claim
//     without app.kubernetes.io/managed-by=devdesk-nexus-bench means no delete, no
//     apply and no scale to 1 (an error, whether the cut came before the read or
//     not). The delete is a raw delete whose body holds a precondition on the UID the
//     read answered (a claim replaced in between is not deleted; the API server
//     answers Conflict), sent once and never retried; it does not wait (DeleteOptions
//     of the API server, no --wait); the wait for NotFound reads the claim by name
//     every round and is bounded by Params.DeleteTimeout, not by Params.Timeout.
//   - The apply carries ONE document, the claim: the 5th document of Render, never
//     the whole stream (a full apply with other parameters would roll the pod of
//     Nexus, which the story forbids). It happens only once the claim is NotFound.
//   - The upstream is brought back with EnsureUpstream: scale to 1, then the wait of
//     the pods until Ready, bounded by Params.Timeout.
//   - Every failure is a *BenchError classified like the others (a delete or a scale
//     refused: Forbidden; an apply: applyFailure; a wait: Deadline) whose text has no
//     password and none of the stderr of kubectl; no write is retried. A cancelled
//     context is returned as is. A custom upstream URL is refused before any call. A
//     nil out is accepted.

// recycleKubectl answers delete and apply on top of an upstreamKubectl, and the read
// of the claim bench-upstream-data by name from a state of its own. The reads of the
// list of claims, of the pods and of the Deployments stay with the rounds of the
// embedded fake.
type recycleKubectl struct {
	*upstreamKubectl

	// afterCut and afterRestore are the rounds played after a scale to 0 and after
	// a scale to 1: the embedded fake has one script after a scale, and the two
	// scales of the recycling need two.
	afterCut, afterRestore []upstreamRound

	claim claimState

	// The failures a test scripts; each answers its call and has no effect.
	claimReply, deleteReply, applyReply *fakeReply
	// scaleFailsFor, when "0" or "1", fails the scale to that count.
	scaleFailsFor string

	ops     []string // "scale 0", "scale 1", "delete", "apply", in order, failed ones included
	deletes []string // the normalized delete calls
	deleted [][]byte // the stdin of every delete
	applies [][]byte // the stdin of every apply
}

var _ Runner = (*recycleKubectl)(nil)

// claimState is the claim bench-upstream-data as the cluster holds it.
type claimState struct {
	exists bool
	labels map[string]string
	phase  string
	uid    string

	// terminating is true once the claim was deleted. A terminating claim is read
	// stuckReads more times, then it is gone; a negative count keeps it for ever (the
	// finalizer kubernetes.io/pvc-protection of a claim a pod still mounts).
	terminating bool
	stuckReads  int
}

func benchLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       upstreamName,
		"app.kubernetes.io/managed-by": managedByValue,
		"app.kubernetes.io/part-of":    managedByValue,
	}
}

const (
	benchClaimUID = "5d1c0a7e-0000-4000-8000-000000000001"
	newClaimUID   = "5d1c0a7e-0000-4000-8000-000000000002"
)

func presentClaim() claimState {
	return claimState{exists: true, labels: benchLabels(), phase: "Bound", uid: benchClaimUID, stuckReads: 2}
}

func (k *recycleKubectl) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, []byte, error) {
	norm := normalizeArgs(args)
	switch verb := verbOf(args); verb {
	case "delete":
		return k.deleteClaim(stdin, norm)
	case "apply":
		return k.applyClaim(stdin, norm)
	case "scale":
		k.beforeScale(norm)
	case "get":
		if g, ok := k.parseGet(norm); ok && g.kind == "pvc" && g.name == upstreamClaim {
			return k.readClaim(norm)
		}
	}
	return k.upstreamKubectl.Run(ctx, stdin, args...)
}

// beforeScale records the scale and picks the rounds that follow it.
func (k *recycleKubectl) beforeScale(norm []string) {
	replicas := ""
	for _, a := range norm {
		if v, ok := strings.CutPrefix(a, "--replicas="); ok {
			replicas = v
		}
	}
	k.ops = append(k.ops, "scale "+replicas)
	k.scaleReply = nil
	if replicas == k.scaleFailsFor {
		k.scaleReply = &fakeReply{
			stderr: "Error from server (Forbidden): deployments.apps \"bench-upstream\" is forbidden: cannot patch resource \"deployments/scale\" token=" + fakeSecret,
			err:    errFakeExit1,
		}
	}
	if replicas == "0" {
		k.after = k.afterCut
	} else {
		k.after = k.afterRestore
	}
}

// journal records a call the fake answers itself in the journals of the embedded fake.
func (k *recycleKubectl) journal(norm []string, write bool) {
	line := strings.Join(norm, " ")
	k.lines = append(k.lines, line)
	if write {
		k.writes = append(k.writes, line)
	}
}

func (k *recycleKubectl) claimObject() map[string]any {
	meta := map[string]any{"name": upstreamClaim, "namespace": k.p.Namespace, "labels": k.claim.labels, "uid": k.claim.uid}
	if k.claim.terminating {
		meta["deletionTimestamp"] = "2026-11-17T09:00:00Z"
		meta["finalizers"] = []string{"kubernetes.io/pvc-protection"}
	}
	return map[string]any{"metadata": meta, "status": map[string]any{"phase": k.claim.phase}}
}

func (k *recycleKubectl) readClaim(norm []string) ([]byte, []byte, error) {
	k.journal(norm, false)
	if k.claimReply != nil {
		return k.claimReply.reply()
	}
	if k.claim.exists && k.claim.terminating {
		if k.claim.stuckReads == 0 {
			k.claim.exists = false
		} else if k.claim.stuckReads > 0 {
			k.claim.stuckReads--
		}
	}
	if !k.claim.exists {
		return notFoundReply("persistentvolumeclaims \"" + upstreamClaim + "\"")
	}
	return k.mustJSON(k.claimObject()), nil, nil
}

func (k *recycleKubectl) deleteClaim(stdin []byte, norm []string) ([]byte, []byte, error) {
	k.journal(norm, true)
	k.ops = append(k.ops, "delete")
	k.deletes = append(k.deletes, strings.Join(norm, " "))
	k.deleted = append(k.deleted, bytes.Clone(stdin))
	raw, file := "", ""
	for i := 1; i < len(norm); i++ {
		switch a := norm[i]; a {
		case "--raw":
			i++
			raw = at(norm, i)
		case "-f", "--filename":
			i++
			file = at(norm, i)
		default:
			k.t.Errorf("unscripted argument %q in %q: the delete of the claim is `delete --raw <path of the claim> -f -`", a, norm)
		}
	}
	if raw != claimAPIPath(k.p) || file != "-" {
		k.t.Errorf("delete %q: want --raw %s -f - (nothing else is ever deleted)", norm, claimAPIPath(k.p))
		return nil, []byte("the fake refuses this delete"), errFakeExit1
	}
	var body struct {
		Kind          string `json:"kind"`
		APIVersion    string `json:"apiVersion"`
		Preconditions struct {
			UID string `json:"uid"`
		} `json:"preconditions"`
	}
	if err := json.Unmarshal(stdin, &body); err != nil || body.Kind != "DeleteOptions" || body.APIVersion != "v1" || body.Preconditions.UID == "" {
		k.t.Errorf("the body of the delete is %q, want DeleteOptions v1 with preconditions.uid", stdin)
		return nil, []byte("the fake refuses this delete"), errFakeExit1
	}
	if k.deleteReply != nil {
		return k.deleteReply.reply()
	}
	if !k.claim.exists {
		return notFoundReply("persistentvolumeclaims \"" + upstreamClaim + "\"")
	}
	if body.Preconditions.UID != k.claim.uid {
		return nil, []byte("Error from server (Conflict): Precondition failed: UID in precondition: " + body.Preconditions.UID +
			", UID in object meta: " + k.claim.uid + "\n"), errFakeExit1
	}
	k.claim.terminating = true
	return []byte("{\"kind\":\"PersistentVolumeClaim\"}"), nil, nil
}

func (k *recycleKubectl) applyClaim(stdin []byte, norm []string) ([]byte, []byte, error) {
	k.journal(norm, true)
	k.ops = append(k.ops, "apply")
	k.applies = append(k.applies, bytes.Clone(stdin))
	joined := strings.Join(norm, " ")
	for _, want := range []string{"--server-side", "--field-manager=" + fieldManager, "-f -"} {
		if !strings.Contains(joined, want) {
			k.t.Errorf("apply %q lacks %q", joined, want)
		}
	}
	if k.applyReply != nil {
		return k.applyReply.reply()
	}
	if k.claim.exists {
		k.t.Errorf("apply while the claim still exists (terminating: %v): it must be NotFound first", k.claim.terminating)
		return nil, []byte("the fake refuses an apply over a live claim"), errFakeExit1
	}
	k.claim = claimState{exists: true, labels: benchLabels(), phase: "Pending", uid: newClaimUID}
	return []byte("persistentvolumeclaim/" + upstreamClaim + " serverside-applied\n"), nil, nil
}

// recycleEnv is one run: a cluster, a clock, an output.
type recycleEnv struct {
	*upstreamEnv
	k *recycleKubectl
}

// recycleParams are the parameters of a run: the wait for the claim has a bound of
// its own, shorter than the one of the pods, so a test sees which one the code used.
func recycleParams() Params {
	p := upstreamParams()
	p.DeleteTimeout = 30 * time.Second
	return p
}

// restoreRounds are the rounds after the scale to 1: a pod being created, then ready.
func restoreRounds() []upstreamRound {
	return []upstreamRound{roundOf(1, creatingPod()), roundOf(1, readyPod())}
}

// newRecycleEnv is a ready upstream, a labelled claim that disappears two reads after
// its delete, and rounds after each scale that need one sleep to see the pod leave
// and one to see the new pod ready.
func newRecycleEnv(t *testing.T, p Params) *recycleEnv {
	t.Helper()
	e := &recycleEnv{upstreamEnv: newUpstreamEnv(t, p, []upstreamRound{roundOf(1, readyPod())}, nil)}
	e.k = &recycleKubectl{
		upstreamKubectl: e.kube,
		afterCut:        []upstreamRound{goingRound(goingPod(upstreamPodName)), cutRound()},
		afterRestore:    restoreRounds(),
		claim:           presentClaim(),
		scaleFailsFor:   "none",
	}
	return e
}

func (e *recycleEnv) recycle(ctx context.Context) error {
	e.t.Helper()
	return recycleUpstreamStorage(ctx, e.k, e.clk, e.p, &e.out)
}

// requireOps checks the writes the run made, in order.
func (e *recycleEnv) requireOps(want ...string) {
	e.t.Helper()
	if !slices.Equal(e.k.ops, want) {
		e.t.Errorf("writes = %q, want %q", e.k.ops, want)
	}
}

// requireNoOp fails when the run made a write that starts with prefix.
func (e *recycleEnv) requireNoOp(prefix string) {
	e.t.Helper()
	for _, op := range e.k.ops {
		if strings.HasPrefix(op, prefix) {
			e.t.Errorf("writes = %q, want none that starts with %q", e.k.ops, prefix)
			return
		}
	}
}

// requireTouchesOnlyTheUpstream checks everything written: no Nexus, no nexus-data,
// no namespace, no pod, no rollout, no exec, and nothing but the four verbs.
func (e *recycleEnv) requireTouchesOnlyTheUpstream() {
	e.t.Helper()
	for _, w := range e.kube.writes {
		verb := strings.Fields(w)[0]
		if !slices.Contains([]string{"scale", "delete", "apply"}, verb) {
			e.t.Errorf("write %q: the verbs of the recycling are scale, delete and apply", w)
		}
		// The only path in a write is the one of the claim, which holds "namespaces":
		// it is checked as a whole, then taken out of the text the words are looked for in.
		rest := w
		if verb == "delete" {
			if !strings.HasPrefix(w, "delete --raw "+claimAPIPath(e.p)+" ") {
				e.t.Errorf("write %q: a delete is the raw one on the claim of the upstream", w)
			}
			rest = strings.Replace(w, claimAPIPath(e.p), "", 1)
		}
		for _, banned := range []string{"nexus-data", "deployment/nexus ", "namespace", "rollout", "pod", "exec"} {
			if strings.Contains(rest, banned) {
				e.t.Errorf("write %q contains %q: the recycling touches the upstream and its claim only", w, banned)
			}
		}
	}
	for _, l := range e.kube.lines {
		if strings.Contains(l, "nexus-data") {
			e.t.Errorf("call %q names the claim of Nexus", l)
		}
	}
}

// requireTheClaimAlone checks the stdin of the apply is the one document of the
// claim of the upstream, the one the full stream of Render holds.
func (e *recycleEnv) requireTheClaimAlone(stdin []byte) {
	e.t.Helper()
	doc := string(stdin)
	full, err := Render(e.p)
	if err != nil {
		e.t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(full), doc) {
		e.t.Errorf("the applied document is not a document of Render:\n%s", doc)
	}
	for _, line := range strings.Split(doc, "\n") {
		if strings.TrimSpace(line) == "---" {
			e.t.Errorf("the applied stream has several documents: %q", doc)
		}
	}
	for _, want := range []string{"kind: PersistentVolumeClaim", "name: " + upstreamClaim} {
		if !strings.Contains(doc, want) {
			e.t.Errorf("the applied document lacks %q:\n%s", want, doc)
		}
	}
	for _, banned := range []string{"kind: Deployment", "kind: Service", "kind: Namespace", "nexus-data"} {
		if strings.Contains(doc, banned) {
			e.t.Errorf("the applied stream contains %q: only the claim of the upstream is applied", banned)
		}
	}
}

// ---- the nominal paths ------------------------------------------------------

func TestRecycleUpstreamStorageReplacesTheClaim(t *testing.T) {
	e := newRecycleEnv(t, recycleParams())
	if err := e.recycle(context.Background()); err != nil {
		t.Fatalf("recycleUpstreamStorage: %v", err)
	}
	e.requireOps("scale 0", "delete", "apply", "scale 1")
	e.requireTouchesOnlyTheUpstream()
	e.kube.requireNoPreflight()
	e.requireNoSecret(nil)

	// The upstream is cut before the claim is deleted, and read before it is deleted.
	scale0 := indexOfLine(e.kube.lines, "scale ")
	label := indexOfLine(e.kube.lines, "get pvc bench-upstream-data")
	del := indexOfLine(e.kube.lines, "delete ")
	if scale0 < 0 || del < scale0 {
		t.Errorf("the delete does not follow the scale to 0: %q", e.kube.lines)
	}
	if label < 0 || del < label {
		t.Errorf("the claim is not read before it is deleted: %q", e.kube.lines)
	}
	// The pods are listed until none is left before the claim goes.
	if pods := indexOfLine(e.kube.lines[scale0+1:], "get pods"); pods < 0 || scale0+1+pods > del {
		t.Errorf("no pod read between the scale to 0 and the delete: %q", e.kube.lines)
	}

	if len(e.k.deletes) != 1 {
		t.Errorf("%d deletes, want one: %q", len(e.k.deletes), e.k.deletes)
	}
	// The delete carries a precondition on the UID of the claim that was read.
	if len(e.k.deleted) == 1 && !strings.Contains(string(e.k.deleted[0]), `"uid":"`+benchClaimUID+`"`) {
		t.Errorf("the body of the delete %q lacks the precondition on the UID %q", e.k.deleted[0], benchClaimUID)
	}
	if len(e.k.applies) != 1 {
		t.Fatalf("%d applies, want one", len(e.k.applies))
	}
	e.requireTheClaimAlone(e.k.applies[0])

	// The new claim is in place and the upstream is ready.
	if !e.k.claim.exists || e.k.claim.terminating {
		t.Errorf("the claim after the recycling: %+v", e.k.claim)
	}
	if !strings.Contains(e.out.String(), "upstream: ") {
		t.Errorf("progress lines %q do not carry the upstream: prefix", e.out.String())
	}
}

func TestRecycleUpstreamStorageWaitsForTheClaimToBeGoneBeforeTheApply(t *testing.T) {
	e := newRecycleEnv(t, recycleParams())
	e.k.claim.stuckReads = 3
	if err := e.recycle(context.Background()); err != nil {
		t.Fatalf("recycleUpstreamStorage: %v", err)
	}
	// The fake fails an apply over a claim that still exists; the sleeps are the wait.
	e.requireOps("scale 0", "delete", "apply", "scale 1")
	// One sleep for the pod that leaves, three for the claim that is terminating, one
	// for the new pod. The code may poll the claim more often, never less.
	if got := len(e.clk.sleeps); got < 5 {
		t.Errorf("slept %d times (%v), want at least 5: the pod, the claim (3 reads) and the new pod", got, e.clk.sleeps)
	}
	for i, d := range e.clk.sleeps {
		if d <= 0 || d > maxPollInterval {
			t.Errorf("sleep #%d lasts %v, want a poll of at most %v", i+1, d, maxPollInterval)
		}
	}
}

func TestRecycleUpstreamStorageWithoutAClaimStillApplies(t *testing.T) {
	// An earlier run stopped between the delete and the apply: the claim is not there.
	e := newRecycleEnv(t, recycleParams())
	e.k.claim = claimState{}
	if err := e.recycle(context.Background()); err != nil {
		t.Fatalf("recycleUpstreamStorage: %v", err)
	}
	e.requireOps("scale 0", "apply", "scale 1")
	e.requireNoOp("delete")
	e.requireTouchesOnlyTheUpstream()
	if len(e.k.applies) != 1 {
		t.Fatalf("%d applies, want one", len(e.k.applies))
	}
	e.requireTheClaimAlone(e.k.applies[0])
}

func TestRecycleUpstreamStorageStartsAgainAfterAnInterruptedRun(t *testing.T) {
	e := newRecycleEnv(t, recycleParams())
	e.k.applyReply = &fakeReply{stderr: "Error from server (Forbidden): persistentvolumeclaims is forbidden token=" + fakeSecret, err: errFakeExit1}
	err := e.recycle(context.Background())
	_ = requireDeployKind(t, err, Forbidden)
	e.requireOps("scale 0", "delete", "apply")

	// The cluster is as the failure left it: the upstream cut, the claim gone.
	e.k.applyReply = nil
	e.kube.before = []upstreamRound{cutRound()}
	e.kube.scaledAt = -1
	e.k.ops = nil
	if err := e.recycle(context.Background()); err != nil {
		t.Fatalf("second recycleUpstreamStorage: %v", err)
	}
	// The upstream is already cut: no scale to 0; the claim is already gone: no delete.
	e.requireOps("apply", "scale 1")
	e.requireTouchesOnlyTheUpstream()
}

func TestRecycleUpstreamStorageAcceptsANilWriter(t *testing.T) {
	e := newRecycleEnv(t, recycleParams())
	if err := recycleUpstreamStorage(context.Background(), e.k, e.clk, e.p, nil); err != nil {
		t.Fatalf("recycleUpstreamStorage with a nil writer: %v", err)
	}
	e.requireOps("scale 0", "delete", "apply", "scale 1")
}

// ---- what it never touches --------------------------------------------------

func TestRecycleUpstreamStorageNeverAppliesTheFullStream(t *testing.T) {
	// Other parameters than the bench's (memory, image, ports): a full apply of them
	// would roll the pod of Nexus. The claim alone does not depend on them.
	p := recycleParams()
	p.MemoryLimit, p.MemoryRequest = "4Gi", "3Gi"
	p.HTTPNodePort, p.DockerNodePort = 30490, 30491
	if err := p.Validate(); err != nil {
		t.Fatalf("the test parameters are not valid: %v", err)
	}
	e := newRecycleEnv(t, p)
	if err := e.recycle(context.Background()); err != nil {
		t.Fatalf("recycleUpstreamStorage: %v", err)
	}
	if len(e.k.applies) != 1 {
		t.Fatalf("%d applies, want one", len(e.k.applies))
	}
	e.requireTheClaimAlone(e.k.applies[0])
	for _, banned := range []string{"4Gi", "3Gi", "30490", "30491"} {
		if strings.Contains(string(e.k.applies[0]), banned) {
			t.Errorf("the applied document carries %q, a parameter of Nexus", banned)
		}
	}
	e.requireTouchesOnlyTheUpstream()
}

func TestRecycleUpstreamStorageRefusesAClaimTheBenchDidNotCreate(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{"no label at all", nil},
		{"the name label only", map[string]string{"app.kubernetes.io/name": upstreamName}},
		{"managed by someone else", map[string]string{"app.kubernetes.io/name": upstreamName, "app.kubernetes.io/managed-by": "helm"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecycleEnv(t, recycleParams())
			e.k.claim.labels = tt.labels
			err := e.recycle(context.Background())
			if err == nil {
				t.Fatal("recycleUpstreamStorage deleted a claim the bench did not create")
			}
			_ = requireCleanError(t, err)
			requireMentions(t, err, []string{upstreamClaim, "app.kubernetes.io/managed-by"}, nil)
			// The cut may come before the read of the label or after it.
			e.requireNoOp("delete")
			e.requireNoOp("apply")
			e.requireNoOp("scale 1")
			e.requireNoSecret(err)
		})
	}
}

// ---- failures, one per step -------------------------------------------------

func TestRecycleUpstreamStorageFailsAtTheCut(t *testing.T) {
	t.Run("the scale to zero is refused", func(t *testing.T) {
		e := newRecycleEnv(t, recycleParams())
		e.k.scaleFailsFor = "0"
		err := e.recycle(context.Background())
		be := requireDeployKind(t, err, Forbidden)
		if be.Err == nil {
			t.Error("the BenchError dropped the underlying error")
		}
		e.requireOps("scale 0")
		e.requireNoSecret(err)
	})
	t.Run("a pod of the upstream never leaves", func(t *testing.T) {
		p := recycleParams()
		e := newRecycleEnv(t, p)
		e.k.afterCut = []upstreamRound{goingRound(goingPod(upstreamPodName))}
		err := e.recycle(context.Background())
		_ = requireDeployKind(t, err, Deadline)
		requireMentions(t, err, []string{upstreamPodName}, nil)
		e.requireOps("scale 0")
		if el := e.clk.elapsed(); el < p.Timeout || el >= p.Timeout+upstreamPollInterval {
			t.Errorf("gave up after %v, want %v within one interval", el, p.Timeout)
		}
	})
	t.Run("the Deployment is missing", func(t *testing.T) {
		e := newRecycleEnv(t, recycleParams())
		e.kube.before = []upstreamRound{absentRound()}
		err := e.recycle(context.Background())
		_ = requireCleanError(t, err)
		requireMentions(t, err, []string{upstreamName, "nexus-bench:up"}, nil)
		e.requireOps()
	})
}

func TestRecycleUpstreamStorageFailsAtTheReadOfTheClaim(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		kind   ErrKind
	}{
		{"forbidden", `Error from server (Forbidden): persistentvolumeclaims "bench-upstream-data" is forbidden token=` + fakeSecret, Forbidden},
		{"the cluster does not answer", "Unable to connect to the server: dial tcp: i/o timeout token=" + fakeSecret, ClusterUnreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecycleEnv(t, recycleParams())
			e.k.claimReply = &fakeReply{stderr: tt.stderr, err: errFakeExit1}
			err := e.recycle(context.Background())
			_ = requireDeployKind(t, err, tt.kind)
			e.requireNoOp("delete")
			e.requireNoOp("apply")
			e.requireNoOp("scale 1")
			e.requireNoSecret(err)
		})
	}
}

func TestRecycleUpstreamStorageFailsAtTheDelete(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		kind   ErrKind
	}{
		{"forbidden", `Error from server (Forbidden): persistentvolumeclaims "bench-upstream-data" is forbidden: cannot delete token=` + fakeSecret, Forbidden},
		{"the cluster does not answer", "Unable to connect to the server: dial tcp: i/o timeout token=" + fakeSecret, ClusterUnreachable},
		{"an unknown failure", "error: something went wrong token=" + fakeSecret, Unclassified},
		{"the claim was replaced after the read", "Error from server (Conflict): Precondition failed: UID in precondition: " + benchClaimUID + " token=" + fakeSecret, Unclassified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecycleEnv(t, recycleParams())
			e.k.deleteReply = &fakeReply{stderr: tt.stderr, err: errFakeExit1}
			err := e.recycle(context.Background())
			be := requireDeployKind(t, err, tt.kind)
			requireMentions(t, err, []string{upstreamClaim}, nil)
			if be.Err == nil {
				t.Error("the BenchError dropped the underlying error")
			}
			if len(e.k.deletes) != 1 {
				t.Errorf("%d delete attempts, want 1: a failed delete is not retried", len(e.k.deletes))
			}
			e.requireOps("scale 0", "delete")
			e.requireNoSecret(err)
		})
	}
}

func TestRecycleUpstreamStorageGivesUpOnAClaimThatStaysTerminating(t *testing.T) {
	tests := []struct {
		name    string
		timeout func(p *Params)
	}{
		{"the default delete timeout of the test", func(*Params) {}},
		{"a shorter delete timeout", func(p *Params) { p.DeleteTimeout = 12 * time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := recycleParams()
			tt.timeout(&p)
			e := newRecycleEnv(t, p)
			// The cut needs no sleep, so the clock measures the wait for the claim alone.
			e.k.afterCut = []upstreamRound{cutRound()}
			e.k.claim.stuckReads = -1
			err := e.recycle(context.Background())
			be := requireDeployKind(t, err, Deadline)
			requireMentions(t, err, []string{upstreamClaim, "kubernetes.io/pvc-protection"}, nil)
			if !strings.Contains(be.Check, "get pvc") {
				t.Errorf("Check %q does not name the kubectl get pvc command", be.Check)
			}
			e.requireOps("scale 0", "delete")
			if el := e.clk.elapsed(); el < p.DeleteTimeout || el >= p.DeleteTimeout+maxPollInterval {
				t.Errorf("gave up after %v, want %v (NEXUS_BENCH_DELETE_TIMEOUT, not the %v of the pods) within one interval", el, p.DeleteTimeout, p.Timeout)
			}
			if len(e.k.deletes) != 1 {
				t.Errorf("%d deletes, want 1: the wait never deletes again", len(e.k.deletes))
			}
			e.requireNoSecret(err)
		})
	}
}

func TestRecycleUpstreamStorageFailsAtTheApply(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		kind   ErrKind
	}{
		{"forbidden", "Error from server (Forbidden): persistentvolumeclaims is forbidden: cannot create token=" + fakeSecret, Forbidden},
		{"an admission webhook denies it", `Error from server: admission webhook "policy.example.test" denied the request token=` + fakeSecret, AdmissionDenied},
		{"the cluster does not answer", "Unable to connect to the server: dial tcp: i/o timeout token=" + fakeSecret, ClusterUnreachable},
		{"an unknown failure", "error: something went wrong token=" + fakeSecret, Unclassified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecycleEnv(t, recycleParams())
			e.k.applyReply = &fakeReply{stderr: tt.stderr, err: errFakeExit1}
			err := e.recycle(context.Background())
			be := requireDeployKind(t, err, tt.kind)
			requireMentions(t, err, []string{upstreamClaim}, nil)
			if be.Err == nil {
				t.Error("the BenchError dropped the underlying error")
			}
			if len(e.k.applies) != 1 {
				t.Errorf("%d apply attempts, want 1: a failed apply is not retried", len(e.k.applies))
			}
			// The upstream stays cut: the volume it needs is not there.
			e.requireOps("scale 0", "delete", "apply")
			e.requireNoSecret(err)
		})
	}
}

func TestRecycleUpstreamStorageFailsAtTheRestart(t *testing.T) {
	t.Run("the scale to one is refused", func(t *testing.T) {
		e := newRecycleEnv(t, recycleParams())
		e.k.scaleFailsFor = "1"
		err := e.recycle(context.Background())
		_ = requireDeployKind(t, err, Forbidden)
		e.requireOps("scale 0", "delete", "apply", "scale 1")
		e.requireNoSecret(err)
	})
	t.Run("the new pod never gets ready", func(t *testing.T) {
		p := recycleParams()
		e := newRecycleEnv(t, p)
		e.k.afterRestore = []upstreamRound{roundOf(1, startingPod())}
		err := e.recycle(context.Background())
		_ = requireDeployKind(t, err, Deadline)
		e.requireOps("scale 0", "delete", "apply", "scale 1")
		e.requireNoSecret(err)
	})
}

// ---- the guards -------------------------------------------------------------

func TestRecycleUpstreamStorageRefusesACustomUpstream(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(p *Params)
	}{
		{"another URL", func(p *Params) { p.UpstreamURL = "http://registry.example.test:5000" }},
		{"the URL of the bench in another namespace", func(p *Params) { p.UpstreamURL = upstreamFor("nexus-bench-2") }},
		{"a namespace that the URL does not follow", func(p *Params) { p.Namespace = "nexus-bench-2" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := recycleParams()
			tt.mutate(&p)
			if p.UpstreamIsBench() {
				t.Fatalf("the case is not a custom upstream: %q in %q", p.UpstreamURL, p.Namespace)
			}
			e := newRecycleEnv(t, p)
			err := e.recycle(context.Background())
			if err == nil {
				t.Fatal("recycleUpstreamStorage accepted an upstream that is not the bench's")
			}
			var be *BenchError
			if !errors.As(err, &be) || be.Cause == "" || be.Check == "" {
				t.Fatalf("error %T %v is not a complete *BenchError", err, err)
			}
			requireMentions(t, err, []string{envUpstreamURL}, nil)
			if len(e.kube.lines) != 0 {
				t.Errorf("the refusal came after kubectl calls: %q", e.kube.lines)
			}
			e.requireOps()
		})
	}
}

func TestRecycleUpstreamStorageReturnsACancelledContextAsIs(t *testing.T) {
	requireAsIs := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		var be *BenchError
		if errors.As(err, &be) {
			t.Errorf("a cancelled context is a BenchError (%v), want it as is", be)
		}
	}
	t.Run("cancelled before the start: nothing is read or written", func(t *testing.T) {
		e := newRecycleEnv(t, recycleParams())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		requireAsIs(t, e.recycle(ctx))
		e.requireOps()
		if len(e.kube.lines) != 0 {
			t.Errorf("kubectl was called with a cancelled context: %q", e.kube.lines)
		}
	})
	t.Run("cancelled while the claim is terminating: no apply", func(t *testing.T) {
		e := newRecycleEnv(t, recycleParams())
		e.k.afterCut = []upstreamRound{cutRound()}
		e.k.claim.stuckReads = -1
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.clk.onSleep = func(n int) {
			if n == 2 {
				cancel()
			}
		}
		requireAsIs(t, e.recycle(ctx))
		e.requireOps("scale 0", "delete")
	})
	t.Run("cancelled while the new pod starts", func(t *testing.T) {
		e := newRecycleEnv(t, recycleParams())
		e.k.afterRestore = []upstreamRound{roundOf(1, creatingPod())}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Cancel at the first sleep after the scale to one: the wait of the new pod.
		e.clk.onSleep = func(int) {
			if slices.Contains(e.k.ops, "scale 1") {
				cancel()
			}
		}
		requireAsIs(t, e.recycle(ctx))
		e.requireOps("scale 0", "delete", "apply", "scale 1")
	})
}

// ---- what the claim delete is built from ------------------------------------

// The delete of the claim is a raw call whose path holds the namespace and whose
// body holds the UID: a UID that is empty would turn the precondition into no
// precondition at all, and a namespace that is not a namespace name would build
// another path. Both are refused before the delete is sent.
func TestRecycleUpstreamStorageRefusesAClaimWithoutAUID(t *testing.T) {
	e := newRecycleEnv(t, recycleParams())
	e.k.claim.uid = ""
	err := e.recycle(context.Background())
	if err == nil {
		t.Fatal("recycleUpstreamStorage deleted a claim it read without a UID")
	}
	requireMentions(t, err, []string{upstreamClaim, "UID"}, nil)
	e.requireNoOp("delete")
	e.requireNoOp("apply")
	e.requireNoOp("scale 1")
	if len(e.k.deleted) != 0 {
		t.Errorf("%d deletes were sent", len(e.k.deleted))
	}
	e.requireNoSecret(err)
}

func TestClaimDeleteRefusesANamespaceThatIsNotOne(t *testing.T) {
	for _, ns := range []string{"", "Nexus-Bench", "nexus bench", "nexus-bench/../kube-system", "-nexus", "nexus-bench\n--all"} {
		t.Run(fmt.Sprintf("%q", ns), func(t *testing.T) {
			p := recycleParams()
			p.Namespace = ns
			p.UpstreamURL = upstreamFor(ns)
			if !p.UpstreamIsBench() {
				t.Fatalf("the case is a custom upstream, not a namespace: %q", ns)
			}
			t.Run("recycleUpstreamStorage", func(t *testing.T) {
				e := newRecycleEnv(t, p)
				err := e.recycle(context.Background())
				if err == nil {
					t.Fatal("recycleUpstreamStorage accepted the namespace")
				}
				requireMentions(t, err, []string{envNamespace}, nil)
				if len(e.kube.lines) != 0 {
					t.Errorf("the refusal came after kubectl calls: %q", e.kube.lines)
				}
				e.requireOps()
			})
			t.Run("deleteUpstreamClaim", func(t *testing.T) {
				e := newRecycleEnv(t, p)
				d := &deployer{pf: preflight{r: e.k, p: p}, w: upstreamWorkload(), tag: "upstream", clk: e.clk, out: &e.out}
				err := d.deleteUpstreamClaim(context.Background())
				if err == nil {
					t.Fatal("deleteUpstreamClaim accepted the namespace")
				}
				requireMentions(t, err, []string{envNamespace}, nil)
				if len(e.kube.lines) != 0 {
					t.Errorf("the refusal came after kubectl calls: %q", e.kube.lines)
				}
				e.requireOps()
			})
		})
	}
}
