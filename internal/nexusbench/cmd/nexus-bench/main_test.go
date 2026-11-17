package main

// Tests of the nexus-bench command (DEVDESK-504, step 8), written before the
// command. They pin down the contract of
//
//	func run(ctx context.Context, args []string, getenv func(string) string,
//		stdout, stderr io.Writer, d deps) int
//
// and of the `deps` struct, which carries every constructor and every library
// call that reaches a cluster, so that no test needs kubectl, a cluster or the
// network. The exact shape of `deps` these tests build is:
//
//	type deps struct {
//		newRunner  func(kubeContext string) (nexusbench.Runner, error)
//		newProber  func() nexusbench.Prober
//		clock      nexusbench.Clock
//		newStore   func(path string) (nexusbench.SecretStore, error)
//		newInitial func(r nexusbench.Runner, namespace string) nexusbench.InitialPassword
//		preflight  func(ctx context.Context, r nexusbench.Runner, p nexusbench.Params, op nexusbench.Op) (nexusbench.PreflightResult, error)
//		deploy     func(ctx context.Context, r nexusbench.Runner, pr nexusbench.Prober, clk nexusbench.Clock, p nexusbench.Params, out io.Writer) (nexusbench.DeployResult, error)
//		provision  func(ctx context.Context, baseURL string, ip nexusbench.InitialPassword, store nexusbench.SecretStore, p nexusbench.Params, out io.Writer) (nexusbench.ProvisionResult, error)
//		destroy    func(ctx context.Context, r nexusbench.Runner, clk nexusbench.Clock, store nexusbench.SecretStore, p nexusbench.Params, out io.Writer) error
//	}
//
// The descriptor is written and read with the real nexusbench.WriteDescriptor
// and LoadDescriptor, in a t.TempDir(), through NEXUS_BENCH_DESCRIPTOR or
// --descriptor. No test leaves the descriptor path empty: that would resolve to
// the descriptor.json versioned in the repository.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// Markers planted where a secret or a raw error could leak; no output of the
// command may carry any of them.
const (
	secretMarker = "hunter2-SECRET"
	storeMarker  = "S3CRET-ADMIN-PW"
)

// progressMarker is what the fake library calls write to the `out` they are
// given: it proves the command hands its stdout to them.
const progressMarker = "FAKE-PROGRESS"

const nonDefaultWarning = "descriptor.json now describes a non-default bench; do not commit it"

// ---- fakes ----------------------------------------------------------------

// fakeRunner records every kubectl call and answers the pod list; it fails the
// test on any write verb or any stdin, so a read-only command cannot cheat.
type fakeRunner struct {
	t    *testing.T
	mu   sync.Mutex
	args [][]string
	pods string
}

var _ nexusbench.Runner = (*fakeRunner)(nil)

var writeVerbs = map[string]bool{
	"apply": true, "create": true, "delete": true, "patch": true, "replace": true,
	"label": true, "annotate": true, "scale": true, "edit": true, "exec": true,
	"cp": true, "drain": true, "cordon": true, "uncordon": true, "taint": true,
	"rollout": true, "set": true, "run": true, "expose": true, "port-forward": true,
}

func (r *fakeRunner) Run(_ context.Context, stdin []byte, args ...string) ([]byte, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.args = append(r.args, append([]string(nil), args...))
	r.t.Helper()
	if len(stdin) > 0 {
		r.t.Errorf("the runner was given stdin (%d bytes) by a command that must only read: kubectl %v", len(stdin), args)
	}
	for _, a := range args {
		if writeVerbs[a] {
			r.t.Errorf("the runner was asked to write (%q): kubectl %v", a, args)
		}
	}
	for _, a := range args {
		if a == "pods" || a == "pod" || strings.HasPrefix(a, "pods/") || strings.HasPrefix(a, "pod/") {
			return []byte(r.pods), nil, nil
		}
	}
	return nil, nil, nil
}

func (r *fakeRunner) calls() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.args...)
}

type fakeProber struct {
	mu   sync.Mutex
	urls []string
	err  error
}

var _ nexusbench.Prober = (*fakeProber)(nil)

func (p *fakeProber) Ready(_ context.Context, baseURL string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.urls = append(p.urls, baseURL)
	return p.err
}

type fakeClock struct{}

var _ nexusbench.Clock = (*fakeClock)(nil)

func (*fakeClock) Now() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }
func (*fakeClock) Sleep(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}

// fakeStore holds a secret the command must never print.
type fakeStore struct{}

var _ nexusbench.SecretStore = (*fakeStore)(nil)

func (s *fakeStore) Load() (nexusbench.Secrets, bool, error) {
	return nexusbench.Secrets{AdminPassword: storeMarker, PullPassword: storeMarker}, true, nil
}
func (s *fakeStore) Save(nexusbench.Secrets) error { return nil }
func (s *fakeStore) Remove() error                 { return nil }

type fakeInitial struct{ id int }

var _ nexusbench.InitialPassword = (*fakeInitial)(nil)

func (*fakeInitial) Read(context.Context) (string, bool, error) { return "", false, nil }

// fakeForwarder and fakeCopier are handed to the (fake) Seed untouched: only
// Seed forwards ports and copies images, so a call to either by the command
// itself fails the test.
type fakeForwarder struct{ t *testing.T }

var _ nexusbench.PortForwarder = (*fakeForwarder)(nil)

func (f *fakeForwarder) Forward(context.Context, string, string, int) (string, func() error, error) {
	f.t.Error("the command forwarded a port itself: only Seed does")
	return "", nil, errors.New("unexpected Forward")
}

type fakeCopier struct{ t *testing.T }

var _ nexusbench.ImageCopier = (*fakeCopier)(nil)

func (f *fakeCopier) Copy(context.Context, nexusbench.ImageRef, nexusbench.ImageRef, nexusbench.CopyOptions) error {
	f.t.Error("the command copied an image itself: only Seed does")
	return errors.New("unexpected Copy")
}

func (f *fakeCopier) ManifestDigest(context.Context, nexusbench.ImageRef, *nexusbench.Credentials) (string, error) {
	f.t.Error("the command read a manifest itself: only Seed does")
	return "", errors.New("unexpected ManifestDigest")
}

// ---- harness --------------------------------------------------------------

// harness wires fake deps that log every call in order and record what they
// were given. The knobs (the fields after "programmable") are set by a test
// before h.run.
type harness struct {
	t          *testing.T
	dir        string
	descriptor string // the descriptor path of the default environment
	env        map[string]string
	ctx        context.Context

	runner  *fakeRunner
	prober  *fakeProber
	clock   *fakeClock
	store   *fakeStore
	initial *fakeInitial
	forward *fakeForwarder
	copier  *fakeCopier

	mu     sync.Mutex
	events []string

	// recorded
	newRunnerContexts   []string
	newStorePaths       []string
	newInitialRunner    nexusbench.Runner
	newInitialNamespace string
	preflightOps        []nexusbench.Op
	preflightParams     []nexusbench.Params
	preflightRunner     nexusbench.Runner
	deployParams        nexusbench.Params
	deployRunner        nexusbench.Runner
	deployProber        nexusbench.Prober
	deployClock         nexusbench.Clock
	provisionParams     nexusbench.Params
	provisionURL        string
	provisionInitial    nexusbench.InitialPassword
	provisionStore      nexusbench.SecretStore
	destroyParams       nexusbench.Params
	destroyRunner       nexusbench.Runner
	destroyClock        nexusbench.Clock
	destroyStore        nexusbench.SecretStore
	// descriptorSeenAtProvision is whether the descriptor file existed when the
	// fake Provision ran: it must not have been written yet.
	descriptorSeenAtProvision bool

	// NX-02a (DEVDESK-544), recorded
	newForwarderContexts []string
	ensureRunner         nexusbench.Runner
	ensureClock          nexusbench.Clock
	ensureParams         nexusbench.Params
	seedDeps             nexusbench.SeedDeps
	seedURL              string
	seedParams           nexusbench.Params
	seedHadDeadline      bool
	seedRemaining        time.Duration // time left on the context Seed was given
	// descriptorSeenAtSeed is whether the descriptor file existed when the fake
	// Seed ran: it must not have been written yet.
	descriptorSeenAtSeed bool
	readUpstreamRunner   nexusbench.Runner
	readImagesURLs       []string

	// programmable
	newRunnerErr error
	newStoreErr  error
	preflightRes nexusbench.PreflightResult
	preflightErr error
	deployRes    nexusbench.DeployResult
	deployErr    error
	provisionRes nexusbench.ProvisionResult
	provisionErr error
	destroyErr   error
	onDeploy     func() // runs inside the fake Deploy, before it answers

	// NX-02a (DEVDESK-544), programmable
	newForwarderErr error
	newCopierErr    error
	ensureErr       error
	seedRes         nexusbench.SeedResult // default: the whole data set, as Seed returns it
	seedErr         error
	onSeed          func() // runs inside the fake Seed, before it answers
	blockSeed       bool   // the fake Seed waits for its context to end
	upstreamState   nexusbench.UpstreamState
	upstreamErr     error
	// listings answers readImages by repository; a repository that is not in
	// the map answers what the data set says it holds, so a seeded bench reads
	// as matching.
	listings       map[string][]listedImage
	readImagesErr  error
	respectContext bool // the fakes return ctx.Err() when ctx is done, like the library
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	h := &harness{
		t:          t,
		dir:        dir,
		descriptor: filepath.Join(dir, "bench", "descriptor.json"),
		ctx:        context.Background(),
		runner: &fakeRunner{t: t, pods: `{"items":[{"metadata":{"name":"nexus-0"},` +
			`"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":0}]}}]}`},
		prober:  &fakeProber{},
		clock:   &fakeClock{},
		store:   &fakeStore{},
		initial: &fakeInitial{id: 1},
		forward: &fakeForwarder{t: t},
		copier:  &fakeCopier{t: t},

		seedRes:       nexusbench.SeedResult{Images: nexusbench.Dataset()},
		upstreamState: nexusbench.UpstreamReady,

		preflightRes: nexusbench.PreflightResult{NamespaceExists: true, NamespaceOurs: true, NodeIP: "192.168.1.31"},
		deployRes:    nexusbench.DeployResult{ReachableHosts: []string{"127.0.0.1"}, NodeIP: "192.168.1.31"},
		provisionRes: nexusbench.ProvisionResult{NexusVersion: "3.98.1-01", Edition: "COMMUNITY", EULAAccepted: true},
	}
	h.env = map[string]string{
		"NEXUS_BENCH_DESCRIPTOR": h.descriptor,
		"XDG_STATE_HOME":         filepath.Join(dir, "state"),
		"HOME":                   filepath.Join(dir, "home"),
		"USERPROFILE":            filepath.Join(dir, "home"),
		"NEXUS_BENCH_TIMEOUT":    "30s",
	}
	return h
}

