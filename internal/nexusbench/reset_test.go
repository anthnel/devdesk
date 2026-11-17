package nexusbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests of Reset (DEVDESK-534, NX-02b, step 6, task DEVDESK-553; CA7):
//
//	Reset(ctx, SeedDeps{Runner, Forwarder, Copier, Clock, Store}, baseURL, p, out) (ProvisionResult, SeedResult, error)
//	storedPasswordOnly{}                       the InitialPassword of a reset: it never reads the pod
//	waitRepositoriesEmpty(ctx, c, clk, repos...) error
//
// against a provisioned and seeded fake Nexus (fakenexus_test.go), the scripted
// kubectl of upstream_recycle_test.go (recycleKubectl, wrapped below by
// resetKubectl for the claim UID), the recording port-forward and image copier of
// seed_test.go (wrapped below by resetCopier, which empties a registry the way
// the real one empties when its repository or its volume is replaced), and the fake
// clock. The kubectl calls, the REST requests, the port-forward, the copier calls
// and the sleeps share one journal (bench.log), so the order is proven.
//
// What the tests assume, to settle with the code (the order is D-P of the plan,
// completed by the review of steps 4 and 5):
//   - Order: the context is looked at -> RunPreflight(OpReset), run by Reset itself
//     (a refused permission, an absent or foreign namespace end the run there, whatever
//     else is wrong) -> a custom upstream is refused, and so is a REST origin that is not
//     the loopback -> the stored credentials, with no read of the pod and no
//     change of any password -> one GET as admin (requireAdmin) -> the claim
//     bench-upstream-data read by name and its managed-by label checked (an absent
//     claim passes) -> and only then the first write of any kind: DELETE of
//     docker-group, docker-proxy, docker-hosted, in this order (404 is "already
//     done") -> recycleUpstreamStorage (scale to 0, delete of the claim, apply of
//     the claim alone, scale to 1) -> Provision with storedPasswordOnly{} -> a wait,
//     bounded by seedListWait, until docker-hosted and docker-proxy list nothing
//     (H-REPODEL, still to measure at step 9) -> Seed.
//   - A refusal before the first write (permissions, namespace, custom upstream,
//     origin, credentials, admin refused, claim without the label, claim unreadable)
//     leaves the bench as it was: no kubectl write, no REST write, no copy.
//   - What is written, in all: DELETE of the three repositories, whatever Provision
//     writes, and on the cluster the scale of bench-upstream, the delete of the claim
//     and the apply of the claim alone. Never the namespace, the Deployment of Nexus,
//     its pod, nexus-data, a user, a role, an admin password; never the full stream.
//   - The delete of the claim carries a precondition on the UID of the claim the
//     bench read (a claim replaced in between is not deleted). Form chosen by these
//     tests, the one kubectl offers for a precondition:
//     delete --raw /api/v1/namespaces/<ns>/persistentvolumeclaims/bench-upstream-data -f -
//     with the stdin {"kind":"DeleteOptions","apiVersion":"v1","preconditions":{"uid":"<uid>"}}.
//     The UID is the one the last get of the claim answered before the delete; the fake
//     answers Conflict for any other UID, and the delete is sent once.
//   - An error after the first DELETE keeps its Kind and its cause, and its Check ends
//     with halfWayCheck. One exception: a repository that never empties
//     (SeedFailed) points at the log of the Nexus pod and does not suggest reset again.
//   - A cancelled context is returned as is (errors.Is, not a *BenchError).
//   - Output: lines "reset: ..." plus the lines of the steps it runs (provision:,
//     upstream:, seed:); no password anywhere; a nil writer is accepted.
//
// The REST origin of the tests is the loopback of the fake instance (127.0.0.1); no
// test depends on localhost being accepted or refused.

// halfWayCheck ends the Check of every error after the first DELETE.
const halfWayCheck = "the reset stopped half-way: run `mise run nexus-bench:reset` again, it starts over"

// claimAPIPath is the path the delete of the claim is sent to.
func claimAPIPath(p Params) string {
	return "/api/v1/namespaces/" + p.Namespace + "/persistentvolumeclaims/" + upstreamClaim
}

// ---- doubles ------------------------------------------------------------------

// kubeLogEntry is the line of the shared journal for one kubectl call.
func kubeLogEntry(norm []string) string {
	switch verb := verbOf(norm); verb {
	case "scale":
		for _, a := range norm {
			if v, ok := strings.CutPrefix(a, "--replicas="); ok {
				return "kube scale " + v
			}
		}
		return "kube scale"
	case "delete", "apply":
		return "kube " + verb
	}
	var words []string
	for _, a := range norm {
		if strings.HasPrefix(a, "-") || len(words) == 4 {
			break
		}
		words = append(words, a)
	}
	return "kube " + strings.Join(words, " ")
}

// resetKubectl adds to recycleKubectl what a reset needs: the UID of the claim, the
// delete with a precondition on it, and the shared journal.
type resetKubectl struct {
	*recycleKubectl
	log *orderLog

	// generation is 1 for the claim of the bench and grows each time the claim is
	// created again: the UID of a new claim is another one.
	generation int
	// staleUID makes every read report a UID the claim does not have, as a claim
	// replaced after the read does.
	staleUID bool

	uidsRead     []string // the UID each read of the claim answered
	deleteBodies [][]byte
	deleteUIDs   []string // the UID of the precondition of each delete
	liveUIDs     []string // the UID of the claim when each delete arrived
	readBefore   []string // the last UID read before each delete
}

func newResetKubectl(t *testing.T, p Params, clk *fakeClock, log *orderLog) *resetKubectl {
	t.Helper()
	uk := newUpstreamKubectl(t, p, clk, []upstreamRound{readyUpstream()}, nil)
	rk := &recycleKubectl{
		upstreamKubectl: uk,
		afterCut:        []upstreamRound{goingRound(goingPod(upstreamPodName)), cutRound()},
		afterRestore:    restoreRounds(),
		claim:           presentClaim(),
		scaleFailsFor:   "none",
	}
	return &resetKubectl{recycleKubectl: rk, log: log, generation: 1}
}

func (k *resetKubectl) claimUID() string {
	return fmt.Sprintf("5d1c0a7e-0000-4000-8000-%012d", k.generation)
}

func (k *resetKubectl) reportedUID() string {
	if k.staleUID {
		return "00000000-dead-4000-8000-000000000000"
	}
	return k.claimUID()
}

var _ Runner = (*resetKubectl)(nil)

func (k *resetKubectl) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, []byte, error) {
	norm := normalizeArgs(args)
	k.log.add(kubeLogEntry(norm))
	switch verbOf(args) {
	case "delete":
		return k.deleteWithUID(stdin, norm)
	case "apply":
		out, errOut, err := k.recycleKubectl.Run(ctx, stdin, args...)
		if err == nil {
			k.generation++
		}
		return out, errOut, err
	case "get":
		if g, ok := k.parseGet(norm); ok && g.kind == "pvc" && g.name == upstreamClaim {
			return k.readClaimWithUID(norm)
		}
	}
	return k.recycleKubectl.Run(ctx, stdin, args...)
}

// readClaimWithUID is the read of the claim by name, with its UID.
func (k *resetKubectl) readClaimWithUID(norm []string) ([]byte, []byte, error) {
	out, errOut, err := k.readClaim(norm)
	if err != nil {
		return out, errOut, err
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		k.t.Fatalf("the claim of the fake is not JSON: %v", err)
	}
	obj["metadata"].(map[string]any)["uid"] = k.reportedUID()
	k.uidsRead = append(k.uidsRead, k.reportedUID())
	return k.mustJSON(obj), nil, nil
}

