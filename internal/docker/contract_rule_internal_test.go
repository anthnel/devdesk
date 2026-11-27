package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09a2 (DEVDESK-943), step 7 of plan OC-09a: the recording runner and the argv
// rule. Every call a contract makes to the engine goes through the recording runner,
// which asks the rule before it starts anything. The rule is an allow list: what it
// does not know it refuses, and a refusal happens BEFORE the engine is reached.
// The signatures are in contract_helpers_internal_test.go.
//
// The recorder is tested on fakecli (a copy of the test binary that answers as the
// engine, with nothing else on PATH), the rule as a pure function.

// allowAll is the rule of a recorder under test for what it does with the bytes.
func allowAll([]string) error { return nil }

func TestRecordingRunnerKeepsWhatTheEngineAnswered(t *testing.T) {
	tests := []struct {
		name     string
		argv     []string
		combined bool
		reply    fakecli.Reply
		wantOut  string
		wantExit int
		wantErr  bool
		wantLog  exchangeLog
	}{
		{
			name:    "success, stdout only",
			argv:    []string{"version"},
			reply:   fakecli.Reply{Stdout: "a\n"},
			wantOut: "a\n",
			wantLog: exchangeLog{Argv: []string{"version"}, Stdout: []byte("a\n")},
		},
		{
			name:     "failure keeps stderr on the ExitError",
			argv:     []string{"rm", idA},
			reply:    fakecli.Reply{Stderr: "boom", Exit: 2},
			wantExit: 2, wantErr: true,
			wantLog: exchangeLog{Argv: []string{"rm", idA}, Exit: 2, Stderr: []byte("boom")},
		},
		{
			name:     "combined success: stdout then stderr",
			argv:     []string{"ps"},
			combined: true,
			reply:    fakecli.Reply{Stdout: "out\n", Stderr: "warn\n"},
			wantOut:  "out\nwarn\n",
			wantLog:  exchangeLog{Argv: []string{"ps"}, Combined: true, Stdout: []byte("out\n"), Stderr: []byte("warn\n")},
		},
		{
			name:     "combined failure: ExitError.Stderr stays empty, as with CombinedOutput",
			argv:     []string{"rm", idA},
			combined: true,
			reply:    fakecli.Reply{Stdout: "o\n", Stderr: "e\n", Exit: 3},
			wantOut:  "o\ne\n", wantExit: 3, wantErr: true,
			wantLog: exchangeLog{Argv: []string{"rm", idA}, Combined: true, Exit: 3, Stdout: []byte("o\n"), Stderr: []byte("e\n")},
		},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				fake := fakecli.Install(t, fakecli.Scenario{{Tool: shape.Binary, Args: tt.argv, Reply: tt.reply}}, shape.Binary)
				rr := newRecordingRunner(t, allowAll)

				out, err := rr.Run(dockerCmd{Args: tt.argv, Combined: tt.combined})

				if string(out) != tt.wantOut {
					t.Errorf("Run() bytes = %q, want %q", out, tt.wantOut)
				}
				if (err != nil) != tt.wantErr {
					t.Fatalf("Run() error = %v, want an error: %v", err, tt.wantErr)
				}
				if err != nil {
					if code, ok := exitCodeOf(err); !ok || code != tt.wantExit {
						t.Errorf("exit code = %d (%v), want %d, read through interface{ ExitCode() int }", code, ok, tt.wantExit)
					}
					stderr, _ := exitStderrOf(err)
					want := ""
					if !tt.combined {
						want = tt.reply.Stderr
					}
					if string(stderr) != want {
						t.Errorf("ExitError.Stderr = %q, want %q", stderr, want)
					}
				}
				if len(rr.log) != 1 {
					t.Fatalf("the journal holds %d exchanges, want 1: %+v", len(rr.log), rr.log)
				}
				got := rr.log[0]
				if !slices.Equal(got.Argv, tt.wantLog.Argv) || got.Combined != tt.wantLog.Combined || got.Exit != tt.wantLog.Exit ||
					!bytes.Equal(got.Stdout, tt.wantLog.Stdout) || !bytes.Equal(got.Stderr, tt.wantLog.Stderr) {
					t.Errorf("journal entry = %+v, want %+v", got, tt.wantLog)
				}
				if n := len(fake.Invocations()); n != 1 {
					t.Errorf("the engine was started %d times, want 1", n)
				}
			})
		}
	})
}