func (h *harness) log(event string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func (h *harness) eventList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.events...)
}

// has reports whether an event with this exact name was logged.
func (h *harness) has(event string) bool {
	for _, e := range h.eventList() {
		if e == event {
			return true
		}
	}
	return false
}

func (h *harness) index(event string) int {
	for i, e := range h.eventList() {
		if e == event {
			return i
		}
	}
	return -1
}

func opName(op nexusbench.Op) string {
	switch op {
	case nexusbench.OpUp:
		return "OpUp"
	case nexusbench.OpProvision:
		return "OpProvision"
	case nexusbench.OpDown:
		return "OpDown"
	case nexusbench.OpStatus:
		return "OpStatus"
	case nexusbench.OpSeed:
		return "OpSeed"
	}
	return fmt.Sprintf("Op(%d)", int(op))
}

func (h *harness) getenv(k string) string { return h.env[k] }

func (h *harness) deps() deps {
	return deps{
		newRunner: func(kubeContext string) (nexusbench.Runner, error) {
			h.log("newRunner")
			h.newRunnerContexts = append(h.newRunnerContexts, kubeContext)
			if h.newRunnerErr != nil {
				return nil, h.newRunnerErr
			}
			return h.runner, nil
		},
		newProber: func() nexusbench.Prober {
			h.log("newProber")
			return h.prober
		},
		clock: h.clock,
		newStore: func(path string) (nexusbench.SecretStore, error) {
			h.log("newStore")
			h.newStorePaths = append(h.newStorePaths, path)
			if h.newStoreErr != nil {
				return nil, h.newStoreErr
			}
			return h.store, nil
		},
		newInitial: func(r nexusbench.Runner, namespace string) nexusbench.InitialPassword {
			h.log("newInitial")
			h.newInitialRunner, h.newInitialNamespace = r, namespace
			return h.initial
		},
		preflight: func(ctx context.Context, r nexusbench.Runner, p nexusbench.Params, op nexusbench.Op) (nexusbench.PreflightResult, error) {
			h.log("preflight:" + opName(op))
			h.preflightOps = append(h.preflightOps, op)
			h.preflightParams = append(h.preflightParams, p)
			h.preflightRunner = r
			if h.respectContext && ctx.Err() != nil {
				return nexusbench.PreflightResult{}, ctx.Err()
			}
			res := h.preflightRes
			if op == nexusbench.OpSeed {
				// The real RunPreflight(OpSeed) does not read the node: no NodeIP.
				res.NodeIP = ""
			}
			return res, h.preflightErr
		},
		deploy: func(ctx context.Context, r nexusbench.Runner, pr nexusbench.Prober, clk nexusbench.Clock, p nexusbench.Params, out io.Writer) (nexusbench.DeployResult, error) {
			h.log("deploy")
			h.deployParams, h.deployRunner, h.deployProber, h.deployClock = p, r, pr, clk
			_, _ = fmt.Fprintln(out, progressMarker+" deploy")
			if h.onDeploy != nil {
				h.onDeploy()
			}
			if h.respectContext && ctx.Err() != nil {
				return nexusbench.DeployResult{}, fmt.Errorf("wait for Nexus: %w", ctx.Err())
			}
			return h.deployRes, h.deployErr
		},
		provision: func(ctx context.Context, baseURL string, ip nexusbench.InitialPassword, store nexusbench.SecretStore, p nexusbench.Params, out io.Writer) (nexusbench.ProvisionResult, error) {
			h.log("provision")
			h.provisionURL, h.provisionInitial, h.provisionStore, h.provisionParams = baseURL, ip, store, p
			if _, err := os.Stat(h.descriptorPathOf(p)); err == nil {
				h.descriptorSeenAtProvision = true
			}
			_, _ = fmt.Fprintln(out, progressMarker+" provision")
			if h.respectContext && ctx.Err() != nil {
				return nexusbench.ProvisionResult{}, fmt.Errorf("provision: %w", ctx.Err())
			}
			return h.provisionRes, h.provisionErr
		},
		destroy: func(ctx context.Context, r nexusbench.Runner, clk nexusbench.Clock, store nexusbench.SecretStore, p nexusbench.Params, out io.Writer) error {
			h.log("destroy")
			h.destroyRunner, h.destroyClock, h.destroyStore, h.destroyParams = r, clk, store, p
			_, _ = fmt.Fprintln(out, progressMarker+" destroy")
			if h.respectContext && ctx.Err() != nil {
				return fmt.Errorf("delete: %w", ctx.Err())
			}
			return h.destroyErr
		},

		newForwarder: func(kubeContext string) (nexusbench.PortForwarder, error) {
			h.log("newForwarder")
			h.newForwarderContexts = append(h.newForwarderContexts, kubeContext)
			if h.newForwarderErr != nil {
				return nil, h.newForwarderErr
			}
			return h.forward, nil
		},
		newCopier: func() (nexusbench.ImageCopier, error) {
			h.log("newCopier")
			if h.newCopierErr != nil {
				return nil, h.newCopierErr
			}
			return h.copier, nil
		},
		ensureUpstream: func(ctx context.Context, r nexusbench.Runner, clk nexusbench.Clock, p nexusbench.Params, out io.Writer) error {
			h.log("ensureUpstream")
			h.ensureRunner, h.ensureClock, h.ensureParams = r, clk, p
			_, _ = fmt.Fprintln(out, progressMarker+" ensureUpstream")
			if h.respectContext && ctx.Err() != nil {
				return ctx.Err()
			}
			return h.ensureErr
		},
		seed: func(ctx context.Context, d nexusbench.SeedDeps, baseURL string, p nexusbench.Params, out io.Writer) (nexusbench.SeedResult, error) {
			h.log("seed")
			h.seedDeps, h.seedURL, h.seedParams = d, baseURL, p
			if deadline, ok := ctx.Deadline(); ok {
				h.seedHadDeadline, h.seedRemaining = true, time.Until(deadline)
			}
			if _, err := os.Stat(h.descriptorPathOf(p)); err == nil {
				h.descriptorSeenAtSeed = true
			}
			_, _ = fmt.Fprintln(out, "seed: "+progressMarker)
			if h.onSeed != nil {
				h.onSeed()
			}
			if h.blockSeed {
				<-ctx.Done()
				return nexusbench.SeedResult{}, fmt.Errorf("seed: %w", ctx.Err())
			}
			if h.respectContext && ctx.Err() != nil {
				return nexusbench.SeedResult{}, fmt.Errorf("seed: %w", ctx.Err())
			}
			return h.seedRes, h.seedErr
		},
		readUpstream: func(_ context.Context, r nexusbench.Runner, _ nexusbench.Params) (nexusbench.UpstreamState, error) {
			h.log("readUpstream")
			h.readUpstreamRunner = r
			return h.upstreamState, h.upstreamErr
		},
		readImages: func(_ context.Context, baseURL, repository string) ([]listedImage, error) {
			h.log("readImages:" + repository)
			h.readImagesURLs = append(h.readImagesURLs, baseURL)
			if h.readImagesErr != nil {
				return nil, h.readImagesErr
			}
			if l, ok := h.listings[repository]; ok {
				return l, nil
			}
			return datasetListing(nexusbench.Dataset(), repository), nil
		},
	}
}

// descriptorPathOf is where the command was told to write the descriptor.
func (h *harness) descriptorPathOf(p nexusbench.Params) string {
	if p.DescriptorPath != "" {
		return p.DescriptorPath
	}
	return h.descriptor
}

// run executes the command with the harness environment and returns what a
// shell would see.
func (h *harness) run(args ...string) (code int, stdout, stderr string) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	code = run(h.ctx, args, h.getenv, &out, &errOut, h.deps())
	return code, out.String(), errOut.String()
}

