package nexusbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// The tests of the restore of the upstream (DEVDESK-534, NX-02b, step 4, task
// DEVDESK-551; CA4 "rétablissement"):
//
//	RestoreUpstream(ctx, SeedDeps{Runner, Forwarder, Copier, Clock, Store}, baseURL, p, out) error
//
// against a provisioned fake Nexus (fakenexus_test.go) whose proxy is auto-blocked,
// the scripted kubectl of upstream_test.go, the recording port-forward and image
// copier of seed_test.go, and the fake clock. The REST requests, the kubectl calls,
// the port-forward and the copier reads share one journal (bench.log), so the order
// of the steps is proven.
//
// What the tests assume, to settle with the code (measures of step 0 in
// .claude/plans/DEVDESK-534-measures.md; the order is D-O of the plan):
//   - Order: the stored credentials (admin and bench-pull; nothing is written before
//     they are known) -> EnsureUpstream (scale to one replica when cut, the wait of
//     the pods until one is ready) -> UnblockProxy (GET, PUT of the proxy
//     configuration, POST invalidate-cache; H-UNBLOCK: the PUT is what lifts the
//     block, so the restore does not wait for the instance to unblock itself) ->
//     the port-forward to svc/bench-upstream:5000, a ManifestDigest of every image
//     of the upstream without credentials, each equal to the digest of the data set,
//     the port-forward stopped -> a ManifestDigest through docker-proxy, with the
//     bench-pull account, of the PullThrough images only, each equal to the digest
//     of the data set (the cache is read once, which refreshes what the invalidation
//     emptied: H-STALE).
//   - It never copies anything (no Copy call), never reads a non-PullThrough image
//     through docker-proxy (F9: the proxy would cache it, and only reset removes it)
//     nor docker-hosted, never uses a reference outside 127.0.0.1, and never writes
//     the cluster but the scale to one replica of bench-upstream.
//   - It is idempotent: an upstream that is ready is left alone (no scale, no wait),
//     and the unblock and the checks run all the same.
//   - Failures: a digest the upstream does not hold, or holds with another value, is a
//     SeedFailed that names the image and says to run `mise run nexus-bench:reset`; a
//     digest the proxy serves that differs from the data set is a SeedFailed too. A
//     failure of a door (kubectl, REST, port-forward, copier) keeps its kind, or is
//     Unclassified with the cause reachable by errors.Is and never printed. Missing
//     secrets, a missing or custom upstream and a deadline are errors before the
//     unblock. A cancelled context is returned as is. The port-forward is stopped on
//     every path. A nil writer is accepted. No password appears in the output, an
//     error or an argument.
//   - Output: lines that start with "restore: " (the unblock) or "upstream: " (the
//     wait).

// restoreEnv is a provisioned bench whose upstream cluster state is scripted.
type restoreEnv struct {
	*seedEnv
	logBase int // entries of the journal before the run
}

// restoreRunner adds the verb of every kubectl call to the shared journal.
type restoreRunner struct {
	Runner
	log *orderLog
}

func (r restoreRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, []byte, error) {
	r.log.add("kube " + verbOf(args))
	return r.Runner.Run(ctx, stdin, args...)
}

// wrongDigest is a manifest digest no image of the data set has.
var wrongDigest = "sha256:" + strings.Repeat("b", 64)

// newRestoreEnv is a bench provisioned against the upstream the bench deploys (the
// proxy configuration the restore sends again names it), the cluster playing before
// and, once a scale is accepted, after. The registries are empty.
func newRestoreEnv(t *testing.T, before, after []upstreamRound) *restoreEnv {
	t.Helper()
	b := provisionedBench(t)
	p := upstreamParams()
	log := b.log
	clk := newFakeClock(t)
	e := &seedEnv{t: t, b: b, p: p, clk: clk, log: log, tmp: t.TempDir(), baseline: len(b.nexus.Requests()), out: &bytes.Buffer{}}
	e.kube = newUpstreamKubectl(t, e.p, clk, before, after)
	e.fwd = &fakeForwarder{t: t, log: log, local: seedForwardLocal}
	sec := b.store.current()
	if sec.PullPassword == "" {
		t.Fatal("the provisioned bench stored no bench-pull password")
	}
	e.cp = &seedCopier{
		t: t, log: log, fwd: e.fwd, nexus: b.nexus, p: &e.p, tmp: e.tmp,
		admin:      Credentials{User: adminUserID, Password: sec.AdminPassword},
		pull:       Credentials{User: pullUserID, Password: sec.PullPassword},
		upstream:   map[string]string{},
		hosted:     map[string]string{},
		proxyCache: map[string]string{},
		listing:    map[string][]fakeComponent{},
		hidden:     map[string]bool{},
		blind:      map[string]bool{},
	}
	e.deps = SeedDeps{Runner: restoreRunner{e.kube, log}, Forwarder: e.fwd, Copier: e.cp, Clock: clk, Store: b.store}
	return &restoreEnv{seedEnv: e, logBase: len(log.snapshot())}
}

