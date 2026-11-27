package enginecontract_test

import (
	"regexp"
	"strings"
	"testing"
)

// OC-09a3 (DEVDESK-944), step 14: contract.sh gets its second action, record:
//
//	contract.sh record <docker|podman> <n|n1> [--mode rootful|rootless] [--keep-out <dir>]
//
// It does what verify does (the gate on this machine first, the reset, the pinned
// image, the sentinel, the contracts, the sentinel check) and then, only when the
// contracts and the sentinel check passed: brings back the one directory the
// recorder wrote in the VM, and imports it on this machine. Rules held here, in the
// way of script_test.go and script_sentinel_test.go: the gate and the argument
// checks are run (the VM host is fixture-host.invalid, the run ends before any
// connection), the rest is read as text, because nothing here reaches a VM:
//
//   - record + --no-reset is refused (exit 2, the message names the option): a
//     recording starts from the clean snapshot, whose state the fixtures describe;
//   - --keep-out <dir> belongs to record only (exit 2 for verify, or without a value);
//   - DEVDESK_FIXTURE_RECORDING=1 and DEVDESK_FIXTURE_OUT=$vmdir/out are in the
//     explicit environment of the test binary IN THE VM, in the recording action
//     only, and in no file DevDesk versions or any task (that is
//     TestRecordingSessionIsNeverSetByTheStandardChecks and the mise test);
//   - the recorder writes to $vmdir/out, created before the run and owned by the
//     user of the mode; one tar of that directory comes back, extracted under the
//     temporary directory of this machine; nothing else is copied out of the VM;
//   - the import (`go run ./internal/enginefixture/cmd/engine-fixture-import --from
//     <dir> [--into <keep-out>]`) runs on this machine, after the sentinel check,
//     and only when nothing failed; with --keep-out the fixtures go there and the
//     repository is not touched (the dry run).
//
// What jakku checks and these tests cannot: a real recording of both engines and
// both lines, the tar of a rootless output directory, the import of that batch.

func TestTheScriptRecordIsSkippedWithoutTheVariable(t *testing.T) {
	out, code := runScript(t, scriptEnv(), "record", "podman", "n")

	if code != 0 || !strings.Contains(out, "skipped") {
		t.Errorf("exit code = %d, want 0 and a skip message\n%s", code, out)
	}
	noRemoteTrace(t, out)
}

func TestTheScriptRecordRefusesEveryCIMarkerEvenWithTheVariable(t *testing.T) {
	for _, marker := range ciMarkerNames {
		t.Run(marker, func(t *testing.T) {
			out, code := runScript(t, scriptEnv("DEVDESK_ENGINE_CONTRACT=1", marker+"=true"), "record", "podman", "n")

			if code != 1 || !strings.Contains(out, "refused") || !strings.Contains(out, marker) {
				t.Errorf("exit code = %d, want 1 and a refusal that names %s\n%s", code, marker, out)
			}
			noRemoteTrace(t, out)
		})
	}
}

// The recording variable in the environment of this process does not open the
// recording: only contract.sh sets it, in the VM. Setting it on the host changes
// nothing about the gate (and the script must not pass it through).
func TestTheScriptRecordGateIgnoresTheRecordingVariableOfTheHost(t *testing.T) {
	out, code := runScript(t, scriptEnv("DEVDESK_FIXTURE_RECORDING=1", "DEVDESK_FIXTURE_OUT=/tmp/somewhere"), "record", "podman", "n")

	if code != 0 || !strings.Contains(out, "skipped") {
		t.Errorf("exit code = %d, want 0 and a skip message: the variable of the host is not the variable of the gate\n%s", code, out)
	}
}

