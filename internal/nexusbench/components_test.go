package nexusbench

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The tests of adminFromStore and listComponents (DEVDESK-541, NX-02a), run
// against the in-memory Nexus of fakenexus_test.go. The signatures are
//
//	adminFromStore(ctx, baseURL string, store SecretStore) (*client, Secrets, error)
//	(*client).listComponents(ctx, repo string) ([]component, error)
//
// with component{Name, Version, Digest}. Hypotheses to settle with the code or at
// step 12 (H-COMPONENTS):
//   - a repository the instance does not know (404) lists as no component, without
//     an error: the caller compares the list with what it expects, and a repository
//     that was just deleted must read as empty;
//   - a component without a manifest asset has an empty Digest, never an error;
//   - adminFromStore sends no request: a stored password the instance refuses shows
//     on the first call as an *APIError, and the initial password is never tried;
//   - a store without an admin password is a *BenchError of kind ProvisionFailed
//     whose Check says to run `mise run nexus-bench:up`; a pending password alone
//     is not a current one.

const (
	digestAlpine = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestBusy   = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	digestNginx  = "sha256:" + "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

	// maxRepeatedTokenCalls bounds a listing whose token does not move: a token
	// identical to the previous one fails at once, so the client makes the call
	// that returns it, the call that repeats it, and no more.
	maxRepeatedTokenCalls = 3
)

// provisioned is a bench whose admin password is stored and whose repositories
// exist, as `up` leaves it.
func provisioned(t *testing.T) *bench {
	t.Helper()
	b := newBench(t).withKnownAdmin()
	b.mustRun()
	return b
}

// adminClient builds the client of the stored admin and closes it with the test.
func (b *bench) adminClient() *client {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	c, _, err := adminFromStore(ctx, b.nexus.URL(), b.store)
	if err != nil {
		b.t.Fatalf("adminFromStore: %v", err)
	}
	b.t.Cleanup(c.closeIdle)
	return c
}

// list calls listComponents and checks the error text is quiet.
func (b *bench) list(c *client, repo string) ([]component, error) {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	got, err := c.listComponents(ctx, repo)
	b.requireQuiet(err)
	return got, err
}

func comps(names ...string) []fakeComponent {
	out := make([]fakeComponent, 0, len(names))
	for i, n := range names {
		out = append(out, fakeComponent{Name: n, Version: fmt.Sprintf("v%d", i+1), Digest: fmt.Sprintf("sha256:%064x", i+1)})
	}
	return out
}

func wantComponents(in []fakeComponent) []component {
	out := make([]component, 0, len(in))
	for _, c := range in {
		out = append(out, component(c))
	}
	return out
}

// ---- adminFromStore ------------------------------------------------------------

func TestAdminFromStoreUsesTheStoredAdminPassword(t *testing.T) {
	b := provisioned(t)
	before := len(b.nexus.Requests())

	ctx := context.Background()
	c, sec, err := adminFromStore(ctx, b.nexus.URL(), b.store)
	if err != nil {
		t.Fatalf("adminFromStore: %v", err)
	}
	defer c.closeIdle()
	b.requireQuiet(nil)

	if sec.AdminPassword != storedAdmin {
		t.Error("the returned secrets do not carry the stored admin password")
	}
	if want := b.store.current().PullPassword; want == "" || sec.PullPassword != want {
		t.Error("the returned secrets do not carry the bench-pull password the store holds")
	}
	if got := len(b.nexus.Requests()); got != before {
		t.Errorf("adminFromStore sent %d request(s), want none", got-before)
	}

	// The client really authenticates as admin with that password.
	if _, err := b.list(c, dockerProxyRepository); err != nil {
		t.Fatalf("listComponents with the stored client: %v", err)
	}
	reqs := b.requestsFrom(before)
	if len(reqs) == 0 || reqs[0].User != adminUserID || reqs[0].EmptyPassword {
		t.Errorf("first call = %+v, want Basic auth as %s with a password", reqs, adminUserID)
	}
	if b.ip.reads != 0 {
		t.Errorf("the initial password of the pod was read %d time(s)", b.ip.reads)
	}
}

