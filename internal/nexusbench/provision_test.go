package nexusbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The tests of Provision (DEVDESK-504, step 6) run it against the in-memory
// Nexus of fakenexus_test.go, with a fake SecretStore and a fake InitialPassword.
// Provision is called as
//
//	Provision(ctx, baseURL, ip, store, p, out) (ProvisionResult, error)
//
// The REST payloads are hypotheses (H-REST), centralized in fakenexus_test.go.
// Hypotheses specific to these tests, to settle at step 10 or with the code:
//   - a version is read from the Server header "Nexus/<version> (<EDITION>)";
//   - a generated password is 32 characters of [A-Za-z0-9];
//   - a refused redirect and an HTTP failure surface as an *APIError reachable
//     with errors.As, inside a *BenchError of kind ProvisionFailed; a transport
//     failure is a *BenchError of kind RESTUnreachable;
//   - the unknown admin password error says "admin password is unknown" and
//     names "mise run nexus-bench:down".

const (
	// storedAdmin is the admin password an earlier run stored.
	storedAdmin = secretMarker + "-stored-admin"

	provisionTimeout = 30 * time.Second
	mib              = 1 << 20

	groupRepository = "docker-group"
)

const generatedAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// ---- doubles ----------------------------------------------------------------

var (
	errFakeSave = errors.New("fake store: disk full")
	errFakeRead = errors.New("fake initial password: exec refused")
)

// memStore is a SecretStore in memory. It logs every Save in the shared journal
// ("STORE Save admin pending pull", only the non-empty fields) and can refuse
// some of them.
type memStore struct {
	mu       sync.Mutex
	log      *orderLog
	secrets  Secrets
	found    bool
	failSave func(Secrets) bool
	attempts []Secrets // every Save call, refused ones included
}

var _ SecretStore = (*memStore)(nil)

func (s *memStore) Load() (Secrets, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets, s.found, nil
}

func (s *memStore) Save(sec Secrets) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := "STORE Save"
	for _, field := range []struct{ name, value string }{
		{"admin", sec.AdminPassword}, {"pending", sec.PendingAdminPassword}, {"pull", sec.PullPassword},
	} {
		if field.value != "" {
			entry += " " + field.name
		}
	}
	s.log.add(entry)
	s.attempts = append(s.attempts, sec)
	if s.failSave != nil && s.failSave(sec) {
		return errFakeSave
	}
	s.secrets, s.found = sec, true
	return nil
}

func (s *memStore) Remove() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets, s.found = Secrets{}, false
	return nil
}

// preset stores secrets as an earlier run (or a hand edit) left them.
func (s *memStore) preset(sec Secrets) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets, s.found = sec, true
}

// current returns what the store holds now.
func (s *memStore) current() Secrets {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets
}