// The recorder must be indistinguishable from cliRunner for the code under
// contract, or what a contract records is not what DevDesk would have seen.
func TestRecordingRunnerEqualsTheCLIRunnerOnEverySyntheticFixture(t *testing.T) {
	fixtures := syntheticFixtures(t)
	if len(fixtures) < 8 {
		t.Fatalf("%d synthetic fixtures, want the 8 of OC-08a", len(fixtures))
	}
	for _, f := range fixtures {
		for _, combined := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/combined=%v", f.Name, combined), func(t *testing.T) {
				usingEngine(t, string(f.Meta.Tool))
				dc := dockerCmd{Args: f.Exchange.Argv, Combined: combined}

				cli := viaCLI(t, f, dc)
				var rec outcome
				t.Run("recording", func(t *testing.T) {
					usingEngine(t, string(f.Meta.Tool))
					fakecli.Install(t, fakecli.Fixtures(t, f), string(f.Meta.Tool))
					out, err := newRecordingRunner(t, allowAll).Run(dc)
					rec = outcome{out, err}
				})

				sameOutcome(t, rec, cli)
			})
		}
	}
}

func TestRecordingRunnerRefusesWhatItCannotRecordWithoutStartingAnything(t *testing.T) {
	tests := []struct {
		name  string
		dc    dockerCmd
		allow func([]string) error
		want  string // in the report
	}{
		{"a credential helper", dockerCmd{Args: []string{"get"}, Helper: "fake"}, allowAll, "helper"},
		{"standard input", dockerCmd{Args: []string{"version"}, Stdin: "secret"}, allowAll, "stdin"},
		{"a context", dockerCmd{Args: []string{"version"}, Ctx: context.Background()}, allowAll, "context"},
		{"an argv the rule refuses", dockerCmd{Args: []string{"system", "prune", "-f"}}, func([]string) error { return errors.New("no thanks") }, "no thanks"},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				fake := fakecli.Install(t, fakecli.Scenario{}, shape.Binary)
				ct := &contractT{TB: t}
				rr := newRecordingRunner(ct, tt.allow)

				out, err := rr.Run(tt.dc)

				if !errors.Is(err, errNotContracted) {
					t.Errorf("Run() error = %v, want errNotContracted", err)
				}
				if len(out) != 0 {
					t.Errorf("Run() = %q, want nothing", out)
				}
				if len(ct.errs) != 1 || !strings.Contains(strings.ToLower(ct.errs[0]), tt.want) {
					t.Errorf("reports = %q, want one that says %q: the refusal must fail the contract, not only return an error nobody reads", ct.errs, tt.want)
				}
				if n := len(fake.Invocations()); n != 0 {
					t.Errorf("the engine was started %d time(s): a refusal comes before anything runs", n)
				}
				if len(rr.log) != 0 {
					t.Errorf("a refused call was journaled: %+v", rr.log)
				}
			})
		}
	})
}

func TestRecordingRunnerDoesNotBuildInteractiveCommands(t *testing.T) {
	ct := &contractT{TB: t}
	rr := newRecordingRunner(ct, allowAll)

	var built *exec.Cmd
	ended := ct.run(func() { built = rr.Build("exec", "-it", idA, "sh") })

	if !ended || len(ct.fatals) != 1 {
		t.Fatalf("Build ended the test = %v, fatals = %q, want a Fatalf: interactive commands are not contracted", ended, ct.fatals)
	}
	if built != nil {
		t.Errorf("Build returned a command, %v", built.Args)
	}
}

func TestRecordingRunnerLookPathGoesToTheEngineBinary(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		fakecli.Install(t, fakecli.Scenario{}, shape.Binary)

		if err := newRecordingRunner(t, allowAll).LookPath(); err != nil {
			t.Errorf("LookPath() = %v with %s on PATH", err, shape.Binary)
		}
	})
}

// ---- The rule. ----