// deleteWithUID answers the delete of the claim: only the raw form with a
// precondition on the UID is scripted, and a UID that is not the one of the claim is
// a Conflict, as the API server answers it.
func (k *resetKubectl) deleteWithUID(stdin []byte, norm []string) ([]byte, []byte, error) {
	k.journal(norm, true)
	k.ops = append(k.ops, "delete")
	k.deletes = append(k.deletes, strings.Join(norm, " "))
	k.deleteBodies = append(k.deleteBodies, bytes.Clone(stdin))
	raw, file := "", ""
	for i := 1; i < len(norm); i++ {
		switch a := norm[i]; {
		case a == "--raw":
			i++
			raw = at(norm, i)
		case strings.HasPrefix(a, "--raw="):
			raw = strings.TrimPrefix(a, "--raw=")
		case a == "-f" || a == "--filename":
			i++
			file = at(norm, i)
		default:
			k.t.Errorf("unscripted argument %q in %q: the delete of the claim is `delete --raw <path of the claim> -f -`", a, norm)
		}
	}
	if raw != claimAPIPath(k.p) || file != "-" {
		k.t.Errorf("delete %q: want --raw %s -f - (nothing else is ever deleted, and always with a precondition)", norm, claimAPIPath(k.p))
		return nil, []byte("the fake refuses this delete"), errFakeExit1
	}
	var body struct {
		Kind          string `json:"kind"`
		APIVersion    string `json:"apiVersion"`
		Preconditions struct {
			UID string `json:"uid"`
		} `json:"preconditions"`
	}
	if err := json.Unmarshal(stdin, &body); err != nil || body.Kind != "DeleteOptions" || body.Preconditions.UID == "" {
		k.t.Errorf("the body of the delete is %q, want DeleteOptions with preconditions.uid", stdin)
		return nil, []byte("the fake refuses this delete"), errFakeExit1
	}
	k.deleteUIDs = append(k.deleteUIDs, body.Preconditions.UID)
	k.liveUIDs = append(k.liveUIDs, k.claimUID())
	last := ""
	if n := len(k.uidsRead); n > 0 {
		last = k.uidsRead[n-1]
	}
	k.readBefore = append(k.readBefore, last)
	if k.deleteReply != nil {
		return k.deleteReply.reply()
	}
	if !k.claim.exists {
		return notFoundReply("persistentvolumeclaims \"" + upstreamClaim + "\"")
	}
	if body.Preconditions.UID != k.claimUID() {
		return nil, []byte("Error from server (Conflict): Precondition failed: UID in precondition: " + body.Preconditions.UID +
			", UID in object meta: " + k.claimUID() + "\n"), errFakeExit1
	}
	k.claim.terminating = true
	return []byte("persistentvolumeclaim \"" + upstreamClaim + "\" deleted\n"), nil, nil
}

// resetCopier is the seedCopier whose registries lose their content when what holds
// it is replaced: the upstream when its claim is created again, docker-hosted and
// docker-proxy when their repository is deleted.
type resetCopier struct {
	*seedCopier
	kube  *resetKubectl
	nexus *fakeNexus

	generationSeen int
	deletesSeen    map[string]int
}

var _ ImageCopier = (*resetCopier)(nil)

func (c *resetCopier) sync() {
	if c.kube.generation > c.generationSeen {
		c.generationSeen = c.kube.generation
		clear(c.upstream)
	}
	for repo, empty := range map[string]func(){
		dockerHostedRepository: func() { clear(c.hosted) },
		dockerProxyRepository:  func() { clear(c.proxyCache) },
	} {
		if n := c.nexus.Count(http.MethodDelete, deletePath(repo)); n > c.deletesSeen[repo] {
			c.deletesSeen[repo] = n
			empty()
			delete(c.listing, repo)
		}
	}
}

func (c *resetCopier) Copy(ctx context.Context, src, dst ImageRef, o CopyOptions) error {
	c.sync()
	return c.seedCopier.Copy(ctx, src, dst, o)
}

func (c *resetCopier) ManifestDigest(ctx context.Context, ref ImageRef, creds *Credentials) (string, error) {
	c.sync()
	return c.seedCopier.ManifestDigest(ctx, ref, creds)
}

// Components returns a copy of what a repository lists.
func (f *fakeNexus) Components(repo string) []fakeComponent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.components[repo])
}

// ClearFaults removes every injected fault.
func (f *fakeNexus) ClearFaults() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = nil
}

// resetEnv is one bench: a provisioned Nexus, a cluster, registries, a clock.
type resetEnv struct {
	t    *testing.T
	b    *bench
	p    Params
	clk  *fakeClock
	kube *resetKubectl
	fwd  *fakeForwarder
	cp   *resetCopier
	log  *orderLog
	tmp  string
	out  *bytes.Buffer
	deps SeedDeps

	nilOut   bool
	baseURL  string
	onSleep  func(n int)
	runs     int
	baseReq  int // requests of the fake Nexus before the first run
	runReq   int // ... before the current run
	runEvent int // entries of the journal before the current run
}

// newResetEnv is a bench in the state a seed leaves it: the three repositories, the
// data set in the registries, an upstream with one ready replica and its claim. The
// temporary folder is private to the test.
func newResetEnv(t *testing.T) *resetEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	b := provisionedBench(t)
	p := recycleParams()
	log := b.log
	clk := newFakeClock(t)
	e := &resetEnv{t: t, b: b, p: p, clk: clk, log: log, tmp: tmp, out: &bytes.Buffer{}, baseURL: b.nexus.URL()}
	clk.onSleep = func(n int) {
		log.add("sleep " + clk.sleeps[n-1].String())
		if e.onSleep != nil {
			e.onSleep(n)
		}
	}
	e.kube = newResetKubectl(t, p, clk, log)
	e.fwd = &fakeForwarder{t: t, log: log, local: seedForwardLocal}
	sec := b.store.current()
	if sec.PullPassword == "" {
		t.Fatal("the provisioned bench stored no bench-pull password")
	}
	cp := &seedCopier{
		t: t, log: log, fwd: e.fwd, nexus: b.nexus, p: &e.p, tmp: tmp,
		admin:      Credentials{User: adminUserID, Password: sec.AdminPassword},
		pull:       Credentials{User: pullUserID, Password: sec.PullPassword},
		upstream:   map[string]string{},
		hosted:     map[string]string{},
		proxyCache: map[string]string{},
		listing:    map[string][]fakeComponent{},
		hidden:     map[string]bool{},
		blind:      map[string]bool{},
	}
	e.cp = &resetCopier{seedCopier: cp, kube: e.kube, nexus: b.nexus, generationSeen: 1, deletesSeen: map[string]int{}}
	e.deps = SeedDeps{Runner: e.kube, Forwarder: e.fwd, Copier: e.cp, Clock: clk, Store: b.store}
	e.baseReq = len(b.nexus.Requests())
	return e
}

// newSeededResetEnv is newResetEnv with the data set in place.
func newSeededResetEnv(t *testing.T) *resetEnv {
	t.Helper()
	e := newResetEnv(t)
	e.cp.preloadAll()
	return e
}

// pollute adds what a measure leaves behind: a tag in docker-hosted, an image read
// through docker-proxy that the data set does not cache, a probe cached by the proxy,
// and an image only the upstream holds.
func (e *resetEnv) pollute() {
	e.t.Helper()
	uncached := datasetImage(e.t, "mirror/uncached", "1.0", LocationUpstream)
	other := "sha256:" + strings.Repeat("d", 64)
	e.cp.addExtra(dockerHostedRepository, fakeComponent{Name: "api", Version: "9.9.9", Digest: other})
	e.cp.hosted["api:9.9.9"] = other
	e.cp.preload(whereProxy, uncached, uncached.Digest)
	e.cp.addExtra(dockerProxyRepository, fakeComponent{Name: "probe/p1", Version: "1.0", Digest: other})
	e.cp.proxyCache["probe/p1:1.0"] = other
	e.cp.upstream["scratch/leftover:1.0"] = other
}

func (e *resetEnv) run() (ProvisionResult, SeedResult, error) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	return e.runWith(ctx)
}

func (e *resetEnv) runWith(ctx context.Context) (ProvisionResult, SeedResult, error) {
	e.t.Helper()
	var out io.Writer = e.out
	if e.nilOut {
		out = nil
	}
	e.runs++
	e.runReq = len(e.b.nexus.Requests())
	e.runEvent = len(e.log.snapshot())
	prov, seed, err := Reset(ctx, e.deps, e.baseURL, e.p, out)
	e.check(err)
	return prov, seed, err
}

func (e *resetEnv) mustReset() (ProvisionResult, SeedResult) {
	e.t.Helper()
	prov, seed, err := e.run()
	if err != nil {
		e.t.Fatalf("Reset: %v", err)
	}
	return prov, seed
}

