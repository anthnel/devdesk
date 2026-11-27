package docker

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-21 (DEVDESK-819, requirement S4): the launch form can never hand the engine
// a privileged option nobody asked for, and a typed value is always ONE argv
// element that cannot be read as an engine flag.
//
// Expected production behavior, pinned below:
//
//   - LaunchContainer and BuildLaunchCmd refuse a launch (an error, nothing
//     started, nothing built) when Image, Name, Entrypoint, User or Network
//     starts with "-", or when an entry of Ports, Volumes or Mounts does. The error never
//     holds the value (it may be a secret typed in the wrong field), and neither
//     does LaunchLogText(err). Suggested API, not referenced by these tests so
//     that the package keeps compiling: `var ErrInvalidLaunchOption error`
//     (errors.Is), in the manner of ErrInvalidLaunchEnv.
//   - Ports are in that list although LoopbackPort rewrites a spec: it only
//     prefixes a spec that names no address, and `--privileged;x:/p` holds two
//     colons, so LoopbackPort reads `--privileged;x` as its address and leaves the
//     element as it is (found by the property test below).
//   - The Env values stay free: "NAME=-x" is a value, it travels in the child's
//     environment and never in argv (OC-14). A name that starts with "-" is
//     refused by ValidEnvName already.
//   - The launcher does NOT classify the network: `Network: "host"` still gives
//     `--network host` (two argv elements, the form kept by OC-21). The
//     confirmation is the form's, as OC-20's is for the mounts
//     (TestLaunchContainerDoesNotSecondGuessASensitiveMount).
//   - buildLaunchArgs emits no other flag than the ones it emits today:
//     --pull=never, --rm, -d, -i, -t, --name, --entrypoint, -p, -e, -v, --mount,
//     --network, --user. In particular never --privileged, --pid, --userns,
//     --ipc, --uts, --cap-add, --security-opt, --device.

// oc21ValueFlags take the next argv element as their value; oc21BoolFlags do not.
var (
	oc21ValueFlags = []string{"--name", "--entrypoint", "-p", "-e", "-v", "--mount", "--network", "--user"}
	oc21BoolFlags  = []string{"--pull=never", "--rm", "-d", "-i", "-t"}
)

// oc21Privileged are the flags (as prefixes: `--pid=host`, `--device=/dev/mem`...)
// that no launch of DevDesk ever builds.
var oc21Privileged = []string{
	"--privileged", "--pid", "--userns", "--ipc", "--uts", "--cap-add", "--cap-drop",
	"--security-opt", "--device", "--cgroupns", "--net=", "--sysctl", "--group-add",
}

// oc21Argv is an argv read the way an engine reads it.
type oc21Argv struct {
	bools []string            // boolean flags, sorted
	pairs map[string][]string // flag -> its values, in order
	image string
}

// oc21Parse reads argv (without the tool name). The second result is empty when
// the argv is well formed: `run`, then flags known to the launcher at flag
// positions (a value flag takes the next element, whatever it looks like), then
// the image as the last element, and no value or image that starts with "-".
func oc21Parse(argv []string) (oc21Argv, string) {
	out := oc21Argv{pairs: map[string][]string{}}
	if len(argv) < 2 || argv[0] != "run" {
		return out, "not a run: " + strings.Join(argv, " ")
	}
	out.image = argv[len(argv)-1]
	if strings.HasPrefix(out.image, "-") {
		return out, "the image reads as a flag: " + out.image
	}
	for i := 1; i < len(argv)-1; {
		e := argv[i]
		switch {
		case slices.Contains(oc21ValueFlags, e):
			if i+1 > len(argv)-2 {
				return out, "flag " + e + " has no value before the image"
			}
			v := argv[i+1]
			if strings.HasPrefix(v, "-") {
				return out, "the value of " + e + " reads as a flag: " + v
			}
			out.pairs[e] = append(out.pairs[e], v)
			i += 2
		case slices.Contains(oc21BoolFlags, e):
			out.bools = append(out.bools, e)
			i++
		default:
			return out, "an element that is neither a known flag nor the value of one, at a flag position: " + e
		}
	}
	slices.Sort(out.bools)
	return out, ""
}

