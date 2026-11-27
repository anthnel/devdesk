package docker

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09b1 (DEVDESK-807), helper (2): the window of an expected refusal. "Name taken"
// and "port taken" are launches DevDesk builds with LaunchContainer, whose options
// have no labels, so the standard rule refuses their argv. A contract that wants to
// record them opens a window: ONE `run` that the standard rule would accept once the
// label of the run is added to the argv it judges, with the name of the window, the
// pinned image, --pull=never and, optionally, the one loopback port of the window.
// Everything else the call does is judged by the rule as before. The engine may leave
// something behind (docker creates the container of a port it cannot bind, then
// fails): after the call, whatever carries the name of the window and is not
// already known is removed by its id, and nothing else.
//
// The signatures are in contract_oc09b_helpers_internal_test.go.

const portGranted = "127.0.0.1:18080:80"

func windowOf(name, port string) launchWindow { return launchWindow{Name: name, Port: port} }

// ---- The rule of the window (pure). ----

type windowCase struct {
	name    string
	w       launchWindow
	argv    string // split on spaces
	ok      bool
	wantErr string // substring of the refusal, "" for any
}

func windowRuleOf(w launchWindow) func(argv []string) error {
	ours := func(ref string) bool { return ref == idA || ref == ctrNameOf("c1") || ref == ctrNameOf("v1") }
	return launchWindowRule(labRunID, contractImage, ours, w)
}

func windowCases() []windowCase {
	name := ctrNameOf("c2")
	launch := "run --pull=never -d --name " + name + " " + contractImage
	withPort := "run --pull=never -d --name " + name + " -p " + portGranted + " " + contractImage
	nameWin, portWin := windowOf(name, ""), windowOf(name, portGranted)
	return []windowCase{
		// Granted.
		{name: "the launch DevDesk builds for a name", w: nameWin, argv: launch, ok: true},
		{name: "--rm", w: nameWin, argv: "run --pull=never --rm -d --name " + name + " " + contractImage, ok: true},
		{name: "--name=", w: nameWin, argv: "run --pull=never -d --name=" + name + " " + contractImage, ok: true},
		{name: "a command after the image is the container's, not read", w: nameWin, argv: launch + " sleep --privileged 5", ok: true},
		{name: "the granted port", w: portWin, argv: withPort, ok: true},
		{name: "a labelled run with the granted port, as the port holder sends it", w: portWin,
			argv: "run -d --pull=never --label " + labelOf(labRunID) + " --name " + name + " -p " + portGranted + " " + contractImage + " sleep 600", ok: true},
		{name: "a labelled run with no port", w: nameWin,
			argv: "run -d --pull=never --label " + labelOf(labRunID) + " --name " + name + " " + contractImage + " sleep 600", ok: true},

		// Refused: the name.
		{name: "another name of the run", w: nameWin, argv: "run --pull=never -d --name " + ctrNameOf("c3") + " " + contractImage, wantErr: name},
		{name: "the name of another run", w: nameWin, argv: "run --pull=never -d --name ddc-" + foreignRunID + "-c2 " + contractImage, wantErr: name},
		{name: "a name that is not ours at all", w: nameWin, argv: "run --pull=never -d --name web " + contractImage, wantErr: name},
		{name: "no name", w: nameWin, argv: "run --pull=never -d " + contractImage, wantErr: "--name"},
		{name: "the name twice", w: nameWin, argv: "run --pull=never -d --name " + name + " --name " + name + " " + contractImage, wantErr: "--name"},

		// Refused: what the standard rule requires of a run.
		{name: "no --pull=never", w: nameWin, argv: "run -d --name " + name + " " + contractImage, wantErr: "--pull=never"},
		{name: "--pull=always next to --pull=never", w: nameWin, argv: "run --pull=never --pull=always -d --name " + name + " " + contractImage, wantErr: "--pull=never"},
		{name: "another image", w: nameWin, argv: "run --pull=never -d --name " + name + " alpine:3.20", wantErr: "image"},
		{name: "no image", w: nameWin, argv: "run --pull=never -d --name " + name, wantErr: "image"},
		{name: "--privileged", w: nameWin, argv: "run --pull=never -d --privileged --name " + name + " " + contractImage, wantErr: "--privileged"},
		{name: "a bind mount", w: nameWin, argv: "run --pull=never -d --name " + name + " -v /:/h " + contractImage, wantErr: "/:/h"},
		{name: "--network host", w: nameWin, argv: "run --pull=never -d --network host --name " + name + " " + contractImage, wantErr: "host"},
		{name: "--replace", w: nameWin, argv: "run --pull=never -d --replace --name " + name + " " + contractImage, wantErr: "--replace"},
		{name: "the label of another run", w: nameWin,
			argv: "run --pull=never -d --label devdesk.test=" + foreignRunID + " --name " + name + " " + contractImage, wantErr: "label"},

		// Refused: the port.
		{name: "a port when the window grants none", w: nameWin, argv: withPort, wantErr: "port"},
		{name: "another loopback port", w: portWin, argv: "run --pull=never -d --name " + name + " -p 127.0.0.1:18081:80 " + contractImage, wantErr: "port"},
		{name: "every interface, by omission", w: portWin, argv: "run --pull=never -d --name " + name + " -p 18080:80 " + contractImage, wantErr: "port"},
		{name: "every interface, by 0.0.0.0", w: portWin, argv: "run --pull=never -d --name " + name + " -p 0.0.0.0:18080:80 " + contractImage, wantErr: "port"},
		{name: "the port twice", w: portWin, argv: "run --pull=never -d --name " + name + " -p " + portGranted + " -p " + portGranted + " " + contractImage, wantErr: "port"},
		{name: "--publish is not -p", w: portWin, argv: "run --pull=never -d --name " + name + " --publish " + portGranted + " " + contractImage, wantErr: "contractArgvRule"},
		{name: "a window that grants a port on every interface grants nothing", w: windowOf(name, "0.0.0.0:18080:80"),
			argv: "run --pull=never -d --name " + name + " -p 0.0.0.0:18080:80 " + contractImage, wantErr: "port"},

		// Refused: it is a window for a run, not for another verb.
		{name: "container run", w: nameWin, argv: "container run --pull=never -d --name " + name + " " + contractImage},
		{name: "create", w: nameWin, argv: "create --pull=never --name " + name + " " + contractImage},
		{name: "rm of a stranger", w: nameWin, argv: "rm -f " + idB},
		{name: "rm of our own", w: nameWin, argv: "rm -f " + idA},
		{name: "a volume prune", w: nameWin, argv: "volume prune -f"},
		{name: "a system prune", w: nameWin, argv: "system prune -a -f"},
		{name: "a read, which the standard rule allows and the window does not need", w: nameWin, argv: "ps -a"},
		{name: "a global option", w: nameWin, argv: "--host x run --pull=never -d --name " + name + " " + contractImage},
		{name: "no argv", w: nameWin, argv: ""},
	}
}

