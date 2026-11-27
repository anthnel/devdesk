package docker

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09a2 (DEVDESK-943), steps 8 and 9 of plan OC-09a, criteria 1 and 2: what a
// contract creates carries the label of its run, and what it removes is exactly
// what carries that label. The negative control runs here with no engine: a strict
// stub answers the listing with resources that are not of the run (a stranger's, one
// with no label, one of another run) and the test fails on any removal that names
// them. On the test VM, the sentinel of contract.sh does the same with a real engine.
//
// The cleanup, in the order the plan fixes, for containers, then networks, then
// volumes (the same four steps each):
//
//	1. list by the filter of the engine:  label=devdesk.test=<run>
//	2. inspect each candidate and read its label; only a label EQUAL to <run> is kept
//	3. remove each kept one with RemoveContainer(id, true), RemoveNetwork or
//	   RemoveVolume, which validate the identifier (G3)
//	4. list again, only when step 3 had something to remove: a leftover is reported
//
// Anything the filter let through that is not of the run is reported (Errorf) and
// never touched. A listing or an inspect that fails removes nothing and is reported:
// when in doubt, the cleanup does not remove. It carries on with the other kinds.

// resourceKind describes one kind of resource, for the three kinds at once.
type resourceKind struct {
	name    string
	list    []string
	inspect func(ref string) []string
	rm      func(ref string) []string
	// mine is a resource that carries the label of the run, other one that carries
	// the label of another run, bare one that carries none.
	mine, other, bare string
}

func resourceKinds(run string) []resourceKind {
	label := "label=" + labelOf(run)
	return []resourceKind{
		{
			name: "container",
			list: []string{"ps", "-a", "-q", "--no-trunc", "--filter", label},
			inspect: func(ref string) []string {
				return []string{"inspect", "--type", "container", "--format", `{{index .Config.Labels "devdesk.test"}}`, ref}
			},
			rm:   func(ref string) []string { return []string{"rm", "-f", ref} },
			mine: idA, other: idB, bare: idC,
		},
		{
			name: "network",
			list: []string{"network", "ls", "-q", "--no-trunc", "--filter", label},
			inspect: func(ref string) []string {
				return []string{"network", "inspect", "--format", `{{index .Labels "devdesk.test"}}`, ref}
			},
			rm:   func(ref string) []string { return []string{"network", "rm", ref} },
			mine: idA, other: idB, bare: idC,
		},
		{
			name: "volume",
			list: []string{"volume", "ls", "-q", "--filter", label},
			inspect: func(ref string) []string {
				return []string{"volume", "inspect", "--format", `{{index .Labels "devdesk.test"}}`, ref}
			},
			rm:   func(ref string) []string { return []string{"volume", "rm", ref} },
			mine: "ddc-" + run + "-v1", other: "pgdata", bare: "scratch",
		},
	}
}

// createArgv is what c.container(role) sends: the label, the name, --pull=never
// (the image is pulled by contract.sh, never by Go code) and the pinned image.
func createArgv(run, role string) []string {
	return []string{"run", "-d", "--pull=never", "--label", labelOf(run), "--name", "ddc-" + run + "-" + role,
		contractImage, "sleep", "600"}
}

// session returns a session of the engine named by shape, for the run.
func sessionOf(shape engine.Shape, run string) *enginecontract.Session {
	return labSession(enginefixture.Tool(shape.Name), run)
}

// ---- Creation: label, name, image. ----

