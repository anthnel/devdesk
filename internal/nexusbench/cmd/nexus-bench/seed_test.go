package main

// Tests of the seed side of the nexus-bench command (DEVDESK-544, NX-02a.10,
// story DEVDESK-505), written before the code. They pin down, on the `deps` of
// main.go and the harness of main_test.go:
//
//   - the `seed` subcommand: preflight(OpSeed), then Seed with the SeedDeps built
//     from deps (runner, forwarder, copier, clock, store), the descriptor written
//     only after a Seed that succeeded;
//   - `up` as D-J says: Deploy, EnsureUpstream, Provision, Seed, descriptor with
//     the whole data set; nothing is written after a failure;
//   - `up` with an upstream URL the bench did not deploy: no EnsureUpstream, no
//     Seed, the descriptor of NX-01 (an empty data set), and it says so;
//   - the context Seed is given, bounded by Params.Timeout;
//   - `status`: the `upstream:` and `dataset:` lines, read only.
//
// cut-upstream, restore-upstream and reset are NX-02b: nothing here tests them.
//
// Interpretations the tests fix (the report says them again):
//
//   - `seed` has no data of its own about Nexus (version, edition, EULA): it
//     carries them over from the descriptor of the previous run, and refuses to
//     seed when there is none readable (run `up` first). The hosts come from the
//     preflight node address, as for `provision`.
//   - `seed` takes --rest-host (the admin credentials go there) and no
//     --accept-eula.
//   - the constructors of the doors (newForwarder, newCopier) run before the
//     cluster is touched, so a missing skopeo stops `up` before it deploys; they
//     are not built when the seed will not run (custom upstream, status).
//   - `status` reads the data set against the descriptor, with the digests of
//     the REST listing of docker-hosted and docker-proxy, anonymously.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// clusterEvents keeps what touches the cluster, in order: the preflights and
// the library calls. The constructors (newRunner, newStore, ...) are left out.
func (h *harness) clusterEvents() []string {
	var out []string
	for _, e := range h.eventList() {
		switch {
		case strings.HasPrefix(e, "preflight:"), e == "deploy", e == "ensureUpstream", e == "provision", e == "seed", e == "destroy":
			out = append(out, e)
		}
	}
	return out
}

// before reports whether event a is logged, and before event b (which may be
// absent).
func (h *harness) before(a, b string) bool {
	i, j := h.index(a), h.index(b)
	return i >= 0 && (j < 0 || i < j)
}

// previousProvisioning is what the descriptor of the previous run says of
// Nexus; the seed carries it over.
var previousProvisioning = nexusbench.ProvisionResult{NexusVersion: "3.97.0-02", Edition: "COMMUNITY", EULAAccepted: true}

// writePreviousDescriptor writes the descriptor `up` left before the seed ran:
// provisioned, with an empty data set.
func (h *harness) writePreviousDescriptor() []byte {
	h.t.Helper()
	return h.writePreviousDescriptorFor(nil)
}

// previousHosts and previousDeployment are what the previous `up` reached: the
// loopback and the node. The seed must not lose the node.
var (
	previousHosts      = []string{"127.0.0.1", "192.168.1.31"}
	previousDeployment = nexusbench.DeployResult{ReachableHosts: previousHosts, NodeIP: "192.168.1.31"}
)

// writePreviousDescriptorFor is writePreviousDescriptor for a bench whose
// parameters are the defaults changed by mod. The UpstreamURL follows the
// namespace, as the command does.
func (h *harness) writePreviousDescriptorFor(mod func(*nexusbench.Params)) []byte {
	h.t.Helper()
	p := nexusbench.Defaults()
	if mod != nil {
		mod(&p)
		p.UpstreamURL = "http://bench-upstream." + p.Namespace + ".svc.cluster.local:5000"
	}
	return writeDescriptorAt(h.t, h.descriptor, p, previousDeployment, previousProvisioning, nexusbench.SeedResult{})
}

// mustParseDescriptor reads the bytes of a descriptor file.
func mustParseDescriptor(t *testing.T, data []byte) nexusbench.Descriptor {
	t.Helper()
	var d nexusbench.Descriptor
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("the descriptor is not JSON: %v\n%s", err, data)
	}
	return d
}

// ---- seed -----------------------------------------------------------------

func TestSeedRunsPreflightThenSeedThenWritesTheDescriptor(t *testing.T) {
	h := newHarness(t)
	seedParams := func(p *nexusbench.Params) { p.Context, p.Namespace, p.HTTPNodePort = "ctx-seed", "nb-seed", 31001 }
	previous := h.writePreviousDescriptorFor(seedParams)
	code, stdout, stderr := h.run("seed", "--context", "ctx-seed", "--namespace", "nb-seed", "--http-nodeport", "31001")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	// The doors are built before the cluster is touched; then preflight, Seed.
	if got, want := h.clusterEvents(), []string{"preflight:OpSeed", "seed"}; !slices.Equal(got, want) {
		t.Fatalf("cluster events = %v, want %v (all: %v)", got, want, h.eventList())
	}
	for _, e := range []string{"newRunner", "newStore", "newForwarder", "newCopier"} {
		if !h.before(e, "preflight:OpSeed") {
			t.Errorf("%s must happen before the preflight, events: %v", e, h.eventList())
		}
	}
	for _, e := range []string{"deploy", "provision", "ensureUpstream", "destroy", "newInitial"} {
		if h.has(e) {
			t.Errorf("seed called %s, events: %v", e, h.eventList())
		}
	}
	if len(h.preflightOps) != 1 || h.preflightOps[0] != nexusbench.OpSeed {
		t.Errorf("preflight ops = %v, want [OpSeed]", h.preflightOps)
	}
	if h.preflightRunner != nexusbench.Runner(h.runner) {
		t.Error("the preflight was not given the runner of the deps")
	}
	if !reflect.DeepEqual(h.newForwarderContexts, []string{"ctx-seed"}) {
		t.Errorf("newForwarder contexts = %v, want [ctx-seed]", h.newForwarderContexts)
	}

	// What Seed was given: the doors of the deps and nothing else.
	d := h.seedDeps
	if d.Runner != nexusbench.Runner(h.runner) || d.Forwarder != nexusbench.PortForwarder(h.forward) ||
		d.Copier != nexusbench.ImageCopier(h.copier) || d.Clock != nexusbench.Clock(h.clock) || d.Store != nexusbench.SecretStore(h.store) {
		t.Errorf("SeedDeps = %+v, want the runner, forwarder, copier, clock and store of the deps", d)
	}
	if want := "http://127.0.0.1:31001"; h.seedURL != want {
		t.Errorf("Seed base URL = %q, want %q", h.seedURL, want)
	}
	if h.seedParams.Namespace != "nb-seed" || h.seedParams.Context != "ctx-seed" || h.seedParams.HTTPNodePort != 31001 {
		t.Errorf("Seed params = %+v", h.seedParams)
	}
	if !h.descriptorSeenAtSeed {
		t.Error("the descriptor of the previous run should still be there while Seed runs")
	}

	// The descriptor: the previous provisioning, the previous hosts (the
	// preflight of a seed does not read the node, so it has no NodeIP to give),
	// the data set Seed returned.
	if h.preflightRes.NodeIP == "" {
		t.Fatal("test setup: the preflight of the harness has a node IP, the seed must not need it")
	}
	p := nexusbench.Defaults()
	seedParams(&p)
	p.UpstreamURL = "http://bench-upstream.nb-seed.svc.cluster.local:5000"
	got, err := os.ReadFile(h.descriptor)
	if err != nil {
		t.Fatalf("no descriptor: %v", err)
	}
	want := wantDescriptorBytesWith(t, p, previousDeployment, previousProvisioning, h.seedRes)
	if !bytes.Equal(got, want) {
		t.Errorf("descriptor bytes differ from BuildDescriptor with the previous hosts:\n got %s\nwant %s", got, want)
	}
	// B1: after an up, a seed keeps the node address and the URLs that depend on it.
	prev := mustParseDescriptor(t, previous)
	now := mustParseDescriptor(t, got)
	if !reflect.DeepEqual(now.Hosts, previousHosts) {
		t.Errorf("hosts after the seed = %v, want the hosts of the previous descriptor %v", now.Hosts, previousHosts)
	}
	if !reflect.DeepEqual(now.REST, prev.REST) || !reflect.DeepEqual(now.DockerConnectors, prev.DockerConnectors) {
		t.Errorf("rest or dockerConnectors changed by the seed:\nbefore %+v %+v\nafter  %+v %+v", prev.REST, prev.DockerConnectors, now.REST, now.DockerConnectors)
	}

	// Output: the lines of Seed (`seed: ...`) on stdout, the descriptor path.
	if !strings.Contains(stdout, "seed: "+progressMarker) {
		t.Errorf("stdout lost the `seed: ...` lines of Seed:\n%s", stdout)
	}
	if !strings.Contains(stdout, h.descriptor) {
		t.Errorf("stdout does not say where the descriptor is (%s):\n%s", h.descriptor, stdout)
	}
	requireClean(t, stdout, stderr)
}

