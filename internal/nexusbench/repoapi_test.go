package nexusbench

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The tests of the REST layer of the reset and of the restore (DEVDESK-534,
// NX-02b, step 3), against the in-memory Nexus of fakenexus_test.go, extended
// for it:
//
//	benchRepositories                                    the three repositories a reset may delete
//	(c *client) deleteRepository(ctx, name) (existed bool, err error)
//	(c *client) invalidateCache(ctx, name) error
//	(c *client) unblockProxy(ctx, p) error
//	(c *client) requireAdmin(ctx) error
//	UnblockProxy(ctx, store, baseURL, p, out) error
//	repositorySpecs(p) []repositorySpec                  provision.go, the body of repositories()
//
// Choices of the tests, to settle with the code (the plan of DEVDESK-534 gives the
// signatures, not every detail):
//   - deleteRepository sends DELETE /service/rest/v1/repositories/{name}: 204 is
//     existed == true, 404 is existed == false with no error, any other status is an
//     *APIError (the client methods return the raw error; the public functions
//     wrap it with asBenchError, as Provision does). A name outside
//     benchRepositories is refused before any request, with an error that is
//     neither an *APIError nor a transport error.
//   - benchRepositories is exactly [docker-group, docker-proxy, docker-hosted], in
//     this order: the group names the two others.
//   - invalidateCache sends POST /service/rest/v1/repositories/{name}/invalidate-cache
//     and expects a 2xx; it is not limited to the bench repositories (it writes no
//     data).
//   - unblockProxy reads the proxy (404: an error that says `nexus-bench:up`, no
//     write), PUTs the configuration of repositorySpecs(p) for docker-proxy (never a
//     configuration read back from the instance), then invalidates the cache. The
//     order GET, PUT, POST is the sequence of the plan.
//   - requireAdmin sends one GET as admin; a refused password (401 or 403) is a
//     *BenchError of kind ProvisionFailed whose message says nothing was changed.
//   - UnblockProxy refuses an upstream the bench did not deploy before any
//     request, and reports as `restore: ...` lines.
//
// What depends on the measures of step 0 (to confirm before the code is written):
//   - H-UNBLOCK: that a PUT of the proxy configuration followed by an
//     invalidate-cache is what puts the proxy back in service. If the measure says
//     the PUT alone, or the POST alone, is enough (or neither), the sequence tests
//     below (TestUnblockProxySequence*) and the fake's "blocked" state change.
//   - H-INVAL: the answer code of invalidate-cache (204 here) and whether it
//     accepts a proxy and a group but not a hosted repository (400 here).
//   - H-REPODEL: the answer codes of DELETE (204 and 404 here) and the shape of a
//     recreated repository (empty at once here, see KeepComponentsAfterDelete).

// provisionedBench is a bench whose Nexus is provisioned against the upstream the
// bench deploys, the way a bench after "up" is: the three repositories, an admin
// password the store holds, the upstream allowed in the SSRF protection.
func provisionedBench(t *testing.T) *bench {
	t.Helper()
	b := newBench(t)
	b.p.UpstreamURL = upstreamFor(b.p.Namespace)
	if !b.p.UpstreamIsBench() {
		t.Fatalf("the bench upstream %q is not the bench's", b.p.UpstreamURL)
	}
	b.mustRun()
	b.ip.ok = false
	return b
}

// admin is a client authenticated with the admin password the store holds.
func (b *bench) admin() *client {
	b.t.Helper()
	c := newClient(b.nexus.URL(), adminUserID, b.store.current().AdminPassword)
	b.t.Cleanup(c.closeIdle)
	return c
}

// addImages gives hosted and proxy a component each.
func (b *bench) addImages() {
	b.t.Helper()
	digest := "sha256:" + strings.Repeat("a", 64)
	b.nexus.AddComponents(dockerHostedRepository, fakeComponent{Name: "api", Version: "1.0", Digest: digest})
	b.nexus.AddComponents(dockerProxyRepository, fakeComponent{Name: "mirror/cached", Version: "1.0", Digest: digest})
}

// calls is "METHOD path" of every request, in order.
func calls(reqs []fakeReq) []string {
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

const invalidateSuffix = "/invalidate-cache"

func deletePath(name string) string { return pathRepositories + "/" + name }

func invalidatePath(name string) string { return pathRepositories + "/" + name + invalidateSuffix }

// proxyConfigOf is what repositorySpecs says docker-proxy is, decoded the way the
// instance decodes a body.
func proxyConfigOf(t *testing.T, p Params) map[string]any {
	t.Helper()
	for _, spec := range repositorySpecs(p) {
		if spec.name == dockerProxyRepository {
			if spec.kind != "proxy" {
				t.Fatalf("the spec of %s has kind %q, want proxy", spec.name, spec.kind)
			}
			return deepCopy(spec.config)
		}
	}
	t.Fatalf("repositorySpecs has no %s", dockerProxyRepository)
	return nil
}

// requireBenchError checks err is a complete *BenchError: a cause and what to
// check. It is not requireCleanError, which bans the word "password": the refusal of
// an account says so in the messages that already ship (asBenchError), and the
// secrets themselves are checked by bench.requireQuiet.
func requireBenchError(t *testing.T, err error) *BenchError {
	t.Helper()
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Cause == "" || be.Check == "" {
		t.Errorf("the BenchError is incomplete: cause %q, check %q", be.Cause, be.Check)
	}
	return be
}

func requireAPIStatus(t *testing.T, err error, status int) *APIError {
	t.Helper()
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("error %T %v, want an *APIError", err, err)
	}
	if api.Status != status {
		t.Errorf("status = %d, want %d", api.Status, status)
	}
	return api
}

// ---- the fake itself --------------------------------------------------------

func TestFakeNexusDeletesARepositoryByName(t *testing.T) {
	b := provisionedBench(t)
	b.addImages()
	c := b.admin()
	do := func(method, path string) int {
		t.Helper()
		resp, err := c.do(context.Background(), request{op: "fake " + method, method: method, path: path})
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp.status
	}
	if got := do(http.MethodDelete, deletePath(dockerHostedRepository)); got != http.StatusNoContent {
		t.Fatalf("DELETE hosted = %d, want 204", got)
	}
	if _, ok := b.nexus.Repo("hosted", dockerHostedRepository); ok {
		t.Error("the hosted repository is still there after its DELETE")
	}
	if n := b.nexus.ComponentCount(dockerHostedRepository); n != 0 {
		t.Errorf("%d components left after the DELETE, want 0", n)
	}
	if n := b.nexus.ComponentCount(dockerProxyRepository); n != 1 {
		t.Errorf("the components of another repository changed: %d, want 1", n)
	}
	if got := do(http.MethodDelete, deletePath(dockerHostedRepository)); got != http.StatusNotFound {
		t.Errorf("second DELETE = %d, want 404", got)
	}
	if got := do(http.MethodDelete, deletePath("no-such-repository")); got != http.StatusNotFound {
		t.Errorf("DELETE of an unknown repository = %d, want 404", got)
	}
	if got := do(http.MethodGet, deletePath(dockerProxyRepository)); got == http.StatusOK {
		t.Errorf("GET %s answered 200: the route is DELETE only", deletePath(dockerProxyRepository))
	}
}

func TestFakeNexusInvalidateCacheAnswers(t *testing.T) {
	b := provisionedBench(t)
	c := b.admin()
	tests := []struct {
		name string
		repo string
		want int
	}{
		{"a proxy", dockerProxyRepository, http.StatusNoContent},
		{"a group", dockerGroupRepository, http.StatusNoContent},
		{"a hosted repository has no cache", dockerHostedRepository, http.StatusBadRequest},
		{"an unknown repository", "no-such-repository", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := c.do(context.Background(), request{op: "fake invalidate", method: http.MethodPost, path: invalidatePath(tt.repo)})
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			if resp.status != tt.want {
				t.Errorf("status = %d, want %d", resp.status, tt.want)
			}
		})
	}
}