func TestCreatedResourcesCarryTheLabelAndTheNameOfTheRun(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		openLab(t)
		t.Setenv(enginecontract.ImageEnv, contractImage)
		label := labelOf(labRunID)
		netOut := idB + "\n"
		if shape.Name == engine.Podman {
			netOut = "ddc-" + labRunID + "-n1\n" // podman prints the name of the network it made
		}
		q := queued(t,
			queuedStep{argv: createArgv(labRunID, "c1"), out: idA + "\n"},
			queuedStep{argv: []string{"volume", "create", "--label", label, "ddc-" + labRunID + "-v1"}, out: "ddc-" + labRunID + "-v1\n"},
			queuedStep{argv: []string{"network", "create", "--label", label, "ddc-" + labRunID + "-n1"}, out: netOut},
		)
		c := newContract(&contractT{TB: t}, sessionOf(shape, labRunID))

		gotC := c.container("c1")
		gotV := c.volume("v1")
		gotN := c.network("n1")

		if gotC != idA || gotV != "ddc-"+labRunID+"-v1" || gotN != strings.TrimSpace(netOut) {
			t.Errorf("container, volume, network = %q, %q, %q, want what the engine printed, trimmed", gotC, gotV, gotN)
		}
		for ref, role := range map[string]string{
			idA: "c1", "ddc-" + labRunID + "-c1": "c1",
			gotV: "v1", gotN: "n1", "ddc-" + labRunID + "-n1": "n1",
		} {
			if c.ours[ref] != role {
				t.Errorf("ours[%q] = %q, want %q: what a contract creates is registered by id and by name", ref, c.ours[ref], role)
			}
		}
		if len(q.calls) != 3 {
			t.Errorf("%d engine calls, want 3 (run, volume create, network create)", len(q.calls))
		}
		for _, dc := range q.calls {
			if dc.Helper != "" || dc.Stdin != "" || dc.Ctx != nil {
				t.Errorf("creation used a helper, stdin or a context: %+v", dc)
			}
		}
	})
}

func TestContainerWithACustomCommand(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		openLab(t)
		t.Setenv(enginecontract.ImageEnv, contractImage)
		want := createArgv(labRunID, "c2")
		want = append(want[:len(want)-2], "sleep", "60")
		queued(t, queuedStep{argv: want, out: idA + "\n"})
		c := newContract(&contractT{TB: t}, sessionOf(shape, labRunID))

		if got := c.container("c2", "sleep", "60"); got != idA {
			t.Errorf("container(c2, sleep 60) = %q, want %q", got, idA)
		}
	})
}

func TestCreationFailsBeforeTheEngineWithoutAnImageOrWithABadAnswer(t *testing.T) {
	tests := []struct {
		name  string
		image string
		steps []queuedStep
		want  string
	}{
		{name: "no image: the variable is empty", image: "", want: enginecontract.ImageEnv},
		{
			name: "the engine does not print a container id", image: contractImage,
			steps: []queuedStep{{argv: createArgv(labRunID, "c1"), out: "WARNING: something\n"}},
			want:  "WARNING",
		},
		{
			name: "the engine fails", image: contractImage,
			steps: []queuedStep{{argv: createArgv(labRunID, "c1"), out: "no space left on device\n", err: errExit}},
			want:  "no space left",
		},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				openLab(t)
				t.Setenv(enginecontract.ImageEnv, tt.image)
				queued(t, tt.steps...)
				ct := &contractT{TB: t}
				c := newContract(ct, sessionOf(shape, labRunID))

				ended := ct.run(func() { c.container("c1") })

				if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], tt.want) {
					t.Errorf("container(c1) ended = %v, fatals = %q, want a Fatalf that says %q", ended, ct.fatals, tt.want)
				}
			})
		}
	})
}

// A creation that was tried may have made the resource even though the call failed
// (a timeout after the container exists): the cleanup of the contract still looks.
// One that was never tried leaves the engine alone, which is what lets a read-only
// contract run with no label and no mark.
func TestCleanupLooksOnlyWhenACreationWasTried(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		t.Run("nothing created, nothing asked", func(t *testing.T) {
			q := queued(t) // no step: any call fails the test
			c := newContract(&contractT{TB: t}, sessionOf(shape, labRunID))

			c.cleanup()

			if len(q.calls) != 0 {
				t.Errorf("cleanup() ran %v, want no engine call when nothing was created", q.calls)
			}
		})
		t.Run("a failed creation is still cleaned", func(t *testing.T) {
			openLab(t)
			t.Setenv(enginecontract.ImageEnv, contractImage)
			steps := []queuedStep{{argv: createArgv(labRunID, "c1"), err: errExit}}
			for _, k := range resourceKinds(labRunID) {
				steps = append(steps, queuedStep{argv: k.list})
			}
			q := queued(t, steps...)
			ct := &contractT{TB: t}
			c := newContract(ct, sessionOf(shape, labRunID))

			ended := ct.run(func() { c.container("c1") })
			c.cleanup()

			if !ended {
				t.Fatal("the failing creation did not end the test")
			}
			if n := len(q.calls); n != 4 {
				t.Errorf("%d engine calls, want 4: the creation, then the three listings of the cleanup", n)
			}
		})
	})
}