// newCutRestoreEnv is the bench after a cut-upstream: the upstream at zero replicas,
// the data set still in the registries (the volume is untouched), the proxy
// auto-blocked with a negative cache entry. A scale to one gives a pod being
// created, then a ready one.
func newCutRestoreEnv(t *testing.T) *restoreEnv {
	t.Helper()
	e := newRestoreEnv(t, []upstreamRound{cutRound()}, restoreRounds())
	e.cp.preloadAll()
	e.b.nexus.AutoBlockProxy()
	e.b.nexus.MarkNegativeCached(dockerProxyRepository)
	return e
}

// newReadyRestoreEnv is a bench whose upstream is already back.
func newReadyRestoreEnv(t *testing.T) *restoreEnv {
	t.Helper()
	e := newRestoreEnv(t, []upstreamRound{readyUpstream()}, nil)
	e.cp.preloadAll()
	return e
}

func (e *restoreEnv) restore() error {
	e.t.Helper()
	return e.restoreWith(context.Background(), e.p)
}

func (e *restoreEnv) restoreWith(ctx context.Context, p Params) error {
	e.t.Helper()
	var out io.Writer = e.out
	if e.nilOut {
		out = nil
	}
	e.runs++
	err := RestoreUpstream(ctx, e.deps, e.b.nexus.URL(), p, out)
	e.check(err)
	return err
}

func (e *restoreEnv) mustRestore() {
	e.t.Helper()
	if err := e.restore(); err != nil {
		e.t.Fatalf("RestoreUpstream: %v", err)
	}
}

// events is the journal since the bench was built.
func (e *restoreEnv) events() []string { return e.log.snapshot()[e.logBase:] }

func indexOfEvent(events []string, prefix string) int {
	return slices.IndexFunc(events, func(ev string) bool { return strings.HasPrefix(ev, prefix) })
}

func lastIndexOfEvent(events []string, prefix string) int {
	for i := len(events) - 1; i >= 0; i-- {
		if strings.HasPrefix(events[i], prefix) {
			return i
		}
	}
	return -1
}

var (
	proxyPut  = "HTTP PUT " + repoPath("proxy", dockerProxyRepository)
	proxyPost = "HTTP POST " + invalidatePath(dockerProxyRepository)
)

