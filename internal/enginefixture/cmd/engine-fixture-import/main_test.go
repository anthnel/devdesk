package main

// Tests of the engine-fixture-import command (OC-09a3, DEVDESK-944, step 13),
// written before the command. It is the workstation half of
// `mise run engine-contract:record`: contract.sh brings the directory the
// recorder wrote in the test VM back (a tar of that one directory) and runs
//
//	go run ./internal/enginefixture/cmd/engine-fixture-import --from <dir> [--into <dir>] [--versions <file>]
//
// which does nothing but enginefixture.Import on files. It reads files and writes
// files: it starts no engine and imports neither internal/testutil/enginecontract
// nor internal/docker (TestTheCommandStartsNothingAndImportsNoEngineCode).
//
// The contract these tests fix:
//
//	func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int
//
//	--from  the batch (required); --into  the target, default enginefixture.SourceDir();
//	--versions  versions.env, default enginefixture.VersionsSourceFile().
//	Local identities: enginefixture.MachineIdentities(); deny list: fixtureleak.ReadDenylist(getenv),
//	so DEVDESK_FIXTURE_DENYLIST names it.
//	Exit codes: 0 done, 1 refused or failed (nothing written), 2 usage.
//	stdout, on success, first line exactly
//	  engine-contract:record: <tool> <version> <mode>: N written, N unchanged, N kept, N not produced by this run
//	then one line per fixture of each non-empty list, which names "<campaign>/<name>".
//	stderr, on failure: "engine-fixture-import: <error>"; the error names files and
//	rules, never the text found. Nothing is printed on stdout on failure.
//
// E1 (TestOnlyTestsImportEngineFixture): this directory is under
// internal/enginefixture, which the guard already admits; no guard is changed.

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

func cliFx(campaign, scenario, version, recorded, stdout string) enginefixture.Fixture {
	return enginefixture.Fixture{
		Campaign: campaign,
		Name:     scenario + "--podman-" + version + "-rootless",
		Scenario: scenario,
		Meta: enginefixture.Meta{
			Tool: enginefixture.Podman, Version: version, Mode: enginefixture.Rootless, Recorded: recorded,
			Scenario: "what " + scenario + " shows", Redacted: "none", RecordedBy: enginefixture.RecordedByContract,
		},
		Exchange: enginefixture.Exchange{Argv: []string{"ps", "-a"}, Stdout: []byte(stdout)},
	}
}

func put(t *testing.T, root string, f enginefixture.Fixture) {
	t.Helper()
	dir := filepath.Join(root, f.Campaign)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, f.Name+".cli"), enginefixture.Format(f), 0o600); err != nil {
		t.Fatal(err)
	}
}

const versionsText = "# synthetic\nDOCKER_N_VERSION=29.10.1\nDOCKER_N1_VERSION=28.5.2\nPODMAN_N_VERSION=6.2.1\nPODMAN_N1_VERSION=5.8.11\n"

// versionsFile writes a synthetic versions.env and returns its path.
func versionsFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "versions.env")
	if err := os.WriteFile(p, []byte(versionsText), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func noEnv(string) string { return "" }

func envOf(kv ...string) func(string) string {
	return func(name string) string {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == name {
				return kv[i+1]
			}
		}
		return ""
	}
}

// invoke runs the command and returns its exit code and both streams.
func invoke(t *testing.T, getenv func(string) string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, getenv, &out, &errOut)
	return code, out.String(), errOut.String()
}

func listing(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(files)
	return files
}

func TestRunImportsABatchAndSaysWhichEngineItWas(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "rm-running", "6.2.1", "2026-11-27", "a\n"))
	put(t, from, cliFx("networks", "ls", "6.2.1", "2026-11-27", "b\n"))

	code, stdout, stderr := invoke(t, noEnv, "--from", from, "--into", into, "--versions", versionsFile(t))

	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q, want 0 and nothing on stderr", code, stderr)
	}
	const first = "engine-contract:record: podman 6.2.1 rootless: 2 written, 0 unchanged, 0 kept, 0 not produced by this run"
	if line, _, _ := strings.Cut(stdout, "\n"); line != first {
		t.Errorf("first line of stdout = %q, want %q", line, first)
	}
	for _, name := range []string{"containers/rm-running--podman-6.2.1-rootless", "networks/ls--podman-6.2.1-rootless"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("stdout does not list %s:\n%s", name, stdout)
		}
	}
	if got, want := listing(t, into), []string{"containers/rm-running--podman-6.2.1-rootless.cli", "networks/ls--podman-6.2.1-rootless.cli"}; !slices.Equal(got, want) {
		t.Errorf("files written = %v, want %v", got, want)
	}
}

