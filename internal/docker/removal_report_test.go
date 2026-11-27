package docker

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-22 (DEVDESK-820), requirements S2 and C2: what the user destroys is what the
// user previewed, one identifier at a time, never forced, never "everything the
// engine finds at that moment".
//
// This file is the docker half of the story: the batch removal that replaces
// `container prune`, `image prune` and `volume prune`, its structured report, and
// the recognition of what the engine said. The views' half is in
// internal/ui/containers and internal/ui/oci_resources; the static half (no force
// option, no prune argv) is in internal/engineguard.
//
// What the code is expected to offer (none of it exists yet, so this file does
// not compile today):
//
//	type RemovalStatus string // RemovalRemoved, RemovalGone, RemovalRefused
//	type RefusalReason string // ReasonRunning, ReasonInUse, ReasonOther
//
//	type RemovalResult struct {
//		Target string        // the identifier as the caller passed it
//		Status RemovalStatus
//		Reason RefusalReason // set when Status is RemovalRefused
//		Err    error         // the engine's error, nil when Status is RemovalRemoved
//	}
//
//	// RemovalReport has one result per target, in the order of the targets.
//	type RemovalReport []RemovalResult
//	func (r RemovalReport) Count(s RemovalStatus) int
//
//	// One argv per target: `rm <id>`, `rmi <id>`, `volume rm <name>`. No option, ever.
//	// A malformed target is refused (ReasonOther, the Err wraps the ErrInvalid...
//	// sentinel) without building anything, and the batch goes on with the next one.
//	// A refusal of the engine does not stop the batch either.
//	func RemoveContainers(ids []string) RemovalReport
//	func RemoveImages(ids []string) RemovalReport
//	func RemoveVolumes(names []string) RemovalReport
//
//	// ClassifyRemoval reads what a Remove* function returned: nil is
//	// RemovalRemoved; an engine that says the thing does not exist (any of the
//	// recorded and unrecorded wordings below) is RemovalGone, which is an
//	// information and not a failure; an engine that refuses is RemovalRefused with
//	// the reason, ReasonOther when the text is not one it knows.
//	func ClassifyRemoval(err error) (RemovalStatus, RefusalReason)
//
// And, as a consequence of C2, RemoveContainer and RemoveImage lose their `force`
// parameter (RemoveVolume, RemoveNetwork and StopContainer already have none):
// TestNoRemovingFunctionTakesAForceFlag.
//
// Every test here goes through stubStrict or useFixtures, so no engine is reached
// (guard G1); the seams are global, so none calls t.Parallel.

// batchKind is one of the three batch removals, with the argv it must send.
type batchKind struct {
	name     string
	targets  []string
	argv     func(target string) []string
	remove   func(targets []string) RemovalReport
	sentinel error
}

var batchKinds = []batchKind{
	{
		name:     "containers",
		targets:  []string{"1111aaaaaaaa", "2222bbbbbbbb", "3333" + strings.Repeat("c", 60)},
		argv:     func(id string) []string { return []string{"rm", id} },
		remove:   RemoveContainers,
		sentinel: ErrInvalidContainerID,
	},
	{
		name:     "images",
		targets:  []string{"1111aaaaaaaa", "sha256:2222bbbbbbbb", "3333cccccccc"},
		argv:     func(id string) []string { return []string{"rmi", id} },
		remove:   RemoveImages,
		sentinel: ErrInvalidImageID,
	},
	{
		name:     "volumes",
		targets:  []string{"scratch", "spare-1", "old_cache.v2"},
		argv:     func(name string) []string { return []string{"volume", "rm", name} },
		remove:   RemoveVolumes,
		sentinel: ErrInvalidVolumeName,
	},
}

// A refusal as each engine words it for a running container: the recorded
// fixtures containers/rm-running (docker 28.5.2 and 29.10.1, podman 5.8.11 and
// 6.2.1) say these, with the identifier of the recording.
var runningRefusal = map[string]string{
	engine.Docker: `Error response from daemon: cannot remove container "2222bbbbbbbb": container is running: stop the container before removing or force remove` + "\n",
	engine.Podman: `Error: cannot remove container 2222bbbbbbbb as it is running - running or paused containers cannot be removed without force: container state improper` + "\n",
}

