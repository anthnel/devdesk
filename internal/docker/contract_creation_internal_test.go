package docker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09b1 (DEVDESK-807), helpers (4) and (5): what the scenarios of OC-09b need to
// create, and the ids a fixture may keep. Every creation goes through the recorder
// of the contract and its rule, so the tests run the helpers on a fakecli engine:
// the argv is exact, and a helper whose argv the rule refuses fails here. The
// signatures are in contract_oc09b_helpers_internal_test.go.
//
// The options and the verb the helpers need are added to contractArgvRule, each with
// its reason: --health-cmd, --health-interval, --health-timeout and --health-retries
// (they run a command inside the container, nothing of the host), and
// `healthcheck run <container of the run>` (podman only: its health state does not
// move by itself, measured 2026-11-27, so the contract asks for one run).

// holderPort is the host port of the port holder: fixed, so the argv a fixture holds
// does not change from one recording to the next, and on loopback only. A lab VM has
// nothing else on it; on another machine the holder fails and says so.
const holderPort = "18080"

func healthArgv(role string) []string {
	return []string{"run", "-d", "--pull=never", "--label", labelOf(labRunID), "--name", ctrNameOf(role),
		"--health-cmd", "true", "--health-interval", "1s", "--health-timeout", "2s", "--health-retries", "1",
		contractImage, "sleep", "600"}
}

func holderArgv(role string) []string {
	return []string{"run", "-d", "--pull=never", "--label", labelOf(labRunID), "--name", ctrNameOf(role),
		"-p", "127.0.0.1:" + holderPort + ":80", contractImage, "sleep", "600"}
}

func healthStatusArgv(id string) []string {
	return []string{"inspect", "--type", "container", "--format", "{{.State.Health.Status}}", id}
}

// ---- The rule learns the options and the verb of the helpers, and no more. ----

func TestRunRuleAllowsTheHealthOptionsAndOnlyThose(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"the argv of healthContainer", ruleBase + "--name " + ruleName + " --health-cmd true --health-interval 1s --health-timeout 2s --health-retries 1 img sleep 600", ""},
		{"the options with =", ruleBase + "--health-cmd=true --health-interval=1s --health-timeout=2s --health-retries=1 img sleep 600", ""},
		{"a health option with no value at the end", "run -d --pull=never --label " + ruleLabel + " --health-cmd", "no value"},
		{"an option of podman's that the list does not name", ruleBase + "--health-on-failure=kill img sleep 1", "contractArgvRule"},
		{"--no-healthcheck", ruleBase + "--no-healthcheck img sleep 1", "contractArgvRule"},
		{"a health option does not hide a refused one", ruleBase + "--health-cmd true --privileged img sleep 1", "--privileged"},
	})
}

func TestHealthcheckRunIsAllowedOnlyOnAContainerOfTheRun(t *testing.T) {
	runRuleCases(t, []ruleCase{
		{"a container of the run, by id", "healthcheck run " + idA, ""},
		{"a container of the run, by name", "healthcheck run " + ruleName, ""},
		{"a stranger's container", "healthcheck run " + idB, "not a resource of this run"},
		{"no container", "healthcheck run", "no target"},
		{"an option", "healthcheck run --timeout 5 " + idA, "contractArgvRule"},
		{"another healthcheck verb", "healthcheck " + idA, "contractArgvRule"},
	})
}

// ---- healthContainer and waitHealthy. ----

func TestHealthContainerStartsAContainerWithAHealthcheckThroughItsOwnRule(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, fake := startOnFake(t, shape, replyTo(fakecli.Reply{Stdout: idA + "\n"}, healthArgv("h1")...))
		var id string

		mustReachTheEnd(t, ct, func() { id = c.healthContainer("h1") })

		if id != idA || c.ours[idA] != "h1" || c.ours[ctrNameOf("h1")] != "h1" {
			t.Errorf("healthContainer = %q, ours = %v, want the id the engine printed, registered by id and by name", id, c.ours)
		}
		if got := invokedArgs(fake); !sameArgvs(got, [][]string{healthArgv("h1")}) {
			t.Errorf("engine calls = %q, want %q", got, healthArgv("h1"))
		}
		if len(ct.errs) != 0 {
			t.Errorf("reports = %q: the rule refused what the helper sends", ct.errs)
		}
	})
}

