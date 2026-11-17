package nexusbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests of the upstream lifecycle (DEVDESK-505, NX-02a, step 5):
//
//	ReadUpstream(ctx, runner, p) (UpstreamState, error)
//	EnsureUpstream(ctx, runner, clock, p, out) error
//
// against the scripted kubectl of preflight_test.go, wrapped by upstreamKubectl
// below, and the fake clock of clock_test.go. No real kubectl, no real wait.
//
// Neither function runs a preflight: the callers do (Deploy, then seed). The
// kubectl calls the tests assume (arguments after "kubectl", without --context;
// "--request-timeout=<d>" may sit anywhere and is ignored):
//
//	get deployment[s] [bench-upstream | deployment/bench-upstream] -n <ns> -o json
//	                          one object when named, a list when not (it then holds
//	                          the Deployment of Nexus too: the reader picks by name)
//	get pods -n <ns> -l app.kubernetes.io/name=bench-upstream -o json
//	get pvc [bench-upstream-data] -n <ns> [-l <selector>] -o json
//	                          the list holds the claim of Nexus too, listed first
//	scale deployment/bench-upstream --replicas=1 -n <ns>   (or "deployment bench-upstream")
//
// Choices of the tests, to settle with the code:
//   - ReadUpstream: absent Deployment is Missing; zero replicas is Cut (even while
//     an old pod is still terminating); one replica without a ready pod that is
//     not terminating is Starting (a pod that fails to pull is Starting, not an
//     error: diagnosing is the wait's job); a ready pod that is not terminating
//     is Ready. It writes nothing and waits for nothing.
//   - EnsureUpstream reads first. Cut: exactly one write, the scale to 1, then
//     the wait. Starting: no write, the wait. Ready: no write, no wait, nil.
//     Missing: an error, no write (Deploy applies the Deployment before).
//   - The wait reads the pods and the claim of the UPSTREAM only (selector
//     app.kubernetes.io/name=bench-upstream, claim bench-upstream-data), once per
//     round, diagnoses, then sleeps between 1 s and 10 s; it gives up at
//     p.Timeout (within one interval) with a Deadline that cites the pod.
//   - Its messages name NEXUS_BENCH_UPSTREAM_IMAGE and _TAG for a pull failure,
//     and never the variables that size Nexus (the upstream is not sized by
//     them) nor "the Nexus pod".
//   - A custom upstream URL is refused before any call, with a message that
//     names NEXUS_BENCH_UPSTREAM_URL.
//   - A cancelled context is returned as is.

const (
	// upstreamName is declared by render_test.go ("bench-upstream").
	upstreamClaim    = "bench-upstream-data"
	upstreamSelector = "app.kubernetes.io/name=bench-upstream"
	upstreamPodName  = "bench-upstream-6f7d9c8b5-q4w2z"
)

// ---- doubles ----------------------------------------------------------------

// upPod is a pod of the upstream as a round sees it.
type upPod struct {
	fakePod
	terminating bool
}

func (u upPod) object() map[string]any {
	pd := u.fakePod
	if pd.name == "" {
		pd.name = upstreamPodName
	}
	obj := pd.object()
	meta := obj["metadata"].(map[string]any)
	meta["labels"] = map[string]string{"app.kubernetes.io/name": upstreamName}
	if u.terminating {
		meta["deletionTimestamp"] = "2026-11-17T09:00:00Z"
	}
	return obj
}

func ups(pods ...fakePod) []upPod {
	out := make([]upPod, 0, len(pods))
	for _, pd := range pods {
		out = append(out, upPod{fakePod: pd})
	}
	return out
}

// upstreamRound is what the cluster answers in one round.
type upstreamRound struct {
	deployment bool // the Deployment bench-upstream exists
	replicas   int  // its spec.replicas
	pods       []upPod
	claim      string // phase of bench-upstream-data; "" means no such claim
	nexusClaim string // phase of nexus-data; "" means Bound
}

func absentRound() upstreamRound { return upstreamRound{} }

// roundOf is the Deployment with the replicas, a Bound claim and the pods.
func roundOf(replicas int, pods ...fakePod) upstreamRound {
	return upstreamRound{deployment: true, replicas: replicas, pods: ups(pods...), claim: "Bound"}
}

func cutRound() upstreamRound { return roundOf(0) }

func (r upstreamRound) withClaim(phase string) upstreamRound {
	r.claim = phase
	return r
}

func (r upstreamRound) withNexusClaim(phase string) upstreamRound {
	r.nexusClaim = phase
	return r
}

func repeatRound(r upstreamRound, n int) []upstreamRound {
	out := make([]upstreamRound, n)
	for i := range out {
		out[i] = r
	}
	return out
}

type scaleCall struct {
	resource, namespace, replicas string
}

// upstreamKubectl answers the reads and the scale of the upstream. The cluster
// plays before[n] on the n-th round (a round is one sleep) until a scale is
// accepted, then after[n] on the n-th round that follows it; the last scripted
// round stays for ever.
type upstreamKubectl struct {
	*fakeKubectl
	clk    *fakeClock
	before []upstreamRound
	after  []upstreamRound

	// The failures a test scripts; each answers its call and has no effect.
	scaleReply, deploymentReply, podsReply *fakeReply

	scaledAt int // len(sleeps) when the scale was accepted; -1 before
	lines    []string
	writes   []string // every call whose verb writes, normalized
	scales   []scaleCall
}