func TestAdminFromStoreNeverFallsBackToTheInitialPassword(t *testing.T) {
	// The instance still has its initial password and the store holds another
	// one: a client that fell back to the pod's password would succeed.
	b := newBench(t)
	b.store.preset(Secrets{AdminPassword: secretMarker + "-not-the-one"})

	c, _, err := adminFromStore(context.Background(), b.nexus.URL(), b.store)
	if err != nil {
		t.Fatalf("adminFromStore: %v", err)
	}
	defer c.closeIdle()
	_, err = b.list(c, dockerProxyRepository)

	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusUnauthorized {
		t.Fatalf("listComponents error = %v, want an *APIError with status 401", err)
	}
	for _, s := range b.nexus.SecretsSeen() {
		if s == fakeInitialAdmin {
			t.Error("the initial admin password was sent to the instance")
		}
	}
	if b.ip.reads != 0 {
		t.Errorf("the initial password of the pod was read %d time(s)", b.ip.reads)
	}
}

func TestAdminFromStoreWithoutAnAdminPassword(t *testing.T) {
	tests := []struct {
		name  string
		store *Secrets // nil: no file
	}{
		{"no secrets file", nil},
		{"a file that is {}", &Secrets{}},
		{"only a pending admin password", &Secrets{PendingAdminPassword: secretMarker + "-pending"}},
		{"only a bench-pull password", &Secrets{PullPassword: secretMarker + "-pull"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBench(t)
			if tt.store != nil {
				b.store.preset(*tt.store)
			}
			c, sec, err := adminFromStore(context.Background(), b.nexus.URL(), b.store)
			if err == nil {
				c.closeIdle()
				t.Fatal("adminFromStore succeeded, want an error")
			}
			b.requireQuiet(err)
			if c != nil {
				t.Error("a client came back with the error")
			}
			if sec != (Secrets{}) {
				t.Error("secrets came back with the error")
			}
			var be *BenchError
			if !errors.As(err, &be) || be.Kind != ProvisionFailed {
				t.Fatalf("error = %#v, want a *BenchError of kind ProvisionFailed", err)
			}
			if !strings.Contains(be.Check, "mise run nexus-bench:up") {
				t.Errorf("Check = %q, want it to say to run `mise run nexus-bench:up`", be.Check)
			}
			if be.Cause == "" {
				t.Error("the error has no cause")
			}
			if got := len(b.nexus.Requests()); got != 0 {
				t.Errorf("%d request(s) sent for a bench that was never provisioned", got)
			}
			if b.ip.reads != 0 {
				t.Error("the initial password of the pod was read")
			}
		})
	}
}

func TestAdminFromStoreHonorsTheContext(t *testing.T) {
	b := provisioned(t)
	before := len(b.nexus.Requests())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c, sec, err := adminFromStore(ctx, b.nexus.URL(), b.store)
	if err == nil {
		c.closeIdle()
		t.Fatal("adminFromStore succeeded with a cancelled context, want an error")
	}
	b.requireQuiet(err)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %#v, want context.Canceled reachable with errors.Is", err)
	}
	if c != nil || sec != (Secrets{}) {
		t.Error("a client or secrets came back with the error")
	}
	if got := len(b.requestsFrom(before)); got != 0 {
		t.Errorf("%d request(s) sent with a cancelled context", got)
	}
	if b.ip.reads != 0 {
		t.Error("the initial password of the pod was read")
	}
}

// failingStore is a SecretStore whose Load fails with an error that carries a password.
type failingStore struct{ memStore }

func (s *failingStore) Load() (Secrets, bool, error) {
	return Secrets{}, false, errors.New("read failed near " + secretMarker + "-in-the-error")
}

