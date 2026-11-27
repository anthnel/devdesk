package engineguard_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engineguard"
)

// OC-03a (DEVDESK-798): guard G1, survey mode. In a test binary, the runner
// that starts a container engine tells this package which binary it resolved;
// a binary outside every directory a test declared (fakecli declares its own)
// is recorded, not refused. OC-03b reads the record, fixes the tests it names,
// then turns the record into a panic.
//
// The API these tests pin down (package internal/engineguard, standard library
// only, production code: cliRunner calls it, so it cannot import fakecli):
//
//	engineguard.Check(op, binary string)       // the runner's first statement; op is "LookPath", "Run" or "Build"
//	engineguard.Hit{Test, Op, Binary string}   // one recorded reach
//	engineguard.Hits() []Hit                   // what this process recorded, oldest first
//	engineguard.Summary(hits []Hit) string     // the list shown at the end of the suite
//	engineguard.ReadSurvey(path) ([]Hit, error) // the hits the processes of a suite appended to SurveyEnv's file
//	engineguard.AllowDir(t, dir)               // dir is a fakecli directory, for the length of t
//	engineguard.Isolate(t)                     // a fresh record and no survey file, for the length of t
//	engineguard.ContractGate(getenv) error     // the T4 gate: DEVDESK_ENGINE_CONTRACT=1 and CI empty
//	engineguard.AllowRealEngine(t)             // what a contract test's allowRealEngine(t) calls
//	engineguard.SurveyEnv, engineguard.ContractEnv // "DEVDESK_ENGINE_SURVEY", "DEVDESK_ENGINE_CONTRACT"

// engineBinary writes an executable file named name in a fresh directory and
// returns its path. Nothing runs it: the guard only resolves it.
func engineBinary(t *testing.T, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("an executable is recognised by its extension on Windows")
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatalf("writing the engine binary: %v", err)
	}
	return path
}

func TestARealEngineOutsideADeclaredDirIsRecordedNotRefused(t *testing.T) {
	for _, op := range []string{"LookPath", "Run", "Build"} {
		t.Run(op, func(t *testing.T) {
			engineguard.Isolate(t)
			binary := engineBinary(t, "docker")

			engineguard.Check(op, binary) // must neither panic nor fail the test

			hits := engineguard.Hits()
			if len(hits) != 1 {
				t.Fatalf("Hits() = %+v, want exactly one", hits)
			}
			got := hits[0]
			if got.Op != op {
				t.Errorf("Hit.Op = %q, want %q", got.Op, op)
			}
			if got.Binary != binary {
				t.Errorf("Hit.Binary = %q, want the resolved binary %q", got.Binary, binary)
			}
			// The offender is named by its top-level test function, found on the
			// stack: the list is read by a person fixing tests one by one.
			if !strings.Contains(got.Test, "TestARealEngineOutsideADeclaredDirIsRecordedNotRefused") {
				t.Errorf("Hit.Test = %q, want it to name the test that reached the engine", got.Test)
			}
		})
	}
}

func TestABareNameIsResolvedThroughPathBeforeItIsJudged(t *testing.T) {
	engineguard.Isolate(t)
	binary := engineBinary(t, "docker")
	t.Setenv("PATH", filepath.Dir(binary))

	engineguard.Check("LookPath", "docker")

	hits := engineguard.Hits()
	if len(hits) != 1 || hits[0].Binary != binary {
		t.Fatalf("Hits() = %+v, want one hit carrying the resolved path %q, not the bare name", hits, binary)
	}
}

func TestABinaryThatDoesNotResolveRecordsNothing(t *testing.T) {
	engineguard.Isolate(t)
	t.Setenv("PATH", t.TempDir())

	engineguard.Check("Run", "devdesk-no-such-engine")

	// Nothing can start: there is no real engine to reach.
	if hits := engineguard.Hits(); len(hits) != 0 {
		t.Errorf("Hits() = %+v, want none for a binary that resolves to nothing", hits)
	}
}