// values returns every non-empty password the store was ever asked to keep.
func (s *memStore) values() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, sec := range append(slices.Clone(s.attempts), s.secrets) {
		for _, v := range []string{sec.AdminPassword, sec.PendingAdminPassword, sec.PullPassword} {
			if v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// fakeInitial is the InitialPassword of the pod: the content of
// /nexus-data/admin.password while it exists.
type fakeInitial struct {
	password string
	ok       bool
	err      error
	reads    int
}

var _ InitialPassword = (*fakeInitial)(nil)

func (i *fakeInitial) Read(context.Context) (string, bool, error) {
	i.reads++
	if i.err != nil {
		return "", false, i.err
	}
	return i.password, i.ok, nil
}

// bench is one scenario: a fake instance, a fake store, a fake pod file.
type bench struct {
	t     *testing.T
	log   *orderLog
	nexus *fakeNexus
	store *memStore
	ip    *fakeInitial
	out   *bytes.Buffer
	p     Params
}

// newBench is a fresh instance with the initial admin password still in the pod,
// nothing stored, and the EULA accepted by the operator.
func newBench(t *testing.T) *bench {
	t.Helper()
	log := &orderLog{}
	p := Defaults()
	p.AcceptEULA = true
	p.UpstreamURL = "http://upstream.bench.invalid:5000"
	return &bench{
		t:     t,
		log:   log,
		nexus: newFakeNexus(t, log),
		store: &memStore{log: log},
		ip:    &fakeInitial{password: fakeInitialAdmin, ok: true},
		out:   &bytes.Buffer{},
		p:     p,
	}
}

// withKnownAdmin is an instance whose admin password an earlier run changed and
// stored: the pod file is gone.
func (b *bench) withKnownAdmin() *bench {
	b.nexus.SetAdminPassword(storedAdmin)
	b.store.preset(Secrets{AdminPassword: storedAdmin})
	b.ip.ok = false
	return b
}

func (b *bench) run() (ProvisionResult, error) {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	return Provision(ctx, b.nexus.URL(), b.ip, b.store, b.p, b.out)
}

// mustRun runs Provision and fails the test on an error.
func (b *bench) mustRun() ProvisionResult {
	b.t.Helper()
	res, err := b.run()
	if err != nil {
		b.t.Fatalf("Provision: %v", err)
	}
	b.requireQuiet(nil)
	return res
}

// knownSecrets is every password the scenario handled, plus the marker.
func (b *bench) knownSecrets() []string {
	set := map[string]bool{secretMarker: true, fakeInitialAdmin: true, storedAdmin: true}
	for _, s := range b.nexus.SecretsSeen() {
		set[s] = true
	}
	for _, s := range b.store.values() {
		set[s] = true
	}
	set[b.ip.password] = true
	delete(set, "")
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	return out
}

// requireQuiet fails when what Provision wrote to out, or err, carries a
// password or the marker.
func (b *bench) requireQuiet(err error) {
	b.t.Helper()
	texts := map[string]string{"output": b.out.String()}
	if err != nil {
		texts["error"] = err.Error()
	}
	for label, text := range texts {
		for _, secret := range b.knownSecrets() {
			if strings.Contains(text, secret) {
				b.t.Errorf("the %s contains a password (%d characters): %q", label, len(secret), text)
			}
		}
	}
}

// failure runs Provision, requires an error and checks it is quiet.
func (b *bench) failure() error {
	b.t.Helper()
	_, err := b.run()
	if err == nil {
		b.t.Fatal("Provision succeeded, want an error")
	}
	b.requireQuiet(err)
	return err
}

// requestsFrom returns the requests the fake received after the first n.
func (b *bench) requestsFrom(n int) []fakeReq {
	return b.nexus.Requests()[n:]
}

// writes returns "METHOD path" of the requests that are not GET.
func writes(reqs []fakeReq) []string {
	var out []string
	for _, r := range reqs {
		if r.Method != http.MethodGet {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}

// posts returns "POST path" of the POST requests.
func posts(reqs []fakeReq) []string {
	var out []string
	for _, r := range reqs {
		if r.Method == http.MethodPost {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}

func requireGeneratedPassword(t *testing.T, label, pw string) {
	t.Helper()
	if len(pw) != 32 {
		t.Errorf("%s has %d characters, want 32", label, len(pw))
	}
	for _, r := range pw {
		if !strings.ContainsRune(generatedAlphabet, r) {
			t.Errorf("%s has the character %q outside [A-Za-z0-9]", label, r)
			break
		}
	}
	for _, banned := range []string{"admin123", "admin", fakeInitialAdmin, secretMarker} {
		if strings.Contains(pw, banned) {
			t.Errorf("%s contains the predictable or known text %q", label, banned)
		}
	}
}

func requireRepo(t *testing.T, f *fakeNexus, kind, name string) map[string]any {
	t.Helper()
	cfg, ok := f.Repo(kind, name)
	if !ok {
		t.Fatalf("no %s repository %q on the instance", kind, name)
	}
	return cfg
}

func sorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

// ---- case 1: a fresh instance -----------------------------------------------

func TestProvisionFreshInstance(t *testing.T) {
	b := newBench(t)
	res, err := b.run()
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	b.requireQuiet(nil)

	if want := (ProvisionResult{NexusVersion: "3.98.1-01", Edition: "COMMUNITY", EULAAccepted: true}); res != want {
		t.Errorf("result = %+v, want %+v", res, want)
	}

	t.Run("admin password changed after it was stored", func(t *testing.T) {
		adminNow := b.nexus.AdminPassword()
		if adminNow == fakeInitialAdmin {
			t.Fatal("the admin password is still the initial one")
		}
		requireGeneratedPassword(t, "the new admin password", adminNow)

		change := b.log.index("HTTP PUT " + pathAdminPassword)
		if change < 0 {
			t.Fatalf("no change-password request; journal: %v", b.log.snapshot())
		}
		pending := b.log.indexFuncFrom(0, func(e string) bool {
			return strings.HasPrefix(e, "STORE Save") && strings.Contains(e, " pending")
		})
		if pending < 0 || pending > change {
			t.Errorf("the pending password was saved at %d, the change-password sent at %d: Save must come first; journal: %v",
				pending, change, b.log.snapshot())
		}
		promoted := b.log.indexFuncFrom(change, func(e string) bool {
			return strings.HasPrefix(e, "STORE Save") && strings.Contains(e, " admin") && !strings.Contains(e, " pending")
		})
		if promoted < 0 {
			t.Errorf("the new password was never promoted to adminPassword after the change; journal: %v", b.log.snapshot())
		}

		stored := b.store.current()
		if stored.AdminPassword != adminNow {
			t.Error("the stored adminPassword is not the password the instance now has")
		}
		if stored.PendingAdminPassword != "" {
			t.Error("pendingAdminPassword was not cleared after the promotion")
		}
		for _, attempt := range b.store.attempts {
			if attempt.PendingAdminPassword != "" && attempt.PendingAdminPassword != adminNow {
				t.Error("the pending password saved is not the one sent to the instance")
			}
		}
	})

	t.Run("EULA accepted with the disclaimer as received", func(t *testing.T) {
		// The fake answers 400 to any other disclaimer, so accepting at all proves it.
		if !b.nexus.EULAAccepted() {
			t.Error("the EULA is not accepted")
		}
		if n := b.nexus.Count(http.MethodPost, pathEULA); n != 1 {
			t.Errorf("POST %s sent %d times, want 1", pathEULA, n)
		}
	})

	t.Run("realms and anonymous access", func(t *testing.T) {
		if got, want := b.nexus.Realms(), []string{"NexusAuthenticatingRealm", "DockerToken"}; !slices.Equal(got, want) {
			t.Errorf("realms = %v, want %v", got, want)
		}
		anon := b.nexus.Anonymous()
		if anon["enabled"] != true || anon["userId"] != "anonymous" {
			t.Errorf("anonymous = %v, want enabled for the user anonymous", anon)
		}
	})

	t.Run("pull role", func(t *testing.T) {
		role, ok := b.nexus.Role("bench-docker-pull")
		if !ok {
			t.Fatal("role bench-docker-pull missing")
		}
		want := []string{"nx-repository-view-docker-*-browse", "nx-repository-view-docker-*-read"}
		if got := stringsAt(role, "privileges"); !slices.Equal(sorted(got), want) {
			t.Errorf("privileges = %v, want %v", got, want)
		}
	})

	t.Run("pull user", func(t *testing.T) {
		u, ok := b.nexus.User("bench-pull")
		if !ok {
			t.Fatal("user bench-pull missing")
		}
		if u.Status != "active" || !slices.Equal(u.Roles, []string{"bench-docker-pull"}) {
			t.Errorf("bench-pull = status %q roles %v, want active with bench-docker-pull", u.Status, u.Roles)
		}
		requireGeneratedPassword(t, "the bench-pull password", u.Password)
		if got := b.store.current().PullPassword; got != u.Password {
			t.Error("the stored pullPassword is not the password of bench-pull on the instance")
		}
		if u.Password == b.nexus.AdminPassword() {
			t.Error("bench-pull and admin share a password")
		}
		saved := b.log.indexFuncFrom(0, func(e string) bool {
			return strings.HasPrefix(e, "STORE Save") && strings.Contains(e, " pull")
		})
		created := b.log.index("HTTP POST " + pathUsers)
		if saved < 0 || created < 0 || saved > created {
			t.Errorf("the pull password was saved at %d, the user created at %d: Save must come first; journal: %v",
				saved, created, b.log.snapshot())
		}
	})

	t.Run("repositories", func(t *testing.T) {
		for _, repo := range []struct{ kind, name string }{{"hosted", "docker-hosted"}, {"proxy", "docker-proxy"}, {"group", groupRepository}} {
			cfg := requireRepo(t, b.nexus, repo.kind, repo.name)
			if cfg["online"] != true || jsonAt(cfg, "storage", "blobStoreName") != "default" {
				t.Errorf("%s: online/blob store = %v/%v, want true/default", repo.name, cfg["online"], jsonAt(cfg, "storage", "blobStoreName"))
			}
			if jsonAt(cfg, "docker", "forceBasicAuth") != false {
				t.Errorf("%s: docker.forceBasicAuth = %v, want false (anonymous pull)", repo.name, jsonAt(cfg, "docker", "forceBasicAuth"))
			}
		}
		proxy := requireRepo(t, b.nexus, "proxy", "docker-proxy")
		if got := jsonAt(proxy, "proxy", "remoteUrl"); got != b.p.UpstreamURL {
			t.Errorf("proxy.remoteUrl = %v, want %s", got, b.p.UpstreamURL)
		}
		if got := jsonAt(proxy, "dockerProxy", "indexType"); got != "REGISTRY" {
			t.Errorf("dockerProxy.indexType = %v, want REGISTRY", got)
		}
		group := requireRepo(t, b.nexus, "group", groupRepository)
		if got, want := stringsAt(group, "group", "memberNames"), []string{"docker-hosted", "docker-proxy"}; !slices.Equal(got, want) {
			t.Errorf("group members = %v, want %v", got, want)
		}
		if got := jsonAt(group, "docker", "httpPort"); got != float64(dockerContainerPort) {
			t.Errorf("group docker.httpPort = %v, want %d", got, dockerContainerPort)
		}
	})
}

// ---- case 2: a second run ---------------------------------------------------

func TestProvisionSecondRunChangesNothing(t *testing.T) {
	b := newBench(t)
	b.mustRun()
	before := b.nexus.Snapshot()
	adminBefore := b.nexus.AdminPassword()
	pull, _ := b.nexus.User("bench-pull")
	storedBefore := b.store.current()
	b.ip.ok = false // the pod file is gone after the first change

	for run := 2; run <= 3; run++ {
		first := len(b.nexus.Requests())
		res := b.mustRun()
		if !res.EULAAccepted || res.NexusVersion == "" {
			t.Errorf("run %d: result = %+v, want the version and the accepted EULA", run, res)
		}
		reqs := b.requestsFrom(first)
		if got := posts(reqs); len(got) != 0 {
			t.Errorf("run %d sent POST requests: %v", run, got)
		}
		if n := len(reqs); n == 0 {
			t.Errorf("run %d sent no request at all: it cannot have read the state", run)
		}
		for _, w := range writes(reqs) {
			if w == "PUT "+pathAdminPassword {
				t.Errorf("run %d changed the admin password again", run)
			}
		}
		if after := b.nexus.Snapshot(); after != before {
			t.Errorf("run %d changed the state of the instance:\n before: %s\n after:  %s", run, before, after)
		}
		if got := b.nexus.AdminPassword(); got != adminBefore {
			t.Errorf("run %d: the admin password changed", run)
		}
		if u, _ := b.nexus.User("bench-pull"); u.Password != pull.Password {
			t.Errorf("run %d: the bench-pull password changed", run)
		}
		if got := b.store.current(); got != storedBefore {
			t.Errorf("run %d: the stored secrets changed", run)
		}
	}
}

// ---- case 3: drift ----------------------------------------------------------

func TestProvisionRestoresDrift(t *testing.T) {
	tests := []struct {
		name      string
		drift     func(f *fakeNexus)
		wantPosts []string
	}{
		{"group member removed", func(f *fakeNexus) {
			f.MutateRepo("group", groupRepository, func(c map[string]any) {
				c["group"].(map[string]any)["memberNames"] = []any{"docker-hosted"}
			})
		}, nil},
		{"proxy upstream changed", func(f *fakeNexus) {
			f.MutateRepo("proxy", "docker-proxy", func(c map[string]any) {
				c["proxy"].(map[string]any)["remoteUrl"] = "http://elsewhere.invalid:5000"
			})
		}, nil},
		{"hosted forces basic auth", func(f *fakeNexus) {
			f.MutateRepo("hosted", "docker-hosted", func(c map[string]any) {
				c["docker"].(map[string]any)["forceBasicAuth"] = true
			})
		}, nil},
		{"group deleted", func(f *fakeNexus) { f.DeleteRepo("group", groupRepository) },
			[]string{"POST " + repoPath("group", "")}},
		{"proxy deleted", func(f *fakeNexus) { f.DeleteRepo("proxy", "docker-proxy") },
			[]string{"POST " + repoPath("proxy", "")}},
		{"role lost a privilege", func(f *fakeNexus) {
			f.MutateRole("bench-docker-pull", func(r map[string]any) {
				r["privileges"] = []any{"nx-repository-view-docker-*-browse"}
			})
		}, nil},
		{"role deleted", func(f *fakeNexus) { f.DeleteRole("bench-docker-pull") }, []string{"POST " + pathRoles}},
		{"user got another role", func(f *fakeNexus) {
			f.MutateUser("bench-pull", func(u *fakeUser) { u.Roles = []string{"nx-admin"} })
		}, nil},
		{"user disabled", func(f *fakeNexus) {
			f.MutateUser("bench-pull", func(u *fakeUser) { u.Status = "disabled" })
		}, nil},
		{"user password changed by hand", func(f *fakeNexus) {
			f.MutateUser("bench-pull", func(u *fakeUser) { u.Password = "changed-elsewhere" })
		}, nil},
		{"user deleted", func(f *fakeNexus) { f.DeleteUser("bench-pull") }, []string{"POST " + pathUsers}},
		{"realm removed", func(f *fakeNexus) { f.SetRealms("NexusAuthenticatingRealm") }, nil},
		{"anonymous disabled", func(f *fakeNexus) { f.SetAnonymous(false) }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBench(t)
			b.mustRun()
			baseline := b.nexus.Snapshot()
			adminBefore := b.nexus.AdminPassword()
			b.ip.ok = false
			tt.drift(b.nexus)

			first := len(b.nexus.Requests())
			b.mustRun()

			if got := sorted(posts(b.requestsFrom(first))); !slices.Equal(got, sorted(tt.wantPosts)) {
				t.Errorf("POST requests = %v, want %v", got, tt.wantPosts)
			}
			if after := b.nexus.Snapshot(); after != baseline {
				t.Errorf("the drift was not repaired:\n want: %s\n got:  %s", baseline, after)
			}
			if got := b.nexus.AdminPassword(); got != adminBefore {
				t.Error("the admin password changed")
			}
			u, _ := b.nexus.User("bench-pull")
			if u.Password != b.store.current().PullPassword {
				t.Error("the bench-pull password on the instance is not the stored one")
			}
		})
	}
}

// ---- case 4: the EULA -------------------------------------------------------

func TestProvisionEULA(t *testing.T) {
	tests := []struct {
		name         string
		prepare      func(b *bench)
		wantErr      string // "" = success
		wantMsg      string // a phrase the error must carry
		wantCreation bool   // whether roles, users and repositories are created
	}{
		{
			name:    "not accepted and not allowed to accept",
			prepare: func(b *bench) { b.p.AcceptEULA = false },
			wantErr: "NEXUS_BENCH_ACCEPT_EULA",
			// The admin password may have been changed by then: only the
			// repository, the role and the account are certainly not created.
			wantMsg: "no repository, role or account was created",
		},
		{
			name: "already accepted, no need to accept",
			prepare: func(b *bench) {
				b.p.AcceptEULA = false
				b.nexus.AcceptEULAAlready()
			},
			wantCreation: true,
		},
		{
			name:         "no EULA endpoint on this edition",
			prepare:      func(b *bench) { b.nexus.RemoveEULAEndpoint() },
			wantCreation: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBench(t)
			tt.prepare(b)
			_, err := b.run()
			b.requireQuiet(err)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one naming %s", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantMsg) || strings.Contains(err.Error(), "nothing was created") {
					t.Errorf("error = %v, want it to say %q", err, tt.wantMsg)
				}
				if got := posts(b.nexus.Requests()); len(got) != 0 {
					t.Errorf("POST requests before the stop: %v", got)
				}
				if b.nexus.RepoCount() != 0 {
					t.Error("a repository was created")
				}
				if _, ok := b.nexus.User("bench-pull"); ok {
					t.Error("the user bench-pull was created")
				}
				if _, ok := b.nexus.Role("bench-docker-pull"); ok {
					t.Error("the role bench-docker-pull was created")
				}
				if b.nexus.EULAAccepted() {
					t.Error("the EULA was accepted without the operator's consent")
				}
				return
			}
			if err != nil {
				t.Fatalf("Provision: %v", err)
			}
			if n := b.nexus.Count(http.MethodPost, pathEULA); n != 0 {
				t.Errorf("POST %s sent %d times, want 0", pathEULA, n)
			}
			if tt.wantCreation && b.nexus.RepoCount() != 3 {
				t.Errorf("%d repositories, want 3", b.nexus.RepoCount())
			}
		})
	}
}

// ---- case 5: the admin password cannot be found -------------------------------

func TestProvisionAdminPasswordUnknown(t *testing.T) {
	tests := []struct {
		name  string
		store *Secrets // nil: nothing stored
	}{
		{"nothing stored", nil},
		{"a wrong stored password", &Secrets{AdminPassword: secretMarker + "-wrong"}},
		{"a wrong pending password", &Secrets{AdminPassword: secretMarker + "-wrong", PendingAdminPassword: secretMarker + "-wrong-pending"}},
		{"a valid file that is {}", &Secrets{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBench(t)
			b.nexus.SetAdminPassword(secretMarker + "-changed-by-someone")
			b.ip.ok = false
			if tt.store != nil {
				b.store.preset(*tt.store)
			}
			err := b.failure()
			for _, want := range []string{"admin password is unknown", "mise run nexus-bench:down"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if got := writes(b.nexus.Requests()); len(got) != 0 {
				t.Errorf("writes before giving up: %v", got)
			}
			for _, r := range b.nexus.Requests() {
				if r.EmptyPassword {
					t.Errorf("%s %s was sent with an empty password: {} is not a password", r.Method, r.Path)
				}
			}
		})
	}
}

// An {} file the store reads fine (found, empty) means "not provided": the
// initial password of the pod is used, as if the file did not exist.
func TestProvisionEmptySecretsFileIsNotAPassword(t *testing.T) {
	path := secretsPathIn(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t, path)
	if sec, found, err := store.Load(); err != nil || !found || sec != (Secrets{}) {
		t.Fatalf("Load = %+v, %v, %v; the fixture should be a found, empty Secrets", sec, found, err)
	}

	log := &orderLog{}
	nexus := newFakeNexus(t, log)
	p := Defaults()
	p.AcceptEULA = true
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	if _, err := Provision(ctx, nexus.URL(), &fakeInitial{password: fakeInitialAdmin, ok: true}, store, p, &out); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	got, found, err := store.Load()
	if err != nil || !found {
		t.Fatalf("Load after Provision = %v, %v", found, err)
	}
	if got.AdminPassword != nexus.AdminPassword() {
		t.Error("the file does not hold the admin password the instance now has")
	}
	requireGeneratedPassword(t, "the stored admin password", got.AdminPassword)
	if got.PendingAdminPassword != "" {
		t.Error("pendingAdminPassword left in the file")
	}
	for _, r := range nexus.Requests() {
		if r.EmptyPassword {
			t.Errorf("%s %s was sent with an empty password", r.Method, r.Path)
		}
	}
	if strings.Contains(out.String(), got.AdminPassword) || strings.Contains(out.String(), fakeInitialAdmin) {
		t.Error("the output contains a password")
	}
}

func TestProvisionInitialPasswordReadFailure(t *testing.T) {
	b := newBench(t)
	b.ip.err = errFakeRead
	err := b.failure()
	if !errors.Is(err, errFakeRead) {
		t.Errorf("error %v does not wrap the failure of the pod read", err)
	}
	if got := writes(b.nexus.Requests()); len(got) != 0 {
		t.Errorf("writes after a failed read: %v", got)
	}
}

// ---- case 6: the store refuses a Save ---------------------------------------

func TestProvisionStopsWhenSaveFails(t *testing.T) {
	t.Run("pending admin password: no change-password", func(t *testing.T) {
		b := newBench(t)
		b.store.failSave = func(s Secrets) bool { return s.PendingAdminPassword != "" }
		err := b.failure()
		if !errors.Is(err, errFakeSave) {
			t.Errorf("error %v does not wrap the failure of Save", err)
		}
		if n := b.nexus.Count(http.MethodPut, pathAdminPassword); n != 0 {
			t.Errorf("change-password sent %d times after a failed Save, want 0", n)
		}
		if got := b.nexus.AdminPassword(); got != fakeInitialAdmin {
			t.Error("the admin password changed although it could not be kept")
		}
	})
	t.Run("pull password: no user created", func(t *testing.T) {
		b := newBench(t)
		b.store.failSave = func(s Secrets) bool { return s.PullPassword != "" }
		err := b.failure()
		if !errors.Is(err, errFakeSave) {
			t.Errorf("error %v does not wrap the failure of Save", err)
		}
		if n := b.nexus.Count(http.MethodPost, pathUsers); n != 0 {
			t.Errorf("POST %s sent %d times after a failed Save, want 0", pathUsers, n)
		}
		if _, ok := b.nexus.User("bench-pull"); ok {
			t.Error("bench-pull exists although its password could not be kept")
		}
	})
}

// ---- case 7: resuming an interrupted password change --------------------------

func TestProvisionResumesAnInterruptedPasswordChange(t *testing.T) {
	const pendingPW = secretMarker + "-pending"

	t.Run("the instance already took the pending password", func(t *testing.T) {
		b := newBench(t)
		b.nexus.SetAdminPassword(pendingPW)
		b.ip.ok = false
		b.store.preset(Secrets{AdminPassword: fakeInitialAdmin, PendingAdminPassword: pendingPW})

		b.mustRun()

		if n := b.nexus.Count(http.MethodPut, pathAdminPassword); n != 0 {
			t.Errorf("the admin password was changed %d more times, want 0", n)
		}
		got := b.store.current()
		if got.AdminPassword != pendingPW || got.PendingAdminPassword != "" {
			t.Error("the pending password was not promoted to adminPassword")
		}
		if b.nexus.AdminPassword() != pendingPW {
			t.Error("the admin password of the instance changed")
		}
	})

	t.Run("the change never reached the instance", func(t *testing.T) {
		b := newBench(t)
		b.store.preset(Secrets{PendingAdminPassword: pendingPW}) // the pod file is still there

		b.mustRun()

		if n := b.nexus.Count(http.MethodPut, pathAdminPassword); n != 1 {
			t.Errorf("change-password sent %d times, want 1", n)
		}
		got := b.store.current()
		if got.AdminPassword == "" || got.AdminPassword != b.nexus.AdminPassword() {
			t.Error("the stored adminPassword is not the admin password of the instance")
		}
		if got.PendingAdminPassword != "" {
			t.Error("pendingAdminPassword was not cleared")
		}
	})
}

// ---- case 8: an HTTP failure ------------------------------------------------

func TestProvisionHTTPFailures(t *testing.T) {
	tests := []struct {
		name           string
		method, prefix string
		status         int
	}{
		{"403 on creating the user", http.MethodPost, pathUsers, http.StatusForbidden},
		{"403 on creating a repository", http.MethodPost, repoPath("hosted", ""), http.StatusForbidden},
		{"403 on creating the role", http.MethodPost, pathRoles, http.StatusForbidden},
		{"403 on the realms", http.MethodPut, pathRealms, http.StatusForbidden},
		{"401 on the anonymous access", http.MethodPut, pathAnonymous, http.StatusUnauthorized},
		{"500 on reading a repository", http.MethodGet, repoPath("proxy", ""), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBench(t).withKnownAdmin()
			b.nexus.Forbid(tt.method, tt.prefix, tt.status)
			err := b.failure()

			var be *BenchError
			if !errors.As(err, &be) || be.Kind != ProvisionFailed {
				t.Fatalf("error = %#v, want a *BenchError of kind ProvisionFailed", err)
			}
			var api *APIError
			if !errors.As(err, &api) {
				t.Fatalf("error %v does not carry an *APIError", err)
			}
			if api.Status != tt.status || api.Op == "" {
				t.Errorf("APIError = %+v, want status %d and an operation", *api, tt.status)
			}
			for _, want := range []string{strconv.Itoa(tt.status), api.Op} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message %q does not contain %q", err, want)
				}
			}
			if be.Check == "" {
				t.Error("the error says nothing to check")
			}
		})
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &APIError{Op: "create user bench-pull", Status: http.StatusForbidden}
	msg := err.Error()
	for _, want := range []string{"create user bench-pull", "403"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, want it to contain %q", msg, want)
		}
	}
	for _, banned := range []string{"://", "@", secretMarker} {
		if strings.Contains(msg, banned) {
			t.Errorf("Error() = %q contains %q: no URL, no credential", msg, banned)
		}
	}
	var wrapped error = &BenchError{Kind: ProvisionFailed, Cause: "x", Check: "y", Err: err}
	var got *APIError
	if !errors.As(wrapped, &got) || got != err {
		t.Error("an *APIError wrapped in a BenchError is not reachable with errors.As")
	}
}