func TestWaitHealthyReadsTheHealthStateUntilItIsHealthy(t *testing.T) {
	tests := []struct {
		name  string
		shape string
		steps []queuedStep
		want  [][]string
		slept int
	}{
		{
			name: "docker: starting, then healthy", shape: engine.Docker,
			steps: []queuedStep{{argv: healthStatusArgv(idA), out: "starting\n"}, {argv: healthStatusArgv(idA), out: "healthy\n"}},
			want:  [][]string{healthStatusArgv(idA), healthStatusArgv(idA)}, slept: 1,
		},
		{
			name: "docker: healthy at once", shape: engine.Docker,
			steps: []queuedStep{{argv: healthStatusArgv(idA), out: "healthy\n"}},
			want:  [][]string{healthStatusArgv(idA)}, slept: 0,
		},
		{
			// podman's health state does not move by itself on the lab VM: each round asks
			// for one run of the check, then reads the state.
			name: "podman: one check run per round", shape: engine.Podman,
			steps: []queuedStep{
				{argv: []string{"healthcheck", "run", idA}}, {argv: healthStatusArgv(idA), out: "starting\n"},
				{argv: []string{"healthcheck", "run", idA}}, {argv: healthStatusArgv(idA), out: "healthy\n"},
			},
			want: [][]string{
				{"healthcheck", "run", idA}, healthStatusArgv(idA),
				{"healthcheck", "run", idA}, healthStatusArgv(idA),
			}, slept: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinEngine(t, tt.shape)
			q := queued(t, tt.steps...)
			ct := &contractT{TB: t}
			c := newContract(ct, sessionOf(engine.ShapeFor(tt.shape), labRunID))
			var slept []time.Duration
			c.sleep = func(d time.Duration) { slept = append(slept, d) }

			mustReachTheEnd(t, ct, func() { c.waitHealthy(idA) })

			var got [][]string
			for _, dc := range q.calls {
				got = append(got, dc.Args)
			}
			if !sameArgvs(got, tt.want) {
				t.Errorf("engine calls = %q, want %q", got, tt.want)
			}
			if len(slept) != tt.slept || slices.ContainsFunc(slept, func(d time.Duration) bool { return d != time.Second }) {
				t.Errorf("slept %v, want %d pause(s) of one second", slept, tt.slept)
			}
		})
	}
}

// The wait is bounded: twenty reads, one second apart, and then the contract ends
// naming the state it last saw.
func TestWaitHealthyGivesUpAfterTwentyReads(t *testing.T) {
	pinEngine(t, engine.Docker)
	steps := make([]queuedStep, 20)
	for i := range steps {
		steps[i] = queuedStep{argv: healthStatusArgv(idA), out: "starting\n"}
	}
	q := queued(t, steps...)
	ct := &contractT{TB: t}
	c := newContract(ct, sessionOf(engine.ShapeFor(engine.Docker), labRunID))
	c.sleep = func(time.Duration) {}

	ended := ct.run(func() { c.waitHealthy(idA) })

	if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], "starting") {
		t.Errorf("ended = %v, fatals = %q, want one Fatalf that names the state it last read", ended, ct.fatals)
	}
	if len(q.calls) != 20 {
		t.Errorf("%d reads, want 20", len(q.calls))
	}
}

func TestWaitHealthyEndsAtOnceOnAnUnhealthyContainerOrAFailedRead(t *testing.T) {
	tests := []struct {
		name string
		step queuedStep
		want string
	}{
		{"unhealthy", queuedStep{argv: healthStatusArgv(idA), out: "unhealthy\n"}, "unhealthy"},
		{"the read fails", queuedStep{argv: healthStatusArgv(idA), out: "Error: no such container", err: errExit}, "no such container"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinEngine(t, engine.Docker)
			q := queued(t, tt.step)
			ct := &contractT{TB: t}
			c := newContract(ct, sessionOf(engine.ShapeFor(engine.Docker), labRunID))
			c.sleep = func(time.Duration) {}

			ended := ct.run(func() { c.waitHealthy(idA) })

			if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], tt.want) {
				t.Errorf("ended = %v, fatals = %q, want one Fatalf that says %q", ended, ct.fatals, tt.want)
			}
			if len(q.calls) != 1 {
				t.Errorf("%d reads, want 1: nothing is gained by waiting", len(q.calls))
			}
		})
	}
}

// ---- portHolder. ----