func TestADeclaredDirectoryIsAccepted(t *testing.T) {
	tests := []struct {
		name string
		// declare returns the directory handed to AllowDir, given the one holding
		// the binary and a scratch directory.
		declare  func(t *testing.T, holding, scratch string) string
		sub      string // the binary lives in holding/sub
		wantHits int
	}{
		{"the directory holding the binary", func(_ *testing.T, holding, _ string) string { return holding }, "", 0},
		{"the same directory, spelled with a trailing separator", func(_ *testing.T, holding, _ string) string { return holding + string(os.PathSeparator) }, "", 0},
		{"a symbolic link to the directory", func(t *testing.T, holding, scratch string) string {
			link := filepath.Join(scratch, "link")
			if err := os.Symlink(holding, link); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}
			return link
		}, "", 0},
		{"another directory", func(_ *testing.T, _, scratch string) string { return scratch }, "", 1},
		{"a parent of the directory: one directory, not a tree", func(_ *testing.T, holding, _ string) string { return filepath.Dir(holding) }, "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engineguard.Isolate(t)
			binary := engineBinary(t, "docker")
			holding := filepath.Dir(binary)
			engineguard.AllowDir(t, tt.declare(t, holding, t.TempDir()))

			engineguard.Check("Run", binary)

			if hits := engineguard.Hits(); len(hits) != tt.wantHits {
				t.Errorf("Hits() = %+v, want %d", hits, tt.wantHits)
			}
		})
	}
}

func TestADeclarationEndsWithItsTest(t *testing.T) {
	engineguard.Isolate(t)
	binary := engineBinary(t, "docker")

	t.Run("declares it", func(t *testing.T) {
		engineguard.AllowDir(t, filepath.Dir(binary))
		engineguard.Check("Run", binary)
		if hits := engineguard.Hits(); len(hits) != 0 {
			t.Errorf("Hits() = %+v inside the declaring test, want none", hits)
		}
	})

	engineguard.Check("Run", binary)

	if hits := engineguard.Hits(); len(hits) != 1 {
		t.Errorf("Hits() = %+v after the declaring test ended, want the one reach recorded", hits)
	}
}

func TestIsolateKeepsADeliberateReachOutOfTheSuiteRecord(t *testing.T) {
	engineguard.Isolate(t)
	binary := engineBinary(t, "docker")

	t.Run("inner", func(t *testing.T) {
		engineguard.Isolate(t)
		engineguard.Check("Run", binary)
		if hits := engineguard.Hits(); len(hits) != 1 {
			t.Errorf("Hits() = %+v in the isolated test, want its own reach", hits)
		}
	})

	if hits := engineguard.Hits(); len(hits) != 0 {
		t.Errorf("Hits() = %+v, want none: a test that reaches an engine on purpose must not end up in the list of offenders", hits)
	}
}

func TestContractGate(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"variable set, CI empty", map[string]string{"DEVDESK_ENGINE_CONTRACT": "1"}, false},
		{"variable set, CI set to empty", map[string]string{"DEVDESK_ENGINE_CONTRACT": "1", "CI": ""}, false},
		{"variable set, CI defined", map[string]string{"DEVDESK_ENGINE_CONTRACT": "1", "CI": "true"}, true},
		{"variable set, CI defined as 1", map[string]string{"DEVDESK_ENGINE_CONTRACT": "1", "CI": "1"}, true},
		{"variable unset", map[string]string{}, true},
		{"variable unset, CI defined", map[string]string{"CI": "true"}, true},
		{"variable is not 1", map[string]string{"DEVDESK_ENGINE_CONTRACT": "true"}, true},
		{"variable is zero", map[string]string{"DEVDESK_ENGINE_CONTRACT": "0"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engineguard.ContractGate(func(name string) string { return tt.env[name] })

			if (err != nil) != tt.wantErr {
				t.Errorf("ContractGate() = %v, want an error: %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "CI") {
				t.Errorf("ContractGate() = %q, want the refusal to name CI", err)
			}
		})
	}
}

// refusedT stands for a test AllowRealEngine fails: Fatalf records the message
// and ends the goroutine through a panic the caller recovers. Only the three
// methods AllowRealEngine may call are implemented; any other one panics on the
// nil interface, which is how an unexpected use shows.
type refusedT struct {
	testing.TB
	real *testing.T // where the cleanups go, so an accepted allowance ends with the real test
	msg  string
}

type refused struct{}

func (r *refusedT) Helper()          {}
func (r *refusedT) Cleanup(f func()) { r.real.Cleanup(f) }
func (r *refusedT) Fatalf(format string, args ...any) {
	r.msg = fmt.Sprintf(format, args...)
	panic(refused{})
}

