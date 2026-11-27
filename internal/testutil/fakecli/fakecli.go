// Package fakecli is the one fake command-line tool of the test suites
// (OC-02, DEVDESK-797). A test names the tools it wants (docker, podman, kind,
// kubectl, trivy, ...) and the exchanges they answer; the code under test then
// starts them by name through PATH, exactly as it starts the real ones.
//
// The fake is the test binary itself. Install links the test binary under each
// tool name in a directory that becomes the whole PATH; when the binary is
// started under one of those names, Main (or AsTool) answers from the declared
// exchanges instead of running the tests. Every run is appended to a journal the
// test reads back with (*Fake).Invocations. The mode is strict: an argv no
// exchange covers is reported on the tool's stderr, exits non-zero, and fails
// the test when it ends, with the argv received.
//
// Install changes PATH and the process environment through t.Setenv, so it does
// not combine with t.Parallel. This package imports "testing": it belongs to
// _test.go files only, which TestFakeCLIGuardWalk enforces (rule F1).
package fakecli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	// envState names the directory holding the scenario and the journal; it is
	// set for the processes the code under test starts.
	envState = "FAKECLI_STATE"
	// envTools lists the tool names the test binary answers to, separated by
	// the path list separator, so a process that merely shares the environment
	// (the test binary re-executed under another name) is not taken for a tool.
	envTools = "FAKECLI_TOOLS"

	scenarioFile = "scenario.json"
	journalFile  = "journal.jsonl"

	// unmatchedExit is what a tool exits with when no exchange covers its argv.
	unmatchedExit = 127
)

// asToolCalled records that the test binary went through AsTool, so that it
// answers as a tool when started under a tool name. Install and InstallAt refuse
// to run without it: a test binary whose TestMain skipped it would run the whole
// suite again each time the code under test started the fake.
var asToolCalled atomic.Bool

// needTestMain fails the test when the binary's TestMain never called Main or
// AsTool.
func needTestMain(t testing.TB) {
	t.Helper()
	if !asToolCalled.Load() {
		t.Fatalf("fakecli: this test binary never called fakecli.Main or fakecli.AsTool; " +
			"add `func TestMain(m *testing.M) { os.Exit(fakecli.Main(m)) }`, " +
			"or the fake would run the whole test suite again each time it is started")
	}
}

// Reply is what a fake tool does for one exchange.
type Reply struct {
	Stdout string
	Stderr string
	Exit   int
}

// Exchange declares the reply to one invocation. Tool restricts it to one tool
// (empty: any installed tool). Args is compared element by element, never as a
// joined string. With Prefix, Args only has to start the argv, so an empty
// Args with Prefix is a catch-all. Among the exchanges that cover an argv an
// exact one wins over a prefix, the longest prefix wins over a shorter one, and
// the first declared wins a tie.
type Exchange struct {
	Tool   string
	Args   []string
	Prefix bool
	Reply  Reply
}

// Source supplies the exchanges a fake answers. Scenario is the usual one;
// anything else that can list exchanges (a recorded fixture replay, OC-08)
// plugs in here without a change to this package.
type Source interface {
	Exchanges() []Exchange
}

// Scenario is a Source made of a literal list of exchanges.
type Scenario []Exchange

// Exchanges returns the list itself.
func (s Scenario) Exchanges() []Exchange { return s }

// Invocation is one run of a fake tool, as journaled.
type Invocation struct {
	Tool string
	// Args is the argv without the tool name.
	Args []string
	// Env is the environment the tool was started with.
	Env []string
	// Matched is false when no exchange covered the argv.
	Matched bool
}

// Fake is the handle Install returns.
type Fake struct {
	dir   string
	state string
}

// Dir is the directory holding the tools, which is the whole of PATH while the
// test runs. It is the directory an engine guard is told to accept.
func (f *Fake) Dir() string { return f.dir }

// Invocations returns the journal, oldest first. It is safe to call while the
// tools are still being started by other goroutines of the code under test.
func (f *Fake) Invocations() []Invocation {
	file, err := os.Open(filepath.Join(f.state, journalFile))
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	var out []Invocation
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			var inv Invocation
			if json.Unmarshal(line, &inv) == nil {
				out = append(out, inv)
			}
		}
		if err != nil {
			return out
		}
	}
}