// Criterion 1 and 5: a batch is one argv per target, in the order previewed, each
// exactly the verb and the identifier. A prune is not one of them, and an option
// (-f, --force, -v) would make the argv longer than the table says.
func TestABatchSendsOneBareArgvPerTargetInOrder(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, k := range batchKinds {
			t.Run(k.name, func(t *testing.T) {
				replies := make([]reply, 0, len(k.targets))
				for _, target := range k.targets {
					replies = append(replies, reply{argv: k.argv(target)})
				}
				s := stubStrict(t, replies...)

				report := k.remove(slices.Clone(k.targets))

				if len(s.calls) != len(k.targets) {
					t.Fatalf("%s ran %d invocations for %d targets, want one each: %q", shape.Name, len(s.calls), len(k.targets), callsOf(s))
				}
				for i, target := range k.targets {
					if got, want := s.calls[i].Args, k.argv(target); !slices.Equal(got, want) {
						t.Errorf("invocation %d = %q, want %q", i, got, want)
					}
					if !s.calls[i].Combined {
						t.Errorf("invocation %d did not ask for combined output: the engine's reason would be lost", i)
					}
				}
				if len(report) != len(k.targets) {
					t.Fatalf("report has %d results, want one per target (%d): %+v", len(report), len(k.targets), report)
				}
				for i, r := range report {
					if r.Target != k.targets[i] || r.Status != RemovalRemoved || r.Err != nil {
						t.Errorf("result %d = %+v, want %q removed with no error", i, r, k.targets[i])
					}
				}
				if got := report.Count(RemovalRemoved); got != len(k.targets) {
					t.Errorf("Count(RemovalRemoved) = %d, want %d", got, len(k.targets))
				}
			})
		}
	})
}

func callsOf(s *stubRunner) [][]string {
	out := make([][]string, 0, len(s.calls))
	for _, c := range s.calls {
		out = append(out, c.Args)
	}
	return out
}

// Criterion 3: the engine refuses a running container (it started between the
// preview and the confirmation). The batch records the refusal with its reason,
// does not retry with a force option, and goes on with the next target.
func TestABatchKeepsGoingAfterTheEngineRefusesOne(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		ids := []string{"1111aaaaaaaa", "2222bbbbbbbb", "3333cccccccc"}
		s := stubStrict(t,
			reply{argv: []string{"rm", ids[0]}},
			reply{argv: []string{"rm", ids[1]}, out: runningRefusal[shape.Name], err: errors.New("exit status 1")},
			reply{argv: []string{"rm", ids[2]}},
		)

		report := RemoveContainers(ids)

		if len(s.calls) != 3 {
			t.Fatalf("%d invocations, want 3 (a refusal does not stop the batch, and is not retried): %q", len(s.calls), callsOf(s))
		}
		want := []RemovalStatus{RemovalRemoved, RemovalRefused, RemovalRemoved}
		for i, r := range report {
			if r.Status != want[i] {
				t.Errorf("result %d (%s) = %q, want %q", i, r.Target, r.Status, want[i])
			}
		}
		if report[1].Reason != ReasonRunning {
			t.Errorf("the refusal's reason = %q, want %q: the report has to say why", report[1].Reason, ReasonRunning)
		}
		if report[1].Err == nil {
			t.Error("the refusal carries no error: the log would have nothing to say")
		}
		if report.Count(RemovalRemoved) != 2 || report.Count(RemovalRefused) != 1 || report.Count(RemovalGone) != 0 {
			t.Errorf("counts = removed %d, refused %d, gone %d, want 2, 1, 0",
				report.Count(RemovalRemoved), report.Count(RemovalRefused), report.Count(RemovalGone))
		}
	})
}

// Criterion 6: "No such container" is not an error, it is a removal somebody else
// already did. It is counted apart (RemovalGone), with no reason.
func TestAnItemThatIsAlreadyGoneIsNotARefusal(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		ids := []string{"1111aaaaaaaa", "2222bbbbbbbb"}
		s := stubStrict(t,
			reply{argv: []string{"rm", ids[0]}, out: goneText(shape.Name, "container", ids[0]), err: errors.New("exit status 1")},
			reply{argv: []string{"rm", ids[1]}},
		)

		report := RemoveContainers(ids)

		if len(s.calls) != 2 {
			t.Fatalf("%d invocations, want 2", len(s.calls))
		}
		if report[0].Status != RemovalGone || report[0].Reason != "" {
			t.Errorf("result 0 = %+v, want RemovalGone with no reason", report[0])
		}
		if report.Count(RemovalGone) != 1 || report.Count(RemovalRemoved) != 1 || report.Count(RemovalRefused) != 0 {
			t.Errorf("counts = gone %d, removed %d, refused %d, want 1, 1, 0",
				report.Count(RemovalGone), report.Count(RemovalRemoved), report.Count(RemovalRefused))
		}
	})
}