func TestAllowRealEngineFollowsTheGate(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		allowed bool
	}{
		{"contract run, outside CI", map[string]string{"DEVDESK_ENGINE_CONTRACT": "1", "CI": ""}, true},
		{"CI defined, even with the variable", map[string]string{"DEVDESK_ENGINE_CONTRACT": "1", "CI": "true"}, false},
		{"no variable", map[string]string{"CI": ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			if _, set := tt.env["DEVDESK_ENGINE_CONTRACT"]; !set {
				t.Setenv("DEVDESK_ENGINE_CONTRACT", "")
			}
			engineguard.Isolate(t)
			binary := engineBinary(t, "docker")

			failed := false
			rt := &refusedT{real: t}
			func() {
				defer func() {
					if r := recover(); r != nil {
						if _, ok := r.(refused); !ok {
							panic(r)
						}
						failed = true
					}
				}()
				engineguard.AllowRealEngine(rt)
			}()

			if failed == tt.allowed {
				t.Fatalf("AllowRealEngine failed the test: %v (%q), want failure: %v", failed, rt.msg, !tt.allowed)
			}
			if failed && !strings.Contains(rt.msg, "CI") {
				t.Errorf("refusal message = %q, want it to say why (the gate names CI)", rt.msg)
			}
			engineguard.Check("Run", binary)
			recorded := len(engineguard.Hits()) == 1
			if recorded == tt.allowed {
				t.Errorf("a reach was recorded: %v, want recorded: %v (an accepted contract test may reach the real engine)", recorded, !tt.allowed)
			}
		})
	}
}

func TestSummaryListsEachOffendingTestOnceAndSorted(t *testing.T) {
	hits := []engineguard.Hit{
		{Test: "app.TestB", Op: "Run", Binary: "/usr/bin/docker"},
		{Test: "app.TestA", Op: "LookPath", Binary: "/usr/bin/docker"},
		{Test: "app.TestB", Op: "Run", Binary: "/usr/bin/docker"}, // the same reach again
		{Test: "app.TestB", Op: "Build", Binary: "/usr/bin/docker"},
	}

	got := engineguard.Summary(hits)

	for _, h := range hits {
		if !strings.Contains(got, h.Test) || !strings.Contains(got, h.Op) || !strings.Contains(got, h.Binary) {
			t.Errorf("Summary() = %q, want it to carry test, operation and binary of %+v", got, h)
		}
	}
	if n := strings.Count(got, "app.TestB\tRun\t/usr/bin/docker"); n != 1 {
		t.Errorf("the same reach is listed %d times, want once:\n%s", n, got)
	}
	if a, b := strings.Index(got, "app.TestA"), strings.Index(got, "app.TestB"); a < 0 || b < 0 || a > b {
		t.Errorf("Summary() is not sorted by test:\n%s", got)
	}
	// Tab-separated lines survive a copy into a Plane description and `sort -u`.
	if !strings.Contains(got, "app.TestA\tLookPath\t/usr/bin/docker") {
		t.Errorf("Summary() lines are not test<TAB>operation<TAB>binary:\n%s", got)
	}
}

func TestSummaryOfAnEmptySurveySaysSo(t *testing.T) {
	got := engineguard.Summary(nil)

	if strings.TrimSpace(got) == "" {
		t.Error("Summary(nil) is empty: a clean suite must say that no test reached an engine, or nobody can tell it from a survey that did not run")
	}
}

func TestEveryProcessOfASuiteAppendsToTheSurveyFile(t *testing.T) {
	engineguard.Isolate(t)
	file := filepath.Join(t.TempDir(), "survey")
	t.Setenv(engineguard.SurveyEnv, file)
	binary := engineBinary(t, "docker")

	engineguard.Check("LookPath", binary)
	engineguard.Check("Run", binary)

	got, err := engineguard.ReadSurvey(file)
	if err != nil {
		t.Fatalf("ReadSurvey() error = %v", err)
	}
	if want := engineguard.Hits(); len(want) != 2 || !slices.Equal(got, want) {
		t.Errorf("ReadSurvey() = %+v, want the two hits recorded in memory %+v", got, want)
	}
}

func TestWithoutSurveyFileNothingIsWritten(t *testing.T) {
	engineguard.Isolate(t) // clears the variable
	dir := t.TempDir()
	t.Chdir(dir)
	binary := engineBinary(t, "docker")

	engineguard.Check("Run", binary)

	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the working directory holds %d entries after a reach, want none: the file is opt-in", len(entries))
	}
}

func TestReadSurveyOfAFileThatNeverWasWrittenIsAnEmptySurvey(t *testing.T) {
	got, err := engineguard.ReadSurvey(filepath.Join(t.TempDir(), "absent"))

	if err != nil || len(got) != 0 {
		t.Errorf("ReadSurvey(absent) = %+v, %v, want no hits and no error: a clean suite never creates the file", got, err)
	}
}