var _ Runner = (*upstreamKubectl)(nil)

func newUpstreamKubectl(t *testing.T, p Params, clk *fakeClock, before, after []upstreamRound) *upstreamKubectl {
	t.Helper()
	if len(before) == 0 {
		before = []upstreamRound{cutRound()}
	}
	return &upstreamKubectl{fakeKubectl: newFakeKubectl(t, p).ours(), clk: clk, before: before, after: after, scaledAt: -1}
}

func (u *upstreamKubectl) current() upstreamRound {
	if u.scaledAt < 0 {
		return u.before[min(len(u.clk.sleeps), len(u.before)-1)]
	}
	if len(u.after) == 0 {
		u.t.Fatalf("a scale was accepted but no round is scripted after it")
	}
	return u.after[min(len(u.clk.sleeps)-u.scaledAt, len(u.after)-1)]
}

func (u *upstreamKubectl) Run(ctx context.Context, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	norm := normalizeArgs(args)
	joined := strings.Join(norm, " ")
	u.lines = append(u.lines, joined)
	verb := verbOf(args)
	if isWriteVerb(verb) {
		u.writes = append(u.writes, joined)
	}
	switch {
	case verb == "scale":
		return u.scale(norm)
	case isWriteVerb(verb):
		u.t.Errorf("unexpected write call %q: the upstream is only scaled", args)
		return nil, []byte("the fake refuses a write: " + verb), errFakeExit1
	case verb == "get":
		if g, ok := u.parseGet(norm); ok {
			return u.answer(g)
		}
	}
	return u.fakeKubectl.Run(ctx, stdin, args...)
}

type getCall struct {
	kind, name, namespace, selector, output string
}

// kindOfResource maps the spellings of kubectl to one name; "" is unknown.
func kindOfResource(r string) string {
	switch r {
	case "deployment", "deployments", "deploy", "deployment.apps", "deployments.apps":
		return "deployment"
	case "pod", "pods", "po":
		return "pod"
	case "pvc", "persistentvolumeclaim", "persistentvolumeclaims":
		return "pvc"
	}
	return ""
}

// parseGet reads "get <resource>[/<name>] [<name>] [flags]"; ok is false for a
// resource the fake does not script itself (it goes to the preflight fake).
func (u *upstreamKubectl) parseGet(norm []string) (getCall, bool) {
	var g getCall
	var positional []string
	for i := 1; i < len(norm); i++ {
		switch norm[i] {
		case "-n", "--namespace":
			i++
			g.namespace = at(norm, i)
		case "-l", "--selector":
			i++
			g.selector = at(norm, i)
		case "-o", "--output":
			i++
			g.output = at(norm, i)
		default:
			if strings.HasPrefix(norm[i], "-") {
				return g, false
			}
			positional = append(positional, norm[i])
		}
	}
	if len(positional) == 0 {
		return g, false
	}
	resource, name, _ := strings.Cut(positional[0], "/")
	if g.kind = kindOfResource(resource); g.kind == "" {
		return g, false
	}
	g.name = name
	if len(positional) > 1 {
		g.name = positional[1]
	}
	if len(positional) > 2 || (len(positional) == 2 && strings.Contains(positional[0], "/")) {
		u.t.Errorf("unscripted arguments in %q", norm)
	}
	if g.namespace != u.p.Namespace {
		u.t.Errorf("%q reads namespace %q, the run is about %q", norm, g.namespace, u.p.Namespace)
	}
	if g.output != "json" {
		u.t.Errorf("%q must ask for -o json", norm)
	}
	return g, true
}

func notFoundReply(what string) ([]byte, []byte, error) {
	return nil, []byte("Error from server (NotFound): " + what + " not found\n"), errFakeExit1
}

func (u *upstreamKubectl) answer(g getCall) ([]byte, []byte, error) {
	round := u.current()
	switch g.kind {
	case "deployment":
		return u.answerDeployments(g, round)
	case "pod":
		return u.answerPods(g, round)
	}
	return u.answerClaims(g, round)
}

func (u *upstreamKubectl) deploymentObject(name string, replicas int, pods []upPod) map[string]any {
	ready := 0
	for _, pd := range pods {
		if pd.ready && !pd.terminating {
			ready++
		}
	}
	return map[string]any{
		"metadata": map[string]any{"name": name, "namespace": u.p.Namespace, "labels": map[string]string{"app.kubernetes.io/name": name}},
		"spec":     map[string]any{"replicas": replicas},
		"status":   map[string]any{"replicas": len(pods), "readyReplicas": ready, "availableReplicas": ready},
	}
}

