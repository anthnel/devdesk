package nexusbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests of Deploy (DEVDESK-504, step 4). Deploy is called as
//
//	Deploy(ctx, runner, prober, clock, p, out) (DeployResult, error)
//
// against the scripted kubectl of preflight_test.go, wrapped by deployKubectl
// below (it adds the two writes of Deploy and the reads of the wait), a fake
// Prober and the fake clock of clock_test.go. No real kubectl, no network, no
// real wait.
//
// The kubectl calls the tests assume (arguments after "kubectl", without
// --context; "--request-timeout=<d>" may sit anywhere and is ignored):
//
//	RunPreflight(OpUp), first, whole (see preflight_test.go); nothing is written
//	                                 before it succeeds
//	apply --server-side --field-manager=devdesk-nexus-bench -f -
//	                                 stdin = RenderNamespace(p): the Namespace alone
//	apply --server-side --field-manager=devdesk-nexus-bench -f -
//	                                 stdin = Render(p): the whole stream, the
//	                                 Namespace again included (idempotent)
//	get pods -n <ns> -l app.kubernetes.io/name=nexus -o json    } once per round,
//	get pvc  -n <ns> -o json                                    } in either order
//
// The order of the arguments of the two reads is free ("-n", "-l" and "-o" may
// come in any order, "pvc" may be spelled "persistentvolumeclaims"); the
// resource, the namespace, the selector of the pods and "-o json" are not.
// Anything else fails the test as unscripted. A round is separated from the
// next by Clock.Sleep, and the cluster answers the n-th round with the n-th
// scripted state (the last one for ever after).
//
// Hypotheses about what Deploy does, to settle with the code:
//   - it checks ctx itself before the first write (the fake runner ignores ctx);
//   - each round reads the pods and the claims, diagnoses them, then asks the
//     Prober about every host; it returns after the first round in which at
//     least one host answered, with every host that answered in that round, in
//     the order of the hosts (p.Hosts, or 127.0.0.1 then the node InternalIP);
//   - a Prober base URL is "http://" + net.JoinHostPort(host, HTTPNodePort);
//   - the wait sleeps between 1 s and 10 s per round, and gives up at p.Timeout
//     (within one interval) with a Deadline BenchError naming the last state;
//   - ImagePull and Crash are returned at the first round that shows them; an
//     Unschedulable pod and a Pending claim are returned as PodPending no later
//     than the deadline, and a claim that is Pending in one round only (the
//     volume binds when the pod is scheduled) is not a failure;
//   - a warning is a line of out containing "warning" (case-insensitive) and
//     the host that did not answer; there is none when every host answered;
//   - no message and no line of out carries "password", the fake secret, or the
//     text of stderr (the Err of the BenchError keeps it for errors.Is).

const (
	deployPodName     = "nexus-7c9d8b6f4-x2k9p"
	deployApplyLine   = "apply --server-side --field-manager=devdesk-nexus-bench -f -"
	deployPodSelector = "app.kubernetes.io/name=nexus"
	deployPVCName     = "nexus-data"

	// The wait polls between these two bounds (the plan says 5 s).
	minPollInterval = time.Second
	maxPollInterval = 10 * time.Second
)

// ---- doubles ----------------------------------------------------------------

// fakePod is the Nexus pod as one round sees it.
type fakePod struct {
	name  string
	phase string
	// scheduled is the status of the PodScheduled condition: "" (no such
	// condition yet), "True" or "False" (then the reason is Unschedulable).
	scheduled     string
	unschedulable string // message of the PodScheduled=False condition
	ready         bool

	// The state of the container; none is reported for a pod that is not scheduled.
	waiting, waitingMessage string // reason and message of state.waiting
	running                 bool
	lastTerminated          string // reason of lastState.terminated
	restarts                int
}

func (pd fakePod) object() map[string]any {
	name := pd.name
	if name == "" {
		name = deployPodName
	}
	var conds []any
	if pd.scheduled != "" {
		c := map[string]any{"type": "PodScheduled", "status": pd.scheduled}
		if pd.scheduled == "False" {
			c["reason"] = "Unschedulable"
			c["message"] = pd.unschedulable
		}
		conds = append(conds, c)
	}
	readyStatus := "False"
	if pd.ready {
		readyStatus = "True"
	}
	conds = append(conds, map[string]any{"type": "Ready", "status": readyStatus})
	status := map[string]any{"phase": pd.phase, "conditions": conds}

	if pd.scheduled == "True" {
		cs := map[string]any{"name": "nexus", "ready": pd.ready, "restartCount": pd.restarts, "image": "docker.io/sonatype/nexus3"}
		switch {
		case pd.waiting != "":
			cs["state"] = map[string]any{"waiting": map[string]any{"reason": pd.waiting, "message": pd.waitingMessage}}
		case pd.running:
			cs["state"] = map[string]any{"running": map[string]any{"startedAt": "2026-11-16T12:00:05Z"}}
		}
		if pd.lastTerminated != "" {
			cs["lastState"] = map[string]any{"terminated": map[string]any{"reason": pd.lastTerminated, "exitCode": 137}}
		}
		status["containerStatuses"] = []any{cs}
	}
	return map[string]any{
		"metadata": map[string]any{"name": name, "labels": map[string]string{"app.kubernetes.io/name": "nexus"}},
		"status":   status,
	}
}

