package engineguard_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engineguard"
)

// OC-03b (DEVDESK-799): guard G1 refuses. In a test binary, a binary the runner
// resolved outside every declared directory panics, with a message naming the
// story and docs/architecture/network.md (as the DEVDESK-610 and DEVDESK-708
// guards do). The survey of OC-03a stays, opt-in: it is the mode the process is
// in only when DEVDESK_ENGINE_SURVEY is set.
//
// The API these tests add to the one of OC-03a:
//
//	engineguard.SurveyMode(t) // Isolate, plus a private survey file: reaches are recorded, not refused

// reach calls Check and returns what it panicked with, nil when it did not.
func reach(op, binary string) (recovered any) {
	defer func() { recovered = recover() }()
	engineguard.Check(op, binary)
	return nil
}

func TestByDefaultARealEngineOutsideADeclaredDirPanics(t *testing.T) {
	for _, op := range []string{"LookPath", "Run", "Build"} {
		t.Run(op, func(t *testing.T) {
			engineguard.Isolate(t)
			binary := engineBinary(t, "docker")

			got := reach(op, binary)

			if got == nil {
				t.Fatalf("Check(%q, %q) returned, want a panic: a test must not reach a real engine", op, binary)
			}
			msg := fmt.Sprint(got)
			for _, want := range []string{"DEVDESK-799", "network.md", "Tests and the real engine", binary, op, "fakecli.Install", "AllowRealEngine", "TestByDefaultARealEngineOutsideADeclaredDirPanics"} {
				if !strings.Contains(msg, want) {
					t.Errorf("panic = %q, want it to contain %q", msg, want)
				}
			}
		})
	}
}

func TestABareNameThatResolvesOutsideADeclaredDirPanics(t *testing.T) {
	engineguard.Isolate(t)
	binary := engineBinary(t, "docker")
	t.Setenv("PATH", filepath.Dir(binary))

	got := reach("LookPath", "docker")

	if got == nil || !strings.Contains(fmt.Sprint(got), binary) {
		t.Fatalf("panic = %v, want one naming the resolved path %q, not the bare name", got, binary)
	}
}

func TestADeclaredDirectoryDoesNotPanic(t *testing.T) {
	engineguard.Isolate(t)
	binary := engineBinary(t, "docker")
	engineguard.AllowDir(t, filepath.Dir(binary))

	if got := reach("Run", binary); got != nil {
		t.Fatalf("Check panicked for a declared directory: %v", got)
	}
}

func TestADeclarationEndsWithItsTestAndThePanicComesBack(t *testing.T) {
	engineguard.Isolate(t)
	binary := engineBinary(t, "docker")

	t.Run("declares it", func(t *testing.T) {
		engineguard.AllowDir(t, filepath.Dir(binary))
		if got := reach("Run", binary); got != nil {
			t.Errorf("Check panicked inside the declaring test: %v", got)
		}
	})

	if got := reach("Run", binary); got == nil {
		t.Error("Check did not panic after the declaring test ended")
	}
}

func TestABinaryThatDoesNotResolveDoesNotPanic(t *testing.T) {
	engineguard.Isolate(t)
	t.Setenv("PATH", t.TempDir())

	if got := reach("Run", "devdesk-no-such-engine"); got != nil {
		t.Fatalf("Check panicked for a binary that resolves to nothing: %v", got)
	}
}

func TestAnAcceptedAllowRealEngineStopsThePanic(t *testing.T) {
	t.Setenv(engineguard.ContractEnv, "1")
	t.Setenv("CI", "")
	engineguard.Isolate(t)
	binary := engineBinary(t, "docker")

	t.Run("contract test", func(t *testing.T) {
		engineguard.AllowRealEngine(t)
		if got := reach("Run", binary); got != nil {
			t.Errorf("Check panicked in an accepted contract test: %v", got)
		}
	})

	if got := reach("Run", binary); got == nil {
		t.Error("Check did not panic after the contract test ended: the allowance is for the length of one test")
	}
}

func TestARefusedAllowRealEngineDoesNotStopThePanic(t *testing.T) {
	t.Setenv(engineguard.ContractEnv, "")
	engineguard.Isolate(t)
	binary := engineBinary(t, "docker")

	failed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(refused); !ok {
					panic(r)
				}
				failed = true
			}
		}()
		engineguard.AllowRealEngine(&refusedT{real: t})
	}()

	if !failed {
		t.Fatal("AllowRealEngine was accepted without the contract gate")
	}
	if got := reach("Run", binary); got == nil {
		t.Error("Check did not panic after a refused AllowRealEngine")
	}
}

func TestTheSurveyIsOptIn(t *testing.T) {
	t.Run("with a survey file, a reach is recorded and the call goes on", func(t *testing.T) {
		engineguard.Isolate(t)
		file := filepath.Join(t.TempDir(), "survey")
		t.Setenv(engineguard.SurveyEnv, file)
		binary := engineBinary(t, "docker")

		if got := reach("Run", binary); got != nil {
			t.Fatalf("Check panicked in survey mode: %v", got)
		}

		hits, err := engineguard.ReadSurvey(file)
		if err != nil || len(hits) != 1 || hits[0].Binary != binary {
			t.Errorf("ReadSurvey() = %+v, %v, want the one reach", hits, err)
		}
	})

	t.Run("without one, nothing is written and the reach panics", func(t *testing.T) {
		engineguard.Isolate(t) // clears the variable
		dir := t.TempDir()
		t.Chdir(dir)
		binary := engineBinary(t, "docker")

		if got := reach("Run", binary); got == nil {
			t.Error("Check did not panic without a survey file")
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("the working directory holds %d entries after a reach, want none", len(entries))
		}
	})

	t.Run("SurveyMode records without a panic", func(t *testing.T) {
		engineguard.SurveyMode(t)
		binary := engineBinary(t, "docker")

		if got := reach("Build", binary); got != nil {
			t.Fatalf("Check panicked under SurveyMode: %v", got)
		}
		if hits := engineguard.Hits(); len(hits) != 1 {
			t.Errorf("Hits() = %+v, want the one reach", hits)
		}
	})
}