func TestSeedDoesNotNeedTheEULA(t *testing.T) {
	h := newHarness(t)
	h.writePreviousDescriptor()
	if code, stdout, stderr := h.run("seed"); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if h.seedParams.AcceptEULA {
		t.Error("seed turned the EULA consent on")
	}
	// The EULA is for provisioning: seed refuses the flag like status does.
	h2 := newHarness(t)
	if code, _, stderr := h2.run("seed", "--accept-eula"); code != 2 || strings.TrimSpace(stderr) == "" {
		t.Errorf("seed --accept-eula: exit code = %d, stderr %q, want 2 and a message", code, stderr)
	}
	h2.requireNoCalls()
}

// A failure at any step stops the seed, and the descriptor is written only after
// a Seed that succeeded: a partial SeedResult is never described.
func TestSeedDescriptorIsWrittenOnlyAfterASuccessfulSeed(t *testing.T) {
	be := benchError()
	skopeoMissing := &nexusbench.BenchError{Kind: nexusbench.SkopeoMissing, Cause: "skopeo was not found on PATH", Check: "install skopeo", Err: errors.New("exec: " + secretMarker)}
	tests := []struct {
		name      string
		setup     func(h *harness)
		wantText  string // the exact text of stderr, trimmed
		notCalled []string
	}{
		{"seed fails", func(h *harness) {
			h.seedErr = be
			// Seed returns what it had put in place together with the error.
			h.seedRes = nexusbench.SeedResult{Images: nexusbench.Dataset()[:2]}
		}, be.Error(), nil},
		{"seed fails with an error that is not a BenchError", func(h *harness) {
			// NX-01 behavior of fail(): an error that is not a BenchError is
			// printed as it is (it holds no secret here).
			h.seedErr = errors.New("seed: the copy was interrupted")
		}, "seed: the copy was interrupted", nil},
		{"seed fails with a secret in the cause of a BenchError", func(h *harness) {
			// The cause (Err) is never printed: requireClean applies.
			h.seedErr = &nexusbench.BenchError{
				Kind: nexusbench.SeedFailed, Cause: "a copy was refused", Check: "run the seed again",
				Err: errors.New("skopeo said: " + secretMarker),
			}
		}, "", nil},
		{"preflight fails", func(h *harness) { h.preflightErr = be }, be.Error(), []string{"seed"}},
		{"skopeo is missing", func(h *harness) { h.newCopierErr = skopeoMissing }, skopeoMissing.Error(), []string{"preflight:OpSeed", "seed"}},
		{"kubectl is missing", func(h *harness) {
			h.newRunnerErr = &nexusbench.BenchError{Kind: nexusbench.KubectlMissing, Cause: "kubectl was not found on PATH", Check: "run `mise install`"}
		}, "", []string{"preflight:OpSeed", "seed"}},
		{"the forwarder cannot be built", func(h *harness) { h.newForwarderErr = be }, be.Error(), []string{"preflight:OpSeed", "seed"}},
		{"the secrets path is refused", func(h *harness) {
			h.newStoreErr = &nexusbench.BenchError{Cause: "the secrets path is in a repository and not ignored", Check: "set NEXUS_BENCH_SECRETS"}
		}, "", []string{"preflight:OpSeed", "seed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			tt.setup(h)
			before := h.writePreviousDescriptor()
			code, stdout, stderr := h.run("seed")
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			want := tt.wantText
			var asBench *nexusbench.BenchError
			if want == "" && errors.As(firstError(h), &asBench) {
				want = asBench.Error()
			}
			if got := strings.TrimSpace(stderr); got != want {
				t.Errorf("stderr = %q, want exactly %q", got, want)
			}
			requireClean(t, stdout, stderr)
			for _, e := range tt.notCalled {
				if h.has(e) {
					t.Errorf("%s must not run after this failure, events: %v", e, h.eventList())
				}
			}
			after, err := os.ReadFile(h.descriptor)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
			}
		})
	}
}