func TestAdminFromStoreUnreadableStore(t *testing.T) {
	b := newBench(t)
	store := &failingStore{memStore{log: b.log}}
	c, _, err := adminFromStore(context.Background(), b.nexus.URL(), store)
	if err == nil {
		c.closeIdle()
		t.Fatal("adminFromStore succeeded, want an error")
	}
	var be *BenchError
	if !errors.As(err, &be) || be.Kind != ProvisionFailed || be.Check == "" {
		t.Fatalf("error = %#v, want a *BenchError of kind ProvisionFailed with a Check", err)
	}
	if strings.Contains(err.Error(), secretMarker) {
		t.Errorf("the message carries what the store said: %q", err)
	}
	if be.Err == nil {
		t.Error("the cause of the failed read is not kept in Err")
	}
}

// ---- listComponents ---------------------------------------------------------------

func TestListComponentsOnThreePages(t *testing.T) {
	b := provisioned(t)
	want := comps("mirror/alpine", "mirror/busybox", "mirror/nginx", "mirror/redis", "mirror/postgres", "mirror/traefik", "mirror/caddy")
	b.nexus.AddComponents(dockerProxyRepository, want...)
	b.nexus.SetComponentPageSize(3) // 3 + 3 + 1
	c := b.adminClient()
	before := len(b.nexus.Requests())

	got, err := b.list(c, dockerProxyRepository)
	if err != nil {
		t.Fatalf("listComponents: %v", err)
	}
	if !reflect.DeepEqual(got, wantComponents(want)) {
		t.Errorf("components = %+v\nwant %+v", got, wantComponents(want))
	}

	reqs := b.requestsFrom(before)
	if len(reqs) != 3 {
		t.Fatalf("%d calls, want 3 (one per page): %+v", len(reqs), reqs)
	}
	for i, r := range reqs {
		if r.Method != http.MethodGet || r.Path != pathComponents {
			t.Errorf("call %d = %s %s, want GET %s", i, r.Method, r.Path, pathComponents)
		}
		q, err := url.ParseQuery(r.Query)
		if err != nil {
			t.Fatalf("call %d: query %q: %v", i, r.Query, err)
		}
		if q.Get("repository") != dockerProxyRepository {
			t.Errorf("call %d: repository = %q, want %q", i, q.Get("repository"), dockerProxyRepository)
		}
		token := q.Get("continuationToken")
		if (i == 0) != (token == "") {
			t.Errorf("call %d: continuationToken = %q, want it only from the second call on", i, token)
		}
		if r.User != adminUserID {
			t.Errorf("call %d: user = %q, want %s", i, r.User, adminUserID)
		}
	}
}

func TestListComponentsPageBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		count     int
		pageSize  int
		wantCalls int
	}{
		{"an empty repository", 0, 3, 1},
		{"one page, not full", 2, 3, 1},
		{"exactly one full page", 3, 3, 1},
		{"a full page then a short one", 4, 3, 2},
		{"exactly two full pages", 6, 3, 2},
		{"one component per page", 3, 1, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisioned(t)
			names := make([]string, tt.count)
			for i := range names {
				names[i] = fmt.Sprintf("mirror/img%d", i)
			}
			want := comps(names...)
			b.nexus.AddComponents(dockerHostedRepository, want...)
			b.nexus.SetComponentPageSize(tt.pageSize)
			c := b.adminClient()
			before := len(b.nexus.Requests())

			got, err := b.list(c, dockerHostedRepository)
			if err != nil {
				t.Fatalf("listComponents: %v", err)
			}
			if len(got) != tt.count || (tt.count > 0 && !reflect.DeepEqual(got, wantComponents(want))) {
				t.Errorf("components = %+v, want %+v", got, wantComponents(want))
			}
			if calls := len(b.requestsFrom(before)); calls != tt.wantCalls {
				t.Errorf("%d calls, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestListComponentsReadsNameTagAndManifestDigest(t *testing.T) {
	b := provisioned(t)
	b.nexus.AddComponents(dockerProxyRepository,
		fakeComponent{Name: "mirror/alpine", Version: "3.20", Digest: digestAlpine},
		fakeComponent{Name: "mirror/busybox", Version: "1.36", Digest: digestBusy},
		// The manifest asset is not listed: no digest, and no error.
		fakeComponent{Name: "mirror/nginx", Version: "1.27"},
	)
	c := b.adminClient()

	got, err := b.list(c, dockerProxyRepository)
	if err != nil {
		t.Fatalf("listComponents: %v", err)
	}
	want := []component{
		{Name: "mirror/alpine", Version: "3.20", Digest: digestAlpine},
		{Name: "mirror/busybox", Version: "1.36", Digest: digestBusy},
		{Name: "mirror/nginx", Version: "1.27", Digest: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("components = %+v\nwant %+v", got, want)
	}
}

func TestListComponentsKeepsTheTagsOfOneImage(t *testing.T) {
	b := provisioned(t)
	b.nexus.AddComponents(dockerProxyRepository,
		fakeComponent{Name: "mirror/alpine", Version: "3.19", Digest: digestAlpine},
		fakeComponent{Name: "mirror/alpine", Version: "3.20", Digest: digestNginx},
	)
	c := b.adminClient()
	got, err := b.list(c, dockerProxyRepository)
	if err != nil {
		t.Fatalf("listComponents: %v", err)
	}
	if len(got) != 2 || got[0].Version != "3.19" || got[1].Version != "3.20" || got[0].Digest == got[1].Digest {
		t.Errorf("components = %+v, want the two tags of mirror/alpine with their own digests", got)
	}
}

func TestListComponentsOnlyReadsTheRepositoryAsked(t *testing.T) {
	b := provisioned(t)
	b.nexus.AddComponents(dockerHostedRepository, comps("mirror/hosted-only")...)
	b.nexus.AddComponents(dockerProxyRepository, comps("mirror/proxy-only")...)
	c := b.adminClient()

	got, err := b.list(c, dockerProxyRepository)
	if err != nil {
		t.Fatalf("listComponents: %v", err)
	}
	if len(got) != 1 || got[0].Name != "mirror/proxy-only" {
		t.Errorf("components = %+v, want only mirror/proxy-only", got)
	}
}

func TestListComponentsOfAnUnknownRepository(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*bench)
		repo  string
	}{
		{"a repository that never existed", func(*bench) {}, "docker-unknown"},
		{"a repository that was deleted", func(b *bench) {
			b.nexus.AddComponents(dockerProxyRepository, comps("mirror/alpine")...)
			b.nexus.DeleteRepo("proxy", dockerProxyRepository)
		}, dockerProxyRepository},
		{"a proxy that is still pending", func(b *bench) { b.nexus.DeleteRepo("proxy", dockerProxyRepository) }, dockerProxyRepository},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisioned(t)
			tt.setup(b)
			c := b.adminClient()
			got, err := b.list(c, tt.repo)
			if err != nil {
				t.Fatalf("listComponents: %v, want no error for a 404", err)
			}
			if len(got) != 0 {
				t.Errorf("components = %+v, want none", got)
			}
		})
	}
}

func TestListComponentsFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"403 for an account that may not browse", http.StatusForbidden},
		{"401 for a password the instance refuses", http.StatusUnauthorized},
		{"500 from the instance", http.StatusInternalServerError},
		{"400 for a request it did not understand", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisioned(t)
			b.nexus.AddComponents(dockerProxyRepository, comps("mirror/alpine")...)
			b.nexus.Forbid(http.MethodGet, pathComponents, tt.status)
			c := b.adminClient()

			got, err := b.list(c, dockerProxyRepository)
			if err == nil {
				t.Fatalf("listComponents = %+v, want an error", got)
			}
			var api *APIError
			if !errors.As(err, &api) || api.Status != tt.status || api.Op == "" {
				t.Fatalf("error = %#v, want an *APIError with status %d and an operation", err, tt.status)
			}
			if !strings.Contains(api.Op, "component") {
				t.Errorf("operation %q says nothing about the components", api.Op)
			}
			for _, banned := range []string{"://", "continuationToken"} {
				if strings.Contains(err.Error(), banned) {
					t.Errorf("message %q contains %q: no URL, no query", err, banned)
				}
			}
			if len(got) != 0 {
				t.Errorf("components = %+v, want none with an error", got)
			}
		})
	}
}

