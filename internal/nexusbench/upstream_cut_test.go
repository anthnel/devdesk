package nexusbench

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The tests of the upstream cut (DEVDESK-534, NX-02b, step 2):
//
//	CutUpstream(ctx, runner, clock, p, out) error
//
// against the scripted kubectl of upstream_test.go (upstreamKubectl, whose rounds
// play before the scale is accepted and after it) and the fake clock of
// clock_test.go. No real kubectl, no real wait, nothing on a cluster.
//
// Like EnsureUpstream, CutUpstream runs no preflight: the command runs OpCut
// first. The kubectl calls the tests assume (arguments after "kubectl", without
// --context; "--request-timeout=<d>" is ignored):
//
//	get deployment bench-upstream -n <ns> -o json        (the read)
//	scale deployment/bench-upstream --replicas=0 -n <ns> (the only write)
//	get pods -n <ns> -l app.kubernetes.io/name=bench-upstream -o json
//	                                                     (the wait, once per round)
//
// Choices of the tests, to settle with the code:
//   - It reads first. Ready or Starting: one scale to zero, then the wait. Cut:
//     no write, and the wait still runs (a pod of a previous cut may be
//     terminating). Missing: an error, no write, no wait.
//   - The wait lists the pods of the upstream, sleeps upstreamPollInterval
//     between two rounds, and ends on the first round with NO pod at all: a pod
//     that is terminating counts (its volume is still mounted).
//   - It gives up at p.Timeout (within one interval) with a Deadline that names
//     the pods still there and NEXUS_BENCH_TIMEOUT.
//   - A failed scale is classified (classifyKubectl) and never retried.
//   - A custom upstream URL is refused before any call. A cancelled context is
//     returned as is. A nil writer is accepted.
//
// What step 0 (H-CUTDELAY) may change: nothing here. The wait is on the
// cluster, not on the proxy.

// goingPod is a pod of the upstream that is shutting down.
func goingPod(name string) upPod {
	return upPod{fakePod: fakePod{name: name, phase: "Running", scheduled: "True", running: true, ready: true}, terminating: true}
}

// goingRound is the upstream at zero replicas with the pods still shutting down.
func goingRound(pods ...upPod) upstreamRound {
	return upstreamRound{deployment: true, replicas: 0, pods: pods, claim: "Bound"}
}

func (e *upstreamEnv) cut(ctx context.Context) error {
	e.t.Helper()
	return CutUpstream(ctx, e.kube, e.clk, e.p, &e.out)
}

// requireEverySleepIsAPoll checks the wait slept upstreamPollInterval each time.
func (e *upstreamEnv) requireEverySleepIsAPoll() {
	e.t.Helper()
	for i, d := range e.clk.sleeps {
		if d != upstreamPollInterval {
			e.t.Errorf("sleep #%d lasts %v, want %v", i+1, d, upstreamPollInterval)
		}
	}
}

// requireOnlyUpstreamScales checks the write journal: scale on bench-upstream
// and nothing else (never Nexus, never a delete).
func (e *upstreamEnv) requireOnlyUpstreamScales() {
	e.t.Helper()
	for _, w := range e.kube.writes {
		if !strings.HasPrefix(w, "scale deployment/"+upstreamName+" ") {
			e.t.Errorf("write %q is not a scale of %s", w, upstreamName)
		}
	}
}

// ---- the nominal paths ------------------------------------------------------

func TestCutUpstreamScalesToZeroThenWaitsForNoPod(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(),
		[]upstreamRound{roundOf(1, readyPod())},
		[]upstreamRound{
			goingRound(goingPod(upstreamPodName)),
			goingRound(goingPod(upstreamPodName)),
			cutRound(),
		})
	if err := e.cut(context.Background()); err != nil {
		t.Fatalf("CutUpstream: %v", err)
	}
	e.requireOneScale("0")
	wantWrite := fmt.Sprintf("scale deployment/%s --replicas=0 -n %s", upstreamName, e.p.Namespace)
	if got := e.kube.writes[0]; !strings.HasPrefix(got, wantWrite) {
		t.Errorf("write = %q, want it to start with %q", got, wantWrite)
	}
	if got := len(e.clk.sleeps); got != 2 {
		t.Errorf("slept %d times, want 2: one per round that still had a pod (%v)", got, e.clk.sleeps)
	}
	e.requireEverySleepIsAPoll()
	e.kube.requireNoPreflight()
	e.requireOnlyUpstreamScales()
	e.requireNoSecret(nil)

	// The scale follows a read of the Deployment and precedes the pod reads.
	scale := indexOfLine(e.kube.lines, "scale ")
	read := indexOfLine(e.kube.lines, "get deployment")
	if read < 0 || scale < read {
		t.Errorf("the scale is not preceded by a read of the Deployment: %q", e.kube.lines)
	}
	if pods := indexOfLine(e.kube.lines[scale+1:], "get pods"); pods < 0 {
		t.Errorf("no pod read after the scale: %q", e.kube.lines)
	}
	if !strings.Contains(e.out.String(), "upstream: ") {
		t.Errorf("progress lines %q do not carry the upstream: prefix", e.out.String())
	}
}