func TestTheScriptRecordRefusesInvalidArgumentsBeforeTheVM(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string // a word the message must contain, "" for none
	}{
		{"an engine that is not docker or podman", []string{"record", "kind", "n"}, ""},
		{"a line that is not n or n1", []string{"record", "docker", "n2"}, ""},
		{"rootless docker: the docker VM is rootful", []string{"record", "docker", "n", "--mode", "rootless"}, "rootless"},
		{"a mode that is not rootful or rootless", []string{"record", "podman", "n", "--mode", "sideways"}, ""},
		{"--no-reset with docker", []string{"record", "docker", "n", "--no-reset"}, "--no-reset"},
		{"--no-reset with podman", []string{"record", "podman", "n1", "--no-reset"}, "--no-reset"},
		{"--no-reset after another option", []string{"record", "podman", "n", "--mode", "rootless", "--no-reset"}, "--no-reset"},
		{"--keep-out without a directory", []string{"record", "podman", "n", "--keep-out"}, ""},
		{"--keep-out with verify: it is the recorder's", []string{"verify", "podman", "n", "--keep-out", "/tmp/ddc-keep"}, ""},
		{"an unknown option", []string{"record", "podman", "n", "--frobnicate"}, ""},
		{"no line", []string{"record", "docker"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runScript(t, scriptEnv("DEVDESK_ENGINE_CONTRACT=1"), tt.args...)

			if code != 2 {
				t.Errorf("exit code = %d, want 2 (usage)\n%s", code, out)
			}
			if tt.want != "" && !strings.Contains(out, tt.want) {
				t.Errorf("the message does not say why (%q):\n%s", tt.want, out)
			}
			noRemoteTrace(t, out)
		})
	}
}

// The usage line says what record takes and how verify differs.
func TestTheScriptUsageNamesTheRecordAction(t *testing.T) {
	out, code := runScript(t, scriptEnv("DEVDESK_ENGINE_CONTRACT=1"), "record")
	if code != 2 || !strings.Contains(out, "record") || !strings.Contains(out, "--keep-out") {
		t.Errorf("exit %d, output:\n%s\nwant usage (2) that names record and --keep-out", code, out)
	}
}

// ---- The text of the script. ----

// codeText is the non-comment code of contract.sh as one string.
func codeText(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, l := range scriptCode(t) {
		b.WriteString(l.text + "\n")
	}
	return b.String()
}

// inRecordBranch reports whether line i is under a condition on the record action:
// one of the eight code lines before it names the action and "record". A weak test
// by design (the script may be written in several ways), and the one that catches the
// real mistake: the recording variables, the tar or the import outside any condition.
func inRecordBranch(lines []scriptLine, i int) bool {
	re := regexp.MustCompile(`\$\{?action\}?|\baction\b`)
	for j := max(i-8, 0); j <= i; j++ {
		if re.MatchString(lines[j].text) && strings.Contains(lines[j].text, "record") {
			return true
		}
	}
	return false
}

func TestTheScriptHeaderNamesTheActionItRuns(t *testing.T) {
	lines := scriptCode(t)
	for _, l := range lines {
		if strings.Contains(l.text, "mode verify") {
			t.Errorf("contract.sh:%d says \"mode verify\" whatever the action: a recording would announce itself as a verification: %s", l.n, strings.TrimSpace(l.text))
		}
	}
	if first(lines, regexp.MustCompile(`engine-contract: .*mode \$\{?action\}?`), 0) < 0 {
		t.Error(`contract.sh never logs "engine-contract: <engine> line <line>, <mode>, run <run>, mode $action": the engine, the line and what the run does must be on screen`)
	}
}

