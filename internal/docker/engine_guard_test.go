package docker

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/engineguard"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-03a (DEVDESK-798), guard G1: cliRunner.LookPath, Run and Build tell
// internal/engineguard which binary they resolved. Outside a directory fakecli
// declared the reach panics (OC-03b, DEVDESK-799); in survey mode (SurveyMode
// here) it is recorded and the call goes on as it did. Nothing here starts a
// real engine (the "real" binary is an empty file).

// useBinary makes cliRunner drive binary, as a configured engine path does.
func useBinary(t *testing.T, binary string) {
	t.Helper()
	previous := engine.Current()
	shape := engine.ShapeFor(engine.Docker)
	shape.Binary = binary
	engine.SetCurrent(shape)
	t.Cleanup(func() { engine.SetCurrent(previous) })
}

// emptyEngine is an executable that is not a fakecli tool and runs nothing.
func emptyEngine(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("an executable is recognised by its extension on Windows")
	}
	path := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// runnerOps are the three operations of cliRunner, each as a call that returns
// the error it ended with.
var runnerOps = []struct {
	op   string
	call func(cliRunner) error
}{
	{"LookPath", func(r cliRunner) error { return r.LookPath() }},
	{"Run", func(r cliRunner) error { _, err := r.Run(dockerCmd{Args: []string{"ps"}}); return err }},
	{"Build", func(r cliRunner) error { _ = r.Build("ps"); return nil }},
}

func TestInSurveyModeCliRunnerRecordsAnEngineOutsideTheFakeCLIDirs(t *testing.T) {
	for _, tt := range runnerOps {
		t.Run(tt.op, func(t *testing.T) {
			engineguard.SurveyMode(t)
			binary := emptyEngine(t)
			useBinary(t, binary)

			err := tt.call(cliRunner{})

			if tt.op == "LookPath" && err != nil {
				t.Errorf("LookPath() = %v: survey mode must not change what the runner answers", err)
			}
			hits := engineguard.Hits()
			if len(hits) != 1 {
				t.Fatalf("Hits() = %+v, want exactly one for %s", hits, tt.op)
			}
			if hits[0].Op != tt.op || hits[0].Binary != binary {
				t.Errorf("Hit = %+v, want operation %q and binary %q", hits[0], tt.op, binary)
			}
			if !strings.Contains(hits[0].Test, "TestInSurveyModeCliRunnerRecordsAnEngineOutsideTheFakeCLIDirs") {
				t.Errorf("Hit.Test = %q, want the test that reached the engine", hits[0].Test)
			}
		})
	}
}

func TestSurveyModeLeavesTheCommandItBuildsAlone(t *testing.T) {
	engineguard.SurveyMode(t)
	binary := emptyEngine(t)
	useBinary(t, binary)

	cmd := cliRunner{}.Build("ps", "-a")

	if cmd.Path != binary || len(cmd.Args) != 3 || cmd.Args[1] != "ps" || cmd.Args[2] != "-a" {
		t.Errorf("Build() = %q %q, want %q ps -a", cmd.Path, cmd.Args, binary)
	}
}

func TestCliRunnerRecordsNothingForTheEnginesOfFakeCLI(t *testing.T) {
	fakecli.Install(t, fakecli.Scenario{{Prefix: true, Reply: fakecli.Reply{Stdout: "ok"}}}, engine.Names()...)

	for _, name := range engine.Names() {
		for _, tt := range runnerOps {
			t.Run(name+"/"+tt.op, func(t *testing.T) {
				engineguard.Isolate(t)
				useBinary(t, name)

				if err := tt.call(cliRunner{}); err != nil {
					t.Errorf("%s through fakecli = %v", tt.op, err)
				}

				if hits := engineguard.Hits(); len(hits) != 0 {
					t.Errorf("Hits() = %+v, want none: the binary comes from the directory fakecli declared", hits)
				}
			})
		}
	}
}

func TestCliRunnerRecordsNothingForAnEngineInstalledByPath(t *testing.T) {
	// The binary a configuration names by its path (engine.Shape.Binary).
	fake := fakecli.Install(t, fakecli.Scenario{{Prefix: true}})
	path := fake.InstallAt(t, t.TempDir(), "docker")
	engineguard.Isolate(t)
	useBinary(t, path)

	for _, tt := range runnerOps {
		if err := tt.call(cliRunner{}); err != nil {
			t.Errorf("%s = %v", tt.op, err)
		}
	}

	if hits := engineguard.Hits(); len(hits) != 0 {
		t.Errorf("Hits() = %+v, want none: InstallAt is a fakecli fake like the ones on PATH", hits)
	}
}

func TestACredentialHelperIsJudgedLikeTheEngine(t *testing.T) {
	// `<engine>-credential-<helper>` is a binary the runner starts too.
	fakecli.Install(t, fakecli.Scenario{{Tool: "docker-credential-probe", Args: []string{"get"}}}, "docker-credential-probe")
	engineguard.Isolate(t)
	useBinary(t, engine.Docker)

	_, err := cliRunner{}.Run(dockerCmd{Helper: "probe", Args: []string{"get"}})

	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if hits := engineguard.Hits(); len(hits) != 0 {
		t.Errorf("Hits() = %+v, want none for a helper installed by fakecli", hits)
	}
}

func TestInstallingFakeCLIDoesNotBlessEveryOtherBinary(t *testing.T) {
	fakecli.Install(t, fakecli.Scenario{{Prefix: true}}, engine.Docker)
	engineguard.SurveyMode(t)
	binary := emptyEngine(t)
	useBinary(t, binary)

	_ = cliRunner{}.Build("ps")

	if hits := engineguard.Hits(); len(hits) != 1 || hits[0].Binary != binary {
		t.Errorf("Hits() = %+v, want the engine outside the fakecli directory recorded", hits)
	}
}

// The negative control through the runner itself: with no survey, a test that
// drives an engine outside a fakecli directory panics before anything is
// started (the "engine" is an empty file, and nothing could run it anyway).
func TestCliRunnerPanicsForAnEngineOutsideTheFakeCLIDirs(t *testing.T) {
	for _, tt := range runnerOps {
		t.Run(tt.op, func(t *testing.T) {
			engineguard.Isolate(t)
			binary := emptyEngine(t)
			useBinary(t, binary)

			defer func() {
				got := recover()
				if got == nil {
					t.Fatalf("%s returned, want a panic: a test must not reach a real engine", tt.op)
				}
				msg, _ := got.(string)
				for _, want := range []string{"DEVDESK-799", "network.md", binary} {
					if !strings.Contains(msg, want) {
						t.Errorf("panic = %q, want it to contain %q", msg, want)
					}
				}
			}()
			_ = tt.call(cliRunner{})
		})
	}
}