// A target that is not an identifier builds no argv (guard G3 at run time), and
// it neither stops the batch nor goes unreported: an option-like string must
// never be the word that makes it into an argv.
func TestABatchRefusesAMalformedTargetWithoutBuildingAnArgv(t *testing.T) {
	bad := []string{"-f", "--all", "", "has space", "$(id)"}
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, k := range batchKinds {
			t.Run(k.name, func(t *testing.T) {
				first, last := k.targets[0], k.targets[1]
				targets := append(append([]string{first}, bad...), last)
				s := stubStrict(t, reply{argv: k.argv(first)}, reply{argv: k.argv(last)})

				report := k.remove(targets)

				if len(s.calls) != 2 {
					t.Fatalf("%d invocations, want only the 2 valid ones: %q", len(s.calls), callsOf(s))
				}
				for _, c := range s.calls {
					for _, w := range c.Args {
						if slices.Contains(bad, w) {
							t.Errorf("a malformed target reached an argv: %q", c.Args)
						}
					}
				}
				if len(report) != len(targets) {
					t.Fatalf("report has %d results, want one per target (%d)", len(report), len(targets))
				}
				for i, r := range report[1 : len(report)-1] {
					if r.Status != RemovalRefused || r.Reason != ReasonOther || !errors.Is(r.Err, k.sentinel) {
						t.Errorf("result for %q = %+v, want refused (other) wrapping %v", bad[i], r, k.sentinel)
					}
				}
				if report.Count(RemovalRemoved) != 2 {
					t.Errorf("Count(RemovalRemoved) = %d, want the 2 valid ones", report.Count(RemovalRemoved))
				}
			})
		}
	})
}

func TestAnEmptyBatchRunsNothingAndReportsNothing(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		s := stubStrict(t) // no declared argv: any invocation fails the test
		for _, k := range batchKinds {
			if got := k.remove(nil); len(got) != 0 {
				t.Errorf("%s: an empty batch reported %+v", k.name, got)
			}
		}
		if len(s.calls) != 0 {
			t.Errorf("an empty batch ran %q", callsOf(s))
		}
	})
}

// Criterion 5, in the signatures: nothing that destroys takes a flag that would
// let a caller ask for force. `RemoveContainer(id, true)` and `RemoveImage(id,
// true)` are the two that did; the contracts that need to remove a running
// container of their own run (cleanupNow, contract_support_test.go) go through a
// helper in the test files, not through production code.
func TestNoRemovingFunctionTakesAForceFlag(t *testing.T) {
	for name, fn := range map[string]any{
		"StopContainer":   StopContainer,
		"RemoveContainer": RemoveContainer,
		"RemoveImage":     RemoveImage,
		"RemoveVolume":    RemoveVolume,
		"RemoveNetwork":   RemoveNetwork,
	} {
		typ := reflect.TypeOf(fn)
		if typ.NumIn() != 1 || typ.In(0).Kind() != reflect.String {
			t.Errorf("%s takes %v, want exactly one string (the identifier): no option, no force flag", name, typ)
		}
	}
}

// ── What the engine said ─────────────────────────────────────────────────────

// removeOne calls the single-target function of a kind, through the production
// wrapper, so the error is the one a view receives.
func removeOne(kind string, target string) error {
	switch kind {
	case "container":
		return RemoveContainer(target)
	case "image":
		return RemoveImage(target)
	case "volume":
		return RemoveVolume(target)
	case "network":
		return RemoveNetwork(target)
	}
	panic("unknown kind " + kind)
}

func argvOne(kind, target string) []string {
	switch kind {
	case "container":
		return []string{"rm", target}
	case "image":
		return []string{"rmi", target}
	case "volume":
		return []string{"volume", "rm", target}
	}
	return []string{"network", "rm", target}
}