// check is what holds after every call, whatever the outcome.
func (e *restoreEnv) check(err error) {
	e.t.Helper()
	// The cluster: the only write is the scale of bench-upstream.
	for _, w := range e.kube.writes {
		if !strings.HasPrefix(w, "scale deployment/"+upstreamName+" ") {
			e.t.Errorf("write %q is not a scale of %s", w, upstreamName)
		}
	}
	e.kube.requireNoPreflight()
	// Nexus: always the admin account; the only writes are the PUT and the POST that
	// put the proxy back in service.
	for _, r := range e.b.nexus.Requests()[e.baseline:] {
		if r.User != adminUserID || r.EmptyPassword {
			e.t.Errorf("request %s %s sent as %q (empty password: %v), want the stored admin", r.Method, r.Path, r.User, r.EmptyPassword)
		}
		switch r.Method {
		case http.MethodGet:
		case http.MethodPut:
			if r.Path != repoPath("proxy", dockerProxyRepository) {
				e.t.Errorf("PUT %s: the restore writes the proxy configuration only", r.Path)
			}
		case http.MethodPost:
			if r.Path != invalidatePath(dockerProxyRepository) {
				e.t.Errorf("POST %s: the restore invalidates the cache of the proxy only", r.Path)
			}
		default:
			e.t.Errorf("%s %s: the restore deletes nothing", r.Method, r.Path)
		}
	}
	// The copier: nothing is copied; the reads follow F9 and stay on the loopback.
	if len(e.cp.copies) != 0 {
		e.t.Errorf("RestoreUpstream copied images: %s", describeCopies(e.cp.copies))
	}
	cached := map[string]bool{}
	for _, img := range pullThrough() {
		cached[imageArg(nexusImageRef(e.p, dockerProxyRepository, img))] = true
	}
	for _, c := range e.cp.digests {
		if !strings.HasPrefix(c.ref.Location, seedLoopbackHost+":") {
			e.t.Errorf("read of %s: not on the loopback", imageArg(c.ref))
		}
		switch c.where {
		case whereUpstream:
			if c.creds != nil {
				e.t.Errorf("the upstream was read with credentials")
			}
		case whereProxy:
			if !cached[imageArg(c.ref)] {
				e.t.Errorf("F9: %s was read through docker-proxy, which caches it: only the PullThrough images may be", imageArg(c.ref))
			}
			if !sameCreds(c.creds, &e.cp.pull) {
				e.t.Errorf("the proxy was read with credentials other than bench-pull")
			}
		default:
			e.t.Errorf("read in %q: the restore reads the upstream and docker-proxy only", c.where)
		}
	}
	// One port-forward per run, always stopped.
	if len(e.fwd.calls) > e.runs {
		e.t.Errorf("%d port-forwards for %d runs", len(e.fwd.calls), e.runs)
	}
	for _, call := range e.fwd.calls {
		if want := (forwardCall{e.p.Namespace, upstreamName, 5000}); call != want {
			e.t.Errorf("port-forward to %+v, want %+v", call, want)
		}
	}
	if e.fwd.active {
		e.t.Error("the port-forward is still open when RestoreUpstream returns")
	}
	if opened := e.openedForwards(); e.fwd.stops < opened {
		e.t.Errorf("%d forwards opened, stop called %d times", opened, e.fwd.stops)
	}
	// Progress lines.
	for _, line := range strings.Split(strings.TrimSpace(e.out.String()), "\n") {
		if line != "" && !strings.HasPrefix(line, "restore: ") && !strings.HasPrefix(line, "upstream: ") {
			e.t.Errorf("progress line %q starts with neither restore: nor upstream:", line)
		}
	}
	e.requireQuiet(err)
}

// requireNothingTouched checks the run stopped before it did anything.
func (e *restoreEnv) requireNothingTouched() {
	e.t.Helper()
	e.requireNoProgress()
	if len(e.kube.lines) != 0 {
		e.t.Errorf("the cluster was called: %q", e.kube.lines)
	}
	if got := len(e.b.nexus.Requests()) - e.baseline; got != 0 {
		e.t.Errorf("%d requests to Nexus: %q", got, calls(e.b.nexus.Requests()[e.baseline:]))
	}
}

// requireUpstreamDigests checks the upstream was read once per image it holds, each
// without credentials, and that the proxy was read for the cached images only.
func (e *restoreEnv) requireReads() {
	e.t.Helper()
	var upstream, proxy []string
	for _, c := range e.cp.digests {
		switch c.where {
		case whereUpstream:
			upstream = append(upstream, imageArg(c.ref))
		case whereProxy:
			proxy = append(proxy, imageArg(c.ref))
		}
	}
	var wantUp, wantProxy []string
	for _, img := range imagesIn(LocationUpstream) {
		wantUp = append(wantUp, imageArg(upstreamImageRef(e.fwd.local, img)))
	}
	for _, img := range pullThrough() {
		wantProxy = append(wantProxy, imageArg(nexusImageRef(e.p, dockerProxyRepository, img)))
	}
	slices.Sort(upstream)
	slices.Sort(wantUp)
	if !slices.Equal(upstream, wantUp) {
		e.t.Errorf("the upstream was read at %q, want every image it holds once: %q", upstream, wantUp)
	}
	if !slices.Equal(proxy, wantProxy) {
		e.t.Errorf("docker-proxy was read at %q, want the PullThrough images only: %q", proxy, wantProxy)
	}
}

// ---- the nominal path -------------------------------------------------------

func TestRestoreUpstreamBringsACutUpstreamBack(t *testing.T) {
	e := newCutRestoreEnv(t)
	e.mustRestore()

	// The cluster: one scale to one replica, then a wait that needs one sleep (the
	// pod is being created at the first round), and nothing else: the unblock does
	// not wait for the instance to unblock itself (H-UNBLOCK).
	e.requireOneScale("1")
	if got := len(e.clk.sleeps); got != 1 {
		t.Errorf("slept %v, want the single poll of the wait for the pod", e.clk.sleeps)
	}
	e.requireOnlyUpstreamScales()

	// The proxy is back in service: not blocked, nothing negative cached.
	if e.b.nexus.ProxyBlocked() || e.b.nexus.NegativeCached(dockerProxyRepository) {
		t.Errorf("the proxy is still blocked (%v) or negative cached (%v)", e.b.nexus.ProxyBlocked(), e.b.nexus.NegativeCached(dockerProxyRepository))
	}
	e.requireReads()
	if len(e.fwd.calls) != 1 {
		t.Errorf("%d port-forwards, want one", len(e.fwd.calls))
	}
	// Reading never changes what the registries hold.
	e.requireListing(dockerProxyRepository, pullThrough())
	if !strings.Contains(e.out.String(), "restore: ") {
		t.Errorf("output %q has no restore: line", e.out.String())
	}
}

