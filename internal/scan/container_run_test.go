package scan

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/netproxy"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// DEVDESK-583, step 3: every `<engine> run --rm` goes through containerRun.cmd,
// which adds `-e NAME` for each proxy variable to forward, right before the
// image, and puts the values in the child's environment, never in argv. The
// argv produced with no proxy is frozen by container_builders_test.go
// (TestContainerBuildersWithoutProxyAreUnchanged); this file does not repeat it.

const (
	fwdHTTPValue  = "http://alice-user:s3cr3tpw@proxy.example.com:3128"
	fwdHTTPSValue = "http://bob-user:hunter2pw@proxy.example.com:3128"
	fwdNoValue    = "internal.example.net,.corp.example.net"
)

// fwdLeaks is everything that must never appear in an argv or in a displayed
// command: the raw values and the pieces a credential or a host is made of.
// The port is matched as it stands in a proxy URL, ":3128": the bare digits
// also occur in the random name t.TempDir gives a directory, which reaches an
// argv or an environment through a mount or HOME (DEVDESK-784).
var fwdLeaks = []string{
	fwdHTTPValue, fwdHTTPSValue, fwdNoValue,
	"s3cr3tpw", "hunter2pw", "alice-user", "bob-user",
	"proxy.example.com", ":3128", "internal.example.net", "corp.example.net",
}

// fullProxy is the three pairs, each given in its upper case name only.
var fullProxy = map[string]string{
	"HTTP_PROXY":  fwdHTTPValue,
	"HTTPS_PROXY": fwdHTTPSValue,
	"NO_PROXY":    fwdNoValue,
}

// forwardOf builds a Forward from the variables a process would have.
func forwardOf(vars map[string]string) netproxy.Forward {
	return netproxy.ForwardFromLookup(func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	})
}

// useProxy swaps the seam for one test and restores it afterwards.
func useProxy(t *testing.T, fwd netproxy.Forward) {
	t.Helper()
	previous := proxyForward
	proxyForward = func() netproxy.Forward { return fwd }
	t.Cleanup(func() { proxyForward = previous })
}

// forEachEngine runs fn once per engine the scanners can use.
func forEachEngine(t *testing.T, fn func(t *testing.T, binary string)) {
	t.Helper()
	for _, name := range []string{engine.Docker, engine.Podman} {
		t.Run(name, func(t *testing.T) {
			useEngine(t, engine.ShapeFor(name))
			fn(t, engine.Current().Binary)
		})
	}
}

// pairsOf lists "-e NAME" for each name.
func pairsOf(names []string) []string {
	var out []string
	for _, n := range names {
		out = append(out, "-e", n)
	}
	return out
}

// containerImages are the images the cases of containerBuilders run.
var containerImages = []string{
	"aquasec/trivy", "zricethezav/gitleaks", DefaultCosignImage,
	"registry.example.com/tools/img:1", "getplumber/plumber",
	"ghcr.io/yannh/kubeconform", "alpine/helm", "registry.k8s.io/kustomize/kustomize:v5.8.1",
}

// withNamesBeforeImage returns args with the -e pairs of names inserted right
// before the image, which is where containerRun.cmd puts them.
func withNamesBeforeImage(t *testing.T, args []string, names []string) []string {
	t.Helper()
	at := slices.IndexFunc(args, func(a string) bool { return slices.Contains(containerImages, a) })
	if at < 0 {
		t.Fatalf("no known image in %q", args)
	}
	return slices.Concat(args[:at], pairsOf(names), args[at:])
}

// checkNoLeak fails when s contains any proxy value or piece of one.
func checkNoLeak(t *testing.T, what, s string) {
	t.Helper()
	checkNoneOf(t, what, s, fwdLeaks)
}

// checkNoneOf fails when s contains any of leaks.
func checkNoneOf(t *testing.T, what, s string, leaks []string) {
	t.Helper()
	for _, leak := range leaks {
		if strings.Contains(s, leak) {
			t.Errorf("%s contains %q: %s", what, leak, s)
		}
	}
}

// checkProxyEnv fails unless env holds each variable of want exactly once with
// its value, and none of the other proxy variables.
func checkProxyEnv(t *testing.T, env []string, want map[string]string) {
	t.Helper()
	for _, name := range netproxy.ForwardedVariables {
		var found []string
		for _, kv := range env {
			if strings.HasPrefix(kv, name+"=") {
				found = append(found, kv)
			}
		}
		value, expected := want[name]
		switch {
		case !expected && len(found) > 0:
			t.Errorf("Env carries %q although %s is not forwarded", found, name)
		case expected && !slices.Equal(found, []string{name + "=" + value}):
			t.Errorf("Env entries for %s = %q, want exactly %q", name, found, name+"="+value)
		}
	}
}

