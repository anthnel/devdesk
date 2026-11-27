package engineguard

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestCheckIsSafeFromManyGoroutines(t *testing.T) {
	Isolate(t)
	declaredDir := t.TempDir()
	AllowDir(t, declaredDir)
	reached := filepath.Join(t.TempDir(), "docker")
	allowed := filepath.Join(declaredDir, "docker")
	for _, p := range []string{reached, allowed} {
		if err := os.WriteFile(p, nil, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	const workers, rounds = 8, 25

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range rounds {
				Check("Run", reached)
				Check("Run", allowed)
				_ = Hits()
			}
		}()
	}
	wg.Wait()

	hits := Hits()
	if len(hits) != workers*rounds {
		t.Fatalf("recorded %d hits, want %d (only the undeclared directory is a reach)", len(hits), workers*rounds)
	}
	for _, h := range hits {
		// The goroutine is a closure of this test, so its frame still names the test.
		if h.Binary != reached || !strings.HasSuffix(h.Test, ".TestCheckIsSafeFromManyGoroutines") {
			t.Fatalf("Hit = %+v, want the undeclared binary, named after this test", h)
		}
	}
}

func TestNoTestFrameNamesTheTestBinaryThePackageAndTheCaller(t *testing.T) {
	got := noTestFrame([]string{"docker.cliRunner.Run", "docker.dockerOutput", "containers.refreshCmd.func1"})

	want := "(no test frame) " + filepath.Base(os.Args[0]) + " docker containers.refreshCmd.func1"
	if got != want {
		t.Errorf("noTestFrame() = %q, want %q", got, want)
	}
}