func TestProvisionUnreachableInstance(t *testing.T) {
	b := newBench(t).withKnownAdmin()
	b.nexus.DropConnections()
	err := b.failure()
	var be *BenchError
	if !errors.As(err, &be) || be.Kind != RESTUnreachable {
		t.Fatalf("error = %#v, want a *BenchError of kind RESTUnreachable", err)
	}
	if be.Check == "" {
		t.Error("the error says nothing to check")
	}
}

// ---- case 9: redirects and oversized bodies ----------------------------------

func TestProvisionRefusesRedirects(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		status int
	}{
		{"the first request, 302", pathUsers, http.StatusFound},
		{"the first request, 301", pathUsers, http.StatusMovedPermanently},
		{"the first request, 307", pathUsers, http.StatusTemporaryRedirect},
		{"the first request, 308", pathUsers, http.StatusPermanentRedirect},
		{"the realms, 302", pathRealms, http.StatusFound},
		{"the roles, 302", pathRoles, http.StatusFound},
		{"the blob stores, 302", pathBlobStores, http.StatusFound},
		{"the repositories, 302", pathRepositories, http.StatusFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(target.Close)

			b := newBench(t).withKnownAdmin()
			b.nexus.RedirectTo("", tt.prefix, tt.status, target.URL+"/stolen")
			err := b.failure()

			if n := hits.Load(); n != 0 {
				t.Errorf("the other origin received %d requests, want 0: the redirect was followed", n)
			}
			var api *APIError
			if !errors.As(err, &api) || api.Status != tt.status {
				t.Errorf("error %v does not carry an *APIError with status %d", err, tt.status)
			}
			if strings.Contains(err.Error(), "admin password is unknown") {
				t.Error("a redirect was taken for a refused password")
			}
			if strings.Contains(err.Error(), target.URL) {
				t.Errorf("the error leaks the redirect target: %v", err)
			}
		})
	}
}

