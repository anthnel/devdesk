package nexusbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests of this file act on the real cluster the bench runs on (datomir by
// default). Every one of them starts with requireBench(t), which skips unless
// DEVDESK_NEXUS_BENCH=1 is set and CI is empty, so `mise run test` and the CI
// never reach them; `mise run nexus-bench:test` sets the variable. guard_test.go
// fails the suite when a test of this package calls the cluster, the bench
// descriptor or the secrets path without that guard.
//
// What they do on the bench:
//
//   - TestIntegrationBenchDescribed, TestIntegrationAnonymousAsDescribed: read
//     only (GET requests as admin or without credentials).
//   - TestIntegrationPullAccountIsReadOnly: GET requests, plus one POST as
//     bench-pull that Nexus has to refuse. Should the account be allowed to
//     create it, the repository is deleted again (as admin) and the test fails.
//   - TestIntegrationDatasetAsDescribed (DEVDESK-505): read only, REST listings
//     of docker-hosted and docker-proxy (and, only when the listing gives no
//     digest, skopeo inspect of an image the descriptor says is hosted or cached).
//     It never reads mirror/uncached through docker-proxy or docker-group: a read
//     through them caches the image (F9) and only a reset undoes that.
//   - TestIntegrationLifecycle: destructive (up, provision, seed, up again, down,
//     up, down), run only with DEVDESK_NEXUS_BENCH_LIFECYCLE=1 and
//     NEXUS_BENCH_ACCEPT_EULA=yes. It needs skopeo on the host.
//
// None of them writes the versioned descriptor.json.

const (
	// benchGateEnv opens the tests that read the bench.
	benchGateEnv = "DEVDESK_NEXUS_BENCH"
	// benchLifecycleEnv opens, in addition, the test that destroys the bench.
	benchLifecycleEnv = "DEVDESK_NEXUS_BENCH_LIFECYCLE"

	// liveCallTimeout bounds one group of REST calls of a read-only test.
	liveCallTimeout = time.Minute
	// lifecycleMinTimeout is the go test -timeout below which the lifecycle
	// refuses to start: three deployments of up to ten minutes each, plus the
	// deletions. The default of go test is 10 minutes.
	lifecycleMinTimeout = 45 * time.Minute

	// readOnlyProbeRepository names the repository the read-only account is
	// asked to create. It must never exist.
	readOnlyProbeRepository = "bench-readonly-probe"
)

// requireBench skips the test unless it may talk to the real bench: the
// variable DEVDESK_NEXUS_BENCH is 1 and CI is empty. The same motif as
// internal/nettrace/live_linux_test.go.
func requireBench(t *testing.T) {
	t.Helper()
	if os.Getenv(benchGateEnv) != "1" {
		t.Skipf("set %s=1 to run against the real bench (it reaches the cluster)", benchGateEnv)
	}
	if os.Getenv("CI") != "" {
		t.Skip("the bench tests never run in CI")
	}
}

// liveBench is the running bench as the versioned descriptor and the secrets
// file describe it.
type liveBench struct {
	desc    Descriptor
	restURL string
	secrets Secrets
}

// secretsPathFor is where the secrets of a bench live: NEXUS_BENCH_SECRETS when
// set, else the default path of the context and the namespace.
func secretsPathFor(t *testing.T, kubeContext, namespace string) string {
	t.Helper()
	requireBench(t)
	if v := os.Getenv(envSecrets); v != "" {
		return v
	}
	path, err := DefaultSecretsPath(kubeContext, namespace, os.Getenv)
	if err != nil {
		t.Fatalf("secrets path: %v", err)
	}
	return path
}

// openLiveBench loads the descriptor and the secrets of the running bench. It
// writes nothing and fails with the command to run when the bench is not up.
func openLiveBench(t *testing.T) liveBench {
	t.Helper()
	requireBench(t)

	path := DescriptorPath()
	desc, err := LoadDescriptor(path)
	if errors.Is(err, ErrNoDescriptor) {
		t.Fatalf("%v: bring the bench up first with `mise run nexus-bench:up`", err)
	}
	if err != nil {
		t.Fatalf("LoadDescriptor(%s): %v", path, err)
	}

	var restURL string
	for _, raw := range desc.REST.URLs {
		u, err := url.Parse(raw)
		if err == nil && u.Hostname() == loopbackHost {
			restURL = raw
			break
		}
	}
	if restURL == "" {
		t.Fatalf("the descriptor lists no REST URL on %s (urls: %v)", loopbackHost, desc.REST.URLs)
	}

	store, err := NewFileStore(secretsPathFor(t, desc.KubeContext, desc.Namespace))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	secrets, found, err := store.Load()
	if err != nil {
		t.Fatalf("read the secrets: %v", err)
	}
	if !found || secrets.AdminPassword == "" {
		t.Fatal("no admin password in the secrets file: provision the bench with `mise run nexus-bench:provision`")
	}
	return liveBench{desc: desc, restURL: restURL, secrets: secrets}
}

// admin returns a client authenticated as the admin account.
func (b liveBench) admin(t *testing.T) *client {
	t.Helper()
	requireBench(t)
	c := newClient(b.restURL, adminUserID, b.secrets.AdminPassword)
	t.Cleanup(c.closeIdle)
	return c
}

// anonymous returns a client that sends no credentials.
func (b liveBench) anonymous(t *testing.T) *client {
	t.Helper()
	requireBench(t)
	c := newClient(b.restURL, "", "")
	t.Cleanup(c.closeIdle)
	return c
}

// puller returns a client authenticated as the read-only account.
func (b liveBench) puller(t *testing.T) *client {
	t.Helper()
	requireBench(t)
	if b.secrets.PullPassword == "" {
		t.Fatalf("no %s password in the secrets file: provision the bench with `mise run nexus-bench:provision`", pullUserID)
	}
	c := newClient(b.restURL, pullUserID, b.secrets.PullPassword)
	t.Cleanup(c.closeIdle)
	return c
}

// liveRepository is one entry of GET /service/rest/v1/repositories.
type liveRepository struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Type   string `json:"type"`
}