// ---- The lab marker (security decision M1). ----

// Before the least resource is created or removed, the lab marks are required. Each
// operation is refused with no engine call, and the message names what is missing.
func TestCreationAndCleanupRequireTheLabMarkers(t *testing.T) {
	ops := map[string]func(c *contract){
		"container":  func(c *contract) { c.container("c1") },
		"volume":     func(c *contract) { c.volume("v1") },
		"network":    func(c *contract) { c.network("n1") },
		"cleanupNow": func(c *contract) { c.cleanupNow() },
	}
	tests := []struct {
		name           string
		marker, runVar bool
		want           []string
	}{
		{"marker absent", false, true, []string{enginecontract.LabMarkerFile}},
		{"variable absent", true, false, []string{enginecontract.RunEnv}},
		{"both absent", false, false, []string{enginecontract.LabMarkerFile, enginecontract.RunEnv}},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for opName, op := range ops {
			for _, tt := range tests {
				t.Run(opName+"/"+tt.name, func(t *testing.T) {
					setLab(t, tt.marker, tt.runVar)
					t.Setenv(enginecontract.ImageEnv, contractImage)
					q := queued(t) // no step: nothing may reach the engine
					ct := &contractT{TB: t}
					c := newContract(ct, sessionOf(shape, labRunID))

					ended := ct.run(func() { op(c) })

					if !ended || len(ct.fatals) != 1 {
						t.Fatalf("%s ended = %v, fatals = %q, want one Fatalf", opName, ended, ct.fatals)
					}
					for _, w := range tt.want {
						if !strings.Contains(ct.fatals[0], w) {
							t.Errorf("fatal message = %q, want it to name %q", ct.fatals[0], w)
						}
					}
					if len(q.calls) != 0 {
						t.Errorf("%s reached the engine %d time(s) without the lab markers: %v", opName, len(q.calls), q.calls)
					}
					if len(c.ours) != 0 {
						t.Errorf("ours = %v after a refusal, want empty", c.ours)
					}
				})
			}
		}
	})
}

// Read-only is not concerned: a contract that creates nothing runs with neither
// mark, outside a VM, in verify mode, and leaves the engine as it found it.
func TestAReadOnlyContractNeedsNoLabMarkers(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		setLab(t, false, false)
		fake := fakecli.Install(t, fakecli.Scenario{{Tool: shape.Binary, Args: []string{"version"}, Reply: fakecli.Reply{Stdout: "x\n"}}}, shape.Binary)
		ct := &contractT{TB: t}
		c := startContract(ct, sessionOf(shape, labRunID))

		ex := c.exchange("campaign", "read-only", "a read", func() { _, _ = dockerOutput("version") })
		ct.runCleanups()

		if string(ex.Stdout) != "x\n" || len(ct.errs)+len(ct.fatals) != 0 {
			t.Errorf("exchange = %+v, reports = %q %q, want the answer and no report", ex, ct.errs, ct.fatals)
		}
		if n := len(fake.Invocations()); n != 1 {
			t.Errorf("%d engine calls, want 1 (the read): the cleanup of a contract that created nothing asks nothing", n)
		}
	})
}

// ---- The cleanup. ----