func creatingPod() fakePod {
	return fakePod{phase: "Pending", scheduled: "True", waiting: "ContainerCreating"}
}
func startingPod() fakePod { return fakePod{phase: "Running", scheduled: "True", running: true} }
func readyPod() fakePod {
	return fakePod{phase: "Running", scheduled: "True", running: true, ready: true}
}
func unscheduledPod() fakePod { return fakePod{phase: "Pending"} }
func unschedulablePod(msg string) fakePod {
	return fakePod{phase: "Pending", scheduled: "False", unschedulable: msg}
}
func pullFailedPod(reason string) fakePod {
	return fakePod{
		phase: "Pending", scheduled: "True", waiting: reason,
		waitingMessage: `Failed to pull image "docker.io/sonatype/nexus3:3.98.1": rpc error: code = Unknown`,
	}
}

// clusterState is what the cluster answers in one round.
type clusterState struct {
	pods     []fakePod
	pvcPhase string // "" means the claim is not there
}

func state(pvcPhase string, pods ...fakePod) clusterState {
	return clusterState{pods: pods, pvcPhase: pvcPhase}
}

// deployKubectl wraps the scripted kubectl with what Deploy writes and reads.
type deployKubectl struct {
	*fakeKubectl
	clk    *fakeClock
	states []clusterState

	nsDoc, fullDoc []byte // what the two applies must send

	// failNamespace and failFull, when set, answer the apply of that stage
	// with an error; the apply has then no effect on the cluster.
	failNamespace, failFull *fakeReply

	lines    []string   // every call, normalized, in order
	applies  []fakeCall // every apply, with its stdin
	podReads int
	pvcReads int
}

var _ Runner = (*deployKubectl)(nil)

func newDeployKubectl(t *testing.T, p Params, clk *fakeClock, states []clusterState) *deployKubectl {
	t.Helper()
	nsDoc, err := RenderNamespace(p)
	if err != nil {
		t.Fatalf("RenderNamespace: %v", err)
	}
	fullDoc, err := Render(p)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(states) == 0 {
		states = []clusterState{state("Bound", readyPod())}
	}
	return &deployKubectl{fakeKubectl: newFakeKubectl(t, p), clk: clk, states: states, nsDoc: nsDoc, fullDoc: fullDoc}
}

// current is the scripted state of the round being played: one round per sleep.
func (d *deployKubectl) current() clusterState {
	return d.states[min(len(d.clk.sleeps), len(d.states)-1)]
}

func (d *deployKubectl) Run(ctx context.Context, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	norm := normalizeArgs(args)
	joined := strings.Join(norm, " ")
	d.lines = append(d.lines, joined)

	switch verb := verbOf(args); {
	case verb == "apply":
		return d.apply(joined, stdin)
	case isWriteVerb(verb):
		d.t.Errorf("unexpected write call %q: Deploy writes with apply only", args)
		return nil, []byte("the fake refuses a write: " + verb), errFakeExit1
	case verb == "get" && len(norm) > 1 && isPodResource(norm[1]):
		d.podReads++
		d.checkRead(norm, true)
		return d.answerPods()
	case verb == "get" && len(norm) > 1 && isPVCResource(norm[1]):
		d.pvcReads++
		d.checkRead(norm, false)
		return d.answerPVCs()
	}
	return d.fakeKubectl.Run(ctx, stdin, args...)
}

func isPodResource(r string) bool { return r == "pods" || r == "pod" }
func isPVCResource(r string) bool {
	return r == "pvc" || r == "persistentvolumeclaims" || r == "persistentvolumeclaim"
}