func TestFakeNexusBlockedProxyAndNegativeCache(t *testing.T) {
	b := provisionedBench(t)
	c := b.admin()
	b.nexus.AutoBlockProxy()
	b.nexus.MarkNegativeCached(dockerProxyRepository)
	if !b.nexus.ProxyBlocked() || !b.nexus.NegativeCached(dockerProxyRepository) {
		t.Fatal("the fixtures did not set the blocked state and the negative cache")
	}

	if _, err := c.do(context.Background(), request{op: "fake invalidate", method: http.MethodPost, path: invalidatePath(dockerProxyRepository)}); err != nil {
		t.Fatalf("POST invalidate-cache: %v", err)
	}
	if b.nexus.NegativeCached(dockerProxyRepository) {
		t.Error("invalidate-cache left the negative cache full")
	}
	if !b.nexus.ProxyBlocked() {
		t.Error("invalidate-cache lifted the blocked state: only a PUT of the proxy does")
	}

	cfg := proxyConfigOf(t, b.p)
	if err := c.write(context.Background(), "fake put", http.MethodPut, repoPath("proxy", dockerProxyRepository), cfg); err != nil {
		t.Fatalf("PUT of the proxy: %v", err)
	}
	if b.nexus.ProxyBlocked() {
		t.Error("a PUT of the proxy left it blocked")
	}
}

func TestFakeNexusKeepsComponentsAfterADeleteForNListings(t *testing.T) {
	b := provisionedBench(t)
	b.addImages()
	b.nexus.KeepComponentsAfterDelete(2)
	c := b.admin()
	ctx := context.Background()
	if _, err := c.deleteRepository(ctx, dockerHostedRepository); err != nil {
		t.Fatalf("deleteRepository: %v", err)
	}
	// Recreate it the way Provision does; the old component still lists.
	b.ip.ok = false
	b.mustRun()
	var counts []int
	for range 4 {
		list, err := c.listComponents(ctx, dockerHostedRepository)
		if err != nil {
			t.Fatalf("listComponents: %v", err)
		}
		counts = append(counts, len(list))
	}
	if want := []int{1, 1, 0, 0}; !slices.Equal(counts, want) {
		t.Errorf("listings after the delete show %v components, want %v", counts, want)
	}
}

// ---- benchRepositories ------------------------------------------------------

func TestBenchRepositoriesAreTheThreeOfTheBenchGroupFirst(t *testing.T) {
	want := []string{dockerGroupRepository, dockerProxyRepository, dockerHostedRepository}
	if !slices.Equal(benchRepositories, want) {
		t.Errorf("benchRepositories = %q, want %q: the group first, as it names the two others", benchRepositories, want)
	}
	// They are the repositories Provision creates, no more.
	var provisioned []string
	for _, spec := range repositorySpecs(Defaults()) {
		provisioned = append(provisioned, spec.name)
	}
	if !slices.Equal(sorted(provisioned), sorted(benchRepositories)) {
		t.Errorf("repositorySpecs names %q, benchRepositories %q", provisioned, benchRepositories)
	}
}

