package docker

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-25 (DEVDESK-823), criterion 1, step 2 of the plan: `Stop and delete` on a running
// container stops it, then removes the SAME ID, never forced.
//
// What the code is expected to offer (removal.go, today a stub):
//
//	// StopAndRemoveContainer stops the container, then removes the same ID: `stop <id>`,
//	// then `rm <id>`, never forced. It is ONE call, so the confirmed deletion ends even if
//	// the user leaves the view, and its outcome is ONE RemovalReport (one result, Target
//	// the ID). A stop that fails removes nothing: the error of the stop is read the way
//	// ClassifyRemoval reads a removal (SingleRemoval), so a container that is already
//	// gone is RemovalGone, an engine that cannot be asked or a text nobody knows is
//	// RemovalRefused with ReasonOther. A rm that fails is read the same way: a container
//	// that started again between the two is ReasonRunning. It reuses StopContainer and
//	// RemoveContainer: the ID is validated once, before anything is built, and no argv is
//	// spelled a second time (TestEveryDestructiveFunctionIsCovered).
//	func StopAndRemoveContainer(id string) RemovalReport
//
// Every test goes through stubStrict: any argv but the declared ones fails the test and
// names it, a declared one that never runs fails it too (so "no rm after a failed stop"
// is held by declaring none). The real engine is never reached (guard G1); the seams are
// global, so none calls t.Parallel.

const stopRemoveID = "0123456789ab"

func stopArgv(id string) []string   { return []string{"stop", id} }
func removeArgv(id string) []string { return []string{"rm", id} }

// Criterion 1: `stop <id>` then `rm <id>`, in that order, on the same ID, both engines;
// no option anywhere, so no force and no `--time` either (the engine's own grace delay
// applies, like `K`). One result, removed.
func TestStopAndRemoveRunsStopThenRmOnTheSameID(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, id := range []string{stopRemoveID, strings.Repeat("ab", 32)} {
			t.Run(id[:12], func(t *testing.T) {
				s := stubStrict(t, reply{argv: stopArgv(id)}, reply{argv: removeArgv(id)})

				report := StopAndRemoveContainer(id)

				if len(s.calls) != 2 || !slices.Equal(s.calls[0].Args, stopArgv(id)) || !slices.Equal(s.calls[1].Args, removeArgv(id)) {
					t.Fatalf("invocations = %q, want exactly `stop %s` then `rm %s`", callsOf(s), id, id)
				}
				for i, c := range s.calls {
					if !c.Combined {
						t.Errorf("invocation %d did not ask for combined output: the engine's reason would be lost", i)
					}
					for _, w := range c.Args[:len(c.Args)-1] {
						if strings.HasPrefix(w, "-") {
							t.Errorf("invocation %d carries the option %q: no force, no timeout, nothing", i, w)
						}
					}
				}
				if len(report) != 1 {
					t.Fatalf("report has %d results, want one for the one container: %+v", len(report), report)
				}
				if r := report[0]; r.Target != id || r.Status != RemovalRemoved || r.Err != nil {
					t.Errorf("result = %+v, want %q removed with no error", r, id)
				}
				if report.Summary() != "1 removed" {
					t.Errorf("Summary() = %q, want %q", report.Summary(), "1 removed")
				}
			})
		}
	})
}

// A stop that fails removes nothing. The strict stub declares no `rm`: one that runs
// fails the test and names it.
//
// Texts: the engines' own words for a container that does not exist (the OC-22 probes,
// removal_probe_test.go; `stop` says the same as `rm` there), the refusal of an engine
// that cannot be asked (the probed answer of unavailable_probe_test.go, DK-A and PM-A),
// and a text nobody knows. "Could not tell" is never read as "already gone".
func TestAFailedStopRemovesNothing(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		cases := []struct {
			name       string
			out        string
			wantStatus RemovalStatus
			wantReason RefusalReason
		}{
			{"the container is already gone", goneText(shape.Name, "container", stopRemoveID), RemovalGone, ""},
			{"the engine cannot be asked", unavailableText(shape.Name), RemovalRefused, ReasonOther},
			{"a text nobody knows", "Error: something new happened\n", RemovalRefused, ReasonOther},
			{"no text at all", "", RemovalRefused, ReasonOther},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				s := stubStrict(t, reply{argv: stopArgv(stopRemoveID), out: tt.out, err: errors.New("exit status 1")})

				report := StopAndRemoveContainer(stopRemoveID)

				if len(s.calls) != 1 {
					t.Fatalf("invocations = %q, want only the `stop`: a container that could not be stopped is not removed", callsOf(s))
				}
				if len(report) != 1 {
					t.Fatalf("report has %d results, want one: %+v", len(report), report)
				}
				if r := report[0]; r.Target != stopRemoveID || r.Status != tt.wantStatus || r.Reason != tt.wantReason {
					t.Errorf("result = %+v, want %q %q (reason %q)", r, stopRemoveID, tt.wantStatus, tt.wantReason)
				}
				if tt.wantStatus == RemovalRefused && report[0].Err == nil {
					t.Error("the refusal carries no error: the log would have nothing to say")
				}
			})
		}
	})
}

// unavailableText is the answer of an engine that is not running, as probed
// (unavailable_probe_test.go: docker 29 and 28 DK-A, podman remote PM-A on the other).
func unavailableText(tool string) string {
	if tool == engine.Docker {
		return "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?\n"
	}
	return "Cannot connect to Podman. Please verify your connection to the Linux system using `podman system connection list`, " +
		"or try `podman machine init` and `podman machine start` to manage a new Linux VM\n" +
		"Error: unable to connect to Podman socket: Get \"http://d/v6.2.1/libpod/_ping\": dial unix /run/user/2000/podman/podman.sock: connect: no such file or directory\n"
}