func TestLaunchWindowRuleGrantsTheOneRunOfItsWindow(t *testing.T) {
	for _, tt := range windowCases() {
		t.Run(tt.name, func(t *testing.T) {
			argv := strings.Fields(tt.argv)

			err := windowRuleOf(tt.w)(argv)

			switch {
			case tt.ok && err != nil:
				t.Errorf("rule(%q) = %v, want it granted", argv, err)
			case !tt.ok && err == nil:
				t.Errorf("rule(%q) = nil, want a refusal", argv)
			case !tt.ok && tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("rule(%q) = %q, want it to name %q", argv, err, tt.wantErr)
			}
		})
	}
}

// One attempt, whatever the next argv is.
func TestLaunchWindowRuleGrantsOneAttempt(t *testing.T) {
	name := ctrNameOf("c2")
	argv := launchArgv(name, "")
	rule := windowRuleOf(windowOf(name, ""))

	if err := rule(argv); err != nil {
		t.Fatalf("rule(%q) = %v, want the first attempt granted", argv, err)
	}
	if err := rule(argv); err == nil {
		t.Errorf("rule(%q) = nil a second time, want a refusal: the window grants one attempt", argv)
	}
}

func TestLaunchWindowRuleKeepsItsAttemptForTheArgvItGrants(t *testing.T) {
	name := ctrNameOf("c2")
	rule := windowRuleOf(windowOf(name, ""))

	if err := rule(launchArgv(ctrNameOf("c3"), "")); err == nil {
		t.Fatal("rule(another name) = nil, want a refusal")
	}
	if err := rule(launchArgv(name, "")); err != nil {
		t.Errorf("rule(the name of the window) = %v after a refused argv, want the attempt to be still there", err)
	}
}

