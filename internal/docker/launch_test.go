package docker

import (
	"slices"
	"strings"
	"testing"
)

// The image must be the last argument: anything after it is passed to the
// container as its command rather than being read as a docker flag.
func TestBuildLaunchArgsPutsImageLast(t *testing.T) {
	args := buildLaunchArgs(ContainerLaunchOptions{
		Image:   "nginx:1.25",
		Name:    "web",
		Ports:   []string{"8080:80"},
		Network: "bridge",
		User:    "1000:1000",
	})

	if args[0] != "run" {
		t.Errorf("args[0] = %q, want \"run\"", args[0])
	}
	if got := args[len(args)-1]; got != "nginx:1.25" {
		t.Errorf("last arg = %q, want the image", got)
	}
}

func TestBuildLaunchArgsFlags(t *testing.T) {
	tests := []struct {
		name string
		opts ContainerLaunchOptions
		want []string
	}{
		{"detached", ContainerLaunchOptions{Image: "img", Detach: true}, []string{"-d"}},
		{"removed on exit", ContainerLaunchOptions{Image: "img", Remove: true}, []string{"--rm"}},
		{"interactive tty", ContainerLaunchOptions{Image: "img", Interactive: true, TTY: true}, []string{"-i", "-t"}},
		{"named", ContainerLaunchOptions{Image: "img", Name: "web"}, []string{"--name", "web"}},
		{"entrypoint override", ContainerLaunchOptions{Image: "img", Entrypoint: "/bin/sh"}, []string{"--entrypoint", "/bin/sh"}},
		{"network", ContainerLaunchOptions{Image: "img", Network: "mynet"}, []string{"--network", "mynet"}},
		{"user", ContainerLaunchOptions{Image: "img", User: "1000:1000"}, []string{"--user", "1000:1000"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildLaunchArgs(tt.opts)

			joined := strings.Join(got, " ")
			if !strings.Contains(joined, strings.Join(tt.want, " ")) {
				t.Errorf("buildLaunchArgs() = %v, want it to contain %v", got, tt.want)
			}
		})
	}
}

// Empty entries come from unfilled form fields; emitting "-p ”" would make
// docker reject the whole run.
func TestBuildLaunchArgsSkipsEmptyRepeatedValues(t *testing.T) {
	got := buildLaunchArgs(ContainerLaunchOptions{
		Image:   "img",
		Ports:   []string{"8080:80", "", "9090:90"},
		Env:     []string{"", "KEY=VALUE"},
		Volumes: []string{"data:/var/lib", ""},
	})

	if slices.Contains(got, "") {
		t.Errorf("buildLaunchArgs() = %v, contains an empty argument", got)
	}
	for _, want := range []string{"8080:80", "9090:90", "KEY=VALUE", "data:/var/lib"} {
		if !slices.Contains(got, want) {
			t.Errorf("buildLaunchArgs() = %v, missing %q", got, want)
		}
	}
	if n := slices.Index(got, "-p"); n < 0 {
		t.Error("buildLaunchArgs() dropped the port flags entirely")
	}
}

func TestBuildLaunchArgsMinimalOptions(t *testing.T) {
	got := buildLaunchArgs(ContainerLaunchOptions{Image: "img"})

	if !slices.Equal(got, []string{"run", "img"}) {
		t.Errorf("buildLaunchArgs() = %v, want [run img]", got)
	}
}

func TestLaunchContainerReturnsTrimmedID(t *testing.T) {
	stubOutput(t, "run", "9f2e1c8b7a6d\n")

	got, err := LaunchContainer(ContainerLaunchOptions{Image: "nginx"})

	if err != nil {
		t.Fatalf("LaunchContainer() error = %v", err)
	}
	if got != "9f2e1c8b7a6d" {
		t.Errorf("LaunchContainer() = %q, want the ID without the trailing newline", got)
	}
}

func TestLaunchContainerReportsMissingDocker(t *testing.T) {
	stub(t, &stubRunner{missing: true})

	if _, err := LaunchContainer(ContainerLaunchOptions{Image: "nginx"}); err == nil {
		t.Error("LaunchContainer() returned nil error when docker is absent")
	}
}

// VerifyEntrypoint reports a missing binary through its bool, not an error:
// a non-zero exit from `command -v` is the expected negative answer.
func TestVerifyEntrypoint(t *testing.T) {
	t.Run("binary present", func(t *testing.T) {
		s := stubOutput(t, "run", "/bin/bash\n")

		ok, err := VerifyEntrypoint("nginx", "bash")

		if err != nil {
			t.Fatalf("VerifyEntrypoint() error = %v", err)
		}
		if !ok {
			t.Error("VerifyEntrypoint() = false, want true")
		}
		if !slices.Contains(s.lastArgs(), "--rm") {
			t.Errorf("args = %v, missing --rm: the probe container would be left behind", s.lastArgs())
		}
	})

	t.Run("binary absent", func(t *testing.T) {
		stub(t, &stubRunner{err: map[string]error{"run": errExit}})

		ok, err := VerifyEntrypoint("nginx", "zsh")

		if err != nil {
			t.Fatalf("VerifyEntrypoint() error = %v, want a false result rather than an error", err)
		}
		if ok {
			t.Error("VerifyEntrypoint() = true, want false")
		}
	})
}