// Criterion 2 through the command: a second recording that differs by its date
// only writes nothing, and the summary says so. A fixture of the target that this
// run did not produce is counted, not deleted.
func TestRunCountsWhatItDidNotWriteAndWhatItDidNotProduce(t *testing.T) {
	into := t.TempDir()
	first := t.TempDir()
	put(t, first, cliFx("containers", "a", "6.2.1", "2026-11-27", "a\n"))
	put(t, first, cliFx("containers", "gone", "6.2.1", "2026-11-27", "gone\n"))
	if code, _, stderr := invoke(t, noEnv, "--from", first, "--into", into, "--versions", versionsFile(t)); code != 0 {
		t.Fatalf("first import: exit %d: %s", code, stderr)
	}
	second := t.TempDir()
	put(t, second, cliFx("containers", "a", "6.2.1", "2027-03-04", "a\n"))

	code, stdout, stderr := invoke(t, noEnv, "--from", second, "--into", into, "--versions", versionsFile(t))

	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	const want = "engine-contract:record: podman 6.2.1 rootless: 0 written, 1 unchanged, 0 kept, 1 not produced by this run"
	if line, _, _ := strings.Cut(stdout, "\n"); line != want {
		t.Errorf("first line of stdout = %q, want %q", line, want)
	}
	if !strings.Contains(stdout, "containers/gone--podman-6.2.1-rootless") {
		t.Errorf("stdout does not name the fixture this run did not produce:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(into, "containers", "gone--podman-6.2.1-rootless.cli")); err != nil {
		t.Errorf("the fixture this run did not produce was deleted: %v", err)
	}
}

func TestRunReadsTheVersionsOfTheRepositoryByDefault(t *testing.T) {
	file, err := enginefixture.VersionsSourceFile()
	if err != nil {
		t.Skipf("no source tree (-trimpath?): %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	supported, err := enginefixture.ParseVersions(data)
	if err != nil {
		t.Fatal(err)
	}
	n := supported[enginefixture.Podman][0]
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", n, "2026-11-27", "a\n"))

	code, stdout, stderr := invoke(t, noEnv, "--from", from, "--into", into)

	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if want := "engine-contract:record: podman " + n + " rootless: 1 written"; !strings.HasPrefix(stdout, want) {
		t.Errorf("stdout = %q, want it to start with %q", stdout, want)
	}
}

// A refusal writes nothing, prints nothing on stdout, and names file and rule on
// stderr without the text found.
func TestRunRefusesALeakAndSaysWhereNotWhat(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "fine", "6.2.1", "2026-11-27", "fine\n"))
	put(t, from, cliFx("containers", "leaky", "6.2.1", "2026-11-27", "user alice at /home/alice/secret-project\n"))

	code, stdout, stderr := invoke(t, noEnv, "--from", from, "--into", into, "--versions", versionsFile(t))

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on a refusal", stdout)
	}
	for _, want := range []string{"engine-fixture-import:", "leaky--podman-6.2.1-rootless", "personal-path"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not contain %q: %s", want, stderr)
		}
	}
	if strings.Contains(stderr, "alice") || strings.Contains(stderr, "secret-project") {
		t.Errorf("stderr repeats what it found: %s", stderr)
	}
	if files := listing(t, into); len(files) != 0 {
		t.Errorf("files written despite the refusal: %v", files)
	}
}

func TestRunAppliesTheExternalDenyListOfTheEnvironment(t *testing.T) {
	list := filepath.Join(t.TempDir(), "deny.txt")
	if err := os.WriteFile(list, []byte("ZorkMid-Project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", "6.2.1", "2026-11-27", "label ZorkMid-Project\n"))

	code, _, stderr := invoke(t, envOf("DEVDESK_FIXTURE_DENYLIST", list), "--from", from, "--into", into, "--versions", versionsFile(t))

	if code != 1 || !strings.Contains(stderr, "exact-term") || strings.Contains(stderr, "ZorkMid") {
		t.Errorf("exit %d, stderr %q, want 1, the rule exact-term and not the term", code, stderr)
	}
	if files := listing(t, into); len(files) != 0 {
		t.Errorf("files written despite the refusal: %v", files)
	}
}