// The seed has no data of its own about Nexus: without a descriptor of a
// provisioned bench it stops before the first copy, and says what to run.
func TestSeedNeedsTheDescriptorOfAProvisionedBench(t *testing.T) {
	v1 := []byte(`{"schemaVersion": 1, "kubeContext": "datomir", "namespace": "nexus-bench"}` + "\n")
	tests := []struct {
		name  string
		setup func(h *harness)
	}{
		{"no descriptor", func(*harness) {}},
		{"a descriptor of schema 1", func(h *harness) {
			if err := os.MkdirAll(filepath.Dir(h.descriptor), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(h.descriptor, v1, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			tt.setup(h)
			code, stdout, stderr := h.run("seed")
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, "nexus-bench:up") {
				t.Errorf("stderr does not say to run nexus-bench:up:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			if h.has("seed") {
				t.Errorf("Seed ran without a descriptor to complete: %v", h.eventList())
			}
			if data, err := os.ReadFile(h.descriptor); err == nil && !bytes.Equal(data, v1) {
				t.Error("the descriptor was rewritten")
			}
		})
	}
}

// B1 of the review: the descriptor of a bench that is not the one asked for (an
// other namespace or an other context) is not completed with the data set of
// this one: the seed stops before the first copy and says to run up.
func TestSeedRefusesTheDescriptorOfAnotherBench(t *testing.T) {
	for _, tt := range []struct {
		name     string
		previous func(*nexusbench.Params)
	}{
		{"another namespace", func(p *nexusbench.Params) { p.Namespace = "other-bench" }},
		{"another context", func(p *nexusbench.Params) { p.Context = "other-context" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			before := h.writePreviousDescriptorFor(tt.previous)
			code, stdout, stderr := h.run("seed")
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, "nexus-bench:up") {
				t.Errorf("stderr does not say to run nexus-bench:up:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			if h.has("seed") {
				t.Errorf("Seed ran with the descriptor of another bench: %v", h.eventList())
			}
			if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
				t.Errorf("the descriptor was rewritten (err=%v)", err)
			}
		})
	}
}

// F2 of the audit: seed only reads (listings) through the REST API and always
// copies through 127.0.0.1, so it has no reason to send the admin password to
// another host: it accepts --rest-host only when it stays on this machine. up and
// provision keep the flag for any host, with the clear-text warning.
func TestSeedAcceptsOnlyALoopbackRestHost(t *testing.T) {
	for _, tt := range []struct {
		name    string
		host    string
		wantURL string
	}{
		{"the loopback", "127.0.0.1", "http://127.0.0.1:30481"},
		{"the IPv6 loopback", "::1", "http://[::1]:30481"},
	} {
		t.Run("accepted "+tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.writePreviousDescriptor()
			code, stdout, stderr := h.run("seed", "--rest-host", tt.host)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if h.seedURL != tt.wantURL {
				t.Errorf("Seed base URL = %q, want %q", h.seedURL, tt.wantURL)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty on the loopback", stderr)
			}
			requireClean(t, stdout, stderr)
		})
	}
	for _, host := range []string{"192.168.1.31", "fd00::1", "nexus.lan", "10.0.0.1"} {
		t.Run("refused "+host, func(t *testing.T) {
			h := newHarness(t)
			before := h.writePreviousDescriptor()
			code, stdout, stderr := h.run("seed", "--rest-host", host)
			if code != 1 && code != 2 {
				t.Errorf("exit code = %d, want 1 or 2", code)
			}
			lower := strings.ToLower(stderr)
			if !strings.Contains(lower, "127.0.0.1") || !strings.Contains(lower, "seed") {
				t.Errorf("stderr does not say that seed always copies through 127.0.0.1:\n%s", stderr)
			}
			if strings.Contains(stderr, "credentials travel in clear text") {
				t.Errorf("seed warned instead of refusing:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			// Refused before anything: no dependency, no call, no write.
			h.requireNoCalls()
			if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
				t.Errorf("the descriptor was changed (err=%v)", err)
			}
		})
	}
	// up and provision keep the flag, with the warning that exists.
	for _, sub := range []string{"up", "provision"} {
		t.Run(sub+" keeps --rest-host", func(t *testing.T) {
			h := newHarness(t)
			code, stdout, stderr := h.run(sub, "--accept-eula", "--rest-host", "192.168.1.31")
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if !strings.Contains(stderr, "credentials travel in clear text") {
				t.Errorf("no clear-text warning:\n%s", stderr)
			}
		})
	}
	for _, host := range []string{"a/b", "user@host", "bad host", "http://127.0.0.1", "127.0.0.1:30481"} {
		t.Run("not a host "+host, func(t *testing.T) {
			h := newHarness(t)
			h.writePreviousDescriptor()
			code, stdout, stderr := h.run("seed", "--rest-host="+host)
			if (code != 1 && code != 2) || strings.TrimSpace(stderr) == "" {
				t.Errorf("exit code = %d, stderr %q", code, stderr)
			}
			requireClean(t, stdout, stderr)
			h.requireNoCalls()
		})
	}
}

func TestSeedUsageErrorsExitWithTwo(t *testing.T) {
	for _, args := range [][]string{
		{"seed", "--bogus"},
		{"seed", "extra"},
		{"seed", "--timeout", "soon"},
		{"seed", "--http-nodeport", "abc"},
		{"seed", "--namespace"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			code, stdout, stderr := h.run(args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout != "" || strings.TrimSpace(stderr) == "" {
				t.Errorf("stdout %q, stderr %q: want an empty stdout and a message", stdout, stderr)
			}
			requireClean(t, stderr)
			h.requireNoCalls()
			if h.descriptorExists() {
				t.Error("a descriptor was written")
			}
		})
	}
}

func TestSeedOfANonDefaultBenchWarnsNotToCommitTheDescriptor(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		previous func(*nexusbench.Params)
		want     bool
	}{
		{"default bench, tuned timeout", []string{"--timeout", "5m"}, nil, false},
		{"namespace", []string{"--namespace", "nexus-bench-2"}, func(p *nexusbench.Params) { p.Namespace = "nexus-bench-2" }, true},
		{"upstream image", []string{"--upstream-image", "registry.example.com/team/registry"}, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			// The descriptor of the previous run is the one of the same bench.
			h.writePreviousDescriptorFor(tt.previous)
			code, stdout, stderr := h.run(append([]string{"seed"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if got := strings.Contains(stderr, nonDefaultWarning); got != tt.want {
				t.Errorf("warning on stderr = %v, want %v:\n%s", got, tt.want, stderr)
			}
			if strings.Contains(stdout, nonDefaultWarning) {
				t.Error("the warning belongs on stderr")
			}
		})
	}
}

// ---- the context Seed is given ---------------------------------------------------

// A copy over a port-forward can hang for ever: Seed gets a context that ends
// after Params.Timeout, counted from the start of the seed, and the library does
// not have to remember it.
func TestSeedContextIsBoundedByTheTimeout(t *testing.T) {
	for _, sub := range []string{"seed", "up"} {
		for _, tt := range []struct {
			name string
			args []string
			want time.Duration
		}{
			{"the flag", []string{"--timeout", "7m"}, 7 * time.Minute},
			{"the variable of the harness", nil, 30 * time.Second},
		} {
			t.Run(sub+" "+tt.name, func(t *testing.T) {
				h := newHarness(t)
				h.writePreviousDescriptor()
				code, stdout, stderr := h.run(append(subArgs(sub), tt.args...)...)
				if code != 0 {
					t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
				}
				if !h.seedHadDeadline {
					t.Fatal("the context of Seed has no deadline")
				}
				if h.seedRemaining > tt.want || h.seedRemaining < tt.want-10*time.Second {
					t.Errorf("time left on the context of Seed = %s, want just under %s", h.seedRemaining, tt.want)
				}
			})
		}
	}
}

func TestASeedThatOutlastsTheTimeoutFailsWithoutADescriptor(t *testing.T) {
	for _, sub := range []string{"seed", "up"} {
		t.Run(sub, func(t *testing.T) {
			h := newHarness(t)
			h.blockSeed = true
			before := h.writePreviousDescriptor()
			code, stdout, stderr := h.run(append(subArgs(sub), "--timeout", "60ms")...)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Error("stderr is empty, want a message")
			}
			requireClean(t, stdout, stderr)
			after, err := os.ReadFile(h.descriptor)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
			}
		})
	}
}

