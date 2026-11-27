package engineguard

import (
	"os"
	"path/filepath"
	"testing"
)

// The seams these tests replace, which the implementation must have:
//
//	var underTest = testing.Testing                 // is this process a test binary
//	var lookPath  = exec.LookPath                   // how Check resolves a binary
//
// They exist so criterion 5 (a production run changes nothing) is checked
// without building a second binary, on the model of refuseRealSignalUnderTest.

func TestOutsideATestBinaryTheGuardDoesNothingAndResolvesNothing(t *testing.T) {
	Isolate(t)
	file := filepath.Join(t.TempDir(), "survey")
	t.Setenv(SurveyEnv, file)
	lookups := 0
	previousLook, previousUnder := lookPath, underTest
	lookPath = func(name string) (string, error) { lookups++; return "/opt/real/" + name, nil }
	underTest = func() bool { return false }
	t.Cleanup(func() { lookPath, underTest = previousLook, previousUnder })

	for _, op := range []string{"LookPath", "Run", "Build"} {
		Check(op, "docker")
	}

	if lookups != 0 {
		t.Errorf("the guard resolved a binary %d times in a production run, want 0: no detection outside a test", lookups)
	}
	if hits := Hits(); len(hits) != 0 {
		t.Errorf("Hits() = %+v in a production run, want none", hits)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("the survey file was written in a production run")
	}
}

func TestTheTestOnlyEntryPointsPanicOutsideATestBinary(t *testing.T) {
	calls := map[string]func(){
		"Isolate":         func() { Isolate(t) },
		"AllowDir":        func() { AllowDir(t, t.TempDir()) },
		"AllowRealEngine": func() { AllowRealEngine(t) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			previous := underTest
			underTest = func() bool { return false }
			t.Cleanup(func() { underTest = previous })

			defer func() {
				if recover() == nil {
					t.Errorf("%s returned outside a test binary, want a panic (as IdentityForTest does)", name)
				}
			}()
			call()
		})
	}
}