func TestListComponentsFailureOnALaterPageDropsThePartialResult(t *testing.T) {
	// 404 on a later page is not "no such repository": the repository answered
	// the first page, so the list is incomplete and the call fails.
	for _, status := range []int{http.StatusInternalServerError, http.StatusNotFound, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			b := provisioned(t)
			b.nexus.AddComponents(dockerProxyRepository, comps("a", "b", "c", "d", "e")...)
			b.nexus.SetComponentPageSize(2)
			c := b.adminClient()
			// The second page is refused: the first one must not be returned as if it were the whole list.
			b.nexus.FailLaterComponentPages(status)
			got, err := b.list(c, dockerProxyRepository)
			var api *APIError
			if !errors.As(err, &api) || api.Status != status || api.Op == "" || len(got) != 0 {
				t.Fatalf("listComponents = %+v, %v, want no partial list and an *APIError with status %d", got, err, status)
			}
		})
	}
}

func TestListComponentsUnreachableInstance(t *testing.T) {
	b := provisioned(t)
	c := b.adminClient()
	b.nexus.DropConnections()
	_, err := b.list(c, dockerProxyRepository)
	var tr *transportError
	if !errors.As(err, &tr) || tr.Op == "" {
		t.Fatalf("error = %#v, want a *transportError", err)
	}
	if strings.Contains(err.Error(), "://") {
		t.Errorf("message %q holds a URL", err)
	}
}

func TestListComponentsBoundsTheAnswer(t *testing.T) {
	b := provisioned(t)
	b.nexus.AddComponents(dockerProxyRepository, comps("mirror/alpine")...)
	b.nexus.Oversize(pathComponents, maxResponseBytes+1)
	c := b.adminClient()
	_, err := b.list(c, dockerProxyRepository)
	var large *tooLargeError
	if !errors.As(err, &large) {
		t.Fatalf("error = %#v, want a *tooLargeError", err)
	}
}

// F6 of the audit: a listing is bounded in components as well as in pages and
// bytes. Past the bound it is an error that names it, never a partial result.
func TestListComponentsBoundsTheNumberOfComponents(t *testing.T) {
	names := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("mirror/img-%d", i)
		}
		return out
	}
	t.Run("exactly the bound is read", func(t *testing.T) {
		b := provisioned(t)
		want := comps(names(maxListedComponents)...)
		b.nexus.AddComponents(dockerProxyRepository, want...)
		b.nexus.SetComponentPageSize(500)
		got, err := b.list(b.adminClient(), dockerProxyRepository)
		if err != nil {
			t.Fatalf("listComponents of %d components: %v", maxListedComponents, err)
		}
		if len(got) != maxListedComponents {
			t.Errorf("%d components, want %d", len(got), maxListedComponents)
		}
	})
	t.Run("one more is an error without a partial result", func(t *testing.T) {
		b := provisioned(t)
		b.nexus.AddComponents(dockerProxyRepository, comps(names(maxListedComponents+1)...)...)
		b.nexus.SetComponentPageSize(500)
		got, err := b.list(b.adminClient(), dockerProxyRepository)
		if err == nil {
			t.Fatalf("listComponents returned %d components, want an error past %d", len(got), maxListedComponents)
		}
		if got != nil {
			t.Errorf("a partial result of %d components came with the error", len(got))
		}
		if msg := err.Error(); !strings.Contains(msg, strconv.Itoa(maxListedComponents)) || !strings.Contains(msg, "components") {
			t.Errorf("the error does not name the bound of %d components: %v", maxListedComponents, err)
		}
	})
}