func (e *restoreEnv) requireOneScale(replicas string) {
	e.t.Helper()
	if len(e.kube.writes) != 1 || len(e.kube.scales) != 1 {
		e.t.Fatalf("writes = %q, want exactly one scale", e.kube.writes)
	}
	if got := e.kube.scales[0]; got.replicas != replicas || got.namespace != e.p.Namespace {
		e.t.Errorf("scale = %+v, want %s replicas in %s", got, replicas, e.p.Namespace)
	}
}

func (e *restoreEnv) requireOnlyUpstreamScales() {
	e.t.Helper()
	for _, w := range e.kube.writes {
		if !strings.HasPrefix(w, "scale deployment/"+upstreamName+" ") {
			e.t.Errorf("write %q is not a scale of %s", w, upstreamName)
		}
	}
}

func TestRestoreUpstreamOrderOfTheSteps(t *testing.T) {
	e := newCutRestoreEnv(t)
	e.mustRestore()
	ev := e.events()

	scale := indexOfEvent(ev, "kube scale")
	put, post := slices.Index(ev, proxyPut), slices.Index(ev, proxyPost)
	forward, stop := slices.Index(ev, "forward"), slices.Index(ev, "stop")
	firstUp, lastUp := indexOfEvent(ev, "digest upstream"), lastIndexOfEvent(ev, "digest upstream")
	firstProxy := indexOfEvent(ev, "digest proxy")
	lastKube := lastIndexOfEvent(ev, "kube ")
	for name, i := range map[string]int{
		"kube scale": scale, "PUT proxy": put, "POST invalidate-cache": post, "forward": forward, "stop": stop,
		"digest upstream": firstUp, "digest proxy": firstProxy, "a kubectl call": lastKube,
	} {
		if i < 0 {
			t.Fatalf("no %q event in %q", name, ev)
		}
	}
	steps := []struct {
		name string
		at   int
	}{
		{"the scale to one", scale},
		{"the last kubectl read of the wait", lastKube},
		{"the PUT of the proxy configuration", put},
		{"the invalidation of the cache", post},
		{"the port-forward opens", forward},
		{"the first read of the upstream", firstUp},
		{"the last read of the upstream", lastUp},
		{"the port-forward stops", stop},
		{"the first read through the proxy", firstProxy},
	}
	for i := 1; i < len(steps); i++ {
		if steps[i-1].at >= steps[i].at {
			t.Errorf("%s (event %d) does not come before %s (event %d):\n%s", steps[i-1].name, steps[i-1].at, steps[i].name, steps[i].at, strings.Join(ev, "\n"))
		}
	}
	// The wait reads the pods after the scale and before the unblock.
	if lastKube == scale {
		t.Errorf("the pods were not read after the scale: %q", ev)
	}
	// The data set never goes through the port-forward twice.
	if n := len(e.fwd.calls); n != 1 {
		t.Errorf("%d port-forwards, want 1", n)
	}
	// The proxy is read only once the port-forward is closed.
	for i, event := range ev {
		if strings.HasPrefix(event, "digest proxy") && i < stop {
			t.Errorf("%q comes before the port-forward is stopped", event)
		}
	}
}

func TestRestoreUpstreamSendsTheConfigurationOfRepositorySpecs(t *testing.T) {
	e := newCutRestoreEnv(t)
	before := len(e.b.nexus.Requests())
	e.mustRestore()
	var put *fakeReq
	for _, r := range e.b.nexus.Requests()[before:] {
		if r.Method == http.MethodPut {
			put = &r
		}
	}
	if put == nil {
		t.Fatal("no PUT reached the proxy")
	}
	if cfg := proxyConfigOf(t, e.p); !strings.Contains(put.Body, fmt.Sprint(jsonAt(cfg, "proxy", "remoteUrl"))) {
		t.Errorf("the PUT body %q does not carry the remote URL of repositorySpecs: %v", put.Body, jsonAt(cfg, "proxy", "remoteUrl"))
	}
}

