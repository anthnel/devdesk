package scan

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// DEVDESK-945 (S9): the forge token of plumber's container travels as `-e NAME`
// in argv and as `NAME=value` in the environment of the child, never as
// `-e NAME=value`. argv is readable by any local user through `ps` and
// /proc/<pid>/cmdline, and ends up in audit logs. Same rule as the proxy
// variables (DEVDESK-583); the exact argv of the no-token launches is frozen by
// container_builders_test.go.

// canaryToken is a value no other test or default uses, so a hit anywhere is
// this token.
const canaryToken = "canary-tok-8f3a91c2d7e45b60"

var tokenVariables = []struct {
	provider string
	name     string
}{
	{providerGitHub, "GH_TOKEN"},
	{providerGitLab, "GITLAB_TOKEN"},
}

// hasPair reports whether args holds first immediately followed by second.
func hasPair(args []string, first, second string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			return true
		}
	}
	return false
}

// The argv names the variable and never carries its value, on both engines and
// for both forges; the value is in the child's environment.
func TestThePlumberTokenIsInTheEnvironmentAndNotInArgv(t *testing.T) {
	noProxyEnvironment(t)
	forEachEngine(t, func(t *testing.T, binary string) {
		for _, tc := range tokenVariables {
			t.Run(tc.provider, func(t *testing.T) {
				opts := PlumberOptions{Provider: tc.provider, Token: canaryToken, ConfigPath: "/home/u/plumber.yaml"}
				cmd := plumberArgs("/repo", ToolSpec{Source: ToolSourceContainer}, opts, "/dev/stdout")

				if cmd.Name != binary {
					t.Errorf("Name = %q, want %q", cmd.Name, binary)
				}
				for _, arg := range cmd.Args {
					if strings.Contains(arg, canaryToken) {
						t.Errorf("the token is in argv: %q", cmd.Args)
					}
					if strings.HasPrefix(arg, tc.name+"=") {
						t.Errorf("argv carries %q, which is NAME=value", arg)
					}
				}
				if !hasPair(cmd.Args, "-e", tc.name) {
					t.Errorf("argv has no `-e %s`: %q", tc.name, cmd.Args)
				}
				// Before the image, like every -e: after it is plumber's own argv.
				if at, image := slices.Index(cmd.Args, tc.name), slices.Index(cmd.Args, DefaultPlumberImage); at < 0 || at > image {
					t.Errorf("-e %s is not before the image: %q", tc.name, cmd.Args)
				}

				want := tc.name + "=" + canaryToken
				if n := countOf(cmd.Env, want); n != 1 {
					t.Errorf("the child's environment holds %q %d times, want once", want, n)
				}
				// The other forge's variable is not declared anywhere.
				for _, other := range tokenVariables {
					if other.name == tc.name {
						continue
					}
					if slices.Contains(cmd.Args, other.name) {
						t.Errorf("argv also names %s: %q", other.name, cmd.Args)
					}
					for _, kv := range cmd.Env {
						if strings.HasPrefix(kv, other.name+"=") && !slices.Contains(os.Environ(), kv) {
							t.Errorf("the environment also sets %s", other.name)
						}
					}
				}
			})
		}
	})
}

// What is shown and what is logged never holds the token, nor `NAME=value`.
func TestThePlumberTokenIsNotInWhatIsShownOrLogged(t *testing.T) {
	noProxyEnvironment(t)
	forEachEngine(t, func(t *testing.T, _ string) {
		for _, tc := range tokenVariables {
			t.Run(tc.provider, func(t *testing.T) {
				opts := PlumberOptions{Provider: tc.provider, Token: canaryToken}
				cmd := plumberArgs("/repo", ToolSpec{Source: ToolSourceContainer}, opts, "/dev/stdout")

				for what, s := range map[string]string{
					"String":            cmd.String(),
					"Loggable":          cmd.Loggable(),
					"GetPlumberCommand": GetPlumberCommand("/repo", ToolSpec{Source: ToolSourceContainer}, opts),
					"plumberCommand":    plumberCommand("/repo", ToolSpec{Source: ToolSourceContainer}, opts).Loggable(),
					"plumberVerbatim":   strings.Join(plumberVerbatim, " "),
					"Verbatim":          strings.Join(cmd.Verbatim, " "),
				} {
					if strings.Contains(s, canaryToken) || strings.Contains(s, tc.name+"=") {
						t.Errorf("%s holds the token or %s=: %s", what, tc.name, s)
					}
				}
			})
		}
	})
}

// The same from the run itself, as the process sees it: RunPlumber starts the
// fake engine, whose journal records the argv and the environment it received.
// No real engine is involved.
func TestThePlumberTokenReachesTheChildOnlyThroughItsEnvironment(t *testing.T) {
	noProxyEnvironment(t)
	forEachEngine(t, func(t *testing.T, binary string) {
		for _, tc := range tokenVariables {
			t.Run(tc.provider, func(t *testing.T) {
				fake := fakecli.Install(t, fakecli.Scenario{
					{Tool: binary, Prefix: true, Reply: fakecli.Reply{Stdout: "{}"}},
				}, binary)

				// The report is not valid: only what the engine received matters here.
				_, _ = RunPlumber(context.Background(), "/repo", ToolSpec{Source: ToolSourceContainer},
					PlumberOptions{Provider: tc.provider, Token: canaryToken}, nil)

				calls := fake.Invocations()
				if len(calls) != 1 {
					t.Fatalf("the engine was started %d times, want once: %v", len(calls), calls)
				}
				got := calls[0]
				for _, arg := range got.Args {
					if strings.Contains(arg, canaryToken) {
						t.Errorf("the token reached the engine's argv: %q", got.Args)
					}
				}
				if !hasPair(got.Args, "-e", tc.name) {
					t.Errorf("the engine's argv has no `-e %s`: %q", tc.name, got.Args)
				}
				if !slices.Contains(got.Env, tc.name+"="+canaryToken) {
					t.Errorf("the engine's environment does not hold %s", tc.name)
				}
			})
		}
	})
}

// A native plumber has no container and so no -e at all: the variable is in its
// environment only, and its argv holds nothing of the token.
func TestANativePlumberGetsTheTokenOnlyThroughItsEnvironment(t *testing.T) {
	r := answering(t, "{}", nil)

	_, _ = RunPlumber(context.Background(), "/repo", ToolSpec{Source: ToolSourceBinary},
		PlumberOptions{Provider: providerGitLab, Token: canaryToken}, nil)

	if len(r.calls) != 1 {
		t.Fatalf("plumber was started %d times, want once", len(r.calls))
	}
	cmd := r.calls[0]
	for _, arg := range cmd.Args {
		if strings.Contains(arg, canaryToken) {
			t.Errorf("the token is in argv: %q", cmd.Args)
		}
	}
	if !slices.Contains(cmd.Env, "GITLAB_TOKEN="+canaryToken) {
		t.Error("the token did not reach the environment of the binary")
	}
}

func countOf(env []string, entry string) int {
	n := 0
	for _, kv := range env {
		if kv == entry {
			n++
		}
	}
	return n
}
