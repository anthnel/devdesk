// Package engineguard is guard G1 of the engine tests (OC-03, DEVDESK-798): in a
// test binary, the runner that starts a container engine tells this package
// which binary it resolved, and a binary outside every directory a test
// declared (fakecli declares its own) is a test reaching a real engine.
//
// Such a reach panics (OC-03b, DEVDESK-799), with a message that names the story
// and docs/architecture/network.md. The survey of OC-03a stays as an opt-in
// mode: when SurveyEnv names a file, a reach is recorded, in memory and in that
// file, and the call goes on as it did (the mise task engine-survey runs the
// whole suite that way to list the offenders).
//
// The package is production code (internal/docker calls Check), so it imports
// the standard library only, "testing" included (as internal/ports does, for
// testing.Testing), and does nothing outside a test binary: Check
// returns before it resolves anything, and the entry points reserved for tests
// panic, as ports.IdentityForTest does.
package engineguard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	// SurveyEnv names the file every test process of a suite appends its hits
	// to. Unset or empty, nothing is written.
	SurveyEnv = "DEVDESK_ENGINE_SURVEY"
	// ContractEnv opens the gate AllowRealEngine checks, together with an empty CI.
	ContractEnv = "DEVDESK_ENGINE_CONTRACT"
)

// Hit is one reach of a real engine binary from a test.
type Hit struct {
	// Test is the top-level test function that reached it, as "package.TestName".
	// A reach from a goroutine no test frame is on reads "(no test frame)" followed
	// by the test binary, the package and the first caller outside it.
	Test string
	// Op is the runner operation: "LookPath", "Run" or "Build".
	Op string
	// Binary is the path the name resolved to.
	Binary string
}

// The seams of the package. Tests replace them to check that a production run
// changes nothing without building a second binary.
var (
	underTest = testing.Testing
	lookPath  = exec.LookPath
)

var (
	mu        sync.Mutex
	hits      []Hit
	allowed   []string // canonical directories, one entry per declaration
	realCount int      // AllowRealEngine declarations still in force
)