// events is the journal of the current run.
func (e *resetEnv) events() []string { return e.log.snapshot()[e.runEvent:] }

// reqs are the requests the fake Nexus received during the current run.
func (e *resetEnv) reqs() []fakeReq { return e.b.nexus.Requests()[e.runReq:] }

func (e *resetEnv) secrets() []string { return append(e.b.knownSecrets(), fakeSecret) }

// closedDeletes are the only paths a DELETE may ever be sent to.
func closedDeletes() []string {
	var out []string
	for _, name := range []string{dockerGroupRepository, dockerProxyRepository, dockerHostedRepository} {
		out = append(out, deletePath(name))
	}
	return out
}

// allowedPrefixes are the lines a run may print: its own and those of the steps it runs.
var allowedPrefixes = []string{"reset: ", "provision: ", "upstream: ", "seed: "}

// check is what holds after every call, whatever the outcome.
func (e *resetEnv) check(err error) {
	e.t.Helper()
	// Nexus: always the stored admin, but the anonymous reads of the version; the only
	// DELETEs are those of the three repositories; the admin password is never changed.
	for _, r := range e.b.nexus.Requests()[e.baseReq:] {
		anonymous := r.User == "" && r.Method == http.MethodGet && (r.Path == pathStatus || r.Path == pathSwagger)
		if !anonymous && (r.User != adminUserID || r.EmptyPassword) {
			e.t.Errorf("request %s %s sent as %q (empty password: %v), want the stored admin", r.Method, r.Path, r.User, r.EmptyPassword)
		}
		if r.Method == http.MethodDelete && !slices.Contains(closedDeletes(), r.Path) {
			e.t.Errorf("DELETE %s: a reset deletes docker-group, docker-proxy and docker-hosted, nothing else", r.Path)
		}
		if r.Path == pathAdminPassword {
			e.t.Errorf("%s %s: a reset never changes the admin password", r.Method, r.Path)
		}
	}
	e.checkClusterWrites()
	for _, line := range e.kube.lines {
		for _, banned := range []string{"nexus-data", "name=nexus", "exec", "rollout"} {
			if strings.Contains(line, banned) {
				e.t.Errorf("kubectl call %q contains %q: a reset reads and writes the upstream and its claim only", line, banned)
			}
		}
	}
	for _, q := range asked(e.kube.fakeKubectl) {
		if strings.Contains(q, "exec") {
			e.t.Errorf("the preflight asked %q: a reset never reads the pod", q)
		}
	}
	// One port-forward per run at most, always stopped.
	if len(e.fwd.calls) > e.runs {
		e.t.Errorf("%d port-forwards for %d runs", len(e.fwd.calls), e.runs)
	}
	for _, call := range e.fwd.calls {
		if want := (forwardCall{e.p.Namespace, upstreamName, 5000}); call != want {
			e.t.Errorf("port-forward to %+v, want %+v", call, want)
		}
	}
	if e.fwd.active {
		e.t.Error("the port-forward is still open when Reset returns")
	}
	opened := 0
	for _, ev := range e.log.snapshot() {
		if ev == "forward" {
			opened++
		}
	}
	if e.fwd.err == nil && e.fwd.stops < opened {
		e.t.Errorf("%d forwards opened, stop called %d times", opened, e.fwd.stops)
	}
	// The temporary folders of the seed are gone.
	if left, rerr := os.ReadDir(e.tmp); rerr != nil || len(left) != 0 {
		e.t.Errorf("temporary files left under TMPDIR: %v (%v)", left, rerr)
	}
	// Progress lines.
	for _, line := range strings.Split(strings.TrimSpace(e.out.String()), "\n") {
		if line != "" && !slices.ContainsFunc(allowedPrefixes, func(p string) bool { return strings.HasPrefix(line, p) }) {
			e.t.Errorf("progress line %q starts with none of %q", line, allowedPrefixes)
		}
	}
	e.requireQuiet(err)
}

// checkClusterWrites checks every write the cluster saw: the scale of bench-upstream,
// the delete of its claim with a precondition, and the apply of the claim alone.
func (e *resetEnv) checkClusterWrites() {
	e.t.Helper()
	for _, w := range e.kube.writes {
		switch verb := strings.Fields(w)[0]; verb {
		case "scale":
			if !strings.HasPrefix(w, "scale deployment/"+upstreamName+" ") {
				e.t.Errorf("write %q: the only scale is the one of %s", w, upstreamName)
			}
		case "delete":
			if !strings.HasPrefix(w, "delete --raw "+claimAPIPath(e.p)+" ") {
				e.t.Errorf("write %q: the only delete is the one of the claim %s", w, upstreamClaim)
			}
		case "apply":
		default:
			e.t.Errorf("write %q: the verbs of a reset on the cluster are scale, delete and apply", w)
		}
		for _, banned := range []string{"nexus-data", "deployment/nexus ", "rollout", "exec", "pod"} {
			if strings.Contains(w, banned) {
				e.t.Errorf("write %q contains %q: the reset touches the upstream and its claim only", w, banned)
			}
		}
	}
	claim, err := renderUpstreamClaim(e.p)
	if err != nil {
		e.t.Fatalf("renderUpstreamClaim: %v", err)
	}
	for _, doc := range e.kube.applies {
		if !bytes.Equal(doc, claim) {
			e.t.Errorf("the applied stream is not the claim alone:\n%s", doc)
		}
	}
}

// requireQuiet fails when a password is in what Reset said, in its error, or in
// anything a process was given.
func (e *resetEnv) requireQuiet(err error) {
	e.t.Helper()
	texts := map[string]string{"output": e.out.String(), "kubectl": strings.Join(e.kube.lines, "\n")}
	var stdin []string
	for _, doc := range e.kube.applies {
		stdin = append(stdin, string(doc))
	}
	for _, doc := range e.kube.deleteBodies {
		stdin = append(stdin, string(doc))
	}
	texts["kubectl stdin"] = strings.Join(stdin, "\n")
	if err != nil {
		texts["error"] = err.Error()
	}
	var argv []string
	for _, c := range e.cp.copies {
		argv = append(argv, imageArg(c.src), imageArg(c.dst))
	}
	for _, c := range e.cp.digests {
		argv = append(argv, imageArg(c.ref))
	}
	for _, f := range e.fwd.calls {
		argv = append(argv, f.namespace, f.service, fmt.Sprint(f.port))
	}
	texts["argv"] = strings.Join(argv, "\n")
	for label, text := range texts {
		for _, secret := range e.secrets() {
			if secret != "" && strings.Contains(text, secret) {
				e.t.Errorf("the %s contains a password (%d characters): %q", label, len(secret), text)
			}
		}
	}
}

// requireNothingWritten checks the run left the bench as it found it: no kubectl
// write, no REST write, no registry touched.
func (e *resetEnv) requireNothingWritten() {
	e.t.Helper()
	if len(e.kube.writes) != 0 {
		e.t.Errorf("the cluster was written: %q", e.kube.writes)
	}
	for _, r := range e.reqs() {
		if r.Method != http.MethodGet {
			e.t.Errorf("%s %s: nothing may be written before the reset is sure of what it deletes", r.Method, r.Path)
		}
	}
	if len(e.fwd.calls) != 0 || len(e.cp.copies) != 0 || len(e.cp.digests) != 0 {
		e.t.Errorf("a registry was used: %d forwards, %d copies, %d reads", len(e.fwd.calls), len(e.cp.copies), len(e.cp.digests))
	}
}

// requirePreflight checks Reset asked the questions of OpReset, and only them.
func (e *resetEnv) requirePreflight() {
	e.t.Helper()
	if got, want := asked(e.kube.fakeKubectl), inNamespace(e.p, questionsOf(OpReset)...); !slices.Equal(got, want) {
		e.t.Errorf("the preflight asked\n  %q\nwant OpReset\n  %q", got, want)
	}
}

// compSet is what a listing says, one entry per component, in a stable order.
func compSet(comps []fakeComponent) []string {
	out := make([]string, 0, len(comps))
	for _, c := range comps {
		out = append(out, c.Name+":"+c.Version+"@"+c.Digest)
	}
	slices.Sort(out)
	return out
}