// bothCasings gives each upper case name of vars its lower case twin.
func bothCasings(vars map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range vars {
		out[name] = value
		out[strings.ToLower(name)] = value
	}
	return out
}

func TestContainerRunWithoutProxyAddsNothing(t *testing.T) {
	useProxy(t, netproxy.Forward{})
	forEachEngine(t, func(t *testing.T, binary string) {
		got := containerRun{
			Flags: []string{"-v", "/repo:/scan:ro", "-w", "/scan"},
			Image: "img:1",
			Argv:  []string{"tool", "--flag"},
			Stdin: []byte("in"),
		}.cmd()
		want := []string{"run", "--rm", "-v", "/repo:/scan:ro", "-w", "/scan", "img:1", "tool", "--flag"}
		if got.Name != binary || !slices.Equal(got.Args, want) {
			t.Errorf("cmd = %s %q, want %s %q", got.Name, got.Args, binary, want)
		}
		if got.Env != nil {
			t.Errorf("Env = %q, want nil", got.Env)
		}
		if string(got.Stdin) != "in" {
			t.Errorf("Stdin = %q, want it carried", got.Stdin)
		}
	})
}

func TestContainerRunWithoutProxyKeepsExtraEnvironment(t *testing.T) {
	useProxy(t, netproxy.Forward{})
	forEachEngine(t, func(t *testing.T, _ string) {
		got := containerRun{
			Flags: []string{"-e", "TOKEN"},
			Image: "img:1",
			Env:   []string{"TOKEN=t0k3n"},
		}.cmd()
		if want := []string{"run", "--rm", "-e", "TOKEN", "img:1"}; !slices.Equal(got.Args, want) {
			t.Errorf("Args = %q, want %q", got.Args, want)
		}
		if want := slices.Concat(os.Environ(), []string{"TOKEN=t0k3n"}); !slices.Equal(got.Env, want) {
			t.Errorf("Env = %q, want os.Environ() plus the credential %q", got.Env, want)
		}
	})
}

func TestContainerRunForwardsTheProxyByName(t *testing.T) {
	fwd := forwardOf(map[string]string{"HTTPS_PROXY": "http://u:pw@p:3128"})
	useProxy(t, fwd)
	// A stale value in this process must lose to the forwarded one.
	t.Setenv("HTTPS_PROXY", "http://stale.example.org:1")
	forEachEngine(t, func(t *testing.T, binary string) {
		got := containerRun{
			Flags: []string{"-v", "/repo:/scan:ro", "-e", "TOKEN"},
			Image: "img:1",
			Argv:  []string{"tool"},
			Env:   []string{"TOKEN=t0k3n"},
		}.cmd()
		want := []string{"run", "--rm", "-v", "/repo:/scan:ro", "-e", "TOKEN",
			"-e", "HTTPS_PROXY", "-e", "https_proxy", "img:1", "tool"}
		if got.Name != binary || !slices.Equal(got.Args, want) {
			t.Errorf("cmd = %s %q, want %s %q", got.Name, got.Args, binary, want)
		}
		for _, a := range got.Args {
			if strings.Contains(a, "pw") || strings.Contains(a, "p:3128") {
				t.Errorf("argv element %q carries the proxy", a)
			}
		}
		if s := got.String(); strings.Contains(s, "pw") || strings.Contains(s, "p:3128") {
			t.Errorf("String() carries the proxy: %s", s)
		}
		checkProxyEnv(t, got.Env, map[string]string{
			"HTTPS_PROXY": "http://u:pw@p:3128", "https_proxy": "http://u:pw@p:3128",
		})
		// The credentials come after the proxy pairs, and the rest of the
		// environment is kept.
		if want := slices.Concat(fwd.Environ(os.Environ()), []string{"TOKEN=t0k3n"}); !slices.Equal(got.Env, want) {
			t.Errorf("Env =\n  %q\nwant\n  %q", got.Env, want)
		}
	})
}