func TestListComponentsRefusesRedirects(t *testing.T) {
	b := provisioned(t)
	b.nexus.RedirectTo(http.MethodGet, pathComponents, http.StatusFound, "http://elsewhere.invalid/steal")
	c := b.adminClient()
	_, err := b.list(c, dockerProxyRepository)
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusFound {
		t.Fatalf("error = %#v, want an *APIError with status 302", err)
	}
}

func TestListComponentsUnexpectedAnswer(t *testing.T) {
	b := provisioned(t)
	c := b.adminClient()
	// A proxy's page where the JSON should be, with a 200.
	b.nexus.ForbidWithBody(http.MethodGet, pathComponents, http.StatusOK, "<html>not json</html>")
	_, err := b.list(c, dockerProxyRepository)
	var dec *decodeError
	if !errors.As(err, &dec) || dec.Op == "" {
		t.Fatalf("error = %#v, want a *decodeError", err)
	}
	if strings.Contains(err.Error(), "html") {
		t.Errorf("message %q repeats the answer", err)
	}
}

func TestListComponentsEscapesTheContinuationToken(t *testing.T) {
	// The fake's tokens hold + / = & ?: sent unescaped they are read as other
	// parameters or as spaces, and the fake answers 400 to the second page.
	b := provisioned(t)
	b.nexus.AddComponents(dockerProxyRepository, comps("a", "b", "c", "d")...)
	b.nexus.SetComponentPageSize(2)
	c := b.adminClient()
	got, err := b.list(c, dockerProxyRepository)
	if err != nil {
		t.Fatalf("listComponents: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("%d components, want 4", len(got))
	}
}

func TestListComponentsStopsAtOnceOnARepeatedToken(t *testing.T) {
	// The instance hands out the same token as the previous page: the listing
	// would never end. The client fails on the repetition, it does not wait for
	// a cap of requests.
	b := provisioned(t)
	b.nexus.AddComponents(dockerProxyRepository, comps("a", "b")...)
	b.nexus.SetComponentPageSize(2)
	b.nexus.LoopComponentToken(dockerProxyRepository)
	c := b.adminClient()
	before := len(b.nexus.Requests())

	got, err := b.list(c, dockerProxyRepository)
	if err == nil {
		t.Fatalf("listComponents = %+v, want an error for a token that repeats", got)
	}
	if len(got) != 0 {
		t.Errorf("components = %+v, want none with an error", got)
	}
	if calls := len(b.requestsFrom(before)); calls > maxRepeatedTokenCalls {
		t.Errorf("%d calls before giving up, want at most %d", calls, maxRepeatedTokenCalls)
	}
	var be *BenchError
	var api *APIError
	if errors.As(err, &api) || errors.As(err, &be) {
		t.Errorf("error = %#v: a repeated token is an unexpected answer, not an HTTP status", err)
	}
}

func TestListComponentsHonorsTheContext(t *testing.T) {
	b := provisioned(t)
	b.nexus.AddComponents(dockerProxyRepository, comps("a", "b", "c", "d")...)
	c := b.adminClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := len(b.nexus.Requests())
	_, err := c.listComponents(ctx, dockerProxyRepository)
	b.requireQuiet(err)
	var tr *transportError
	if !errors.As(err, &tr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %#v, want a *transportError wrapping context.Canceled", err)
	}
	if got := len(b.requestsFrom(before)); got != 0 {
		t.Errorf("%d request(s) sent with a cancelled context", got)
	}
}

func TestListComponentsNeverPrintsAPassword(t *testing.T) {
	b := provisioned(t)
	b.nexus.AddComponents(dockerProxyRepository, comps("a", "b", "c")...)
	b.nexus.SetComponentPageSize(1)
	c := b.adminClient()
	got, err := b.list(c, dockerProxyRepository)
	if err != nil {
		t.Fatalf("listComponents: %v", err)
	}
	// What comes back is data of the registry, never a credential.
	text := fmt.Sprintf("%v %+v %#v", got, got, got)
	for _, s := range b.knownSecrets() {
		if strings.Contains(text, s) {
			t.Errorf("a component dump contains a password (%d characters)", len(s))
		}
	}
	b.requireQuiet(nil)
}
