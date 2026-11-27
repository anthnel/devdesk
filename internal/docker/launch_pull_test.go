package docker

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-10 (DEVDESK-808), criterion 3: a launch never pulls an image on its own.
// An image reaches this machine through imagepull.Pull, which checks its
// signature, or not at all. The static half of the story (G8) is
// internal/engineguard/pull_guard_test.go; this is the run-time half, on the argv
// each engine receives.
//
// The strict stub fails on any argv a test did not declare, so a `pull` issued
// next to the `run` would fail the test as well as a `run` missing the flag.

// launchCases are the argv a launch builds, flag by flag. The flag sits right
// after the verb, before anything the user typed in the form.
var launchCases = []struct {
	name string
	opts ContainerLaunchOptions
	argv []string
}{
	{"image only", ContainerLaunchOptions{Image: "img"}, []string{"run", "--pull=never", "img"}},
	{"tagged and named", ContainerLaunchOptions{Image: "nginx:1.25", Name: "web"},
		[]string{"run", "--pull=never", "--name", "web", "nginx:1.25"}},
	{"detached and removed", ContainerLaunchOptions{Image: "img", Detach: true, Remove: true},
		[]string{"run", "--pull=never", "--rm", "-d", "img"}},
	{"everything the form can set", ContainerLaunchOptions{
		Name: "web", Image: "img", Entrypoint: "/bin/sh", Ports: []string{"8080:80"}, Env: []string{"K=V"},
		Volumes: []string{"data:/var/lib"}, Network: "mynet", User: "1000:1000", Detach: true, Remove: true,
		Interactive: true, TTY: true,
	}, []string{
		"run", "--pull=never", "--rm", "-d", "-i", "-t", "--name", "web", "--entrypoint", "/bin/sh",
		"-p", "127.0.0.1:8080:80", "-e", "K", "-v", "data:/var/lib", "--network", "mynet", "--user", "1000:1000", "img",
	}},
}

// Docker and Podman receive the same run argv, `--pull=never` included, from the
// background launch and from the interactive one.
func TestLaunchNeverPullsOnEveryEngine(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range launchCases {
			t.Run(tt.name, func(t *testing.T) {
				stubStrict(t, reply{argv: tt.argv, out: "9f2e1c8b7a6d\n"})

				if _, err := LaunchContainer(tt.opts); err != nil {
					t.Fatalf("LaunchContainer() error = %v", err)
				}
				cmd, err := BuildLaunchCmd(tt.opts)
				if err != nil {
					t.Fatalf("BuildLaunchCmd() error = %v", err)
				}
				if got := cmd.Args[1:]; !slices.Equal(got, tt.argv) {
					t.Errorf("BuildLaunchCmd() argv = %q, want %q", got, tt.argv)
				}
			})
		}
	})
}

// The entrypoint probe starts a throwaway container from the image: it is a run
// too, and it must not pull one either.
func TestEntrypointProbeNeverPullsOnEveryEngine(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		argv := []string{"run", "--pull=never", "--rm", "--entrypoint", "/bin/sh", "img", "-c", "command -v bash"}
		stubStrict(t, reply{argv: argv, out: "/bin/bash\n"})

		ok, err := VerifyEntrypoint("img", "bash")
		if err != nil || !ok {
			t.Fatalf("VerifyEntrypoint() = %v, %v, want true, nil", ok, err)
		}
	})
}

// An image the engine does not have makes `run --pull=never` exit 125 on both
// engines, with a message that says the image is absent (`No such image` or
// `image not known`, docs/backlog.md). The launch reports that and goes no
// further: the only argv the stub accepts is the run, so a fallback `pull` would
// fail the test.
func TestLaunchOfAMissingImageIsRefusedAndNothingIsPulled(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		const refusal = "Error: No such image: ghost:1\n"
		s := stubStrict(t, reply{argv: []string{"run", "--pull=never", "ghost:1"}, out: refusal, err: errExit})

		id, err := LaunchContainer(ContainerLaunchOptions{Image: "ghost:1"})

		if err == nil {
			t.Fatalf("LaunchContainer() = %q, nil error, want the engine's refusal", id)
		}
		if id != "" {
			t.Errorf("LaunchContainer() id = %q, want none", id)
		}
		if !strings.Contains(err.Error(), "No such image") {
			t.Errorf("error = %q, want the engine's message", err)
		}
		if len(s.calls) != 1 {
			t.Errorf("the engine was called %d times, want exactly the one run: %v", len(s.calls), s.calls)
		}
	})
}

// A probe on an image that is not there answers "no binary" without a pull.
func TestEntrypointProbeOfAMissingImageIsNotAPull(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		argv := []string{"run", "--pull=never", "--rm", "--entrypoint", "/bin/sh", "ghost:1", "-c", "command -v bash"}
		stubStrict(t, reply{argv: argv, err: errors.New("exit status 125")})

		ok, err := VerifyEntrypoint("ghost:1", "bash")
		if err != nil || ok {
			t.Errorf("VerifyEntrypoint() = %v, %v, want false, nil", ok, err)
		}
	})
}

// Whatever the form sets, the flag is the first word after the verb and no other
// word asks for a pull: a value the user typed cannot slide in ahead of it, and
// no option adds `--pull=always`.
func TestLaunchArgsStartWithNoPullFlagAndAskForNoOtherPull(t *testing.T) {
	for _, tt := range launchCases {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLaunchArgs(tt.opts)
			if len(got) < 2 || got[0] != "run" || got[1] != "--pull=never" {
				t.Fatalf("buildLaunchArgs() = %q, want it to start with run --pull=never", got)
			}
			for i, w := range got {
				if i != 1 && (strings.HasPrefix(w, "--pull") || w == "pull") {
					t.Errorf("buildLaunchArgs() = %q, word %d %q asks for a pull", got, i, w)
				}
			}
		})
	}
}