// The window never reads what it was not given: ours is what the contract owns, and
// the rule asks it only about the references of the argv, never grants by a name the
// window did not announce.
func TestLaunchWindowRuleDoesNotAddAnythingToWhatTheStandardRuleRefuses(t *testing.T) {
	name := ctrNameOf("c2")
	// Each of these is refused by contractArgvRule for a run WITH the label, so the
	// window, which only adds the label, must refuse them too.
	for _, extra := range []string{
		"--privileged", "--mount type=bind,src=/,dst=/h", "--cap-add ALL", "--device /dev/sda",
		"--security-opt seccomp=unconfined", "--pid host", "--userns host", "--volumes-from " + idB,
		"--env-file /etc/shadow", "-e HOME", "-v pgdata:/data", "--network backend",
	} {
		t.Run(extra, func(t *testing.T) {
			argv := strings.Fields("run --pull=never -d " + extra + " --name " + name + " " + contractImage)
			standard := append([]string{"run", "--label", labelOf(labRunID)}, argv[1:]...)
			if err := contractArgvRule(labRunID, func(string) bool { return false })(standard); err == nil {
				t.Fatalf("test premise: the standard rule allows %q", standard)
			}

			if err := windowRuleOf(windowOf(name, ""))(argv); err == nil {
				t.Errorf("the window granted %q, which the standard rule refuses once labelled", argv)
			}
		})
	}
}

// ---- The window on a contract: name taken. ----

func TestExpectRefusalRecordsALaunchTheEngineRefusesForATakenName(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		name := ctrNameOf("c1")
		c, ct, fake := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stderr: "Error: the container name \"" + name + "\" is already in use", Exit: 125}, launchArgv(name, "")...),
			// The name belongs to the container the contract made: it is not a stray.
			replyTo(fakecli.Reply{Stdout: idA + " " + labRunID + "\n"}, strayInspectArgv(name)...))
		c.ours[idA], c.ours[name] = "c1", "c1"
		var (
			ex  enginefixture.Exchange
			err error
		)

		mustReachTheEnd(t, ct, func() {
			ex = c.expectRefusal("campaign", "name-taken", "a launch with a taken name is refused", windowOf(name, ""), func() { err = launchCall(name)() })
		})

		if err == nil {
			t.Error("the launch = nil, want the engine's refusal")
		}
		if ex.Exit != 125 || !strings.Contains(string(ex.Stderr), "already in use") {
			t.Errorf("exchange = exit %d, stderr %q, want the exit code and the words of the engine", ex.Exit, ex.Stderr)
		}
		want := [][]string{launchArgv(name, ""), strayInspectArgv(name)}
		if got := invokedArgs(fake); !sameArgvs(got, want) {
			t.Errorf("engine calls = %q, want the launch and then one look at the name, no removal", got)
		}
		if len(ct.errs) != 0 {
			t.Errorf("reports = %q, want none", ct.errs)
		}
		// The window is closed with the call: the same launch is refused again.
		before := len(fake.Invocations())
		if _, again := c.rec.Run(dockerCmd{Args: launchArgv(name, ""), Combined: true}); !errors.Is(again, errNotContracted) {
			t.Errorf("the launch after the window = %v, want errNotContracted", again)
		}
		ct.errs = nil
		if n := len(fake.Invocations()); n != before {
			t.Errorf("the engine was started %d more time(s) after the window closed", n-before)
		}
	})
}

// ---- The window on a contract: port taken, and what the engine leaves behind. ----