func TestRepositorySpecsAreWhatProvisionWrites(t *testing.T) {
	// The refactor of repositories() into a function of Params must not change a
	// byte of what Provision sends: the fake keeps what it received.
	b := provisionedBench(t)
	for _, spec := range repositorySpecs(b.p) {
		got := requireRepo(t, b.nexus, spec.kind, spec.name)
		if !reflect.DeepEqual(got, deepCopy(spec.config)) {
			t.Errorf("%s: the instance holds %v, repositorySpecs says %v", spec.name, got, spec.config)
		}
	}
	proxy := proxyConfigOf(t, b.p)
	if jsonAt(proxy, "docker", "pathEnabled") != true {
		t.Errorf("the proxy spec lacks docker.pathEnabled: true (H-PATH): %v", proxy)
	}
	if jsonAt(proxy, "httpClient", "autoBlock") != true {
		t.Errorf("the proxy spec lacks httpClient.autoBlock: true (the configuration of NX-01 is kept): %v", proxy)
	}
	if got := jsonAt(proxy, "proxy", "remoteUrl"); got != b.p.UpstreamURL {
		t.Errorf("proxy.remoteUrl = %v, want the upstream %q", got, b.p.UpstreamURL)
	}
}

// ---- deleteRepository -------------------------------------------------------

func TestDeleteRepositoryRemovesEachBenchRepository(t *testing.T) {
	kinds := map[string]string{dockerGroupRepository: "group", dockerProxyRepository: "proxy", dockerHostedRepository: "hosted"}
	for _, name := range benchRepositories {
		t.Run(name, func(t *testing.T) {
			b := provisionedBench(t)
			b.addImages()
			before := len(b.nexus.Requests())
			existed, err := b.admin().deleteRepository(context.Background(), name)
			if err != nil {
				t.Fatalf("deleteRepository(%s): %v", name, err)
			}
			if !existed {
				t.Errorf("existed = false for a repository that was there")
			}
			if _, ok := b.nexus.Repo(kinds[name], name); ok {
				t.Errorf("%s is still on the instance", name)
			}
			if n := b.nexus.ComponentCount(name); n != 0 {
				t.Errorf("%d components of %s survived its deletion", n, name)
			}
			if got, want := calls(b.requestsFrom(before)), []string{"DELETE " + deletePath(name)}; !slices.Equal(got, want) {
				t.Errorf("requests = %q, want exactly %q", got, want)
			}
			if r := b.requestsFrom(before)[0]; r.User != adminUserID || r.EmptyPassword {
				t.Errorf("the DELETE was sent as %q (empty password: %v), want admin with its password", r.User, r.EmptyPassword)
			}
			for _, other := range benchRepositories {
				if other == name {
					continue
				}
				if _, ok := b.nexus.Repo(kinds[other], other); !ok {
					t.Errorf("deleting %s removed %s as well", name, other)
				}
			}
		})
	}
}

func TestDeleteRepositoryAnAbsentRepositoryIsNotAnError(t *testing.T) {
	b := provisionedBench(t)
	c := b.admin()
	ctx := context.Background()
	if _, err := c.deleteRepository(ctx, dockerProxyRepository); err != nil {
		t.Fatalf("first deleteRepository: %v", err)
	}
	existed, err := c.deleteRepository(ctx, dockerProxyRepository)
	if err != nil {
		t.Fatalf("second deleteRepository: %v (a 404 means it is already done)", err)
	}
	if existed {
		t.Error("existed = true for a repository that was already deleted")
	}
}

func TestDeleteRepositoryRefusesAnyNameOutsideTheBench(t *testing.T) {
	names := []string{
		"maven-releases", "nuget-hosted", "", "nexus", "DOCKER-HOSTED", "docker-hosted2", "docker-", " docker-hosted",
		"docker-hosted/", "../security/users/admin", "docker/hosted/docker-hosted", "docker-hosted?x=1", "docker-hosted#x",
		"docker-hosted/../docker-proxy", "docker-hosted%2F..%2Fx",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			b := provisionedBench(t)
			before := len(b.nexus.Requests())
			existed, err := b.admin().deleteRepository(context.Background(), name)
			if err == nil {
				t.Fatalf("deleteRepository(%q) succeeded: only the three bench repositories may be deleted", name)
			}
			if existed {
				t.Errorf("existed = true for a refused name")
			}
			var api *APIError
			var transport *transportError
			if errors.As(err, &api) || errors.As(err, &transport) {
				t.Errorf("the refusal of %q is a REST error (%T): it has to come before any request", name, err)
			}
			if sent := b.requestsFrom(before); len(sent) != 0 {
				t.Errorf("%d request(s) sent for the refused name %q: %q", len(sent), name, calls(sent))
			}
			if n := b.nexus.RepoCount(); n != 3 {
				t.Errorf("%d repositories left after the refusal of %q, want 3", n, name)
			}
		})
	}
}