func (h *harness) descriptorExists() bool {
	_, err := os.Stat(h.descriptor)
	return err == nil
}

// writeDescriptor puts a descriptor of p at the default path.
// The descriptor is a seeded bench's (schema 2: the data set is listed), as `up`
// leaves it once step 10 has made it seed.
func (h *harness) writeDescriptor(p nexusbench.Params, d nexusbench.DeployResult, r nexusbench.ProvisionResult) []byte {
	h.t.Helper()
	return writeDescriptorAt(h.t, h.descriptor, p, d, r, nexusbench.SeedResult{Images: nexusbench.Dataset()})
}

func writeDescriptorAt(t *testing.T, path string, p nexusbench.Params, d nexusbench.DeployResult, r nexusbench.ProvisionResult, s nexusbench.SeedResult) []byte {
	t.Helper()
	if err := nexusbench.WriteDescriptor(path, nexusbench.BuildDescriptor(p, d, r, s)); err != nil {
		t.Fatalf("WriteDescriptor(%s): %v", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// wantDescriptorBytes is the file the library writes for these inputs.
func wantDescriptorBytes(t *testing.T, p nexusbench.Params, d nexusbench.DeployResult, r nexusbench.ProvisionResult) []byte {
	t.Helper()
	// `up` seeds the bench (D-J): it describes the whole data set.
	return wantDescriptorBytesWith(t, p, d, r, nexusbench.SeedResult{Images: nexusbench.Dataset()})
}

// wantDescriptorBytesWith is the file the library writes for these inputs and
// this seed result.
func wantDescriptorBytesWith(t *testing.T, p nexusbench.Params, d nexusbench.DeployResult, r nexusbench.ProvisionResult, s nexusbench.SeedResult) []byte {
	t.Helper()
	return writeDescriptorAt(t, filepath.Join(t.TempDir(), "want.json"), p, d, r, s)
}

// datasetListing is what repository holds on a bench seeded with images: the
// hosted images in docker-hosted, the pulled-through ones in docker-proxy.
func datasetListing(images []nexusbench.DatasetImage, repository string) []listedImage {
	var out []listedImage
	for _, img := range images {
		hosted := img.Location == nexusbench.LocationHosted
		switch {
		case repository == "docker-hosted" && hosted,
			repository == "docker-proxy" && !hosted && img.PullThrough:
			out = append(out, listedImage{Name: img.Name, Tag: img.Tag, Digest: img.Digest})
		}
	}
	return out
}

// requireNoCalls fails when any dependency was used.
func (h *harness) requireNoCalls() {
	h.t.Helper()
	if ev := h.eventList(); len(ev) != 0 {
		h.t.Errorf("no dependency should have been used, got %v", ev)
	}
	if calls := h.runner.calls(); len(calls) != 0 {
		h.t.Errorf("the runner should not have been used, got %v", calls)
	}
}

// requireClean fails when an output carries a secret, a stack trace or the raw
// dump of a Go value (%+v, %#v).
func requireClean(t *testing.T, outputs ...string) {
	t.Helper()
	banned := []string{
		secretMarker, storeMarker, "adminPassword", "pullPassword",
		"goroutine ", "runtime/", "panic:", ".go:", "&{", "{Kind:", "Err:", "&nexusbench.", "nexusbench.BenchError", "%!",
	}
	for _, o := range outputs {
		for _, b := range banned {
			if strings.Contains(o, b) {
				t.Errorf("output carries %q:\n%s", b, o)
			}
		}
	}
}

// benchError is a BenchError whose underlying error carries a secret, as the
// text of a failed kubectl or REST call can.
func benchError() *nexusbench.BenchError {
	return &nexusbench.BenchError{
		Kind:  nexusbench.ProvisionFailed,
		Cause: "Nexus refused the admin credentials",
		Check: "kubectl --context datomir -n nexus-bench logs deploy/nexus",
		Err:   errors.New("exit status 1: Authorization: Basic " + secretMarker),
	}
}

// ---- help and usage ---------------------------------------------------------

var subcommands = []string{"up", "provision", "down", "seed", "status", "render"}

func TestHelpPrintsTheSubcommandsOnStdout(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		t.Run(arg, func(t *testing.T) {
			h := newHarness(t)
			code, stdout, stderr := h.run(arg)
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			for _, sub := range subcommands {
				if !strings.Contains(stdout, sub) {
					t.Errorf("stdout does not mention %q:\n%s", sub, stdout)
				}
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			h.requireNoCalls()
		})
	}
}

// go run prints "exit status 2" and exits 1 whatever the program returned, so
// the help has to say where code 2 can be seen.
func TestHelpSaysGoRunDoesNotKeepExitCodeTwo(t *testing.T) {
	h := newHarness(t)
	_, stdout, _ := h.run("-h")
	if !strings.Contains(stdout, "go run") || !strings.Contains(stdout, "compiled") {
		t.Errorf("the help does not say that go run hides exit code 2:\n%s", stdout)
	}
}

func TestSubcommandHelpListsItsFlags(t *testing.T) {
	tests := []struct {
		args  []string
		flags []string
	}{
		{[]string{"up", "-h"}, []string{"namespace", "context", "http-nodeport", "accept-eula", "rest-host", "timeout"}},
		{[]string{"up", "--help"}, []string{"namespace", "rest-host"}},
		{[]string{"provision", "-h"}, []string{"namespace", "rest-host", "accept-eula"}},
		{[]string{"down", "-h"}, []string{"namespace", "context", "delete-timeout"}},
		{[]string{"status", "-h"}, []string{"namespace", "context"}},
		{[]string{"render", "-h"}, []string{"namespace", "image-tag"}},
		{[]string{"up", "-h"}, []string{"upstream-image", "upstream-image-tag"}},
		{[]string{"provision", "-h"}, []string{"upstream-image", "upstream-image-tag"}},
		{[]string{"down", "-h"}, []string{"upstream-image", "upstream-image-tag"}},
		{[]string{"status", "-h"}, []string{"upstream-image", "upstream-image-tag"}},
		{[]string{"render", "-h"}, []string{"upstream-image", "upstream-image-tag"}},
		// seed (DEVDESK-544): the common flags and the REST host (the admin
		// credentials go there), but no EULA: it provisions nothing.
		{[]string{"seed", "-h"}, []string{"namespace", "context", "http-nodeport", "rest-host", "timeout", "upstream-image", "upstream-image-tag", "upstream-url"}},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			h := newHarness(t)
			code, stdout, _ := h.run(tt.args...)
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			for _, f := range tt.flags {
				if !strings.Contains(stdout, f) {
					t.Errorf("the help of %v does not list %q:\n%s", tt.args, f, stdout)
				}
			}
			h.requireNoCalls()
		})
	}
}

func TestUsageErrorsExitWithTwo(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// wantHelp: the help (every subcommand name) is printed on stderr.
		wantHelp bool
	}{
		{"no subcommand", nil, true},
		{"unknown subcommand", []string{"bogus"}, true},
		{"a flag first", []string{"--namespace", "x"}, true},
		{"unknown flag", []string{"up", "--bogus"}, false},
		{"extra argument", []string{"up", "extra"}, false},
		{"extra argument on render", []string{"render", "extra"}, false},
		{"port is not a number", []string{"up", "--http-nodeport", "abc"}, false},
		{"duration is not a duration", []string{"up", "--timeout", "soon"}, false},
		{"eula is not a boolean", []string{"up", "--accept-eula=maybe"}, false},
		{"anonymous is not a boolean", []string{"provision", "--anonymous=perhaps"}, false},
		{"flag without its value", []string{"down", "--namespace"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			code, stdout, stderr := h.run(tt.args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Error("stderr is empty, want a message")
			}
			if tt.wantHelp {
				for _, sub := range subcommands {
					if !strings.Contains(stderr, sub) {
						t.Errorf("the help on stderr does not mention %q:\n%s", sub, stderr)
					}
				}
			}
			requireClean(t, stderr)
			h.requireNoCalls()
			if h.descriptorExists() {
				t.Error("a descriptor was written")
			}
		})
	}
}

// ---- render ----------------------------------------------------------------

func TestRenderPrintsTheManifestsAndBuildsNothing(t *testing.T) {
	want, err := nexusbench.Render(nexusbench.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("render used a dependency it must not touch (empty deps): %v", r)
			}
		}()
		code := run(context.Background(), []string{"render"}, func(string) string { return "" }, &out, &errOut, deps{})
		if code != 0 {
			t.Errorf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
		}
	}()
	if out.String() != string(want) {
		t.Errorf("stdout is not Render(Defaults()):\n got %d bytes, want %d bytes", out.Len(), len(want))
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want empty", errOut.String())
	}
}