func TestCutUpstreamDoesNotSleepWhenNoPodIsLeftAtTheFirstRound(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{cutRound()})
	if err := e.cut(context.Background()); err != nil {
		t.Fatalf("CutUpstream: %v", err)
	}
	e.requireOneScale("0")
	if len(e.clk.sleeps) != 0 {
		t.Errorf("slept %v although the first round after the scale had no pod", e.clk.sleeps)
	}
}

func TestCutUpstreamAlsoCutsAnUpstreamThatIsStarting(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(),
		[]upstreamRound{roundOf(1, creatingPod())},
		[]upstreamRound{goingRound(goingPod(upstreamPodName)), cutRound()})
	if err := e.cut(context.Background()); err != nil {
		t.Fatalf("CutUpstream: %v", err)
	}
	e.requireOneScale("0")
	if len(e.clk.sleeps) != 1 {
		t.Errorf("slept %v, want one sleep", e.clk.sleeps)
	}
}

func TestCutUpstreamIsIdempotent(t *testing.T) {
	t.Run("already cut with no pod: no write, no wait", func(t *testing.T) {
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{cutRound()}, nil)
		if err := e.cut(context.Background()); err != nil {
			t.Fatalf("CutUpstream: %v", err)
		}
		e.requireNoWrite()
		if len(e.clk.sleeps) != 0 {
			t.Errorf("waited %v for an upstream that was already cut", e.clk.sleeps)
		}
		e.kube.requireNoPreflight()
	})
	t.Run("already cut with a pod that is terminating: no write, the wait still runs", func(t *testing.T) {
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{
			goingRound(goingPod(upstreamPodName)),
			goingRound(goingPod(upstreamPodName)),
			cutRound(),
		}, nil)
		if err := e.cut(context.Background()); err != nil {
			t.Fatalf("CutUpstream: %v", err)
		}
		e.requireNoWrite()
		if got := len(e.clk.sleeps); got != 2 {
			t.Errorf("slept %d times, want 2 (%v)", got, e.clk.sleeps)
		}
		e.requireEverySleepIsAPoll()
	})
	t.Run("twice in a row writes once", func(t *testing.T) {
		// The first call scales; the second reads the same cluster, now cut.
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{cutRound()})
		if err := e.cut(context.Background()); err != nil {
			t.Fatalf("first CutUpstream: %v", err)
		}
		e.kube.before = []upstreamRound{cutRound()}
		e.kube.scaledAt = -1
		if err := e.cut(context.Background()); err != nil {
			t.Fatalf("second CutUpstream: %v", err)
		}
		e.requireOneScale("0")
	})
}

func TestCutUpstreamAcceptsANilWriter(t *testing.T) {
	p := upstreamParams()
	e := newUpstreamEnv(t, p, []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{goingRound(goingPod(upstreamPodName)), cutRound()})
	if err := CutUpstream(context.Background(), e.kube, e.clk, p, nil); err != nil {
		t.Fatalf("CutUpstream with a nil writer: %v", err)
	}
}

func TestCutUpstreamOnlyScalesTheUpstream(t *testing.T) {
	// The fake reports an unexpected write (delete, apply, a scale of nexus) and
	// refuses it; the journal is checked as well, on the paths that wait.
	e := newUpstreamEnv(t, upstreamParams(),
		[]upstreamRound{roundOf(1, readyPod())},
		[]upstreamRound{goingRound(goingPod(upstreamPodName)), cutRound()})
	if err := e.cut(context.Background()); err != nil {
		t.Fatalf("CutUpstream: %v", err)
	}
	e.requireOnlyUpstreamScales()
	for _, l := range e.kube.lines {
		for _, banned := range []string{"nexus-data", "deployment/nexus", "delete", "apply", "rollout", "exec"} {
			if strings.Contains(l, banned) {
				t.Errorf("call %q contains %q: the cut touches the upstream Deployment only", l, banned)
			}
		}
	}
}

// ---- failures ---------------------------------------------------------------

func TestCutUpstreamFailsWhenTheDeploymentIsMissing(t *testing.T) {
	e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{absentRound()}, nil)
	err := e.cut(context.Background())
	if err == nil {
		t.Fatal("CutUpstream succeeded without a Deployment")
	}
	_ = requireCleanError(t, err)
	requireMentions(t, err, []string{upstreamName, "nexus-bench:up"}, nil)
	e.requireNoWrite()
	if len(e.clk.sleeps) != 0 {
		t.Errorf("waited %v for a Deployment that is not there", e.clk.sleeps)
	}
	if indexOfLine(e.kube.lines, "get pods") >= 0 {
		t.Errorf("pods were read for a Deployment that is not there: %q", e.kube.lines)
	}
}