func TestArgvRule(t *testing.T) {
	name := func(role string) string { return "ddc-" + labRunID + "-" + role }
	ours := map[string]bool{idA: true, name("c1"): true, name("v1"): true}
	rule := contractArgvRule(labRunID, func(ref string) bool { return ours[ref] })
	label := labelOf(labRunID)
	idFormat := []string{"--format", `{{index .Config.Labels "devdesk.test"}}`}

	words := func(s string) []string { return strings.Fields(s) }
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	tests := []struct {
		name    string
		argv    []string
		wantErr string // substring of the refusal; "" when the argv is allowed
	}{
		// What only reads is allowed, on any resource: the cleanup inspects candidates
		// it does not own yet, to find out whose they are.
		{"ps -a", words("ps -a"), ""},
		{"version", words("version"), ""},
		{"info with a template", []string{"info", "--format", "{{.Host.Security.Rootless}}"}, ""},
		{"volume ls", words("volume ls"), ""},
		{"network ls with the filter", cat(words("network ls -q --no-trunc --filter"), []string{"label=" + label}), ""},
		{"ps with the filter", cat(words("ps -a -q --no-trunc --filter"), []string{"label=" + label}), ""},
		{"inspect of one of ours", cat(words("inspect --type container"), idFormat, []string{idA}), ""},
		{"inspect of a stranger's container", cat(words("inspect --type container"), idFormat, []string{idB}), ""},
		{"volume inspect", cat(words("volume inspect --format"), []string{`{{index .Labels "devdesk.test"}}`, "other"}), ""},
		{"network inspect", words("network inspect " + idB), ""},
		{"logs", words("logs " + idA), ""},
		{"images", words("images"), ""},
		{"stats without a stream", words("stats --no-stream"), ""},

		// Creation needs the exact label of this run, and run and create need --pull=never.
		{"run with everything", words("run -d --pull=never --label " + label + " --name " + name("c1") + " img sleep 1"), ""},
		{"run, label as --label=", words("run -d --pull=never --label=" + label + " img sleep 1"), ""},
		{"run, label as -l", words("run -d --pull=never -l " + label + " img sleep 1"), ""},
		{"run without the label", words("run -d --pull=never --name " + name("c1") + " img sleep 1"), "label"},
		{"run with the label of another run", words("run -d --pull=never --label " + labelOf(foreignRunID) + " img sleep 1"), "label"},
		{"run with a label that only starts like ours", words("run -d --pull=never --label " + label + "0 img sleep 1"), "label"},
		{"run with an empty label value", words("run -d --pull=never --label " + enginecontract.LabelKey + "= img sleep 1"), "label"},
		{"run with another key", words("run -d --pull=never --label other=" + labRunID + " img sleep 1"), "label"},
		{"run without --pull=never", words("run -d --label " + label + " img sleep 1"), "--pull=never"},
		{"run with --pull=always", words("run -d --pull=always --label " + label + " img sleep 1"), "--pull=never"},
		{"run with --pull=missing", words("run -d --pull=missing --label " + label + " img sleep 1"), "--pull=never"},
		{"create with the label and --pull=never", words("create --pull=never --label " + label + " img"), ""},
		{"create without the label", words("create --pull=never img"), "label"},
		{"create without --pull=never", words("create --label " + label + " img"), "--pull=never"},
		{"container run without the label", words("container run -d --pull=never img sleep 1"), "label"},
		{"container create without the label", words("container create --pull=never img"), "label"},
		{"container run with everything", words("container run -d --pull=never --label " + label + " img sleep 1"), ""},
		{"volume create with the label", words("volume create --label " + label + " " + name("v1")), ""},
		{"volume create without the label", words("volume create " + name("v1")), "label"},
		{"network create with the label", words("network create --label " + label + " " + name("n1")), ""},
		{"network create without the label", words("network create " + name("n1")), "label"},
		{"pod create without the label", words("pod create --name p"), "label"},
		{"pod create with the label", words("pod create --label " + label + " p"), ""},

		// Destruction and state changes reach only what this run created.
		{"rm -f of ours", words("rm -f " + idA), ""},
		{"rm of ours by name", words("rm " + name("c1")), ""},
		{"stop of ours", words("stop " + idA), ""},
		{"container rm of ours", words("container rm -f " + idA), ""},
		{"rm -f of a stranger", words("rm -f " + idB), idB},
		{"rm of ours and a stranger", words("rm -f " + idA + " " + idB), idB},
		{"rm by a prefix of ours", words("rm -f " + idA[:12]), idA[:12]},
		{"rm -a", words("rm -a"), "-a"},
		{"rm --all", words("rm --all"), "--all"},
		{"rm with no target", words("rm -f"), "target"},
		{"kill of a stranger", words("kill " + idC), idC},
		{"restart of a stranger", words("restart " + idB), idB},
		{"volume rm of ours", words("volume rm " + name("v1")), ""},
		{"volume rm of a stranger", words("volume rm pgdata"), "pgdata"},
		{"network rm of a name that is not ours", words("network rm " + name("n9")), name("n9")},
		{"network connect to a stranger", words("network connect bridge " + idB), idB},
		{"rmi, images are never ours", words("rmi " + idA), idA},

		// Refused as a whole.
		{"volume prune", words("volume prune -f"), "prune"},
		{"volume prune -a", words("volume prune -a -f"), "prune"},
		{"container prune", words("container prune -f"), "prune"},
		{"image prune", words("image prune -a -f"), "prune"},
		{"network prune", words("network prune -f"), "prune"},
		{"system prune", words("system prune -a -f"), "system"},
		{"system df", words("system df"), "system"},
		{"pull", words("pull alpine"), "pull"},
		{"image pull", words("image pull alpine"), "pull"},
		{"push", words("push img"), "push"},
		{"exec", words("exec " + idA + " sh"), "exec"},
		{"cp", words("cp " + idA + ":/etc/passwd ."), "cp"},
		{"build", words("build ."), "build"},
		{"commit", words("commit " + idA + " img"), "commit"},
		{"tag", words("tag a b"), "tag"},
		{"login", words("login registry.invalid"), "login"},
		{"load", words("load -i x.tar"), "load"},
		{"save", words("save img"), "save"},
		{"export", words("export " + idA), "export"},
		{"import", words("import x.tar"), "import"},

		// What it does not know it refuses, and it says how to teach it.
		{"an unknown verb", words("frobnicate"), "unknown verb"},
		{"no argv at all", nil, "unknown verb"},
		{"an option where the verb should be", words("--debug ps"), "unknown verb"},
		{"a remote engine by --host", words("--host tcp://other:2375 ps"), "unknown verb"},
		{"a remote engine by -H", words("-H tcp://other:2375 ps"), "unknown verb"},
		{"a context", words("--context prod ps"), "unknown verb"},
		{"a connection", words("--connection prod ps"), "unknown verb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rule(tt.argv)

			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("rule(%q) = %v, want it allowed", tt.argv, err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("rule(%q) = nil, want a refusal naming %q", tt.argv, tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Errorf("rule(%q) = %q, want it to name %q", tt.argv, err, tt.wantErr)
			}
		})
	}
}