func targetOf(kind string) string {
	switch kind {
	case "volume":
		return "pgdata"
	case "network":
		return "backend"
	}
	return "0123456789ab"
}

// goneText is what an engine says for a missing thing. The two engines do not use
// the same words, and neither uses the one `inspect` uses.
//
// NOT RECORDED: unlike the refusals below, no fixture holds the answer of `rm`,
// `rmi`, `volume rm` or `network rm` for something that does not exist (the
// recorded `inspect-absent` is checked separately, below). These are the wordings
// of docker 2x and podman 5/6 as known from their sources, and must be recorded
// (a `*-absent` scenario per kind, OC-09 style) before they are relied on: until
// then, a change of wording on an engine upgrade makes the item a refusal
// (ReasonOther), which is the safe direction.
func goneText(tool, kind, target string) string {
	switch tool + "/" + kind {
	case "docker/container":
		return "Error response from daemon: No such container: " + target + "\n"
	case "podman/container":
		return `Error: no container with name or ID "` + target + `" found: no such container` + "\n"
	case "docker/image":
		return "Error response from daemon: No such image: " + target + "\n"
	case "podman/image":
		return "Error: " + target + ": image not known\n"
	case "docker/volume":
		return "Error response from daemon: get " + target + ": no such volume\n"
	case "podman/volume":
		return `Error: no volume with name "` + target + `" found: no such volume` + "\n"
	case "docker/network":
		return "Error response from daemon: network " + target + " not found\n"
	}
	return "Error: unable to find network with name or ID " + target + ": network not found\n"
}

type classifyCase struct {
	name       string
	text       string
	kind       string
	wantStatus RemovalStatus
	wantReason RefusalReason
}

// Criteria 3 and 6: the engine's own words, read through the production wrappers.
func TestClassifyRemovalReadsWhatEachEngineSays(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		var cases []classifyCase
		for _, kind := range []string{"container", "image", "volume", "network"} {
			cases = append(cases, classifyCase{
				name: "already gone: " + kind, text: goneText(shape.Name, kind, targetOf(kind)), kind: kind,
				wantStatus: RemovalGone,
			})
		}
		cases = append(cases,
			classifyCase{"a running container", runningRefusal[shape.Name], "container", RemovalRefused, ReasonRunning},
			// The trap: the engine itself is missing. `not found` is in the text,
			// and nothing has been removed, so it must never read as "already gone".
			classifyCase{"the engine is not installed", `exec: "` + shape.Binary + `": executable file not found in $PATH`, "container", RemovalRefused, ReasonOther},
			classifyCase{"the socket is not reachable", "permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock", "container", RemovalRefused, ReasonOther},
			classifyCase{"the daemon is down", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?", "image", RemovalRefused, ReasonOther},
			classifyCase{"no text at all", "", "volume", RemovalRefused, ReasonOther},
		)
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				target := targetOf(tt.kind)
				stubStrict(t, reply{argv: argvOne(tt.kind, target), out: tt.text, err: errors.New("exit status 1")})

				err := removeOne(tt.kind, target)
				if err == nil {
					t.Fatalf("the stubbed engine failed but Remove%s returned nil", tt.kind)
				}
				gotStatus, gotReason := ClassifyRemoval(err)

				if gotStatus != tt.wantStatus || gotReason != tt.wantReason {
					t.Errorf("ClassifyRemoval(%q) = (%q, %q), want (%q, %q)", err, gotStatus, gotReason, tt.wantStatus, tt.wantReason)
				}
			})
		}
	})
}

func TestClassifyRemovalOfNilIsRemoved(t *testing.T) {
	if status, reason := ClassifyRemoval(nil); status != RemovalRemoved || reason != "" {
		t.Errorf("ClassifyRemoval(nil) = (%q, %q), want (%q, \"\")", status, reason, RemovalRemoved)
	}
}

// removalScenarios are the recorded answers (internal/enginefixture/fixtures) the
// classification has to read, on every recorded configuration of both engines:
// docker 28.5.2 and 29.10.1, podman 5.8.11 and 6.2.1.
var removalScenarios = []struct {
	campaign, scenario, kind string
	wantStatus               RemovalStatus
	wantReason               RefusalReason
}{
	{"containers", "rm-running", "container", RemovalRefused, ReasonRunning},
	{"images", "rmi-used", "image", RemovalRefused, ReasonInUse},
	{"volumes", "rm-used", "volume", RemovalRefused, ReasonInUse},
	{"networks", "rm-used", "network", RemovalRefused, ReasonInUse},
}