// checkRead verifies the namespace, the selector (pods) and the output of a read.
func (d *deployKubectl) checkRead(norm []string, withSelector bool) {
	var ns, selector, output string
	for i := 2; i < len(norm); i++ {
		switch norm[i] {
		case "-n", "--namespace":
			i++
			ns = at(norm, i)
		case "-l", "--selector":
			i++
			selector = at(norm, i)
		case "-o", "--output":
			i++
			output = at(norm, i)
		default:
			d.t.Errorf("unscripted argument %q in %q", norm[i], norm)
		}
	}
	if ns != d.p.Namespace {
		d.t.Errorf("%q reads namespace %q, the run is about %q", norm, ns, d.p.Namespace)
	}
	if output != "json" {
		d.t.Errorf("%q must ask for -o json", norm)
	}
	if withSelector && selector != deployPodSelector {
		d.t.Errorf("%q must select %s, got %q", norm, deployPodSelector, selector)
	}
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// apply records a write and, when it is accepted, applies its effect: the
// namespace exists (and is ours) after the first, the Service holds the
// NodePorts after the second.
func (d *deployKubectl) apply(joined string, stdin []byte) ([]byte, []byte, error) {
	d.applies = append(d.applies, fakeCall{args: strings.Fields(joined), stdin: slices.Clone(stdin)})
	if joined != deployApplyLine {
		d.t.Errorf("apply call %q, want %q", joined, deployApplyLine)
		return nil, []byte("the fake refuses this apply"), errFakeExit1
	}
	switch {
	case bytes.Equal(stdin, d.nsDoc):
		if d.failNamespace != nil {
			return d.failNamespace.reply()
		}
		d.ours()
		return []byte("namespace/" + d.p.Namespace + " serverside-applied\n"), nil, nil
	case bytes.Equal(stdin, d.fullDoc):
		if d.failFull != nil {
			return d.failFull.reply()
		}
		d.ours().addService(d.p.Namespace, nexusServiceName, d.p.HTTPNodePort, d.p.DockerNodePort)
		return []byte("service/nexus serverside-applied\n"), nil, nil
	}
	d.t.Errorf("apply sent a document that is neither RenderNamespace nor Render:\n%s", stdin)
	return nil, []byte("the fake refuses this document"), errFakeExit1
}

func (d *deployKubectl) answerPods() ([]byte, []byte, error) {
	items := []any{}
	for _, pd := range d.current().pods {
		items = append(items, pd.object())
	}
	return d.mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": items}), nil, nil
}

func (d *deployKubectl) answerPVCs() ([]byte, []byte, error) {
	items := []any{}
	if phase := d.current().pvcPhase; phase != "" {
		items = append(items, map[string]any{
			"metadata": map[string]any{"name": deployPVCName},
			"spec": map[string]any{
				"storageClassName": "local-path",
				"resources":        map[string]any{"requests": map[string]any{"storage": d.p.StorageSize}},
			},
			"status": map[string]any{"phase": phase},
		})
	}
	return d.mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": items}), nil, nil
}

// fakeProber answers Ready by the host of the URL and the time of the fake
// clock: a host is ready from its entry of readyAfter on, and never when it has
// none. It is safe for concurrent use, because Deploy may probe the hosts in
// parallel.
type fakeProber struct {
	clk        *fakeClock
	readyAfter map[string]time.Duration

	mu   sync.Mutex
	urls []string
}

var _ Prober = (*fakeProber)(nil)

func (f *fakeProber) Ready(ctx context.Context, baseURL string) error {
	f.mu.Lock()
	f.urls = append(f.urls, baseURL)
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("fake prober: bad URL %q: %w", baseURL, err)
	}
	if after, ok := f.readyAfter[u.Hostname()]; ok && f.clk.elapsed() >= after {
		return nil
	}
	// The text carries a secret on purpose: it must not reach a message.
	return fmt.Errorf("dial tcp %s: connect: connection refused (token=%s)", u.Host, fakeSecret)
}

func (f *fakeProber) probed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.urls)
}

// deployEnv is one Deploy run: its cluster, prober, clock and output.
type deployEnv struct {
	t    *testing.T
	p    Params
	kube *deployKubectl
	pr   *fakeProber
	clk  *fakeClock
	out  bytes.Buffer
}

// deployParams are the defaults with a short deadline (the clock is fake, but a
// shorter one keeps the loops small).
func deployParams() Params {
	p := Defaults()
	p.Timeout = 2 * time.Minute
	return p
}

// newDeployEnv is a healthy cluster without the namespace. readyAfter says from
// when on each host answers (elapsed time of the fake clock).
func newDeployEnv(t *testing.T, p Params, readyAfter map[string]time.Duration, states ...clusterState) *deployEnv {
	t.Helper()
	clk := newFakeClock(t)
	return &deployEnv{
		t:    t,
		p:    p,
		kube: newDeployKubectl(t, p, clk, states),
		pr:   &fakeProber{clk: clk, readyAfter: readyAfter},
		clk:  clk,
	}
}

func (e *deployEnv) deploy(ctx context.Context) (DeployResult, error) {
	e.t.Helper()
	return Deploy(ctx, e.kube, e.pr, e.clk, e.p, &e.out)
}

// warmUp is the usual way a bench starts: the pod is created, then starting,
// then ready, and the claim is bound from the first round on.
func warmUp() []clusterState {
	return []clusterState{
		state("Bound", creatingPod()),
		state("Bound", startingPod()),
		state("Bound", readyPod()),
	}
}

func hostAnswers(after time.Duration, hosts ...string) map[string]time.Duration {
	m := map[string]time.Duration{}
	for _, h := range hosts {
		m[h] = after
	}
	return m
}

// requireTwoApplies checks the writes of a run that got past the preflight:
// the Namespace alone, then the whole stream.
func (e *deployEnv) requireTwoApplies() {
	e.t.Helper()
	got := e.kube.applies
	if len(got) != 2 {
		e.t.Fatalf("%d apply calls, want 2 (the Namespace, then everything): %q", len(got), e.kube.lines)
	}
	if !bytes.Equal(got[0].stdin, e.kube.nsDoc) {
		e.t.Errorf("the first apply did not send RenderNamespace(p):\n%s", got[0].stdin)
	}
	if !bytes.Equal(got[1].stdin, e.kube.fullDoc) {
		e.t.Errorf("the second apply did not send Render(p):\n%s", got[1].stdin)
	}
}