// TestEveryContainerBuilderForwardsTheProxy runs every launcher of
// containerBuilders with the three pairs set: the six -e NAME sit right before
// the image, nothing else moves, no value reaches argv or the displayed
// command, and the child's environment carries them.
func TestEveryContainerBuilderForwardsTheProxy(t *testing.T) {
	fwd := forwardOf(fullProxy)
	useProxy(t, fwd)
	forEachEngine(t, func(t *testing.T, _ string) {
		for _, tc := range containerBuilders() {
			t.Run(tc.name, func(t *testing.T) {
				useProxy(t, netproxy.Forward{})
				plain := tc.build()
				useProxy(t, fwd)
				got := tc.build()

				if want := withNamesBeforeImage(t, plain.Args, netproxy.ForwardedVariables); !slices.Equal(got.Args, want) {
					t.Errorf("Args =\n  %q\nwant\n  %q", got.Args, want)
				}
				if got.Name != plain.Name || got.Dir != plain.Dir || string(got.Stdin) != string(plain.Stdin) {
					t.Errorf("Name, Dir or Stdin changed: %q %q %q, was %q %q %q",
						got.Name, got.Dir, got.Stdin, plain.Name, plain.Dir, plain.Stdin)
				}
				for _, a := range got.Args {
					checkNoLeak(t, "argv element", a)
				}
				checkNoLeak(t, "String()", got.String())

				checkProxyEnv(t, got.Env, bothCasings(fullProxy))
				if want := slices.Concat(fwd.Environ(os.Environ()), tc.env); !slices.Equal(got.Env, want) {
					t.Errorf("Env is not the forwarded environment plus the credentials:\n  %q\nwant\n  %q", got.Env, want)
				}
			})
		}
	})
}

// TestContainerBuildersProxyForwardsOnlyTheNamesSet: a pair that is not set is
// not passed, and a pair set in one casing is passed in both.
func TestContainerBuildersProxyForwardsOnlyTheNamesSet(t *testing.T) {
	noProxyEnvironment(t) // the case where nothing is forwarded keeps os.Environ() as is
	cases := []struct {
		name string
		vars map[string]string
		want map[string]string // what the child's environment must hold
	}{
		{"NO_PROXY alone", map[string]string{"NO_PROXY": fwdNoValue},
			map[string]string{"NO_PROXY": fwdNoValue, "no_proxy": fwdNoValue}},
		{"HTTP_PROXY alone", map[string]string{"HTTP_PROXY": fwdHTTPValue},
			map[string]string{"HTTP_PROXY": fwdHTTPValue, "http_proxy": fwdHTTPValue}},
		{"lower case https_proxy alone", map[string]string{"https_proxy": fwdHTTPSValue},
			map[string]string{"HTTPS_PROXY": fwdHTTPSValue, "https_proxy": fwdHTTPSValue}},
		{"HTTPS_PROXY and NO_PROXY", map[string]string{"HTTPS_PROXY": fwdHTTPSValue, "NO_PROXY": fwdNoValue},
			map[string]string{"HTTPS_PROXY": fwdHTTPSValue, "https_proxy": fwdHTTPSValue,
				"NO_PROXY": fwdNoValue, "no_proxy": fwdNoValue}},
		{"ALL_PROXY is not read", map[string]string{"ALL_PROXY": fwdHTTPValue, "FTP_PROXY": fwdHTTPValue}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fwd := forwardOf(c.vars)
			forEachEngine(t, func(t *testing.T, _ string) {
				for _, tc := range containerBuilders() {
					t.Run(tc.name, func(t *testing.T) {
						useProxy(t, netproxy.Forward{})
						plain := tc.build()
						useProxy(t, fwd)
						got := tc.build()

						if want := withNamesBeforeImage(t, plain.Args, fwd.Names()); !slices.Equal(got.Args, want) {
							t.Errorf("Args =\n  %q\nwant\n  %q", got.Args, want)
						}
						for _, a := range got.Args {
							checkNoLeak(t, "argv element", a)
						}
						checkProxyEnv(t, got.Env, c.want)
					})
				}
			})
		})
	}
}

// TestCosignProxyKeepsRegistryCredentials: the existing -e COSIGN_REGISTRY_*
// stay once, in front of the proxy names, with their values after the
// forwarded variables in Env.
func TestCosignProxyKeepsRegistryCredentials(t *testing.T) {
	fwd := forwardOf(fullProxy)
	useProxy(t, fwd)
	forEachEngine(t, func(t *testing.T, _ string) {
		checked := 0
		for _, tc := range containerBuilders() {
			if !strings.HasPrefix(tc.name, "cosign") || len(tc.env) == 0 {
				continue
			}
			checked++
			t.Run(tc.name, func(t *testing.T) {
				got := tc.build()
				first := slices.Index(got.Args, "HTTP_PROXY")
				for _, name := range []string{cosignUserEnv, cosignPasswordEnv} {
					if n := strings.Count(strings.Join(got.Args, " "), name); n != 1 {
						t.Errorf("%s appears %d times in argv, want once: %q", name, n, got.Args)
					}
					at := slices.Index(got.Args, name)
					if at < 0 || first < 0 || at > first {
						t.Errorf("%s at %d must come before the proxy names (at %d): %q", name, at, first, got.Args)
					}
					if at > 0 && got.Args[at-1] != "-e" {
						t.Errorf("%s is not passed as -e NAME: %q", name, got.Args)
					}
				}
				for _, kv := range tc.env {
					if !slices.Contains(got.Env, kv) {
						t.Errorf("Env lost %s", strings.SplitN(kv, "=", 2)[0])
					}
				}
				if tail := got.Env[len(got.Env)-len(tc.env):]; !slices.Equal(tail, tc.env) {
					t.Errorf("credentials must come after the proxy variables: %q", got.Env)
				}
			})
		}
		if checked == 0 {
			t.Fatal("no cosign case with credentials in containerBuilders")
		}
	})
}