func (u *upstreamKubectl) answerDeployments(g getCall, round upstreamRound) ([]byte, []byte, error) {
	if u.deploymentReply != nil {
		return u.deploymentReply.reply()
	}
	if g.name != "" && g.name != upstreamName {
		u.t.Errorf("get deployment %q: the upstream lifecycle is about %q only", g.name, upstreamName)
		return notFoundReply("deployments.apps \"" + g.name + "\"")
	}
	if g.selector != "" && g.selector != upstreamSelector {
		u.t.Errorf("get deployment -l %q: not the upstream", g.selector)
	}
	if g.name == upstreamName {
		if !round.deployment {
			return notFoundReply("deployments.apps \"" + upstreamName + "\"")
		}
		return u.mustJSON(u.deploymentObject(upstreamName, round.replicas, round.pods)), nil, nil
	}
	// Nexus first: a reader that takes the first item reads the wrong one.
	items := []any{u.deploymentObject("nexus", 1, nil)}
	if round.deployment {
		items = append(items, u.deploymentObject(upstreamName, round.replicas, round.pods))
	}
	return u.mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": items}), nil, nil
}

func (u *upstreamKubectl) answerPods(g getCall, round upstreamRound) ([]byte, []byte, error) {
	if u.podsReply != nil {
		return u.podsReply.reply()
	}
	if g.selector != upstreamSelector {
		u.t.Errorf("get pods selects %q, want %q", g.selector, upstreamSelector)
	}
	items := []any{}
	for _, pd := range round.pods {
		items = append(items, pd.object())
	}
	return u.mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": items}), nil, nil
}

func (u *upstreamKubectl) claimObject(name, app, phase string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"name": name, "labels": map[string]string{"app.kubernetes.io/name": app}},
		"status":   map[string]any{"phase": phase},
	}
}

func (u *upstreamKubectl) answerClaims(g getCall, round upstreamRound) ([]byte, []byte, error) {
	nexusPhase := round.nexusClaim
	if nexusPhase == "" {
		nexusPhase = "Bound"
	}
	// Nexus first, against the order of kubectl: the reader must pick by name.
	claims := []map[string]any{u.claimObject(deployPVCName, "nexus", nexusPhase)}
	if round.claim != "" {
		claims = append(claims, u.claimObject(upstreamClaim, upstreamName, round.claim))
	}
	var items []any
	for _, c := range claims {
		name := c["metadata"].(map[string]any)["name"]
		app := c["metadata"].(map[string]any)["labels"].(map[string]string)["app.kubernetes.io/name"]
		if g.name != "" && g.name != name {
			continue
		}
		if g.selector != "" && g.selector != "app.kubernetes.io/name="+app {
			continue
		}
		items = append(items, c)
	}
	if g.name != "" {
		if len(items) == 0 {
			return notFoundReply("persistentvolumeclaims \"" + g.name + "\"")
		}
		return u.mustJSON(items[0]), nil, nil
	}
	if items == nil {
		items = []any{}
	}
	return u.mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": items}), nil, nil
}

// scale reads "scale deployment/bench-upstream --replicas=N -n <ns>".
func (u *upstreamKubectl) scale(norm []string) ([]byte, []byte, error) {
	var call scaleCall
	var positional []string
	for i := 1; i < len(norm); i++ {
		switch a := norm[i]; {
		case a == "-n" || a == "--namespace":
			i++
			call.namespace = at(norm, i)
		case strings.HasPrefix(a, "--replicas="):
			call.replicas = strings.TrimPrefix(a, "--replicas=")
		case strings.HasPrefix(a, "--timeout=") || strings.HasPrefix(a, "--current-replicas="):
		case strings.HasPrefix(a, "-"):
			u.t.Errorf("unscripted argument %q in %q", a, norm)
		default:
			positional = append(positional, a)
		}
	}
	call.resource = strings.Join(positional, "/")
	u.scales = append(u.scales, call)
	if call.resource != "deployment/"+upstreamName && call.resource != "deployments/"+upstreamName && call.resource != "deploy/"+upstreamName {
		u.t.Errorf("scale targets %q, want deployment/%s: the upstream is the only thing to scale", call.resource, upstreamName)
		return nil, []byte("the fake refuses this scale"), errFakeExit1
	}
	if call.namespace != u.p.Namespace {
		u.t.Errorf("scale in namespace %q, the run is about %q", call.namespace, u.p.Namespace)
	}
	if u.scaleReply != nil {
		return u.scaleReply.reply()
	}
	if call.replicas == "" {
		u.t.Errorf("scale without --replicas: %q", norm)
	}
	u.scaledAt = len(u.clk.sleeps)
	return []byte("deployment.apps/" + upstreamName + " scaled\n"), nil, nil
}

// requireNoPreflight checks the call set holds no question of the preflight.
func (u *upstreamKubectl) requireNoPreflight() {
	u.t.Helper()
	for _, line := range u.lines {
		for _, banned := range []string{"auth ", "config ", "get namespace", "get nodes", "get services", "get --raw"} {
			if strings.HasPrefix(line, banned) {
				u.t.Errorf("call %q belongs to the preflight, which the caller runs", line)
			}
		}
	}
}

// upstreamEnv is one run: a cluster, a clock, an output.
type upstreamEnv struct {
	t    *testing.T
	p    Params
	kube *upstreamKubectl
	clk  *fakeClock
	out  bytes.Buffer
}

func newUpstreamEnv(t *testing.T, p Params, before, after []upstreamRound) *upstreamEnv {
	t.Helper()
	clk := newFakeClock(t)
	return &upstreamEnv{t: t, p: p, kube: newUpstreamKubectl(t, p, clk, before, after), clk: clk}
}