func TestDeleteRepositoryReportsAnswersOutsideTheNominalOnes(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(b *bench)
		status  int // the *APIError status; 0 means a transport error
	}{
		{"forbidden", func(b *bench) { b.nexus.Forbid(http.MethodDelete, pathRepositories+"/", http.StatusForbidden) }, http.StatusForbidden},
		{"unauthorized", func(b *bench) { b.nexus.Forbid(http.MethodDelete, pathRepositories+"/", http.StatusUnauthorized) }, http.StatusUnauthorized},
		{"the instance fails", func(b *bench) {
			b.nexus.Forbid(http.MethodDelete, pathRepositories+"/", http.StatusInternalServerError)
		}, http.StatusInternalServerError},
		{"a conflict", func(b *bench) { b.nexus.Forbid(http.MethodDelete, pathRepositories+"/", http.StatusConflict) }, http.StatusConflict},
		{"the instance does not answer", func(b *bench) { b.nexus.DropConnections() }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisionedBench(t)
			tt.prepare(b)
			existed, err := b.admin().deleteRepository(context.Background(), dockerHostedRepository)
			if err == nil {
				t.Fatal("deleteRepository succeeded")
			}
			if existed {
				t.Error("existed = true with an error")
			}
			if tt.status != 0 {
				_ = requireAPIStatus(t, err, tt.status)
			} else {
				var transport *transportError
				if !errors.As(err, &transport) {
					t.Errorf("error %T, want a *transportError", err)
				}
			}
			// Once wrapped the way the public functions wrap it, it is a clean
			// BenchError: no password, no URL, no body.
			be := asBenchError(err, b.p, b.nexus.URL())
			_ = requireBenchError(t, be)
			b.requireQuiet(be)
			if msg := be.Error(); strings.Contains(msg, b.nexus.URL()) {
				t.Errorf("the message %q carries the URL of the instance", msg)
			}
			if tt.status == http.StatusForbidden || tt.status == http.StatusUnauthorized {
				var wrapped *BenchError
				if !errors.As(be, &wrapped) || wrapped.Kind != ProvisionFailed {
					t.Errorf("a refused account is not ProvisionFailed: %v", be)
				}
			}
			if tt.status == 0 {
				var wrapped *BenchError
				if !errors.As(be, &wrapped) || wrapped.Kind != RESTUnreachable {
					t.Errorf("no answer is not RESTUnreachable: %v", be)
				}
			}
		})
	}
}

func TestDeleteRepositoryNeverFollowsARedirect(t *testing.T) {
	b := provisionedBench(t)
	b.nexus.RedirectTo(http.MethodDelete, pathRepositories+"/", http.StatusTemporaryRedirect, "http://elsewhere.invalid/x")
	_, err := b.admin().deleteRepository(context.Background(), dockerHostedRepository)
	_ = requireAPIStatus(t, err, http.StatusTemporaryRedirect)
}

func TestDeleteRepositoryReturnsACancelledContext(t *testing.T) {
	b := provisionedBench(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := len(b.nexus.Requests())
	_, err := b.admin().deleteRepository(ctx, dockerHostedRepository)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one that is context.Canceled", err)
	}
	if sent := b.requestsFrom(before); len(sent) != 0 {
		t.Errorf("requests sent with a cancelled context: %q", calls(sent))
	}
}

// ---- invalidateCache --------------------------------------------------------

func TestRepoInvalidateCacheEmptiesTheNegativeCache(t *testing.T) {
	for _, name := range []string{dockerProxyRepository, dockerGroupRepository} {
		t.Run(name, func(t *testing.T) {
			b := provisionedBench(t)
			b.nexus.MarkNegativeCached(name)
			before := len(b.nexus.Requests())
			if err := b.admin().invalidateCache(context.Background(), name); err != nil {
				t.Fatalf("invalidateCache(%s): %v", name, err)
			}
			if b.nexus.NegativeCached(name) {
				t.Error("the negative cache still holds an entry")
			}
			if got, want := calls(b.requestsFrom(before)), []string{"POST " + invalidatePath(name)}; !slices.Equal(got, want) {
				t.Errorf("requests = %q, want exactly %q", got, want)
			}
		})
	}
}

// invalidateCache is not an open door to the instance: it empties the cache of the
// proxy and of the group of the bench and of nothing else. A hosted repository
// (which has no cache, the instance would answer 400) and a name the instance does
// not know (404) are refused by the client, before any request; so is a name that
// would reach another path.
func TestRepoInvalidateCacheRefusesAnyNameOutsideTheBench(t *testing.T) {
	names := []string{
		dockerHostedRepository, "no-such-repository", "maven-central", "", "DOCKER-PROXY", "docker-proxy2", " docker-proxy",
		"docker-proxy/", "docker-proxy/../docker-hosted", "../security/users/admin", "docker-proxy?x=1", "docker-proxy#x",
		"docker-proxy%2F..%2Fx",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			b := provisionedBench(t)
			before := len(b.nexus.Requests())
			err := b.admin().invalidateCache(context.Background(), name)
			if err == nil {
				t.Fatalf("invalidateCache(%q) succeeded: only the proxy and the group of the bench may be emptied", name)
			}
			var api *APIError
			var transport *transportError
			if errors.As(err, &api) || errors.As(err, &transport) {
				t.Errorf("the refusal of %q is a REST error (%T): it has to come before any request", name, err)
			}
			if sent := b.requestsFrom(before); len(sent) != 0 {
				t.Errorf("%d request(s) sent for the refused name %q: %q", len(sent), name, calls(sent))
			}
			b.requireQuiet(err)
		})
	}
}