// requireOrder checks that the preflight finished before the first write and
// that nothing was read from the pods before the second one.
func (e *deployEnv) requireOrder() {
	e.t.Helper()
	first := slices.Index(e.kube.lines, deployApplyLine)
	second := -1
	if first >= 0 {
		second = first + 1 + slices.Index(e.kube.lines[first+1:], deployApplyLine)
	}
	if first < 0 || second <= first {
		e.t.Fatalf("expected two apply calls in %q", e.kube.lines)
	}
	for _, want := range []string{"get services -A -o json", "get nodes -o json", "get namespace " + e.p.Namespace + " -o json"} {
		if i := slices.Index(e.kube.lines, want); i < 0 || i > first {
			e.t.Errorf("the preflight call %q is not before the first apply (index %d, apply at %d)", want, i, first)
		}
	}
	for i, line := range e.kube.lines {
		isRead := strings.HasPrefix(line, "get pods") || strings.HasPrefix(line, "get pvc") || strings.HasPrefix(line, "get persistentvolumeclaims")
		if isRead && i < second {
			e.t.Errorf("%q is called before the second apply (index %d, apply at %d)", line, i, second)
		}
	}
}

// requirePollIntervals checks every wait is between the bounds.
func (e *deployEnv) requirePollIntervals() {
	e.t.Helper()
	for i, d := range e.clk.sleeps {
		if d < minPollInterval || d > maxPollInterval {
			e.t.Errorf("sleep #%d lasts %v, want between %v and %v", i+1, d, minPollInterval, maxPollInterval)
		}
	}
}

// requireNoWrite checks nothing was written and nothing was waited for.
func (e *deployEnv) requireNoWrite() {
	e.t.Helper()
	if len(e.kube.applies) != 0 {
		e.t.Errorf("Deploy wrote to the cluster: %q", e.kube.lines)
	}
	if e.kube.podReads != 0 || e.kube.pvcReads != 0 {
		e.t.Errorf("Deploy read the pods (%d) or the claims (%d) although it stopped before the apply", e.kube.podReads, e.kube.pvcReads)
	}
	if got := e.pr.probed(); len(got) != 0 {
		e.t.Errorf("Deploy probed %q although it stopped before the apply", got)
	}
}

// requireNoSecret checks that nothing Deploy said carries a password or a secret.
func (e *deployEnv) requireNoSecret(err error) {
	e.t.Helper()
	said := e.out.String()
	if err != nil {
		said += "\n" + err.Error()
	}
	for _, banned := range []string{"password", strings.ToLower(secretMarker), strings.ToLower(fakeSecret)} {
		if strings.Contains(strings.ToLower(said), banned) {
			e.t.Errorf("Deploy said %q, which contains %q", said, banned)
		}
	}
}

// requireDeployKind checks err is a clean *BenchError of the kind.
func requireDeployKind(t *testing.T, err error, kind ErrKind) *BenchError {
	t.Helper()
	if err == nil {
		t.Fatalf("Deploy succeeded, want a BenchError of kind %d", kind)
	}
	be := requireCleanError(t, err)
	if be.Kind != kind {
		t.Errorf("Kind = %d, want %d; message: %v", be.Kind, kind, err)
	}
	return be
}