func (e *upstreamEnv) ensure(ctx context.Context) error {
	e.t.Helper()
	return EnsureUpstream(ctx, e.kube, e.clk, e.p, &e.out)
}

func (e *upstreamEnv) read(ctx context.Context) (UpstreamState, error) {
	e.t.Helper()
	return ReadUpstream(ctx, e.kube, e.p)
}

func (e *upstreamEnv) requireNoWrite() {
	e.t.Helper()
	if len(e.kube.writes) != 0 {
		e.t.Errorf("wrote to the cluster: %q", e.kube.writes)
	}
}

// requireOneScale checks the one write is the scale to the replicas.
func (e *upstreamEnv) requireOneScale(replicas string) {
	e.t.Helper()
	if len(e.kube.writes) != 1 || len(e.kube.scales) != 1 {
		e.t.Fatalf("writes = %q, want exactly one scale", e.kube.writes)
	}
	if got := e.kube.scales[0]; got.replicas != replicas || got.namespace != e.p.Namespace {
		e.t.Errorf("scale = %+v, want %s replicas in %s", got, replicas, e.p.Namespace)
	}
}

func (e *upstreamEnv) requirePollIntervals() {
	e.t.Helper()
	for i, d := range e.clk.sleeps {
		if d < minPollInterval || d > maxPollInterval {
			e.t.Errorf("sleep #%d lasts %v, want between %v and %v", i+1, d, minPollInterval, maxPollInterval)
		}
	}
}

func (e *upstreamEnv) requireNoSecret(err error) {
	e.t.Helper()
	said := e.out.String()
	if err != nil {
		said += "\n" + err.Error()
	}
	for _, banned := range []string{"password", strings.ToLower(secretMarker), strings.ToLower(fakeSecret)} {
		if strings.Contains(strings.ToLower(said), banned) {
			e.t.Errorf("said %q, which contains %q", said, banned)
		}
	}
}

func upstreamParams() Params {
	p := Defaults()
	p.Timeout = 2 * time.Minute
	return p
}

// ---- the workload -----------------------------------------------------------

func TestUpstreamWorkloadNamesWhatItDiagnoses(t *testing.T) {
	nexus := nexusWorkload()
	if nexus.selector != podSelector {
		t.Errorf("Nexus selector = %q, want %q", nexus.selector, podSelector)
	}
	if !slices.Equal(nexus.imageVars, []string{envImage, envImageTag}) {
		t.Errorf("Nexus imageVars = %q", nexus.imageVars)
	}
	for _, v := range []string{envMemoryRequest, envMemoryLimit} {
		if !slices.Contains(nexus.memoryVars, v) {
			t.Errorf("Nexus memoryVars = %q, lacks %s", nexus.memoryVars, v)
		}
	}

	up := upstreamWorkload()
	if up.selector != upstreamSelector {
		t.Errorf("upstream selector = %q, want %q", up.selector, upstreamSelector)
	}
	if !slices.Equal(up.imageVars, []string{envUpstreamImage, envUpstreamTag}) {
		t.Errorf("upstream imageVars = %q, want %q and %q", up.imageVars, envUpstreamImage, envUpstreamTag)
	}
	if len(up.memoryVars) != 0 {
		t.Errorf("upstream memoryVars = %q: its memory is fixed by the manifest, no variable sizes it", up.memoryVars)
	}
}

// ---- ReadUpstream -----------------------------------------------------------

func TestReadUpstreamStates(t *testing.T) {
	tests := []struct {
		name  string
		round upstreamRound
		want  UpstreamState
	}{
		{"no Deployment", absentRound(), UpstreamMissing},
		{"zero replicas, no pod", cutRound(), UpstreamCut},
		{
			"zero replicas while the old pod is still terminating",
			upstreamRound{deployment: true, replicas: 0, claim: "Bound", pods: []upPod{{fakePod: readyPod(), terminating: true}}},
			UpstreamCut,
		},
		{"one replica, the pod is not created yet", roundOf(1), UpstreamStarting},
		{"one replica, pod not scheduled", roundOf(1, unscheduledPod()), UpstreamStarting},
		{"one replica, container being created", roundOf(1, creatingPod()), UpstreamStarting},
		{"one replica, container running but not ready", roundOf(1, startingPod()), UpstreamStarting},
		{"one replica, image pull failing is starting, not an error", roundOf(1, pullFailedPod("ImagePullBackOff")), UpstreamStarting},
		{
			"one replica, the only ready pod is terminating",
			upstreamRound{deployment: true, replicas: 1, claim: "Bound", pods: []upPod{{fakePod: readyPod(), terminating: true}}},
			UpstreamStarting,
		},
		{
			"one replica, a terminating ready pod and its replacement starting",
			upstreamRound{deployment: true, replicas: 1, claim: "Bound", pods: []upPod{{fakePod: readyPod(), terminating: true}, {fakePod: startingPod()}}},
			UpstreamStarting,
		},
		{"one replica, pod ready", roundOf(1, readyPod()), UpstreamReady},
		{
			"one replica, ready pod next to a terminating one",
			upstreamRound{deployment: true, replicas: 1, claim: "Bound", pods: []upPod{{fakePod: readyPod(), terminating: true}, {fakePod: readyPod(), terminating: false}}},
			UpstreamReady,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{tt.round}, nil)
			got, err := e.read(context.Background())
			if err != nil {
				t.Fatalf("ReadUpstream: %v", err)
			}
			if got != tt.want {
				t.Errorf("state = %d, want %d", got, tt.want)
			}
			e.requireNoWrite()
			e.kube.requireNoPreflight()
			if len(e.clk.sleeps) != 0 {
				t.Errorf("ReadUpstream waited: %v", e.clk.sleeps)
			}
		})
	}
}