func TestCutUpstreamDeadlineNamesThePodThatStays(t *testing.T) {
	tests := []struct {
		name    string
		forever upstreamRound
		mention []string
	}{
		{"one pod never leaves", goingRound(goingPod(upstreamPodName)), []string{upstreamPodName, envTimeout}},
		{
			"two pods, both named",
			goingRound(goingPod(upstreamPodName), goingPod("bench-upstream-6f7d9c8b5-zz9x8")),
			[]string{upstreamPodName, "bench-upstream-6f7d9c8b5-zz9x8", envTimeout},
		},
		{"a pod that is not terminating comes back", roundOf(0, readyPod()), []string{upstreamPodName, envTimeout}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := upstreamParams()
			e := newUpstreamEnv(t, p, []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{tt.forever})
			err := e.cut(context.Background())
			be := requireDeployKind(t, err, Deadline)
			requireMentions(t, err, tt.mention, []string{"Nexus did not answer", "was not ready"})
			if !strings.Contains(be.Check, "kubectl") {
				t.Errorf("Check %q does not name a kubectl command", be.Check)
			}
			e.requireOneScale("0")
			e.requireEverySleepIsAPoll()
			if el := e.clk.elapsed(); el < p.Timeout || el >= p.Timeout+upstreamPollInterval {
				t.Errorf("gave up after %v, want %v within one interval (%v)", el, p.Timeout, upstreamPollInterval)
			}
			e.requireNoSecret(err)
		})
	}
}

func TestCutUpstreamDeadlineHonorsTheTimeout(t *testing.T) {
	p := upstreamParams()
	p.Timeout = 30 * time.Second
	e := newUpstreamEnv(t, p, []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{goingRound(goingPod(upstreamPodName))})
	err := e.cut(context.Background())
	_ = requireDeployKind(t, err, Deadline)
	if el := e.clk.elapsed(); el < p.Timeout || el >= p.Timeout+upstreamPollInterval {
		t.Errorf("gave up after %v, want %v within one interval", el, p.Timeout)
	}
}

func TestCutUpstreamFailsWhenTheScaleFails(t *testing.T) {
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
			e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{cutRound()})
			e.kube.scaleReply = &fakeReply{stderr: tt.stderr, err: errFakeExit1}
			err := e.cut(context.Background())
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
			// The read of the state may list the pods; the wait comes after the scale.
			if scale := indexOfLine(e.kube.lines, "scale "); scale < 0 || indexOfLine(e.kube.lines[scale+1:], "get pods") >= 0 {
				t.Errorf("the wait started after a failed scale: %q", e.kube.lines)
			}
			e.requireNoSecret(err)
		})
	}
}

func TestCutUpstreamFailsWhenAReadFails(t *testing.T) {
	t.Run("the Deployment", func(t *testing.T) {
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{cutRound()})
		e.kube.deploymentReply = &fakeReply{stderr: "Unable to connect to the server: dial tcp: i/o timeout token=" + fakeSecret, err: errFakeExit1}
		err := e.cut(context.Background())
		_ = requireDeployKind(t, err, ClusterUnreachable)
		e.requireNoWrite()
		e.requireNoSecret(err)
	})
	t.Run("the pods, during the wait", func(t *testing.T) {
		// Already cut: the read of the state lists no pod, the wait does.
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{cutRound()}, nil)
		e.kube.podsReply = &fakeReply{stderr: "Error from server (Forbidden): pods is forbidden token=" + fakeSecret, err: errFakeExit1}
		err := e.cut(context.Background())
		_ = requireDeployKind(t, err, Forbidden)
		e.requireNoWrite()
		if len(e.clk.sleeps) != 0 {
			t.Errorf("waited %v after a failed read", e.clk.sleeps)
		}
		e.requireNoSecret(err)
	})
}

// ---- the guards -------------------------------------------------------------

func TestCutUpstreamRefusesACustomUpstream(t *testing.T) {
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
			e := newUpstreamEnv(t, p, []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{cutRound()})
			err := e.cut(context.Background())
			if err == nil {
				t.Fatal("CutUpstream accepted an upstream that is not the bench's")
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

func TestCutUpstreamReturnsACancelledContextAsIs(t *testing.T) {
	requireAsIs := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		var be *BenchError
		if errors.As(err, &be) {
			t.Errorf("a cancelled context is a BenchError (%v), want it as is", be)
		}
	}
	t.Run("cancelled before the start: nothing is read or written", func(t *testing.T) {
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{cutRound()})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		requireAsIs(t, e.cut(ctx))
		e.requireNoWrite()
		if len(e.kube.lines) != 0 {
			t.Errorf("kubectl was called with a cancelled context: %q", e.kube.lines)
		}
	})
	t.Run("cancelled during the wait", func(t *testing.T) {
		e := newUpstreamEnv(t, upstreamParams(), []upstreamRound{roundOf(1, readyPod())}, []upstreamRound{goingRound(goingPod(upstreamPodName))})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.clk.onSleep = func(n int) {
			if n == 2 {
				cancel()
			}
		}
		requireAsIs(t, e.cut(ctx))
		e.requireOneScale("0")
		if len(e.clk.sleeps) != 2 {
			t.Errorf("slept %d times, want it to stop at 2", len(e.clk.sleeps))
		}
	})
}

// indexOfLine is the index of the first recorded call that starts with prefix,
// -1 when there is none.
func indexOfLine(lines []string, prefix string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}
	return -1
}