// TestGitleaksCommandProxyShowsNamesNotValues: the command the UI shows is
// replayable in the same shell, so it names the variables and carries none of
// their values.
func TestGitleaksCommandProxyShowsNamesNotValues(t *testing.T) {
	useProxy(t, forwardOf(fullProxy))
	forEachEngine(t, func(t *testing.T, _ string) {
		shown := GetGitleaksCommand("/repo", ToolSpec{Source: ToolSourceContainer}, false, "")
		for _, name := range netproxy.ForwardedVariables {
			if !strings.Contains(shown, " -e "+name+" ") {
				t.Errorf("command does not name %s: %s", name, shown)
			}
		}
		checkNoLeak(t, "the displayed command", shown)
	})
}

// TestContainerVersionCmdProxy covers the one launcher that was not a toolCmd.
func TestContainerVersionCmdProxy(t *testing.T) {
	t.Run("no proxy", func(t *testing.T) {
		useProxy(t, netproxy.Forward{})
		forEachEngine(t, func(t *testing.T, binary string) {
			got := containerVersionCmd("aquasec/trivy", "--version")
			if want := []string{"run", "--rm", "aquasec/trivy", "--version"}; got.Name != binary || !slices.Equal(got.Args, want) {
				t.Errorf("cmd = %s %q, want %s %q", got.Name, got.Args, binary, want)
			}
			if got.Env != nil {
				t.Errorf("Env = %q, want nil", got.Env)
			}
		})
	})
	t.Run("proxy", func(t *testing.T) {
		fwd := forwardOf(fullProxy)
		useProxy(t, fwd)
		forEachEngine(t, func(t *testing.T, binary string) {
			got := containerVersionCmd("aquasec/trivy", "--version")
			want := slices.Concat([]string{"run", "--rm"}, pairsOf(netproxy.ForwardedVariables), []string{"aquasec/trivy", "--version"})
			if got.Name != binary || !slices.Equal(got.Args, want) {
				t.Errorf("cmd = %s %q, want %s %q", got.Name, got.Args, binary, want)
			}
			for _, a := range got.Args {
				checkNoLeak(t, "argv element", a)
			}
			checkNoLeak(t, "String()", got.String())
			checkProxyEnv(t, got.Env, bothCasings(fullProxy))
		})
	})
}

// TestContainerToolVersionProxyEnvironment runs containerToolVersion through a
// fake engine (internal/testutil/fakecli, reached through the binary the shape
// names): the proxy names are in its argv, the values only in the environment
// it was started with.
func TestContainerToolVersionProxyEnvironment(t *testing.T) {
	useProxy(t, forwardOf(fullProxy))
	fake := fakecli.Install(t, fakecli.Scenario{
		{Tool: "docker", Prefix: true, Reply: fakecli.Reply{Stdout: "v1\n"}},
	})
	shape := engine.ShapeFor(engine.Docker)
	shape.Binary = fake.InstallAt(t, t.TempDir(), "docker")
	useEngine(t, shape)

	got := containerToolVersion("aquasec/trivy", "--version")

	if want := "docker:v1\n"; got != want {
		t.Errorf("containerToolVersion = %q, want %q", got, want)
	}
	wantArgs := []string{"run", "--rm",
		"-e", "HTTP_PROXY", "-e", "http_proxy", "-e", "HTTPS_PROXY", "-e", "https_proxy", "-e", "NO_PROXY", "-e", "no_proxy",
		"aquasec/trivy", "--version"}
	inv := fake.Invocations()
	if len(inv) != 1 || !slices.Equal(inv[0].Args, wantArgs) {
		t.Fatalf("engine runs = %+v, want one with argv %q", inv, wantArgs)
	}
	for _, want := range []string{
		"HTTP_PROXY=" + fwdHTTPValue,
		"https_proxy=" + fwdHTTPSValue,
		"no_proxy=" + fwdNoValue,
	} {
		if !slices.Contains(inv[0].Env, want) {
			t.Errorf("the engine environment lacks %q", want)
		}
	}
}