func TestPortHolderPublishesOneLoopbackPortThroughItsOwnWindow(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, fake := startOnFake(t, shape, replyTo(fakecli.Reply{Stdout: idA + "\n"}, holderArgv("p1")...))
		var id, port string

		mustReachTheEnd(t, ct, func() { id, port = c.portHolder("p1") })

		if id != idA || port != holderPort {
			t.Errorf("portHolder = %q, %q, want the container id and the host port %s", id, port, holderPort)
		}
		if c.ours[idA] != "p1" || c.ours[ctrNameOf("p1")] != "p1" {
			t.Errorf("ours = %v, want the holder registered by id and by name", c.ours)
		}
		if got := invokedArgs(fake); !sameArgvs(got, [][]string{holderArgv("p1")}) {
			t.Errorf("engine calls = %q, want %q: labelled, named, on 127.0.0.1", got, holderArgv("p1"))
		}
		if len(ct.errs) != 0 {
			t.Errorf("reports = %q: the window did not let the holder through", ct.errs)
		}
		// The window is closed: the standard rule still refuses a published port.
		before := len(fake.Invocations())
		if _, err := c.rec.Run(dockerCmd{Args: holderArgv("p2"), Combined: true}); err == nil {
			t.Error("the holder argv went through after portHolder returned: the window must close with the call")
		}
		ct.errs = nil
		if n := len(fake.Invocations()); n != before {
			t.Errorf("the engine was started %d more time(s)", n-before)
		}
	})
}

func TestPortHolderEndsNamingThePortWhenTheEngineRefuses(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, _ := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stderr: "Error: address already in use", Exit: 126}, holderArgv("p1")...))

		ended := ct.run(func() { c.portHolder("p1") })

		if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], holderPort) {
			t.Errorf("ended = %v, fatals = %q, want one Fatalf that names port %s: the lab is not clean, or this is not a lab", ended, ct.fatals, holderPort)
		}
	})
}

// ---- usedVolume, usedNetwork, usedImage. ----

func TestUsedVolumeMakesAVolumeAndAContainerThatMountsIt(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		vol := ctrNameOf("v1")
		run := []string{"run", "-d", "--pull=never", "--label", labelOf(labRunID), "--name", ctrNameOf("c1"), "-v", vol + ":/data", contractImage, "sleep", "600"}
		create := []string{"volume", "create", "--label", labelOf(labRunID), vol}
		c, ct, fake := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stdout: vol + "\n"}, create...),
			replyTo(fakecli.Reply{Stdout: idA + "\n"}, run...))
		var gotVolume, gotContainer string

		mustReachTheEnd(t, ct, func() { gotVolume, gotContainer = c.usedVolume("v1", "c1") })

		if gotVolume != vol || gotContainer != idA {
			t.Errorf("usedVolume = %q, %q, want %q and %q", gotVolume, gotContainer, vol, idA)
		}
		if got := invokedArgs(fake); !sameArgvs(got, [][]string{create, run}) {
			t.Errorf("engine calls = %q, want the volume first, then the container that mounts it", got)
		}
		if c.ours[vol] != "v1" || c.ours[idA] != "c1" || len(ct.errs) != 0 {
			t.Errorf("ours = %v, reports = %q, want both registered and no refusal", c.ours, ct.errs)
		}
	})
}

func TestUsedNetworkMakesANetworkAndAContainerThatJoinsIt(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		net := ctrNameOf("n1")
		printed := strings.TrimSpace(networkAnswer(shape, net))
		create := []string{"network", "create", "--label", labelOf(labRunID), net}
		run := []string{"run", "-d", "--pull=never", "--label", labelOf(labRunID), "--name", ctrNameOf("c1"), "--network", net, contractImage, "sleep", "600"}
		c, ct, fake := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stdout: networkAnswer(shape, net)}, create...),
			replyTo(fakecli.Reply{Stdout: idA + "\n"}, run...))
		var gotNetwork, gotContainer string

		mustReachTheEnd(t, ct, func() { gotNetwork, gotContainer = c.usedNetwork("n1", "c1") })

		if gotNetwork != printed || gotContainer != idA {
			t.Errorf("usedNetwork = %q, %q, want what the engine printed (%q) and %q", gotNetwork, gotContainer, printed, idA)
		}
		if got := invokedArgs(fake); !sameArgvs(got, [][]string{create, run}) {
			t.Errorf("engine calls = %q, want the network first, then the container that joins it by name", got)
		}
		if c.ours[printed] != "n1" || c.ours[net] != "n1" || c.ours[idA] != "c1" || len(ct.errs) != 0 {
			t.Errorf("ours = %v, reports = %q, want all registered and no refusal", c.ours, ct.errs)
		}
	})
}