func TestProvisionBoundsTheBodies(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		size    int
		wantErr bool
	}{
		{"1 MiB is read", pathUsers, mib, false},
		{"1 MiB + 1 is refused", pathUsers, mib + 1, true},
		{"2 MiB of users is refused", pathUsers, 2 * mib, true},
		{"2 MiB of blob stores is refused", pathBlobStores, 2 * mib, true},
		{"2 MiB of a repository is refused", pathRepositories, 2 * mib, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBench(t).withKnownAdmin()
			// Valid JSON padded with spaces: a client that decoded it before
			// counting would succeed, so only the cap can fail the run. Reads
			// the instance answers 404 to are not padded (only a 200 is).
			if tt.prefix == pathRepositories {
				b.mustRun() // so that the repositories exist and their GET is a 200
				b.nexus.Oversize(tt.prefix, tt.size)
				_, err := b.run()
				b.requireQuiet(err)
				if err == nil {
					t.Fatal("Provision accepted an oversized body")
				}
				return
			}
			b.nexus.Oversize(tt.prefix, tt.size)
			_, err := b.run()
			b.requireQuiet(err)
			if tt.wantErr && err == nil {
				t.Fatal("Provision accepted an oversized body")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Provision refused a body of exactly the cap: %v", err)
			}
		})
	}
}