// wantList is what a repository lists once the data set is in place.
func wantList(keep func(DatasetImage) bool) []string {
	var comps []fakeComponent
	for _, img := range Dataset() {
		if keep(img) {
			comps = append(comps, fakeComponent{Name: img.Name, Version: img.Tag, Digest: img.Digest})
		}
	}
	return compSet(comps)
}

// benchState is everything a reset must bring back, but the passwords.
type benchState struct {
	Nexus                   string
	HostedList, ProxyList   []string
	Hosted, Proxy, Upstream map[string]string
}

func (e *resetEnv) state() benchState {
	return benchState{
		Nexus:      e.b.nexus.Snapshot(),
		HostedList: compSet(e.b.nexus.Components(dockerHostedRepository)),
		ProxyList:  compSet(e.b.nexus.Components(dockerProxyRepository)),
		Hosted:     maps.Clone(e.cp.hosted),
		Proxy:      maps.Clone(e.cp.proxyCache),
		Upstream:   maps.Clone(e.cp.upstream),
	}
}

// passwords are the accounts of the bench as the store and the instance hold them.
func (e *resetEnv) passwords() [4]string {
	pull, _ := e.b.nexus.User(pullUserID)
	sec := e.b.store.current()
	return [4]string{sec.AdminPassword, sec.PullPassword, e.b.nexus.AdminPassword(), pull.Password}
}

func requireSameState(t *testing.T, got, want benchState) {
	t.Helper()
	if got.Nexus != want.Nexus {
		t.Errorf("the instance is not in its initial state:\n got %s\nwant %s", got.Nexus, want.Nexus)
	}
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"docker-hosted lists", got.HostedList, want.HostedList},
		{"docker-proxy lists", got.ProxyList, want.ProxyList},
		{"docker-hosted holds", got.Hosted, want.Hosted},
		{"docker-proxy caches", got.Proxy, want.Proxy},
		{"the upstream holds", got.Upstream, want.Upstream},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s %v, want %v", c.name, c.got, c.want)
		}
	}
}

func requireAsIs(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var be *BenchError
	if errors.As(err, &be) {
		t.Errorf("a cancelled context is a BenchError (%v), want it as is", be)
	}
}

func requireNotHalfWay(t *testing.T, err error) {
	t.Helper()
	var be *BenchError
	if errors.As(err, &be) && strings.Contains(be.Check, "half-way") {
		t.Errorf("Check %q says the reset stopped half-way, but nothing was changed", be.Check)
	}
}

// ---- the nominal path -----------------------------------------------------------

func TestResetBringsTheBenchBackToItsInitialState(t *testing.T) {
	e := newSeededResetEnv(t)
	wantProv := e.b.mustRun() // what Provision says of this bench
	initial := e.state()
	pwBefore := e.passwords()
	ssrfBefore := e.b.nexus.SSRF()
	anonymousBefore := e.b.nexus.Anonymous()
	e.pollute()
	if reflect.DeepEqual(e.state(), initial) {
		t.Fatal("the pollution changed nothing")
	}

	prov, seed := e.mustReset()

	if prov != wantProv {
		t.Errorf("ProvisionResult = %+v, want what Provision says of this bench: %+v", prov, wantProv)
	}
	if want := (SeedResult{Images: Dataset()}); !reflect.DeepEqual(seed, want) {
		t.Errorf("SeedResult = %+v, want the data set %+v", seed, want)
	}
	requireSameState(t, e.state(), initial)
	if got, want := compSet(e.b.nexus.Components(dockerHostedRepository)), wantList(isHosted); !slices.Equal(got, want) {
		t.Errorf("docker-hosted lists %q, want the hosted images of the data set %q", got, want)
	}
	if got, want := compSet(e.b.nexus.Components(dockerProxyRepository)), wantList(isPullThrough); !slices.Equal(got, want) {
		t.Errorf("docker-proxy lists %q, want the PullThrough images of the data set %q", got, want)
	}
	for _, spec := range repositorySpecs(e.p) {
		if got := requireRepo(t, e.b.nexus, spec.kind, spec.name); !reflect.DeepEqual(got, deepCopy(spec.config)) {
			t.Errorf("repository %s is %v, want the configuration of repositorySpecs %v", spec.name, got, deepCopy(spec.config))
		}
	}
	if got := e.passwords(); got != pwBefore {
		t.Error("a password changed: the admin and bench-pull passwords of a reset are the ones of the bench")
	}
	if got := e.b.nexus.SSRF(); !reflect.DeepEqual(got, ssrfBefore) {
		t.Errorf("SSRF protection = %+v, want it untouched: %+v", got, ssrfBefore)
	}
	if got := countRequests(e.reqs(), http.MethodPut, pathSSRF); got != 0 {
		t.Errorf("%d writes of the SSRF protection: a reset leaves it alone", got)
	}
	if got := e.b.nexus.Anonymous(); !reflect.DeepEqual(got, anonymousBefore) {
		t.Errorf("anonymous access = %v, want %v (the current parameters did not change)", got, anonymousBefore)
	}
	deletes := 0
	for _, r := range e.reqs() {
		if r.Method == http.MethodDelete {
			deletes++
		}
	}
	if deletes != 3 {
		t.Errorf("%d DELETE requests, want one per repository", deletes)
	}
	// The cluster: the four writes of the recycling, in this order, and nothing else.
	if got, want := e.kube.ops, []string{"scale 0", "delete", "apply", "scale 1"}; !slices.Equal(got, want) {
		t.Errorf("writes on the cluster = %q, want %q", got, want)
	}
	if e.kube.generation != 2 {
		t.Errorf("the claim was created %d times, want once", e.kube.generation-1)
	}
	if !strings.HasPrefix(e.out.String(), "reset: ") && !strings.Contains(e.out.String(), "\nreset: ") {
		t.Errorf("Reset printed no \"reset: \" line:\n%s", e.out.String())
	}
}

func TestResetLeavesAFreshSeedAsItIs(t *testing.T) {
	e := newSeededResetEnv(t)
	initial := e.state()
	pwBefore := e.passwords()
	_, seed := e.mustReset()
	requireSameState(t, e.state(), initial)
	if got := e.passwords(); got != pwBefore {
		t.Error("a password changed")
	}
	// The result is the one a first seed gives on an empty bench.
	empty := newResetEnv(t)
	first, err := Seed(context.Background(), empty.deps, empty.baseURL, empty.p, io.Discard)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if !reflect.DeepEqual(seed, first) {
		t.Errorf("SeedResult = %+v, want the one of a first seed %+v", seed, first)
	}
}

func TestResetIsIdempotent(t *testing.T) {
	e := newSeededResetEnv(t)
	e.pollute()
	e.mustReset()
	once := e.state()
	e.mustReset()
	requireSameState(t, e.state(), once)
}