func TestUsedImageStartsAContainerAndReadsTheIDOfItsImageAsPublic(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, fake := startOnFake(t, shape, usedImageSteps(shape)...)
		var container, image string

		mustReachTheEnd(t, ct, func() { container, image = c.usedImage("c1") })

		if container != idA || image != idImg {
			t.Errorf("usedImage = %q, %q, want %q and %q without the algorithm in front of it", container, image, idA, idImg)
		}
		want := [][]string{createArgv(labRunID, "c1"), imageInspectArgv()}
		if got := invokedArgs(fake); !sameArgvs(got, want) {
			t.Errorf("engine calls = %q, want %q", got, want)
		}
		if got := c.redaction().Public; !slices.Equal(got, []string{idImg}) {
			t.Errorf("Redaction.Public = %q, want the id of the image the scenarios print", got)
		}
		if _, redacted := c.redaction().IDs[idImg]; redacted {
			t.Error("the image id is in Redaction.IDs: it would be replaced instead of kept")
		}
		if len(ct.errs) != 0 {
			t.Errorf("reports = %q, want none", ct.errs)
		}
	})
}

func TestUsedImageEndsWhenTheEngineDoesNotPrintAnImageID(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, _ := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stdout: idA + "\n"}, createArgv(labRunID, "c1")...),
			replyTo(fakecli.Reply{Stdout: "<no value>\n"}, imageInspectArgv()...))

		ended := ct.run(func() { c.usedImage("c1") })

		if !ended || len(ct.fatals) != 1 {
			t.Errorf("ended = %v, fatals = %q, want one Fatalf", ended, ct.fatals)
		}
		if got := c.redaction().Public; len(got) != 0 {
			t.Errorf("Redaction.Public = %q after a bad answer, want nothing declared", got)
		}
	})
}

// ---- What every creation helper needs. ----

func TestTheNewCreationHelpersNeedTheLabMarkersAndThePinnedImage(t *testing.T) {
	ops := map[string]func(c *contract){
		"healthContainer": func(c *contract) { c.healthContainer("h1") },
		"portHolder":      func(c *contract) { c.portHolder("p1") },
		"usedVolume":      func(c *contract) { c.usedVolume("v1", "c1") },
		"usedNetwork":     func(c *contract) { c.usedNetwork("n1", "c1") },
		"usedImage":       func(c *contract) { c.usedImage("c1") },
	}
	tests := []struct {
		name           string
		marker, runVar bool
		image          string
		want           string
	}{
		{"no marker file", false, true, contractImage, enginecontract.LabMarkerFile},
		{"no run id variable", true, false, contractImage, enginecontract.RunEnv},
		{"no image", true, true, "", enginecontract.ImageEnv},
		{"an image that is a tag", true, true, "alpine:3.20", enginecontract.ImageEnv},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for opName, op := range ops {
			for _, tt := range tests {
				// usedVolume and usedNetwork create their volume or network first: they
				// need the lab marks too, but an image problem is only met by the container.
				if tt.want == enginecontract.ImageEnv && (opName == "usedVolume" || opName == "usedNetwork") {
					continue
				}
				t.Run(opName+"/"+tt.name, func(t *testing.T) {
					setLab(t, tt.marker, tt.runVar)
					t.Setenv(enginecontract.ImageEnv, tt.image)
					q := queued(t) // no step: nothing may reach the engine
					ct := &contractT{TB: t}
					c := newContract(ct, sessionOf(shape, labRunID))

					ended := ct.run(func() { op(c) })

					if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], tt.want) {
						t.Fatalf("%s ended = %v, fatals = %q, want one Fatalf that names %q", opName, ended, ct.fatals, tt.want)
					}
					if len(q.calls) != 0 || len(c.ours) != 0 {
						t.Errorf("%s reached the engine %d time(s), ours = %v, want nothing", opName, len(q.calls), c.ours)
					}
				})
			}
		}
	})
}

// ---- (5) The ids a fixture may keep. ----