func TestRepoInvalidateCacheReportsAnAnswerOutsideTheNominalOnes(t *testing.T) {
	b := provisionedBench(t)
	b.nexus.Forbid(http.MethodPost, invalidatePath(dockerProxyRepository), http.StatusForbidden)
	err := b.admin().invalidateCache(context.Background(), dockerProxyRepository)
	_ = requireAPIStatus(t, err, http.StatusForbidden)
	b.requireQuiet(asBenchError(err, b.p, b.nexus.URL()))
}

// ---- unblockProxy -----------------------------------------------------------

func TestUnblockProxySequence(t *testing.T) {
	b := provisionedBench(t)
	b.nexus.AutoBlockProxy()
	b.nexus.MarkNegativeCached(dockerProxyRepository)
	before := len(b.nexus.Requests())

	if err := b.admin().unblockProxy(context.Background(), b.p); err != nil {
		t.Fatalf("unblockProxy: %v", err)
	}
	// H-UNBLOCK: the sequence of the plan. To confirm with step 0.
	want := []string{
		"GET " + repoPath("proxy", dockerProxyRepository),
		"PUT " + repoPath("proxy", dockerProxyRepository),
		"POST " + invalidatePath(dockerProxyRepository),
	}
	if got := calls(b.requestsFrom(before)); !slices.Equal(got, want) {
		t.Errorf("requests = %q, want %q", got, want)
	}
	if b.nexus.ProxyBlocked() {
		t.Error("the proxy is still blocked")
	}
	if b.nexus.NegativeCached(dockerProxyRepository) {
		t.Error("the negative cache of the proxy still holds an entry")
	}
}

func TestUnblockProxySendsTheConfigurationOfRepositorySpecs(t *testing.T) {
	b := provisionedBench(t)
	// What the instance holds has drifted: the PUT must bring back the
	// configuration of Provision, not read it from the instance.
	b.nexus.MutateRepo("proxy", dockerProxyRepository, func(cfg map[string]any) {
		cfg["httpClient"] = map[string]any{"blocked": true, "autoBlock": false}
		cfg["proxy"].(map[string]any)["contentMaxAge"] = float64(1)
	})
	before := len(b.nexus.Requests())
	if err := b.admin().unblockProxy(context.Background(), b.p); err != nil {
		t.Fatalf("unblockProxy: %v", err)
	}
	var put *fakeReq
	for _, r := range b.requestsFrom(before) {
		if r.Method == http.MethodPut {
			put = &r
		}
	}
	if put == nil {
		t.Fatal("no PUT of the proxy")
	}
	body, ok := decodeObject([]byte(put.Body))
	if !ok {
		t.Fatalf("the body of the PUT is not a JSON object: %q", put.Body)
	}
	want := proxyConfigOf(t, b.p)
	if !reflect.DeepEqual(body, want) {
		t.Errorf("the PUT sent %v, want the configuration of repositorySpecs %v", body, want)
	}
	if jsonAt(body, "docker", "pathEnabled") != true || jsonAt(body, "httpClient", "autoBlock") != true {
		t.Errorf("the PUT lacks pathEnabled or autoBlock: %v", body)
	}
	if got := requireRepo(t, b.nexus, "proxy", dockerProxyRepository); !reflect.DeepEqual(got, want) {
		t.Errorf("the instance holds %v after the unblock, want %v", got, want)
	}
}

func TestUnblockProxyTouchesNothingElse(t *testing.T) {
	b := provisionedBench(t)
	b.addImages()
	snapshot := b.nexus.Snapshot()
	b.nexus.AutoBlockProxy()
	before := len(b.nexus.Requests())
	if err := b.admin().unblockProxy(context.Background(), b.p); err != nil {
		t.Fatalf("unblockProxy: %v", err)
	}
	for _, r := range b.requestsFrom(before) {
		if r.Method != http.MethodGet && !strings.Contains(r.Path, dockerProxyRepository) {
			t.Errorf("write %s %s is about something else than %s", r.Method, r.Path, dockerProxyRepository)
		}
		if strings.HasPrefix(r.Path, pathSSRF) {
			t.Errorf("the unblock touched the SSRF protection (%s %s): Provision keeps that setting", r.Method, r.Path)
		}
	}
	if got := b.nexus.Snapshot(); got != snapshot {
		t.Errorf("the state of the instance changed beyond the proxy:\nbefore %s\nafter  %s", snapshot, got)
	}
	if n := b.nexus.ComponentCount(dockerProxyRepository); n != 1 {
		t.Errorf("the proxy lists %d components after the unblock, want its 1: it is a cache refresh, not a delete", n)
	}
}

func TestUnblockProxyIsIdempotent(t *testing.T) {
	b := provisionedBench(t)
	c := b.admin()
	for i := range 2 {
		if err := c.unblockProxy(context.Background(), b.p); err != nil {
			t.Fatalf("unblockProxy #%d: %v", i+1, err)
		}
	}
	if !reflect.DeepEqual(requireRepo(t, b.nexus, "proxy", dockerProxyRepository), proxyConfigOf(t, b.p)) {
		t.Error("the proxy configuration is not the one of repositorySpecs after two unblocks")
	}
}