type cleanupCase struct {
	name     string
	list     string
	listErr  error
	inspects []queuedStep // argv filled from the kind: the ref is in argv[len-1] of the result, set below
	removes  []queuedStep
	relist   *string

	inspectRefs, removeRefs []string // the refs, in the order of the steps above

	wantRemoved []string
	wantReports []string // each must appear in some Errorf
	noReport    []string // none may appear in any Errorf
	untouched   []string // no rm, stop or kill may name any of them
	anyReport   bool     // at least one report, whatever it says
}

func ptr(s string) *string { return &s }

func cleanupCases(k resourceKind, run string) []cleanupCase {
	m, o, b := k.mine, k.other, k.bare
	return []cleanupCase{
		{
			name:     "nominal: one of ours, removed, then the list is empty",
			list:     m + "\n",
			inspects: []queuedStep{{out: run + "\n"}}, inspectRefs: []string{m},
			removes: []queuedStep{{}}, removeRefs: []string{m},
			relist:      ptr(""),
			wantRemoved: []string{m},
		},
		{
			// The failing filter of the plan, simulated: the engine lists a stranger's
			// resource and one with no label next to ours.
			name:     "negative control: a stranger's and an unlabelled one are listed and never touched",
			list:     m + "\n" + o + "\n" + b + "\n",
			inspects: []queuedStep{{out: run + "\n"}, {out: foreignRunID + "\n"}, {out: "\n"}}, inspectRefs: []string{m, o, b},
			removes: []queuedStep{{}}, removeRefs: []string{m},
			relist:      ptr(o + "\n" + b + "\n"),
			wantRemoved: []string{m},
			wantReports: []string{o, b},
			noReport:    []string{m},
			untouched:   []string{o, b},
		},
		{
			name:     "a label that only starts with ours (the run of a sibling) is not ours",
			list:     o + "\n",
			inspects: []queuedStep{{out: run + "-x\n"}}, inspectRefs: []string{o},
			wantReports: []string{o},
			untouched:   []string{o},
		},
		{
			name:     "only strangers: nothing removed, nothing listed again",
			list:     o + "\n" + b + "\n",
			inspects: []queuedStep{{out: foreignRunID + "\n"}, {out: "\n"}}, inspectRefs: []string{o, b},
			wantReports: []string{o, b},
			untouched:   []string{o, b},
		},
		{
			name:     "a leftover after the removal is reported by name",
			list:     m + "\n",
			inspects: []queuedStep{{out: run + "\n"}}, inspectRefs: []string{m},
			removes: []queuedStep{{}}, removeRefs: []string{m},
			relist:      ptr(m + "\n"),
			wantRemoved: []string{m},
			wantReports: []string{m},
		},
		{
			name:     "a removal that fails is reported with the engine's own words, and not counted",
			list:     m + "\n",
			inspects: []queuedStep{{out: run + "\n"}}, inspectRefs: []string{m},
			removes: []queuedStep{{out: "Error: resource is busy\n", err: errExit}}, removeRefs: []string{m},
			relist:      ptr(m + "\n"),
			wantReports: []string{"busy", m},
		},
		{
			name:     "an inspect that fails: in doubt, nothing is removed",
			list:     m + "\n",
			inspects: []queuedStep{{err: errExit}}, inspectRefs: []string{m},
			wantReports: []string{m},
			untouched:   []string{m},
		},
		{
			name:      "a listing that fails: nothing to remove, and a report",
			listErr:   errExit,
			anyReport: true,
		},
	}
}