func TestPublicDeclaresTheIDsAFixtureMayKeep(t *testing.T) {
	good := strings.Repeat("e5", 32)
	tests := []struct {
		name  string
		ids   []string
		fatal string // substring of the Fatalf, "" when the declaration is accepted
		want  []string
	}{
		{name: "one image id", ids: []string{good}, want: []string{good}},
		{name: "two ids, in the order given", ids: []string{good, idImg}, want: []string{good, idImg}},
		{name: "the same id twice is kept once", ids: []string{good, good}, want: []string{good}},
		{name: "a short id", ids: []string{good[:12]}, fatal: "64"},
		{name: "upper case", ids: []string{strings.ToUpper(good)}, fatal: "64"},
		{name: "with the algorithm in front", ids: []string{"sha256:" + good}, fatal: "64"},
		{name: "not hexadecimal", ids: []string{strings.Repeat("g", 64)}, fatal: "64"},
		{name: "empty", ids: []string{""}, fatal: "64"},
		{name: "an id of a resource of this run: it is redacted, never public", ids: []string{idA}, fatal: "this run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := &contractT{TB: t}
			c := newContract(ct, labSession(enginefixture.Podman, labRunID))
			c.ours[idA] = "c1"

			ended := ct.run(func() { c.public(tt.ids...) })

			if tt.fatal != "" {
				if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], tt.fatal) {
					t.Fatalf("ended = %v, fatals = %q, want one Fatalf that says %q", ended, ct.fatals, tt.fatal)
				}
				if got := c.redaction().Public; len(got) != 0 {
					t.Errorf("Redaction.Public = %q after a refused declaration, want nothing", got)
				}
				return
			}
			if ended {
				t.Fatalf("public(%q) ended on %q", tt.ids, ct.fatals)
			}
			r := c.redaction()
			if !slices.Equal(r.Public, tt.want) {
				t.Errorf("Redaction.Public = %q, want %q", r.Public, tt.want)
			}
			for _, id := range tt.want {
				if _, redacted := r.IDs[id]; redacted {
					t.Errorf("public id %s is also in Redaction.IDs: Redact would replace it", id)
				}
			}
			if r.IDs[idA] != "c1" || r.RunID != labRunID {
				t.Errorf("Redaction = %+v, want the ids and the run it always had", r)
			}
		})
	}
}

// End to end, in a recording session: an image id that the scenario prints is kept
// when it was declared public (usedImage does), and the recording is refused, with
// nothing written, when it was not.
func TestARecordingKeepsADeclaredPublicIDAndRefusesAStranger(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		readIDs := []string{"images", "-q", "--no-trunc"}
		record := func(t *testing.T, declare bool) (written []byte, ct *contractT, ended bool) {
			t.Helper()
			steps := append(usedImageSteps(shape), replyTo(fakecli.Reply{Stdout: idImg + "\n"}, readIDs...))
			c, ct, _ := startOnFake(t, shape, steps...)
			out := t.TempDir()
			c.s.Mode, c.s.OutDir = enginecontract.Record, out
			ended = ct.run(func() {
				if declare {
					c.usedImage("c1")
				} else {
					c.container("c1") // the image id is never declared
				}
				c.exchange("campaign", "image-ids", "the ids of the images", func() { _, _ = dockerOutput(readIDs...) })
			})
			files, _ := filepath.Glob(filepath.Join(out, "campaign", "*.cli"))
			if len(files) == 1 {
				written, _ = os.ReadFile(files[0])
			}
			return written, ct, ended
		}

		t.Run("declared", func(t *testing.T) {
			written, ct, ended := record(t, true)

			if ended {
				t.Fatalf("the recording ended on %q", ct.fatals)
			}
			if !strings.Contains(string(written), idImg) {
				t.Errorf("the fixture does not hold the public image id (written %d bytes)", len(written))
			}
		})
		t.Run("not declared", func(t *testing.T) {
			// c.container makes the container only, so the id of its image is a stranger.
			written, ct, ended := record(t, false)

			if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], enginefixture.RuleUnknownEngineID) {
				t.Fatalf("ended = %v, fatals = %q, want one Fatalf that names %s", ended, ct.fatals, enginefixture.RuleUnknownEngineID)
			}
			if len(written) != 0 {
				t.Error("something was written for a recording that was refused")
			}
			if strings.Contains(strings.Join(ct.fatals, "\n"), idImg) {
				t.Error("the refusal shows the id it refused")
			}
		})
	})
}