// oc21Want is the argv structure the options must produce, written from the
// options and not from buildLaunchArgs.
func oc21Want(o ContainerLaunchOptions) oc21Argv {
	w := oc21Argv{pairs: map[string][]string{}, image: o.Image}
	w.bools = []string{"--pull=never"}
	for flag, on := range map[string]bool{"--rm": o.Remove, "-d": o.Detach, "-i": o.Interactive, "-t": o.TTY} {
		if on {
			w.bools = append(w.bools, flag)
		}
	}
	slices.Sort(w.bools)
	one := func(flag, v string) {
		if v != "" {
			w.pairs[flag] = append(w.pairs[flag], v)
		}
	}
	one("--name", o.Name)
	one("--entrypoint", o.Entrypoint)
	one("--network", o.Network)
	one("--user", o.User)
	for _, p := range o.Ports {
		one("-p", LoopbackPort(p))
	}
	for _, e := range o.Env {
		if name, _, _ := strings.Cut(e, "="); e != "" {
			one("-e", name)
		}
	}
	for _, v := range o.Volumes {
		one("-v", v)
	}
	for _, m := range o.Mounts {
		one("--mount", m)
	}
	return w
}

// oc21Problem returns what is wrong with an argv built for opts, or "".
func oc21Problem(argv []string, opts ContainerLaunchOptions) string {
	for _, e := range argv {
		for _, p := range oc21Privileged {
			if strings.HasPrefix(e, p) {
				return "the argv holds the element " + e + " (a privileged option nobody asked for)"
			}
		}
	}
	got, problem := oc21Parse(argv)
	if problem != "" {
		return problem
	}
	want := oc21Want(opts)
	if !slices.Equal(got.bools, want.bools) {
		return "boolean flags " + strings.Join(got.bools, " ") + ", want " + strings.Join(want.bools, " ")
	}
	for _, flag := range oc21ValueFlags {
		if !slices.Equal(got.pairs[flag], want.pairs[flag]) {
			return "values of " + flag + " = " + strings.Join(got.pairs[flag], "|") +
				", want " + strings.Join(want.pairs[flag], "|") + " (each typed value must be one element)"
		}
	}
	if got.image != want.image {
		return "image " + got.image + ", want " + want.image
	}
	return ""
}

// ── The property ─────────────────────────────────────────────────────────────

// oc21Dash and oc21Plain are hostile or boundary values: the first ones start
// with a dash, the second ones do not.
var (
	oc21Dash = []string{
		"--privileged", "-privileged", "--pid=host", "--pid", "--cap-add=SYS_ADMIN", "--cap-add",
		"--cap-add=ALL", "--userns=host", "--ipc=host", "--uts=host", "--security-opt",
		"--security-opt=seccomp=unconfined", "--security-opt=apparmor=unconfined",
		"--device=/dev/mem", "--device", "--mount", "-v", "-e", "-p", "--", "-", "-x", "-it",
		"--network=host", "--net=host", "-d", "--rm",
	}
	oc21Plain = []string{
		" ", "host", "HOST", " HOST ", "container:abc", "a b", "x y z", "'; id", "$(id)", "`id`",
		"\"quoted\"", "a,b", " --privileged", " -v /:/h", "\n--privileged", "é", "\t-x", "SYS_ADMIN", "ALL",
		"type=bind,source=/,target=/h", "=", "a=b", "/etc:/x", "./d:/d", "~/d:/d", "name:/p:ro", "bridge",
		"--privileged"[2:], "pid=host",
	}
	oc21Env = []string{"TOKEN", "A_B", "x.y", "REGION"}
)

type oc21Gen struct {
	r    *rand.Rand
	dash bool // whether a value may start with "-"
}

// value draws a field value: often empty, often benign, sometimes hostile, and
// sometimes two hostile words glued by a separator.
func (g *oc21Gen) value(benign ...string) string {
	pick := func() string {
		if g.dash && g.r.IntN(2) == 0 {
			return oc21Dash[g.r.IntN(len(oc21Dash))]
		}
		return oc21Plain[g.r.IntN(len(oc21Plain))]
	}
	switch n := g.r.IntN(10); {
	case n < 3:
		return ""
	case n < 6:
		return benign[g.r.IntN(len(benign))]
	case n < 8:
		return pick()
	default:
		seps := []string{" ", ",", "=", ":", ";", "\n"}
		return pick() + seps[g.r.IntN(len(seps))] + pick()
	}
}

