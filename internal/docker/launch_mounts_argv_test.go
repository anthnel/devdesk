package docker

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-20 (DEVDESK-818), the argv half: the --mount form reaches the engine as a
// single `--mount VALUE` pair, right after the -v pairs, on both engines and in
// both ways of launching. The value is passed as typed (it holds commas and
// equals signs, and no secret: the form never types one there).
//
// Expected production behavior, pinned below:
//
//   - ContainerLaunchOptions gains `Mounts []string`, "the value of each --mount".
//   - buildLaunchArgs emits, in this order: ... -e NAME ..., -v V ..., --mount M ...,
//     --network ..., --user ..., image. A blank entry is skipped, as a blank -v is.
//   - LaunchContainer and BuildLaunchCmd do NOT ask for a confirmation and do not
//     refuse a sensitive mount: the confirmation is the form's (the classification
//     is SensitiveMounts, a reader). A sensitive mount that reaches them is
//     launched as given.

// Both forms in one argv, in a fixed order, on both engines and both launch paths.
func TestLaunchPassesTheMountFormAsOneWordPair(t *testing.T) {
	tests := []struct {
		name string
		opts ContainerLaunchOptions
		argv []string
	}{
		{"one mount", ContainerLaunchOptions{Image: "img", Mounts: []string{"type=bind,source=/data,target=/d,readonly"}},
			[]string{"run", "--pull=never", "--mount", "type=bind,source=/data,target=/d,readonly", "img"}},
		{"volumes then mounts", ContainerLaunchOptions{
			Image: "img", Volumes: []string{"data:/var/lib", "./src:/src"},
			Mounts: []string{"type=bind,src=./a,dst=/a", "type=tmpfs,target=/tmp"},
		}, []string{
			"run", "--pull=never", "-v", "data:/var/lib", "-v", "./src:/src",
			"--mount", "type=bind,src=./a,dst=/a", "--mount", "type=tmpfs,target=/tmp", "img",
		}},
		{"blank mounts are skipped", ContainerLaunchOptions{Image: "img", Mounts: []string{"", "type=volume,source=v,target=/v", ""}},
			[]string{"run", "--pull=never", "--mount", "type=volume,source=v,target=/v", "img"}},
		{"mounts sit before the network and the user", ContainerLaunchOptions{
			Image: "img", Network: "mynet", User: "1000:1000", Volumes: []string{"v:/v"}, Mounts: []string{"type=tmpfs,target=/t"},
		}, []string{
			"run", "--pull=never", "-v", "v:/v", "--mount", "type=tmpfs,target=/t", "--network", "mynet", "--user", "1000:1000", "img",
		}},
		{"a value with an equals sign and commas is one word", ContainerLaunchOptions{
			Image: "img", Mounts: []string{"type=bind,source=/a=b,target=/c"},
		}, []string{"run", "--pull=never", "--mount", "type=bind,source=/a=b,target=/c", "img"}},
	}
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := buildLaunchArgs(tt.opts); !slices.Equal(got, tt.argv) {
					t.Fatalf("buildLaunchArgs() = %q, want %q", got, tt.argv)
				}
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

// A sensitive mount that reaches the launcher is launched as given: the check is
// the form's. The real runner and fakecli prove the argv the engine receives.
func TestLaunchContainerDoesNotSecondGuessASensitiveMount(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		argv := []string{
			"run", "--pull=never",
			"-v", "/var/run/docker.sock:/var/run/docker.sock",
			"--mount", "type=bind,source=/etc,target=/host-etc,readonly",
			"img",
		}
		fake := fakecli.Install(t, fakecli.Scenario{
			{Tool: shape.Binary, Args: argv, Reply: fakecli.Reply{Stdout: "9f2e1c8b7a6d\n"}},
		}, shape.Binary)
		useRealRunner(t)

		id, err := LaunchContainer(ContainerLaunchOptions{
			Image:   "img",
			Volumes: []string{"/var/run/docker.sock:/var/run/docker.sock"},
			Mounts:  []string{"type=bind,source=/etc,target=/host-etc,readonly"},
		})

		if err != nil || id != "9f2e1c8b7a6d" {
			t.Fatalf("LaunchContainer() = %q, %v, want the container ID and no error", id, err)
		}
		calls := fake.Invocations()
		if len(calls) != 1 || !calls[0].Matched || !slices.Equal(calls[0].Args, argv) {
			t.Errorf("the engine saw %v, want exactly one run of %q", calls, argv)
		}
	})
}