// Install puts the named tools alone on PATH and makes them answer src. The
// journal and the scenario live in a directory of their own under t.TempDir.
// When the test ends, every argv no exchange covered fails it.
func Install(t testing.TB, src Source, tools ...string) *Fake {
	t.Helper()
	needTestMain(t)

	dir, state := t.TempDir(), t.TempDir()

	var exchanges []Exchange
	if src != nil {
		exchanges = src.Exchanges()
	}
	body, err := json.Marshal(exchanges)
	if err != nil {
		t.Fatalf("fakecli: encoding the scenario: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state, scenarioFile), body, 0o600); err != nil {
		t.Fatalf("fakecli: writing the scenario: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("fakecli: locating the test binary: %v", err)
	}
	for _, name := range tools {
		if err := link(self, filepath.Join(dir, toolFile(name))); err != nil {
			t.Fatalf("fakecli: installing %s: %v", name, err)
		}
	}

	t.Setenv("PATH", dir)
	t.Setenv(envState, state)
	t.Setenv(envTools, strings.Join(tools, string(os.PathListSeparator)))

	f := &Fake{dir: dir, state: state}
	t.Cleanup(func() {
		for _, inv := range f.Invocations() {
			if !inv.Matched {
				t.Errorf("fakecli: no exchange declared for %s %q", inv.Tool, inv.Args)
			}
		}
	})
	return f
}

// InstallAt links one more tool, outside PATH, at dir/name and returns its path:
// the situation of a binary the configuration names by its path. The tool
// answers the scenario of f like the ones on PATH.
func (f *Fake) InstallAt(t testing.TB, dir, name string) string {
	t.Helper()
	needTestMain(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("fakecli: locating the test binary: %v", err)
	}
	path := filepath.Join(dir, toolFile(name))
	if err := link(self, path); err != nil {
		t.Fatalf("fakecli: installing %s: %v", path, err)
	}
	t.Setenv(envTools, os.Getenv(envTools)+string(os.PathListSeparator)+name)
	return path
}

// toolFile is the file name a tool is installed under.
func toolFile(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// link makes path start the test binary: a symbolic link where there is one, a
// copy where there is not (Windows, or a file system that refuses links). A path
// that already exists is an error, never overwritten: the copy opens its target
// with O_TRUNC and would write through an existing link into the test binary.
//
// Not done: on Windows a hard link (os.Link) could be tried before the copy,
// which is tens of megabytes per tool.
func link(self, path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%s is already installed: %w", path, fs.ErrExist)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(self, path); err == nil {
			return nil
		}
	}
	src, err := os.Open(self)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

// Main is what a TestMain calls: it answers as the tool when the test binary
// was started under a name Install gave it, and runs the tests otherwise.
//
//	func TestMain(m *testing.M) { os.Exit(fakecli.Main(m)) }
//
// A TestMain that prepares state before m.Run (a temporary HOME, say) calls
// AsTool first, so a tool process neither creates nor deletes it.
func Main(m *testing.M) int {
	if code, ok := AsTool(); ok {
		return code
	}
	return m.Run()
}

// AsTool serves one invocation when the process was started as an installed
// tool, and returns its exit code with true. Otherwise it does nothing.
func AsTool() (int, bool) {
	asToolCalled.Store(true)
	state, tools := os.Getenv(envState), os.Getenv(envTools)
	if state == "" || tools == "" {
		return 0, false
	}
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if !slices.Contains(filepath.SplitList(tools), name) {
		return 0, false
	}
	return serve(state, name, os.Args[1:]), true
}

// serve is the whole of a fake tool. It runs in its own process, so it reports
// through its exit code and streams rather than through testing.T.
func serve(state, tool string, argv []string) int {
	raw, err := os.ReadFile(filepath.Join(state, scenarioFile))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fakecli: unreadable scenario: %v\n", err)
		return unmatchedExit
	}
	var exchanges []Exchange
	if err := json.Unmarshal(raw, &exchanges); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fakecli: unreadable scenario: %v\n", err)
		return unmatchedExit
	}

	chosen := pick(exchanges, tool, argv)
	if err := journal(state, Invocation{Tool: tool, Args: argv, Env: os.Environ(), Matched: chosen != nil}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fakecli: cannot journal the invocation: %v\n", err)
		return unmatchedExit
	}
	if chosen == nil {
		_, _ = fmt.Fprintf(os.Stderr, "fakecli: no exchange declared for %s %q\n", tool, argv)
		return unmatchedExit
	}

	_, _ = fmt.Fprint(os.Stdout, chosen.Reply.Stdout)
	_, _ = fmt.Fprint(os.Stderr, chosen.Reply.Stderr)
	return chosen.Reply.Exit
}

// pick returns the exchange answering the argv, or nil.
func pick(exchanges []Exchange, tool string, argv []string) *Exchange {
	var best *Exchange
	for i := range exchanges {
		e := &exchanges[i]
		if e.Tool != "" && e.Tool != tool {
			continue
		}
		switch {
		case !e.Prefix && slices.Equal(e.Args, argv):
			return e
		case e.Prefix && len(e.Args) <= len(argv) && slices.Equal(e.Args, argv[:len(e.Args)]):
			if best == nil || len(e.Args) > len(best.Args) {
				best = e
			}
		}
	}
	return best
}

// The journal and the scenario are JSON: an argv or an environment value that is
// not valid UTF-8 is rewritten on the way (U+FFFD). Keep it in mind for the
// fixture replay of OC-08, which compares recorded argv.
//
// journal appends one entry with a single write, so concurrent tools never
// interleave inside a line.
func journal(state string, inv Invocation) error {
	line, err := json.Marshal(inv)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(state, journalFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