func (g *oc21Gen) list(max int, benign ...string) []string {
	var out []string
	for range g.r.IntN(max + 1) {
		out = append(out, g.value(benign...))
	}
	return out
}

func (g *oc21Gen) options() ContainerLaunchOptions {
	o := ContainerLaunchOptions{
		Image:      "img",
		Name:       g.value("web", "api-1"),
		Entrypoint: g.value("/bin/sh", `sh -c "x"`),
		User:       g.value("1000:1000", "root"),
		Network:    g.value("bridge", "host", "none"),
		Ports:      g.list(3, "8080:80", "80", "127.0.0.1:8080:80/tcp"),
		Volumes:    g.list(3, "data:/d", "./d:/d", "/etc:/e:ro"),
		Mounts:     g.list(2, "type=bind,source=./d,target=/d", "type=tmpfs,target=/t"),
	}
	if g.r.IntN(5) == 0 {
		o.Image = g.value("img", "nginx:1.25")
	}
	for range g.r.IntN(4) {
		if g.r.IntN(3) == 0 {
			o.Env = append(o.Env, g.value("A=1"))
		} else {
			o.Env = append(o.Env, oc21Env[g.r.IntN(len(oc21Env))]+"="+g.value("v", "-x", "--privileged"))
		}
	}
	o.Remove, o.Detach = g.r.IntN(2) == 0, g.r.IntN(2) == 0
	o.Interactive = g.r.IntN(2) == 0
	o.TTY = o.Interactive
	return o
}

// For every combination of fields, drawn from a fixed seed, a launch is either
// refused with nothing started, or its argv holds no privileged option, only
// the flags the launcher knows at flag positions, and every typed value as one
// element that does not read as a flag. Half the draws may start a value with a
// dash (they must be refused), the other half may not (they must go through).
func TestLaunchArgvOfAnyCombinationOfFieldsIsSafe(t *testing.T) {
	const iterations = 700
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		s := stub(t, &stubRunner{})
		gen := &oc21Gen{r: rand.New(rand.NewPCG(21, 819))}
		accepted, refused := 0, 0
		for i := range iterations {
			gen.dash = i%2 == 0
			opts := gen.options()
			before := len(s.calls)

			cmd, buildErr := BuildLaunchCmd(opts)
			_, runErr := LaunchContainer(opts)

			if (buildErr == nil) != (runErr == nil) {
				t.Fatalf("BuildLaunchCmd err = %v but LaunchContainer err = %v for %#v: the two entry points disagree", buildErr, runErr, opts)
			}
			if buildErr != nil {
				refused++
				if len(s.calls) != before {
					t.Fatalf("a refused launch started or built a command: %v for %#v", s.calls[before:], opts)
				}
				for _, err := range []error{buildErr, runErr} {
					for _, text := range []string{err.Error(), LaunchLogText(err)} {
						for _, v := range []string{opts.Name, opts.Entrypoint, opts.User, opts.Network, opts.Image} {
							if len(v) >= 8 && strings.HasPrefix(v, "-") && strings.Contains(text, v) {
								t.Fatalf("the error %q repeats the typed value %q", text, v)
							}
						}
					}
				}
				continue
			}
			accepted++
			if len(s.calls) != before+2 {
				t.Fatalf("an accepted launch made %d calls, want a build and a run", len(s.calls)-before)
			}
			for _, argv := range [][]string{cmd.Args[1:], s.calls[before+1].Args} {
				if problem := oc21Problem(argv, opts); problem != "" {
					t.Fatalf("%s\nargv %q\nfrom %#v", problem, argv, opts)
				}
			}
		}
		// The property is not vacuous: hostile values were refused, and plenty of
		// odd but legitimate ones went through.
		if refused < 100 || accepted < 100 {
			t.Errorf("%d launches refused and %d accepted out of %d draws, want at least 100 of each", refused, accepted, iterations)
		}
	})
}

// ── A value that starts with "-" is refused ──────────────────────────────────

