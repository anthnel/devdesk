package docker

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-08a criterion 6: serving a fixture through fakecli gives the code under test
// the same bytes as the in-memory replay.
//
// The reference is the real path: cliRunner starts a real process, a copy of
// the test binary answering as the engine (fakecli.Install with
// fakecli.Fixtures). The replay (fixtureRunner) is the quick path unit tests
// use. Every synthetic fixture goes through both, with Combined false then
// true, and the two must agree on the bytes, the error text, the exit code and
// ExitError.Stderr. The divergence the fixtureExit error can have from a real
// *exec.ExitError is therefore measured, not assumed.

// outcome is what one Run produced.
type outcome struct {
	out []byte
	err error
}

// sameOutcome compares what a caller can observe of two Runs.
func sameOutcome(t *testing.T, replay, cli outcome) {
	t.Helper()
	if !bytes.Equal(replay.out, cli.out) {
		t.Errorf("bytes differ\n replay: %q\n    cli: %q", replay.out, cli.out)
	}
	if (replay.err == nil) != (cli.err == nil) {
		t.Fatalf("error differs: replay %v, cli %v", replay.err, cli.err)
	}
	if replay.err == nil {
		return
	}
	if replay.err.Error() != cli.err.Error() {
		t.Errorf("error text differs: replay %q, cli %q", replay.err, cli.err)
	}
	rc, rok := exitCodeOf(replay.err)
	cc, cok := exitCodeOf(cli.err)
	if rok != cok || rc != cc {
		t.Errorf("ExitCode() differs: replay %d (%v), cli %d (%v)", rc, rok, cc, cok)
	}
	rs, rok := exitStderrOf(replay.err)
	cs, cok := exitStderrOf(cli.err)
	if rok != cok || !bytes.Equal(rs, cs) {
		t.Errorf("ExitError.Stderr differs: replay %q (%v), cli %q (%v)", rs, rok, cs, cok)
	}
}

// viaReplay runs dc through a fixtureRunner holding f.
func viaReplay(t *testing.T, f enginefixture.Fixture, dc dockerCmd) outcome {
	t.Helper()
	out, err := useFixtures(t, f).Run(dc)
	return outcome{out, err}
}

// viaCLI runs dc through the real cliRunner against a fake engine serving f.
func viaCLI(t *testing.T, f enginefixture.Fixture, dc dockerCmd) outcome {
	t.Helper()
	fakecli.Install(t, fakecli.Fixtures(t, f), string(f.Meta.Tool))
	out, err := cliRunner{}.Run(dc)
	return outcome{out, err}
}

// Every synthetic fixture, both modes of capture: the replay equals the real
// process. The binary fixture is the one that fails while fakecli carries its
// reply as JSON text (an invalid byte becomes U+FFFD); this test is the reason
// fakecli moves bytes.
func TestFixtureRunnerEqualsTheCLIRunnerOnEveryFixture(t *testing.T) {
	fixtures := syntheticFixtures(t)
	if len(fixtures) < 8 {
		t.Fatalf("%d synthetic fixtures, want the 8 of OC-08a", len(fixtures))
	}
	for _, f := range fixtures {
		for _, combined := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/combined=%v", f.Name, combined), func(t *testing.T) {
				usingEngine(t, string(f.Meta.Tool))
				dc := dockerCmd{Args: f.Exchange.Argv, Combined: combined}

				replay := viaReplay(t, f, dc)
				cli := viaCLI(t, f, dc)

				sameOutcome(t, replay, cli)
				// The recorded numbers are what both must have produced.
				want := f.Exchange.Stdout
				if combined {
					want = append(slices.Clone(want), f.Exchange.Stderr...)
				}
				if !bytes.Equal(replay.out, want) {
					t.Errorf("replay bytes = %q, want the recording %q", replay.out, want)
				}
				if code, _ := exitCodeOf(replay.err); (replay.err == nil) != (f.Exchange.Exit == 0) || (replay.err != nil && code != f.Exchange.Exit) {
					t.Errorf("replay exit = %d (%v), want the recorded %d", code, replay.err, f.Exchange.Exit)
				}
			})
		}
	}
}