// ---- idempotence ------------------------------------------------------------

func TestRestoreUpstreamIsIdempotent(t *testing.T) {
	t.Run("an upstream that is already ready is left alone, the checks run", func(t *testing.T) {
		e := newReadyRestoreEnv(t)
		e.b.nexus.AutoBlockProxy()
		e.mustRestore()
		if len(e.kube.writes) != 0 {
			t.Errorf("wrote to the cluster: %q", e.kube.writes)
		}
		if len(e.clk.sleeps) != 0 {
			t.Errorf("waited %v for an upstream that was ready", e.clk.sleeps)
		}
		if e.b.nexus.ProxyBlocked() {
			t.Error("the proxy was not put back in service")
		}
		e.requireReads()
	})
	t.Run("an upstream that is starting is waited for without a write", func(t *testing.T) {
		e := newRestoreEnv(t, []upstreamRound{roundOf(1, creatingPod()), roundOf(1, readyPod())}, nil)
		e.cp.preloadAll()
		e.mustRestore()
		if len(e.kube.writes) != 0 {
			t.Errorf("wrote to the cluster: %q", e.kube.writes)
		}
		if len(e.clk.sleeps) != 1 {
			t.Errorf("slept %v, want the one poll of the wait", e.clk.sleeps)
		}
		e.requireReads()
	})
	t.Run("twice in a row writes once", func(t *testing.T) {
		e := newCutRestoreEnv(t)
		e.mustRestore()
		first := len(e.b.nexus.Requests())
		e.mustRestore()
		e.requireOneScale("1")
		if len(e.fwd.calls) != 2 {
			t.Errorf("%d port-forwards for two runs, want one per run", len(e.fwd.calls))
		}
		if len(e.b.nexus.Requests()) == first {
			t.Error("the second run did not put the proxy in service again: it is idempotent, not skipped")
		}
	})
}

// ---- what the upstream holds ------------------------------------------------

func TestRestoreUpstreamChecksEveryUpstreamImageAgainstTheDataSet(t *testing.T) {
	for _, img := range imagesIn(LocationUpstream) {
		for _, kind := range []string{"lost", "changed"} {
			t.Run(kind+" "+nameTag(img), func(t *testing.T) {
				e := newCutRestoreEnv(t)
				if kind == "lost" {
					e.cp.removeUpstream(img)
				} else {
					e.cp.preload(whereUpstream, img, wrongDigest)
				}
				err := e.restore()
				if err == nil {
					t.Fatal("RestoreUpstream succeeded on an upstream that does not hold the data set")
				}
				_ = requireKind(t, err, SeedFailed)
				be := requireCleanError(t, err)
				requireMentions(t, err, []string{img.Name + ":" + img.Tag}, nil)
				if !strings.Contains(be.Check, "nexus-bench:reset") {
					t.Errorf("Check %q does not say to run mise run nexus-bench:reset", be.Check)
				}
				if kind == "lost" && !strings.Contains(strings.ToLower(err.Error()), "lost") {
					t.Errorf("error %q does not say the upstream lost the image", err)
				}
				// The proxy is read only once the upstream is known to be right.
				for _, c := range e.cp.digests {
					if c.where == whereProxy {
						t.Errorf("docker-proxy was read although the upstream is wrong: %s", imageArg(c.ref))
					}
				}
			})
		}
	}
}

func TestRestoreUpstreamFailsWhenTheProxyServesAnotherDigest(t *testing.T) {
	e := newCutRestoreEnv(t)
	img := datasetImage(t, "mirror/cached", "1.0", LocationUpstream)
	e.cp.preload(whereProxy, img, wrongDigest)
	err := e.restore()
	if err == nil {
		t.Fatal("RestoreUpstream succeeded although docker-proxy serves another content")
	}
	_ = requireKind(t, err, SeedFailed)
	_ = requireCleanError(t, err)
	requireMentions(t, err, []string{nameTag(img), dockerProxyRepository}, nil)
	// The reads of the upstream all came first.
	e.requireReads()
}

func TestRestoreUpstreamReadsTheUpstreamWithoutCredentials(t *testing.T) {
	e := newCutRestoreEnv(t)
	e.mustRestore()
	var n int
	for _, c := range e.cp.digests {
		if c.where == whereUpstream {
			n++
		}
	}
	if want := len(imagesIn(LocationUpstream)); n != want {
		t.Errorf("%d reads of the upstream, want %d", n, want)
	}
}

