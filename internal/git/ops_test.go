package git

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// requireGit skips the test when no git binary is available.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
}

// seedRepo creates a git repository holding a single committed file and returns
// its path. Identity and signing are forced on the command line so the test does
// not depend on the machine's git configuration.
func seedRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating source dir: %v", err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# seed\n"), 0o600); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.name=devdesk", "-c", "user.email=devdesk@example.com",
		"-c", "commit.gpgsign=false", "commit", "-m", "seed")

	return dir
}

func TestCloneCopiesRepositoryContents(t *testing.T) {
	requireGit(t)
	source := seedRepo(t)
	target := filepath.Join(t.TempDir(), "clone")

	if err := Clone(source, target, CloneOptions{}); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Errorf("cloned repo is missing README.md: %v", err)
	}
	if !DirExists(filepath.Join(target, ".git")) {
		t.Error("cloned repo has no .git directory")
	}
}

func TestCloneFailsOnMissingSource(t *testing.T) {
	requireGit(t)
	source := filepath.Join(t.TempDir(), "does-not-exist")
	target := filepath.Join(t.TempDir(), "clone")

	if err := Clone(source, target, CloneOptions{}); err == nil {
		t.Error("Clone() from a missing source returned no error")
	}
}

func TestCloneFailsWhenTargetIsNotEmpty(t *testing.T) {
	requireGit(t)
	source := seedRepo(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing existing file: %v", err)
	}

	if err := Clone(source, target, CloneOptions{}); err == nil {
		t.Error("Clone() into a non-empty directory returned no error")
	}
}

// A failure reports git's own reason. It is what the clone list's Detail column
// shows, and without it a failed row says only that something went wrong.
func TestCloneReportsGitsReason(t *testing.T) {
	requireGit(t)
	source := filepath.Join(t.TempDir(), "does-not-exist")

	err := Clone(source, filepath.Join(t.TempDir(), "clone"), CloneOptions{})

	if err == nil {
		t.Fatal("Clone() from a missing source returned no error")
	}
	// "exit status 128" is what the bare ExitError says, and it is what this
	// used to return: git's stderr went to the null device.
	if strings.HasPrefix(err.Error(), "exit status") {
		t.Errorf("Clone() reported only an exit status: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "repository") {
		t.Errorf("Clone() error does not name the problem: %v", err)
	}
}

// Nothing a clone runs may prompt. DevDesk owns the terminal: git's own prompt
// is skipped because stdin is the null device, so the *credential helper* takes
// over — a separate process that writes to the console over the rendered frame
// and then waits for a browser. The row spins for ever and the screen is
// corrupted, which is what was observed.
func TestTheCloneEnvironmentForbidsEveryPrompt(t *testing.T) {
	env := nonInteractiveEnv("")

	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0", // git's own prompt
		"GCM_INTERACTIVE=never", // Git Credential Manager's browser flow
		"GIT_ASKPASS=",          // the GUI hook git would fall back to
		"SSH_ASKPASS=",          // and ssh's
	} {
		if !slices.Contains(env, want) {
			t.Errorf("the clone environment is missing %q", want)
		}
	}
	// BatchMode covers ssh's own two prompts: a key passphrase and an unknown
	// host key. Both otherwise hang exactly the same way.
	if !slices.ContainsFunc(env, func(v string) bool {
		return strings.HasPrefix(v, "GIT_SSH_COMMAND=") && strings.Contains(v, "BatchMode=yes")
	}) {
		t.Error("the clone environment does not put ssh in batch mode")
	}
}

// A stalled transfer is aborted by git, not by killing it: git cleans up the
// directory it was writing into, so a clone still never leaves half a
// repository behind (§3.16, decision 12).
func TestTheCloneEnvironmentBoundsAStalledTransfer(t *testing.T) {
	env := nonInteractiveEnv("")

	if !slices.Contains(env, "GIT_CONFIG_KEY_0=http.lowSpeedLimit") {
		t.Errorf("no low-speed abort is configured:\n%v", gitConfigOf(env))
	}
	if !slices.Contains(env, "GIT_CONFIG_KEY_1=http.lowSpeedTime") {
		t.Errorf("no low-speed window is configured:\n%v", gitConfigOf(env))
	}
}

// The token travels in the environment. A command line is readable from the
// process list by anyone on the machine, and the URL form is written into every
// cloned repository's .git/config and stays there.
func TestTheTokenTravelsInTheEnvironmentAsBasicAuth(t *testing.T) {
	env := nonInteractiveEnv("glpat-secret")

	want := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("oauth2:glpat-secret"))
	if !slices.Contains(env, "GIT_CONFIG_VALUE_2="+want) {
		t.Errorf("the token is not passed as a Basic header:\n%v", gitConfigOf(env))
	}
	if !slices.Contains(env, "GIT_CONFIG_COUNT=3") {
		t.Errorf("GIT_CONFIG_COUNT does not cover the header:\n%v", gitConfigOf(env))
	}
}

// No token means no header, and a count that still matches what is set — an
// over-count makes git fail on every clone, including the public ones that
// need no token at all.
func TestWithoutATokenTheHeaderIsAbsentAndTheCountMatches(t *testing.T) {
	env := nonInteractiveEnv("")

	if !slices.Contains(env, "GIT_CONFIG_COUNT=2") {
		t.Errorf("GIT_CONFIG_COUNT does not match what is set:\n%v", gitConfigOf(env))
	}
	for _, v := range env {
		if strings.Contains(v, "Authorization") {
			t.Errorf("an empty token still produced a header: %q", v)
		}
	}
}

// gitConfigOf returns the GIT_CONFIG_* entries, for failure messages.
func gitConfigOf(env []string) []string {
	var out []string
	for _, v := range env {
		if strings.HasPrefix(v, "GIT_CONFIG_") {
			out = append(out, v)
		}
	}
	return out
}

func TestLastLineIsGitsReasonNotItsProgress(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"the reason is last", "Cloning into 'x'...\nfatal: repository not found\n", "fatal: repository not found"},
		{"carriage returns are progress", "remote: Counting\rremote: Done\rfatal: boom\n", "fatal: boom"},
		{"trailing blank lines", "fatal: boom\n\n\n", "fatal: boom"},
		{"nothing at all", "   \n\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lastLine(tt.output); got != tt.want {
				t.Errorf("lastLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDirExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"existing directory", dir, true},
		{"nested missing path", filepath.Join(dir, "nope", "deeper"), false},
		{"a file is not a directory", file, false},
		{"empty path", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DirExists(tt.path); got != tt.want {
				t.Errorf("DirExists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