func TestUnblockProxyWithoutAProxyAsksForUp(t *testing.T) {
	b := provisionedBench(t)
	b.nexus.DeleteRepo("group", dockerGroupRepository)
	b.nexus.DeleteRepo("proxy", dockerProxyRepository)
	before := len(b.nexus.Requests())
	err := b.admin().unblockProxy(context.Background(), b.p)
	if err == nil {
		t.Fatal("unblockProxy succeeded without a proxy")
	}
	requireMentions(t, err, []string{"nexus-bench:up"}, nil)
	var be *BenchError
	if !errors.As(err, &be) || be.Kind != ProvisionFailed {
		t.Errorf("error %T %v, want a *BenchError of kind ProvisionFailed", err, err)
	}
	if w := writes(b.requestsFrom(before)); len(w) != 0 {
		t.Errorf("wrote after a proxy that is not there: %q", w)
	}
}

// A proxy specification that repositorySpecs does not carry is a defect of the
// bench, not something to skip: the unblock would invalidate a cache without having
// put the configuration back, and report a success.
func TestUnblockProxyWithoutASpecificationIsAnErrorBeforeAnyRequest(t *testing.T) {
	b := provisionedBench(t)
	var specs []repositorySpec
	for _, spec := range repositorySpecs(b.p) {
		if spec.name != dockerProxyRepository {
			specs = append(specs, spec)
		}
	}
	if len(specs) != len(repositorySpecs(b.p))-1 {
		t.Fatalf("the test did not remove exactly the proxy: %d specifications left", len(specs))
	}
	before := len(b.nexus.Requests())
	err := b.admin().unblockProxyFrom(context.Background(), specs)
	if err == nil {
		t.Fatal("unblockProxyFrom succeeded without a specification of the proxy")
	}
	requireMentions(t, err, []string{dockerProxyRepository}, nil)
	_ = requireBenchError(t, err)
	if sent := b.requestsFrom(before); len(sent) != 0 {
		t.Errorf("requests sent without a specification: %q", calls(sent))
	}
}

func TestUnblockProxyStopsAtTheFirstFailure(t *testing.T) {
	t.Run("the PUT fails: the cache is not invalidated", func(t *testing.T) {
		b := provisionedBench(t)
		b.nexus.Forbid(http.MethodPut, repoPath("proxy", dockerProxyRepository), http.StatusInternalServerError)
		before := len(b.nexus.Requests())
		err := b.admin().unblockProxy(context.Background(), b.p)
		_ = requireAPIStatus(t, err, http.StatusInternalServerError)
		for _, c := range calls(b.requestsFrom(before)) {
			if strings.HasPrefix(c, "POST") {
				t.Errorf("%q was sent after the failed PUT", c)
			}
		}
	})
	t.Run("the invalidation fails: the error says so", func(t *testing.T) {
		b := provisionedBench(t)
		b.nexus.Forbid(http.MethodPost, invalidatePath(dockerProxyRepository), http.StatusInternalServerError)
		err := b.admin().unblockProxy(context.Background(), b.p)
		_ = requireAPIStatus(t, err, http.StatusInternalServerError)
		requireMentions(t, err, []string{"invalidate"}, nil)
	})
	t.Run("the SSRF protection refuses the remote URL: it is reported, not loosened", func(t *testing.T) {
		b := provisionedBench(t)
		b.nexus.SetSSRF(ssrfSettings{Enabled: true, AllowedIPs: []string{}, AllowedDomains: []string{}})
		ssrfBefore := b.nexus.SSRF()
		puts := b.nexus.Count(http.MethodPut, pathSSRF)
		before := len(b.nexus.Requests())
		err := b.admin().unblockProxy(context.Background(), b.p)
		api := requireAPIStatus(t, err, http.StatusBadRequest)
		if !api.RemoteURLPrivate {
			t.Error("the refusal is not marked as a private upstream (RemoteURLPrivate)")
		}
		if got := b.nexus.SSRF(); !reflect.DeepEqual(got, ssrfBefore) {
			t.Errorf("SSRF settings = %+v, want them untouched %+v", got, ssrfBefore)
		}
		if got := b.nexus.Count(http.MethodPut, pathSSRF); got != puts {
			t.Errorf("%d PUT of the SSRF settings during the unblock, want none", got-puts)
		}
		for _, c := range calls(b.requestsFrom(before)) {
			if strings.HasPrefix(c, "POST") {
				t.Errorf("%q was sent after the refused PUT", c)
			}
		}
	})
}

// ---- requireAdmin -----------------------------------------------------------

func TestRepoRequireAdminAcceptsTheStoredPasswordWithOneRead(t *testing.T) {
	b := provisionedBench(t)
	before := len(b.nexus.Requests())
	if err := b.admin().requireAdmin(context.Background()); err != nil {
		t.Fatalf("requireAdmin: %v", err)
	}
	sent := b.requestsFrom(before)
	if len(sent) != 1 || sent[0].Method != http.MethodGet || sent[0].User != adminUserID || sent[0].EmptyPassword {
		t.Errorf("requests = %+v, want one GET as admin with its password", sent)
	}
}

