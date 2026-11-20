package nexusbench

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/oci"
)

// The two scenario tests of NX-02b (DEVDESK-534, step 8 of its plan). They act on
// the real bench, and they change it: the first cuts the upstream of the proxy and
// brings it back, the second deletes the three repositories and the claim of the
// upstream. They need, on top of what bench_integration_test.go needs, a bench that
// is up and seeded (`mise run nexus-bench:up`) and DEVDESK_NEXUS_BENCH_SCENARIOS=1:
//
//	DEVDESK_NEXUS_BENCH_SCENARIOS=1 mise run nexus-bench:test
//
// Both start with requireBench(t) and requireScenarios(t), so neither `mise run
// test` nor the CI reaches them, and neither runs together with the measures of
// NX-03 to NX-05, which assume a bench that is not being cut or reset.
//
//   - TestIntegrationUpstreamCutRestore: CutUpstream (mirror/cached still served,
//     mirror/uncached refused by the proxy), RestoreUpstream (both served, with
//     the digest of the data set), then a second cut and restore that read
//     mirror/cached only (H-STALE). It ends with a Reset (t.Cleanup, so it also
//     runs when the test fails half-way) that gives the bench its initial state back.
//   - TestIntegrationResetKeepsInstance: one wrong-UID delete of the claim of the
//     upstream (refused with a conflict), one tag added to docker-hosted and one
//     image to the upstream, then Reset, which has to bring the data set back
//     while the namespace, the Nexus pod and nexus-data stay the same objects. The
//     image of the upstream is also read through docker-proxy before the reset
//     (outside the data set, so F9 holds), and the proxy must not list it after.
//
// F9: a read through docker-proxy or docker-group caches the image. The cut
// scenario reads mirror/uncached through docker-proxy on purpose, once per state of
// its first cut and restore (CA4: what the proxy has not cached fails while the
// upstream is cut and is served again once it is back), and its closing Reset
// removes what that read cached. No other test reads mirror/uncached through the
// proxy or the group: they list it by the REST API of the components, or read it in
// the upstream itself by a port-forward. Only mirror/cached (the PullThrough
// image) is read through the proxy in both states, as the seed does.

const (
	// benchScenariosEnv opens, in addition to benchGateEnv, the tests that change
	// the bench.
	benchScenariosEnv = "DEVDESK_NEXUS_BENCH_SCENARIOS"

	// scenarioMinTimeout is the go test -timeout below which a scenario refuses to
	// start: a reset waits for a volume to be deleted and re-created, and the cut
	// scenario runs two cuts, two restores and a reset.
	scenarioMinTimeout = 30 * time.Minute

	// scenarioListWait and scenarioListInterval bound the wait for a repository to
	// list a component that was just pushed.
	scenarioListWait     = 30 * time.Second
	scenarioListInterval = 2 * time.Second

	// uncachedTries is the number of reads of mirror/uncached through the proxy
	// while the upstream is cut, uncachedTryTimeout bounds one of them, and
	// restoreTries and restoreTryInterval bound the same read once the upstream is
	// back (the proxy may need a moment to leave its blocked state, H-UNBLOCK).
	uncachedTries      = 3
	uncachedTryTimeout = 2 * time.Minute
	restoreTries       = 3
	restoreTryInterval = 5 * time.Second

	// pollutedTag is the tag, outside the data set, the reset test pushes.
	pollutedTag = "9.9.9"
	// pollutedUpstreamName is the image, outside the data set, the reset test puts
	// in the upstream.
	pollutedUpstreamName = "extra/offset"
	// unknownUID is a UID no object has: the precondition of a delete that must be
	// refused.
	unknownUID = "00000000-0000-0000-0000-000000000000"
)

// requireScenarios skips the test unless the bench may be changed: the guard of
// requireBench, then DEVDESK_NEXUS_BENCH_SCENARIOS=1.
func requireScenarios(t *testing.T) {
	t.Helper()
	requireBench(t)
	if os.Getenv(benchScenariosEnv) != "1" {
		t.Skipf("set %s=1 as well (with %s=1) to run the scenarios: they cut the upstream and reset the bench", benchScenariosEnv, benchGateEnv)
	}
}

// scenarioBench is the running bench as the scenarios drive it: the library
// functions get the same doors (kubectl, skopeo, port-forward, secrets) the
// command gives them.
type scenarioBench struct {
	p       Params
	runner  Runner
	deps    SeedDeps
	restURL string
	admin   *client
	pull    Credentials
	secrets Secrets
}