// An unknown verb says what to do about it: the list is the contract of the run, and
// a verb is added to it with its reason, not worked around.
func TestArgvRuleSaysHowToAddAVerb(t *testing.T) {
	rule := contractArgvRule(labRunID, func(string) bool { return false })

	err := rule([]string{"frobnicate"})

	if err == nil || !strings.Contains(err.Error(), "contractArgvRule") {
		t.Errorf("rule(frobnicate) = %v, want a refusal that points to contractArgvRule, where the verb is added with its reason", err)
	}
}

// The rule is bound to the run: what is ours is decided by the contract's own
// resources, and nothing else.
func TestArgvRuleAsksTheContractWhatIsItsOwn(t *testing.T) {
	var asked []string
	rule := contractArgvRule(labRunID, func(ref string) bool {
		asked = append(asked, ref)
		return ref == idA
	})

	if err := rule([]string{"rm", "-f", idA}); err != nil {
		t.Errorf("rule(rm -f A) = %v, want it allowed", err)
	}
	if err := rule([]string{"rm", "-f", idB}); err == nil {
		t.Error("rule(rm -f B) = nil, want a refusal: B is not of this run")
	}
	if !slices.Contains(asked, idA) || !slices.Contains(asked, idB) {
		t.Errorf("the rule asked about %q, want it to ask about each target", asked)
	}
}

// The recorder of a contract, with its rule and its resources, end to end on
// fakecli: what is not of the run is refused before the engine, what is its own
// goes through.
func TestTheRecorderOfAContractRefusesWhatIsNotOfItsRun(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		openLab(t)
		t.Setenv(enginecontract.ImageEnv, contractImage)
		runArgv := createArgv(labRunID, "c1")
		scenario := fakecli.Scenario{
			{Tool: shape.Binary, Args: runArgv, Reply: fakecli.Reply{Stdout: idA + "\n"}},
			{Tool: shape.Binary, Args: []string{"rm", "-f", idA}},
		}
		// The cleanup at the end of the contract lists the three kinds, and finds nothing.
		for _, k := range resourceKinds(labRunID) {
			scenario = append(scenario, fakecli.Exchange{Tool: shape.Binary, Args: k.list})
		}
		fake := fakecli.Install(t, scenario, shape.Binary)
		ct := &contractT{TB: t}
		c := startContract(ct, labSession(enginefixture.Tool(shape.Name), labRunID))

		id := c.container("c1")
		_, errMine := c.rec.Run(dockerCmd{Args: []string{"rm", "-f", id}, Combined: true})
		_, errStranger := c.rec.Run(dockerCmd{Args: []string{"rm", "-f", idB}, Combined: true})
		_, errPrune := c.rec.Run(dockerCmd{Args: []string{"volume", "prune", "-f"}, Combined: true})
		ct.runCleanups()

		if errMine != nil {
			t.Errorf("rm -f of the container this contract created = %v, want it to go through", errMine)
		}
		if !errors.Is(errStranger, errNotContracted) || !errors.Is(errPrune, errNotContracted) {
			t.Errorf("rm -f of a stranger = %v, volume prune = %v, want errNotContracted for both", errStranger, errPrune)
		}
		if len(ct.errs) != 2 {
			t.Errorf("reports = %q, want exactly the two refusals", ct.errs)
		}
		for _, inv := range fake.Invocations() {
			if slices.Contains(inv.Args, idB) || slices.Contains(inv.Args, "prune") {
				t.Errorf("the engine was started with %q: a refused argv must not reach it", inv.Args)
			}
		}
	})
}