func TestExpectRefusalCleansWhatTheEngineLeftBehind(t *testing.T) {
	tests := []struct {
		name     string
		launch   fakecli.Reply
		lookup   fakecli.Reply
		wantRm   bool
		wantErrs []string // lower-case substrings of the reports; none = no report
	}{
		{
			name:   "docker creates the container, prints its id, then fails: removed by that id",
			launch: fakecli.Reply{Stdout: idB + "\n", Stderr: "Bind for 127.0.0.1:18080 failed: port is already allocated", Exit: 125},
			lookup: fakecli.Reply{Stdout: idB + " \n"}, wantRm: true,
		},
		{
			name:   "podman leaves nothing: no removal",
			launch: fakecli.Reply{Stderr: "Error: cannot bind tcp port :18080: address already in use", Exit: 126},
			lookup: fakecli.Reply{Stderr: "Error: no such object", Exit: 1},
		},
		{
			name:   "the engine accepted the launch: removed at once, and reported",
			launch: fakecli.Reply{Stdout: idB + "\n"},
			lookup: fakecli.Reply{Stdout: idB + " \n"}, wantRm: true, wantErrs: []string{"refus"},
		},
		{
			name:   "a container of that name with the label of another run is left alone and reported",
			launch: fakecli.Reply{Stderr: "in use", Exit: 125},
			lookup: fakecli.Reply{Stdout: idB + " " + foreignRunID + "\n"}, wantErrs: []string{strings.ToLower(idB), "label"},
		},
		{
			name:   "a container of that name with the label of this run is the business of the cleanup",
			launch: fakecli.Reply{Stderr: "in use", Exit: 125},
			lookup: fakecli.Reply{Stdout: idB + " " + labRunID + "\n"},
		},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				name := ctrNameOf("c2")
				c, ct, fake := startOnFake(t, shape,
					replyTo(tt.launch, launchArgv(name, portGranted)...),
					replyTo(tt.lookup, strayInspectArgv(name)...),
					replyTo(fakecli.Reply{}, "rm", "-f", idB))

				mustReachTheEnd(t, ct, func() {
					c.expectRefusal("campaign", "port-taken", "a launch with a taken port is refused", windowOf(name, portGranted), func() { _ = launchCall(name, "18080:80")() })
				})

				want := [][]string{launchArgv(name, portGranted), strayInspectArgv(name)}
				if tt.wantRm {
					want = append(want, []string{"rm", "-f", idB})
				}
				if got := invokedArgs(fake); !sameArgvs(got, want) {
					t.Errorf("engine calls = %q, want %q", got, want)
				}
				joined := strings.ToLower(strings.Join(ct.errs, "\n"))
				if len(tt.wantErrs) == 0 && len(ct.errs) != 0 {
					t.Errorf("reports = %q, want none", ct.errs)
				}
				for _, w := range tt.wantErrs {
					if !strings.Contains(joined, w) {
						t.Errorf("reports = %q, want one that says %q", ct.errs, w)
					}
				}
			})
		}
	})
}

// A rule that refuses the call (another name) leaves nothing to record: the contract
// ends, the engine never saw the launch, and the lookup of the name still happens.
func TestExpectRefusalEndsWhenTheCallIsNotTheOneOfTheWindow(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		name := ctrNameOf("c2")
		c, ct, fake := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stderr: "no such object", Exit: 1}, strayInspectArgv(name)...))

		ended := ct.run(func() {
			c.expectRefusal("campaign", "name-taken", "a launch of another name", windowOf(name, ""), func() { _ = launchCall(ctrNameOf("c3"))() })
		})

		if !ended {
			t.Error("the contract went on: no exchange happened in the window, a fixture is one exchange")
		}
		if len(ct.errs) == 0 {
			t.Error("the refusal of the rule was not reported")
		}
		if got := invokedArgs(fake); !sameArgvs(got, [][]string{strayInspectArgv(name)}) {
			t.Errorf("engine calls = %q, want the lookup of the name only: the launch of another name must not reach the engine", got)
		}
	})
}

// The window relaxes nothing else: inside it, a removal of a stranger, a prune and a
// volume the run does not own are refused as ever.
func TestExpectRefusalDoesNotLetTheCallDoAnythingElse(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		name := ctrNameOf("c2")
		c, ct, fake := startOnFake(t, shape,
			replyTo(fakecli.Reply{Stderr: "in use", Exit: 125}, launchArgv(name, "")...),
			replyTo(fakecli.Reply{Stderr: "no such object", Exit: 1}, strayInspectArgv(name)...))

		mustReachTheEnd(t, ct, func() {
			c.expectRefusal("campaign", "name-taken", "a launch that also tries other things", windowOf(name, ""), func() {
				_ = launchCall(name)()
				_ = RemoveContainer(idB, true)
				_, _ = PruneVolumes()
				_ = RemoveVolume("pgdata")
			})
		})

		if len(ct.errs) != 3 {
			t.Errorf("reports = %q, want exactly the three refusals", ct.errs)
		}
		want := [][]string{launchArgv(name, ""), strayInspectArgv(name)}
		if got := invokedArgs(fake); !sameArgvs(got, want) {
			t.Errorf("engine calls = %q, want %q", got, want)
		}
	})
}