func TestResetOrderOfTheSteps(t *testing.T) {
	e := newSeededResetEnv(t)
	e.pollute()
	e.mustReset()
	ev := e.events()
	find := func(prefix string) int {
		t.Helper()
		i := indexOfEvent(ev, prefix)
		if i < 0 {
			t.Fatalf("no journal entry starts with %q:\n%s", prefix, strings.Join(ev, "\n"))
		}
		return i
	}
	lastAuth := lastIndexOfEvent(ev, "kube auth")
	if lastAuth < 0 {
		t.Fatalf("the preflight asked nothing:\n%s", strings.Join(ev, "\n"))
	}
	if first := ev[0]; !strings.HasPrefix(first, "kube ") {
		t.Errorf("the first thing Reset does is %q, want a question of the preflight", first)
	}
	for i, entry := range ev[:lastAuth] {
		if strings.HasPrefix(entry, "HTTP ") || strings.HasPrefix(entry, "kube scale") || entry == "kube delete" || entry == "kube apply" {
			t.Errorf("entry #%d %q comes before the end of the preflight", i, entry)
		}
	}
	lastPost := -1
	for _, kind := range []string{"hosted", "proxy", "group"} {
		path := repoPath(kind, "")
		if n := e.b.nexus.Count(http.MethodPost, path) - e.baseCountPost(kind); n != 1 {
			t.Errorf("POST %s sent %d times by the run, want once: the repository was deleted, Provision creates it", path, n)
		}
		lastPost = max(lastPost, lastIndexOfEvent(ev, "HTTP POST "+path))
	}
	steps := []struct {
		name string
		at   int
	}{
		{"the last question of the preflight", lastAuth},
		{"the check of the admin account", find("HTTP GET " + pathUsers)},
		{"the DELETE of docker-group", find("HTTP DELETE " + deletePath(dockerGroupRepository))},
		{"the DELETE of docker-proxy", find("HTTP DELETE " + deletePath(dockerProxyRepository))},
		{"the DELETE of docker-hosted", find("HTTP DELETE " + deletePath(dockerHostedRepository))},
		{"the scale to 0", find("kube scale 0")},
		{"the delete of the claim", find("kube delete")},
		{"the apply of the claim", find("kube apply")},
		{"the scale to 1", find("kube scale 1")},
		{"the first request of Provision", find("HTTP GET " + pathStatus)},
		{"the last repository Provision creates", lastPost},
	}
	for i := 1; i < len(steps); i++ {
		if steps[i-1].at >= steps[i].at {
			t.Errorf("%s (entry #%d) must come before %s (entry #%d)\n%s", steps[i-1].name, steps[i-1].at, steps[i].name, steps[i].at, strings.Join(ev, "\n"))
		}
	}
	// Before the first write of any kind: the admin account is checked, and the claim
	// is read by name (its label is checked) so that a refusal leaves the bench whole.
	firstWrite := min(find("HTTP DELETE "), find("kube scale"))
	if admin := find("HTTP GET " + pathUsers); admin > firstWrite {
		t.Errorf("the admin account is checked at #%d, after the first write at #%d", admin, firstWrite)
	}
	if claim := find("kube get pvc " + upstreamClaim); claim > firstWrite {
		t.Errorf("the claim is read at #%d, after the first write at #%d", claim, firstWrite)
	}
	// The repositories are read empty after Provision and before the seed opens its
	// port-forward, and nothing waits when they are empty at once.
	forward, copied := find("forward"), find("copy ")
	if lastPost >= forward || forward >= copied {
		t.Errorf("Provision (#%d), the port-forward (#%d) and the first copy (#%d) are out of order", lastPost, forward, copied)
	}
	httpBefore := 0
	for _, entry := range ev[:forward] {
		if strings.HasPrefix(entry, "HTTP ") {
			httpBefore++
		}
	}
	seen := map[string]bool{}
	for _, r := range e.reqs()[:httpBefore] {
		if r.Path == pathComponents {
			seen[strings.TrimPrefix(r.Query, "repository=")] = true
		}
	}
	for _, repo := range []string{dockerHostedRepository, dockerProxyRepository} {
		if !seen[repo] {
			t.Errorf("%s was not read before the seed started: Reset must see it empty first (H-REPODEL)", repo)
		}
	}
	for _, entry := range ev[lastPost:forward] {
		if strings.HasPrefix(entry, "sleep ") {
			t.Errorf("Reset slept (%s) between Provision and the seed although the repositories were empty at once", entry)
		}
	}
}

