package enginefixture_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/fixtureleak"
)

// OC-08b, criterion 1: every fixture file of the tree passes the leak check, and
// a file that holds a user name, a host name or a personal path fails it. A
// failure names the file, the line and the rule, never the text found.

// leakFindings runs the leak check on every file the patterns match, byte for
// byte as it sits on disk, pending files included, and returns what was scanned
// and one finding per leak: "file:line: leak, rule "rule"".
func leakFindings(t testing.TB, patterns []string, exact, local []string) (files []string, findings []string) {
	t.Helper()
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(file), ".cli")
		for _, l := range enginefixture.CheckLeaksAs(name, data, exact, local) {
			findings = append(findings, fmt.Sprintf("%s:%d: leak, rule %q", file, l.Line, l.Rule))
		}
	}
	return files, findings
}

// TestFixturesLeakNothing is the guard: the versioned fixtures (none before
// OC-09b) and the synthetic ones, with the external deny list and the identities
// of the machine that runs the test.
func TestFixturesLeakNothing(t *testing.T) {
	exact, note, err := fixtureleak.ReadDenylist(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	local := enginefixture.MachineIdentities()
	t.Logf("external deny list: %s", note)
	t.Logf("%d local identity term(s) applied", len(local))

	files, findings := leakFindings(t, []string{
		filepath.Join("fixtures", "*", "*.cli"),
		filepath.Join("testdata", "*", "*.cli"),
	}, exact, local)
	t.Logf("%d fixture file(s) scanned, pending ones included", len(files))
	if len(files) < len(syntheticNames(t)) {
		t.Fatalf("%d files scanned, fewer than the %d synthetic ones: the patterns miss files", len(files), len(syntheticNames(t)))
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// writeTree writes files (path relative to the root -> content) under a new
// directory and returns it.
func writeTree(t *testing.T, files map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	for rel, data := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// Criterion 1: a fixture with a user name, a host name or a personal path makes
// the check fail, and the failure names file, line and rule without the text.
func TestACheckedTreeFailsOnAUserNameAHostNameOrAPersonalPath(t *testing.T) {
	const camp = "camp/leaky--docker-29.10.1-rootful.cli"
	tests := []struct {
		name    string
		file    string
		data    []byte
		local   []string
		exact   []string
		planted string // never in a finding
		rule    string
		line    int
	}{
		{
			name: "a personal path", file: camp, data: plantInto(t, inStdout("see /home/alice/x")),
			planted: "alice", rule: rPath,
		},
		{
			name: "the user name of the machine", file: camp, data: plantInto(t, inStdout("owner zedowner")),
			local: enginefixture.LocalIdentityTerms("zedowner"), planted: "zedowner", rule: rLocal,
		},
		{
			name: "the host name of the machine", file: camp, data: plantInto(t, inStderr("cannot reach zedhost")),
			local: enginefixture.LocalIdentityTerms("zedhost"), planted: "zedhost", rule: rLocal,
		},
		{
			name: "a lab name", file: camp, data: plantInto(t, inStdout("on dd-podman")),
			planted: "dd-podman", rule: rLab,
		},
		{
			name: "a term of the external deny list", file: camp, data: plantInto(t, inStdout("zz-denied-term")),
			exact: []string{"zz-denied-term"}, planted: "zz-denied-term", rule: rExact,
		},
		{
			name: "a leak in a base64 stream", file: camp, data: plantInto(t, inBinaryStdout("/home/alice/x")),
			planted: "alice", rule: rPath,
		},
		{
			name: "a pending file", file: camp, data: plantInto(t, asPending(inArgv("run", "-v", "/home/alice/x:/x", "alpine"))),
			planted: "alice", rule: rPath,
		},
		{
			name: "a file that is not a valid fixture", file: "camp/broken.cli", data: []byte("# tool: docker\nsee /home/alice/x\n"),
			planted: "alice", rule: rPath,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeTree(t, map[string][]byte{tt.file: tt.data})
			_, findings := leakFindings(t, []string{filepath.Join(root, "*", "*.cli")}, tt.exact, tt.local)
			if len(findings) == 0 {
				t.Fatal("the check found nothing: a fixture with this leak would be versioned")
			}
			wantFile := filepath.Join(root, filepath.FromSlash(tt.file))
			wantLine := leakLineOf(string(tt.data), tt.planted)
			if tt.rule == rPath && strings.Contains(tt.name, "base64") {
				wantLine = leakLineOf(string(tt.data), "# stdout:")
			}
			want := fmt.Sprintf("%s:%d: leak, rule %q", wantFile, wantLine, tt.rule)
			found := false
			for _, f := range findings {
				found = found || f == want
				if strings.Contains(f, tt.planted) && !strings.Contains(wantFile, tt.planted) {
					t.Errorf("a finding repeats the text found: %s", f)
				}
			}
			if !found {
				t.Errorf("no finding %s, got %v", want, findings)
			}
		})
	}
}

func TestACheckedTreeOfCleanFilesHasNoFinding(t *testing.T) {
	files := map[string][]byte{
		"camp/clean--docker-29.10.1-rootful.cli":   plantInto(t, inStdout("nothing here")),
		"camp/pending--docker-29.10.1-rootful.cli": plantInto(t, asPending(inArgv("run", "-d", "alpine:3.20"))),
		"camp/notes.txt": []byte("/home/alice is not a fixture file and is not scanned\n"),
	}
	root := writeTree(t, files)
	scanned, findings := leakFindings(t, []string{filepath.Join(root, "*", "*.cli")}, nil, enginefixture.LocalIdentityTerms("zedhost"))
	if len(scanned) != 2 {
		t.Errorf("%d files scanned, want the 2 .cli files", len(scanned))
	}
	if len(findings) != 0 {
		t.Errorf("findings on a clean tree: %v", findings)
	}
}