// Criteria 3 and 6 on real recordings: the refusals of a running container and of
// something in use are classified, and the argv of the recording is exactly the
// bare verb and identifier our functions send (the replay fails the test on any
// other argv, so a `-f` would be caught here too).
func TestRecordedRefusalsAreClassifiedOnEveryRecordedConfiguration(t *testing.T) {
	all, err := enginefixture.All()
	if err != nil {
		t.Fatalf("loading the recorded fixtures: %v", err)
	}
	seen := map[string]map[enginefixture.Tool]int{}
	for _, sc := range removalScenarios {
		for _, f := range all {
			if f.Campaign != sc.campaign || f.Scenario != sc.scenario || strings.HasPrefix(f.Meta.Record, "skip") {
				continue
			}
			key := sc.campaign + "/" + sc.scenario
			if seen[key] == nil {
				seen[key] = map[enginefixture.Tool]int{}
			}
			seen[key][f.Meta.Tool]++
			t.Run(f.Name, func(t *testing.T) {
				usingEngine(t, string(f.Meta.Tool))
				useFixtures(t, f)
				target := f.Exchange.Argv[len(f.Exchange.Argv)-1]
				if want := argvOne(sc.kind, target); !slices.Equal(f.Exchange.Argv, want) {
					t.Fatalf("the recording sent %q, want the bare %q", f.Exchange.Argv, want)
				}

				err := removeOne(sc.kind, target)
				status, reason := ClassifyRemoval(err)

				if status != sc.wantStatus || reason != sc.wantReason {
					t.Errorf("ClassifyRemoval(%v) = (%q, %q), want (%q, %q)", err, status, reason, sc.wantStatus, sc.wantReason)
				}
			})
		}
	}
	for _, sc := range removalScenarios {
		key := sc.campaign + "/" + sc.scenario
		for _, tool := range []enginefixture.Tool{enginefixture.Docker, enginefixture.Podman} {
			if seen[key][tool] == 0 {
				t.Errorf("no recorded %s fixture for %s: the classification is not tested on that engine", tool, key)
			}
		}
	}
}

// The batch on the recording of a running container, through RemoveContainers.
func TestARecordedRunningRefusalIsReportedAsRunning(t *testing.T) {
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
			useFixtures(t, f)
			id := f.Exchange.Argv[len(f.Exchange.Argv)-1]

			report := RemoveContainers([]string{id})

			if len(report) != 1 || report[0].Status != RemovalRefused || report[0].Reason != ReasonRunning {
				t.Errorf("report = %+v, want one refusal, reason %q", report, ReasonRunning)
			}
		})
	}
	if n == 0 {
		t.Fatal("no recorded rm-running fixture: the test checked nothing")
	}
}

// The recorded `inspect-absent` says "no such object" on both engines. It is an
// `inspect`, not a removal, but it is the one recorded text of a missing
// container, and a removal that finds nothing may well say the same through a
// newer engine: it is read as "already gone" too.
func TestTheRecordedWordsOfAMissingContainerAreRecognised(t *testing.T) {
	all, err := enginefixture.All()
	if err != nil {
		t.Fatalf("loading the recorded fixtures: %v", err)
	}
	n := 0
	for _, f := range all {
		if f.Campaign != "containers" || f.Scenario != "inspect-absent" || strings.HasPrefix(f.Meta.Record, "skip") {
			continue
		}
		n++
		t.Run(f.Name, func(t *testing.T) {
			err := fmt.Errorf("%s failed: %s", f.Meta.Tool+" rm", strings.TrimSpace(string(f.Exchange.Stderr)))

			status, reason := ClassifyRemoval(err)

			if status != RemovalGone || reason != "" {
				t.Errorf("ClassifyRemoval(%q) = (%q, %q), want (%q, \"\")", err, status, reason, RemovalGone)
			}
		})
	}
	if n == 0 {
		t.Fatal("no recorded inspect-absent fixture: the test checked nothing")
	}
}