func TestReadUpstreamStatesAreDistinctAndOrdered(t *testing.T) {
	order := []UpstreamState{UpstreamMissing, UpstreamCut, UpstreamStarting, UpstreamReady}
	for i, s := range order {
		if int(s) != i {
			t.Errorf("state #%d has the value %d", i, s)
		}
	}
}

func TestReadUpstreamFailures(t *testing.T) {
	stderr := func(text string) *fakeReply {
		return &fakeReply{stderr: text + " token=" + fakeSecret, err: errFakeExit1}
	}
	tests := []struct {
		name  string
		setup func(u *upstreamKubectl)
		kind  ErrKind
	}{
		{
			name: "forbidden to read the Deployment",
			setup: func(u *upstreamKubectl) {
				u.deploymentReply = stderr(`Error from server (Forbidden): deployments.apps "bench-upstream" is forbidden`)
			},
			kind: Forbidden,
		},
		{
			name: "the cluster does not answer",
			setup: func(u *upstreamKubectl) {
				u.deploymentReply = stderr("Unable to connect to the server: dial tcp 192.168.1.31:6443: i/o timeout")
			},
			kind: ClusterUnreachable,
		},
		{
			name:  "forbidden to list the pods",
			setup: func(u *upstreamKubectl) { u.podsReply = stderr(`Error from server (Forbidden): pods is forbidden`) },
			kind:  Forbidden,
		},
		{
			name:  "something that is not JSON",
			setup: func(u *upstreamKubectl) { u.deploymentReply = &fakeReply{stdout: "<html>proxy error</html>"} },
			kind:  Unclassified,
		},
		{
			name: "an unknown failure",
			setup: func(u *upstreamKubectl) {
				u.deploymentReply = stderr("the server has asked for the client to provide credentials")
			},
			kind: Unclassified,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{roundOf(1, readyPod())}, nil)
			tt.setup(e.kube)
			_, err := e.read(context.Background())
			if err == nil {
				t.Fatal("ReadUpstream succeeded, want a BenchError")
			}
			be := requireCleanError(t, err)
			if be.Kind != tt.kind {
				t.Errorf("Kind = %d, want %d; message: %v", be.Kind, tt.kind, err)
			}
			e.requireNoWrite()
		})
	}
}

// ---- EnsureUpstream: the nominal paths --------------------------------------

func TestEnsureUpstreamScalesACutUpstreamOnceThenWaits(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(),
		[]upstreamRound{cutRound()},
		[]upstreamRound{
			roundOf(1),
			roundOf(1, creatingPod()),
			roundOf(1, startingPod()),
			roundOf(1, readyPod()),
		})
	err := e.ensure(context.Background())
	if err != nil {
		t.Fatalf("EnsureUpstream: %v", err)
	}
	e.requireOneScale("1")
	if got := len(e.clk.sleeps); got != 3 {
		t.Errorf("slept %d times, want 3: one per round that was not ready (%v)", got, e.clk.sleeps)
	}
	e.requirePollIntervals()
	e.kube.requireNoPreflight()
	e.requireNoSecret(nil)

	// The write comes after a read: the state decides whether to write.
	scale := slices.IndexFunc(e.kube.lines, func(l string) bool { return strings.HasPrefix(l, "scale ") })
	read := slices.IndexFunc(e.kube.lines, func(l string) bool { return strings.HasPrefix(l, "get ") })
	if read < 0 || scale < read {
		t.Errorf("the scale is not preceded by a read: %q", e.kube.lines)
	}
	// The wait reads the pods once the scale is written.
	if after := e.kube.lines[scale+1:]; !slices.ContainsFunc(after, func(l string) bool { return strings.HasPrefix(l, "get pods") }) {
		t.Errorf("no pod read after the scale: %q", e.kube.lines)
	}
}

func TestEnsureUpstreamScalesAnUpstreamThatReadyOnTheFirstRound(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{cutRound()}, []upstreamRound{roundOf(1, readyPod())})
	if err := e.ensure(context.Background()); err != nil {
		t.Fatalf("EnsureUpstream: %v", err)
	}
	e.requireOneScale("1")
	if len(e.clk.sleeps) != 0 {
		t.Errorf("slept %v although the first round after the scale was ready", e.clk.sleeps)
	}
}

func TestEnsureUpstreamWritesNothingWhenAlreadyReady(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{roundOf(1, readyPod())}, nil)
	if err := e.ensure(context.Background()); err != nil {
		t.Fatalf("EnsureUpstream: %v", err)
	}
	e.requireNoWrite()
	e.kube.requireNoPreflight()
	if len(e.clk.sleeps) != 0 {
		t.Errorf("waited %v for an upstream that was ready", e.clk.sleeps)
	}
}