func TestACancelledContextEndsASeedCleanly(t *testing.T) {
	t.Run("seed, cancelled before the run", func(t *testing.T) {
		h := newHarness(t)
		h.respectContext = true
		h.writePreviousDescriptor()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h.ctx = ctx
		code, stdout, stderr := h.run("seed")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(strings.ToLower(stderr), "cancel") || strings.Count(strings.TrimSpace(stderr), "\n") != 0 {
			t.Errorf("stderr should say in one line that the run was cancelled:\n%s", stderr)
		}
		requireClean(t, stdout, stderr)
	})
	for _, sub := range []string{"seed", "up"} {
		t.Run(sub+", cancelled during Seed", func(t *testing.T) {
			h := newHarness(t)
			h.respectContext = true
			before := h.writePreviousDescriptor()
			ctx, cancel := context.WithCancel(context.Background())
			h.ctx = ctx
			h.onSeed = cancel
			code, stdout, stderr := h.run(subArgs(sub)...)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(strings.ToLower(stderr), "cancel") {
				t.Errorf("stderr does not say the run was cancelled:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
				t.Errorf("a descriptor was written after the cancellation (err=%v)", err)
			}
		})
	}
}

// ---- up, as D-J says --------------------------------------------------------------

func TestUpDeploysEnsuresTheUpstreamProvisionsThenSeeds(t *testing.T) {
	h := newHarness(t)
	h.deployRes = nexusbench.DeployResult{ReachableHosts: []string{"127.0.0.1", "192.168.1.31"}, NodeIP: "192.168.1.31"}
	code, stdout, stderr := h.run("up", "--accept-eula")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	// The exact order of the calls that reach the cluster. Deploy runs its own
	// preflight(OpUp), so one of the command before it is allowed, for OpUp only.
	got := h.clusterEvents()
	if len(got) > 0 && got[0] == "preflight:OpUp" {
		got = got[1:]
	}
	if want := []string{"deploy", "ensureUpstream", "provision", "seed"}; !slices.Equal(got, want) {
		t.Fatalf("cluster calls = %v, want %v (all: %v)", got, want, h.eventList())
	}
	for _, op := range h.preflightOps {
		if op != nexusbench.OpUp {
			t.Errorf("up ran a preflight for %s, want OpUp only", opName(op))
		}
	}
	// Everything that can refuse comes before the first write to the cluster.
	for _, e := range []string{"newRunner", "newStore", "newForwarder", "newCopier"} {
		if !h.before(e, "deploy") {
			t.Errorf("%s must happen before Deploy, events: %v", e, h.eventList())
		}
	}
	for _, e := range []string{"destroy", "readUpstream", "readImages:docker-hosted", "readImages:docker-proxy"} {
		if h.has(e) {
			t.Errorf("up called %s: %v", e, h.eventList())
		}
	}

	// What each step was given.
	if h.ensureRunner != nexusbench.Runner(h.runner) || h.ensureClock != nexusbench.Clock(h.clock) {
		t.Error("EnsureUpstream was not given the runner and the clock of the deps")
	}
	d := h.seedDeps
	if d.Runner != nexusbench.Runner(h.runner) || d.Forwarder != nexusbench.PortForwarder(h.forward) ||
		d.Copier != nexusbench.ImageCopier(h.copier) || d.Clock != nexusbench.Clock(h.clock) || d.Store != nexusbench.SecretStore(h.store) {
		t.Errorf("SeedDeps = %+v, want the runner, forwarder, copier, clock and store of the deps", d)
	}
	if h.seedURL != "http://127.0.0.1:30481" || h.seedURL != h.provisionURL {
		t.Errorf("Seed base URL = %q, Provision base URL = %q, want the same loopback origin", h.seedURL, h.provisionURL)
	}
	if !reflect.DeepEqual(h.seedParams, h.provisionParams) || !reflect.DeepEqual(h.ensureParams, h.provisionParams) {
		t.Errorf("the steps were not given the same parameters:\nprovision %+v\nensure    %+v\nseed      %+v", h.provisionParams, h.ensureParams, h.seedParams)
	}
	if h.descriptorSeenAtSeed || h.descriptorSeenAtProvision {
		t.Error("the descriptor existed before the seed finished")
	}

	// The descriptor describes the whole data set, the proxy created and a
	// member of the group.
	got2, err := os.ReadFile(h.descriptor)
	if err != nil {
		t.Fatalf("no descriptor written: %v", err)
	}
	want := wantDescriptorBytes(t, nexusbench.Defaults(), h.deployRes, h.provisionRes)
	if !bytes.Equal(got2, want) {
		t.Errorf("descriptor bytes differ from BuildDescriptor(..., SeedResult{Dataset()}):\n got %s\nwant %s", got2, want)
	}
	desc, err := nexusbench.LoadDescriptor(h.descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Dataset) != len(nexusbench.Dataset()) {
		t.Errorf("the descriptor lists %d images, want %d", len(desc.Dataset), len(nexusbench.Dataset()))
	}
	var proxy, group bool
	for _, r := range desc.Repositories {
		proxy = proxy || r.Name == "docker-proxy"
		group = group || (r.Type == "group" && slices.Contains(r.Members, "docker-proxy") && slices.Contains(r.Members, "docker-hosted"))
	}
	if !proxy || !group {
		t.Errorf("the descriptor lacks the proxy (%v) or its place in the group (%v)", proxy, group)
	}

	// Output: the progress of every step, in the order they ran.
	last := -1
	for _, marker := range []string{progressMarker + " deploy", progressMarker + " ensureUpstream", progressMarker + " provision", "seed: " + progressMarker, h.descriptor} {
		i := strings.Index(stdout, marker)
		if i < 0 {
			t.Errorf("stdout lacks %q:\n%s", marker, stdout)
			continue
		}
		if i < last {
			t.Errorf("stdout shows %q out of order:\n%s", marker, stdout)
		}
		last = i
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty for a default bench", stderr)
	}
	requireClean(t, stdout, stderr)
}

// The descriptor is written after a complete success only, and each step stops
// the ones after it.
func TestUpStopsAtTheFirstFailureOfTheSeedSteps(t *testing.T) {
	be := benchError()
	skopeoMissing := &nexusbench.BenchError{Kind: nexusbench.SkopeoMissing, Cause: "skopeo was not found on PATH", Check: "install skopeo"}
	tests := []struct {
		name      string
		setup     func(h *harness)
		notCalled []string
	}{
		{"deploy fails", func(h *harness) { h.deployErr = be }, []string{"ensureUpstream", "provision", "seed"}},
		{"the upstream does not come up", func(h *harness) { h.ensureErr = be }, []string{"provision", "seed"}},
		{"provision fails", func(h *harness) {
			h.provisionErr = be
			h.provisionRes = nexusbench.ProvisionResult{NexusVersion: "3.98.1-01"}
		}, []string{"seed"}},
		{"seed fails", func(h *harness) {
			h.seedErr = be
			h.seedRes = nexusbench.SeedResult{Images: nexusbench.Dataset()[:3]}
		}, nil},
		// skopeo is a prerequisite of the host: it is checked before anything is
		// deployed, not after twenty minutes of waiting.
		{"skopeo is missing", func(h *harness) { h.newCopierErr = skopeoMissing }, []string{"preflight:OpUp", "deploy", "ensureUpstream", "provision", "seed"}},
		{"the forwarder cannot be built", func(h *harness) { h.newForwarderErr = be }, []string{"preflight:OpUp", "deploy", "ensureUpstream", "provision", "seed"}},
	}
	for _, tt := range tests {
		for _, existing := range []bool{false, true} {
			name := tt.name + " / no descriptor yet"
			if existing {
				name = tt.name + " / a descriptor exists"
			}
			t.Run(name, func(t *testing.T) {
				h := newHarness(t)
				tt.setup(h)
				var before []byte
				if existing {
					before = h.writePreviousDescriptor()
				}
				code, stdout, stderr := h.run("up", "--accept-eula")
				if code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
				var asBench *nexusbench.BenchError
				if !errors.As(firstError(h), &asBench) {
					t.Fatal("test setup: no BenchError programmed")
				}
				if got := strings.TrimSpace(stderr); got != asBench.Error() {
					t.Errorf("stderr = %q, want exactly %q", got, asBench.Error())
				}
				requireClean(t, stdout, stderr)
				for _, e := range tt.notCalled {
					if h.has(e) {
						t.Errorf("%s must not run after this failure, events: %v", e, h.eventList())
					}
				}
				if existing {
					if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
						t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
					}
				} else if h.descriptorExists() {
					t.Error("a descriptor was written although the run failed")
				}
			})
		}
	}
}