// liveGroup and liveProxy hold the one field of a repository the tests compare.
type liveGroup struct {
	Group struct {
		MemberNames []string `json:"memberNames"`
	} `json:"group"`
}

type liveProxy struct {
	Proxy struct {
		RemoteURL string `json:"remoteUrl"`
	} `json:"proxy"`
}

func TestIntegrationBenchDescribed(t *testing.T) {
	requireBench(t)
	b := openLiveBench(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveCallTimeout)
	defer cancel()

	t.Run("the REST status answers 200 on the loopback URL", func(t *testing.T) {
		resp, err := b.anonymous(t).do(ctx, request{op: "read the Nexus status", method: http.MethodGet, path: restStatusPath})
		if err != nil {
			t.Fatalf("GET %s: %v", restStatusPath, err)
		}
		if resp.status != http.StatusOK {
			t.Errorf("GET %s = HTTP %d, want 200", restStatusPath, resp.status)
		}
	})

	admin := b.admin(t)
	var listed []liveRepository
	if err := admin.read(ctx, "list the repositories", restAPI+"/repositories", nil, &listed); err != nil {
		t.Fatalf("list the repositories: %v", err)
	}

	for _, want := range b.desc.Repositories {
		t.Run("repository "+want.Name, func(t *testing.T) {
			i := slices.IndexFunc(listed, func(r liveRepository) bool { return r.Name == want.Name })
			if i < 0 {
				t.Fatalf("repository %s is not on the instance", want.Name)
			}
			if got := listed[i]; got.Type != want.Type || got.Format != "docker" {
				t.Errorf("repository %s is %s/%s, want docker/%s", want.Name, got.Format, got.Type, want.Type)
			}
			path := dockerReposPath + "/" + want.Type + "/" + want.Name
			switch want.Type {
			case "group":
				var g liveGroup
				if err := admin.read(ctx, "read the group "+want.Name, path, nil, &g); err != nil {
					t.Fatal(err)
				}
				got, members := slices.Clone(g.Group.MemberNames), slices.Clone(want.Members)
				slices.Sort(got)
				slices.Sort(members)
				if !slices.Equal(got, members) {
					t.Errorf("members of %s = %v, want %v", want.Name, got, members)
				}
			case "proxy":
				var p liveProxy
				if err := admin.read(ctx, "read the proxy "+want.Name, path, nil, &p); err != nil {
					t.Fatal(err)
				}
				if p.Proxy.RemoteURL != want.RemoteURL {
					t.Errorf("remote URL of %s = %q, want %q", want.Name, p.Proxy.RemoteURL, want.RemoteURL)
				}
			}
		})
	}
}