func TestRenderHonoursEnvironmentAndFlags(t *testing.T) {
	envOnly := nexusbench.Defaults()
	envOnly.Namespace = "nb-env"
	both := nexusbench.Defaults()
	both.Namespace = "nb-flag"
	both.ImageTag = "3.99.0"
	tests := []struct {
		name string
		env  map[string]string
		args []string
		want nexusbench.Params
	}{
		{"environment", map[string]string{"NEXUS_BENCH_NAMESPACE": "nb-env"}, []string{"render"}, envOnly},
		{"flag over environment", map[string]string{"NEXUS_BENCH_NAMESPACE": "nb-env", "NEXUS_BENCH_IMAGE_TAG": "3.99.0"}, []string{"render", "--namespace", "nb-flag"}, both},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := nexusbench.Render(tt.want)
			if err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			code := run(context.Background(), tt.args, func(k string) string { return tt.env[k] }, &out, &errOut, deps{})
			if code != 0 {
				t.Fatalf("exit code = %d (stderr: %s)", code, errOut.String())
			}
			if out.String() != string(want) {
				t.Errorf("stdout is not Render(%+v)", tt.want.Namespace)
			}
		})
	}
}

// ---- parameters: flag > environment > default --------------------------------

// normalized clears what the command may legitimately fill in on its own: the
// secrets path (derived from the context and the namespace) and an empty host
// list.
func normalized(p nexusbench.Params) nexusbench.Params {
	p.SecretsPath = ""
	if len(p.Hosts) == 0 {
		p.Hosts = nil
	}
	return p
}

func TestParametersFlagOverEnvironmentOverDefault(t *testing.T) {
	defaults := func(h *harness) nexusbench.Params {
		p := nexusbench.Defaults()
		p.DescriptorPath = h.descriptor
		p.Timeout = 30 * time.Second // the harness environment sets NEXUS_BENCH_TIMEOUT
		return p
	}
	tests := []struct {
		name string
		env  map[string]string
		args []string
		want func(p *nexusbench.Params)
	}{
		{name: "defaults", want: func(*nexusbench.Params) {}},
		{
			name: "every variable",
			env: map[string]string{
				"NEXUS_BENCH_CONTEXT":            "ctx-env",
				"NEXUS_BENCH_NAMESPACE":          "nb-env",
				"NEXUS_BENCH_HTTP_NODEPORT":      "31001",
				"NEXUS_BENCH_DOCKER_NODEPORT":    "31002",
				"NEXUS_BENCH_IMAGE_TAG":          "3.99.0",
				"NEXUS_BENCH_UPSTREAM_IMAGE":     "registry.example.com/team/registry",
				"NEXUS_BENCH_UPSTREAM_IMAGE_TAG": "3.0.1",
				"NEXUS_BENCH_TIMEOUT":            "20m",
				"NEXUS_BENCH_DELETE_TIMEOUT":     "7m",
				"NEXUS_BENCH_ANONYMOUS":          "false",
				"NEXUS_BENCH_ACCEPT_EULA":        "yes",
				"NEXUS_BENCH_HOSTS":              "10.0.0.1,10.0.0.2",
			},
			want: func(p *nexusbench.Params) {
				p.Context, p.Namespace = "ctx-env", "nb-env"
				p.UpstreamURL = "http://bench-upstream.nb-env.svc.cluster.local:5000"
				p.HTTPNodePort, p.DockerNodePort = 31001, 31002
				p.ImageTag = "3.99.0"
				p.UpstreamImage, p.UpstreamImageTag = "registry.example.com/team/registry", "3.0.1"
				p.Timeout, p.DeleteTimeout = 20*time.Minute, 7*time.Minute
				p.Anonymous, p.AcceptEULA = false, true
				p.Hosts = []string{"10.0.0.1", "10.0.0.2"}
			},
		},
		{
			name: "flag beats variable",
			env:  map[string]string{"NEXUS_BENCH_NAMESPACE": "nb-env", "NEXUS_BENCH_CONTEXT": "ctx-env", "NEXUS_BENCH_IMAGE_TAG": "3.99.0"},
			args: []string{"--namespace", "nb-flag", "--context=ctx-flag"},
			want: func(p *nexusbench.Params) {
				p.Namespace, p.Context = "nb-flag", "ctx-flag"
				p.UpstreamURL = "http://bench-upstream.nb-flag.svc.cluster.local:5000"
				p.ImageTag = "3.99.0" // the variable stays where no flag overrides it
			},
		},
		{
			name: "an explicit upstream URL wins over the namespace",
			args: []string{"--namespace", "nb-flag", "--upstream-url", "http://up.example.com:5000"},
			want: func(p *nexusbench.Params) {
				p.Namespace = "nb-flag"
				p.UpstreamURL = "http://up.example.com:5000"
			},
		},
		{name: "context", args: []string{"--context", "c1"}, want: func(p *nexusbench.Params) { p.Context = "c1" }},
		{name: "namespace", args: []string{"--namespace", "n1"}, want: func(p *nexusbench.Params) {
			p.Namespace = "n1"
			p.UpstreamURL = "http://bench-upstream.n1.svc.cluster.local:5000"
		}},
		{name: "http nodeport", args: []string{"--http-nodeport", "31010"}, want: func(p *nexusbench.Params) { p.HTTPNodePort = 31010 }},
		{name: "docker nodeport", args: []string{"--docker-nodeport", "31011"}, want: func(p *nexusbench.Params) { p.DockerNodePort = 31011 }},
		{name: "cpu request", args: []string{"--cpu-request", "1"}, want: func(p *nexusbench.Params) { p.CPURequest = "1" }},
		{name: "cpu limit", args: []string{"--cpu-limit", "3"}, want: func(p *nexusbench.Params) { p.CPULimit = "3" }},
		{name: "memory request", args: []string{"--memory-request", "2Gi"}, want: func(p *nexusbench.Params) { p.MemoryRequest = "2Gi" }},
		{name: "memory limit", args: []string{"--memory-limit", "6Gi"}, want: func(p *nexusbench.Params) { p.MemoryLimit = "6Gi" }},
		{name: "storage size", args: []string{"--storage-size", "8Gi"}, want: func(p *nexusbench.Params) { p.StorageSize = "8Gi" }},
		{name: "jvm options", args: []string{"--jvm-opts", "-Xms1g -Xmx2g"}, want: func(p *nexusbench.Params) { p.JVMOpts = "-Xms1g -Xmx2g" }},
		{name: "image", args: []string{"--image", "registry.example.com/sonatype/nexus3"}, want: func(p *nexusbench.Params) { p.Image = "registry.example.com/sonatype/nexus3" }},
		{name: "image tag", args: []string{"--image-tag", "3.97.0"}, want: func(p *nexusbench.Params) { p.ImageTag = "3.97.0" }},
		{name: "upstream image", args: []string{"--upstream-image", "registry.example.com/team/registry"}, want: func(p *nexusbench.Params) {
			p.UpstreamImage = "registry.example.com/team/registry"
		}},
		{name: "upstream image tag", args: []string{"--upstream-image-tag", "3.0.1"}, want: func(p *nexusbench.Params) { p.UpstreamImageTag = "3.0.1" }},
		{
			name: "upstream image flag beats the variable",
			env:  map[string]string{"NEXUS_BENCH_UPSTREAM_IMAGE": "registry.example.com/env/registry", "NEXUS_BENCH_UPSTREAM_IMAGE_TAG": "3.0.1"},
			args: []string{"--upstream-image=registry.example.com/flag/registry", "--upstream-image-tag", "3.0.2"},
			want: func(p *nexusbench.Params) {
				p.UpstreamImage, p.UpstreamImageTag = "registry.example.com/flag/registry", "3.0.2"
			},
		},
		{
			name: "upstream image variable stays where no flag overrides it",
			env:  map[string]string{"NEXUS_BENCH_UPSTREAM_IMAGE": "registry.example.com/env/registry", "NEXUS_BENCH_UPSTREAM_IMAGE_TAG": "3.0.1"},
			args: []string{"--upstream-image-tag", "3.0.2"},
			want: func(p *nexusbench.Params) {
				p.UpstreamImage, p.UpstreamImageTag = "registry.example.com/env/registry", "3.0.2"
			},
		},
		{name: "anonymous off", args: []string{"--anonymous=false"}, want: func(p *nexusbench.Params) { p.Anonymous = false }},
		{
			name: "anonymous flag beats the variable",
			env:  map[string]string{"NEXUS_BENCH_ANONYMOUS": "false"},
			args: []string{"--anonymous"},
			want: func(p *nexusbench.Params) { p.Anonymous = true },
		},
		{name: "hosts", args: []string{"--hosts", "10.0.0.5,10.0.0.6"}, want: func(p *nexusbench.Params) { p.Hosts = []string{"10.0.0.5", "10.0.0.6"} }},
		{name: "timeout", args: []string{"--timeout", "15m"}, want: func(p *nexusbench.Params) { p.Timeout = 15 * time.Minute }},
		{name: "delete timeout", args: []string{"--delete-timeout", "5m"}, want: func(p *nexusbench.Params) { p.DeleteTimeout = 5 * time.Minute }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			for k, v := range tt.env {
				h.env[k] = v
			}
			want := defaults(h)
			tt.want(&want)
			if _, ok := tt.env["NEXUS_BENCH_TIMEOUT"]; ok {
				want.Timeout = 20 * time.Minute
			}
			args := append([]string{"provision"}, tt.args...)
			code, stdout, stderr := h.run(args...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if got := normalized(h.provisionParams); !reflect.DeepEqual(got, normalized(want)) {
				t.Errorf("params given to Provision:\n got %+v\nwant %+v", got, normalized(want))
			}
			// Every step of one run sees the same parameters.
			for _, p := range h.preflightParams {
				if !reflect.DeepEqual(normalized(p), normalized(want)) {
					t.Errorf("params given to the preflight:\n got %+v\nwant %+v", normalized(p), normalized(want))
				}
			}
			if len(h.newRunnerContexts) != 1 || h.newRunnerContexts[0] != want.Context {
				t.Errorf("newRunner contexts = %v, want [%s]", h.newRunnerContexts, want.Context)
			}
		})
	}
}