// My reading of the story: an upstream URL the bench did not deploy is the
// NX-01 behavior. The bench has nothing to scale there and nothing to push to,
// so up neither ensures the upstream nor seeds, says so, and describes an empty
// data set. skopeo is not needed for it.
func TestUpWithACustomUpstreamKeepsTheBehaviorOfNX01(t *testing.T) {
	const custom = "http://registry.example.com:5000"
	for _, tt := range []struct {
		name  string
		args  []string
		env   map[string]string
		setup func(h *harness)
	}{
		{"the flag", []string{"--upstream-url", custom}, nil, nil},
		{"the variable", nil, map[string]string{"NEXUS_BENCH_UPSTREAM_URL": custom}, nil},
		{"skopeo missing is not an error when nothing is seeded", []string{"--upstream-url", custom}, nil, func(h *harness) {
			h.newCopierErr = &nexusbench.BenchError{Kind: nexusbench.SkopeoMissing, Cause: "skopeo was not found on PATH", Check: "install skopeo"}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			for k, v := range tt.env {
				h.env[k] = v
			}
			if tt.setup != nil {
				tt.setup(h)
			}
			code, stdout, stderr := h.run(append([]string{"up", "--accept-eula"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			for _, e := range []string{"ensureUpstream", "seed", "newCopier", "newForwarder"} {
				if h.has(e) {
					t.Errorf("up called %s with an upstream the bench did not deploy: %v", e, h.eventList())
				}
			}
			if got := h.clusterEvents(); len(got) < 2 || got[len(got)-2] != "deploy" || got[len(got)-1] != "provision" {
				t.Errorf("calls = %v, want deploy then provision", h.eventList())
			}

			// Said on stdout, on a `seed: ` line, naming the variable.
			var said bool
			for _, line := range strings.Split(stdout, "\n") {
				if strings.HasPrefix(line, "seed: skipped") && strings.Contains(line, "NEXUS_BENCH_UPSTREAM_URL") {
					said = true
				}
			}
			if !said {
				t.Errorf("stdout has no `seed: skipped ...` line that names NEXUS_BENCH_UPSTREAM_URL:\n%s", stdout)
			}

			// The descriptor: the proxy points at the custom upstream, the data
			// set is empty (the descriptor of NX-01, schema 2).
			p := nexusbench.Defaults()
			p.UpstreamURL = custom
			got, err := os.ReadFile(h.descriptor)
			if err != nil {
				t.Fatalf("no descriptor: %v", err)
			}
			if want := wantDescriptorBytesWith(t, p, h.deployRes, h.provisionRes, nexusbench.SeedResult{}); !bytes.Equal(got, want) {
				t.Errorf("descriptor bytes differ from BuildDescriptor(..., SeedResult{}):\n got %s\nwant %s", got, want)
			}
			desc, err := nexusbench.LoadDescriptor(h.descriptor)
			if err != nil {
				t.Fatal(err)
			}
			if len(desc.Dataset) != 0 {
				t.Errorf("the descriptor lists %d images, want none", len(desc.Dataset))
			}
			requireClean(t, stdout, stderr)
		})
	}
}

// The seed of the bench upstream is refused by Seed itself for a custom URL (an
// error of the library); the command never even asks.
func TestSeedWithACustomUpstreamIsLeftToTheLibraryToRefuse(t *testing.T) {
	h := newHarness(t)
	h.writePreviousDescriptor()
	refusal := &nexusbench.BenchError{Kind: nexusbench.SeedFailed, Cause: "NEXUS_BENCH_UPSTREAM_URL points at an upstream the bench did not deploy", Check: "unset NEXUS_BENCH_UPSTREAM_URL"}
	h.seedErr = refusal
	code, stdout, stderr := h.run("seed", "--upstream-url", "http://registry.example.com:5000")
	if code != 1 || strings.TrimSpace(stderr) != refusal.Error() {
		t.Errorf("exit code = %d, stderr %q, want 1 and %q", code, stderr, refusal.Error())
	}
	requireClean(t, stdout, stderr)
}

// ---- help --------------------------------------------------------------------------

func TestHelpListsSeedAndSaysWhatItNeeds(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run("help")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	var line string
	for _, l := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "seed") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("the general help has no line for seed:\n%s", stdout)
	}
	// up now seeds: its entry says so.
	upEntry, _, _ := strings.Cut(stdout[max(strings.Index(stdout, "  up "), 0):], "  provision ")
	if !strings.Contains(strings.ToLower(upEntry), "seed") {
		t.Errorf("the entry of up does not say it seeds the bench:\n%s", stdout)
	}

	for _, sub := range []string{"seed", "up"} {
		h := newHarness(t)
		code, stdout, _ := h.run(sub, "-h")
		if code != 0 {
			t.Fatalf("%s -h: exit code = %d", sub, code)
		}
		if !strings.HasPrefix(stdout, "Usage: nexus-bench "+sub+" [flags]") {
			t.Errorf("%s -h does not start with its usage line:\n%s", sub, stdout)
		}
		// skopeo is a prerequisite of the host, not installed by mise.
		if !strings.Contains(stdout, "skopeo") {
			t.Errorf("%s -h does not name skopeo, the tool of the host the seed needs:\n%s", sub, stdout)
		}
		h.requireNoCalls()
	}
}

// ---- status: the upstream and the data set ------------------------------------------

// statusLines returns the value of every "key: value" line of stdout.
func statusLines(stdout string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		if k, v, ok := strings.Cut(line, ": "); ok && !strings.HasPrefix(line, " ") {
			out[k] = v
		}
	}
	return out
}