// ---- failures, one per step -------------------------------------------------

func TestRestoreUpstreamNeedsTheStoredCredentials(t *testing.T) {
	tests := []struct {
		name  string
		store func(e *restoreEnv) SecretStore
	}{
		{"no secrets file", func(e *restoreEnv) SecretStore {
			if err := e.b.store.Remove(); err != nil {
				t.Fatal(err)
			}
			return e.b.store
		}},
		{"no admin password", func(e *restoreEnv) SecretStore {
			e.b.store.preset(Secrets{PullPassword: secretMarker + "-pull"})
			return e.b.store
		}},
		{"no bench-pull password", func(e *restoreEnv) SecretStore {
			e.b.store.preset(Secrets{AdminPassword: storedAdmin})
			return e.b.store
		}},
		{"an unreadable secrets file", func(*restoreEnv) SecretStore { return &failingStore{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newCutRestoreEnv(t)
			e.deps.Store = tt.store(e)
			err := e.restore()
			if err == nil {
				t.Fatal("RestoreUpstream succeeded without the credentials")
			}
			be := requireKind(t, err, ProvisionFailed)
			if !strings.Contains(be.Error(), "nexus-bench:up") {
				t.Errorf("error %q does not say to run mise run nexus-bench:up", be)
			}
			// Nothing is written before the credentials are known: no scale either.
			e.requireNothingTouched()
		})
	}
}

func TestRestoreUpstreamFailsWhenTheUpstreamCannotComeBack(t *testing.T) {
	t.Run("the Deployment is missing", func(t *testing.T) {
		e := newRestoreEnv(t, []upstreamRound{absentRound()}, nil)
		err := e.restore()
		_ = requireCleanError(t, err)
		requireMentions(t, err, []string{upstreamName, "nexus-bench:up"}, nil)
		e.requireNoProgress()
		if len(e.kube.writes) != 0 {
			t.Errorf("wrote to the cluster: %q", e.kube.writes)
		}
		if got := len(e.b.nexus.Requests()) - e.baseline; got != 0 {
			t.Errorf("%d requests to Nexus for an upstream that is not there", got)
		}
	})
	t.Run("the scale is refused", func(t *testing.T) {
		e := newCutRestoreEnv(t)
		e.kube.scaleReply = &fakeReply{stderr: `Error from server (Forbidden): deployments.apps "bench-upstream" is forbidden: cannot patch resource "deployments/scale" token=` + fakeSecret, err: errFakeExit1}
		err := e.restore()
		be := requireDeployKind(t, err, Forbidden)
		if be.Err == nil {
			t.Error("the BenchError dropped the underlying error")
		}
		e.requireNoProgress()
		if len(e.kube.scales) != 1 {
			t.Errorf("%d scale attempts, want 1: a failed write is not retried", len(e.kube.scales))
		}
		if got := len(e.b.nexus.Requests()) - e.baseline; got != 0 {
			t.Errorf("%d requests to Nexus after a failed scale", got)
		}
	})
	t.Run("the cluster does not answer", func(t *testing.T) {
		e := newCutRestoreEnv(t)
		e.kube.deploymentReply = &fakeReply{stderr: "Unable to connect to the server: dial tcp: i/o timeout token=" + fakeSecret, err: errFakeExit1}
		err := e.restore()
		_ = requireDeployKind(t, err, ClusterUnreachable)
		e.requireNoProgress()
		if got := len(e.b.nexus.Requests()) - e.baseline; got != 0 {
			t.Errorf("%d requests to Nexus", got)
		}
	})
	t.Run("the pod never gets ready", func(t *testing.T) {
		e := newRestoreEnv(t, []upstreamRound{cutRound()}, []upstreamRound{roundOf(1, startingPod())})
		e.cp.preloadAll()
		e.b.nexus.AutoBlockProxy()
		err := e.restore()
		_ = requireDeployKind(t, err, Deadline)
		e.requireOneScale("1")
		if el := e.clk.elapsed(); el < e.p.Timeout || el >= e.p.Timeout+upstreamPollInterval {
			t.Errorf("gave up after %v, want %v within one interval (%v)", el, e.p.Timeout, upstreamPollInterval)
		}
		// The proxy is not touched for an upstream that is not there.
		if !e.b.nexus.ProxyBlocked() {
			t.Error("the proxy was put back in service although the upstream never got ready")
		}
		e.requireNoProgress()
	})
}

func TestRestoreUpstreamFailsWhenTheProxyCannotBeUnblocked(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(e *restoreEnv)
		kind    ErrKind
		mention []string
	}{
		{"the instance does not answer", func(e *restoreEnv) { e.b.nexus.DropConnections() }, RESTUnreachable, nil},
		{"the PUT is refused", func(e *restoreEnv) {
			e.b.nexus.Forbid(http.MethodPut, repoPath("proxy", dockerProxyRepository), http.StatusInternalServerError)
		}, ProvisionFailed, nil},
		{"the invalidation is refused", func(e *restoreEnv) {
			e.b.nexus.Forbid(http.MethodPost, invalidatePath(dockerProxyRepository), http.StatusForbidden)
		}, ProvisionFailed, nil},
		{"the proxy does not exist", func(e *restoreEnv) { e.b.nexus.DeleteRepo("proxy", dockerProxyRepository) }, ProvisionFailed, []string{"nexus-bench:up"}},
		{"the stored admin password is refused", func(e *restoreEnv) {
			e.b.store.preset(Secrets{AdminPassword: secretMarker + "-wrong-admin", PullPassword: e.b.store.current().PullPassword})
		}, ProvisionFailed, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newCutRestoreEnv(t)
			tt.prepare(e)
			err := e.restore()
			if err == nil {
				t.Fatal("RestoreUpstream succeeded")
			}
			be := requireBenchError(t, err)
			if be.Kind != tt.kind {
				t.Errorf("Kind = %d, want %d; message: %v", be.Kind, tt.kind, err)
			}
			requireMentions(t, err, tt.mention, nil)
			// The upstream is back by then; the checks of its content come after the unblock.
			e.requireNoProgress()
		})
	}
}

