package docker

import (
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-14 (DEVDESK-812, requirement S9): the values typed in the launch form never
// reach the process list. The argv of `<engine> run` carries `-e NAME`; the value
// travels in the environment of the child process, which DevDesk builds
// explicitly: its own environment plus exactly the variables the form set.
//
// Expected production API, pinned by these tests:
//
//	var ErrInvalidLaunchEnv error
//	    returned (wrapped is fine, errors.Is) by LaunchContainer and BuildLaunchCmd
//	    when an Env entry has no "=" or a name ValidEnvName refuses. Its text never
//	    holds the entry. Nothing is started, nothing is built.
//	func ValidEnvName(name string) bool
//	    the one rule for a variable name; the launch form calls it too, so the form
//	    and the launcher cannot disagree.
//	ContainerLaunchOptions.Env stays []string of "NAME=VALUE". buildLaunchArgs
//	    keeps its signature and emits "-e", NAME only.
//	BuildLaunchCmd(opts).Env is non-nil as soon as a variable is typed, and holds
//	    os.Environ() followed by the typed entries (the last one wins, as exec).
//	cliRunner.Run applies the same environment to a LaunchContainer (a new
//	    dockerCmd.Env field, whatever the plumbing).
//
// The real runner is used against fakecli, which journals the argv AND the
// environment the child was started with (Invocation.Env): no extension of
// fakecli is needed.

// hostOnlyVar is set in the environment of the test process: a variable DevDesk
// holds that the form never named. It must reach the child (it is part of the
// base environment) and must never be named in argv, or the engine would copy its
// host value into the container.
const (
	hostOnlyVar   = "DEVDESK_OC14_HOST_ONLY"
	hostOnlyValue = "host-secret-value"
	baseVar       = "DEVDESK_OC14_BASE"
)

// envMap reads NAME=VALUE entries the way exec does: the last entry of a name wins.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		name, value, _ := strings.Cut(e, "=")
		m[name] = value
	}
	return m
}

// withTyped returns the environment the child must have: base, then typed on top.
func withTyped(base, typed []string) map[string]string {
	want := envMap(base)
	// A blank entry sets nothing: os/exec drops an empty environment entry, so
	// the child holds no variable of that (empty) name.
	maps.Copy(want, envMap(slices.DeleteFunc(slices.Clone(typed), func(e string) bool { return e == "" })))
	return want
}

// useRealRunner swaps the stub for the production runner, which starts the engine
// named by engine.Current() through PATH: the fakecli tool.
func useRealRunner(t *testing.T) {
	t.Helper()
	stub(t, &stubRunner{}) // restores the runner and the engine when the test ends
	runner = cliRunner{}
}

// launchEnvCases are the environments a launch can carry. names is the `-e` list
// the argv must hold, in order; nothing else may name a variable.
var launchEnvCases = []struct {
	name  string
	env   []string
	names []string
}{
	{"a canary", []string{"API_TOKEN=canary"}, []string{"API_TOKEN"}},
	{"two variables keep their order", []string{"API_TOKEN=canary", "REGION=eu"}, []string{"API_TOKEN", "REGION"}},
	{"a value with equals signs and spaces", []string{"DSN=postgres://u:p@db/app?sslmode=disable&x=a b"}, []string{"DSN"}},
	{"a value that looks like an engine flag", []string{"EXTRA=--privileged"}, []string{"EXTRA"}},
	{"a value that looks like a volume", []string{"MOUNT=/etc:/host:rw"}, []string{"MOUNT"}},
	{"a value with commas", []string{"LIST=a,b,c"}, []string{"LIST"}},
	{"a value with non-ASCII text", []string{"GREETING=héllo wörld"}, []string{"GREETING"}},
	{"a variable that is not sensitive goes the same way", []string{"LOG_LEVEL=info"}, []string{"LOG_LEVEL"}},
	{"blank entries are skipped", []string{"", "REGION=eu", ""}, []string{"REGION"}},
	{"a typed variable replaces the same name of DevDesk", []string{baseVar + "=typed"}, []string{baseVar}},
	// An explicit empty value is a value: the launcher passes it as empty. It is the
	// form that refuses it (OC-14 criterion 3), because a variable the user left
	// empty is almost always one they forgot to fill in.
	{"an explicit empty value", []string{"EMPTY="}, []string{"EMPTY"}},
	{"no variable at all", nil, nil},
}