// ---- case 10: the blob store ------------------------------------------------

func TestProvisionNeedsTheDefaultBlobStore(t *testing.T) {
	b := newBench(t)
	b.nexus.RemoveBlobStore("default")
	err := b.failure()

	msg := strings.ToLower(err.Error())
	for _, want := range []string{"blob store", "default"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	var be *BenchError
	if !errors.As(err, &be) || be.Kind != ProvisionFailed {
		t.Errorf("error = %#v, want a *BenchError of kind ProvisionFailed", err)
	}
	if b.nexus.RepoCount() != 0 {
		t.Error("a repository was created without a blob store to hold it")
	}
}

// ---- case 11: anonymous access ----------------------------------------------

func TestProvisionAnonymousAccess(t *testing.T) {
	tests := []struct {
		name    string
		initial bool
		want    bool
	}{
		{"disabled on an enabled instance", true, false},
		{"enabled on a disabled instance", false, true},
		{"enabled stays enabled", true, true},
		{"disabled stays disabled", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBench(t)
			b.p.Anonymous = tt.want
			b.nexus.SetAnonymous(tt.initial)
			b.mustRun()

			if got := b.nexus.Anonymous()["enabled"]; got != tt.want {
				t.Errorf("enabled = %v, want %v", got, tt.want)
			}
			var last map[string]any
			for _, r := range b.nexus.Requests() {
				if r.Method == http.MethodPut && r.Path == pathAnonymous {
					last, _ = decodeObject([]byte(r.Body))
				}
				if r.Method == http.MethodPost && r.Path == pathAnonymous {
					t.Error("anonymous access was POSTed: it is a setting, not a collection")
				}
			}
			if last == nil {
				t.Fatalf("no PUT %s", pathAnonymous)
			}
			if last["enabled"] != tt.want || last["userId"] != "anonymous" {
				t.Errorf("PUT body = %v, want enabled=%v for the user anonymous", last, tt.want)
			}
		})
	}
}