func TestIntegrationPullAccountIsReadOnly(t *testing.T) {
	requireBench(t)
	b := openLiveBench(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveCallTimeout)
	defer cancel()
	pull := b.puller(t)

	t.Run("it reads the repositories", func(t *testing.T) {
		resp, err := pull.do(ctx, request{op: "list the repositories as " + pullUserID, method: http.MethodGet, path: restAPI + "/repositories"})
		if err != nil {
			t.Fatal(err)
		}
		if resp.status != http.StatusOK {
			t.Errorf("GET /repositories as %s = HTTP %d, want 200", pullUserID, resp.status)
		}
	})

	t.Run("it cannot create a repository", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"name": readOnlyProbeRepository, "online": true,
			"storage": map[string]any{"blobStoreName": defaultBlobStore, "strictContentTypeValidation": true, "writePolicy": "ALLOW"},
			"docker":  map[string]any{"v1Enabled": false, "forceBasicAuth": false},
		})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := pull.do(ctx, request{
			op: "create a repository as " + pullUserID, method: http.MethodPost,
			path: dockerReposPath + "/hosted", contentType: "application/json", body: body,
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.status >= 200 && resp.status <= 299 {
			// The account can write: remove what it made, then fail.
			cleanup := b.admin(t)
			_, delErr := cleanup.expect(ctx, request{op: "delete the probe repository", method: http.MethodDelete, path: restAPI + "/repositories/" + readOnlyProbeRepository})
			t.Fatalf("POST a repository as %s = HTTP %d, want 403; the probe repository was deleted again (error: %v)", pullUserID, resp.status, delErr)
		}
		if resp.status != http.StatusForbidden {
			t.Errorf("POST a repository as %s = HTTP %d, want 403", pullUserID, resp.status)
		}
	})
}

func TestIntegrationAnonymousAsDescribed(t *testing.T) {
	requireBench(t)
	b := openLiveBench(t)
	ctx, cancel := context.WithTimeout(context.Background(), liveCallTimeout)
	defer cancel()

	i := slices.IndexFunc(b.desc.Accounts, func(a DescriptorAccount) bool { return a.UserID == anonymousUser })
	if i < 0 || b.desc.Accounts[i].Enabled == nil {
		t.Fatal("the descriptor does not say whether anonymous access is enabled")
	}
	enabled := *b.desc.Accounts[i].Enabled

	t.Run("the instance setting", func(t *testing.T) {
		var got struct {
			Enabled bool   `json:"enabled"`
			UserID  string `json:"userId"`
		}
		if err := b.admin(t).read(ctx, "read the anonymous access", anonymousPath, nil, &got); err != nil {
			t.Fatal(err)
		}
		if got.Enabled != enabled {
			t.Errorf("anonymous access enabled = %v, the descriptor says %v", got.Enabled, enabled)
		}
	})

	// Without credentials the repository list is answered when anonymous access
	// is on and refused when it is off (hypothesis H-REST, first run at step 10).
	t.Run("what a request without credentials gets", func(t *testing.T) {
		want := http.StatusUnauthorized
		if enabled {
			want = http.StatusOK
		}
		resp, err := b.anonymous(t).do(ctx, request{op: "list the repositories anonymously", method: http.MethodGet, path: restAPI + "/repositories"})
		if err != nil {
			t.Fatal(err)
		}
		if resp.status != want {
			t.Errorf("GET /repositories without credentials = HTTP %d, want %d (anonymous enabled: %v)", resp.status, want, enabled)
		}
	})
}

// ---- the data set --------------------------------------------------------------

// datasetKey is how a listing and the descriptor name an image tag.
func datasetKey(name, tag string) string { return name + ":" + tag }

// indexComponents maps "name:tag" to the component a listing holds.
func indexComponents(list []component) map[string]component {
	out := make(map[string]component, len(list))
	for _, c := range list {
		out[datasetKey(c.Name, c.Version)] = c
	}
	return out
}