func TestEnsureUpstreamWaitsWithoutScalingWhileItStarts(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{
		roundOf(1, creatingPod()),
		roundOf(1, startingPod()),
		roundOf(1, readyPod()),
	}, nil)
	if err := e.ensure(context.Background()); err != nil {
		t.Fatalf("EnsureUpstream: %v", err)
	}
	e.requireNoWrite()
	if got := len(e.clk.sleeps); got != 2 {
		t.Errorf("slept %d times, want 2 (%v)", got, e.clk.sleeps)
	}
	e.requirePollIntervals()
}

func TestEnsureUpstreamToleratesATransientPendingReading(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(), nil, []upstreamRound{
		roundOf(1, unschedulablePod("0/1 nodes are available: 1 Insufficient memory.")),
		roundOf(1, unschedulablePod("0/1 nodes are available: 1 Insufficient memory.")),
		roundOf(1, creatingPod()).withClaim("Pending"),
		roundOf(1, startingPod()),
		roundOf(1, readyPod()),
	})
	if err := e.ensure(context.Background()); err != nil {
		t.Fatalf("EnsureUpstream: %v", err)
	}
	e.requireOneScale("1")
}

func TestEnsureUpstreamAcceptsANilWriter(t *testing.T) {
	p := upstreamParams()
	e := newUpstreamEnv(t, p, nil, []upstreamRound{roundOf(1, startingPod()), roundOf(1, readyPod())})
	if err := EnsureUpstream(context.Background(), e.kube, e.clk, p, nil); err != nil {
		t.Fatalf("EnsureUpstream with a nil writer: %v", err)
	}
}

func TestEnsureUpstreamIgnoresTheClaimOfNexus(t *testing.T) {
	// The claim of Nexus is Pending and listed first; the upstream's is Bound. The
	// pod is scheduled and not ready for more than pendingGrace whatever the
	// interval of the wait (it lies between minPollInterval and maxPollInterval):
	// enough rounds to last pendingGrace at the shortest interval, and a timeout
	// that the same rounds cannot reach at the longest one.
	rounds := int(pendingGrace/minPollInterval) + 2
	p := upstreamParams()
	p.Timeout = time.Duration(rounds+1) * maxPollInterval
	pending := roundOf(1, startingPod()).withNexusClaim("Pending")
	before := append(repeatRound(pending, rounds), roundOf(1, readyPod()).withNexusClaim("Pending"))
	e := newUpstreamEnv(t, p, before, nil)
	if err := e.ensure(context.Background()); err != nil {
		t.Fatalf("EnsureUpstream: %v (the claim of Nexus is not the upstream's business)", err)
	}
	e.requireNoWrite()
	if len(e.clk.sleeps) != rounds {
		t.Errorf("slept %d times, want %d: one per round that was not ready", len(e.clk.sleeps), rounds)
	}
	if el := e.clk.elapsed(); el < pendingGrace {
		t.Errorf("waited %v, less than pendingGrace %v: the test would not catch a reader of the wrong claim", el, pendingGrace)
	}
}

// ---- EnsureUpstream: failures -----------------------------------------------

func TestEnsureUpstreamFailsWhenTheDeploymentIsMissing(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{absentRound()}, nil)
	err := e.ensure(context.Background())
	if err == nil {
		t.Fatal("EnsureUpstream succeeded without a Deployment")
	}
	_ = requireCleanError(t, err)
	requireMentions(t, err, []string{upstreamName}, nil)
	e.requireNoWrite()
	if len(e.clk.sleeps) != 0 {
		t.Errorf("waited %v for a Deployment that is not there", e.clk.sleeps)
	}
}

func TestEnsureUpstreamDeadline(t *testing.T) {
	tests := []struct {
		name    string
		forever upstreamRound
		mention []string
	}{
		{"the container never gets ready", roundOf(1, startingPod()), []string{upstreamPodName, envTimeout}},
		{"the container stays in creation", roundOf(1, creatingPod()), []string{upstreamPodName, envTimeout}},
		{"the pod is not even created", roundOf(1), []string{upstreamName, envTimeout}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := upstreamParams()
			e := newUpstreamEnv(t, p, nil, []upstreamRound{tt.forever})
			err := e.ensure(context.Background())
			be := requireDeployKind(t, err, Deadline)
			requireMentions(t, err, tt.mention, []string{"Nexus did not answer"})
			if !strings.Contains(be.Check, "kubectl") {
				t.Errorf("Check %q does not name a kubectl command", be.Check)
			}
			e.requireOneScale("1")
			e.requirePollIntervals()
			if el := e.clk.elapsed(); el < p.Timeout || el >= p.Timeout+maxPollInterval {
				t.Errorf("gave up after %v, want %v within one interval", el, p.Timeout)
			}
			e.requireNoSecret(err)
		})
	}
}

func TestEnsureUpstreamDeadlineHonorsTheTimeout(t *testing.T) {
	p := upstreamParams()
	p.Timeout = 30 * time.Second
	e := newUpstreamEnv(t, p, nil, []upstreamRound{roundOf(1, startingPod())})
	err := e.ensure(context.Background())
	_ = requireDeployKind(t, err, Deadline)
	if el := e.clk.elapsed(); el < p.Timeout || el >= p.Timeout+maxPollInterval {
		t.Errorf("gave up after %v, want %v within one interval", el, p.Timeout)
	}
}