// wantEnvArgv is the argv a launch of `img` with these variable names builds.
func wantEnvArgv(names []string) []string {
	argv := []string{"run", "--pull=never"}
	for _, n := range names {
		argv = append(argv, "-e", n)
	}
	return append(argv, "img")
}

// typedValues returns the value of every entry, which must never be found in argv.
func typedValues(env []string) []string {
	var out []string
	for _, e := range env {
		if _, v, ok := strings.Cut(e, "="); ok && len(v) >= 3 {
			out = append(out, v)
		}
	}
	return out
}

// Criteria 1, 2 and 6: through the real runner, on both engines, the argv carries
// `-e NAME` and never the value, and the child environment is exactly the base
// environment of DevDesk plus the typed variables.
func TestLaunchContainerPutsTheValuesInTheChildEnvironmentNotInArgv(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range launchEnvCases {
			t.Run(tt.name, func(t *testing.T) {
				argv := wantEnvArgv(tt.names)
				fake := fakecli.Install(t, fakecli.Scenario{
					{Tool: shape.Binary, Args: argv, Reply: fakecli.Reply{Stdout: "9f2e1c8b7a6d\n"}},
				}, shape.Binary)
				useRealRunner(t)
				t.Setenv(hostOnlyVar, hostOnlyValue)
				t.Setenv(baseVar, "from-devdesk")
				base := os.Environ()

				id, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Env: tt.env})

				if err != nil {
					t.Fatalf("LaunchContainer() error = %v", err)
				}
				if id != "9f2e1c8b7a6d" {
					t.Errorf("LaunchContainer() = %q, want the ID the engine printed", id)
				}
				calls := fake.Invocations()
				if len(calls) != 1 {
					t.Fatalf("the engine was started %d times, want exactly one run: %v", len(calls), calls)
				}
				got := calls[0]
				if !got.Matched || !slices.Equal(got.Args, argv) {
					t.Errorf("argv = %q, want %q", got.Args, argv)
				}
				for _, v := range typedValues(tt.env) {
					for _, a := range got.Args {
						if strings.Contains(a, v) {
							t.Errorf("argv word %q holds the typed value %q: it would show in the process list", a, v)
						}
					}
				}
				if slices.Contains(got.Args, hostOnlyVar) {
					t.Errorf("argv names %s, which the form never set: the engine would copy the host value", hostOnlyVar)
				}

				if want := withTyped(base, tt.env); !maps.Equal(envMap(got.Env), want) {
					t.Errorf("child environment differs from DevDesk's environment plus the typed variables\n got %v\nwant %v",
						envMap(got.Env), want)
				}
				for _, e := range tt.env {
					name, value, ok := strings.Cut(e, "=")
					if !ok {
						continue
					}
					if v, found := envMap(got.Env)[name]; !found || v != value {
						t.Errorf("child environment %s = %q (set %v), want the typed %q", name, v, found, value)
					}
					if n := countName(got.Env, name); n != 1 {
						t.Errorf("child environment holds %s %d times, want once", name, n)
					}
				}
				if envMap(got.Env)[hostOnlyVar] != hostOnlyValue {
					t.Errorf("the base environment of DevDesk did not reach the child: %s missing", hostOnlyVar)
				}
			})
		}
	})
}

// countName counts the entries of env that set name.
func countName(env []string, name string) int {
	n := 0
	for _, e := range env {
		if k, _, _ := strings.Cut(e, "="); k == name {
			n++
		}
	}
	return n
}