// The container stopped, then went away before its turn (a container started with
// --rm does, and somebody else may have removed it): the `rm` says so, and the answer is
// "already gone", an information. Nothing is retried.
func TestAContainerGoneBetweenStopAndRmIsReportedGone(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		s := stubStrict(t,
			reply{argv: stopArgv(stopRemoveID)},
			reply{argv: removeArgv(stopRemoveID), out: goneText(shape.Name, "container", stopRemoveID), err: errors.New("exit status 1")})

		report := StopAndRemoveContainer(stopRemoveID)

		if len(s.calls) != 2 {
			t.Fatalf("invocations = %q, want `stop` then `rm`, once each", callsOf(s))
		}
		if len(report) != 1 || report[0].Status != RemovalGone || report[0].Reason != "" {
			t.Errorf("report = %+v, want one RemovalGone with no reason", report)
		}
	})
}

// The container started again between the stop and the rm (a restart policy, a
// supervisor): the engine refuses the `rm` and DevDesk reports "running". It does not
// stop it a second time, and it never retries with `-f`: the strict stub declares one
// `stop` and one `rm`.
func TestARmRefusedAfterTheStopIsReportedRunning(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		s := stubStrict(t,
			reply{argv: stopArgv("2222bbbbbbbb")},
			reply{argv: removeArgv("2222bbbbbbbb"), out: runningRefusal[shape.Name], err: errors.New("exit status 1")})

		report := StopAndRemoveContainer("2222bbbbbbbb")

		if len(s.calls) != 2 {
			t.Fatalf("invocations = %q, want `stop` then `rm`, no retry and no force", callsOf(s))
		}
		if len(report) != 1 || report[0].Status != RemovalRefused || report[0].Reason != ReasonRunning || report[0].Err == nil {
			t.Fatalf("report = %+v, want one refusal (running) that carries its error", report)
		}
		if got := report.Summary(); got != "1 not removed (running)" {
			t.Errorf("Summary() = %q, want %q", got, "1 not removed (running)")
		}
	})
}

// The same on the real answers: the recorded refusal of `rm` of a running container
// (containers/rm-running, docker 29.10.1 and 28.5.2, podman 6.2.1 and 5.8.11), served
// after a `stop`, reads as running on every recorded configuration.
func TestARecordedRunningRefusalAfterTheStopIsReportedRunning(t *testing.T) {
	all, err := enginefixture.All()
	if err != nil {
		t.Fatalf("loading the recorded fixtures: %v", err)
	}
	n := 0
	for _, f := range all {
		if f.Campaign != "containers" || f.Scenario != "rm-running" || strings.HasPrefix(f.Meta.Record, "skip") {
			continue
		}
		n++
		t.Run(f.Name, func(t *testing.T) {
			usingEngine(t, string(f.Meta.Tool))
			id := f.Exchange.Argv[len(f.Exchange.Argv)-1]
			stubStrict(t,
				reply{argv: stopArgv(id)},
				reply{argv: removeArgv(id), out: string(f.Exchange.Stdout) + string(f.Exchange.Stderr), err: errors.New("exit status 1")})

			report := StopAndRemoveContainer(id)

			if len(report) != 1 || report[0].Status != RemovalRefused || report[0].Reason != ReasonRunning {
				t.Errorf("report = %+v, want one refusal for reason %q", report, ReasonRunning)
			}
		})
	}
	if n == 0 {
		t.Error("no recorded containers/rm-running fixture: the refusal is not tested on a real answer")
	}
}

// A malformed ID builds nothing: no `stop`, no `rm` (guard G3 at run time, S1). The
// report says so with one refusal that wraps the sentinel.
func TestStopAndRemoveRefusesAMalformedIDBeforeAnyArgv(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, bad := range badIDs[kindContainer] {
			t.Run(bad.label, func(t *testing.T) {
				s := stubStrict(t) // no declared argv: any invocation fails the test

				report := StopAndRemoveContainer(bad.id)

				if len(s.calls) != 0 {
					t.Errorf("StopAndRemoveContainer(%q) reached the runner with %q, want nothing built", bad.id, s.lastArgs())
				}
				if len(report) != 1 {
					t.Fatalf("report has %d results, want one refusal: %+v", len(report), report)
				}
				r := report[0]
				if r.Status != RemovalRefused || r.Reason != ReasonOther || !errors.Is(r.Err, ErrInvalidContainerID) {
					t.Errorf("result = %+v, want a refusal (other) wrapping %v", r, ErrInvalidContainerID)
				}
			})
		}
	})
}

// The signature is the id and nothing else: no force flag, no timeout, no list of
// options (C2). The result is the one report a view turns into the footer line.
func TestStopAndRemoveTakesOnlyTheIDAndReturnsAReport(t *testing.T) {
	typ := reflect.TypeOf(StopAndRemoveContainer)

	if typ.NumIn() != 1 || typ.In(0).Kind() != reflect.String {
		t.Errorf("StopAndRemoveContainer takes %v, want exactly one string (the identifier): no option, no force flag, no timeout", typ)
	}
	if typ.NumOut() != 1 || typ.Out(0) != reflect.TypeOf(RemovalReport(nil)) {
		t.Errorf("StopAndRemoveContainer returns %v, want one RemovalReport", typ)
	}
}