func TestCleanupNeverTouchesWhatDoesNotCarryTheLabelOfTheRun(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		kinds := resourceKinds(labRunID)
		for _, k := range kinds {
			for _, tc := range cleanupCases(k, labRunID) {
				t.Run(k.name+"/"+tc.name, func(t *testing.T) {
					openLab(t)
					steps := []queuedStep{{argv: k.list, out: tc.list, err: tc.listErr}}
					for i, s := range tc.inspects {
						s.argv = k.inspect(tc.inspectRefs[i])
						steps = append(steps, s)
					}
					for i, s := range tc.removes {
						s.argv = k.rm(tc.removeRefs[i])
						steps = append(steps, s)
					}
					if tc.relist != nil {
						steps = append(steps, queuedStep{argv: k.list, out: *tc.relist})
					}
					for _, other := range kinds {
						if other.name != k.name {
							steps = append(steps, queuedStep{argv: other.list})
						}
					}
					q := queued(t, steps...)
					ct := &contractT{TB: t}
					c := newContract(ct, sessionOf(shape, labRunID))

					var removed []string
					ct.run(func() { removed = c.cleanupNow() })

					if !slices.Equal(removed, tc.wantRemoved) {
						t.Errorf("cleanupNow() = %q, want %q", removed, tc.wantRemoved)
					}
					if len(ct.fatals) != 0 {
						t.Errorf("fatals = %q, want none: the cleanup reports and carries on", ct.fatals)
					}
					report := strings.Join(ct.errs, "\n")
					for _, w := range tc.wantReports {
						if !strings.Contains(report, w) {
							t.Errorf("reports = %q, want one that names %q", ct.errs, w)
						}
					}
					for _, w := range tc.noReport {
						if strings.Contains(report, w) {
							t.Errorf("reports = %q, must not name %q: it is of this run and was removed", ct.errs, w)
						}
					}
					if tc.anyReport && len(ct.errs) == 0 {
						t.Error("no report, want one")
					}
					if len(tc.wantReports) == 0 && !tc.anyReport && len(ct.errs) != 0 {
						t.Errorf("reports = %q, want none", ct.errs)
					}
					// Criterion 2 itself, read from the argv that ran and not from the stub's strictness.
					for _, dc := range q.calls {
						if !slices.ContainsFunc(dc.Args, isRemovalWord) {
							continue
						}
						for _, ref := range tc.untouched {
							if slices.Contains(dc.Args, ref) {
								t.Errorf("%q names %q, which does not carry the label of the run", dc.Args, ref)
							}
						}
					}
				})
			}
		}
	})
}

func isRemovalWord(s string) bool { return s == "rm" || s == "stop" || s == "kill" || s == "rmi" }

// The order the plan fixes: containers, then networks, then volumes, so that a
// container is gone before the network and the volume it used.
func TestCleanupRemovesContainersThenNetworksThenVolumes(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		openLab(t)
		var steps []queuedStep
		for _, k := range resourceKinds(labRunID) {
			steps = append(steps,
				queuedStep{argv: k.list, out: k.mine + "\n"},
				queuedStep{argv: k.inspect(k.mine), out: labRunID + "\n"},
				queuedStep{argv: k.rm(k.mine)},
				queuedStep{argv: k.list},
			)
		}
		q := queued(t, steps...)
		c := newContract(&contractT{TB: t}, sessionOf(shape, labRunID))

		removed := c.cleanupNow()

		want := []string{idA, idA, "ddc-" + labRunID + "-v1"}
		if !slices.Equal(removed, want) {
			t.Errorf("cleanupNow() = %q, want %q (container, network, volume)", removed, want)
		}
		var verbs []string
		for _, dc := range q.calls {
			if slices.ContainsFunc(dc.Args, isRemovalWord) {
				verbs = append(verbs, strings.Join(dc.Args[:min(2, len(dc.Args))], " "))
			}
		}
		if got := strings.Join(verbs, ", "); got != "rm -f, network rm, volume rm" {
			t.Errorf("removals ran in the order %q, want containers, networks, volumes", got)
		}
	})
}

// The filter is only as good as the id: an empty or odd id would give
// `label=devdesk.test=` and match every run. The cleanup refuses to list.
func TestCleanupRefusesToListWithAnInvalidRunID(t *testing.T) {
	for _, run := range []string{"", "x", "UPPER-CASE-ID", "has space", "../escape", strings.Repeat("a", 33)} {
		t.Run("run id "+run, func(t *testing.T) {
			openLab(t)
			q := queued(t) // no step: not one command
			ct := &contractT{TB: t}
			c := newContract(ct, labSession(enginefixture.Podman, run))

			var removed []string
			ct.run(func() { removed = c.cleanupNow() })

			if len(q.calls) != 0 {
				t.Errorf("cleanupNow() ran %v with run id %q, want no command at all", q.calls, run)
			}
			if len(removed) != 0 || len(ct.errs)+len(ct.fatals) == 0 {
				t.Errorf("removed = %q, reports = %q %q, want nothing removed and a report", removed, ct.errs, ct.fatals)
			}
		})
	}
}