func requireMentions(t *testing.T, err error, mention, notMention []string) {
	t.Helper()
	msg := strings.ToLower(err.Error())
	for _, want := range mention {
		if !strings.Contains(msg, strings.ToLower(want)) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
	for _, banned := range notMention {
		if strings.Contains(msg, strings.ToLower(banned)) {
			t.Errorf("message %q mentions %q, which is not the cause", err.Error(), banned)
		}
	}
}

// ---- success ----------------------------------------------------------------

func TestDeploySucceeds(t *testing.T) {
	otherNS := func(p *Params) {
		p.Namespace = "nexus-bench-2"
		p.UpstreamURL = upstreamFor(p.Namespace)
		p.HTTPNodePort, p.DockerNodePort = 30490, 30491
	}
	tests := []struct {
		name       string
		params     func(p *Params)
		nodeAddrs  [][2]string // nil keeps the node of the fake cluster
		readyAfter map[string]time.Duration
		wantURLs   []string // every URL the prober may be asked about
		want       DeployResult
		warnAbout  string // a host the warning must name; "" means no warning
	}{
		{
			name:       "both default hosts answer",
			readyAfter: hostAnswers(15*time.Second, "127.0.0.1", fakeNodeIP),
			wantURLs:   []string{"http://127.0.0.1:30481", "http://" + fakeNodeIP + ":30481"},
			want:       DeployResult{ReachableHosts: []string{"127.0.0.1", fakeNodeIP}, NodeIP: fakeNodeIP},
		},
		{
			name:       "only the loopback answers: success, with a warning about the node address",
			readyAfter: hostAnswers(15*time.Second, "127.0.0.1"),
			wantURLs:   []string{"http://127.0.0.1:30481", "http://" + fakeNodeIP + ":30481"},
			want:       DeployResult{ReachableHosts: []string{"127.0.0.1"}, NodeIP: fakeNodeIP},
			warnAbout:  fakeNodeIP,
		},
		{
			name:       "only the node address answers: success, with a warning about the loopback",
			readyAfter: hostAnswers(15*time.Second, fakeNodeIP),
			wantURLs:   []string{"http://127.0.0.1:30481", "http://" + fakeNodeIP + ":30481"},
			want:       DeployResult{ReachableHosts: []string{fakeNodeIP}, NodeIP: fakeNodeIP},
			warnAbout:  "127.0.0.1",
		},
		{
			name:       "a node without InternalIP: the loopback alone is tried, no warning",
			nodeAddrs:  [][2]string{{"Hostname", "datomir"}},
			readyAfter: hostAnswers(15*time.Second, "127.0.0.1"),
			wantURLs:   []string{"http://127.0.0.1:30481"},
			want:       DeployResult{ReachableHosts: []string{"127.0.0.1"}, NodeIP: ""},
		},
		{
			name: "explicit hosts replace the default ones, other port and namespace, IPv6 in brackets",
			params: func(p *Params) {
				otherNS(p)
				p.Hosts = []string{"10.0.0.5", "::1"}
			},
			readyAfter: hostAnswers(15*time.Second, "10.0.0.5", "::1"),
			wantURLs:   []string{"http://10.0.0.5:30490", "http://[::1]:30490"},
			want:       DeployResult{ReachableHosts: []string{"10.0.0.5", "::1"}, NodeIP: fakeNodeIP},
		},
		{
			name:       "the hosts answer at once: nothing to wait for",
			readyAfter: hostAnswers(0, "127.0.0.1", fakeNodeIP),
			wantURLs:   []string{"http://127.0.0.1:30481", "http://" + fakeNodeIP + ":30481"},
			want:       DeployResult{ReachableHosts: []string{"127.0.0.1", fakeNodeIP}, NodeIP: fakeNodeIP},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := deployParams()
			if tt.params != nil {
				tt.params(&p)
			}
			e := newDeployEnv(t, p, tt.readyAfter, warmUp()...)
			if tt.nodeAddrs != nil {
				e.kube.nodeAddresses = tt.nodeAddrs
			}

			got, err := e.deploy(context.Background())
			if err != nil {
				t.Fatalf("Deploy: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("result = %+v, want %+v", got, tt.want)
			}

			e.requireTwoApplies()
			e.requireOrder()
			e.requirePollIntervals()
			e.requireNoSecret(nil)

			// It asked only about the hosts it should, never about another one.
			for _, u := range e.pr.probed() {
				if !slices.Contains(tt.wantURLs, u) {
					t.Errorf("probed %q, want one of %q", u, tt.wantURLs)
				}
			}
			for _, host := range tt.want.ReachableHosts {
				u := "http://" + net.JoinHostPort(host, strconv.Itoa(p.HTTPNodePort))
				if !slices.Contains(e.pr.probed(), u) {
					t.Errorf("never probed %q; probed %q", u, e.pr.probed())
				}
			}

			// It comes back as soon as a host answers: one interval at most
			// after the first answer, never at the deadline.
			if limit := 15*time.Second + maxPollInterval; e.clk.elapsed() > limit {
				t.Errorf("returned after %v although a host answered from 15s on (limit %v)", e.clk.elapsed(), limit)
			}

			said := strings.ToLower(e.out.String())
			if tt.warnAbout == "" {
				if strings.Contains(said, "warning") {
					t.Errorf("unexpected warning in %q", e.out.String())
				}
				return
			}
			if !strings.Contains(said, "warning") || !strings.Contains(e.out.String(), tt.warnAbout) {
				t.Errorf("out = %q, want a warning that names %q", e.out.String(), tt.warnAbout)
			}
		})
	}
}

// An old OOM kill stays in lastState for as long as the pod lives: a container
// that is running and ready again is healthy, and a second up must not fail on it.
func TestDeployIgnoresAnOldOOMOnAReadyContainer(t *testing.T) {
	old := readyPod()
	old.lastTerminated = "OOMKilled"
	old.restarts = 2
	e := newDeployEnv(t, deployParams(), hostAnswers(0, "127.0.0.1", fakeNodeIP), state("Bound", old))
	if _, err := e.deploy(context.Background()); err != nil {
		t.Fatalf("Deploy failed on a ready container whose last state is an old OOMKilled: %v", err)
	}
}

// A transient state is not a failure: the claim of a WaitForFirstConsumer
// class stays Pending until the pod is scheduled, and the pod has no
// condition at first.
func TestDeployToleratesTheStartUp(t *testing.T) {
	p := deployParams()
	e := newDeployEnv(t, p, hostAnswers(30*time.Second, "127.0.0.1", fakeNodeIP),
		state(""),                          // the Deployment has made no pod yet
		state("Pending", unscheduledPod()), // the volume waits for the pod
		state("Pending", creatingPod()),    // scheduled, the volume is being bound
		state("Bound", creatingPod()),      // image being pulled
		state("Bound", startingPod()),      // Nexus is starting
		state("Bound", readyPod()),         // up
	)
	got, err := e.deploy(context.Background())
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if want := []string{"127.0.0.1", fakeNodeIP}; !slices.Equal(got.ReachableHosts, want) {
		t.Errorf("ReachableHosts = %q, want %q", got.ReachableHosts, want)
	}
	e.requirePollIntervals()
	e.requireNoSecret(nil)
}

// CA3: a second run is the same calls and the same result.
func TestDeployIsIdempotent(t *testing.T) {
	p := deployParams()
	e := newDeployEnv(t, p, hostAnswers(15*time.Second, "127.0.0.1", fakeNodeIP), warmUp()...)

	first, err := e.deploy(context.Background())
	if err != nil {
		t.Fatalf("first Deploy: %v", err)
	}
	if len(e.kube.applies) != 2 {
		t.Fatalf("first Deploy made %d applies, want 2", len(e.kube.applies))
	}
	second, err := e.deploy(context.Background())
	if err != nil {
		t.Fatalf("second Deploy on the namespace and the Service of the first: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second result = %+v, first = %+v", second, first)
	}
	if len(e.kube.applies) != 4 {
		t.Fatalf("after two runs: %d applies, want 4 (the same two again)", len(e.kube.applies))
	}
	for i := range 2 {
		a, b := e.kube.applies[i], e.kube.applies[i+2]
		if !slices.Equal(a.args, b.args) || !bytes.Equal(a.stdin, b.stdin) {
			t.Errorf("apply #%d of the second run differs from the first run:\n%q\n%s\nvs\n%q\n%s", i+1, b.args, b.stdin, a.args, a.stdin)
		}
	}
	e.requirePollIntervals()
	e.requireNoSecret(nil)
}

// ---- diagnosis --------------------------------------------------------------

func TestDeployDiagnosesTheWait(t *testing.T) {
	tests := []struct {
		name       string
		params     func(p *Params)
		states     []clusterState
		kind       ErrKind
		mention    []string
		notMention []string
		immediate  bool // returned at the first round that shows it, not at the deadline
	}{
		{
			name:    "unschedulable for memory, the claim waits for the pod: the memory is to blame",
			params:  func(p *Params) { p.MemoryRequest, p.MemoryLimit = "6Gi", "8Gi" },
			states:  []clusterState{state("Pending", unschedulablePod("0/1 nodes are available: 1 Insufficient memory. preemption: 0/1 nodes are available: 1 No preemption victims found for incoming pod."))},
			kind:    PodPending,
			mention: []string{"memory", "6Gi", "NEXUS_BENCH_MEMORY_REQUEST"},
			// The claim is Pending too, but only because the pod is not scheduled.
			notMention: []string{"NEXUS_BENCH_STORAGE_SIZE"},
		},
		{
			name:    "unschedulable for memory with the default request",
			states:  []clusterState{state("Bound", unschedulablePod("0/1 nodes are available: 1 Insufficient memory."))},
			kind:    PodPending,
			mention: []string{"memory", "3Gi", "NEXUS_BENCH_MEMORY_REQUEST"},
		},
		{
			name:    "unschedulable for cpu",
			params:  func(p *Params) { p.CPURequest = "750m" },
			states:  []clusterState{state("Bound", unschedulablePod("0/1 nodes are available: 1 Insufficient cpu."))},
			kind:    PodPending,
			mention: []string{"cpu", "750m", "NEXUS_BENCH_CPU_REQUEST"},
		},
		{
			name:    "the pod is scheduled and the claim stays Pending",
			params:  func(p *Params) { p.StorageSize = "7Gi" },
			states:  []clusterState{state("Pending", creatingPod())},
			kind:    PodPending,
			mention: []string{"storage", "7Gi", "NEXUS_BENCH_STORAGE_SIZE"},
		},
		{
			name:      "ErrImagePull",
			params:    func(p *Params) { p.ImageTag = "3.98.9" },
			states:    []clusterState{state("Bound", pullFailedPod("ErrImagePull"))},
			kind:      ImagePull,
			mention:   []string{"docker.io/sonatype/nexus3:3.98.9"},
			immediate: true,
		},
		{
			name:      "ImagePullBackOff",
			states:    []clusterState{state("Bound", pullFailedPod("ImagePullBackOff"))},
			kind:      ImagePull,
			mention:   []string{Defaults().ImageRef()},
			immediate: true,
		},
		{
			name: "ImagePullBackOff seen from the third round",
			states: []clusterState{
				state("Bound", creatingPod()),
				state("Bound", creatingPod()),
				state("Bound", pullFailedPod("ImagePullBackOff")),
			},
			kind:      ImagePull,
			mention:   []string{Defaults().ImageRef()},
			immediate: true,
		},
		{
			name:   "OOMKilled in the last state of a restarted container",
			params: func(p *Params) { p.MemoryLimit = "5Gi" },
			states: []clusterState{state("Bound", fakePod{
				phase: "Running", scheduled: "True", running: true, lastTerminated: "OOMKilled", restarts: 1,
			})},
			kind:      Crash,
			mention:   []string{"OOMKilled", "5Gi", "NEXUS_BENCH_MEMORY_LIMIT"},
			immediate: true,
		},
		{
			name: "CrashLoopBackOff after an OOMKilled",
			states: []clusterState{state("Bound", fakePod{
				phase: "Running", scheduled: "True", waiting: "CrashLoopBackOff", lastTerminated: "OOMKilled", restarts: 3,
			})},
			kind:      Crash,
			mention:   []string{"OOMKilled", "4Gi", "NEXUS_BENCH_MEMORY_LIMIT"},
			immediate: true,
		},
		{
			name: "CrashLoopBackOff after an error exit",
			states: []clusterState{state("Bound", fakePod{
				phase: "Running", scheduled: "True", waiting: "CrashLoopBackOff", lastTerminated: "Error", restarts: 5,
			})},
			kind:      Crash,
			mention:   []string{"CrashLoopBackOff", "NEXUS_BENCH_MEMORY_LIMIT"},
			immediate: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := deployParams()
			if tt.params != nil {
				tt.params(&p)
			}
			// The prober never answers: only the diagnosis can end the wait early.
			e := newDeployEnv(t, p, nil, tt.states...)

			_, err := e.deploy(context.Background())
			_ = requireDeployKind(t, err, tt.kind)
			requireMentions(t, err, tt.mention, tt.notMention)
			e.requireTwoApplies()
			e.requireNoSecret(err)
			e.requirePollIntervals()

			if tt.immediate {
				// At most one interval per scripted round before the failing one.
				if limit := time.Duration(len(tt.states)) * maxPollInterval; e.clk.elapsed() > limit {
					t.Errorf("returned after %v, want at the first round that shows the failure (within %v)", e.clk.elapsed(), limit)
				}
			} else if limit := p.Timeout + maxPollInterval; e.clk.elapsed() > limit {
				t.Errorf("returned after %v, past the deadline %v", e.clk.elapsed(), p.Timeout)
			}
		})
	}
}