func TestRunNamesTheVariableOfAnUnreadableDenyList(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", "6.2.1", "2026-11-27", "a\n"))
	absent := filepath.Join(t.TempDir(), "absent.txt")

	code, _, stderr := invoke(t, envOf("DEVDESK_FIXTURE_DENYLIST", absent), "--from", from, "--into", into, "--versions", versionsFile(t))

	if code != 1 || !strings.Contains(stderr, "DEVDESK_FIXTURE_DENYLIST") {
		t.Errorf("exit %d, stderr %q, want 1 and the variable named: a deny list that cannot be read is not 'no deny list'", code, stderr)
	}
	if files := listing(t, into); len(files) != 0 {
		t.Errorf("files written without the deny list that was asked for: %v", files)
	}
}

// The identities of the machine that runs the import are applied: the workstation
// is the one place the recording user's own name can be checked against.
func TestRunAppliesTheIdentitiesOfThisMachine(t *testing.T) {
	ids := enginefixture.MachineIdentities()
	if len(ids) == 0 {
		t.Skip("this machine has no identity worth looking for")
	}
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", "6.2.1", "2026-11-27", "owner "+ids[0]+" here\n"))

	code, _, stderr := invoke(t, noEnv, "--from", from, "--into", into, "--versions", versionsFile(t))

	if code != 1 || !strings.Contains(stderr, "local-identity") || strings.Contains(stderr, ids[0]) {
		t.Errorf("exit %d, stderr %q, want 1, the rule local-identity and not the name", code, stderr)
	}
}

func TestRunRefusesAVersionOutsideTheSupportedOnes(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", "6.3.0", "2026-11-27", "a\n"))

	code, stdout, stderr := invoke(t, noEnv, "--from", from, "--into", into, "--versions", versionsFile(t))

	if code != 1 || stdout != "" || !strings.Contains(stderr, "6.3.0") {
		t.Errorf("exit %d, stdout %q, stderr %q, want 1, nothing, and the version named", code, stdout, stderr)
	}
}

func TestRunFailsOnAVersionsFileItCannotRead(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", "6.2.1", "2026-11-27", "a\n"))

	code, _, stderr := invoke(t, noEnv, "--from", from, "--into", into, "--versions", filepath.Join(t.TempDir(), "absent.env"))

	if code != 1 || !strings.Contains(stderr, "engine-fixture-import:") {
		t.Errorf("exit %d, stderr %q, want 1", code, stderr)
	}
	if files := listing(t, into); len(files) != 0 {
		t.Errorf("files written without the list of supported versions: %v", files)
	}
}

func TestRunRefusesBadArguments(t *testing.T) {
	versions := versionsFile(t)
	tests := []struct {
		name string
		args []string
	}{
		{"no argument", nil},
		{"no --from", []string{"--into", t.TempDir(), "--versions", versions}},
		{"an unknown flag", []string{"--from", t.TempDir(), "--frobnicate"}},
		{"a positional argument", []string{"--from", t.TempDir(), "extra"}},
		{"--from without a value", []string{"--from"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := invoke(t, noEnv, tt.args...)

			if code != 2 {
				t.Errorf("exit code = %d, want 2 (usage)", code)
			}
			if stdout != "" || !strings.Contains(strings.ToLower(stderr), "usage") {
				t.Errorf("stdout %q, stderr %q, want a usage line on stderr only", stdout, stderr)
			}
		})
	}
}

// The command reads files and writes files. It must not import what starts an
// engine, nor the contract helpers (guard C1 would refuse that anyway: it is not a
// test file), nor os/exec. Read as text through go/parser, as the guards do.
func TestTheCommandStartsNothingAndImportsNoEngineCode(t *testing.T) {
	forbidden := []string{
		"os/exec",
		"testing",
		"github.com/anthnel/devdesk/internal/testutil/enginecontract",
		"github.com/anthnel/devdesk/internal/testutil/fakecli",
		"github.com/anthnel/devdesk/internal/docker",
		"github.com/anthnel/devdesk/internal/engine",
		"github.com/anthnel/devdesk/internal/engineguard",
	}
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source file in the directory of the command: %v", err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if slices.Contains(forbidden, p) {
				t.Errorf("%s:%d imports %s: the command starts no engine and uses no contract helper", file, fset.Position(imp.Pos()).Line, p)
			}
		}
	}
}