func TestSecretsAndDescriptorPathsFollowTheSameOrder(t *testing.T) {
	t.Run("descriptor flag beats the variable", func(t *testing.T) {
		h := newHarness(t)
		other := filepath.Join(h.dir, "other", "descriptor.json")
		code, _, stderr := h.run("provision", "--descriptor", other)
		if code != 0 {
			t.Fatalf("exit code = %d (%s)", code, stderr)
		}
		if _, err := nexusbench.LoadDescriptor(other); err != nil {
			t.Errorf("the descriptor was not written to the flag path: %v", err)
		}
		if h.descriptorExists() {
			t.Error("the descriptor was written to the variable path although a flag gave another")
		}
	})
	t.Run("descriptor variable", func(t *testing.T) {
		h := newHarness(t)
		code, _, stderr := h.run("provision")
		if code != 0 {
			t.Fatalf("exit code = %d (%s)", code, stderr)
		}
		if _, err := nexusbench.LoadDescriptor(h.descriptor); err != nil {
			t.Errorf("the descriptor was not written to NEXUS_BENCH_DESCRIPTOR: %v", err)
		}
	})
	t.Run("secrets default is derived from the context and the namespace", func(t *testing.T) {
		h := newHarness(t)
		want, err := nexusbench.DefaultSecretsPath("c1", "n1", h.getenv)
		if err != nil {
			t.Fatal(err)
		}
		code, _, stderr := h.run("provision", "--context", "c1", "--namespace", "n1")
		if code != 0 {
			t.Fatalf("exit code = %d (%s)", code, stderr)
		}
		if !reflect.DeepEqual(h.newStorePaths, []string{want}) {
			t.Errorf("newStore paths = %v, want [%s]", h.newStorePaths, want)
		}
	})
	t.Run("secrets variable", func(t *testing.T) {
		h := newHarness(t)
		path := filepath.Join(h.dir, "vault", "secrets.json")
		h.env["NEXUS_BENCH_SECRETS"] = path
		if code, _, stderr := h.run("provision"); code != 0 {
			t.Fatalf("exit code = %d (%s)", code, stderr)
		}
		if !reflect.DeepEqual(h.newStorePaths, []string{path}) {
			t.Errorf("newStore paths = %v, want [%s]", h.newStorePaths, path)
		}
	})
	t.Run("secrets flag beats the variable", func(t *testing.T) {
		h := newHarness(t)
		h.env["NEXUS_BENCH_SECRETS"] = filepath.Join(h.dir, "vault", "secrets.json")
		flagPath := filepath.Join(h.dir, "flag", "secrets.json")
		if code, _, stderr := h.run("down", "--secrets", flagPath); code != 0 {
			t.Fatalf("exit code = %d (%s)", code, stderr)
		}
		if !reflect.DeepEqual(h.newStorePaths, []string{flagPath}) {
			t.Errorf("newStore paths = %v, want [%s]", h.newStorePaths, flagPath)
		}
	})
	t.Run("no place for the secrets", func(t *testing.T) {
		h := newHarness(t)
		delete(h.env, "XDG_STATE_HOME")
		delete(h.env, "HOME")
		delete(h.env, "USERPROFILE")
		code, stdout, stderr := h.run("up", "--accept-eula")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "NEXUS_BENCH_SECRETS") {
			t.Errorf("stderr does not tell how to set the secrets path:\n%s", stderr)
		}
		if h.has("deploy") || h.has("provision") {
			t.Errorf("the cluster was touched without a place for the secrets: %v", h.eventList())
		}
		requireClean(t, stdout, stderr)
	})
}

func TestAcceptEULAIsTheExactValueOnly(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
		want bool
	}{
		{"nothing", "", nil, false},
		{"yes", "yes", nil, true},
		{"true is not yes", "true", nil, false},
		{"YES is not yes", "YES", nil, false},
		{"1 is not yes", "1", nil, false},
		{"flag", "", []string{"--accept-eula"}, true},
		{"flag true", "", []string{"--accept-eula=true"}, true},
		{"flag false", "", []string{"--accept-eula=false"}, false},
		{"flag false beats yes", "yes", []string{"--accept-eula=false"}, false},
		{"flag beats true", "true", []string{"--accept-eula"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			if tt.env != "" {
				h.env["NEXUS_BENCH_ACCEPT_EULA"] = tt.env
			}
			code, stdout, stderr := h.run(append([]string{"provision"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d\n%s\n%s", code, stdout, stderr)
			}
			if h.provisionParams.AcceptEULA != tt.want {
				t.Errorf("AcceptEULA = %v, want %v", h.provisionParams.AcceptEULA, tt.want)
			}
		})
	}
}

// ---- invalid parameters -----------------------------------------------------

func TestInvalidParametersStopBeforeAnyCall(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		args     []string
		wantText string
	}{
		{"reserved namespace from the variable", map[string]string{"NEXUS_BENCH_NAMESPACE": "default"}, nil, "NEXUS_BENCH_NAMESPACE"},
		{"reserved namespace from a flag", nil, []string{"--namespace", "kube-system"}, "NEXUS_BENCH_NAMESPACE"},
		{"bad context", nil, []string{"--context", "a b"}, "NEXUS_BENCH_CONTEXT"},
		{"NodePort out of range", nil, []string{"--http-nodeport", "80"}, "NEXUS_BENCH_HTTP_NODEPORT"},
		{"the two NodePorts are equal", nil, []string{"--http-nodeport", "31000", "--docker-nodeport", "31000"}, "NodePort"},
		{"a variable that is not a number", map[string]string{"NEXUS_BENCH_HTTP_NODEPORT": "abc"}, nil, "NEXUS_BENCH_HTTP_NODEPORT"},
		{"request above limit", nil, []string{"--memory-request", "8Gi"}, "NEXUS_BENCH_MEMORY"},
		{"upstream image in the zot registry from a flag", nil, []string{"--upstream-image", "tvp-zot.incus:5000/x"}, "NEXUS_BENCH_UPSTREAM_IMAGE"},
		{"upstream image in the zot registry from the variable", map[string]string{"NEXUS_BENCH_UPSTREAM_IMAGE": "tvp-zot.incus:5000/x"}, nil, "NEXUS_BENCH_UPSTREAM_IMAGE"},
		{"upstream image tag that is not a tag", nil, []string{"--upstream-image-tag", "bad tag"}, "NEXUS_BENCH_UPSTREAM_IMAGE_TAG"},
	}
	for _, sub := range []string{"up", "provision", "down", "seed", "status", "render"} {
		for _, tt := range tests {
			t.Run(sub+" "+tt.name, func(t *testing.T) {
				h := newHarness(t)
				for k, v := range tt.env {
					h.env[k] = v
				}
				code, stdout, stderr := h.run(append([]string{sub}, tt.args...)...)
				if code != 1 {
					t.Errorf("exit code = %d, want 1\nstderr: %s", code, stderr)
				}
				if !strings.Contains(stderr, tt.wantText) {
					t.Errorf("stderr does not name %q:\n%s", tt.wantText, stderr)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want empty", stdout)
				}
				requireClean(t, stderr)
				h.requireNoCalls()
				if h.descriptorExists() {
					t.Error("a descriptor was written")
				}
			})
		}
	}
}

// ---- up ----------------------------------------------------------------------

// upArgs are the arguments of a default bench whose EULA the operator accepts.
var upArgs = []string{"--accept-eula"}

// subArgs is a subcommand followed by --accept-eula where the subcommand
// provisions (down and status have no use for the EULA).
func subArgs(sub string) []string {
	if sub == "up" || sub == "provision" {
		return []string{sub, "--accept-eula"}
	}
	return []string{sub}
}

