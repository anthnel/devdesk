// Package stepshell pastes a command a finding's Steps tell the user to copy
// into a real POSIX shell (DEVDESK-772), and reports what the shell did with it.
//
// The folder names in a step come from a repository DevDesk does not trust. A
// step is correct only if a shell reads the folder as one literal argument and
// runs nothing else, and no string comparison proves that: this runs it.
//
// The shell is `sh -c` in a fresh temporary directory, with a stub in place of
// each program the command names. A stub runs nothing: it records the argv it
// received. A name that breaks out of its word shows up as a file `pwned`
// created by the injected `touch pwned`, as a different argv, or as a stub never
// called.
//
// It imports testing and is meant to be imported from _test.go files only, so
// it never reaches the production binary.
package stepshell

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// Result is what the shell did with a pasted command.
type Result struct {
	// Called says a stub ran. False means the shell never reached the program:
	// a syntax error, or a word that swallowed the whole command.
	Called bool
	// Args is the argv the stub received, without the program name.
	Args []string
	// Pwned says the file `pwned` exists in the directory the command ran in:
	// something the command was not meant to run created it.
	Pwned bool
	// Stderr is what the shell printed on its error output.
	Stderr string
}

// tails are what a step says after the command it holds, longest first. A command
// ends where its sentence continues, or at the period that closes the step.
var tails = []string{
	" and fix the first error it reports.",
	" again to confirm.",
	".",
}

// Command is the command a step tells the user to copy: the text from program
// (for example "helm template") to the end of the command, without the words
// that continue the sentence. A command is introduced by "run" ("Run helm lint
// ...", "..., run helm dependency build ..."): a sentence that merely names the
// program ("Fix what helm lint reports in ...") holds none. It reports false
// when the step holds no such command. A step that is not a copyable command,
// such as a text saying a folder name needs quoting, must give false for the
// program it would run.
func Command(step, program string) (string, bool) {
	loc := regexp.MustCompile(`(?i)\brun ` + regexp.QuoteMeta(program) + ` `).FindStringIndex(step)
	if loc == nil {
		return "", false
	}
	rest := step[loc[0]+len("run "):]
	for _, tail := range tails {
		if cmd, found := strings.CutSuffix(rest, tail); found {
			return cmd, true
		}
	}
	return rest, true
}

// Available skips the test when no POSIX shell can run the command.
func Available(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the steps are written for a POSIX shell")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
}

// Run pastes command into `sh -c`, from a fresh temporary directory that holds
// three decoy files (a1, a2 and x) so that an unquoted glob has something to
// expand to, and with a recording stub for each of programs (the first word of
// the command, such as "helm" or "kustomize"). It skips the test when there is
// no shell.
func Run(t testing.TB, command string, programs ...string) Result {
	t.Helper()
	Available(t)

	cwd, stubs, out := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "argv")
	for _, decoy := range []string{"a1", "a2", "x"} {
		if err := os.WriteFile(filepath.Join(cwd, decoy), nil, 0o600); err != nil {
			t.Fatalf("decoy file: %v", err)
		}
	}
	// One argument per NUL-terminated record: no argument can hold a NUL.
	const stub = "#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$STEPSHELL_OUT\"\n"
	for _, program := range programs {
		if err := os.WriteFile(filepath.Join(stubs, program), []byte(stub), 0o700); err != nil {
			t.Fatalf("stub %s: %v", program, err)
		}
	}

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	cmd.Env = []string{
		"PATH=" + stubs + string(os.PathListSeparator) + "/usr/bin:/bin",
		"STEPSHELL_OUT=" + out,
		"HOME=" + cwd,
		"LC_ALL=C",
	}
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	_ = cmd.Run() // a failure is a Result, not an error: the test reads Called and Stderr

	res := Result{Stderr: stderr.String()}
	if _, err := os.Stat(filepath.Join(cwd, "pwned")); err == nil {
		res.Pwned = true
	}
	if raw, err := os.ReadFile(out); err == nil {
		res.Called = true
		if len(raw) > 0 {
			res.Args = strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
		}
	}
	return res
}