// Outside any window the launch of DevDesk is refused: nothing in the helpers makes
// a launch without a label legal by default.
func TestALaunchWithoutALabelIsRefusedOutsideAWindow(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		_, ct, fake := startOnFake(t, shape)

		err := launchCall(ctrNameOf("c2"))()

		if !errors.Is(err, errNotContracted) {
			t.Errorf("the launch = %v, want errNotContracted", err)
		}
		if len(ct.errs) != 1 || len(fake.Invocations()) != 0 {
			t.Errorf("reports = %q, engine calls = %q, want one report and no engine call", ct.errs, invokedArgs(fake))
		}
	})
}

// A window is declared whole before anything runs: a name that is not of the run or a
// port that is not one fixed loopback port ends the contract with no engine call.
func TestExpectRefusalRefusesAnInvalidWindowBeforeAnyCall(t *testing.T) {
	tests := []struct {
		name string
		w    launchWindow
	}{
		{"no name", windowOf("", "")},
		{"a name without the prefix", windowOf("web", "")},
		{"the name of another run", windowOf("ddc-"+foreignRunID+"-c1", "")},
		{"the prefix alone", windowOf("ddc-"+labRunID+"-", "")},
		{"a port on every interface by omission", windowOf(ctrNameOf("c2"), "18080:80")},
		{"a port on 0.0.0.0", windowOf(ctrNameOf("c2"), "0.0.0.0:18080:80")},
		{"a port the engine would pick", windowOf(ctrNameOf("c2"), "127.0.0.1::80")},
		{"a port with no container side", windowOf(ctrNameOf("c2"), "127.0.0.1:18080")},
		{"a port that is not a number", windowOf(ctrNameOf("c2"), "127.0.0.1:abc:80")},
		{"an IPv6 loopback", windowOf(ctrNameOf("c2"), "[::1]:18080:80")},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c, ct, fake := startOnFake(t, shape)
				called := false

				ended := ct.run(func() {
					c.expectRefusal("campaign", "x", "an invalid window", tt.w, func() { called = true })
				})

				if !ended || len(ct.fatals) != 1 {
					t.Fatalf("ended = %v, fatals = %q, want one Fatalf", ended, ct.fatals)
				}
				if called || len(fake.Invocations()) != 0 {
					t.Errorf("called = %v, engine calls = %q, want nothing", called, invokedArgs(fake))
				}
			})
		}
	})
}

func TestExpectRefusalNeedsTheLabMarkersAndThePinnedImage(t *testing.T) {
	tests := []struct {
		name   string
		marker bool
		runVar bool
		image  string
		want   string
	}{
		{"no marker file", false, true, contractImage, enginecontract.LabMarkerFile},
		{"no run id variable", true, false, contractImage, enginecontract.RunEnv},
		{"no image", true, true, "", enginecontract.ImageEnv},
		{"an image that is a tag", true, true, "alpine:3.20", enginecontract.ImageEnv},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				setLab(t, tt.marker, tt.runVar)
				t.Setenv(enginecontract.ImageEnv, tt.image)
				q := queued(t) // no step: nothing may reach the engine
				ct := &contractT{TB: t}
				c := newContract(ct, sessionOf(shape, labRunID))
				called := false

				ended := ct.run(func() {
					c.expectRefusal("campaign", "x", "a window outside a lab", windowOf(ctrNameOf("c2"), ""), func() { called = true })
				})

				if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], tt.want) {
					t.Fatalf("ended = %v, fatals = %q, want one Fatalf that names %q", ended, ct.fatals, tt.want)
				}
				if called || len(q.calls) != 0 {
					t.Errorf("called = %v, engine calls = %v, want nothing", called, q.calls)
				}
			})
		}
	})
}

// ---- rmi of an image a container of the run uses. ----