func TestUpRunsDeployThenProvisionThenWritesTheDescriptor(t *testing.T) {
	h := newHarness(t)
	h.deployRes = nexusbench.DeployResult{ReachableHosts: []string{"127.0.0.1", "192.168.1.31"}, NodeIP: "192.168.1.31"}
	code, stdout, stderr := h.run(append([]string{"up"}, upArgs...)...)
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	// Order: the runner and the store exist before the cluster is touched;
	// Deploy runs its own preflight(OpUp), so a preflight of the command is
	// allowed but only before Deploy, and only for OpUp.
	deploy, provision := h.index("deploy"), h.index("provision")
	if deploy < 0 || provision < 0 || deploy > provision {
		t.Fatalf("want deploy then provision, got %v", h.eventList())
	}
	for _, e := range []string{"newRunner", "newStore"} {
		if i := h.index(e); i < 0 || i > deploy {
			t.Errorf("%s must happen before Deploy, events: %v", e, h.eventList())
		}
	}
	for _, op := range h.preflightOps {
		if op != nexusbench.OpUp {
			t.Errorf("up ran a preflight for %s, want OpUp only", opName(op))
		}
	}
	if i := h.index("preflight:OpUp"); i > deploy {
		t.Errorf("the preflight must come before Deploy, events: %v", h.eventList())
	}
	if h.has("destroy") {
		t.Errorf("up destroyed something: %v", h.eventList())
	}

	// What each step was given.
	if h.deployRunner != nexusbench.Runner(h.runner) || h.deployProber != nexusbench.Prober(h.prober) || h.deployClock != nexusbench.Clock(h.clock) {
		t.Error("Deploy was not given the runner, the prober and the clock of the deps")
	}
	if h.newInitialRunner != nexusbench.Runner(h.runner) || h.newInitialNamespace != "nexus-bench" {
		t.Errorf("newInitial got (%v, %q), want the runner and the namespace", h.newInitialRunner, h.newInitialNamespace)
	}
	if h.provisionInitial != nexusbench.InitialPassword(h.initial) || h.provisionStore != nexusbench.SecretStore(h.store) {
		t.Error("Provision was not given the InitialPassword of newInitial and the store of newStore")
	}
	if want := nexusbench.RESTBaseURL("127.0.0.1", h.provisionParams); h.provisionURL != want || want != "http://127.0.0.1:30481" {
		t.Errorf("Provision base URL = %q, want %q", h.provisionURL, want)
	}
	if !h.provisionParams.AcceptEULA {
		t.Error("--accept-eula did not reach Provision")
	}
	if h.descriptorSeenAtProvision {
		t.Error("the descriptor existed before Provision ran")
	}

	// The descriptor: what the library writes for these inputs, nothing else.
	p := nexusbench.Defaults()
	got, err := os.ReadFile(h.descriptor)
	if err != nil {
		t.Fatalf("no descriptor written: %v", err)
	}
	if want := wantDescriptorBytes(t, p, h.deployRes, h.provisionRes); !bytes.Equal(got, want) {
		t.Errorf("descriptor bytes differ from BuildDescriptor:\n got %s\nwant %s", got, want)
	}

	// Output: the progress of the steps, the place of the descriptor, no warning.
	if !strings.Contains(stdout, h.descriptor) {
		t.Errorf("stdout does not say where the descriptor is (%s):\n%s", h.descriptor, stdout)
	}
	for _, step := range []string{"deploy", "provision"} {
		if !strings.Contains(stdout, progressMarker+" "+step) {
			t.Errorf("stdout lost the progress of %s:\n%s", step, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty for a default bench", stderr)
	}
	requireClean(t, stdout, stderr)
}

func TestProvisionAloneRunsPreflightProvisionThenDescriptor(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("provision", "--accept-eula")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if h.has("deploy") || h.has("destroy") {
		t.Errorf("provision must not deploy or destroy: %v", h.eventList())
	}
	pre, prov := h.index("preflight:OpProvision"), h.index("provision")
	if pre < 0 || prov < 0 || pre > prov {
		t.Fatalf("want preflight(OpProvision) then provision, got %v", h.eventList())
	}
	if h.preflightRunner != nexusbench.Runner(h.runner) {
		t.Error("the preflight was not given the runner of the deps")
	}
	if h.newInitialRunner != nexusbench.Runner(h.runner) || h.newInitialNamespace != "nexus-bench" {
		t.Errorf("newInitial got (%v, %q)", h.newInitialRunner, h.newInitialNamespace)
	}
	if h.provisionURL != "http://127.0.0.1:30481" {
		t.Errorf("Provision base URL = %q", h.provisionURL)
	}
	if h.descriptorSeenAtProvision {
		t.Error("the descriptor existed before Provision ran")
	}

	// Without a Deploy, the hosts come from the preflight's node address.
	got, err := os.ReadFile(h.descriptor)
	if err != nil {
		t.Fatalf("no descriptor written: %v", err)
	}
	// provision seeds nothing (NX-01 behavior kept): the data set is described as empty.
	want := wantDescriptorBytesWith(t, nexusbench.Defaults(), nexusbench.DeployResult{NodeIP: h.preflightRes.NodeIP}, h.provisionRes, nexusbench.SeedResult{})
	if !bytes.Equal(got, want) {
		t.Errorf("descriptor bytes differ from BuildDescriptor(p, DeployResult{NodeIP: %s}, ...):\n got %s\nwant %s", h.preflightRes.NodeIP, got, want)
	}
	if !strings.Contains(stdout, h.descriptor) {
		t.Errorf("stdout does not say where the descriptor is:\n%s", stdout)
	}
	if !strings.Contains(stdout, progressMarker+" provision") {
		t.Errorf("stdout lost the progress of Provision:\n%s", stdout)
	}
	requireClean(t, stdout, stderr)
}

// A failure at any step stops the run, and the descriptor is written only
// after a successful Provision: a partial ProvisionResult is never described.
func TestDescriptorIsNotWrittenUnlessProvisionSucceeded(t *testing.T) {
	be := benchError()
	steps := []struct {
		name  string
		sub   string
		setup func(h *harness)
	}{
		{"up: provision fails", "up", func(h *harness) {
			h.provisionErr = be
			// Provision returns what it had gathered together with the error.
			h.provisionRes = nexusbench.ProvisionResult{NexusVersion: "3.98.1-01", Edition: "COMMUNITY"}
		}},
		{"provision: provision fails", "provision", func(h *harness) {
			h.provisionErr = be
			h.provisionRes = nexusbench.ProvisionResult{NexusVersion: "3.98.1-01", Edition: "COMMUNITY"}
		}},
		{"up: deploy fails", "up", func(h *harness) { h.deployErr = be }},
		{"provision: preflight fails", "provision", func(h *harness) { h.preflightErr = be }},
	}
	for _, st := range steps {
		for _, existing := range []bool{false, true} {
			name := st.name + " / no descriptor yet"
			if existing {
				name = st.name + " / a descriptor exists"
			}
			t.Run(name, func(t *testing.T) {
				h := newHarness(t)
				st.setup(h)
				var before []byte
				if existing {
					other := nexusbench.Defaults()
					other.ImageTag = "3.97.0"
					before = h.writeDescriptor(other, nexusbench.DeployResult{}, nexusbench.ProvisionResult{NexusVersion: "3.97.0-01"})
				}
				code, stdout, stderr := h.run(st.sub, "--accept-eula")
				if code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
				if got := strings.TrimSpace(stderr); got != be.Error() {
					t.Errorf("stderr = %q, want exactly the BenchError text %q", got, be.Error())
				}
				requireClean(t, stdout, stderr)
				if existing {
					after, err := os.ReadFile(h.descriptor)
					if err != nil || !bytes.Equal(before, after) {
						t.Errorf("the descriptor of the previous run was changed (err=%v)", err)
					}
				} else if h.descriptorExists() {
					t.Error("a descriptor was written although the run failed")
				}
			})
		}
	}
}

func TestUpStopsAtTheFirstFailure(t *testing.T) {
	be := benchError()
	tests := []struct {
		name      string
		sub       string
		setup     func(h *harness)
		notCalled []string
	}{
		{"kubectl missing", "up", func(h *harness) {
			h.newRunnerErr = &nexusbench.BenchError{Kind: nexusbench.KubectlMissing, Cause: "kubectl was not found on PATH", Check: "run `mise install`", Err: errors.New("exec: " + secretMarker)}
		}, []string{"deploy", "provision", "preflight:OpUp"}},
		{"secrets path refused", "up", func(h *harness) {
			h.newStoreErr = &nexusbench.BenchError{Cause: "the secrets path is in a repository and not ignored", Check: "set NEXUS_BENCH_SECRETS", Err: errors.New(secretMarker)}
		}, []string{"deploy", "provision"}},
		{"deploy fails", "up", func(h *harness) { h.deployErr = be }, []string{"provision"}},
		{"provision: kubectl missing", "provision", func(h *harness) {
			h.newRunnerErr = &nexusbench.BenchError{Kind: nexusbench.KubectlMissing, Cause: "kubectl was not found on PATH", Check: "run `mise install`"}
		}, []string{"provision", "preflight:OpProvision"}},
		{"provision: preflight fails", "provision", func(h *harness) { h.preflightErr = be }, []string{"provision"}},
		{"down: kubectl missing", "down", func(h *harness) {
			h.newRunnerErr = &nexusbench.BenchError{Kind: nexusbench.KubectlMissing, Cause: "kubectl was not found on PATH", Check: "run `mise install`"}
		}, []string{"destroy"}},
		{"down: secrets path refused", "down", func(h *harness) {
			h.newStoreErr = &nexusbench.BenchError{Cause: "the secrets path is refused", Check: "set NEXUS_BENCH_SECRETS"}
		}, []string{"destroy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			tt.setup(h)
			code, stdout, stderr := h.run(subArgs(tt.sub)...)
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
			if h.descriptorExists() {
				t.Error("a descriptor was written")
			}
		})
	}
}

func firstError(h *harness) error {
	for _, err := range []error{h.newRunnerErr, h.newStoreErr, h.newForwarderErr, h.newCopierErr, h.preflightErr, h.deployErr, h.ensureErr, h.provisionErr, h.seedErr, h.destroyErr} {
		if err != nil {
			return err
		}
	}
	return nil
}

// ---- error rendering ------------------------------------------------------

func TestErrorsAreShownAsTheirMessageOnly(t *testing.T) {
	be := benchError()
	tests := []struct {
		name string
		err  error
		want string // the exact text of stderr, trimmed
	}{
		{"a BenchError", be, be.Error()},
		{"a BenchError wrapped with a secret in the wrapper", fmt.Errorf("provision step 3 (token=%s): %w", secretMarker, be), be.Error()},
		{"a joined error holding a BenchError", errors.Join(errors.New("first"), be), be.Error()},
	}
	for _, sub := range []string{"up", "provision", "down"} {
		for _, tt := range tests {
			t.Run(sub+" "+tt.name, func(t *testing.T) {
				h := newHarness(t)
				switch sub {
				case "down":
					h.destroyErr = tt.err
				default:
					h.provisionErr = tt.err
				}
				code, stdout, stderr := h.run(subArgs(sub)...)
				if code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
				if got := strings.TrimSpace(stderr); got != tt.want {
					t.Errorf("stderr = %q, want %q", got, tt.want)
				}
				if n := strings.Count(strings.TrimSpace(stderr), "\n"); n != 0 {
					t.Errorf("stderr has %d extra lines, want one message:\n%s", n, stderr)
				}
				requireClean(t, stdout, stderr)
			})
		}
	}
}

func TestAnErrorThatIsNotABenchErrorStillPrintsItsMessage(t *testing.T) {
	h := newHarness(t)
	h.provisionErr = errors.New("the descriptor could not be placed")
	code, stdout, stderr := h.run("provision", "--accept-eula")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "the descriptor could not be placed") {
		t.Errorf("stderr lost the message:\n%s", stderr)
	}
	requireClean(t, stdout, stderr)
	if h.descriptorExists() {
		t.Error("a descriptor was written")
	}
}

func TestACancelledContextEndsCleanly(t *testing.T) {
	t.Run("cancelled before the run", func(t *testing.T) {
		for _, sub := range []string{"up", "provision", "down", "status"} {
			t.Run(sub, func(t *testing.T) {
				h := newHarness(t)
				h.respectContext = true
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				h.ctx = ctx
				code, stdout, stderr := h.run(subArgs(sub)...)
				if code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
				if !strings.Contains(strings.ToLower(stderr), "cancel") {
					t.Errorf("stderr does not say the run was cancelled:\n%s", stderr)
				}
				if n := strings.Count(strings.TrimSpace(stderr), "\n"); n != 0 {
					t.Errorf("stderr should be one line, got:\n%s", stderr)
				}
				requireClean(t, stdout, stderr)
				if h.descriptorExists() {
					t.Error("a descriptor was written")
				}
				if h.has("provision") && !h.has("deploy") && sub == "up" {
					t.Error("Provision ran without Deploy")
				}
			})
		}
	})
	t.Run("cancelled during Deploy", func(t *testing.T) {
		h := newHarness(t)
		h.respectContext = true
		ctx, cancel := context.WithCancel(context.Background())
		h.ctx = ctx
		h.onDeploy = cancel
		code, stdout, stderr := h.run("up", "--accept-eula")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(strings.ToLower(stderr), "cancel") {
			t.Errorf("stderr does not say the run was cancelled:\n%s", stderr)
		}
		requireClean(t, stdout, stderr)
		if h.has("provision") {
			t.Error("Provision ran after the context was cancelled")
		}
		if h.descriptorExists() {
			t.Error("a descriptor was written")
		}
	})
}

// ---- credentials go to the loopback unless told otherwise ---------------------

func TestCredentialsGoToTheLoopbackByDefault(t *testing.T) {
	tests := []struct {
		name         string
		sub          string
		args         []string
		deployRes    *nexusbench.DeployResult
		wantURL      string
		wantClearMsg bool
	}{
		{name: "up", sub: "up", wantURL: "http://127.0.0.1:30481"},
		{name: "provision", sub: "provision", wantURL: "http://127.0.0.1:30481"},
		{name: "a custom NodePort", sub: "up", args: []string{"--http-nodeport", "31001"}, wantURL: "http://127.0.0.1:31001"},
		{
			name: "the node answered but the loopback was not reached by Deploy", sub: "up",
			deployRes: &nexusbench.DeployResult{ReachableHosts: []string{"192.168.1.31"}, NodeIP: "192.168.1.31"},
			wantURL:   "http://127.0.0.1:30481",
		},
		{name: "explicit loopback", sub: "up", args: []string{"--rest-host", "127.0.0.1"}, wantURL: "http://127.0.0.1:30481"},
		{name: "explicit node IP", sub: "up", args: []string{"--rest-host", "192.168.1.31"}, wantURL: "http://192.168.1.31:30481", wantClearMsg: true},
		{name: "explicit node IP, provision", sub: "provision", args: []string{"--rest-host", "192.168.1.31"}, wantURL: "http://192.168.1.31:30481", wantClearMsg: true},
		{name: "explicit host name", sub: "up", args: []string{"--rest-host=nexus.lan"}, wantURL: "http://nexus.lan:30481", wantClearMsg: true},
		{name: "explicit IPv6 address", sub: "up", args: []string{"--rest-host", "fd00::1"}, wantURL: "http://[fd00::1]:30481", wantClearMsg: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			if tt.deployRes != nil {
				h.deployRes = *tt.deployRes
			}
			code, stdout, stderr := h.run(append([]string{tt.sub, "--accept-eula"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if h.provisionURL != tt.wantURL {
				t.Errorf("Provision base URL = %q, want %q", h.provisionURL, tt.wantURL)
			}
			hasWarning := strings.Contains(stderr, "credentials travel in clear text")
			if hasWarning != tt.wantClearMsg {
				t.Errorf("clear-text warning on stderr = %v, want %v:\n%s", hasWarning, tt.wantClearMsg, stderr)
			}
			if tt.wantClearMsg && strings.Contains(stdout, "credentials travel in clear text") {
				t.Error("the clear-text warning belongs on stderr")
			}
			requireClean(t, stdout, stderr)
		})
	}
}

func TestARestHostThatIsNotAHostIsRefusedBeforeTheCluster(t *testing.T) {
	for _, host := range []string{"a/b", "evil.example.com/x?y=", "user@host", "bad host", "http://127.0.0.1", "127.0.0.1:30481"} {
		for _, sub := range []string{"up", "provision"} {
			t.Run(sub+" "+host, func(t *testing.T) {
				h := newHarness(t)
				code, stdout, stderr := h.run(sub, "--accept-eula", "--rest-host="+host)
				if code != 1 && code != 2 {
					t.Errorf("exit code = %d, want 1 or 2", code)
				}
				if strings.TrimSpace(stderr) == "" {
					t.Error("stderr is empty, want a message")
				}
				requireClean(t, stdout, stderr)
				if h.has("deploy") || h.has("provision") || h.provisionURL != "" {
					t.Errorf("the cluster was touched with a refused --rest-host: %v", h.eventList())
				}
				if h.descriptorExists() {
					t.Error("a descriptor was written")
				}
			})
		}
	}
}

// ---- the non-default warning --------------------------------------------------

func TestNonDefaultBenchWarnsNotToCommitTheDescriptor(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"default bench, tuned timeouts and paths", []string{"--accept-eula", "--timeout", "5m", "--delete-timeout", "2m"}, false},
		{"default bench, explicit defaults", []string{"--namespace", "nexus-bench", "--context", "datomir", "--http-nodeport", "30481"}, false},
		{"namespace", []string{"--namespace", "nexus-bench-2"}, true},
		{"context", []string{"--context", "other"}, true},
		{"http nodeport", []string{"--http-nodeport", "31001"}, true},
		{"docker nodeport", []string{"--docker-nodeport", "31002"}, true},
		{"image tag", []string{"--image-tag", "3.97.0"}, true},
		{"image", []string{"--image", "registry.example.com/sonatype/nexus3"}, true},
		{"upstream image", []string{"--upstream-image", "registry.example.com/team/registry"}, true},
		{"upstream image tag", []string{"--upstream-image-tag", "3.0.1"}, true},
		{"upstream image, explicit defaults", []string{"--upstream-image", "docker.io/library/registry", "--upstream-image-tag", "3.0.0"}, false},
		{"anonymous off", []string{"--anonymous=false"}, true},
		{"memory limit", []string{"--memory-limit", "6Gi"}, true},
	}
	for _, sub := range []string{"up", "provision"} {
		for _, tt := range tests {
			t.Run(sub+" "+tt.name, func(t *testing.T) {
				h := newHarness(t)
				code, stdout, stderr := h.run(append([]string{sub}, tt.args...)...)
				if code != 0 {
					t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
				}
				if got := strings.Contains(stderr, nonDefaultWarning); got != tt.want {
					t.Errorf("warning on stderr = %v, want %v:\n%s", got, tt.want, stderr)
				}
				if strings.Contains(stdout, nonDefaultWarning) {
					t.Error("the warning belongs on stderr")
				}
				// The descriptor is written all the same: the warning is the whole mitigation.
				if !h.descriptorExists() {
					t.Error("the descriptor was not written")
				}
				requireClean(t, stdout, stderr)
			})
		}
	}
	for _, sub := range []string{"down", "status", "render"} {
		t.Run(sub+" has nothing to say about the descriptor", func(t *testing.T) {
			h := newHarness(t)
			_, stdout, stderr := h.run(sub, "--namespace", "nexus-bench-2")
			if strings.Contains(stdout+stderr, nonDefaultWarning) {
				t.Errorf("%s warned about the descriptor, which it does not write:\n%s%s", sub, stdout, stderr)
			}
		})
	}
}

// ---- down -----------------------------------------------------------------

func TestDownDestroysTheBenchAndKeepsTheDescriptor(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		h := newHarness(t)
		before := h.writeDescriptor(nexusbench.Defaults(), h.deployRes, h.provisionRes)
		code, stdout, stderr := h.run("down", "--namespace", "nb-down", "--delete-timeout", "9m")
		if code != 0 {
			t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if !h.has("destroy") {
			t.Fatalf("Destroy was not called: %v", h.eventList())
		}
		for _, e := range []string{"deploy", "provision"} {
			if h.has(e) {
				t.Errorf("down called %s", e)
			}
		}
		for _, op := range h.preflightOps {
			if op != nexusbench.OpDown {
				t.Errorf("down ran a preflight for %s, want OpDown only", opName(op))
			}
		}
		if h.destroyRunner != nexusbench.Runner(h.runner) || h.destroyClock != nexusbench.Clock(h.clock) || h.destroyStore != nexusbench.SecretStore(h.store) {
			t.Error("Destroy was not given the runner, the clock and the store of the deps")
		}
		if h.destroyParams.Namespace != "nb-down" || h.destroyParams.DeleteTimeout != 9*time.Minute {
			t.Errorf("Destroy params = %+v", h.destroyParams)
		}
		if !strings.Contains(stdout, progressMarker+" destroy") {
			t.Errorf("stdout lost the progress of Destroy:\n%s", stdout)
		}
		after, err := os.ReadFile(h.descriptor)
		if err != nil || !bytes.Equal(before, after) {
			t.Errorf("down must leave the versioned descriptor alone (err=%v)", err)
		}
		requireClean(t, stdout, stderr)
	})
	t.Run("no descriptor is created", func(t *testing.T) {
		h := newHarness(t)
		if code, _, stderr := h.run("down"); code != 0 {
			t.Fatalf("exit code = %d (%s)", code, stderr)
		}
		if h.descriptorExists() {
			t.Error("down created a descriptor")
		}
	})
	t.Run("destroy fails", func(t *testing.T) {
		h := newHarness(t)
		be := &nexusbench.BenchError{Kind: nexusbench.NamespaceForeign, Cause: "namespace nexus-bench is not ours", Check: "kubectl get ns nexus-bench --show-labels", Err: errors.New(secretMarker)}
		h.destroyErr = be
		code, stdout, stderr := h.run("down")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if got := strings.TrimSpace(stderr); got != be.Error() {
			t.Errorf("stderr = %q, want %q", got, be.Error())
		}
		requireClean(t, stdout, stderr)
	})
}