// ---- the version ------------------------------------------------------------

func TestProvisionReadsTheVersion(t *testing.T) {
	tests := []struct {
		name         string
		header       string
		swagger      string
		wantVersion  string
		wantEdition  string
		checkEdition bool
	}{
		{"community header", "Nexus/3.98.1-01 (COMMUNITY)", "", "3.98.1-01", "COMMUNITY", true},
		{"another edition and version", "Nexus/3.99.0-02 (PRO)", "", "3.99.0-02", "PRO", true},
		{"no header, the swagger document", "", "3.98.1-01", "3.98.1-01", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBench(t)
			b.nexus.SetServerHeader(tt.header, tt.swagger)
			res := b.mustRun()
			if res.NexusVersion != tt.wantVersion {
				t.Errorf("NexusVersion = %q, want %q", res.NexusVersion, tt.wantVersion)
			}
			if tt.checkEdition && res.Edition != tt.wantEdition {
				t.Errorf("Edition = %q, want %q", res.Edition, tt.wantEdition)
			}
			if !res.EULAAccepted {
				t.Error("EULAAccepted = false after accepting it")
			}
		})
	}
}

// The version is optional: a swagger fallback that is not JSON must not fail
// the provisioning.
func TestProvisionToleratesAnUnreadableSwagger(t *testing.T) {
	b := newBench(t)
	b.nexus.SetServerHeader("", "")
	b.nexus.SetSwaggerBody("<html>a proxy login page</html>")
	res := b.mustRun()
	if res.NexusVersion != "" {
		t.Errorf("NexusVersion = %q, want none", res.NexusVersion)
	}
	if !res.EULAAccepted || b.nexus.RepoCount() != 3 {
		t.Errorf("provisioning stopped short: result %+v, %d repositories", res, b.nexus.RepoCount())
	}
}