func TestEnsureUpstreamDiagnosesTheUpstream(t *testing.T) {
	custom := func() Params {
		p := upstreamParams()
		p.UpstreamImageTag = "3.0.1"
		return p
	}
	tests := []struct {
		name       string
		params     func() Params
		round      upstreamRound
		kind       ErrKind
		mention    []string
		notMention []string
		minElapsed time.Duration // the reading must have lasted this long
	}{
		{
			name:       "ErrImagePull",
			round:      roundOf(1, pullFailedPod("ErrImagePull")),
			kind:       ImagePull,
			mention:    []string{envUpstreamImage, envUpstreamTag, "docker.io/library/registry:3.0.0", "app.kubernetes.io/name=bench-upstream"},
			notMention: []string{envImage, envImageTag, "sonatype/nexus3", "app.kubernetes.io/name=nexus"},
		},
		{
			name:       "ImagePullBackOff names the reference of the parameters",
			params:     custom,
			round:      roundOf(1, pullFailedPod("ImagePullBackOff")),
			kind:       ImagePull,
			mention:    []string{envUpstreamImage, envUpstreamTag, "docker.io/library/registry:3.0.1"},
			notMention: []string{envImage, envImageTag, "sonatype/nexus3", "registry:3.0.0"},
		},
		{
			name:       "CrashLoopBackOff",
			round:      roundOf(1, fakePod{phase: "Running", scheduled: "True", waiting: "CrashLoopBackOff", lastTerminated: "Error", restarts: 4}),
			kind:       Crash,
			mention:    []string{upstreamName},
			notMention: []string{envMemoryLimit, envMemoryRequest, "the Nexus container"},
		},
		{
			name:       "OOMKilled does not suggest a variable that does not size the upstream",
			round:      roundOf(1, fakePod{phase: "Running", scheduled: "True", waiting: "CrashLoopBackOff", lastTerminated: "OOMKilled", restarts: 2}),
			kind:       Crash,
			mention:    []string{upstreamName, "OOMKilled"},
			notMention: []string{envMemoryLimit, envMemoryRequest, "the Nexus container"},
		},
		{
			name:       "unschedulable for longer than the grace",
			round:      roundOf(1, unschedulablePod("0/1 nodes are available: 1 Insufficient memory.")),
			kind:       PodPending,
			mention:    []string{"cannot be scheduled", "app.kubernetes.io/name=bench-upstream"},
			notMention: []string{envMemoryRequest, envCPURequest, "the Nexus pod", "app.kubernetes.io/name=nexus"},
			minElapsed: pendingGrace,
		},
		{
			name:       "the claim of the upstream stays Pending although the pod is scheduled",
			round:      roundOf(1, creatingPod()).withClaim("Pending"),
			kind:       PodPending,
			mention:    []string{upstreamClaim, "Pending"},
			notMention: []string{deployPVCName, envStorageSize},
			minElapsed: pendingGrace,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := upstreamParams()
			if tt.params != nil {
				p = tt.params()
			}
			e := newUpstreamEnv(t, p, nil, []upstreamRound{tt.round})
			err := e.ensure(context.Background())
			_ = requireDeployKind(t, err, tt.kind)
			requireMentions(t, err, tt.mention, tt.notMention)
			e.requireOneScale("1")
			e.requireNoSecret(err)
			if el := e.clk.elapsed(); el < tt.minElapsed || el >= p.Timeout {
				t.Errorf("failed after %v, want at least %v and before the deadline %v", el, tt.minElapsed, p.Timeout)
			}
			if tt.minElapsed == 0 && len(e.clk.sleeps) != 0 {
				t.Errorf("slept %v: a pull or a crash is reported at the first round that shows it", e.clk.sleeps)
			}
		})
	}
}

func TestEnsureUpstreamFailsWhenTheScaleFails(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		kind   ErrKind
	}{
		{"forbidden", `Error from server (Forbidden): deployments.apps "bench-upstream" is forbidden: cannot patch resource "deployments/scale" token=` + fakeSecret, Forbidden},
		{"the cluster does not answer", "Unable to connect to the server: dial tcp: i/o timeout token=" + fakeSecret, ClusterUnreachable},
		{"an unknown failure", "error: something went wrong token=" + fakeSecret, Unclassified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{cutRound()}, []upstreamRound{roundOf(1, readyPod())})
			e.kube.scaleReply = &fakeReply{stderr: tt.stderr, err: errFakeExit1}
			err := e.ensure(context.Background())
			be := requireDeployKind(t, err, tt.kind)
			requireMentions(t, err, []string{upstreamName}, nil)
			if be.Err == nil {
				t.Error("the BenchError dropped the underlying error")
			}
			if len(e.kube.scales) != 1 {
				t.Errorf("%d scale attempts, want 1: a failed write is not retried", len(e.kube.scales))
			}
			if len(e.clk.sleeps) != 0 {
				t.Errorf("waited %v after a failed scale", e.clk.sleeps)
			}
			e.requireNoSecret(err)
		})
	}
}

func TestEnsureUpstreamFailsWhenAReadFails(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{cutRound()}, []upstreamRound{roundOf(1, readyPod())})
	e.kube.podsReply = &fakeReply{stderr: "Error from server (Forbidden): pods is forbidden token=" + fakeSecret, err: errFakeExit1}
	err := e.ensure(context.Background())
	_ = requireDeployKind(t, err, Forbidden)
	e.requireNoSecret(err)
}

