package pager

import (
	"errors"
	"slices"
	"testing"
)

// The pager used to be started with a fixed -R. A bare PAGER=less would lose
// the colours of a log, so Resolve adds -R to less unless the line already
// passes control characters through. Any other program is left as written.
func TestResolveKeepsTheColoursOfLess(t *testing.T) {
	tests := []struct {
		name string
		env  string
		goos string
		want []string
	}{
		{"a bare less gets -R", "less", "linux", []string{"less", "-R"}},
		{"less with other options gets -R", "less -F -X", "linux", []string{"less", "-F", "-X", "-R"}},
		{"a path to less", "/usr/bin/less", "linux", []string{"/usr/bin/less", "-R"}},
		{"a quoted Windows path to less", `"C:\Tools\less.exe"`, "windows", []string{`C:\Tools\less.exe`, "-R"}},
		{"-R is kept as is", "less -R", "linux", []string{"less", "-R"}},
		{"-r is kept as is", "less -r", "linux", []string{"less", "-r"}},
		{"a cluster with R is kept as is", "less -FRX", "linux", []string{"less", "-FRX"}},
		{"the long form is kept as is", "less --raw-control-chars", "linux", []string{"less", "--raw-control-chars"}},
		{"another pager is not touched", "most", "linux", []string{"most"}},
		{"a name that merely starts with less", "lessfilter", "linux", []string{"lessfilter"}},
		{"bat is not touched", "bat --paging=always", "linux", []string{"bat", "--paging=always"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve("", tt.env, tt.goos)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", tt.env, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Resolve(%q) = %q, want %q", tt.env, got, tt.want)
			}
		})
	}
}

// A line whose program name is empty has nothing to start. It is refused with
// its own error, not turned into an empty exec.
func TestResolveRefusesAnEmptyProgram(t *testing.T) {
	for _, line := range []string{`""`, `''`, `"" -R`, `'' less`} {
		t.Run(line, func(t *testing.T) {
			got, err := Resolve("", line, "linux")
			if !errors.Is(err, ErrEmptyProgram) {
				t.Errorf("Resolve(%q) = %q, %v; want ErrEmptyProgram", line, got, err)
			}
		})
	}
	if _, err := Resolve(`""`, "less", "linux"); !errors.Is(err, ErrEmptyProgram) {
		t.Errorf("an empty configured program fell back instead of failing: %v", err)
	}
}