func TestDeployDeadline(t *testing.T) {
	tests := []struct {
		name    string
		states  []clusterState
		never   bool // the prober answers nowhere, the pod is up
		mention []string
	}{
		{
			name:    "Nexus is running but never ready",
			states:  []clusterState{state("Bound", creatingPod()), state("Bound", startingPod())},
			mention: []string{"Running", "NEXUS_BENCH_TIMEOUT"},
		},
		{
			name:    "the container never leaves ContainerCreating",
			states:  []clusterState{state("Bound", creatingPod())},
			mention: []string{"ContainerCreating", "NEXUS_BENCH_TIMEOUT"},
		},
		{
			name:    "no pod ever appears",
			states:  []clusterState{state("")},
			mention: []string{"no pod", "NEXUS_BENCH_TIMEOUT"},
		},
		{
			name:    "the pod is ready but the NodePort does not answer",
			states:  []clusterState{state("Bound", readyPod())},
			mention: []string{"127.0.0.1", "NEXUS_BENCH_TIMEOUT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := deployParams()
			e := newDeployEnv(t, p, nil, tt.states...)

			_, err := e.deploy(context.Background())
			_ = requireDeployKind(t, err, Deadline)
			requireMentions(t, err, tt.mention, nil)
			e.requireTwoApplies()
			e.requireNoSecret(err)
			e.requirePollIntervals()

			// It waited for the whole timeout, give or take one interval.
			if got := e.clk.elapsed(); got < p.Timeout-maxPollInterval || got > p.Timeout+maxPollInterval {
				t.Errorf("gave up after %v, want about %v", got, p.Timeout)
			}
		})
	}
}