func TestRepoRequireAdminRefusedPasswordChangesNothing(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(b *bench)
	}{
		{"401: the password is not the instance's", func(b *bench) { b.nexus.SetAdminPassword(secretMarker + "-changed-elsewhere") }},
		{"403 on every call", func(b *bench) { b.nexus.Forbid("", pathAPIRoot+"/", http.StatusForbidden) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisionedBench(t)
			tt.prepare(b)
			before := len(b.nexus.Requests())
			err := b.admin().requireAdmin(context.Background())
			if err == nil {
				t.Fatal("requireAdmin accepted a refused password")
			}
			be := requireBenchError(t, err)
			if be.Kind != ProvisionFailed {
				t.Errorf("Kind = %d, want ProvisionFailed (%d); message: %v", be.Kind, ProvisionFailed, err)
			}
			requireMentions(t, err, []string{"nothing was changed"}, nil)
			b.requireQuiet(err)
			if w := writes(b.requestsFrom(before)); len(w) != 0 {
				t.Errorf("wrote while checking the password: %q", w)
			}
		})
	}
}

func TestRepoRequireAdminOtherFailuresStayRESTErrors(t *testing.T) {
	b := provisionedBench(t)
	b.nexus.DropConnections()
	err := b.admin().requireAdmin(context.Background())
	if err == nil {
		t.Fatal("requireAdmin succeeded on an instance that does not answer")
	}
	be := asBenchError(err, b.p, b.nexus.URL())
	var wrapped *BenchError
	if !errors.As(be, &wrapped) || wrapped.Kind != RESTUnreachable {
		t.Errorf("no answer is not RESTUnreachable: %v", be)
	}
	b.requireQuiet(be)
}

// ---- UnblockProxy -----------------------------------------------------------

func TestUnblockProxyPublicPutsTheProxyBackInService(t *testing.T) {
	b := provisionedBench(t)
	b.nexus.AutoBlockProxy()
	b.nexus.MarkNegativeCached(dockerProxyRepository)
	before := len(b.nexus.Requests())

	if err := UnblockProxy(context.Background(), b.store, b.nexus.URL(), b.p, b.out); err != nil {
		t.Fatalf("UnblockProxy: %v", err)
	}
	b.requireQuiet(nil)
	if b.nexus.ProxyBlocked() || b.nexus.NegativeCached(dockerProxyRepository) {
		t.Errorf("the proxy is still blocked (%v) or negative cached (%v)", b.nexus.ProxyBlocked(), b.nexus.NegativeCached(dockerProxyRepository))
	}
	wantWrites := []string{"PUT " + repoPath("proxy", dockerProxyRepository), "POST " + invalidatePath(dockerProxyRepository)}
	if got := writes(b.requestsFrom(before)); !slices.Equal(got, wantWrites) {
		t.Errorf("writes = %q, want %q", got, wantWrites)
	}
	for _, r := range b.requestsFrom(before) {
		if !strings.HasPrefix(r.Path, pathRepositories+"/") {
			t.Errorf("request %s %s is outside the repositories: the unblock reads and writes the proxy only", r.Method, r.Path)
		}
		if r.User != adminUserID || r.EmptyPassword {
			t.Errorf("request %s %s sent as %q (empty password: %v), want the stored admin", r.Method, r.Path, r.User, r.EmptyPassword)
		}
	}
	if !strings.Contains(b.out.String(), "restore: ") {
		t.Errorf("output %q has no restore: line", b.out.String())
	}
}

func TestUnblockProxyPublicAcceptsANilWriter(t *testing.T) {
	b := provisionedBench(t)
	if err := UnblockProxy(context.Background(), b.store, b.nexus.URL(), b.p, nil); err != nil {
		t.Fatalf("UnblockProxy with a nil writer: %v", err)
	}
}

func TestUnblockProxyPublicWithoutStoredSecretsAsksForUp(t *testing.T) {
	b := newBench(t)
	b.p.UpstreamURL = upstreamFor(b.p.Namespace)
	err := UnblockProxy(context.Background(), b.store, b.nexus.URL(), b.p, b.out)
	if err == nil {
		t.Fatal("UnblockProxy succeeded on a bench that was never provisioned")
	}
	_ = requireBenchError(t, err)
	requireMentions(t, err, []string{"nexus-bench:up"}, nil)
	if n := len(b.nexus.Requests()); n != 0 {
		t.Errorf("%d request(s) sent without an admin password", n)
	}
}

func TestUnblockProxyPublicRefusedPasswordChangesNothing(t *testing.T) {
	b := provisionedBench(t)
	b.store.preset(Secrets{AdminPassword: secretMarker + "-wrong-admin"})
	before := len(b.nexus.Requests())
	err := UnblockProxy(context.Background(), b.store, b.nexus.URL(), b.p, b.out)
	if err == nil {
		t.Fatal("UnblockProxy succeeded with a refused password")
	}
	be := requireBenchError(t, err)
	if be.Kind != ProvisionFailed {
		t.Errorf("Kind = %d, want ProvisionFailed", be.Kind)
	}
	b.requireQuiet(err)
	if w := writes(b.requestsFrom(before)); len(w) != 0 {
		t.Errorf("wrote with a refused password: %q", w)
	}
}

func TestUnblockProxyPublicRefusesACustomUpstream(t *testing.T) {
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
			b := provisionedBench(t)
			p := b.p
			tt.mutate(&p)
			if p.UpstreamIsBench() {
				t.Fatalf("the case is not a custom upstream: %q in %q", p.UpstreamURL, p.Namespace)
			}
			before := len(b.nexus.Requests())
			err := UnblockProxy(context.Background(), b.store, b.nexus.URL(), p, b.out)
			if err == nil {
				t.Fatal("UnblockProxy accepted an upstream that is not the bench's")
			}
			var be *BenchError
			if !errors.As(err, &be) || be.Cause == "" || be.Check == "" {
				t.Fatalf("error %T %v is not a complete *BenchError", err, err)
			}
			requireMentions(t, err, []string{envUpstreamURL}, nil)
			if sent := b.requestsFrom(before); len(sent) != 0 {
				t.Errorf("the refusal came after requests: %q", calls(sent))
			}
		})
	}
}

