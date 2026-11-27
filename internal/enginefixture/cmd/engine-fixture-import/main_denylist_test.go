package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OC-09a3 (DEVDESK-944), security finding M1: with --require-denylist (what
// contract.sh record passes) the import is refused when DEVDESK_FIXTURE_DENYLIST is
// unset, names a file that cannot be read, or names a file with no active term.
// Without the option the behavior is the one of main_test.go: a note on stdout.

func denyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "deny.txt")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRequireDenylistRefusesWithoutAList(t *testing.T) {
	tests := []struct {
		name string
		env  func(t *testing.T) func(string) string
	}{
		{"the variable is not set", func(*testing.T) func(string) string { return noEnv }},
		{"the variable is empty", func(*testing.T) func(string) string { return envOf("DEVDESK_FIXTURE_DENYLIST", "") }},
		{"an empty file", func(t *testing.T) func(string) string {
			return envOf("DEVDESK_FIXTURE_DENYLIST", denyFile(t, ""))
		}},
		{"a file of comments and blank lines", func(t *testing.T) func(string) string {
			return envOf("DEVDESK_FIXTURE_DENYLIST", denyFile(t, "# nothing yet\n\n   \n# still nothing\n"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, into := t.TempDir(), t.TempDir()
			put(t, from, cliFx("containers", "a", "6.2.1", "2026-11-27", "a\n"))

			code, stdout, stderr := invoke(t, tt.env(t), "--from", from, "--into", into, "--versions", versionsFile(t), "--require-denylist")

			if code != 1 || stdout != "" {
				t.Errorf("exit %d, stdout %q, want 1 and nothing on stdout", code, stdout)
			}
			for _, want := range []string{"engine-fixture-import:", "--require-denylist", "DEVDESK_FIXTURE_DENYLIST", "domains, project and customer names"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr does not contain %q: %s", want, stderr)
				}
			}
			if files := listing(t, into); len(files) != 0 {
				t.Errorf("files written without the deny list that was required: %v", files)
			}
		})
	}
}

func TestRequireDenylistImportsWithAListThatHasATerm(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", "6.2.1", "2026-11-27", "a\n"))
	env := envOf("DEVDESK_FIXTURE_DENYLIST", denyFile(t, "# mine\nzorkmid-customer\n"))

	code, stdout, stderr := invoke(t, env, "--from", from, "--into", into, "--versions", versionsFile(t), "--require-denylist")

	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q, want 0", code, stderr)
	}
	if line, _, _ := strings.Cut(stdout, "\n"); !strings.HasPrefix(line, "engine-contract:record: podman 6.2.1 rootless: 1 written") {
		t.Errorf("first line of stdout = %q", line)
	}
}

// An unreadable list is still the error that names the variable, option or not.
func TestRequireDenylistWithAnUnreadableListNamesTheVariable(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", "6.2.1", "2026-11-27", "a\n"))
	env := envOf("DEVDESK_FIXTURE_DENYLIST", filepath.Join(t.TempDir(), "absent.txt"))

	code, _, stderr := invoke(t, env, "--from", from, "--into", into, "--versions", versionsFile(t), "--require-denylist")

	if code != 1 || !strings.Contains(stderr, "DEVDESK_FIXTURE_DENYLIST") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// Without the option, the import of a verification run goes ahead with a note.
func TestWithoutTheOptionNoListIsOnlyANote(t *testing.T) {
	from, into := t.TempDir(), t.TempDir()
	put(t, from, cliFx("containers", "a", "6.2.1", "2026-11-27", "a\n"))

	code, stdout, stderr := invoke(t, noEnv, "--from", from, "--into", into, "--versions", versionsFile(t))

	if code != 0 || stderr != "" || !strings.Contains(stdout, "deny list: ") || !strings.Contains(stdout, "not used") {
		t.Errorf("exit %d, stdout %q, stderr %q, want 0 and a note that the list was not used", code, stdout, stderr)
	}
}