func TestRmiWindowRuleGrantsTheOneArgvOfItsImageOnce(t *testing.T) {
	rule := rmiWindowRule(idImg)
	argv := []string{"rmi", idImg}

	if err := rule(argv); err != nil {
		t.Fatalf("rule(%q) = %v, want it granted", argv, err)
	}
	if err := rule(argv); err == nil {
		t.Errorf("rule(%q) = nil a second time, want a refusal: the window grants one attempt", argv)
	}
}

func TestRmiWindowRuleRefusesEverythingElse(t *testing.T) {
	tests := []struct {
		name string
		argv string
	}{
		{"-f, which would remove the image from under the container", "rmi -f " + idImg},
		{"--force", "rmi --force " + idImg},
		{"another image", "rmi " + idB},
		{"two images", "rmi " + idImg + " " + idB},
		{"the image by its tag", "rmi alpine:3.20"},
		{"the image by the pinned reference", "rmi " + contractImage},
		{"no image", "rmi"},
		{"--all", "rmi --all"},
		{"image rm", "image rm " + idImg},
		{"image rmi", "image rmi " + idImg},
		{"image prune", "image prune -a -f"},
		{"a container removal", "rm -f " + idA},
		{"a read", "image ls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv := strings.Fields(tt.argv)

			if err := rmiWindowRule(idImg)(argv); err == nil {
				t.Errorf("rule(%q) = nil, want a refusal: the window grants `rmi <id>` of one image and nothing else", argv)
			}
		})
	}
	t.Run("a window for something that is not an image id grants nothing", func(t *testing.T) {
		for _, bad := range []string{"", "abc", "alpine:3.20", strings.ToUpper(idImg), "sha256:" + idImg} {
			if err := rmiWindowRule(bad)([]string{"rmi", bad}); err == nil {
				t.Errorf("rmiWindowRule(%q) granted rmi %s", bad, bad)
			}
		}
	})
}

// usedImageSteps are the answers that make a contract own a container of the pinned
// image and know the id of that image.
func usedImageSteps(shape engine.Shape) []fakecli.Exchange {
	return []fakecli.Exchange{
		replyTo(fakecli.Reply{Stdout: idA + "\n"}, createArgv(labRunID, "c1")...),
		replyTo(fakecli.Reply{Stdout: imageAnswer(shape, idImg)}, imageInspectArgv()...),
	}
}

func TestExpectRmiRefusalRecordsTheRefusalOfAnImageInUse(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		steps := append(usedImageSteps(shape),
			replyTo(fakecli.Reply{Stdout: idA + "\n"}, ancestorArgv(idImg)...),
			replyTo(fakecli.Reply{Stderr: "Error: image used by " + idA + ": image is in use by a container", Exit: 2}, "rmi", idImg))
		c, ct, fake := startOnFake(t, shape, steps...)
		var (
			imageID string
			ex      enginefixture.Exchange
			err     error
		)

		mustReachTheEnd(t, ct, func() {
			_, imageID = c.usedImage("c1")
			ex = c.expectRmiRefusal("campaign", "rmi-used", "rmi of an image a container uses is refused", imageID, func() { err = RemoveImage(imageID, false) })
		})

		if err == nil || ex.Exit == 0 || !strings.Contains(string(ex.Stderr), "in use") {
			t.Errorf("rmi = %v, exchange = exit %d stderr %q, want the engine's refusal", err, ex.Exit, ex.Stderr)
		}
		want := [][]string{createArgv(labRunID, "c1"), imageInspectArgv(), ancestorArgv(idImg), {"rmi", idImg}}
		if got := invokedArgs(fake); !sameArgvs(got, want) {
			t.Errorf("engine calls = %q, want %q: the use of the image is read right before the rmi", got, want)
		}
		if len(ct.errs) != 0 {
			t.Errorf("reports = %q, want none", ct.errs)
		}
		before := len(fake.Invocations())
		if _, again := c.rec.Run(dockerCmd{Args: []string{"rmi", idImg}, Combined: true}); !errors.Is(again, errNotContracted) {
			t.Errorf("the rmi after the window = %v, want errNotContracted", again)
		}
		ct.errs = nil
		if n := len(fake.Invocations()); n != before {
			t.Errorf("the engine was started %d more time(s) after the window closed", n-before)
		}
	})
}