func TestRestoreUpstreamFailsWhenADoorFails(t *testing.T) {
	plain := fmt.Errorf("FATA[0000] auth=%s:%s token=%s", adminUserID, storedAdmin, fakeSecret)
	tests := []struct {
		name   string
		inject func(e *restoreEnv)
	}{
		{"the port-forward", func(e *restoreEnv) { e.fwd.err = plain }},
		{"a read of the upstream", func(e *restoreEnv) {
			e.cp.digestErr = func(c digestCall) error {
				if c.where == whereUpstream {
					return plain
				}
				return nil
			}
		}},
		{"a read through the proxy", func(e *restoreEnv) {
			e.cp.digestErr = func(c digestCall) error {
				if c.where == whereProxy {
					return plain
				}
				return nil
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newCutRestoreEnv(t)
			tt.inject(e)
			err := e.restore()
			if err == nil {
				t.Fatal("RestoreUpstream succeeded")
			}
			var be *BenchError
			if !errors.As(err, &be) {
				t.Fatalf("error = %#v, want a *BenchError", err)
			}
			if !errors.Is(err, plain) {
				t.Error("the cause is not reachable with errors.Is")
			}
			if strings.Contains(err.Error(), "FATA") {
				t.Errorf("error %q repeats what the sub-process said", err)
			}
			if len(e.cp.copies) != 0 {
				t.Errorf("copies after a failure: %s", describeCopies(e.cp.copies))
			}
		})
	}
	t.Run("a registry that cannot be reached keeps its kind", func(t *testing.T) {
		e := newCutRestoreEnv(t)
		injected := &BenchError{Kind: RegistryUnreachable, Cause: "skopeo inspect could not reach a registry", Check: "check the port-forward"}
		e.cp.digestErr = func(c digestCall) error {
			if c.where == whereUpstream {
				return injected
			}
			return nil
		}
		err := e.restore()
		if !errors.Is(err, injected) {
			t.Fatalf("error = %v, want the failure of the copier", err)
		}
		_ = requireKind(t, err, RegistryUnreachable)
	})
}

func TestRestoreUpstreamRefusesAForwardOutsideTheLoopback(t *testing.T) {
	for _, local := range []string{"192.168.1.31:41234", "localhost:41234", "127.0.0.1.evil.example:41234", "127.0.0.1@evil.example:41234"} {
		t.Run(local, func(t *testing.T) {
			e := newCutRestoreEnv(t)
			e.fwd.local = local
			err := e.restore()
			if err == nil {
				t.Fatal("RestoreUpstream accepted a port-forward that is not on the loopback")
			}
			var be *BenchError
			if !errors.As(err, &be) {
				t.Fatalf("error = %#v, want a *BenchError", err)
			}
			if len(e.fwd.calls) != 1 {
				t.Errorf("%d port-forwards, want the one that was refused", len(e.fwd.calls))
			}
			if len(e.cp.copies) != 0 || len(e.cp.digests) != 0 {
				t.Errorf("the copier was given a reference outside the loopback: %d copies, %d reads", len(e.cp.copies), len(e.cp.digests))
			}
		})
	}
}

// ---- the guards -------------------------------------------------------------

func TestRestoreUpstreamRefusesACustomUpstream(t *testing.T) {
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
			e := newCutRestoreEnv(t)
			p := e.p
			tt.mutate(&p)
			if p.UpstreamIsBench() {
				t.Fatalf("the case is not a custom upstream: %q in %q", p.UpstreamURL, p.Namespace)
			}
			err := e.restoreWith(context.Background(), p)
			if err == nil {
				t.Fatal("RestoreUpstream accepted an upstream that is not the bench's")
			}
			var be *BenchError
			if !errors.As(err, &be) || be.Cause == "" || be.Check == "" {
				t.Fatalf("error %T %v is not a complete *BenchError", err, err)
			}
			requireMentions(t, err, []string{envUpstreamURL}, nil)
			e.requireNothingTouched()
		})
	}
}