func TestALaunchWithAValueThatReadsAsAFlagIsRefusedAndStartsNothing(t *testing.T) {
	const canary = "--canary-flag"
	tests := []struct {
		name string
		set  func(o *ContainerLaunchOptions)
	}{
		{"image", func(o *ContainerLaunchOptions) { o.Image = canary }},
		{"image, one dash", func(o *ContainerLaunchOptions) { o.Image = "-" + canary[2:] }},
		{"name", func(o *ContainerLaunchOptions) { o.Name = canary }},
		{"entrypoint", func(o *ContainerLaunchOptions) { o.Entrypoint = canary }},
		{"user", func(o *ContainerLaunchOptions) { o.User = canary }},
		{"network", func(o *ContainerLaunchOptions) { o.Network = canary }},
		{"network, an engine flag", func(o *ContainerLaunchOptions) { o.Network = "--privileged" }},
		{"a port", func(o *ContainerLaunchOptions) { o.Ports = []string{canary + ";x:80"} }},
		{"a port, a negative number", func(o *ContainerLaunchOptions) { o.Ports = []string{"-8080:80"} }},
		{"the second port", func(o *ContainerLaunchOptions) { o.Ports = []string{"8080:80", canary + ";x:80"} }},
		{"a volume", func(o *ContainerLaunchOptions) { o.Volumes = []string{canary} }},
		{"a volume named with a dash", func(o *ContainerLaunchOptions) { o.Volumes = []string{"-data:/d"} }},
		{"the second volume", func(o *ContainerLaunchOptions) { o.Volumes = []string{"data:/d", canary} }},
		{"a mount", func(o *ContainerLaunchOptions) { o.Mounts = []string{canary} }},
		{"the second mount", func(o *ContainerLaunchOptions) {
			o.Mounts = []string{"type=tmpfs,target=/t", canary}
		}},
	}
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stubStrict(t) // any argv, built or run, fails the test
				opts := ContainerLaunchOptions{Image: "img"}
				tt.set(&opts)

				cmd, buildErr := BuildLaunchCmd(opts)
				id, runErr := LaunchContainer(opts)

				if buildErr == nil || cmd != nil {
					t.Errorf("BuildLaunchCmd() = %v, %v, want a refusal and no command", cmd, buildErr)
				}
				if runErr == nil || id != "" {
					t.Errorf("LaunchContainer() = %q, %v, want a refusal", id, runErr)
				}
				for _, err := range []error{buildErr, runErr} {
					if err == nil {
						continue
					}
					for _, text := range []string{err.Error(), LaunchLogText(err)} {
						if strings.Contains(text, canary) || strings.Contains(text, "privileged") {
							t.Errorf("the error %q repeats what was typed", text)
						}
					}
				}
			})
		}
	})
}

// What stays valid, with the argv it gives: a dash that is not the first
// character, a value that holds "-x" inside a longer one, a path that starts
// with `/`, `.`, `~` or a name, an environment value that starts with "-".
func TestAValueThatDoesNotStartWithADashStillLaunches(t *testing.T) {
	tests := []struct {
		name string
		opts ContainerLaunchOptions
		want []string // between "--pull=never" and the image
	}{
		{"a name with dashes", ContainerLaunchOptions{Name: "my-web-1"}, []string{"--name", "my-web-1"}},
		{"an entrypoint with a dash inside", ContainerLaunchOptions{Entrypoint: "sh -c x"}, []string{"--entrypoint", "sh -c x"}},
		{"a user", ContainerLaunchOptions{User: "1000:1000"}, []string{"--user", "1000:1000"}},
		{"a network", ContainerLaunchOptions{Network: "my-net"}, []string{"--network", "my-net"}},
		{"volume ./", ContainerLaunchOptions{Volumes: []string{"./data:/d"}}, []string{"-v", "./data:/d"}},
		{"volume ../", ContainerLaunchOptions{Volumes: []string{"../data:/d"}}, []string{"-v", "../data:/d"}},
		{"volume .", ContainerLaunchOptions{Volumes: []string{".:/d"}}, []string{"-v", ".:/d"}},
		{"volume ~", ContainerLaunchOptions{Volumes: []string{"~/p:/p"}}, []string{"-v", "~/p:/p"}},
		{"volume /", ContainerLaunchOptions{Volumes: []string{"/srv/p:/p:ro"}}, []string{"-v", "/srv/p:/p:ro"}},
		{"a named volume with a dash inside", ContainerLaunchOptions{Volumes: []string{"pg-data:/d"}}, []string{"-v", "pg-data:/d"}},
		{"a mount whose source starts with a dash", ContainerLaunchOptions{Mounts: []string{"type=bind,source=-x,target=/t"}},
			[]string{"--mount", "type=bind,source=-x,target=/t"}},
		{"a port", ContainerLaunchOptions{Ports: []string{"8080:80"}}, []string{"-p", "127.0.0.1:8080:80"}},
		{"an environment value that starts with a dash", ContainerLaunchOptions{Env: []string{"EXTRA=-x"}}, []string{"-e", "EXTRA"}},
		{"an environment value that is a flag", ContainerLaunchOptions{Env: []string{"EXTRA=--privileged"}}, []string{"-e", "EXTRA"}},
	}
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stub(t, &stubRunner{})
				tt.opts.Image = "img"
				want := append(append([]string{"run", "--pull=never"}, tt.want...), "img")

				cmd, err := BuildLaunchCmd(tt.opts)

				if err != nil {
					t.Fatalf("BuildLaunchCmd() error = %v, want the launch to go through", err)
				}
				if !slices.Equal(cmd.Args[1:], want) {
					t.Errorf("argv = %q, want %q", cmd.Args[1:], want)
				}
			})
		}
	})
}