// ---- generated passwords ----------------------------------------------------

func TestProvisionGeneratesDistinctPasswords(t *testing.T) {
	seen := map[string]string{}
	for run := 0; run < 3; run++ {
		b := newBench(t)
		b.mustRun()
		pull, _ := b.nexus.User("bench-pull")
		for label, pw := range map[string]string{"admin": b.nexus.AdminPassword(), "bench-pull": pull.Password} {
			requireGeneratedPassword(t, fmt.Sprintf("run %d %s password", run, label), pw)
			if prev, dup := seen[pw]; dup {
				t.Errorf("run %d %s password repeats the %s password of an earlier run", run, label, prev)
			}
			seen[pw] = label
		}
	}
}

// ---- no password in the output, whatever happens ----------------------------

func TestProvisionNeverPrintsAPassword(t *testing.T) {
	scenarios := []struct {
		name  string
		setup func(b *bench)
	}{
		{"fresh instance", func(*bench) {}},
		{"known admin", func(b *bench) { b.withKnownAdmin() }},
		{"unknown admin", func(b *bench) {
			b.nexus.SetAdminPassword(secretMarker + "-someone-else")
			b.ip.ok = false
			b.store.preset(Secrets{AdminPassword: secretMarker + "-stale"})
		}},
		{"a refused write", func(b *bench) {
			b.withKnownAdmin()
			b.nexus.Forbid(http.MethodPost, pathUsers, http.StatusForbidden)
		}},
		{"a refused Save", func(b *bench) { b.store.failSave = func(Secrets) bool { return true } }},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			b := newBench(t)
			sc.setup(b)
			_, err := b.run()
			b.requireQuiet(err)
		})
	}
}

// ---- the client -------------------------------------------------------------

// The client has to hold a dedicated http.Client with the 15 s timeout and a
// CheckRedirect that refuses redirections. The redirect behavior itself is
// tested through Provision above; this pins the two settings whatever the field
// is called.
func TestNewClientSettings(t *testing.T) {
	c := newClient("http://127.0.0.1:1", "admin", secretMarker)
	if c == nil {
		t.Fatal("newClient returned nil")
	}
	v := reflect.ValueOf(c).Elem()
	var hc reflect.Value
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		switch f.Type() {
		case reflect.TypeOf((*http.Client)(nil)):
			if !f.IsNil() {
				hc = f.Elem()
			}
		case reflect.TypeOf(http.Client{}):
			hc = f
		}
	}
	if !hc.IsValid() {
		t.Fatalf("client has no http.Client field; fields: %v", v.Type())
	}
	if got, want := time.Duration(hc.FieldByName("Timeout").Int()), 15*time.Second; got != want {
		t.Errorf("Timeout = %v, want %v", got, want)
	}
	if hc.FieldByName("CheckRedirect").IsNil() {
		t.Error("CheckRedirect is nil: redirections would be followed")
	}
}