func TestTheScriptSetsTheRecordingVariablesOnlyForARecordInTheVM(t *testing.T) {
	lines := scriptCode(t)
	gate := first(lines, regexp.MustCompile(`DEVDESK_ENGINE_CONTRACT`), 0)
	rec := first(lines, regexp.MustCompile(`DEVDESK_FIXTURE_RECORDING=1`), 0)
	out := first(lines, regexp.MustCompile(`DEVDESK_FIXTURE_OUT=\$\{?vmdir\}?/out\b`), 0)
	if rec < 0 || out < 0 {
		t.Fatalf("contract.sh lacks a variable of the recording: DEVDESK_FIXTURE_RECORDING=1 at %d, DEVDESK_FIXTURE_OUT=$vmdir/out at %d (-1: none)", rec, out)
	}
	if rec < gate || out < gate {
		t.Errorf("a recording variable is set at line %d or %d, before the gate at line %d", lines[rec].n, lines[out].n, lines[gate].n)
	}
	for _, i := range []int{rec, out} {
		if !inRecordBranch(lines, i) {
			t.Errorf("contract.sh:%d sets a recording variable without a condition on the record action just above it:\n%s", lines[i].n, statement(lines, i))
		}
	}
	// The explicit environment shared by both actions must not carry them.
	if v := first(lines, regexp.MustCompile(`^\s*vars=\(`), 0); v >= 0 {
		for j := v; j < len(lines); j++ {
			if strings.Contains(lines[j].text, "DEVDESK_FIXTURE") {
				t.Errorf("contract.sh:%d: the shared explicit environment (vars) carries a recording variable: a verification would record: %s",
					lines[j].n, strings.TrimSpace(lines[j].text))
			}
			if strings.TrimSpace(lines[j].text) == ")" {
				break
			}
		}
	}
	// The recording variable is never read from the host: no `${DEVDESK_FIXTURE_RECORDING`.
	if regexp.MustCompile(`\$\{?DEVDESK_FIXTURE_(RECORDING|OUT)`).MatchString(codeText(t)) {
		t.Error("contract.sh reads a recording variable of this machine's environment: it must set them, never pass them through")
	}
}

func TestTheScriptCreatesTheOutputDirectoryForTheUserOfTheMode(t *testing.T) {
	lines := scriptCode(t)
	mk := first(lines, regexp.MustCompile(`\bmkdir\b.*\$\{?vmdir\}?/out\b`), 0)
	run := first(lines, reContractsRun, 0)
	if mk < 0 {
		t.Fatal(`contract.sh never creates "$vmdir/out" in the VM: the recorder has nowhere to write`)
	}
	if mk > run {
		t.Errorf("the output directory is made at line %d, after the contracts run at line %d", lines[mk].n, lines[run].n)
	}
	if stmt := statement(lines, mk); !strings.Contains(stmt, "vm_root") {
		t.Errorf("contract.sh:%d does not make the output directory through vm_root:\n%s", lines[mk].n, stmt)
	}
	// A rootless run writes as the unprivileged user: the chown of the work directory comes after.
	chown := first(lines, regexp.MustCompile(`\bchown\b.*\$\{?vmdir\}?`), 0)
	if chown < 0 || chown < mk {
		t.Errorf("the chown of the work directory (line index %d) must come after the output directory is made (%d): a rootless recorder could not write there", chown, mk)
	}
}

// Only the output directory comes back, as one tar, and it is extracted under the
// temporary directory of this machine; the other ways out of a VM are not used.
func TestTheScriptBringsBackOnlyTheOutputDirectoryByTar(t *testing.T) {
	lines := scriptCode(t)
	var tars []int
	for i, l := range lines {
		if regexp.MustCompile(`\btar\b`).MatchString(l.text) {
			tars = append(tars, i)
		}
	}
	if len(tars) == 0 {
		t.Fatal("contract.sh never uses tar: nothing brings the recorded fixtures back")
	}
	seen := map[string]bool{}
	for _, i := range tars {
		stmt := statement(lines, i)
		if seen[stmt] {
			continue
		}
		seen[stmt] = true
		if !strings.Contains(stmt, "/out") && !strings.Contains(stmt, "$work") && !strings.Contains(stmt, "${work}") {
			t.Errorf("contract.sh:%d: a tar that is neither of the output directory in the VM nor of the temporary directory of this machine:\n%s", lines[i].n, stmt)
		}
		if regexp.MustCompile(`-C\s+["']?(/|\$HOME|\$\{?root\}?|\$\{?DD_DIR\}?)`).MatchString(stmt) && !strings.Contains(stmt, "/out") {
			t.Errorf("contract.sh:%d: tar reads from a directory that is not the output directory:\n%s", lines[i].n, stmt)
		}
	}
	pack := first(lines, regexp.MustCompile(`\bvm_root\b.*\btar\b|\btar\b.*-[a-zA-Z]*c`), 0)
	if pack < 0 {
		t.Fatal("contract.sh has no `vm_root ... tar -C $vmdir/out -cf -`: the output directory is not packed in the VM")
	}
	if stmt := statement(lines, pack); !strings.Contains(stmt, "vm_root") || !regexp.MustCompile(`-C\s+["']?\$\{?vmdir\}?/out`).MatchString(stmt) ||
		!strings.Contains(stmt, "-cf -") {
		t.Errorf("contract.sh:%d does not pack `$vmdir/out` with `-C ... -cf -` through vm_root:\n%s", lines[pack].n, stmt)
	}
	if !inRecordBranch(lines, pack) {
		t.Errorf("contract.sh:%d packs the output directory outside a condition on the record action", lines[pack].n)
	}
	// Nothing else leaves the VM.
	if m := regexp.MustCompile(`\b(scp|rsync|sftp)\b|\bfile\s+pull\b`).FindString(codeText(t)); m != "" {
		t.Errorf("contract.sh uses %q: the only thing that comes out of a VM is the tar of the output directory", m)
	}
}