// openScenarioBench builds the doors of a scenario from the environment of the
// bench (NEXUS_BENCH_*) and the stored secrets. It writes nothing.
func openScenarioBench(t *testing.T) scenarioBench {
	t.Helper()
	requireBench(t)
	requireScenarios(t)
	if deadline, ok := t.Deadline(); ok && time.Until(deadline) < scenarioMinTimeout {
		t.Fatalf("go test stops this run in %s; a scenario needs at least %s: rerun with -timeout 60m", time.Until(deadline).Round(time.Second), scenarioMinTimeout)
	}
	p, err := FromEnv(Defaults(), os.Getenv)
	if err != nil {
		t.Fatalf("parameters: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("parameters: %v", err)
	}
	if !p.UpstreamIsBench() {
		t.Fatalf("the scenarios cut and reset the upstream of the bench: unset %s", envUpstreamURL)
	}
	secretsPath := p.SecretsPath
	if secretsPath == "" {
		secretsPath = secretsPathFor(t, p.Context, p.Namespace)
	}
	store, err := NewFileStore(secretsPath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	secrets, found, err := store.Load()
	if err != nil {
		t.Fatalf("read the secrets: %v", err)
	}
	if !found || secrets.AdminPassword == "" || secrets.PullPassword == "" {
		t.Fatal("the secrets file holds no admin or no bench-pull password: bring the bench up with `mise run nexus-bench:up`")
	}
	runner, err := NewExecRunner(p.Context)
	if err != nil {
		t.Fatalf("NewExecRunner: %v", err)
	}
	forwarder, err := NewExecPortForwarder(p.Context)
	if err != nil {
		t.Fatalf("NewExecPortForwarder: %v", err)
	}
	copier, err := NewSkopeoCopier()
	if err != nil {
		t.Fatalf("the scenarios copy images with skopeo, which is a prerequisite of the host: %v", err)
	}
	restURL := RESTBaseURL(loopbackHost, p)
	admin := newClient(restURL, adminUserID, secrets.AdminPassword)
	t.Cleanup(admin.closeIdle)
	return scenarioBench{
		p:       p,
		runner:  runner,
		deps:    SeedDeps{Runner: runner, Forwarder: forwarder, Copier: copier, Clock: NewClock(), Store: store},
		restURL: restURL,
		admin:   admin,
		pull:    Credentials{User: pullUserID, Password: secrets.PullPassword},
		secrets: secrets,
	}
}

// resetOnCleanup registers a Reset that gives the bench its initial state back
// when the test ends, whatever the outcome. A failing Reset is reported with the
// command that does the same by hand.
func (b scenarioBench) resetOnCleanup(t *testing.T, skip func() bool) {
	t.Helper()
	t.Cleanup(func() {
		if skip != nil && skip() {
			return
		}
		t.Log("cleanup: reset, to give the bench its initial state back")
		if _, _, err := Reset(context.Background(), b.deps, b.restURL, b.p, logWriter{t}); err != nil {
			t.Errorf("the closing reset failed, the bench may be changed: run `mise run nexus-bench:reset` (%v)", err)
		}
	})
}

// ---- the data set, read by the REST listings ---------------------------------------

// wantedKeys returns the "name:tag" of the data set images that keep is true for,
// sorted.
func wantedKeys(keep func(DatasetImage) bool) []string {
	var keys []string
	for _, img := range Dataset() {
		if keep(img) {
			keys = append(keys, datasetKey(img.Name, img.Tag))
		}
	}
	slices.Sort(keys)
	return keys
}

// listedKeys returns the "name:tag" a repository lists, sorted, with the digest
// problems it finds against the data set: a digest the listing gives and that is
// not the one of the data set. The digest of a component is looked up among the
// images of the location the repository serves (docker-hosted: the hosted ones;
// docker-proxy: the ones of the upstream), as a name and a tag can exist in both
// places with different contents (shared/base:1.0).
func listedKeys(ctx context.Context, t *testing.T, c *client, repo string) (keys, problems []string) {
	t.Helper()
	list, err := c.listComponents(ctx, repo)
	if err != nil {
		t.Fatalf("list the components of %s: %v", repo, err)
	}
	location := LocationUpstream
	if repo == dockerHostedRepository {
		location = LocationHosted
	}
	want := map[string]string{}
	for _, img := range Dataset() {
		if img.Location == location {
			want[datasetKey(img.Name, img.Tag)] = img.Digest
		}
	}
	for _, comp := range list {
		key := datasetKey(comp.Name, comp.Version)
		keys = append(keys, key)
		if digest, known := want[key]; known && comp.Digest != "" && comp.Digest != digest {
			problems = append(problems, repo+" lists "+key+" with the digest "+comp.Digest+", the data set says "+digest)
		}
	}
	slices.Sort(keys)
	return keys, problems
}

// datasetProblems tells what differs from the initial state of the data set:
// docker-hosted lists exactly the hosted images, docker-proxy exactly the
// PullThrough ones, each with the digest of the data set.
func datasetProblems(ctx context.Context, t *testing.T, c *client) []string {
	t.Helper()
	var problems []string
	for repo, keep := range map[string]func(DatasetImage) bool{dockerHostedRepository: isHosted, dockerProxyRepository: isPullThrough} {
		got, digests := listedKeys(ctx, t, c, repo)
		problems = append(problems, digests...)
		if want := wantedKeys(keep); !slices.Equal(got, want) {
			problems = append(problems, repo+" lists "+strings.Join(got, ", ")+", the data set is "+strings.Join(want, ", "))
		}
	}
	slices.Sort(problems)
	return problems
}

// requireInitialDataset fails, before anything is changed, unless the bench is in
// its initial state: the scenarios assume it, and the closing reset restores it.
func requireInitialDataset(ctx context.Context, t *testing.T, c *client) {
	t.Helper()
	if problems := datasetProblems(ctx, t, c); len(problems) > 0 {
		t.Fatalf("the bench is not in its initial state (%s): %s", strings.Join(problems, "; "), resetCheck)
	}
}

// pullThroughServed checks that docker-proxy serves every PullThrough image with
// the digest of the data set, and logs how long that took. It reads no image but
// the PullThrough ones (F9).
func (b scenarioBench) pullThroughServed(ctx context.Context, t *testing.T, when string) {
	t.Helper()
	for _, img := range Dataset() {
		if !img.PullThrough {
			continue
		}
		start := time.Now()
		got, err := b.deps.Copier.ManifestDigest(ctx, nexusImageRef(b.p, dockerProxyRepository, img), &b.pull)
		if err != nil {
			t.Fatalf("%s: reading %s through %s: %v", when, datasetKey(img.Name, img.Tag), dockerProxyRepository, err)
		}
		if got != img.Digest {
			t.Fatalf("%s: %s serves %s with the digest %s, the data set says %s", when, dockerProxyRepository, datasetKey(img.Name, img.Tag), got, img.Digest)
		}
		t.Logf("%s: %s is served by %s in %s", when, datasetKey(img.Name, img.Tag), dockerProxyRepository, time.Since(start).Round(time.Millisecond))
	}
}

// uncachedImage returns the image of the data set the proxy has never been asked
// for: mirror/uncached.
func uncachedImage(t *testing.T) DatasetImage {
	t.Helper()
	for _, img := range Dataset() {
		if img.Location == LocationUpstream && !img.PullThrough && img.Name == "mirror/uncached" {
			return img
		}
	}
	t.Fatal("the data set has no mirror/uncached image")
	return DatasetImage{}
}

// uncachedRefusedWhileCut reads mirror/uncached through docker-proxy while the
// upstream is cut, up to uncachedTries times: every read has to fail, as the proxy
// holds no copy and has nothing to fetch it from. The duration of each failure is
// logged (H-CUTDELAY: the first one is the delay a proxy takes to give up). A read
// that succeeds fails the test, and so does a read that ends because the time of
// the try ran out: that says nothing about what the proxy answers, so it does not
// validate CA4.
func (b scenarioBench) uncachedRefusedWhileCut(ctx context.Context, t *testing.T, when string) {
	t.Helper()
	img := uncachedImage(t)
	for try := 1; try <= uncachedTries; try++ {
		tryCtx, cancel := context.WithTimeout(ctx, uncachedTryTimeout)
		start := time.Now()
		got, err := b.deps.Copier.ManifestDigest(tryCtx, nexusImageRef(b.p, dockerProxyRepository, img), &b.pull)
		timedOut := tryCtx.Err() != nil
		cancel()
		if err == nil {
			t.Fatalf("%s: %s served %s (digest %s) while the upstream is cut: it was cached before the cut", when, dockerProxyRepository, datasetKey(img.Name, img.Tag), got)
		}
		if timedOut {
			t.Fatalf("%s: try %d, reading %s through %s timed out after %s, which is not a refusal by the proxy (CA4 is not shown): %v", when, try, datasetKey(img.Name, img.Tag), dockerProxyRepository, time.Since(start).Round(time.Millisecond), err)
		}
		t.Logf("%s: try %d, reading %s through %s failed after %s (H-CUTDELAY): %v", when, try, datasetKey(img.Name, img.Tag), dockerProxyRepository, time.Since(start).Round(time.Millisecond), err)
	}
}

// uncachedServedAgain reads mirror/uncached through docker-proxy once the upstream
// is back, up to restoreTries times, and fails unless one read answers the digest
// of the data set (H-UNBLOCK: the content of before the cut). It returns once the
// proxy lists the image, which is what it caches by this read: the closing Reset
// removes it.
func (b scenarioBench) uncachedServedAgain(ctx context.Context, t *testing.T, when string) {
	t.Helper()
	img := uncachedImage(t)
	var lastErr error
	for try := 1; try <= restoreTries; try++ {
		start := time.Now()
		got, err := b.deps.Copier.ManifestDigest(ctx, nexusImageRef(b.p, dockerProxyRepository, img), &b.pull)
		if err == nil {
			if got != img.Digest {
				t.Fatalf("%s: %s serves %s with the digest %s, the data set says %s", when, dockerProxyRepository, datasetKey(img.Name, img.Tag), got, img.Digest)
			}
			t.Logf("%s: try %d, %s is served by %s in %s", when, try, datasetKey(img.Name, img.Tag), dockerProxyRepository, time.Since(start).Round(time.Millisecond))
			b.waitListed(ctx, t, dockerProxyRepository, img.Name, img.Tag)
			return
		}
		lastErr = err
		t.Logf("%s: try %d, reading %s through %s failed after %s: %v", when, try, datasetKey(img.Name, img.Tag), dockerProxyRepository, time.Since(start).Round(time.Millisecond), err)
		if err := b.deps.Clock.Sleep(ctx, restoreTryInterval); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("%s: %s did not serve %s again after %d tries: %v", when, dockerProxyRepository, datasetKey(img.Name, img.Tag), restoreTries, lastErr)
}

// proxyKeysAre fails when docker-proxy lists other components than want.
func proxyKeysAre(ctx context.Context, t *testing.T, c *client, want []string, when string) {
	t.Helper()
	got, _ := listedKeys(ctx, t, c, dockerProxyRepository)
	if !slices.Equal(got, want) {
		t.Errorf("%s: %s lists %v, want %v", when, dockerProxyRepository, got, want)
	}
}

// requireUpstreamState fails unless the upstream is in the given state.
func (b scenarioBench) requireUpstreamState(ctx context.Context, t *testing.T, want UpstreamState, when string) {
	t.Helper()
	got, err := ReadUpstream(ctx, b.runner, b.p)
	if err != nil {
		t.Fatalf("%s: read the upstream: %v", when, err)
	}
	if got != want {
		t.Fatalf("%s: the upstream is in state %d, want %d", when, got, want)
	}
}

// timed runs fn and logs how long it took.
func timed(t *testing.T, what string, fn func() error) {
	t.Helper()
	start := time.Now()
	if err := fn(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	t.Logf("%s took %s", what, time.Since(start).Round(time.Second))
}

// ---- the cut and the restore ---------------------------------------------------------

func TestIntegrationUpstreamCutRestore(t *testing.T) {
	requireBench(t)
	requireScenarios(t)
	b := openScenarioBench(t)
	ctx, out := context.Background(), logWriter{t}

	requireInitialDataset(ctx, t, b.admin)
	// proxyWant is what docker-proxy has to list: what it listed before, plus
	// mirror/uncached once the first restore has had it served and cached.
	proxyWant, _ := listedKeys(ctx, t, b.admin, dockerProxyRepository)
	b.resetOnCleanup(t, nil)

	// first is true for the first cut and restore, which read mirror/uncached
	// through the proxy; the second pair reads mirror/cached only, as the proxy
	// holds mirror/uncached by then (H-STALE).
	cut := func(when string, first bool) {
		t.Helper()
		timed(t, when+": cut the upstream", func() error { return CutUpstream(ctx, b.runner, b.deps.Clock, b.p, out) })
		b.requireUpstreamState(ctx, t, UpstreamCut, when)
		// What the proxy has cached is served while the upstream is gone; what it
		// has not fails, and a failed read adds nothing to its listing.
		b.pullThroughServed(ctx, t, when+", upstream cut")
		if first {
			b.uncachedRefusedWhileCut(ctx, t, when+", upstream cut")
		}
		proxyKeysAre(ctx, t, b.admin, proxyWant, when+", upstream cut")
	}
	restore := func(when string, first bool) {
		t.Helper()
		// RestoreUpstream reads every upstream image through a port-forward and
		// checks its digest against the data set, then reads the PullThrough images
		// through docker-proxy.
		timed(t, when+": restore the upstream", func() error { return RestoreUpstream(ctx, b.deps, b.restURL, b.p, out) })
		b.requireUpstreamState(ctx, t, UpstreamReady, when)
		b.pullThroughServed(ctx, t, when+", upstream restored")
		if first {
			b.uncachedServedAgain(ctx, t, when+", upstream restored")
			img := uncachedImage(t)
			proxyWant = append(proxyWant, datasetKey(img.Name, img.Tag))
			slices.Sort(proxyWant)
		}
		proxyKeysAre(ctx, t, b.admin, proxyWant, when+", upstream restored")
	}

	cut("first", true)
	restore("first", true)
	cut("second", false)
	restore("second", false)
}

// TestIntegrationDevDeskOnACutUpstream measures what DevDesk itself shows for the
// proxy member of a Nexus group when the upstream is cut (NX-04, DEVDESK-580 step
// 11.8). It is a measure, not a fixture: a fixture is replayed at once, and
// cannot give back the delay the user waits. The first read of an image the proxy
// does not hold takes Nexus 20 s to answer 502 (H-CUTDELAY), while the registry
// client of DevDesk gives up after 15 s: on the real bench DevDesk therefore sees
// a deadline, not the 502 that the recorded fixture holds (cut1-catalog-proxy-path).
//
// It cuts the upstream, walks the catalog of docker-proxy by its path with the
// CatalogSearcher the registry browser uses (the same Target as a Nexus group
// member: the API URL of the HTTP port, the prefix docker-proxy, the catalog read
// under it), then walks it again once Nexus has answered its own 502 and blocked
// the proxy, and logs for each the verdict, the typed cause and the duration. The
// proxy is not blocked at the start: run it right after a reset, never after
// another read of an uncached image. It asserts no verdict, the numbers are the
// result; it fails only when the bench is not in the state it needs. The upstream
// is restored when the test ends, whatever the outcome; the bench is not reset.
// The PUT of the restore lifts the proxy block (H-UNBLOCK in
// docs/architecture/nexus-bench.md); without it Nexus unblocks the proxy by
// itself, measured in NX-02b at 21 to 79 s after the cut. `mise run
// nexus-bench:reset` also lifts what a restore leaves of the cache. That the proxy
// is not blocked at the start is not asserted: Nexus exposes no read-only signal
// of it (only a read of an uncached image shows it, and that read both writes to
// the cache and may block the proxy), so the precondition stays "run it right
// after a reset".
func TestIntegrationDevDeskOnACutUpstream(t *testing.T) {
	requireBench(t)
	requireScenarios(t)
	b := openScenarioBench(t)
	ctx, out := context.Background(), logWriter{t}

	requireInitialDataset(ctx, t, b.admin)
	b.requireUpstreamState(ctx, t, UpstreamReady, "before the cut")

	// Registered before the cut so that a failure after the scale still brings the
	// upstream back.
	t.Cleanup(func() {
		t.Log("cleanup: restore the upstream")
		if err := RestoreUpstream(context.Background(), b.deps, b.restURL, b.p, out); err != nil {
			t.Errorf("the restore failed, the upstream may stay cut: run `mise run nexus-bench:restore-upstream` (%v)", err)
		}
	})
	timed(t, "cut the upstream", func() error { return CutUpstream(ctx, b.runner, b.deps.Clock, b.p, out) })
	b.requireUpstreamState(ctx, t, UpstreamCut, "after the cut")

	tg := imagesearch.Target{
		EntryKey: "bench", APIURL: b.restURL, RepoPrefix: dockerProxyRepository, CatalogUnderPrefix: true,
		Username: adminUserID, Password: b.secrets.AdminPassword,
	}
	search := func(label string) time.Duration {
		start := time.Now()
		// The context only bounds the test: the 15 s of the registry client are
		// what ends the read.
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		imgs, partial, err := imagesearch.CatalogSearcher{}.Search(sctx, tg, imagesearch.Query{Text: "mirror"})
		took := time.Since(start)
		outcome := imagesearch.Classify(err, len(imgs), partial)
		var se *oci.StatusError
		var ne net.Error
		var cause string
		switch {
		case err == nil:
			cause = "none"
		case errors.As(err, &se):
			cause = "oci.StatusError, code " + strconv.Itoa(se.Code) + " " + http.StatusText(se.Code) + ", source " + strconv.Itoa(int(se.Source))
		case errors.As(err, &ne) && ne.Timeout(), errors.Is(err, context.DeadlineExceeded):
			cause = "deadline (net timeout)"
		default:
			cause = "other"
		}
		t.Logf("MEASURE %s: verdict %q (partial %v), %d rows, cause: %s, took %s", label, outcome.Verdict, outcome.Partial, len(imgs), cause, took.Round(100*time.Millisecond))
		if err != nil {
			t.Logf("MEASURE %s: error text: %v", label, err)
		}
		return took
	}

	first := search("first read after the cut")
	// Nexus answers its own 502 about 20 s after the request and then blocks the
	// proxy: wait for that, whatever the client did, before reading again.
	if wait := 25*time.Second - first; wait > 0 {
		if err := b.deps.Clock.Sleep(ctx, wait); err != nil {
			t.Fatal(err)
		}
	}
	search("second read, once Nexus has answered and blocked the proxy")
}

// ---- the reset ---------------------------------------------------------------------------

// benchIdentity is what the reset must leave alone, or, for the upstream claim,
// replace: the UIDs of the objects of the instance, and the restarts of its pod.
type benchIdentity struct {
	NamespaceUID     string
	NexusPodUID      string
	NexusRestarts    int
	NexusClaimUID    string
	UpstreamClaimUID string
}

// kubectlText runs kubectl and returns its standard output, trimmed.
func kubectlText(ctx context.Context, t *testing.T, r Runner, args ...string) string {
	t.Helper()
	stdout, stderr, err := r.Run(ctx, nil, args...)
	if err != nil {
		t.Fatalf("kubectl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(stderr)))
	}
	return strings.TrimSpace(string(stdout))
}

// readIdentity reads the UIDs of the namespace, the Nexus pod and the two claims,
// and the restart count of the Nexus pod. It fails unless exactly one Nexus pod
// exists, as the two readings must name the same pod.
func (b scenarioBench) readIdentity(ctx context.Context, t *testing.T) benchIdentity {
	t.Helper()
	ns := b.p.Namespace
	id := benchIdentity{
		NamespaceUID:     kubectlText(ctx, t, b.runner, "get", "namespace", ns, "-o", "jsonpath={.metadata.uid}"),
		NexusClaimUID:    kubectlText(ctx, t, b.runner, "get", "pvc", nexusClaimName, "-n", ns, "-o", "jsonpath={.metadata.uid}"),
		UpstreamClaimUID: kubectlText(ctx, t, b.runner, "get", "pvc", upstreamClaimName, "-n", ns, "-o", "jsonpath={.metadata.uid}"),
	}
	var pods struct {
		Items []struct {
			Metadata struct {
				UID string `json:"uid"`
			} `json:"metadata"`
			Status struct {
				ContainerStatuses []struct {
					RestartCount int `json:"restartCount"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	raw := kubectlText(ctx, t, b.runner, "get", "pods", "-n", ns, "-l", podSelector, "-o", "json")
	if err := json.Unmarshal([]byte(raw), &pods); err != nil {
		t.Fatalf("read the Nexus pod: %v", err)
	}
	if len(pods.Items) != 1 {
		t.Fatalf("%d pods match %s in %s, want exactly one", len(pods.Items), podSelector, ns)
	}
	id.NexusPodUID = pods.Items[0].Metadata.UID
	for _, cs := range pods.Items[0].Status.ContainerStatuses {
		id.NexusRestarts += cs.RestartCount
	}
	for name, uid := range map[string]string{"namespace": id.NamespaceUID, "Nexus pod": id.NexusPodUID, nexusClaimName: id.NexusClaimUID, upstreamClaimName: id.UpstreamClaimUID} {
		if uid == "" {
			t.Fatalf("kubectl gave no UID for the %s", name)
		}
	}
	return id
}

// requireWrongUIDDeleteRefused sends the delete the library sends for the claim
// of the upstream, with a precondition on a UID the claim does not have: the API
// server has to answer a conflict and leave the claim as it is. It checks the
// precondition mechanism Reset relies on (kubectl delete --raw with a
// DeleteOptions body); it can only target the claim of the upstream.
func (b scenarioBench) requireWrongUIDDeleteRefused(ctx context.Context, t *testing.T, claimUID string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"kind":          "DeleteOptions",
		"apiVersion":    "v1",
		"preconditions": map[string]string{"uid": unknownUID},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/namespaces/" + b.p.Namespace + "/persistentvolumeclaims/" + upstreamClaimName
	_, stderr, runErr := b.runner.Run(ctx, body, "delete", "--raw", path, "-f", "-", applyTimeout)
	after := kubectlText(ctx, t, b.runner, "get", "pvc", upstreamClaimName, "-n", b.p.Namespace, "-o", "jsonpath={.metadata.uid}")
	if after != claimUID {
		t.Fatalf("the claim %s has the UID %s after a delete with a wrong UID, it had %s: the claim was deleted or replaced, run `mise run nexus-bench:reset`", upstreamClaimName, after, claimUID)
	}
	if runErr == nil {
		t.Fatalf("a delete of %s with the UID %s in its precondition was accepted: the precondition does not protect the claim", upstreamClaimName, unknownUID)
	}
	if text := strings.ToLower(string(stderr)); !strings.Contains(text, "conflict") && !strings.Contains(text, "precondition") {
		t.Errorf("a delete with a wrong UID failed, but not with a conflict: %s", strings.TrimSpace(string(stderr)))
	}
}

// withUpstream runs fn with the local address of a port-forward to the upstream,
// and stops the forward before it returns.
func (b scenarioBench) withUpstream(ctx context.Context, t *testing.T, fn func(local string)) {
	t.Helper()
	local, stop, err := b.deps.Forwarder.Forward(ctx, b.p.Namespace, upstreamServiceName, upstreamServicePort)
	if err != nil {
		t.Fatalf("port-forward to the upstream: %v", err)
	}
	if stop != nil {
		defer func() { _ = stop() }()
	}
	if err := checkLoopbackForward(local); err != nil {
		t.Fatal(err)
	}
	fn(local)
}

// waitListed fails unless repo lists name:tag within scenarioListWait.
func (b scenarioBench) waitListed(ctx context.Context, t *testing.T, repo, name, tag string) {
	t.Helper()
	deadline := b.deps.Clock.Now().Add(scenarioListWait)
	for {
		keys, _ := listedKeys(ctx, t, b.admin, repo)
		if slices.Contains(keys, datasetKey(name, tag)) {
			return
		}
		if !b.deps.Clock.Now().Before(deadline) {
			t.Fatalf("%s does not list %s within %s of the read or push that should have added it", repo, datasetKey(name, tag), scenarioListWait)
		}
		if err := b.deps.Clock.Sleep(ctx, scenarioListInterval); err != nil {
			t.Fatal(err)
		}
	}
}

// pollute puts what the data set does not hold in the bench: the tag
// api:9.9.9 in docker-hosted (a copy of api:1.0.0, with the admin account) and the
// image extra/offset:9.9.9 in the upstream (a copy of mirror/uncached:1.0, inside
// the upstream, so docker-proxy never reads it).
func (b scenarioBench) pollute(ctx context.Context, t *testing.T) {
	t.Helper()
	admin := &Credentials{User: adminUserID, Password: b.secrets.AdminPassword}
	src := DatasetImage{Name: "api", Tag: "1.0.0"}
	extra := DatasetImage{Name: "api", Tag: pollutedTag}
	err := b.deps.Copier.Copy(ctx,
		nexusImageRef(b.p, dockerHostedRepository, src), nexusImageRef(b.p, dockerHostedRepository, extra),
		CopyOptions{SrcCreds: admin, DestCreds: admin, PreserveDigests: true})
	if err != nil {
		t.Fatalf("add %s to %s: %v", datasetKey(extra.Name, extra.Tag), dockerHostedRepository, err)
	}
	b.waitListed(ctx, t, dockerHostedRepository, extra.Name, extra.Tag)

	b.withUpstream(ctx, t, func(local string) {
		from := DatasetImage{Name: "mirror/uncached", Tag: "1.0"}
		to := DatasetImage{Name: pollutedUpstreamName, Tag: pollutedTag}
		if err := b.deps.Copier.Copy(ctx, upstreamImageRef(local, from), upstreamImageRef(local, to), CopyOptions{PreserveDigests: true}); err != nil {
			t.Fatalf("add %s to the upstream: %v", datasetKey(to.Name, to.Tag), err)
		}
	})
}

// readPollutedThroughProxy reads extra/offset:9.9.9 through docker-proxy with the
// bench-pull account, so that the proxy caches it and lists it: the image is in the
// upstream (pollute put it there), outside the data set, so the other tests and the
// F9 rule are left alone. The closing Reset deletes and re-creates the proxy
// repository, which has to forget it.
func (b scenarioBench) readPollutedThroughProxy(ctx context.Context, t *testing.T) {
	t.Helper()
	extra := DatasetImage{Name: pollutedUpstreamName, Tag: pollutedTag}
	got, err := b.deps.Copier.ManifestDigest(ctx, nexusImageRef(b.p, dockerProxyRepository, extra), &b.pull)
	if err != nil {
		t.Fatalf("reading %s through %s: %v", datasetKey(extra.Name, extra.Tag), dockerProxyRepository, err)
	}
	// pollute copies mirror/uncached:1.0 and keeps its digest.
	if want := uncachedImage(t).Digest; got != want {
		t.Fatalf("%s serves %s with the digest %s, the copy it was made from has %s", dockerProxyRepository, datasetKey(extra.Name, extra.Tag), got, want)
	}
	b.waitListed(ctx, t, dockerProxyRepository, extra.Name, extra.Tag)
}

// upstreamProblems reads the upstream by a port-forward and tells what differs
// from the data set: an image of the upstream with another digest or absent, and
// the image outside the data set, which has to be gone.
func (b scenarioBench) upstreamProblems(ctx context.Context, t *testing.T) []string {
	t.Helper()
	var problems []string
	b.withUpstream(ctx, t, func(local string) {
		for _, img := range Dataset() {
			if img.Location != LocationUpstream {
				continue
			}
			got, err := b.deps.Copier.ManifestDigest(ctx, upstreamImageRef(local, img), nil)
			switch {
			case err != nil:
				problems = append(problems, "reading "+datasetKey(img.Name, img.Tag)+" in the upstream: "+err.Error())
			case got != img.Digest:
				problems = append(problems, "the upstream holds "+got+" for "+datasetKey(img.Name, img.Tag)+", the data set says "+img.Digest)
			}
		}
		extra := DatasetImage{Name: pollutedUpstreamName, Tag: pollutedTag}
		if _, err := b.deps.Copier.ManifestDigest(ctx, upstreamImageRef(local, extra), nil); !errors.Is(err, errImageAbsent) {
			problems = append(problems, datasetKey(extra.Name, extra.Tag)+" is still in the upstream (read: "+errText(err)+")")
		}
	})
	return problems
}

// errText is the text of err, "no error" for nil.
func errText(err error) string {
	if err == nil {
		return "no error"
	}
	return err.Error()
}

func TestIntegrationResetKeepsInstance(t *testing.T) {
	requireBench(t)
	requireScenarios(t)
	b := openScenarioBench(t)
	ctx, out := context.Background(), logWriter{t}

	requireInitialDataset(ctx, t, b.admin)
	before := b.readIdentity(ctx, t)
	t.Logf("before: namespace %s, Nexus pod %s (%d restarts), %s %s, %s %s",
		before.NamespaceUID, before.NexusPodUID, before.NexusRestarts, nexusClaimName, before.NexusClaimUID, upstreamClaimName, before.UpstreamClaimUID)

	// A reset that fails before it finishes leaves the bench changed: run one when
	// the test ends before its own succeeded.
	reset := false
	b.resetOnCleanup(t, func() bool { return reset })

	// Nothing else is changed when the precondition does not hold: stop here.
	if !t.Run("a delete of the claim with a wrong UID is refused", func(t *testing.T) {
		b.requireWrongUIDDeleteRefused(ctx, t, before.UpstreamClaimUID)
	}) {
		t.FailNow()
	}
	b.pollute(ctx, t)
	b.readPollutedThroughProxy(ctx, t)

	var seed SeedResult
	timed(t, "reset", func() error {
		_, res, err := Reset(ctx, b.deps, b.restURL, b.p, out)
		seed = res
		return err
	})
	reset = true

	t.Run("the proxy forgot the image it had cached", func(t *testing.T) {
		got, _ := listedKeys(ctx, t, b.admin, dockerProxyRepository)
		if key := datasetKey(pollutedUpstreamName, pollutedTag); slices.Contains(got, key) {
			t.Errorf("%s still lists %s after the reset: the repository was not re-created empty (%v)", dockerProxyRepository, key, got)
		}
	})

	t.Run("the data set is back to its initial state", func(t *testing.T) {
		for _, problem := range datasetProblems(ctx, t, b.admin) {
			t.Error(problem)
		}
		for _, problem := range b.upstreamProblems(ctx, t) {
			t.Error(problem)
		}
		if !reflect.DeepEqual(seed.Images, Dataset()) {
			t.Errorf("the reset seeded %+v, want the data set %+v", seed.Images, Dataset())
		}
	})

	t.Run("the instance is the same", func(t *testing.T) {
		after := b.readIdentity(ctx, t)
		t.Logf("after: namespace %s, Nexus pod %s (%d restarts), %s %s, %s %s",
			after.NamespaceUID, after.NexusPodUID, after.NexusRestarts, nexusClaimName, after.NexusClaimUID, upstreamClaimName, after.UpstreamClaimUID)
		if after.NamespaceUID != before.NamespaceUID {
			t.Errorf("the namespace was replaced: UID %s, was %s", after.NamespaceUID, before.NamespaceUID)
		}
		if after.NexusPodUID != before.NexusPodUID || after.NexusRestarts != before.NexusRestarts {
			t.Errorf("the Nexus pod was restarted or replaced: UID %s with %d restarts, was %s with %d", after.NexusPodUID, after.NexusRestarts, before.NexusPodUID, before.NexusRestarts)
		}
		if after.NexusClaimUID != before.NexusClaimUID {
			t.Errorf("%s was replaced: UID %s, was %s", nexusClaimName, after.NexusClaimUID, before.NexusClaimUID)
		}
		if after.UpstreamClaimUID == before.UpstreamClaimUID {
			t.Errorf("%s was not replaced: its UID is still %s, so the upstream was not emptied by a new volume", upstreamClaimName, after.UpstreamClaimUID)
		}
	})

	t.Run("the stored passwords are unchanged and accepted", func(t *testing.T) {
		// Same file, read again: a reset never changes a password.
		secrets, found, err := b.deps.Store.Load()
		if err != nil || !found {
			t.Fatalf("read the secrets again (found %v): %v", found, err)
		}
		if !reflect.DeepEqual(secrets, b.secrets) {
			t.Error("the stored secrets are not the ones the reset started with")
		}
		admin := newClient(b.restURL, adminUserID, secrets.AdminPassword)
		defer admin.closeIdle()
		if err := admin.requireAdmin(ctx); err != nil {
			t.Errorf("the admin password is not accepted after the reset: %v", err)
		}
		pull := newClient(b.restURL, pullUserID, secrets.PullPassword)
		defer pull.closeIdle()
		resp, err := pull.do(ctx, request{op: "list the repositories as " + pullUserID, method: http.MethodGet, path: restAPI + "/repositories"})
		if err != nil {
			t.Fatal(err)
		}
		if resp.status != http.StatusOK {
			t.Errorf("GET /repositories as %s = HTTP %d after the reset, want 200", pullUserID, resp.status)
		}
	})
}

// ---- the access variants (DEVDESK-580, PR4, W5 and W6) -------------------------------

const (
	// dockerTokenRealm is the Docker Bearer Token Realm of the bench.
	dockerTokenRealm = "DockerToken"
	// accessCampaign is the campaign the two variants record.
	accessCampaign = "access"
	// realmOffPrefix and anonymousOffPrefix select, by the name of the fixture, what
	// each variant records: the run declares no state (StateAny), the name tells the
	// fixtures of the realm-off bench from those of the anonymous-off one.
	realmOffPrefix       = "w5-"
	anonymousOffPrefix   = "w6-"
	accessRestoreTimeout = 10 * time.Minute
)

// readAnonymousEnabled reads the anonymous access setting as admin.
func (b scenarioBench) readAnonymousEnabled(ctx context.Context, t *testing.T) bool {
	t.Helper()
	var got struct {
		Enabled bool `json:"enabled"`
	}
	if err := b.admin.read(ctx, "read the anonymous access", anonymousPath, nil, &got); err != nil {
		t.Fatalf("read the anonymous access: %v", err)
	}
	return got.Enabled
}

// requireNominalAccess fails unless the bench is as `status` reports a nominal one:
// the data set is the descriptor's, both required realms are active and the
// anonymous access is what the parameters say.
func (b scenarioBench) requireNominalAccess(ctx context.Context, t *testing.T, when string) {
	t.Helper()
	if problems := datasetProblems(ctx, t, b.admin); len(problems) > 0 {
		t.Fatalf("%s: the data set is not the descriptor's (%s): %s", when, strings.Join(problems, "; "), resetCheck)
	}
	realms, err := ActiveRealms(ctx, b.restURL, b.deps.Store, b.p)
	if err != nil {
		t.Fatalf("%s: read the active realms: %v", when, err)
	}
	for _, want := range requiredRealms {
		if !slices.Contains(realms, want) {
			t.Fatalf("%s: the realm %s is not active (active: %v): run `mise run nexus-bench:provision`", when, want, realms)
		}
	}
	if got := b.readAnonymousEnabled(ctx, t); got != b.p.Anonymous {
		t.Fatalf("%s: anonymous access is %v, the parameters say %v: run `mise run nexus-bench:provision`", when, got, b.p.Anonymous)
	}
}

// recordAccess runs the recorder in-process on the fixtures of the access campaign
// whose name starts with prefix, whatever their declared state. It writes them
// under the source tree like `nexus-bench record` does, and fails when none was
// recorded: a missing fixture must not pass for a recording.
func recordAccess(ctx context.Context, t *testing.T, b scenarioBench, prefix string, asAdmin bool) {
	t.Helper()
	requireBench(t)
	desc, err := LoadDescriptor(DescriptorPath())
	if err != nil {
		t.Fatalf("LoadDescriptor: %v", err)
	}
	dir, err := nexusfixture.SourceDir()
	if err != nil {
		t.Fatalf("fixtures folder: %v", err)
	}
	var deny []string
	if file := os.Getenv("DEVDESK_FIXTURE_DENYLIST"); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read the deny list: %v", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				deny = append(deny, line)
			}
		}
	}
	report, err := RecordFixtures(ctx, RecordOptions{
		Origins:        RecordOrigins(desc, ""),
		Descriptor:     desc,
		Secrets:        b.secrets,
		Dir:            dir,
		Campaigns:      []string{accessCampaign},
		Only:           prefix + "*",
		DenyTerms:      deny,
		State:          StateAny,
		DatasetAsAdmin: asAdmin,
		RequestTimeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("record %s/%s*: %v", accessCampaign, prefix, err)
	}
	t.Logf("recorded %s/%s*: written %v, unchanged %v, skipped %v", accessCampaign, prefix, report.Written, report.Unchanged, report.Skipped)
	if len(report.Written)+len(report.Unchanged) == 0 {
		t.Fatalf("no fixture named %s* in the campaign %s was recorded: the campaign holds none, or each is skipped", prefix, accessCampaign)
	}
}

// TestIntegrationDevDeskAccessVariants records the two fixtures sets of NX-05 that
// need the bench changed: W5 (the Docker Bearer Token Realm removed from the active
// realms, fixtures named w5-*) then W6 (anonymous access cut, fixtures named w6-*,
// the data set compared as admin). It writes nothing else on the bench: no W7 and
// no W8. Provision, which puts the bench back, is registered before the first write
// and runs again whatever the outcome; the test ends by checking that the bench is
// nominal again. It needs the same environment as the other scenarios.
func TestIntegrationDevDeskAccessVariants(t *testing.T) {
	requireBench(t)
	requireScenarios(t)
	b := openScenarioBench(t)
	ctx, out := context.Background(), logWriter{t}

	b.requireNominalAccess(ctx, t, "before the variants")
	original, err := ActiveRealms(ctx, b.restURL, b.deps.Store, b.p)
	if err != nil {
		t.Fatalf("read the active realms: %v", err)
	}

	// restored is true while the bench is known to be nominal: it is cleared before
	// each write, and set once Provision has put the bench back.
	restored := true
	restore := func(ctx context.Context) error {
		rctx, cancel := context.WithTimeout(ctx, accessRestoreTimeout)
		defer cancel()
		initial := NewKubectlInitialPassword(b.runner, b.p.Namespace)
		if _, err := Provision(rctx, b.restURL, initial, b.deps.Store, b.p, out); err != nil {
			return err
		}
		restored = true
		return nil
	}
	// Registered before the first write, so that a failure half-way still puts the
	// settings back.
	t.Cleanup(func() {
		if restored {
			return
		}
		t.Log("cleanup: provision, to put the realms and the anonymous access back")
		if err := restore(context.Background()); err != nil {
			t.Errorf("the closing provision failed, the bench may be changed: run `mise run nexus-bench:provision` (%v)", err)
		}
	})

	t.Run("W5 DockerToken realm off", func(t *testing.T) {
		without := slices.DeleteFunc(slices.Clone(original), func(r string) bool { return r == dockerTokenRealm })
		restored = false
		if err := SetActiveRealms(ctx, b.restURL, b.deps.Store, b.p, without); err != nil {
			t.Fatalf("remove %s from the active realms: %v", dockerTokenRealm, err)
		}
		if got, err := ActiveRealms(ctx, b.restURL, b.deps.Store, b.p); err != nil || slices.Contains(got, dockerTokenRealm) {
			t.Fatalf("the realm %s is still active after the change (active %v, error %v)", dockerTokenRealm, got, err)
		}
		recordAccess(ctx, t, b, realmOffPrefix, false)
		if err := restore(ctx); err != nil {
			t.Fatalf("provision after W5: %v", err)
		}
		b.requireNominalAccess(ctx, t, "after W5")
	})

	t.Run("W6 anonymous access off", func(t *testing.T) {
		if !restored {
			t.Fatal("W5 left the bench changed: W6 does not start before the closing provision has put it back")
		}
		restored = false
		if err := SetAnonymousAccess(ctx, b.restURL, b.deps.Store, b.p, false); err != nil {
			t.Fatalf("cut the anonymous access: %v", err)
		}
		if b.readAnonymousEnabled(ctx, t) {
			t.Fatal("anonymous access is still enabled after the cut")
		}
		recordAccess(ctx, t, b, anonymousOffPrefix, true)
		if err := restore(ctx); err != nil {
			t.Fatalf("provision after W6: %v", err)
		}
	})

	// The end state: what `status` reports of a nominal bench, and the realms as
	// they were, in any order.
	b.requireNominalAccess(ctx, t, "after the variants")
	// Compared as sets: Provision appends DockerToken at the end of the list, so
	// a variant that removed it gets it back in a different position.
	if got, err := ActiveRealms(ctx, b.restURL, b.deps.Store, b.p); err != nil || !sameRealms(got, original) {
		t.Errorf("the active realms are %v after the variants (error %v), they were %v", got, err, original)
	}
}

// sameRealms reports whether two realm lists hold the same names, in any order.
func sameRealms(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