// The admin password goes to the REST origin it is given: UnblockProxy (and so
// the restore and the reset) refuses an origin that is not the loopback before any
// request, and before it reads the stored secrets.
func TestRequireLoopbackBase(t *testing.T) {
	tests := []struct {
		baseURL string
		ok      bool
	}{
		{"http://127.0.0.1:30481", true},
		{"http://127.0.0.1", true},
		{"http://localhost:30481", false},
		{"http://[::1]:30481", true},
		{"https://127.0.0.1:30481", true},
		{"http://192.168.1.31:30481", false},
		{"http://datomir.home.arpa:30481", false},
		{"http://127.0.0.1.evil.example:30481", false},
		{"http://localhost.evil.example:30481", false},
		{"http://127.0.0.1@evil.example:30481", false},
		{"http://user@127.0.0.1:30481", false},
		{"http://user:secret@127.0.0.1:30481", false},
		{"http://:secret@[::1]:30481", false},
		{"http://[::ffff:127.0.0.1]:30481", false},
		{"http://0.0.0.0:30481", false},
		{"http://127.0.0.2:30481", false},
		{"", false},
		{"127.0.0.1:30481", false},
		{"http:///path", false},
		{"http://127.0.0.1:30481\n.evil.example", false},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			err := requireLoopbackBase(tt.baseURL)
			if (err == nil) != tt.ok {
				t.Fatalf("requireLoopbackBase(%q) = %v, want ok = %v", tt.baseURL, err, tt.ok)
			}
			if err != nil {
				_ = requireBenchError(t, err)
				requireMentions(t, err, []string{"loopback"}, nil)
			}
		})
	}
}

// nonLoopbackBase is the origin of the fake instance written as an address the
// loopback list does not know: an IPv4-mapped IPv6 literal, which the kernel still
// connects to the fake, so a missing refusal shows as a request that arrived.
func nonLoopbackBase(t *testing.T, b *bench) string {
	t.Helper()
	port := b.nexus.URL()[strings.LastIndex(b.nexus.URL(), ":")+1:]
	return "http://[::ffff:127.0.0.1]:" + port
}

func TestUnblockProxyPublicRefusesAnOriginThatIsNotTheLoopback(t *testing.T) {
	b := provisionedBench(t)
	before := len(b.nexus.Requests())
	err := UnblockProxy(context.Background(), b.store, nonLoopbackBase(t, b), b.p, b.out)
	if err == nil {
		t.Fatal("UnblockProxy sent the admin password to an origin that is not the loopback")
	}
	be := requireBenchError(t, err)
	if be.Kind == RESTUnreachable || be.Kind == ProvisionFailed {
		t.Errorf("Kind = %d: the refusal is not a REST outcome, it comes before any request", be.Kind)
	}
	requireMentions(t, err, []string{"loopback"}, nil)
	if sent := b.requestsFrom(before); len(sent) != 0 {
		t.Errorf("%d request(s) reached the instance: %q", len(sent), calls(sent))
	}
	b.requireQuiet(err)
}

func TestRestoreUpstreamRefusesAnOriginThatIsNotTheLoopback(t *testing.T) {
	e := newCutRestoreEnv(t)
	err := RestoreUpstream(context.Background(), e.deps, nonLoopbackBase(t, e.b), e.p, e.out)
	if err == nil {
		t.Fatal("RestoreUpstream sent the admin password to an origin that is not the loopback")
	}
	requireMentions(t, err, []string{"loopback"}, nil)
	e.requireNothingTouched()
}

func TestUnblockProxyPublicReturnsACancelledContext(t *testing.T) {
	b := provisionedBench(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := len(b.nexus.Requests())
	err := UnblockProxy(ctx, b.store, b.nexus.URL(), b.p, b.out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one that is context.Canceled", err)
	}
	if sent := b.requestsFrom(before); len(sent) != 0 {
		t.Errorf("requests sent with a cancelled context: %q", calls(sent))
	}
}

func TestUnblockProxyPublicWrapsRESTFailures(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(b *bench)
		kind    ErrKind
	}{
		{"the instance does not answer", func(b *bench) { b.nexus.DropConnections() }, RESTUnreachable},
		{"the PUT is refused", func(b *bench) {
			b.nexus.Forbid(http.MethodPut, repoPath("proxy", dockerProxyRepository), http.StatusInternalServerError)
		}, ProvisionFailed},
		{"the invalidation is refused", func(b *bench) {
			b.nexus.Forbid(http.MethodPost, invalidatePath(dockerProxyRepository), http.StatusForbidden)
		}, ProvisionFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisionedBench(t)
			tt.prepare(b)
			err := UnblockProxy(context.Background(), b.store, b.nexus.URL(), b.p, b.out)
			be := requireBenchError(t, err)
			if be.Kind != tt.kind {
				t.Errorf("Kind = %d, want %d; message: %v", be.Kind, tt.kind, err)
			}
			b.requireQuiet(err)
		})
	}
}