// ── The entrypoint is one element ────────────────────────────────────────────

// Characterization, green today, and what the fix must keep: `--entrypoint` takes
// ONE element, whatever words the field holds, and no word of it ever becomes an
// engine flag (or a command argument after the image: the form has no command
// field). Note for the user: docker and podman read that element as the path of
// ONE executable, so `sh -c "x"` is stored safely and then fails inside the
// engine (`executable file not found`); it is not a way to give arguments.
func TestAnEntrypointWithSeveralWordsIsOneArgvElement(t *testing.T) {
	entrypoints := []string{
		`sh -c "x"`,
		`/bin/sh -c 'echo --privileged'`,
		`bash -lc "id"`,
		`env --pid=host sh`,
		`sh -c x; --cap-add=SYS_ADMIN`,
		`/bin/sh --privileged`,
		`a   b`,
	}
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, ep := range entrypoints {
			t.Run(ep, func(t *testing.T) {
				stub(t, &stubRunner{})
				want := []string{"run", "--pull=never", "--entrypoint", ep, "img"}

				cmd, err := BuildLaunchCmd(ContainerLaunchOptions{Image: "img", Entrypoint: ep})

				if err != nil {
					t.Fatalf("BuildLaunchCmd() error = %v", err)
				}
				if !slices.Equal(cmd.Args[1:], want) {
					t.Errorf("argv = %q, want exactly %q: one element after --entrypoint, none after the image", cmd.Args[1:], want)
				}
				for _, word := range strings.Fields(ep) {
					if strings.HasPrefix(word, "-") && slices.Contains(cmd.Args[1:], word) {
						t.Errorf("the word %q of the entrypoint is an argv element of its own", word)
					}
				}
			})
		}
	})
}

// ── Not second-guessing the network, and what is not handled here ────────────

// The launcher launches the network it is given, in the form OC-21 keeps:
// `--network host` as two elements (the confirmation is the form's), and a
// `container:<id>` network namespace as it is today: no confirmation, and no
// --pid, --ipc or --uts to go with it (follow-up: joining another container's
// network is not classified by OC-21).
func TestLaunchContainerDoesNotSecondGuessTheNetwork(t *testing.T) {
	tests := []struct{ name, network string }{
		{"host", "host"},
		{"host, any case, spaces", " HOST "},
		{"another container's namespace", "container:0123456789ab"},
		{"none", "none"},
	}
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				argv := []string{"run", "--pull=never", "--network", tt.network, "img"}
				stubStrict(t, reply{argv: argv, out: "9f2e1c8b7a6d\n"})

				id, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Network: tt.network})

				if err != nil || id != "9f2e1c8b7a6d" {
					t.Fatalf("LaunchContainer() = %q, %v, want the container ID and no error", id, err)
				}
				for _, e := range argv {
					for _, p := range []string{"--pid", "--ipc", "--uts", "--userns", "--privileged"} {
						if strings.HasPrefix(e, p) {
							t.Errorf("argv holds %q: a network never brings a namespace option with it", e)
						}
					}
				}
			})
		}
	})
}