func TestRestoreUpstreamReturnsACancelledContextAsIs(t *testing.T) {
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
	t.Run("cancelled before the start: nothing is called", func(t *testing.T) {
		e := newCutRestoreEnv(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		requireAsIs(t, e.restoreWith(ctx, e.p))
		e.requireNothingTouched()
	})
	t.Run("cancelled during the wait for the pod: the proxy is not touched", func(t *testing.T) {
		e := newRestoreEnv(t, []upstreamRound{cutRound()}, []upstreamRound{roundOf(1, creatingPod())})
		e.cp.preloadAll()
		e.b.nexus.AutoBlockProxy()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.clk.onSleep = func(n int) {
			if n == 2 {
				cancel()
			}
		}
		requireAsIs(t, e.restoreWith(ctx, e.p))
		e.requireOneScale("1")
		if !e.b.nexus.ProxyBlocked() {
			t.Error("the proxy was put back in service although the run was cancelled before the upstream was ready")
		}
		e.requireNoProgress()
	})
	t.Run("cancelled during the reads of the upstream: the port-forward is stopped", func(t *testing.T) {
		e := newCutRestoreEnv(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.cp.digestErr = func(c digestCall) error {
			if c.where == whereUpstream {
				cancel()
				return &BenchError{Kind: Unclassified, Cause: "skopeo inspect was interrupted", Check: "run the command again", Err: context.Canceled}
			}
			return nil
		}
		requireAsIs(t, e.restoreWith(ctx, e.p))
		if e.fwd.stops != 1 {
			t.Errorf("the port-forward was stopped %d times, want once", e.fwd.stops)
		}
		for _, c := range e.cp.digests {
			if c.where == whereProxy {
				t.Errorf("docker-proxy was read after the cancellation: %s", imageArg(c.ref))
			}
		}
	})
	t.Run("cancelled during the reads through the proxy", func(t *testing.T) {
		e := newCutRestoreEnv(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.cp.digestErr = func(c digestCall) error {
			if c.where == whereProxy {
				cancel()
				return &BenchError{Kind: Unclassified, Cause: "skopeo inspect was interrupted", Check: "run the command again", Err: context.Canceled}
			}
			return nil
		}
		requireAsIs(t, e.restoreWith(ctx, e.p))
	})
}

func TestRestoreUpstreamAcceptsANilWriter(t *testing.T) {
	e := newCutRestoreEnv(t)
	e.nilOut = true
	e.mustRestore()
	e.requireOneScale("1")
	e.requireReads()
}

func TestRestoreUpstreamNeverPrintsAPassword(t *testing.T) {
	e := newCutRestoreEnv(t)
	e.mustRestore()
	if e.out.Len() == 0 {
		t.Fatal("RestoreUpstream said nothing")
	}
	// check (run by restore) holds the generic rule; this is the explicit one.
	for _, secret := range e.secrets() {
		if secret != "" && strings.Contains(e.out.String(), secret) {
			t.Errorf("the output contains a password (%d characters)", len(secret))
		}
	}
	argv := &strings.Builder{}
	for _, c := range e.cp.digests {
		argv.WriteString(imageArg(c.ref) + "\n")
	}
	for _, secret := range e.secrets() {
		if secret != "" && strings.Contains(argv.String(), secret) {
			t.Errorf("a reference given to skopeo contains a password")
		}
	}
}