// ---- EnsureUpstream: the guards ---------------------------------------------

func TestEnsureUpstreamRefusesACustomUpstream(t *testing.T) {
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
			p := upstreamParams()
			tt.mutate(&p)
			if p.UpstreamIsBench() {
				t.Fatalf("the case is not a custom upstream: %q in %q", p.UpstreamURL, p.Namespace)
			}
			e := newUpstreamEnv(t, p, []upstreamRound{cutRound()}, []upstreamRound{roundOf(1, readyPod())})
			err := e.ensure(context.Background())
			if err == nil {
				t.Fatal("EnsureUpstream accepted an upstream that is not the bench's")
			}
			var be *BenchError
			if !errors.As(err, &be) || be.Cause == "" || be.Check == "" {
				t.Fatalf("error %T %v is not a complete *BenchError", err, err)
			}
			requireMentions(t, err, []string{envUpstreamURL, "upstream"}, nil)
			if len(e.kube.lines) != 0 {
				t.Errorf("the refusal came after kubectl calls: %q", e.kube.lines)
			}
			e.requireNoWrite()
		})
	}
}

func TestEnsureUpstreamReturnsACancelledContextAsIs(t *testing.T) {
	t.Run("cancelled before the start: nothing is read or written", func(t *testing.T) {
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{cutRound()}, []upstreamRound{roundOf(1, readyPod())})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := e.ensure(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		var be *BenchError
		if errors.As(err, &be) {
			t.Errorf("a cancelled context is a BenchError (%v), want it as is", be)
		}
		e.requireNoWrite()
	})
	t.Run("cancelled during the wait", func(t *testing.T) {
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{cutRound()}, []upstreamRound{roundOf(1, startingPod())})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.clk.onSleep = func(n int) {
			if n == 2 {
				cancel()
			}
		}
		err := e.ensure(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		var be *BenchError
		if errors.As(err, &be) {
			t.Errorf("a cancelled context is a BenchError (%v), want it as is", be)
		}
		e.requireOneScale("1")
		if len(e.clk.sleeps) != 2 {
			t.Errorf("slept %d times after the cancellation, want it to stop at 2", len(e.clk.sleeps))
		}
	})
}

// ---- Deploy next to the upstream --------------------------------------------

// twoClaims answers the claim list of Deploy the way the namespace looks once the
// upstream manifests are applied: the claim of the upstream, which kubectl lists
// first (by name), then the claim of Nexus.
type twoClaims struct {
	*deployKubectl
	upstreamPhase string
}

func (c twoClaims) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, []byte, error) {
	out, errOut, err := c.deployKubectl.Run(ctx, stdin, args...)
	norm := normalizeArgs(args)
	if err != nil || verbOf(args) != "get" || len(norm) < 2 || !isPVCResource(norm[1]) {
		return out, errOut, err
	}
	var list struct {
		Items []any `json:"items"`
	}
	if jerr := json.Unmarshal(out, &list); jerr != nil {
		c.t.Fatalf("decoding the claim list: %v", jerr)
	}
	upstream := map[string]any{
		"metadata": map[string]any{"name": upstreamClaim, "labels": map[string]string{"app.kubernetes.io/name": upstreamName}},
		"status":   map[string]any{"phase": c.upstreamPhase},
	}
	list.Items = append([]any{upstream}, list.Items...)
	return c.mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": list.Items}), errOut, nil
}

func TestDeployReadsTheClaimOfNexusOnly(t *testing.T) {
	// Thirteen rounds of five seconds: the pod is scheduled and not ready for far
	// longer than pendingGrace.
	slow := append(repeatStates(state("Bound", startingPod()), 13), state("Bound", readyPod()))
	t.Run("a Pending claim of the upstream is not Nexus's", func(t *testing.T) {
		e := newDeployEnv(t, deployParams(), hostAnswers(60*time.Second, "127.0.0.1"), slow...)
		res, err := Deploy(context.Background(), twoClaims{e.kube, "Pending"}, e.pr, e.clk, e.p, &e.out)
		if err != nil {
			t.Fatalf("Deploy: %v (the claim %s does not belong to Nexus)", err, upstreamClaim)
		}
		if !slices.Contains(res.ReachableHosts, "127.0.0.1") {
			t.Errorf("ReachableHosts = %q", res.ReachableHosts)
		}
	})
	t.Run("a Pending claim of Nexus is still reported although the upstream's is Bound", func(t *testing.T) {
		stuck := repeatStates(state("Pending", startingPod()), 3)
		e := newDeployEnv(t, deployParams(), nil, stuck...)
		_, err := Deploy(context.Background(), twoClaims{e.kube, "Bound"}, e.pr, e.clk, e.p, &e.out)
		be := requireDeployKind(t, err, PodPending)
		if !strings.Contains(be.Cause, deployPVCName) {
			t.Errorf("Cause %q does not name the claim %s", be.Cause, deployPVCName)
		}
	})
}

func repeatStates(s clusterState, n int) []clusterState {
	out := make([]clusterState, n)
	for i := range out {
		out[i] = s
	}
	return out
}