// Check is the first statement of each runner operation. op is "LookPath",
// "Run" or "Build"; binary is the name or path the runner is about to use.
func Check(op, binary string) {
	if !underTest() {
		return
	}
	path, err := lookPath(binary)
	if err != nil {
		return // nothing resolves, so there is no real engine to reach
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	dir := canonical(filepath.Dir(path)) // file system work stays outside the lock
	mu.Lock()
	reaching := realCount == 0 && !declared(dir)
	mu.Unlock()
	if !reaching {
		return
	}
	h := Hit{Test: callerTest(), Op: op, Binary: path}
	if os.Getenv(SurveyEnv) != "" {
		record(h)
		return
	}
	panic(refusal(h))
}

// refusal is the message of the panic. A panic rather than an error: an error
// would be shown as an ordinary failed call and the test could pass, leaving the
// guard silent (as refuseRealSignalUnderTest in internal/ports does).
func refusal(h Hit) string {
	return fmt.Sprintf("engineguard: %s was resolved outside a fakecli directory during a test (DEVDESK-799, G1): "+
		"install a fake with fakecli.Install / AllowFakeCLIDir, or call engineguard.AllowRealEngine(t) in a contract test; "+
		"see docs/architecture/network.md, \"Tests and the real engine\" (operation %s, reached by %s)",
		h.Binary, h.Op, h.Test)
}

// declared reports whether dir, already canonical, is one of the directories a
// test declared. The caller holds mu.
func declared(dir string) bool {
	return slices.ContainsFunc(allowed, func(d string) bool { return sameDir(d, dir) })
}

func sameDir(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// canonical is dir made absolute, clean and free of symbolic links where they
// can be resolved. The directory is resolved, never the binary in it: a fakecli
// tool is a link to the test binary.
func canonical(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

func record(h Hit) {
	mu.Lock()
	hits = append(hits, h)
	mu.Unlock()
	file := os.Getenv(SurveyEnv)
	if file == "" {
		return
	}
	// One Write on an O_APPEND file: the processes of a suite share it.
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.WriteString(line(h) + "\n")
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "engineguard: writing the survey file %s: %v\n", file, err)
	}
}

// line is the tab-separated form of a hit, used by the survey file and Summary.
func line(h Hit) string {
	return clean(h.Test) + "\t" + clean(h.Op) + "\t" + clean(h.Binary)
}

// clean keeps a field on one line and in one column.
func clean(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}

// callerTest names the test that is running Check: the first frame of a test
// function in a _test.go file, innermost first (a subtest runs on a goroutine of
// its own, and its stack holds only the closure "TestX.func1").
func callerTest() string {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var chain []string // the frames outside this package, innermost first
	for {
		frame, more := frames.Next()
		pkg, fn := splitFunction(frame.Function)
		if strings.HasSuffix(frame.File, "_test.go") {
			if name, _, _ := strings.Cut(fn, "."); strings.HasPrefix(name, "Test") {
				return pkg + "." + name
			}
		}
		if pkg != "engineguard" && pkg != "runtime" && frame.Function != "" {
			chain = append(chain, pkg+"."+fn)
		}
		if !more {
			break
		}
	}
	return noTestFrame(chain)
}

// noTestFrame describes a reach no test frame is on (a goroutine, a tea.Cmd):
// the test binary (os.Args[0], such as "app.test", which says which package was
// under test), the package that called Check and the first caller outside that
// package, so that the list still says where to look.
func noTestFrame(chain []string) string {
	if len(chain) == 0 {
		return "(no test frame)"
	}
	pkg, _, _ := strings.Cut(chain[0], ".")
	caller := chain[0]
	for _, c := range chain[1:] {
		if !strings.HasPrefix(c, pkg+".") {
			caller = c
			break
		}
	}
	return "(no test frame) " + filepath.Base(os.Args[0]) + " " + pkg + " " + caller
}

// splitFunction cuts "a/b/pkg.Func.func1" into "pkg" and "Func.func1".
func splitFunction(function string) (pkg, fn string) {
	if i := strings.LastIndex(function, "/"); i >= 0 {
		function = function[i+1:]
	}
	pkg, fn, _ = strings.Cut(function, ".")
	return pkg, fn
}

// Hits returns what this process recorded, oldest first.
func Hits() []Hit {
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(hits)
}

// Summary is the list shown at the end of a survey: one line per distinct reach,
// "Test<TAB>Op<TAB>Binary", sorted. A clean suite says so, or nobody could tell
// it from a survey that did not run.
func Summary(hits []Hit) string {
	if len(hits) == 0 {
		return "none: no test reached a real container engine outside a fakecli directory"
	}
	sorted := slices.Clone(hits)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Test != b.Test {
			return a.Test < b.Test
		}
		if a.Op != b.Op {
			return a.Op < b.Op
		}
		return a.Binary < b.Binary
	})
	var lines []string
	for _, h := range sorted {
		l := line(h)
		if len(lines) == 0 || lines[len(lines)-1] != l {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// ReadSurvey reads the hits the processes of a suite appended to path. A file
// that does not exist is an empty survey: a clean suite never creates it.
func ReadSurvey(path string) ([]Hit, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Hit
	for _, l := range strings.Split(string(data), "\n") {
		if l == "" {
			continue
		}
		fields := strings.Split(l, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("survey file %s: malformed line %q", path, l)
		}
		out = append(out, Hit{Test: fields[0], Op: fields[1], Binary: fields[2]})
	}
	return out, nil
}

// mustBeUnderTest panics outside a test binary: the entry points below are
// reserved for tests, like ports.IdentityForTest.
func mustBeUnderTest(name string) {
	if !underTest() {
		panic("engineguard: " + name + " is reserved for tests (DEVDESK-798)")
	}
}

// Isolate gives t a fresh record and no survey file, for the length of t, so a
// reach panics whatever the mode of the suite (a test that reaches an engine on
// purpose must not end up in the list of offenders). Declared directories are
// left alone.
func Isolate(t testing.TB) {
	mustBeUnderTest("Isolate")
	t.Helper()
	t.Setenv(SurveyEnv, "")
	mu.Lock()
	previous := hits
	hits = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		hits = previous
		mu.Unlock()
	})
}

// SurveyMode puts t in the mode of OC-03a: Isolate, plus a survey file of its
// own, so a reach is recorded (Hits) and the call goes on instead of panicking.
// It is for the tests of the guard itself and of the runner that calls it.
func SurveyMode(t testing.TB) {
	mustBeUnderTest("SurveyMode")
	t.Helper()
	Isolate(t)
	t.Setenv(SurveyEnv, filepath.Join(t.TempDir(), "survey"))
}

// AllowDir declares dir, exactly (not its parents or children), as holding fake
// tools, for the length of t. Symbolic links in dir are resolved.
func AllowDir(t testing.TB, dir string) {
	mustBeUnderTest("AllowDir")
	t.Helper()
	dir = canonical(dir)
	mu.Lock()
	allowed = append(allowed, dir)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if i := slices.Index(allowed, dir); i >= 0 {
			allowed = slices.Delete(allowed, i, i+1)
		}
	})
}

// ContractGate is the gate of the tests that reach a real engine on purpose:
// nil only when ContractEnv is "1" and CI is empty.
func ContractGate(getenv func(string) string) error {
	if getenv(ContractEnv) == "1" && getenv("CI") == "" {
		return nil
	}
	return fmt.Errorf("reaching a real container engine needs %s=1 and an empty CI (got %s=%q, CI=%q)",
		ContractEnv, ContractEnv, getenv(ContractEnv), getenv("CI"))
}

// AllowRealEngine is what a contract test calls before it reaches the real
// engine: it fails t, rather than skipping it, when the gate is closed, and
// otherwise stops the guard refusing the test's reaches for the length of t.
func AllowRealEngine(t testing.TB) {
	mustBeUnderTest("AllowRealEngine")
	t.Helper()
	if err := ContractGate(os.Getenv); err != nil {
		t.Fatalf("%v", err)
		return
	}
	mu.Lock()
	realCount++
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		realCount--
		mu.Unlock()
	})
}