// countRequests is how many of reqs are a request for method and path.
func countRequests(reqs []fakeReq, method, path string) int {
	n := 0
	for _, r := range reqs {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// baseCountPost is the POSTs to the creation path of a kind before the current run.
func (e *resetEnv) baseCountPost(kind string) int {
	n := 0
	for _, r := range e.b.nexus.Requests()[:e.runReq] {
		if r.Method == http.MethodPost && r.Path == repoPath(kind, "") {
			n++
		}
	}
	return n
}

func TestResetOnlyWritesTheUpstreamAndItsClaimOnTheCluster(t *testing.T) {
	e := newSeededResetEnv(t)
	e.pollute()
	e.mustReset()
	if len(e.kube.writes) != 4 {
		t.Fatalf("writes = %q, want the four of the recycling", e.kube.writes)
	}
	if len(e.kube.applies) != 1 {
		t.Fatalf("%d applies, want one", len(e.kube.applies))
	}
	full, err := Render(e.p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if bytes.Equal(e.kube.applies[0], full) || strings.Contains(string(e.kube.applies[0]), "kind: Deployment") {
		t.Error("the full stream of manifests was applied: it would roll the pod of Nexus when the parameters differ from the bench's")
	}
	for _, s := range e.kube.scales {
		if s.resource != "deployment/"+upstreamName || s.namespace != e.p.Namespace {
			t.Errorf("scale %+v, want deployment/%s in %s", s, upstreamName, e.p.Namespace)
		}
	}
	// No claim, Deployment or pod of Nexus is read either: the only claim read by name is
	// the one of the upstream.
	for _, line := range e.kube.lines {
		if strings.HasPrefix(line, "get pvc "+deployPVCName) || strings.HasPrefix(line, "get deployment nexus") {
			t.Errorf("call %q: a reset has nothing to read about Nexus", line)
		}
	}
}

func TestResetDeletesTheClaimOfTheUIDItRead(t *testing.T) {
	e := newSeededResetEnv(t)
	e.mustReset()
	if len(e.kube.deleteUIDs) != 1 {
		t.Fatalf("%d deletes of the claim, want one (never retried)", len(e.kube.deleteUIDs))
	}
	uid := e.kube.deleteUIDs[0]
	if want := (&resetKubectl{generation: 1}).claimUID(); uid != want {
		t.Errorf("the delete carries the UID %q, want the UID of the claim of the bench %q", uid, want)
	}
	if got := e.kube.readBefore[0]; got != uid {
		t.Errorf("the delete carries the UID %q, the last read before it answered %q", uid, got)
	}
	if got := e.kube.liveUIDs[0]; got != uid {
		t.Errorf("the delete carries the UID %q, the claim has %q", uid, got)
	}
	if len(e.kube.uidsRead) == 0 {
		t.Error("the claim was never read")
	}
}

func TestResetDoesNotDeleteAClaimReplacedAfterTheRead(t *testing.T) {
	e := newSeededResetEnv(t)
	e.pollute()
	e.kube.staleUID = true
	_, _, err := e.run()
	be := requireBenchError(t, err)
	if !strings.HasSuffix(be.Check, halfWayCheck) {
		t.Errorf("Check = %q, want it to end with %q: the repositories were deleted already", be.Check, halfWayCheck)
	}
	if n := len(e.kube.deleteUIDs); n != 1 {
		t.Errorf("%d deletes of the claim, want one: a Conflict is not retried", n)
	}
	if !e.kube.claim.exists || e.kube.claim.terminating {
		t.Error("the claim was deleted although its UID was not the one that was read")
	}
	if len(e.kube.applies) != 0 {
		t.Errorf("%d applies after a refused delete", len(e.kube.applies))
	}
}

func TestResetNeverReadsThePodNorChangesAPassword(t *testing.T) {
	e := newSeededResetEnv(t)
	e.pollute()
	pwBefore := e.passwords()
	e.mustReset()
	if got := e.passwords(); got != pwBefore {
		t.Error("a password changed")
	}
	if n := countRequests(e.reqs(), http.MethodPut, pathAdminPassword); n != 0 {
		t.Errorf("%d changes of the admin password", n)
	}
	for _, q := range asked(e.kube.fakeKubectl) {
		if strings.Contains(q, "exec") {
			t.Errorf("the preflight asked %q", q)
		}
	}
}

func TestResetAppliesTheCurrentParameters(t *testing.T) {
	// Hypothesis, to settle by the code and the doc: the provisioning of a reset is
	// the one of "provision", so a reset with another parameter changes it.
	e := newSeededResetEnv(t)
	if enabled, _ := e.b.nexus.Anonymous()["enabled"].(bool); !enabled {
		t.Fatal("the bench starts with anonymous access on")
	}
	e.p.Anonymous = false
	e.mustReset()
	if enabled, _ := e.b.nexus.Anonymous()["enabled"].(bool); enabled {
		t.Error("anonymous access is still enabled after a reset with NEXUS_BENCH_ANONYMOUS=false")
	}
}

func TestResetAcceptsANilWriter(t *testing.T) {
	e := newSeededResetEnv(t)
	e.nilOut = true
	e.mustReset()
}

func TestResetNeverPrintsAPassword(t *testing.T) {
	e := newSeededResetEnv(t)
	e.pollute()
	e.mustReset()
	if e.out.Len() == 0 {
		t.Fatal("Reset said nothing")
	}
	for _, secret := range e.secrets() {
		if strings.Contains(e.out.String(), secret) {
			t.Errorf("the output contains a password")
		}
	}
}

// ---- before the first write ------------------------------------------------------

func TestResetAsksTheQuestionsOfOpResetItself(t *testing.T) {
	e := newSeededResetEnv(t)
	e.mustReset()
	e.requirePreflight()
}

func TestResetRefusesBeforeItWritesAnything(t *testing.T) {
	loopbackHint := "loopback"
	tests := []struct {
		name    string
		setup   func(e *resetEnv)
		kind    ErrKind
		message []string // substrings of the error
	}{
		{
			name:    "a REST origin that is not the loopback",
			setup:   func(e *resetEnv) { e.baseURL = "http://192.0.2.1:30481" },
			kind:    Unclassified,
			message: []string{loopbackHint},
		},
		{
			name:    "a REST origin whose user information is the loopback",
			setup:   func(e *resetEnv) { e.baseURL = "http://127.0.0.1@evil.invalid:30481" },
			kind:    Unclassified,
			message: []string{loopbackHint},
		},
		{
			name:    "a REST origin whose name only starts like the loopback",
			setup:   func(e *resetEnv) { e.baseURL = "http://127.0.0.1.evil.invalid:30481" },
			kind:    Unclassified,
			message: []string{loopbackHint},
		},
		{
			name:    "an upstream the bench did not deploy",
			setup:   func(e *resetEnv) { e.p.UpstreamURL = "http://registry.example.invalid:5000" },
			kind:    Unclassified,
			message: []string{envUpstreamURL},
		},
		{
			name:    "no stored credentials",
			setup:   func(e *resetEnv) { _ = e.b.store.Remove() },
			kind:    ProvisionFailed,
			message: []string{"admin password"},
		},
		{
			name: "an admin password the instance refuses",
			setup: func(e *resetEnv) {
				e.b.store.preset(Secrets{AdminPassword: "not-the-admin-password", PullPassword: e.b.store.current().PullPassword})
			},
			kind:    ProvisionFailed,
			message: []string{"nothing was changed"},
		},
		{
			name:    "an instance that fails the check of the admin account",
			setup:   func(e *resetEnv) { e.b.nexus.Forbid(http.MethodGet, pathUsers, http.StatusInternalServerError) },
			kind:    ProvisionFailed,
			message: []string{"Nexus refused a request"},
		},
		{
			name:    "a claim the bench did not create",
			setup:   func(e *resetEnv) { e.kube.claim.labels = map[string]string{"app.kubernetes.io/name": upstreamName} },
			kind:    Unclassified,
			message: []string{upstreamClaim, managedByLabel},
		},
		{
			name: "a claim that cannot be read",
			setup: func(e *resetEnv) {
				e.kube.claimReply = &fakeReply{stderr: "Error from server (Forbidden): persistentvolumeclaims \"bench-upstream-data\" is forbidden token=" + fakeSecret, err: errFakeExit1}
			},
			kind:    Forbidden,
			message: []string{upstreamClaim},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeededResetEnv(t)
			e.pollute()
			before := e.state()
			tt.setup(e)
			_, _, err := e.run()
			be := requireBenchError(t, err)
			if be.Kind != tt.kind {
				t.Errorf("Kind = %d, want %d; error: %v", be.Kind, tt.kind, err)
			}
			for _, want := range tt.message {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			requireNotHalfWay(t, err)
			e.requirePreflight()
			e.requireNothingWritten()
			requireSameState(t, e.state(), before)
		})
	}
}

func TestResetRefusesACustomUpstreamAndAnAbsentSecretBeforeItTouchesNexus(t *testing.T) {
	// The two refusals that need no request: Nexus never hears from the run.
	for name, setup := range map[string]func(e *resetEnv){
		"custom upstream":    func(e *resetEnv) { e.p.UpstreamURL = "http://registry.example.invalid:5000" },
		"no stored secrets":  func(e *resetEnv) { _ = e.b.store.Remove() },
		"origin not on lo":   func(e *resetEnv) { e.baseURL = "http://192.0.2.1:30481" },
		"origin with a user": func(e *resetEnv) { e.baseURL = "http://127.0.0.1@evil.invalid:30481" },
	} {
		t.Run(name, func(t *testing.T) {
			e := newSeededResetEnv(t)
			setup(e)
			if _, _, err := e.run(); err == nil {
				t.Fatal("Reset succeeded")
			}
			if n := len(e.reqs()); n != 0 {
				t.Errorf("%d requests to Nexus: %q", n, calls(e.reqs()))
			}
		})
	}
}

func TestResetStopsAtTheFirstRefusedPermission(t *testing.T) {
	for _, question := range questionsOf(OpReset) {
		t.Run(question, func(t *testing.T) {
			e := newSeededResetEnv(t)
			verb, resource := denialOf(question)
			e.kube.deny(verb, resource)
			_, _, err := e.run()
			if be := requireBenchError(t, err); be.Kind != Forbidden {
				t.Errorf("Kind = %d, want Forbidden; error: %v", be.Kind, err)
			}
			requireNotHalfWay(t, err)
			e.requireNothingWritten()
			for _, entry := range e.events() {
				for _, banned := range []string{"HTTP ", "kube scale", "kube delete", "kube apply", "kube get deployment", "kube get pods", "kube get pvc", "forward", "copy", "digest"} {
					if strings.HasPrefix(entry, banned) {
						t.Errorf("%q after a refused permission", entry)
					}
				}
			}
		})
	}
}

func TestResetStopsAtTheNamespace(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *resetEnv)
		kind  ErrKind
	}{
		{"absent", func(e *resetEnv) { e.kube.nsPresent = false }, NamespaceMissing},
		{"not created by the bench", func(e *resetEnv) {
			e.kube.withNamespace(map[string]string{"kubernetes.io/metadata.name": e.p.Namespace})
		}, NamespaceForeign},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeededResetEnv(t)
			tt.setup(e)
			_, _, err := e.run()
			if be := requireBenchError(t, err); be.Kind != tt.kind {
				t.Errorf("Kind = %d, want %d; error: %v", be.Kind, tt.kind, err)
			}
			e.requireNothingWritten()
			if n := len(e.reqs()); n != 0 {
				t.Errorf("%d requests to Nexus", n)
			}
		})
	}
}

func TestResetRunsThePreflightBeforeEveryOtherCheck(t *testing.T) {
	// Everything is wrong at once: the preflight is the one that speaks.
	e := newSeededResetEnv(t)
	e.kube.deny("delete", "persistentvolumeclaims")
	e.baseURL = "http://192.0.2.1:30481"
	e.p.UpstreamURL = "http://registry.example.invalid:5000"
	_ = e.b.store.Remove()
	_, _, err := e.run()
	if be := requireBenchError(t, err); be.Kind != Forbidden {
		t.Errorf("Kind = %d, want Forbidden (the preflight): %v", be.Kind, err)
	}
	e.requireNothingWritten()
}

func TestResetAClaimThatIsAbsentIsNotDeleted(t *testing.T) {
	e := newSeededResetEnv(t)
	e.pollute()
	e.kube.claim = claimState{}
	e.mustReset()
	if len(e.kube.deleteUIDs) != 0 {
		t.Errorf("%d deletes of a claim that is not there", len(e.kube.deleteUIDs))
	}
	if got, want := e.kube.ops, []string{"scale 0", "apply", "scale 1"}; !slices.Equal(got, want) {
		t.Errorf("writes = %q, want %q", got, want)
	}
	if got, want := compSet(e.b.nexus.Components(dockerHostedRepository)), wantList(isHosted); !slices.Equal(got, want) {
		t.Errorf("docker-hosted lists %q, want %q", got, want)
	}
}

// ---- the repositories that empty late (H-REPODEL) -----------------------------------