// ---- The contract as a whole: the cleanup runs when the test failed, and before the runner goes back. ----

func TestTheCleanupRunsAfterAFailureAndBeforeTheRunnerIsPutBack(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		openLab(t)
		t.Setenv(enginecontract.ImageEnv, contractImage)
		scenario := fakecli.Scenario{{
			Tool: shape.Binary, Args: createArgv(labRunID, "c1"),
			Reply: fakecli.Reply{Stderr: "no space left on device", Exit: 1},
		}}
		kinds := resourceKinds(labRunID)
		for _, k := range kinds {
			scenario = append(scenario, fakecli.Exchange{Tool: shape.Binary, Args: k.list})
		}
		// The engine the test found is the other one: the contract pins its own and puts this one back.
		useEngine(t, otherEngine(enginefixture.Tool(shape.Name)))
		found := engine.Current()
		fake := fakecli.Install(t, scenario, shape.Binary)
		original := runner
		ct := &contractT{TB: t}

		c := startContract(ct, sessionOf(shape, labRunID))

		if runner != dockerRunner(c.rec) {
			t.Fatalf("runner = %T after startContract, want the recording runner of the contract: every call of a contract goes through the rule", runner)
		}
		if got := engine.Current().Name; got != shape.Name {
			t.Errorf("engine.Current() = %q during the contract, want %q", got, shape.Name)
		}
		if ended := ct.run(func() { c.container("c1") }); !ended {
			t.Fatal("the failing creation did not end the test")
		}

		// The test is over, failed: the cleanups run last in, first out. The first one is the
		// cleanup of the contract, and it still finds the recorder installed.
		if len(ct.cleanups) < 2 {
			t.Fatalf("%d cleanups registered, want at least 2 (the cleanup, and putting the runner and the engine back)", len(ct.cleanups))
		}
		first := ct.cleanups[len(ct.cleanups)-1]
		ct.cleanups = ct.cleanups[:len(ct.cleanups)-1]
		first()
		if runner != dockerRunner(c.rec) {
			t.Error("the runner was put back before the cleanup ran: the cleanup would reach the engine without the rule")
		}
		listed := 0
		for _, inv := range fake.Invocations() {
			if slices.Equal(inv.Args, kinds[0].list) {
				listed++
			}
		}
		if listed != 1 {
			t.Errorf("the container listing of the cleanup ran %d times after the failed creation, want 1: %+v", listed, fake.Invocations())
		}

		ct.runCleanups()
		if runner != original {
			t.Errorf("runner = %T after the contract, want the one it found, %T", runner, original)
		}
		if engine.Current() != found {
			t.Errorf("engine.Current() = %+v after the contract, want the engine it found, %+v", engine.Current(), found)
		}
	})
}

// ---- A resource of another run, owned by a sibling contract: the self-check of the cleanup. ----

