package enginecontract_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// OC-09a3 (DEVDESK-944), security finding M1: a recording needs the external deny
// list. contract.sh refuses it before any connection when DEVDESK_FIXTURE_DENYLIST
// is unset, names a file that cannot be read or holds no active term, and runs the
// import with --require-denylist, so the command refuses as well. A verification
// run needs no list. The script is run only as far as its refusal (the VM host does
// not resolve); the rest is read as text.

const denylistMessage = "record needs a deny list: set DEVDESK_FIXTURE_DENYLIST to a file of terms (domains, project and customer names)"

func TestTheScriptRecordRefusesWithoutADenyListBeforeTheVM(t *testing.T) {
	file := func(content string) string {
		p := filepath.Join(t.TempDir(), "deny.txt")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tests := []struct {
		name string
		env  []string
	}{
		{"the variable is not set", nil},
		{"the variable is empty", []string{"DEVDESK_FIXTURE_DENYLIST="}},
		{"a file that does not exist", []string{"DEVDESK_FIXTURE_DENYLIST=" + filepath.Join(t.TempDir(), "absent.txt")}},
		{"an empty file", []string{"DEVDESK_FIXTURE_DENYLIST=" + file("")}},
		{"comments and blank lines only", []string{"DEVDESK_FIXTURE_DENYLIST=" + file("# todo\n\n   \n# later\n")}},
		{"a directory", []string{"DEVDESK_FIXTURE_DENYLIST=" + t.TempDir()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := append([]string{"DEVDESK_ENGINE_CONTRACT=1"}, tt.env...)

			out, code := runScript(t, scriptEnv(env...), "record", "podman", "n")

			if code != 1 || !strings.Contains(out, denylistMessage) {
				t.Errorf("exit code = %d, want 1 and the message %q\n%s", code, denylistMessage, out)
			}
			noRemoteTrace(t, out)
		})
	}
}

// The deny list is checked after the arguments (a usage error stays a usage error)
// and before the first thing that reaches the VM host or builds for it.
func TestTheScriptChecksTheDenyListBeforeAnyRemoteCall(t *testing.T) {
	lines := scriptCode(t)
	check := first(lines, regexp.MustCompile(`record needs a deny list`), 0)
	args := first(lines, regexp.MustCompile(`the docker VM is rootful`), 0)
	remote := first(lines, remoteUse, 0)
	lib := first(lines, regexp.MustCompile(`lib\.sh`), 0)
	if check < 0 || args < 0 || remote < 0 || lib < 0 {
		t.Fatalf("indexes: deny list check %d, argument check %d, first remote use %d, lib.sh %d", check, args, remote, lib)
	}
	if check < args || check > remote || check > lib {
		t.Errorf("the deny list check is at line %d, want it after the arguments (line %d) and before lib.sh (%d) and the first remote use (%d)",
			lines[check].n, lines[args].n, lines[lib].n, lines[remote].n)
	}
	if !inRecordBranch(lines, check-1) && !inRecordBranch(lines, check) {
		t.Errorf("contract.sh:%d refuses a missing deny list outside a condition on the record action: verify needs none", lines[check].n)
	}
}

func TestTheScriptImportsWithTheDenyListRequired(t *testing.T) {
	lines := scriptCode(t)
	imp := first(lines, regexp.MustCompile(`engine-fixture-import`), 0)
	if imp < 0 {
		t.Fatal("contract.sh never runs engine-fixture-import")
	}
	if stmt := statement(lines, imp); !strings.Contains(stmt, "--require-denylist") {
		t.Errorf("contract.sh:%d imports without --require-denylist: a recording is not imported without the names only you know:\n%s", lines[imp].n, stmt)
	}
}