// inspectThroughNexus reads the manifest digest of an image with skopeo, by the
// path of its repository, as the pull account. It is the fallback of a listing
// that gives no digest, and it refuses to ask for what the bench keeps out of
// the proxy: reading mirror/uncached through docker-proxy would cache it (F9).
func inspectThroughNexus(ctx context.Context, t *testing.T, b liveBench, repository string, img DescriptorImage) string {
	t.Helper()
	requireBench(t)
	if img.Location != string(LocationHosted) && !img.CachedInProxy {
		t.Fatalf("refusing to read %s through %s: it would cache it", datasetKey(img.Name, img.Tag), repository)
	}
	if b.secrets.PullPassword == "" {
		t.Fatalf("no %s password in the secrets file: provision the bench with `mise run nexus-bench:provision`", pullUserID)
	}
	copier, err := NewSkopeoCopier()
	if err != nil {
		t.Fatalf("the REST listing gives no digest for %s and skopeo is not usable: %v", datasetKey(img.Name, img.Tag), err)
	}
	ref := nexusImageRef(Params{HTTPNodePort: b.desc.REST.NodePort}, repository, DatasetImage{Name: img.Name, Tag: img.Tag})
	digest, err := copier.ManifestDigest(ctx, ref, &Credentials{User: pullUserID, Password: b.secrets.PullPassword})
	if err != nil {
		t.Fatalf("read the manifest of %s in %s: %v", datasetKey(img.Name, img.Tag), repository, err)
	}
	return digest
}

// TestIntegrationDatasetAsDescribed checks, read only, that the bench holds the
// data set its descriptor lists: every hosted image and every image the proxy
// has cached carries the digest of the descriptor, the image the proxy has never
// been asked for is not in docker-proxy, and docker-proxy points at the upstream
// of the descriptor and is a member of docker-group (the report of NX-01).
func TestIntegrationDatasetAsDescribed(t *testing.T) {
	requireBench(t)
	b := openLiveBench(t)
	if len(b.desc.Dataset) == 0 {
		t.Fatal("the descriptor lists no data set: run `mise run nexus-bench:up` or `mise run nexus-bench:seed` first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveCallTimeout)
	defer cancel()
	admin := b.admin(t)

	hosted, err := admin.listComponents(ctx, dockerHostedRepository)
	if err != nil {
		t.Fatalf("list the components of %s: %v", dockerHostedRepository, err)
	}
	proxied, err := admin.listComponents(ctx, dockerProxyRepository)
	if err != nil {
		t.Fatalf("list the components of %s: %v", dockerProxyRepository, err)
	}
	inHosted, inProxy := indexComponents(hosted), indexComponents(proxied)

	for _, img := range b.desc.Dataset {
		key := datasetKey(img.Name, img.Tag)
		switch {
		case img.Location == string(LocationHosted):
			t.Run("hosted "+key, func(t *testing.T) {
				c, ok := inHosted[key]
				if !ok {
					t.Fatalf("%s is not in %s", key, dockerHostedRepository)
				}
				got := c.Digest
				if got == "" {
					got = inspectThroughNexus(ctx, t, b, dockerHostedRepository, img)
				}
				if got != img.Digest {
					t.Errorf("digest of %s in %s = %s, the descriptor says %s", key, dockerHostedRepository, got, img.Digest)
				}
			})
		case img.CachedInProxy:
			t.Run("cached "+key, func(t *testing.T) {
				c, ok := inProxy[key]
				if !ok {
					t.Fatalf("%s is not in %s: the seed pulls it through the proxy once", key, dockerProxyRepository)
				}
				got := c.Digest
				if got == "" {
					got = inspectThroughNexus(ctx, t, b, dockerProxyRepository, img)
				}
				if got != img.Digest {
					t.Errorf("digest of %s in %s = %s, the descriptor says %s", key, dockerProxyRepository, got, img.Digest)
				}
			})
		default:
			// Read by the REST listing only: a pull through the proxy or the group
			// would cache it (F9).
			t.Run("uncached "+key, func(t *testing.T) {
				if _, ok := inProxy[key]; ok {
					t.Errorf("%s is in %s although no one asked the proxy for it: run `mise run nexus-bench:reset`", key, dockerProxyRepository)
				}
			})
		}
	}

	t.Run("the proxy and the group", func(t *testing.T) {
		var wantProxy *DescriptorRepository
		for i, r := range b.desc.Repositories {
			if r.Name == dockerProxyRepository {
				wantProxy = &b.desc.Repositories[i]
			}
		}
		if wantProxy == nil {
			t.Fatalf("the descriptor lists no %s: the proxy is created since NX-02, run `mise run nexus-bench:up`", dockerProxyRepository)
		}
		var proxy liveProxy
		if err := admin.read(ctx, "read the proxy "+dockerProxyRepository, dockerReposPath+"/proxy/"+dockerProxyRepository, nil, &proxy); err != nil {
			t.Fatalf("%s does not exist on the instance: %v", dockerProxyRepository, err)
		}
		if proxy.Proxy.RemoteURL != wantProxy.RemoteURL {
			t.Errorf("remote URL of %s = %q, the descriptor says %q", dockerProxyRepository, proxy.Proxy.RemoteURL, wantProxy.RemoteURL)
		}
		var group liveGroup
		if err := admin.read(ctx, "read the group "+dockerGroupRepository, dockerReposPath+"/group/"+dockerGroupRepository, nil, &group); err != nil {
			t.Fatal(err)
		}
		for _, member := range []string{dockerHostedRepository, dockerProxyRepository} {
			if !slices.Contains(group.Group.MemberNames, member) {
				t.Errorf("%s is not a member of %s (members: %v)", member, dockerGroupRepository, group.Group.MemberNames)
			}
		}
	})
}

// ---- lifecycle --------------------------------------------------------------

// logWriter sends what the bench prints to the test log, line by line.
type logWriter struct{ t *testing.T }

func (w logWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		w.t.Log(line)
	}
	return len(p), nil
}

// benchSnapshot is what has to be identical after a second up and provision.
type benchSnapshot struct {
	Repositories []string
	Roles        []string
	Users        []string
	// Hosted and Proxy are the components of docker-hosted and docker-proxy,
	// "name:tag@digest", sorted (DEVDESK-505: a second up leaves them alone).
	Hosted []string
	Proxy  []string
}

// snapshotBench lists repositories, roles and users as admin, sorted.
func snapshotBench(ctx context.Context, t *testing.T, c *client) benchSnapshot {
	t.Helper()
	names := func(op, path, field string) []string {
		var rows []map[string]any
		if err := c.read(ctx, op, path, nil, &rows); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			s, _ := row[field].(string)
			out = append(out, s)
		}
		slices.Sort(out)
		return out
	}
	images := func(repo string) []string {
		list, err := c.listComponents(ctx, repo)
		if err != nil {
			t.Fatalf("list the components of %s: %v", repo, err)
		}
		out := make([]string, 0, len(list))
		for _, comp := range list {
			out = append(out, datasetKey(comp.Name, comp.Version)+"@"+comp.Digest)
		}
		slices.Sort(out)
		return out
	}
	return benchSnapshot{
		Repositories: names("list the repositories", restAPI+"/repositories", "name"),
		Roles:        names("list the roles", rolesPath, "id"),
		Users:        names("list the users", usersPath, "userId"),
		Hosted:       images(dockerHostedRepository),
		Proxy:        images(dockerProxyRepository),
	}
}