func TestTheScriptImportsOnThisMachineAfterTheSentinelCheckAndOnlyOnSuccess(t *testing.T) {
	lines := scriptCode(t)
	imp := first(lines, regexp.MustCompile(`engine-fixture-import`), 0)
	if imp < 0 {
		t.Fatal("contract.sh never runs engine-fixture-import: the recorded fixtures stay in a temporary directory")
	}
	stmt := statement(lines, imp)
	for _, want := range []string{"go run", "./internal/enginefixture/cmd/engine-fixture-import", "--from"} {
		if !strings.Contains(stmt, want) {
			t.Errorf("contract.sh:%d: the import does not contain %q:\n%s", lines[imp].n, want, stmt)
		}
	}
	for _, vm := range []string{"vm_root", "vm_user", "vm_as_mode", "ssh", "incus"} {
		if strings.Contains(stmt, vm) {
			t.Errorf("contract.sh:%d: the import runs in the VM (%s): it is the workstation that checks what the VM recorded:\n%s", lines[imp].n, vm, stmt)
		}
	}
	if !regexp.MustCompile(`\$\{?root\}?`).MatchString(stmt) {
		t.Errorf("contract.sh:%d: `go run ./internal/...` needs the repository as its working directory ($root):\n%s", lines[imp].n, stmt)
	}
	inspect := first(lines, regexp.MustCompile(`\binspect\b.*`+reSentinelVar.String()), 0)
	untar := first(lines, regexp.MustCompile(`\btar\b`), 0)
	if inspect < 0 || imp < inspect || imp < untar {
		t.Errorf("order: sentinel check at index %d, tar at %d, import at %d; want the import last", inspect, untar, imp)
	}
	if !inRecordBranch(lines, imp) {
		t.Errorf("contract.sh:%d imports outside a condition on the record action", lines[imp].n)
	}
	// Only when nothing failed: the status of the contracts and of the sentinel check is read just above.
	guarded := false
	for j := max(imp-8, 0); j < imp; j++ {
		if regexp.MustCompile(`\bstatus\b`).MatchString(lines[j].text) {
			guarded = true
		}
	}
	if !guarded {
		t.Errorf("contract.sh:%d imports without reading $status just above: a failed run (or a sentinel that moved) must import nothing", lines[imp].n)
	}
	// The dry run: --keep-out sends the fixtures somewhere else and the repository is not touched.
	if first(lines, regexp.MustCompile(`--keep-out\)`), 0) < 0 {
		t.Error("contract.sh does not parse --keep-out")
	}
	if first(lines, regexp.MustCompile(`--into`), 0) < 0 {
		t.Error("contract.sh never passes --into: --keep-out would not keep the fixtures out of the repository")
	}
}

// The import is the one place that writes into the repository, and only through
// engine-fixture-import: the script itself writes nothing under internal/.
func TestTheScriptWritesNothingIntoTheRepositoryItself(t *testing.T) {
	for _, l := range scriptCode(t) {
		if regexp.MustCompile(`(>|\btee\b|\bcp\b|\bmv\b|\binstall\b).*(\$\{?root\}?/)?internal/enginefixture`).MatchString(l.text) {
			t.Errorf("contract.sh:%d writes into internal/enginefixture itself: only engine-fixture-import does, after its checks: %s", l.n, strings.TrimSpace(l.text))
		}
	}
}