// ---- failures of kubectl ----------------------------------------------------

func TestDeployClassifiesTheFailureOfAnApply(t *testing.T) {
	fail := func(stderr string) *fakeReply {
		// The stderr carries a secret on purpose: it must not reach a message.
		return &fakeReply{stderr: stderr + " token=" + fakeSecret + "\n", err: errFakeExit1}
	}
	tests := []struct {
		name         string
		inNamespace  bool // the first apply fails, else the second
		reply        *fakeReply
		kind         ErrKind
		mention      []string
		wantedWrites int
	}{
		{
			name:         "a NodePort is taken at the second apply",
			reply:        fail(`Error from server (Invalid): Service "nexus" is invalid: spec.ports[0].nodePort: Invalid value: 30481: provided port is already allocated`),
			kind:         NodePortTaken,
			mention:      []string{"NEXUS_BENCH_HTTP_NODEPORT"},
			wantedWrites: 2,
		},
		{
			name:         "an admission webhook denies the second apply",
			reply:        fail(`Error from server (Forbidden): admission webhook "validate.kyverno.svc-fail" denied the request: policy blocked it`),
			kind:         AdmissionDenied,
			wantedWrites: 2,
		},
		{
			name:         "an admission webhook denies the Namespace",
			inNamespace:  true,
			reply:        fail(`Error from server (Forbidden): admission webhook "validate.kyverno.svc-fail" denied the request: policy blocked it`),
			kind:         AdmissionDenied,
			wantedWrites: 1,
		},
		{
			name:         "the API server refuses the second apply",
			reply:        fail(`Error from server (Forbidden): services "nexus" is forbidden: User "anthoni" cannot patch resource "services" in API group "" in the namespace "nexus-bench"`),
			kind:         Forbidden,
			wantedWrites: 2,
		},
		{
			name:         "the API server refuses the Namespace",
			inNamespace:  true,
			reply:        fail(`Error from server (Forbidden): namespaces is forbidden: User "anthoni" cannot create resource "namespaces" in API group "" at the cluster scope`),
			kind:         Forbidden,
			wantedWrites: 1,
		},
		{
			name:         "a failure nobody classified",
			reply:        fail(`error: unable to recognize "STDIN": no matches for kind "Deployment"`),
			kind:         Unclassified,
			wantedWrites: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := deployParams()
			e := newDeployEnv(t, p, hostAnswers(0, "127.0.0.1"), warmUp()...)
			if tt.inNamespace {
				e.kube.failNamespace = tt.reply
			} else {
				e.kube.failFull = tt.reply
			}

			_, err := e.deploy(context.Background())
			be := requireDeployKind(t, err, tt.kind)
			requireMentions(t, err, tt.mention, nil)
			if !errors.Is(err, errFakeExit1) {
				t.Errorf("the error does not wrap the failure of kubectl: %v", err)
			}
			if be.Cause == "" {
				t.Error("no Cause")
			}
			e.requireNoSecret(err)

			if got := len(e.kube.applies); got != tt.wantedWrites {
				t.Errorf("%d applies, want %d: nothing after a failed apply", got, tt.wantedWrites)
			}
			if e.kube.podReads != 0 || e.kube.pvcReads != 0 {
				t.Errorf("the wait started although an apply failed (pods %d, claims %d)", e.kube.podReads, e.kube.pvcReads)
			}
			if got := e.pr.probed(); len(got) != 0 {
				t.Errorf("probed %q although an apply failed", got)
			}
			if len(e.clk.sleeps) != 0 {
				t.Errorf("slept %v although an apply failed", e.clk.sleeps)
			}
		})
	}
}