func TestStatusSaysWhatTheUpstreamIsDoing(t *testing.T) {
	tests := []struct {
		name  string
		state nexusbench.UpstreamState
		err   error
		want  string // lower-cased prefix of the value of the `upstream:` line
	}{
		{"ready", nexusbench.UpstreamReady, nil, "ready"},
		{"cut", nexusbench.UpstreamCut, nil, "cut (0 replicas)"},
		{"missing", nexusbench.UpstreamMissing, nil, "missing"},
		{"starting", nexusbench.UpstreamStarting, nil, "starting"},
		{"unreadable", nexusbench.UpstreamMissing, benchError(), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.upstreamState, h.upstreamErr = tt.state, tt.err
			h.writeDescriptor(nexusbench.Defaults(), h.deployRes, h.provisionRes)
			code, stdout, stderr := h.run("status")
			if code != 0 {
				t.Fatalf("exit code = %d (a finding is not a failure)\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if got := strings.ToLower(statusLines(stdout)["upstream"]); !strings.HasPrefix(got, tt.want) {
				t.Errorf("upstream line = %q, want it to start with %q:\n%s", got, tt.want, stdout)
			}
			requireClean(t, stdout, stderr) // the error carries a secret marker
			if h.readUpstreamRunner != nexusbench.Runner(h.runner) {
				t.Error("the upstream was not read through the runner of the deps")
			}
			if !h.before("preflight:OpStatus", "readUpstream") {
				t.Errorf("the upstream was read before the preflight: %v", h.eventList())
			}
		})
	}
}

// An upstream URL the bench did not deploy is nobody's to read or to cut.
func TestStatusDoesNotReadAnUpstreamTheBenchDidNotDeploy(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("status", "--upstream-url", "http://registry.example.com:5000")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if got := strings.ToLower(statusLines(stdout)["upstream"]); !strings.Contains(got, "not managed") {
		t.Errorf("upstream line = %q, want it to say the bench does not manage it:\n%s", got, stdout)
	}
	if h.has("readUpstream") {
		t.Errorf("status read an upstream the bench did not deploy: %v", h.eventList())
	}
	requireClean(t, stdout, stderr)
}

func TestStatusComparesTheDataSetWithTheDescriptor(t *testing.T) {
	images := nexusbench.Dataset()
	var hosted, cached, uncached []nexusbench.DatasetImage
	for _, img := range images {
		switch {
		case img.Location == nexusbench.LocationHosted:
			hosted = append(hosted, img)
		case img.PullThrough:
			cached = append(cached, img)
		default:
			uncached = append(uncached, img)
		}
	}
	if len(hosted) < 3 || len(cached) == 0 || len(uncached) == 0 {
		t.Fatalf("the data set has %d hosted, %d cached and %d uncached images: this test needs 3, 1 and 1", len(hosted), len(cached), len(uncached))
	}
	altered := func(list []listedImage, n int) []listedImage {
		out := slices.Clone(list)
		for i := 0; i < n; i++ {
			out[i].Digest = "sha256:" + strings.Repeat("0", 63) + string(rune('1'+i))
		}
		return out
	}
	hostedListing := datasetListing(images, "docker-hosted")
	extra := listedImage{Name: uncached[0].Name, Tag: uncached[0].Tag, Digest: uncached[0].Digest}

	tests := []struct {
		name     string
		listings map[string][]listedImage
		want     string // prefix of the value of the `dataset:` line
	}{
		// Only the hosted images and the cached ones are compared (the others are
		// checked absent from the proxy): the label counts those, not the data set.
		{"as the descriptor says", nil, "matches (" + strconv.Itoa(len(hosted)+len(cached)) + " images compared)"},
		{"two hosted digests differ", map[string][]listedImage{"docker-hosted": altered(hostedListing, 2)}, "differs (2 images)"},
		{"the proxy lacks the cached image and holds the uncached one",
			map[string][]listedImage{"docker-proxy": {extra}}, "differs (2 images)"},
		{"nothing is hosted", map[string][]listedImage{"docker-hosted": {}}, "differs (" + strconv.Itoa(len(hosted)) + " images)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.listings = tt.listings
			h.writeDescriptor(nexusbench.Defaults(), h.deployRes, h.provisionRes)
			code, stdout, stderr := h.run("status")
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if got := statusLines(stdout)["dataset"]; !strings.HasPrefix(got, tt.want) {
				t.Errorf("dataset line = %q, want it to start with %q:\n%s", got, tt.want, stdout)
			}
			requireClean(t, stdout, stderr)
			requireStatusReadsOnly(t, h)
		})
	}
}