// The interactive launch (-it) hands a *exec.Cmd to the terminal: its argv and its
// Env are what the process list and the child see, and the stub proves the same
// two things without starting anything.
func TestBuildLaunchCmdCarriesTheValuesInItsEnvironmentNotItsArgv(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range launchEnvCases {
			t.Run(tt.name, func(t *testing.T) {
				argv := wantEnvArgv(tt.names)
				stubStrict(t, reply{argv: argv})
				t.Setenv(hostOnlyVar, hostOnlyValue)
				t.Setenv(baseVar, "from-devdesk")

				cmd, err := BuildLaunchCmd(ContainerLaunchOptions{Image: "img", Env: tt.env})

				if err != nil {
					t.Fatalf("BuildLaunchCmd() error = %v", err)
				}
				if !slices.Equal(cmd.Args[1:], argv) {
					t.Errorf("argv = %q, want %q", cmd.Args[1:], argv)
				}
				for _, v := range typedValues(tt.env) {
					for _, a := range cmd.Args {
						if strings.Contains(a, v) {
							t.Errorf("argv word %q holds the typed value %q", a, v)
						}
					}
				}
				typed := slices.DeleteFunc(slices.Clone(tt.env), func(e string) bool { return e == "" })
				if len(typed) == 0 {
					return // nothing to carry: an implicit or an explicit base environment are both fine
				}
				if cmd.Env == nil {
					t.Fatal("cmd.Env is nil: the child would inherit DevDesk's environment implicitly, the values would not reach it")
				}
				if want := withTyped(os.Environ(), typed); !maps.Equal(envMap(cmd.Env), want) {
					t.Errorf("cmd.Env differs from DevDesk's environment plus the typed variables\n got %v\nwant %v",
						envMap(cmd.Env), want)
				}
			})
		}
	})
}

// Criteria 3 and 4 at the launcher: an entry that would make the engine copy the
// host's variable (`-e NAME` with no value of ours), or that names nothing usable,
// is refused whole. Nothing is started, nothing is partially run, and the error
// never repeats what was typed.
var refusedLaunchEnvCases = []struct {
	name string
	env  []string
}{
	{"a bare name that DevDesk's own environment holds", []string{"HOME"}},
	{"a bare name", []string{"API_TOKEN"}},
	{"a bare text that is itself a secret", []string{"sk-live-secretvalue"}},
	{"an empty name", []string{"=secretvalue"}},
	{"a lone equals sign", []string{"="}},
	{"a space in the name", []string{"API TOKEN=secretvalue"}},
	{"a tab in the name", []string{"API\tTOKEN=secretvalue"}},
	{"a newline in the name", []string{"API\nTOKEN=secretvalue"}},
	{"a leading space in the name", []string{" API_TOKEN=secretvalue"}},
	{"one bad entry among good ones refuses all", []string{"REGION=eu", "API_TOKEN", "LOG_LEVEL=info"}},
}

func TestLaunchContainerRefusesAnEntryThatWouldInheritTheHostEnvironment(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range refusedLaunchEnvCases {
			t.Run(tt.name, func(t *testing.T) {
				s := stubStrict(t) // any engine call fails the test, with its argv

				id, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Env: tt.env})

				if !errors.Is(err, ErrInvalidLaunchEnv) {
					t.Fatalf("LaunchContainer() error = %v, want ErrInvalidLaunchEnv", err)
				}
				if id != "" {
					t.Errorf("LaunchContainer() = %q, want no container", id)
				}
				if len(s.calls) != 0 {
					t.Errorf("the engine was called for a refused launch: %v", s.calls)
				}
				assertNoEntryIn(t, err, tt.env)
			})
		}
	})
}