func TestExpectRmiRefusalDoesNotSendTheRmiUnlessAContainerOfTheRunUsesTheImage(t *testing.T) {
	tests := []struct {
		name     string
		ancestor string
	}{
		{"nobody uses it any more", ""},
		{"only a stranger's container uses it", idB + "\n"},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				steps := append(usedImageSteps(shape), replyTo(fakecli.Reply{Stdout: tt.ancestor}, ancestorArgv(idImg)...))
				c, ct, fake := startOnFake(t, shape, steps...)
				called := false

				ended := ct.run(func() {
					_, imageID := c.usedImage("c1")
					c.expectRmiRefusal("campaign", "rmi-used", "rmi of an image nobody uses", imageID, func() { called = true })
				})

				if !ended || len(ct.fatals) != 1 {
					t.Fatalf("ended = %v, fatals = %q, want one Fatalf: without a user, rmi would remove the image of every contract", ended, ct.fatals)
				}
				if called {
					t.Error("the call ran")
				}
				want := [][]string{createArgv(labRunID, "c1"), imageInspectArgv(), ancestorArgv(idImg)}
				if got := invokedArgs(fake); !sameArgvs(got, want) {
					t.Errorf("engine calls = %q, want %q and no rmi", got, want)
				}
			})
		}
	})
}

func TestExpectRmiRefusalOnlyKnowsTheImageUsedImageReturned(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		c, ct, fake := startOnFake(t, shape)
		called := false

		ended := ct.run(func() {
			c.expectRmiRefusal("campaign", "rmi-used", "rmi of an image nobody announced", idImg, func() { called = true })
		})

		if !ended || len(ct.fatals) != 1 {
			t.Fatalf("ended = %v, fatals = %q, want one Fatalf", ended, ct.fatals)
		}
		if called || len(fake.Invocations()) != 0 {
			t.Errorf("called = %v, engine calls = %q, want nothing", called, invokedArgs(fake))
		}
	})
}

func TestExpectRmiRefusalReportsAnImageThatWasRemoved(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		steps := append(usedImageSteps(shape),
			replyTo(fakecli.Reply{Stdout: idA + "\n"}, ancestorArgv(idImg)...),
			replyTo(fakecli.Reply{Stdout: "Untagged: x\n"}, "rmi", idImg))
		c, ct, _ := startOnFake(t, shape, steps...)

		mustReachTheEnd(t, ct, func() {
			_, imageID := c.usedImage("c1")
			c.expectRmiRefusal("campaign", "rmi-used", "an rmi the engine accepted", imageID, func() { _ = RemoveImage(imageID, false) })
		})

		if len(ct.errs) != 1 || !strings.Contains(strings.ToLower(ct.errs[0]), "removed") {
			t.Errorf("reports = %q, want one that says the image was removed: the pinned image is gone for the next contract", ct.errs)
		}
	})
}

func TestExpectRmiRefusalDoesNotLetTheCallForceTheRemoval(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		steps := append(usedImageSteps(shape), replyTo(fakecli.Reply{Stdout: idA + "\n"}, ancestorArgv(idImg)...))
		c, ct, fake := startOnFake(t, shape, steps...)

		ended := ct.run(func() {
			_, imageID := c.usedImage("c1")
			c.expectRmiRefusal("campaign", "rmi-used", "a forced rmi", imageID, func() { _ = RemoveImage(imageID, true) })
		})

		if !ended || len(ct.errs) == 0 {
			t.Errorf("ended = %v, reports = %q, want a refusal reported and the contract ended (no exchange)", ended, ct.errs)
		}
		for _, argv := range invokedArgs(fake) {
			if len(argv) > 0 && argv[0] == "rmi" {
				t.Errorf("the engine was started with %q: a forced rmi must not reach it", argv)
			}
		}
	})
}

func TestExpectRmiRefusalNeedsTheLabMarkers(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		setLab(t, false, true)
		q := queued(t)
		ct := &contractT{TB: t}
		c := newContract(ct, sessionOf(shape, labRunID))

		ended := ct.run(func() {
			c.expectRmiRefusal("campaign", "rmi-used", "rmi outside a lab", idImg, func() {})
		})

		if !ended || len(ct.fatals) != 1 || len(q.calls) != 0 {
			t.Errorf("ended = %v, fatals = %q, engine calls = %v, want one Fatalf and no call", ended, ct.fatals, q.calls)
		}
	})
}