func TestIntegrationLifecycle(t *testing.T) {
	requireBench(t)
	if os.Getenv(benchLifecycleEnv) != "1" {
		t.Skipf("set %s=1 as well to run the destructive lifecycle (it deletes the bench namespace)", benchLifecycleEnv)
	}

	p, err := FromEnv(Defaults(), os.Getenv)
	if err != nil {
		t.Fatalf("parameters: %v", err)
	}
	if !p.AcceptEULA {
		t.Fatalf("the lifecycle provisions Nexus, which needs the EULA accepted: read it, then set %s=yes", envAcceptEULA)
	}
	if deadline, ok := t.Deadline(); ok && time.Until(deadline) < lifecycleMinTimeout {
		t.Fatalf("go test stops this run in %s; the lifecycle needs at least %s: rerun with -timeout 60m", time.Until(deadline).Round(time.Second), lifecycleMinTimeout)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("parameters: %v", err)
	}

	secretsPath := p.SecretsPath
	if secretsPath == "" {
		secretsPath = secretsPathFor(t, p.Context, p.Namespace)
	}
	runner, err := NewExecRunner(p.Context)
	if err != nil {
		t.Fatalf("NewExecRunner: %v", err)
	}
	store, err := NewFileStore(secretsPath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if !p.UpstreamIsBench() {
		t.Fatalf("the lifecycle seeds the bench upstream: unset %s", envUpstreamURL)
	}
	if _, err := exec.LookPath("skopeo"); err != nil {
		t.Fatalf("the lifecycle seeds the bench with skopeo, which is a prerequisite of the host: %v", err)
	}
	// The command that `mise run nexus-bench:up` runs, built once: the lifecycle
	// compares the descriptor.json it really writes, not one built in memory.
	root := miseRepoRoot(t)
	binary := filepath.Join(t.TempDir(), "nexus-bench")
	build := exec.Command("go", "build", "-o", binary, "./internal/nexusbench/cmd/nexus-bench")
	build.Dir = root
	if built, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the nexus-bench command: %v\n%s", err, built)
	}
	clock, prober, out := NewClock(), NewProber(), logWriter{t}
	ctx := context.Background()
	initial := NewKubectlInitialPassword(runner, p.Namespace)

	up := func() {
		t.Helper()
		if _, err := Deploy(ctx, runner, prober, clock, p, out); err != nil {
			t.Fatalf("up: %v", err)
		}
		if err := EnsureUpstream(ctx, runner, clock, p, out); err != nil {
			t.Fatalf("up, the upstream: %v", err)
		}
	}
	// commandUp runs `nexus-bench up` with the descriptor written to path, and
	// returns the bytes of that file: Deploy, the upstream, Provision, Seed.
	commandUp := func(path string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, "up")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), envDescriptor+"="+path, envSecrets+"="+secretsPath, envAcceptEULA+"=yes")
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			t.Fatalf("nexus-bench up: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("nexus-bench up wrote no descriptor: %v", err)
		}
		return data
	}
	down := func() {
		t.Helper()
		if err := Destroy(ctx, runner, clock, store, p, out); err != nil {
			t.Fatalf("down: %v", err)
		}
		_, stderr, err := runner.Run(ctx, nil, "get", "namespace", p.Namespace)
		if err == nil || !isNotFound(string(stderr)) {
			t.Fatalf("after down, `get namespace %s` = (err %v), want NotFound", p.Namespace, err)
		}
	}
	snapshot := func() benchSnapshot {
		t.Helper()
		secrets, found, err := store.Load()
		if err != nil || !found {
			t.Fatalf("read the secrets (found %v): %v", found, err)
		}
		c := newClient(RESTBaseURL(loopbackHost, p), adminUserID, secrets.AdminPassword)
		defer c.closeIdle()
		return snapshotBench(ctx, t, c)
	}

	t.Log("first nexus-bench up: deploy, provision and seed")
	descriptorPath := filepath.Join(t.TempDir(), "descriptor.json")
	firstDescriptor := commandUp(descriptorPath)
	desc, err := LoadDescriptor(descriptorPath)
	if err != nil {
		t.Fatalf("the descriptor written by up: %v", err)
	}
	first := snapshot()
	for _, want := range desc.Repositories {
		if !slices.Contains(first.Repositories, want.Name) {
			t.Fatalf("after up the instance lists %v, want the repository %s of the descriptor", first.Repositories, want.Name)
		}
	}
	// up populates: the data set is in the repositories and in the descriptor.
	if len(desc.Dataset) != len(Dataset()) || len(first.Hosted) == 0 || len(first.Proxy) == 0 {
		t.Fatalf("after up: %d images described, %d hosted components, %d proxy components, want the whole data set (%d images)",
			len(desc.Dataset), len(first.Hosted), len(first.Proxy), len(Dataset()))
	}

	t.Log("second nexus-bench up: the same descriptor.json, byte for byte, and the same components")
	if second := commandUp(descriptorPath); !bytes.Equal(firstDescriptor, second) {
		t.Errorf("a second up wrote another descriptor.json:\nfirst  %s\nsecond %s", firstDescriptor, second)
	}
	if second := snapshot(); !reflect.DeepEqual(first, second) {
		t.Errorf("a second up changed the instance:\nfirst  %+v\nsecond %+v", first, second)
	}

	t.Log("down")
	down()

	t.Log("up on a fresh instance")
	up()
	password, ok, err := initial.Read(ctx)
	if err != nil {
		t.Fatalf("read the initial password: %v", err)
	}
	if !ok || password == "" {
		t.Error("a fresh instance has no admin.password in the pod")
	}

	t.Log("final down")
	down()
}