func TestBuildLaunchCmdRefusesAnEntryThatWouldInheritTheHostEnvironment(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range refusedLaunchEnvCases {
			t.Run(tt.name, func(t *testing.T) {
				s := stubStrict(t)

				cmd, err := BuildLaunchCmd(ContainerLaunchOptions{Image: "img", Env: tt.env, Interactive: true, TTY: true})

				if !errors.Is(err, ErrInvalidLaunchEnv) {
					t.Fatalf("BuildLaunchCmd() error = %v, want ErrInvalidLaunchEnv", err)
				}
				if cmd != nil {
					t.Errorf("BuildLaunchCmd() built %q for a refused launch", cmd.Args)
				}
				if len(s.calls) != 0 {
					t.Errorf("the engine was reached for a refused launch: %v", s.calls)
				}
				assertNoEntryIn(t, err, tt.env)
			})
		}
	})
}

// assertNoEntryIn fails when the error text repeats an entry or the part of one
// that follows its first "=": the error reaches the log.
func assertNoEntryIn(t *testing.T, err error, env []string) {
	t.Helper()
	if err == nil {
		return
	}
	for _, e := range env {
		if e != "" && strings.Contains(err.Error(), e) {
			t.Errorf("error %q repeats the entry %q", err, e)
		}
		if _, v, ok := strings.Cut(e, "="); ok && v != "" && strings.Contains(err.Error(), v) {
			t.Errorf("error %q repeats the value %q", err, v)
		}
	}
}

// ValidEnvName is the one rule for a name, shared with the launch form. It only
// settles what the story names (a space, a "=", an empty name); the strictness of
// the rest (a leading digit, a dash) is left to the implementation and is not
// pinned here.
func TestValidEnvName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"upper snake", "API_TOKEN", true},
		{"one letter", "a", true},
		{"leading underscore", "_X", true},
		{"digits after the first character", "X1", true},
		{"lower snake", "lower_case", true},
		{"mixed case with digits", "Mixed_Case_9", true},
		{"empty", "", false},
		{"a space", " ", false},
		{"a space inside", "A B", false},
		{"an equals sign", "A=B", false},
		{"a leading space", " A", false},
		{"a trailing space", "A ", false},
		{"a tab", "A\tB", false},
		{"a newline", "A\nB", false},
		{"a NUL byte", "A\x00B", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidEnvName(tt.input); got != tt.want {
				t.Errorf("ValidEnvName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// A refused `run` keeps the engine's own words in its error (the caller that
// shows them needs them), but what a log line may say is a summary: the engine
// can echo a typed value on its stderr.
func TestARefusedLaunchLogsASummaryWithNoEngineOutput(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		argv := wantEnvArgv([]string{"API_TOKEN"})
		fakecli.Install(t, fakecli.Scenario{
			{Tool: shape.Binary, Args: argv, Reply: fakecli.Reply{Stderr: "Error: invalid value canary for API_TOKEN\n", Exit: 125}},
		}, shape.Binary)
		useRealRunner(t)

		_, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Env: []string{"API_TOKEN=canary"}})

		if err == nil || !strings.Contains(err.Error(), "invalid value canary") {
			t.Fatalf("LaunchContainer() error = %v, want the engine's own diagnostic kept", err)
		}
		var le *LaunchError
		if !errors.As(err, &le) {
			t.Fatalf("error %T is not a *LaunchError", err)
		}
		want := shape.Name + " run failed (exit code 125)"
		if got := LaunchLogText(err); got != want {
			t.Errorf("LaunchLogText() = %q, want %q", got, want)
		}
	})
}

func TestLaunchLogTextLeavesOtherErrorsAsTheyAre(t *testing.T) {
	for _, err := range []error{ErrInvalidLaunchEnv, errors.New("docker not found"), &LaunchError{label: "docker run"}} {
		want := err.Error()
		if _, ok := err.(*LaunchError); ok {
			want = "docker run failed (could not start the process)"
		}
		if got := LaunchLogText(err); got != want {
			t.Errorf("LaunchLogText(%v) = %q, want %q", err, got, want)
		}
	}
}
