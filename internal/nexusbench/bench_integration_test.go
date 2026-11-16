package nexusbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
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
//   - TestIntegrationLifecycle: destructive (up, provision, down, up, down), run
//     only with DEVDESK_NEXUS_BENCH_LIFECYCLE=1 and NEXUS_BENCH_ACCEPT_EULA=yes.
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
	return benchSnapshot{
		Repositories: names("list the repositories", restAPI+"/repositories", "name"),
		Roles:        names("list the roles", rolesPath, "id"),
		Users:        names("list the users", usersPath, "userId"),
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
	clock, prober, out := NewClock(), NewProber(), logWriter{t}
	ctx := context.Background()
	initial := NewKubectlInitialPassword(runner, p.Namespace)

	up := func() {
		t.Helper()
		if _, err := Deploy(ctx, runner, prober, clock, p, out); err != nil {
			t.Fatalf("up: %v", err)
		}
	}
	provision := func() ProvisionResult {
		t.Helper()
		if _, err := RunPreflight(ctx, runner, p, OpProvision); err != nil {
			t.Fatalf("provision preflight: %v", err)
		}
		res, err := Provision(ctx, RESTBaseURL(loopbackHost, p), initial, store, p, out)
		if err != nil {
			t.Fatalf("provision: %v", err)
		}
		return res
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

	t.Log("first up and provision")
	up()
	desc := BuildDescriptor(p, DeployResult{}, provision())
	first := snapshot()
	for _, want := range desc.Repositories {
		if !slices.Contains(first.Repositories, want.Name) {
			t.Fatalf("after provision the instance lists %v, want the repository %s of the descriptor", first.Repositories, want.Name)
		}
	}

	t.Log("second up and provision: nothing changes")
	up()
	provision()
	if second := snapshot(); !reflect.DeepEqual(first, second) {
		t.Errorf("a second up and provision changed the instance:\nfirst  %+v\nsecond %+v", first, second)
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