func TestASiblingOwnsResourcesOfAnotherRunThroughItsOwnRule(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		openLab(t)
		t.Setenv(enginecontract.ImageEnv, contractImage)
		siblingRun := labRunID + "-x"
		scenario := fakecli.Scenario{{Tool: shape.Binary, Args: createArgv(siblingRun, "f1"), Reply: fakecli.Reply{Stdout: idB + "\n"}}}
		for _, run := range []string{labRunID, siblingRun} {
			for _, k := range resourceKinds(run) {
				scenario = append(scenario, fakecli.Exchange{Tool: shape.Binary, Args: k.list})
			}
		}
		fake := fakecli.Install(t, scenario, shape.Binary)
		ct := &contractT{TB: t}
		c := startContract(ct, sessionOf(shape, labRunID))

		sib := c.sibling("x")
		id := sib.container("f1")

		if id != idB {
			t.Errorf("sibling container id = %q, want %q", id, idB)
		}
		if runner != dockerRunner(c.rec) {
			t.Errorf("runner = %T after a call of the sibling, want the recorder of the main contract put back", runner)
		}
		if sib.s.RunID != siblingRun {
			t.Errorf("sibling run id = %q, want %q", sib.s.RunID, siblingRun)
		}
		if _, mine := c.ours[idB]; mine {
			t.Error("the main contract counts the sibling's container as its own: its cleanup would remove it")
		}
		if sib.ours[idB] != "f1" {
			t.Errorf("sibling ours[%s] = %q, want f1", idB, sib.ours[idB])
		}
		_, err := c.rec.Run(dockerCmd{Args: []string{"rm", "-f", idB}, Combined: true})
		if err == nil {
			t.Error("the main contract's recorder let rm -f of the sibling's container through")
		}
		var sawLabel bool
		for _, inv := range fake.Invocations() {
			if slices.Contains(inv.Args, labelOf(siblingRun)) && slices.Contains(inv.Args, "ddc-"+siblingRun+"-f1") {
				sawLabel = true
			}
			if slices.Equal(inv.Args, []string{"rm", "-f", idB}) {
				t.Error("rm -f of the sibling's container reached the engine through the main contract")
			}
		}
		if !sawLabel {
			t.Errorf("the sibling's creation did not carry %q and the name of its own run: %+v", labelOf(siblingRun), fake.Invocations())
		}
		ct.errs = nil // the refusal above was the point
		ct.runCleanups()
	})
}

// ---- The exchange window (step 9; a3 adds Record and Verify). ----

func TestExchangeReturnsTheOneExchangeOfItsWindow(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		setLab(t, false, false)
		fakecli.Install(t, fakecli.Scenario{
			{Tool: shape.Binary, Args: []string{"version"}, Reply: fakecli.Reply{Stdout: "x\n"}},
			{Tool: shape.Binary, Args: []string{"ps", "-a"}, Reply: fakecli.Reply{Stderr: "boom", Exit: 2}},
		}, shape.Binary)
		ct := &contractT{TB: t}
		c := startContract(ct, sessionOf(shape, labRunID))

		t.Run("one call, success", func(t *testing.T) {
			ex := c.exchange("campaign", "scenario", "text", func() { _, _ = dockerOutput("version") })

			if !slices.Equal(ex.Argv, []string{"version"}) || ex.Exit != 0 || string(ex.Stdout) != "x\n" || len(ex.Stderr) != 0 {
				t.Errorf("exchange = %+v, want the version call, exit 0, stdout x", ex)
			}
		})
		t.Run("one call, failure: only its own window counts", func(t *testing.T) {
			ex := c.exchange("campaign", "scenario", "text", func() { _, _ = dockerOutput("ps", "-a") })

			if !slices.Equal(ex.Argv, []string{"ps", "-a"}) || ex.Exit != 2 || string(ex.Stderr) != "boom" {
				t.Errorf("exchange = %+v, want the ps call, exit 2, stderr boom", ex)
			}
		})
		t.Run("no call: a fixture is an exchange", func(t *testing.T) {
			before := len(ct.fatals)
			ended := ct.run(func() { c.exchange("campaign", "scenario", "text", func() {}) })

			if !ended || len(ct.fatals) != before+1 {
				t.Errorf("exchange with no call ended = %v, fatals = %q, want a Fatalf", ended, ct.fatals)
			}
		})
		t.Run("two calls", func(t *testing.T) {
			before := len(ct.fatals)
			ended := ct.run(func() {
				c.exchange("campaign", "scenario", "text", func() { _, _ = dockerOutput("version"); _, _ = dockerOutput("version") })
			})

			if !ended || len(ct.fatals) != before+1 {
				t.Errorf("exchange with two calls ended = %v, fatals = %q, want a Fatalf", ended, ct.fatals)
			}
		})
		ct.fatals = nil
		ct.runCleanups()
	})
}
