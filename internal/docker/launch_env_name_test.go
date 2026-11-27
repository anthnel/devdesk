package docker

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-14: the name rule is ASCII only, ^[A-Za-z0-9_][A-Za-z0-9_.-]*$. A wildcard
// would make Podman copy every variable of DevDesk starting with the prefix
// (`-e PREFIX*`), a leading dash would read as an engine flag, and invisible or
// bidirectional characters would hide what the name really is.
var envNameCases = []struct {
	name  string
	input string
	want  bool
}{
	{"upper snake", "API_TOKEN", true},
	{"a dot and a dash inside", "my-app.setting", true},
	{"a leading underscore", "_X", true},
	{"a leading digit", "1ABC", true},
	{"a trailing dash", "A-", true},
	{"a star", "*", false},
	{"a prefix and a star", "PREFIX*", false},
	{"a star inside", "PRE*FIX", false},
	{"a question mark", "?", false},
	{"a question mark inside", "A?B", false},
	{"a flag", "-e", false},
	{"a long flag", "--privileged", false},
	{"a short flag", "-V", false},
	{"a leading dot", ".hidden", false},
	{"a right-to-left override", "A\u202eB", false},
	{"a zero-width space", "A\u200bB", false},
	{"an accent", "caf\u00e9", false},
	{"an emoji", "A\U0001F600", false},
	{"a non-ASCII digit", "A\u0661", false},
	{"a bracket", "A[0]", false},
	{"a slash", "A/B", false},
}

func TestValidEnvNameIsASCIIOnlyAndRefusesWildcards(t *testing.T) {
	for _, tt := range envNameCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidEnvName(tt.input); got != tt.want {
				t.Errorf("ValidEnvName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// A wildcard entry is refused before anything runs, on both engines, by both
// entry points, and the error does not repeat it.
func TestAWildcardOrFlagNameIsRefusedBeforeAnythingRuns(t *testing.T) {
	entries := []string{"*=x", "PREFIX*=x", "?=x", "-e=x", "--privileged=x", "-V=x", "A\u202eB=x", "A\u200bB=x", "caf\u00e9=x"}
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, entry := range entries {
			t.Run(entry, func(t *testing.T) {
				s := stubStrict(t) // any engine call fails the test

				id, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Env: []string{"REGION=eu", entry}})
				if !errors.Is(err, ErrInvalidLaunchEnv) || id != "" {
					t.Errorf("LaunchContainer() = %q, %v, want ErrInvalidLaunchEnv", id, err)
				}
				cmd, err := BuildLaunchCmd(ContainerLaunchOptions{Image: "img", Env: []string{entry}, Interactive: true, TTY: true})
				if !errors.Is(err, ErrInvalidLaunchEnv) || cmd != nil {
					t.Errorf("BuildLaunchCmd() = %v, %v, want ErrInvalidLaunchEnv and no command", cmd, err)
				}
				if len(s.calls) != 0 {
					t.Errorf("the engine was reached for a refused launch: %v", s.calls)
				}
				if name, _, _ := strings.Cut(entry, "="); err != nil && strings.Contains(err.Error(), name) {
					t.Errorf("error %q repeats the name %q", err, name)
				}
			})
		}
	})
}
