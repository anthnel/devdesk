package pager

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// OC-84 (DEVDESK-801, requirement S1). The pager a user picks (the PAGER
// variable, or the configuration) is a command line with arguments, such as
// `less -R`. DevDesk cuts it into argv elements itself; it is never handed to a
// shell, so a shell metacharacter is an ordinary character.
//
// This package is new. The API these tests expect:
//
//	var ErrUnterminatedQuote = errors.New(...)
//	func Split(line string) ([]string, error)
//	func Resolve(configured, env, goos string) ([]string, error)
//
// Split cuts on blanks (space, tab), and a single or double quote groups
// elements that hold blanks; the quote characters are dropped; a backslash is
// an ordinary character (a Windows path); nothing else is special, and nothing
// is expanded (no variable, no `~`, no glob).
// Resolve picks the line (configured, then env, then the platform default
// `less -R`, or `more` on windows, a blank value counting as unset) and splits
// it. A pager that cannot be split is an error, never a silent fallback.

func TestSplitCutsACommandLineIntoArgv(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"a command with an argument", "less -R", []string{"less", "-R"}},
		{"several arguments", "less -R -F -X", []string{"less", "-R", "-F", "-X"}},
		{"a bare command", "less", []string{"less"}},
		{"extra blanks and tabs", "  less \t -R  ", []string{"less", "-R"}},
		{"double quotes group a path with a space", `"/opt/my pager/less" -R`, []string{"/opt/my pager/less", "-R"}},
		{"single quotes group a path with a space", `'/opt/my pager/less' -R`, []string{"/opt/my pager/less", "-R"}},
		{"a quote inside the other kind", `less "-x'y"`, []string{"less", "-x'y"}},
		{"a backslash is a character, for Windows paths", `"C:\Program Files\Git\usr\bin\less.exe" -R`, []string{`C:\Program Files\Git\usr\bin\less.exe`, "-R"}},
		{"nothing at all", "", nil},
		{"only blanks", " \t ", nil},

		// The metacharacters of a shell are ordinary characters.
		{"a semicolon", "less; touch /tmp/pwned", []string{"less;", "touch", "/tmp/pwned"}},
		{"a semicolon between spaces", "less ; rm -rf ~", []string{"less", ";", "rm", "-rf", "~"}},
		{"a command substitution", "less $(touch /tmp/pwned)", []string{"less", "$(touch", "/tmp/pwned)"}},
		{"backticks", "less `id`", []string{"less", "`id`"}},
		{"a pipe", "less | sh", []string{"less", "|", "sh"}},
		{"and, or, redirect", "less && id || id > /tmp/x", []string{"less", "&&", "id", "||", "id", ">", "/tmp/x"}},
		{"a variable is not expanded", "less $HOME ${HOME}", []string{"less", "$HOME", "${HOME}"}},
		{"a tilde is not expanded", "~/bin/less", []string{"~/bin/less"}},
		{"a glob is not expanded", "less *.log", []string{"less", "*.log"}},
		{"a quoted metacharacter stays whole", `less "a;b|c$(d)"`, []string{"less", "a;b|c$(d)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Split(tt.line)
			if err != nil {
				t.Fatalf("Split(%q) error = %v", tt.line, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Split(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestSplitRefusesAnUnterminatedQuote(t *testing.T) {
	for _, line := range []string{`"less -R`, `'less`, `less "-R`, `less '-R -F`} {
		t.Run(line, func(t *testing.T) {
			got, err := Split(line)
			if !errors.Is(err, ErrUnterminatedQuote) {
				t.Errorf("Split(%q) = %q, %v; want ErrUnterminatedQuote", line, got, err)
			}
		})
	}
}

func TestResolvePicksTheConfigurationThenTheEnvironmentThenTheDefault(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		env        string
		goos       string
		want       []string
	}{
		{"the configuration wins over the environment", "bat --paging=always", "less -R", "linux", []string{"bat", "--paging=always"}},
		{"the environment when nothing is configured", "", "less -R -F", "linux", []string{"less", "-R", "-F"}},
		{"a blank configuration counts as unset", "  ", "most", "linux", []string{"most"}},
		{"the default on unix", "", "", "linux", []string{"less", "-R"}},
		{"the default on macOS", "", "", "darwin", []string{"less", "-R"}},
		{"blank everywhere", " ", "\t", "linux", []string{"less", "-R"}},
		{"the default on windows", "", "", "windows", []string{"more"}},
		{"the environment on windows", "", `"C:\Tools\less.exe" -R`, "windows", []string{`C:\Tools\less.exe`, "-R"}},
		{"metacharacters in the environment stay arguments", "", "less; touch x", "linux", []string{"less;", "touch", "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.configured, tt.env, tt.goos)
			if err != nil {
				t.Fatalf("Resolve(%q, %q, %q) error = %v", tt.configured, tt.env, tt.goos, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Resolve(%q, %q, %q) = %q, want %q", tt.configured, tt.env, tt.goos, got, tt.want)
			}
		})
	}
}

// A pager that cannot be read is reported, not replaced: falling back to the
// environment, or to less, would run a program the user did not pick.
func TestResolveDoesNotFallBackFromABrokenPager(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		env        string
	}{
		{"broken configuration, good environment", `"less -R`, "less -R"},
		{"broken environment", "", `'less`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.configured, tt.env, "linux")
			if !errors.Is(err, ErrUnterminatedQuote) {
				t.Errorf("Resolve(%q, %q) = %q, %v; want ErrUnterminatedQuote", tt.configured, tt.env, got, err)
			}
		})
	}
}

// Criterion 3 end to end: the argv a hostile PAGER resolves to is started the
// way DevDesk starts it (exec, no shell), and what the metacharacters ask for
// does not happen. Each line would create the marker file under a shell.
func TestAHostilePagerNeverReachesAShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses true(1)")
	}
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("no true(1) on this machine")
	}
	for _, template := range []string{
		"true; touch %s",
		"true $(touch %s)",
		"true `touch %s`",
		"true | touch %s",
		"true && touch %s",
		"true > %s",
		"true & touch %s",
	} {
		t.Run(template, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "marker")
			line := template
			for i := 0; i+1 < len(template); i++ {
				if template[i] == '%' && template[i+1] == 's' {
					line = template[:i] + marker + template[i+2:]
				}
			}

			argv, err := Resolve("", line, "linux")
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", line, err)
			}
			// The run may fail ("true;" is not a program) or succeed (true with
			// odd arguments); both are fine. Only the marker matters.
			_ = exec.Command(argv[0], argv[1:]...).Run() //nolint:gosec // argv of a fixed test line
			if _, err := os.Stat(marker); err == nil {
				t.Errorf("%q created %s: a shell interpreted it", line, marker)
			}
		})
	}
}