// ---- the preflight comes first ----------------------------------------------

func TestDeployStopsWhenThePreflightFails(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fakeKubectl)
		kind  ErrKind
	}{
		{"context not in the kubeconfig", func(f *fakeKubectl) { f.contexts = "k0s\n" }, ContextNotFound},
		{"cluster unreachable", func(f *fakeKubectl) {
			f.readyz = fakeReply{stderr: "Unable to connect to the server: dial tcp 192.168.1.31:6443: connect: connection refused\n", err: errFakeExit1}
		}, ClusterUnreachable},
		{"not allowed to create namespaces", func(f *fakeKubectl) { f.deny("create", "namespaces") }, Forbidden},
		{"not allowed to patch deployments", func(f *fakeKubectl) { f.deny("patch", "deployments") }, Forbidden},
		{"not allowed to read the pods", func(f *fakeKubectl) { f.deny("get", "pods") }, Forbidden},
		{"the namespace is not ours", func(f *fakeKubectl) { f.withNamespace(map[string]string{"team": "other"}) }, NamespaceForeign},
		{"a NodePort is held by another Service", func(f *fakeKubectl) { f.addService("plane", "plane-extra", 30481) }, NodePortTaken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := deployParams()
			e := newDeployEnv(t, p, hostAnswers(0, "127.0.0.1"), warmUp()...)
			tt.setup(e.kube.fakeKubectl)

			_, err := e.deploy(context.Background())
			_ = requireDeployKind(t, err, tt.kind)
			e.requireNoWrite()
			e.requireNoSecret(err)
			if len(e.clk.sleeps) != 0 {
				t.Errorf("slept %v although the preflight failed", e.clk.sleeps)
			}
		})
	}
}

func TestDeployRefusesInvalidParamsBeforeAnyCall(t *testing.T) {
	p := deployParams()
	p.Namespace = "default"
	clk := newFakeClock(t)
	kube := newDeployKubectl(t, deployParams(), clk, nil) // built from valid parameters
	pr := &fakeProber{clk: clk}

	var out bytes.Buffer
	if _, err := Deploy(context.Background(), kube, pr, clk, p, &out); err == nil {
		t.Fatal("Deploy accepted the namespace \"default\"")
	}
	if len(kube.lines) != 0 {
		t.Errorf("kubectl was called with invalid parameters: %q", kube.lines)
	}
	if len(pr.probed()) != 0 {
		t.Errorf("the prober was called with invalid parameters: %q", pr.probed())
	}
}

// ---- cancellation -----------------------------------------------------------

func TestDeployStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Run("before anything is written", func(t *testing.T) {
		p := deployParams()
		e := newDeployEnv(t, p, hostAnswers(0, "127.0.0.1"), warmUp()...)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := e.deploy(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Deploy with a cancelled context = %v, want context.Canceled", err)
		}
		if len(e.kube.applies) != 0 {
			t.Errorf("Deploy wrote after the context was cancelled: %q", e.kube.lines)
		}
		e.requireNoSecret(err)
	})

	t.Run("while it waits", func(t *testing.T) {
		p := deployParams()
		e := newDeployEnv(t, p, nil, state("Bound", creatingPod()))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.clk.onSleep = func(n int) {
			if n == 2 {
				cancel()
			}
		}

		_, err := e.deploy(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Deploy cancelled while waiting = %v, want context.Canceled", err)
		}
		var be *BenchError
		if errors.As(err, &be) && be.Kind == Deadline {
			t.Errorf("a cancellation is not a deadline: %v", err)
		}
		if got := len(e.clk.sleeps); got != 2 {
			t.Errorf("%d sleeps, want 2: it stops at the one that was cancelled", got)
		}
		e.requireTwoApplies()
		e.requireNoSecret(err)
	})
}