func TestStatusSaysUnknownWhenItCannotReadTheDataSet(t *testing.T) {
	tests := []struct {
		name  string
		setup func(h *harness)
		args  []string
		// wantRead: whether the listings are read at all.
		wantRead bool
	}{
		{"the listing fails", func(h *harness) { h.readImagesErr = benchError() }, nil, true},
		{"Nexus does not answer", func(h *harness) { h.prober.err = errors.New("refused " + secretMarker) }, nil, false},
		{"anonymous access is off", func(*harness) {}, []string{"--anonymous=false"}, false},
		{"no descriptor to compare with", func(h *harness) { _ = os.Remove(h.descriptor) }, nil, false},
		{"the descriptor lists no image", func(h *harness) {
			writeDescriptorAt(h.t, h.descriptor, nexusbench.Defaults(), h.deployRes, h.provisionRes, nexusbench.SeedResult{})
		}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.writeDescriptor(nexusbench.Defaults(), h.deployRes, h.provisionRes)
			tt.setup(h)
			code, stdout, stderr := h.run(append([]string{"status"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d (unknown is a finding, not a failure)\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if got := statusLines(stdout)["dataset"]; !strings.HasPrefix(got, "unknown") {
				t.Errorf("dataset line = %q, want it to start with \"unknown\":\n%s", got, stdout)
			}
			if !tt.wantRead && (h.has("readImages:docker-hosted") || h.has("readImages:docker-proxy")) {
				t.Errorf("status read the listings although it cannot compare them: %v", h.eventList())
			}
			requireClean(t, stdout, stderr)
			requireStatusReadsOnly(t, h)
		})
	}
}

// status reads: no skopeo, no port-forward, no seed, no stored secret, and REST
// only on the loopback origin, only the two repositories that hold the data set.
func requireStatusReadsOnly(t *testing.T, h *harness) {
	t.Helper()
	for _, e := range h.eventList() {
		switch e {
		case "seed", "newCopier", "newForwarder", "newStore", "newInitial", "deploy", "provision", "ensureUpstream", "destroy":
			t.Errorf("status called %s, which is not a read: %v", e, h.eventList())
		}
		if repo, ok := strings.CutPrefix(e, "readImages:"); ok && repo != "docker-hosted" && repo != "docker-proxy" {
			t.Errorf("status listed %s: only docker-hosted and docker-proxy hold the data set, and the group must never be read (F9)", repo)
		}
	}
	for _, u := range h.readImagesURLs {
		if u != "http://127.0.0.1:30481" {
			t.Errorf("status listed images at %q, want the loopback REST origin", u)
		}
	}
}

// The data set of the descriptor is fixed in the code, so a provision of the
// same bench keeps it; a provision of another one cannot vouch for it.
func TestProvisionKeepsTheDataSetOfTheSameBenchAndDropsOthersSaying(t *testing.T) {
	const custom = "http://registry.example.com:5000"
	full := nexusbench.SeedResult{Images: nexusbench.Dataset()}
	tests := []struct {
		name     string
		previous func(*nexusbench.Params) // the bench of the descriptor on disk; nil: the default bench
		args     []string
		current  func(*nexusbench.Params) // the parameters of this run, for the expected bytes
		keeps    bool
		// seedable: the empty data set can be filled by seed, so the line may send there.
		seedable bool
	}{
		{name: "the same bench", keeps: true},
		{name: "the same bench, another timeout", args: []string{"--timeout", "5m"}, keeps: true},
		{
			name: "another namespace", previous: func(p *nexusbench.Params) { p.Namespace = "other-bench" }, seedable: true,
		},
		{
			name: "another context", previous: func(p *nexusbench.Params) { p.Context = "other-context" }, seedable: true,
		},
		{
			name: "another upstream URL", args: []string{"--upstream-url", custom},
			current: func(p *nexusbench.Params) { p.UpstreamURL = custom },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			prev := nexusbench.Defaults()
			if tt.previous != nil {
				tt.previous(&prev)
				prev.UpstreamURL = "http://bench-upstream." + prev.Namespace + ".svc.cluster.local:5000"
			}
			writeDescriptorAt(t, h.descriptor, prev, previousDeployment, previousProvisioning, full)

			code, stdout, stderr := h.run(append([]string{"provision", "--accept-eula"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			p := nexusbench.Defaults()
			if tt.current != nil {
				tt.current(&p)
			}
			wantData := nexusbench.SeedResult{}
			if tt.keeps {
				wantData = full
			}
			want := wantDescriptorBytesWith(t, p, nexusbench.DeployResult{NodeIP: h.preflightRes.NodeIP}, h.provisionRes, wantData)
			if got, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(got, want) {
				t.Errorf("descriptor bytes differ from BuildDescriptor(..., data set kept: %v) (err=%v):\n got %s\nwant %s", tt.keeps, err, got, want)
			}

			var line string
			for _, l := range strings.Split(stdout, "\n") {
				if strings.Contains(strings.ToLower(l), "no data set") {
					line = l
				}
			}
			switch {
			case tt.keeps && line != "":
				t.Errorf("provision says the data set is gone although it kept it: %q", line)
			case !tt.keeps && line == "":
				t.Errorf("provision dropped the data set without saying so:\n%s", stdout)
			case !tt.keeps && tt.seedable && !strings.Contains(strings.ToLower(line), "seed"):
				t.Errorf("the line does not say to run seed: %q", line)
			case !tt.keeps && !tt.seedable && strings.Contains(strings.ToLower(line), "run seed"):
				// An upstream the bench did not deploy is refused by seed.
				t.Errorf("the line sends to seed, which refuses this bench: %q", line)
			}
			requireClean(t, stdout, stderr)
		})
	}
}

// A descriptor that cannot be read (none, or schema 1) has no data set to keep:
// the file is written with an empty one, as in NX-01.
func TestProvisionWithoutAPreviousDescriptorWritesAnEmptyDataSet(t *testing.T) {
	h := newHarness(t)
	if code, stdout, stderr := h.run("provision", "--accept-eula"); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	desc, err := nexusbench.LoadDescriptor(h.descriptor)
	if err != nil || len(desc.Dataset) != 0 {
		t.Errorf("descriptor dataset = %d images (err=%v), want none", len(desc.Dataset), err)
	}
}

// --rest-host moves the REST origin only: the images are always copied through
// 127.0.0.1. up and provision say so and take any host; seed takes the loopback
// only and says so.
func TestRestHostHelpSaysWhatItConcerns(t *testing.T) {
	for _, tt := range []struct {
		sub  string
		want []string // lower-case words of the help of the flag
	}{
		{"up", []string{"rest", "only", "images", "127.0.0.1"}},
		{"provision", []string{"rest", "only", "images", "127.0.0.1"}},
		{"seed", []string{"loopback", "127.0.0.1", "images"}},
	} {
		t.Run(tt.sub, func(t *testing.T) {
			h := newHarness(t)
			code, stdout, _ := h.run(tt.sub, "-h")
			if code != 0 {
				t.Fatalf("exit code = %d", code)
			}
			var para []string
			in := false
			for _, l := range strings.Split(stdout, "\n") {
				switch {
				case strings.Contains(l, "-rest-host"):
					in = true
					para = append(para, l)
				case in && strings.HasPrefix(l, "  -"):
					in = false
				case in:
					para = append(para, l)
				}
			}
			text := strings.ToLower(strings.Join(para, " "))
			if text == "" {
				t.Fatalf("%s -h has no --rest-host:\n%s", tt.sub, stdout)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("the help of --rest-host lacks %q:\n%s", want, strings.Join(para, "\n"))
				}
			}
			// seed refuses what up warns about: its help must not offer other hosts.
			if tt.sub == "seed" && strings.Contains(text, "clear text") {
				t.Errorf("the help of seed talks of clear text, which seed refuses instead:\n%s", strings.Join(para, "\n"))
			}
		})
	}
}

// An empty data set in the descriptor sends to seed, except for an upstream the
// bench did not deploy, which seed refuses.
func TestStatusEmptyDataSetOnlySendsToSeedWhenSeedCanHelp(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		upstream string
		wantSeed bool
	}{
		{"the bench upstream", nil, "", true},
		{"an upstream the bench did not deploy", []string{"--upstream-url", "http://registry.example.com:5000"}, "http://registry.example.com:5000", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			p := nexusbench.Defaults()
			if tt.upstream != "" {
				p.UpstreamURL = tt.upstream
			}
			writeDescriptorAt(t, h.descriptor, p, h.deployRes, h.provisionRes, nexusbench.SeedResult{})
			code, stdout, stderr := h.run(append([]string{"status"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			line := statusLines(stdout)["dataset"]
			if !strings.HasPrefix(line, "unknown") {
				t.Fatalf("dataset line = %q, want it to start with \"unknown\"", line)
			}
			if got := strings.Contains(strings.ToLower(line), "seed"); got != tt.wantSeed {
				t.Errorf("dataset line %q mentions seed = %v, want %v", line, got, tt.wantSeed)
			}
			requireClean(t, stdout, stderr)
		})
	}
}

// ---- F1: what the descriptor of the previous run is trusted for -----------------------

// The hosts of the previous descriptor end up in the URLs of the new one: they
// are held to the rule of NEXUS_BENCH_HOSTS (an IP address or a host name) and to
// a bound on their number, or the seed stops before the first copy and says to
// run up. Nothing of the hostile text is echoed.
func TestSeedRefusesHostsThatAreNotHosts(t *testing.T) {
	many := make([]string, 100)
	for i := range many {
		many[i] = "10.0.0." + strconv.Itoa(i)
	}
	tests := []struct {
		name  string
		hosts []string
	}{
		{"a path", []string{"127.0.0.1", "registry.example.com/x"}},
		{"credentials", []string{"user@host"}},
		{"a space", []string{"bad host"}},
		{"a scheme", []string{"http://127.0.0.1"}},
		{"a port", []string{"127.0.0.1:30481"}},
		{"an empty host", []string{"127.0.0.1", ""}},
		{"an escape sequence", []string{"127.0.0.1", "\x1b]0;pwned\x07evil.example"}},
		{"a host name that is too long", []string{strings.Repeat("a", 300)}},
		{"too many hosts", many},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			before := writeDescriptorAt(t, h.descriptor, nexusbench.Defaults(), nexusbench.DeployResult{ReachableHosts: tt.hosts}, previousProvisioning, nexusbench.SeedResult{})
			code, stdout, stderr := h.run("seed")
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, "nexus-bench:up") {
				t.Errorf("stderr does not say to run nexus-bench:up:\n%s", stderr)
			}
			for _, bad := range []string{"\x1b", "\x07", "pwned"} {
				if strings.Contains(stdout+stderr, bad) {
					t.Errorf("the output echoes the hostile text (%q):\n%q", bad, stdout+stderr)
				}
			}
			requireClean(t, stdout, stderr)
			if h.has("seed") {
				t.Errorf("Seed ran with the hosts of a descriptor that is not trusted: %v", h.eventList())
			}
			if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(before, after) {
				t.Errorf("the descriptor was rewritten (err=%v)", err)
			}
		})
	}
}

// Valid hosts, host names included, are carried over as they are.
func TestSeedKeepsTheValidHostsOfThePreviousDescriptor(t *testing.T) {
	h := newHarness(t)
	hosts := []string{"127.0.0.1", "192.168.1.31", "fd00::1", "nexus.lan"}
	writeDescriptorAt(t, h.descriptor, nexusbench.Defaults(), nexusbench.DeployResult{ReachableHosts: hosts}, previousProvisioning, nexusbench.SeedResult{})
	if code, stdout, stderr := h.run("seed"); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	got, err := os.ReadFile(h.descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if now := mustParseDescriptor(t, got); !reflect.DeepEqual(now.Hosts, hosts) {
		t.Errorf("hosts after the seed = %v, want %v", now.Hosts, hosts)
	}
}

// provision does not read the hosts of the previous descriptor: whatever it
// holds, it does not reach the new one.
func TestProvisionDoesNotCarryTheHostsOfAnUntrustedDescriptor(t *testing.T) {
	h := newHarness(t)
	writeDescriptorAt(t, h.descriptor, nexusbench.Defaults(), nexusbench.DeployResult{ReachableHosts: []string{"bad host", "registry.example.com/x"}},
		previousProvisioning, nexusbench.SeedResult{Images: nexusbench.Dataset()})
	code, stdout, stderr := h.run("provision", "--accept-eula")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	got, err := os.ReadFile(h.descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "bad host") || strings.Contains(string(got), "registry.example.com/x") {
		t.Errorf("a host of the previous descriptor reached the new one:\n%s", got)
	}
}

// The data set is fixed in the code. provision does not copy the one of the file:
// it compares it to Dataset() and writes the one of the code when the file is the
// same bench's and agrees; a file that disagrees (another digest, an image added,
// removed or moved) counts as no data set, and provision says so.
func TestProvisionOnlyKeepsADataSetThatIsTheOneOfTheCode(t *testing.T) {
	zero := "sha256:" + strings.Repeat("0", 64)
	tests := []struct {
		name   string
		mutate func(images []nexusbench.DatasetImage) []nexusbench.DatasetImage
	}{
		{"another digest", func(im []nexusbench.DatasetImage) []nexusbench.DatasetImage { im[0].Digest = zero; return im }},
		{"an image added", func(im []nexusbench.DatasetImage) []nexusbench.DatasetImage {
			return append(im, nexusbench.DatasetImage{Name: "evil/img", Tag: "1", Location: nexusbench.LocationHosted, Digest: zero})
		}},
		{"an image removed", func(im []nexusbench.DatasetImage) []nexusbench.DatasetImage { return im[1:] }},
		{"an image that is not cached any more", func(im []nexusbench.DatasetImage) []nexusbench.DatasetImage {
			for i := range im {
				im[i].PullThrough = false
			}
			return im
		}},
		{"an image that moved to the upstream", func(im []nexusbench.DatasetImage) []nexusbench.DatasetImage {
			im[0].Location = nexusbench.LocationUpstream
			return im
		}},
		{"another order", func(im []nexusbench.DatasetImage) []nexusbench.DatasetImage {
			im[0], im[1] = im[1], im[0]
			return im
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			writeDescriptorAt(t, h.descriptor, nexusbench.Defaults(), previousDeployment, previousProvisioning,
				nexusbench.SeedResult{Images: tt.mutate(nexusbench.Dataset())})
			code, stdout, stderr := h.run("provision", "--accept-eula")
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			desc, err := nexusbench.LoadDescriptor(h.descriptor)
			if err != nil {
				t.Fatal(err)
			}
			if len(desc.Dataset) != 0 {
				t.Errorf("provision kept a data set that is not the one of the code: %+v", desc.Dataset)
			}
			var said bool
			for _, l := range strings.Split(stdout, "\n") {
				said = said || strings.Contains(l, "the descriptor now lists no data set")
			}
			if !said {
				t.Errorf("provision dropped the data set without the line `the descriptor now lists no data set`:\n%s", stdout)
			}
			requireClean(t, stdout, stderr)
		})
	}
}