// ---- status ---------------------------------------------------------------

func TestStatusReadsAndComparesWithTheDescriptor(t *testing.T) {
	matching := nexusbench.Defaults()
	oldImage := nexusbench.Defaults()
	oldImage.ImageTag = "3.97.0"
	otherNamespace := nexusbench.Defaults()
	otherNamespace.Namespace = "elsewhere"
	otherNamespace.UpstreamURL = "http://bench-upstream.elsewhere.svc.cluster.local:5000"
	oldUpstreamTag := nexusbench.Defaults()
	oldUpstreamTag.UpstreamImageTag = "3.0.1"
	otherUpstreamImage := nexusbench.Defaults()
	otherUpstreamImage.UpstreamImage = "registry.example/mirror/registry"

	const noPod = `{"items":[]}`
	tests := []struct {
		name       string
		descriptor *nexusbench.Params // nil: no descriptor file
		pods       string
		proberErr  error
		want       []string // lower-cased substrings of stdout
		notWant    []string
	}{
		{
			name: "running, ready, matching", descriptor: &matching,
			want:    []string{"pod: running", "rest: ready", "descriptor: matches"},
			notWant: []string{"rest: not ready", "descriptor: differs"},
		},
		{
			name: "image drift", descriptor: &oldImage,
			want: []string{"descriptor: differs", "image", "3.97.0", "3.98.1"},
		},
		{
			name: "namespace drift", descriptor: &otherNamespace,
			want: []string{"descriptor: differs", "namespace", "elsewhere", "nexus-bench"},
		},
		{
			// NX-02 (D-M): the upstream image is part of what the descriptor
			// describes. The descriptor of a seeded bench lists the data set;
			// the parameters know nothing of it, so it never counts as a difference.
			name: "upstream image tag drift", descriptor: &oldUpstreamTag,
			want: []string{"descriptor: differs", "upstream image", "3.0.1", "3.0.0"},
		},
		{
			name: "upstream image drift", descriptor: &otherUpstreamImage,
			want: []string{"descriptor: differs", "upstream image", "registry.example/mirror/registry", "docker.io/library/registry"},
		},
		{
			name: "no descriptor", descriptor: nil,
			want: []string{"descriptor: not found", "rest: ready"},
		},
		{
			name: "rest not answering", descriptor: &matching, proberErr: errors.New("connection refused to " + secretMarker),
			want:    []string{"rest: not ready", "pod: running"},
			notWant: []string{"rest: ready", "refused"},
		},
		{
			name: "no pod", descriptor: &matching, pods: noPod,
			want: []string{"pod: not found"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			if tt.pods != "" {
				h.runner.pods = tt.pods
			}
			h.prober.err = tt.proberErr
			var descriptorBefore []byte
			if tt.descriptor != nil {
				descriptorBefore = h.writeDescriptor(*tt.descriptor, h.deployRes, h.provisionRes)
			}
			code, stdout, stderr := h.run("status")
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			lower := strings.ToLower(stdout)
			for _, w := range tt.want {
				if !strings.Contains(lower, strings.ToLower(w)) {
					t.Errorf("stdout lacks %q:\n%s", w, stdout)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(lower, strings.ToLower(w)) {
					t.Errorf("stdout must not contain %q:\n%s", w, stdout)
				}
			}
			requireClean(t, stdout, stderr)

			// Read-only: nothing but a preflight and reads. The fake runner has
			// already failed the test on any write verb or stdin.
			for _, e := range h.eventList() {
				switch e {
				case "deploy", "provision", "destroy", "newStore", "newInitial":
					t.Errorf("status called %s, which is not a read: %v", e, h.eventList())
				}
			}
			for _, op := range h.preflightOps {
				if op != nexusbench.OpStatus {
					t.Errorf("status ran a preflight for %s, want OpStatus", opName(op))
				}
			}
			if !h.has("preflight:OpStatus") {
				t.Errorf("status did not run preflight(OpStatus): %v", h.eventList())
			}
			if len(h.runner.calls()) == 0 {
				t.Error("status read nothing from the cluster (no pod read)")
			}
			// The REST check goes to the loopback, with no credentials.
			if len(h.prober.urls) == 0 || h.prober.urls[0] != "http://127.0.0.1:30481" {
				t.Errorf("prober URLs = %v, want the loopback REST origin first", h.prober.urls)
			}
			if tt.descriptor != nil {
				after, err := os.ReadFile(h.descriptor)
				if err != nil || !bytes.Equal(descriptorBefore, after) {
					t.Errorf("status changed the descriptor (err=%v)", err)
				}
			} else if h.descriptorExists() {
				t.Error("status created a descriptor")
			}
		})
	}
}

// A descriptor of NX-01 (schema 1) is not read any more: status says so and what
// to run, and still reports the pod and REST, without touching the file.
func TestStatusReportsASchema1Descriptor(t *testing.T) {
	h := newHarness(t)
	v1 := []byte(`{"schemaVersion": 1, "kubeContext": "datomir", "namespace": "nexus-bench"}` + "\n")
	if err := os.MkdirAll(filepath.Dir(h.descriptor), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.descriptor, v1, 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := h.run("status")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	lower := strings.ToLower(stdout)
	for _, want := range []string{"descriptor: unreadable", "schema", "nexus-bench:up", "pod: running"} {
		if !strings.Contains(lower, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	for _, banned := range []string{"descriptor: matches", "descriptor: differs"} {
		if strings.Contains(lower, banned) {
			t.Errorf("stdout must not contain %q:\n%s", banned, stdout)
		}
	}
	requireClean(t, stdout, stderr)
	if after, err := os.ReadFile(h.descriptor); err != nil || !bytes.Equal(after, v1) {
		t.Errorf("status changed the descriptor (err=%v)", err)
	}
}

func TestStatusStopsWhenThePreflightFails(t *testing.T) {
	h := newHarness(t)
	be := &nexusbench.BenchError{Kind: nexusbench.ClusterUnreachable, Cause: "the cluster does not answer", Check: "kubectl --context datomir get --raw /readyz", Err: errors.New(secretMarker)}
	h.preflightErr = be
	code, stdout, stderr := h.run("status")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if got := strings.TrimSpace(stderr); got != be.Error() {
		t.Errorf("stderr = %q, want %q", got, be.Error())
	}
	requireClean(t, stdout, stderr)
	if calls := h.runner.calls(); len(calls) != 0 {
		t.Errorf("status read the cluster after a failed preflight: %v", calls)
	}
	if len(h.prober.urls) != 0 {
		t.Errorf("status probed REST after a failed preflight: %v", h.prober.urls)
	}
}