// ---- Through the public functions of the package. ----

// both runs call twice, once in memory and once through the fake engine, each in
// a subtest of its own so the cleanups (runner, engine, PATH) end before the
// next one starts. It returns what each produced.
func both[T any](t *testing.T, f enginefixture.Fixture, call func() T) (replay, cli T) {
	t.Helper()
	t.Run("replay", func(t *testing.T) {
		usingEngine(t, string(f.Meta.Tool))
		useFixtures(t, f)
		replay = call()
	})
	t.Run("cli", func(t *testing.T) {
		usingEngine(t, string(f.Meta.Tool))
		fakecli.Install(t, fakecli.Fixtures(t, f), string(f.Meta.Tool))
		cli = call()
	})
	return replay, cli
}

type inspectResult struct {
	out []byte
	err string
}

func TestInspectContainerIsTheSameThroughBothPaths(t *testing.T) {
	tests := []struct {
		scenario string
		tool     enginefixture.Tool
	}{
		{"inspect-present", enginefixture.Docker},
		{"inspect-absent", enginefixture.Podman},
	}
	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			f := pickFixture(t, tt.scenario, tt.tool)
			call := func() inspectResult {
				out, err := InspectContainer(f.Exchange.Argv[1])
				if err != nil {
					return inspectResult{out, err.Error()}
				}
				return inspectResult{out, ""}
			}

			replay, cli := both(t, f, call)

			if !bytes.Equal(replay.out, cli.out) || replay.err != cli.err {
				t.Errorf("InspectContainer differs\n replay: %q, %q\n    cli: %q, %q", replay.out, replay.err, cli.out, cli.err)
			}
		})
	}
}

// The ps fixture holds the argv DevDesk builds from the engine's template. When
// the template changes the fixture becomes wrong, and this is meant: the message
// says to record it again rather than to edit the test.
func TestThePSFixtureHoldsTheArgvDevDeskBuilds(t *testing.T) {
	f := pickFixture(t, "ps-all", enginefixture.Docker)
	want := []string{"ps", "--all", "--format", engine.ShapeFor(engine.Docker).Templates.PS}

	if !slices.Equal(f.Exchange.Argv, want) {
		t.Errorf("the ps-all fixture holds\n  %q\nbut ListContainers(true) builds\n  %q\n"+
			"engine.Templates.PS changed: record the fixture again (do not edit the test)", f.Exchange.Argv, want)
	}
}

type listResult struct {
	containers []Container
	err        string
}

func TestListContainersIsTheSameThroughBothPaths(t *testing.T) {
	f := pickFixture(t, "ps-all", enginefixture.Docker)
	call := func() listResult {
		containers, err := ListContainers(true)
		if err != nil {
			return listResult{containers, err.Error()}
		}
		return listResult{containers, ""}
	}

	replay, cli := both(t, f, call)

	if !reflect.DeepEqual(replay, cli) {
		t.Errorf("ListContainers differs\n replay: %+v\n    cli: %+v", replay, cli)
	}
	// And it is the recorded list, not an empty one that two paths agree on.
	if replay.err != "" || len(replay.containers) != 3 {
		t.Fatalf("ListContainers = %d containers, %q, want the 3 recorded rows", len(replay.containers), replay.err)
	}
	var names []string
	for _, c := range replay.containers {
		names = append(names, c.Name)
	}
	if want := []string{"ddm-web", "ddm-db", "ddm-cache"}; !slices.Equal(names, want) {
		t.Errorf("names = %q, want %q in the recorded order", names, want)
	}
	if got := replay.containers[0]; got.State != "running" || got.CreatedAt != "2026-11-27 10:00:00 +0000 UTC" || len(got.Ports) == 0 {
		t.Errorf("first container = %+v, want the recorded state, creation time and ports (no normalization)", got)
	}
}