func TestResetWaitsForTheRepositoriesToBeEmpty(t *testing.T) {
	e := newSeededResetEnv(t)
	initial := e.state()
	e.pollute()
	e.b.nexus.KeepComponentsAfterDelete(2)
	e.mustReset()
	requireSameState(t, e.state(), initial)

	ev := e.events()
	lastPost := -1
	for _, kind := range []string{"hosted", "proxy", "group"} {
		lastPost = max(lastPost, lastIndexOfEvent(ev, "HTTP POST "+repoPath(kind, "")))
	}
	forward := indexOfEvent(ev, "forward")
	if lastPost < 0 || forward < lastPost {
		t.Fatalf("Provision (#%d) and the port-forward (#%d) are out of order", lastPost, forward)
	}
	sleeps := 0
	for _, entry := range ev[lastPost:forward] {
		if strings.HasPrefix(entry, "sleep ") {
			sleeps++
		}
	}
	if sleeps < 2 {
		t.Errorf("%d sleeps between Provision and the seed, want it to wait while the repositories still list their old components", sleeps)
	}
	httpBefore := 0
	for _, entry := range ev[:forward] {
		if strings.HasPrefix(entry, "HTTP ") {
			httpBefore++
		}
	}
	read := map[string]int{}
	for _, r := range e.reqs()[:httpBefore] {
		if r.Path == pathComponents {
			read[strings.TrimPrefix(r.Query, "repository=")]++
		}
	}
	for _, repo := range []string{dockerHostedRepository, dockerProxyRepository} {
		if read[repo] < 3 {
			t.Errorf("%s read %d times before the seed, want at least 3 (two listings of the old components, then an empty one)", repo, read[repo])
		}
	}
}

func TestResetGivesUpWhenTheRepositoriesNeverEmpty(t *testing.T) {
	e := newSeededResetEnv(t)
	e.pollute()
	e.b.nexus.KeepComponentsAfterDelete(100000)
	_, _, err := e.run()
	be := requireBenchError(t, err)
	if be.Kind != SeedFailed {
		t.Errorf("Kind = %d, want SeedFailed; error: %v", be.Kind, err)
	}
	// Running reset again would delete the same repositories for the same result: the
	// message sends to the log of the pod of Nexus instead.
	if strings.Contains(be.Check, "nexus-bench:reset") {
		t.Errorf("Check = %q: it must not suggest to run reset again, the repositories did not empty", be.Check)
	}
	if !strings.Contains(be.Check, podsCommand(e.p)) && !strings.Contains(be.Check, "logs") {
		t.Errorf("Check = %q, want the way to read the log of the Nexus pod", be.Check)
	}
	if len(e.fwd.calls) != 0 || len(e.cp.copies) != 0 {
		t.Errorf("the seed started (%d forwards, %d copies) on repositories that are not empty", len(e.fwd.calls), len(e.cp.copies))
	}
	var waited time.Duration
	lastPost := -1
	ev := e.events()
	for _, kind := range []string{"hosted", "proxy", "group"} {
		lastPost = max(lastPost, lastIndexOfEvent(ev, "HTTP POST "+repoPath(kind, "")))
	}
	for _, entry := range ev[lastPost:] {
		if s, ok := strings.CutPrefix(entry, "sleep "); ok {
			d, err := time.ParseDuration(s)
			if err != nil {
				t.Fatalf("journal entry %q: %v", entry, err)
			}
			waited += d
		}
	}
	// Bound: seedListWait, the one the seed gives a listing (or the value measured at
	// step 9), once per repository at most.
	if waited < seedListWait/2 || waited > 2*seedListWait+2*seedListInterval {
		t.Errorf("waited %v for the repositories to empty, want about %v", waited, seedListWait)
	}
	if n := e.b.nexus.Count(http.MethodDelete, deletePath(dockerHostedRepository)); n != 1 {
		t.Errorf("docker-hosted deleted %d times, want once: the wait does not delete again", n)
	}
}

func TestWaitRepositoriesEmpty(t *testing.T) {
	recreate := func(t *testing.T, b *bench) {
		t.Helper()
		c := b.admin()
		ctx := context.Background()
		for _, name := range benchRepositories {
			if _, err := c.deleteRepository(ctx, name); err != nil {
				t.Fatalf("deleteRepository(%s): %v", name, err)
			}
		}
		b.mustRun()
	}
	t.Run("repositories that are empty are not waited for", func(t *testing.T) {
		b := provisionedBench(t)
		clk := newFakeClock(t)
		if err := waitRepositoriesEmpty(context.Background(), b.admin(), clk, dockerHostedRepository, dockerProxyRepository); err != nil {
			t.Fatalf("waitRepositoriesEmpty: %v", err)
		}
		if len(clk.sleeps) != 0 {
			t.Errorf("slept %v for empty repositories", clk.sleeps)
		}
	})
	t.Run("components that stay for two listings", func(t *testing.T) {
		b := provisionedBench(t)
		b.addImages()
		b.nexus.KeepComponentsAfterDelete(2)
		recreate(t, b)
		clk := newFakeClock(t)
		if err := waitRepositoriesEmpty(context.Background(), b.admin(), clk, dockerHostedRepository, dockerProxyRepository); err != nil {
			t.Fatalf("waitRepositoriesEmpty: %v", err)
		}
		if len(clk.sleeps) < 2 {
			t.Errorf("slept %v, want at least two waits", clk.sleeps)
		}
		if clk.elapsed() > seedListWait {
			t.Errorf("waited %v for what empties in two listings", clk.elapsed())
		}
		for _, repo := range []string{dockerHostedRepository, dockerProxyRepository} {
			if n := b.nexus.ComponentCount(repo); n != 0 {
				t.Errorf("%s still holds %d components", repo, n)
			}
		}
	})
	t.Run("components that never go", func(t *testing.T) {
		b := provisionedBench(t)
		b.addImages()
		b.nexus.KeepComponentsAfterDelete(100000)
		recreate(t, b)
		clk := newFakeClock(t)
		err := waitRepositoriesEmpty(context.Background(), b.admin(), clk, dockerHostedRepository, dockerProxyRepository)
		be := requireBenchError(t, err)
		if be.Kind != SeedFailed {
			t.Errorf("Kind = %d, want SeedFailed; error: %v", be.Kind, err)
		}
		if clk.elapsed() < seedListWait/2 || clk.elapsed() > 2*seedListWait+2*seedListInterval {
			t.Errorf("gave up after %v, want about %v", clk.elapsed(), seedListWait)
		}
		b.requireQuiet(err)
	})
	t.Run("a listing that fails", func(t *testing.T) {
		b := provisionedBench(t)
		b.nexus.Forbid(http.MethodGet, pathComponents, http.StatusInternalServerError)
		clk := newFakeClock(t)
		err := waitRepositoriesEmpty(context.Background(), b.admin(), clk, dockerHostedRepository)
		if err == nil {
			t.Fatal("waitRepositoriesEmpty succeeded")
		}
		if len(clk.sleeps) != 0 {
			t.Errorf("slept %v after a failed listing", clk.sleeps)
		}
		b.requireQuiet(err)
	})
	t.Run("a cancelled context is returned as is", func(t *testing.T) {
		b := provisionedBench(t)
		b.addImages()
		b.nexus.KeepComponentsAfterDelete(100000)
		recreate(t, b)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		clk := newFakeClock(t)
		clk.onSleep = func(int) { cancel() }
		requireAsIs(t, waitRepositoriesEmpty(ctx, b.admin(), clk, dockerHostedRepository, dockerProxyRepository))
	})
}

// ---- storedPasswordOnly ---------------------------------------------------------------

func TestStoredPasswordOnlyNeverReadsThePod(t *testing.T) {
	var ip InitialPassword = storedPasswordOnly{}
	password, ok, err := ip.Read(context.Background())
	if password != "" || ok || err != nil {
		t.Errorf("Read = (%q, %v, %v), want the pod to have no initial password, without an error", password, ok, err)
	}
}

func TestProvisionWithStoredPasswordOnlyNeverChangesTheAdminPassword(t *testing.T) {
	t.Run("a stored password the instance refuses", func(t *testing.T) {
		b := provisionedBench(t)
		b.nexus.SetAdminPassword("another-admin-password")
		before := b.nexus.Snapshot()
		start := len(b.nexus.Requests())
		_, err := Provision(context.Background(), b.nexus.URL(), storedPasswordOnly{}, b.store, b.p, io.Discard)
		be := requireBenchError(t, err)
		if be.Kind != ProvisionFailed {
			t.Errorf("Kind = %d, want ProvisionFailed; error: %v", be.Kind, err)
		}
		for _, r := range b.nexus.Requests()[start:] {
			if r.Method != http.MethodGet {
				t.Errorf("%s %s: the instance was written although the admin password is unknown", r.Method, r.Path)
			}
		}
		if got := b.nexus.Snapshot(); got != before {
			t.Error("the instance changed")
		}
		if b.nexus.AdminPassword() != "another-admin-password" {
			t.Error("the admin password of the instance changed")
		}
		b.requireQuiet(err)
	})
	t.Run("a stored password the instance accepts", func(t *testing.T) {
		b := provisionedBench(t)
		pw := b.nexus.AdminPassword()
		start := len(b.nexus.Requests())
		res, err := Provision(context.Background(), b.nexus.URL(), storedPasswordOnly{}, b.store, b.p, io.Discard)
		if err != nil {
			t.Fatalf("Provision: %v", err)
		}
		if res.NexusVersion == "" || b.nexus.AdminPassword() != pw {
			t.Errorf("result %+v, admin password changed: %v", res, b.nexus.AdminPassword() != pw)
		}
		if n := countRequests(b.nexus.Requests()[start:], http.MethodPut, pathAdminPassword); n != 0 {
			t.Errorf("%d changes of the admin password", n)
		}
	})
}

// ---- interruptions ---------------------------------------------------------------------

func TestResetResumesAfterAnInterruption(t *testing.T) {
	refused := func(what string) *fakeReply {
		return &fakeReply{stderr: "Error from server (Forbidden): " + what + " is forbidden token=" + fakeSecret, err: errFakeExit1}
	}
	tests := []struct {
		name   string
		inject func(e *resetEnv)
		heal   func(e *resetEnv)
		kind   ErrKind
		cause  string // what the error says about its cause, kept as it was
	}{
		{
			name:   "the DELETE of docker-proxy is refused",
			inject: func(e *resetEnv) { e.b.nexus.Forbid(http.MethodDelete, deletePath(dockerProxyRepository), 500) },
			heal:   func(e *resetEnv) { e.b.nexus.ClearFaults() },
			kind:   ProvisionFailed, cause: "Nexus refused a request",
		},
		{
			name:   "the DELETE of docker-hosted is refused",
			inject: func(e *resetEnv) { e.b.nexus.Forbid(http.MethodDelete, deletePath(dockerHostedRepository), 500) },
			heal:   func(e *resetEnv) { e.b.nexus.ClearFaults() },
			kind:   ProvisionFailed, cause: "Nexus refused a request",
		},
		{
			name:   "kubectl is refused the delete of the claim",
			inject: func(e *resetEnv) { e.kube.deleteReply = refused("persistentvolumeclaims") },
			heal:   func(e *resetEnv) { e.kube.deleteReply = nil },
			kind:   Forbidden, cause: upstreamClaim,
		},
		{
			name: "an admission webhook denies the claim",
			inject: func(e *resetEnv) {
				e.kube.applyReply = &fakeReply{stderr: "Error from server: admission webhook \"policy.example\" denied the request token=" + fakeSecret, err: errFakeExit1}
			},
			heal: func(e *resetEnv) { e.kube.applyReply = nil },
			kind: AdmissionDenied, cause: "claim",
		},
		{
			name:   "the upstream cannot be scaled up again",
			inject: func(e *resetEnv) { e.kube.scaleFailsFor = "1" },
			heal:   func(e *resetEnv) { e.kube.scaleFailsFor = "none" },
			kind:   Forbidden,
		},
		{
			name:   "Provision cannot create the group",
			inject: func(e *resetEnv) { e.b.nexus.Forbid(http.MethodPost, repoPath("group", ""), 500) },
			heal:   func(e *resetEnv) { e.b.nexus.ClearFaults() },
			kind:   ProvisionFailed, cause: "Nexus refused a request",
		},
		{
			name: "a copy of the seed is refused",
			inject: func(e *resetEnv) {
				e.cp.copyErr = func(int, copyCall) error {
					return &BenchError{Kind: RegistryRefused, Cause: "the registry refused the copy", Check: "mise run nexus-bench:status", Err: errors.New("refused")}
				}
			},
			heal: func(e *resetEnv) { e.cp.copyErr = nil },
			kind: RegistryRefused, cause: "the registry refused the copy",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeededResetEnv(t)
			initial := e.state()
			pwBefore := e.passwords()
			e.pollute()
			tt.inject(e)

			_, _, err := e.run()
			be := requireBenchError(t, err)
			if be.Kind != tt.kind {
				t.Errorf("Kind = %d, want %d (the kind of the failure is kept); error: %v", be.Kind, tt.kind, err)
			}
			if !strings.Contains(be.Cause, tt.cause) {
				t.Errorf("Cause = %q, want it to keep %q", be.Cause, tt.cause)
			}
			if !strings.HasSuffix(be.Check, halfWayCheck) {
				t.Errorf("Check = %q, want it to end with %q", be.Check, halfWayCheck)
			}
			if got := len(e.kube.deleteUIDs); got > 1 {
				t.Errorf("%d deletes of the claim: a refused delete is not sent again", got)
			}
			if e.fwd.active {
				t.Error("the port-forward is still open")
			}

			tt.heal(e)
			e.mustReset()
			requireSameState(t, e.state(), initial)
			if got := e.passwords(); got != pwBefore {
				t.Error("a password changed")
			}
		})
	}
}

// ---- cancellation ----------------------------------------------------------------------

func TestResetReturnsACancelledContextAsIs(t *testing.T) {
	t.Run("cancelled before the start: nothing is written", func(t *testing.T) {
		e := newSeededResetEnv(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := e.runWith(ctx)
		requireAsIs(t, err)
		e.requireNothingWritten()
	})
	t.Run("cancelled while the upstream is cut", func(t *testing.T) {
		e := newSeededResetEnv(t)
		e.pollute()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.onSleep = func(int) { cancel() }
		_, _, err := e.runWith(ctx)
		requireAsIs(t, err)
	})
	t.Run("cancelled while the repositories empty", func(t *testing.T) {
		e := newSeededResetEnv(t)
		e.pollute()
		e.b.nexus.KeepComponentsAfterDelete(100000)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.onSleep = func(int) {
			if e.b.nexus.Count(http.MethodGet, pathComponents) > 0 {
				cancel()
			}
		}
		_, _, err := e.runWith(ctx)
		requireAsIs(t, err)
		if len(e.fwd.calls) != 0 {
			t.Error("the seed started after the cancellation")
		}
	})
	t.Run("cancelled during the seed: the port-forward is stopped", func(t *testing.T) {
		e := newSeededResetEnv(t)
		e.pollute()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.cp.copyErr = func(int, copyCall) error {
			cancel()
			return &BenchError{Kind: Unclassified, Cause: "skopeo copy was interrupted", Check: "run the command again", Err: context.Canceled}
		}
		_, _, err := e.runWith(ctx)
		requireAsIs(t, err)
		if len(e.fwd.calls) != 1 || e.fwd.stops != 1 {
			t.Errorf("%d port-forwards, stopped %d times, want one each", len(e.fwd.calls), e.fwd.stops)
		}
	})
}

func TestResetStopsThePortForwardWhenTheSeedFails(t *testing.T) {
	e := newSeededResetEnv(t)
	e.cp.copyErr = func(int, copyCall) error {
		return &BenchError{Kind: RegistryRefused, Cause: "the registry refused the copy", Check: "mise run nexus-bench:status"}
	}
	if _, _, err := e.run(); err == nil {
		t.Fatal("Reset succeeded")
	}
	if len(e.fwd.calls) != 1 || e.fwd.stops != 1 || e.fwd.active {
		t.Errorf("%d port-forwards, stopped %d times, active %v", len(e.fwd.calls), e.fwd.stops, e.fwd.active)
	}
}
