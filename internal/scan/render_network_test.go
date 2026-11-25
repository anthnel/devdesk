package scan

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/netproxy"
)

// DEVDESK-767: the renderers (kustomize, helm) run with the network cut. A
// repository that is scanned is not trusted, and an overlay that names
// `git@host:o/r` or `https://host/m.yaml` makes kustomize fetch it, with the
// user's ssh agent, proxy and credentials, before anything was validated.
//
// This file holds the environment a native renderer gets (offlineEnviron), the
// argv a container renderer gets (containerRun.Offline), the wiring that picks
// between the two and the `allow_remote_bases` switch, and what a blocked fetch
// is reported as (remoteBlocked, the K8S-RENDER finding).

// ── What a blocked fetch looks like ──────────────────────────────────────────

// Measured on kustomize v5.8.3 and git 2.47.3, run with offlineEnviron's
// environment (GIT_ALLOW_PROTOCOL naming no transport and a proxy that refuses
// every connection), paths anonymised. These are the markers remoteBlocked reads.
const (
	sshBlockedStderr = "Error: accumulating resources: accumulation err='accumulating resources from " +
		"'git@unreachable.invalid:o/r': evalsymlink failure on " +
		"'/home/u/repo/kustomizations/dev/git@unreachable.invalid:o/r' : lstat " +
		"/home/u/repo/kustomizations/dev/git@unreachable.invalid:o: no such file or directory': " +
		"failed to run '/usr/bin/git fetch --depth=1 git@unreachable.invalid:o/r HEAD': " +
		"fatal: transport 'ssh' not allowed\n: exit status 128"

	httpsGitBlockedStderr = "Error: accumulating resources: accumulation err='accumulating resources from " +
		"'https://unreachable.invalid/o/r.git': Get \"https://unreachable.invalid/o/r.git\": " +
		"proxyconnect tcp: dial tcp 127.0.0.1:0: connect: connection refused': " +
		"failed to run '/usr/bin/git fetch --depth=1 https://unreachable.invalid/o/r.git HEAD': " +
		"fatal: transport 'https' not allowed\n: exit status 128"

	urlBlockedStderr = "Error: accumulating resources: accumulation err='accumulating resources from " +
		"'https://unreachable.invalid/m.yaml': Get \"https://unreachable.invalid/m.yaml\": " +
		"proxyconnect tcp: dial tcp 127.0.0.1:0: connect: connection refused': " +
		"must build at directory: not a valid directory: evalsymlink failure on " +
		"'/home/u/repo/kustomizations/dev/https:/unreachable.invalid/m.yaml' : lstat " +
		"/home/u/repo/kustomizations/dev/https:: no such file or directory"

	// A path that is not there, and a YAML mistake: nothing to do with the
	// network, so never reported as blocked.
	missingLocalStderr = "Error: accumulating resources: accumulation err='accumulating resources from '../nope': " +
		"evalsymlink failure on '/home/u/repo/nope' : lstat /home/u/repo/nope: no such file or directory': " +
		"must build at directory: not a valid directory: evalsymlink failure on '/home/u/repo/nope' : " +
		"lstat /home/u/repo/nope: no such file or directory"
	patchMistakeStderr = "Error: no matches for Id Deployment.v1.apps/web.[noNs]; failed to find unique target for patch"

	// What an image of kustomize is expected to print under `--network none`
	// (NOT measured: docker is not reachable here; the plan names these markers
	// and the real run in a container is to be recorded when one is available).
	containerNoSuchHostStderr = "Error: accumulating resources: accumulation err='accumulating resources from " +
		"'https://unreachable.invalid/m.yaml': Get \"https://unreachable.invalid/m.yaml\": " +
		"dial tcp: lookup unreachable.invalid on 127.0.0.11:53: no such host': must build at directory: " +
		"not a valid directory: evalsymlink failure on '/scan/kustomizations/dev/https:/unreachable.invalid/m.yaml' : " +
		"lstat /scan/kustomizations/dev/https:: no such file or directory"
	containerCouldNotResolveStderr = "Error: accumulating resources: accumulation err='accumulating resources from " +
		"'https://unreachable.invalid/o/r.git': evalsymlink failure on '/scan/kustomizations/dev/https:/unreachable.invalid/o/r.git' : " +
		"lstat /scan/kustomizations/dev/https:: no such file or directory': failed to run " +
		"'/usr/bin/git fetch --depth=1 https://unreachable.invalid/o/r.git HEAD': " +
		"fatal: unable to access 'https://unreachable.invalid/o/r.git/': Could not resolve host: unreachable.invalid"
	containerUnreachableStderr = "Error: accumulating resources: accumulation err='accumulating resources from " +
		"'https://203.0.113.9/m.yaml': Get \"https://203.0.113.9/m.yaml\": " +
		"dial tcp 203.0.113.9:443: connect: network is unreachable': must build at directory: not a valid directory"
	containerNoGitStderr = "Error: accumulating resources: accumulation err='accumulating resources from " +
		"'git@unreachable.invalid:o/r': evalsymlink failure on '/scan/kustomizations/dev/git@unreachable.invalid:o/r' : " +
		"lstat /scan/kustomizations/dev/git@unreachable.invalid:o: no such file or directory': " +
		"no 'git' program on path: exec: \"git\": executable file not found in $PATH"
	containerMissingLocalStderr = "Error: accumulating resources: accumulation err='accumulating resources from '../nope': " +
		"evalsymlink failure on '/scan/nope' : lstat /scan/nope: no such file or directory': " +
		"must build at directory: not a valid directory"
)

func TestRemoteBlockedReadsWhatKustomizePrintedWithTheNetworkCut(t *testing.T) {
	tests := []struct {
		name   string
		detail string
		from   ToolSource
		want   bool
	}{
		// Measured, from a binary.
		{"ssh base, native", sshBlockedStderr, ToolSourceBinary, true},
		{"https git base, native", httpsGitBlockedStderr, ToolSourceBinary, true},
		{"https resource, native", urlBlockedStderr, ToolSourceBinary, true},

		// Expected, from an image.
		{"no such host, container", containerNoSuchHostStderr, ToolSourceContainer, true},
		{"could not resolve host, container", containerCouldNotResolveStderr, ToolSourceContainer, true},
		{"network unreachable, container", containerUnreachableStderr, ToolSourceContainer, true},
		{"no git program, container", containerNoGitStderr, ToolSourceContainer, true},

		// Not the network.
		{"a local path that is not there, native", missingLocalStderr, ToolSourceBinary, false},
		{"a local path that is not there, container", containerMissingLocalStderr, ToolSourceContainer, false},
		{"a patch that matches nothing, native", patchMistakeStderr, ToolSourceBinary, false},
		{"a patch that matches nothing, container", patchMistakeStderr, ToolSourceContainer, false},
		{"the load restrictor", loadRestrictorStderr, ToolSourceBinary, false},
		{"a host that does not resolve, away from any resource of the overlay",
			"Error: lookup registry.example.com on 127.0.0.11:53: no such host", ToolSourceContainer, false},
		{"nothing printed", "", ToolSourceBinary, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := remoteBlocked(tt.detail, tt.from); got != tt.want {
				t.Errorf("remoteBlocked(%q, %s) = %v, want %v", tt.detail, tt.from, got, tt.want)
			}
		})
	}
}

// ── offlineEnviron ───────────────────────────────────────────────────────────

// valuesOf is every value env gives to name, in order.
func valuesOf(env []string, name string) []string {
	var out []string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			out = append(out, v)
		}
	}
	return out
}

// checkOffline fails unless env cuts the network of a native renderer: every
// name exactly once, with its dead value.
func checkOffline(t *testing.T, env []string) {
	t.Helper()
	const dead = "http://127.0.0.1:0"
	want := map[string]string{
		"GIT_ALLOW_PROTOCOL": "devdesk-none", "GIT_TERMINAL_PROMPT": "0",
		"HTTP_PROXY": dead, "HTTPS_PROXY": dead, "http_proxy": dead, "https_proxy": dead,
		"NO_PROXY": "", "no_proxy": "",
		"LC_ALL": "C",
	}
	for name, value := range want {
		if got := valuesOf(env, name); !slices.Equal(got, []string{value}) {
			t.Errorf("%s = %q, want exactly [%q]", name, got, value)
		}
	}
	for _, name := range []string{"ALL_PROXY", "all_proxy", "REQUEST_METHOD",
		"SSH_AUTH_SOCK", "GIT_SSH", "GIT_SSH_COMMAND", "GIT_PROXY_COMMAND"} {
		if got := valuesOf(env, name); len(got) != 0 {
			t.Errorf("%s = %q, want it removed", name, got)
		}
	}
}

func TestOfflineEnvironCutsTheNetworkAndKeepsTheRest(t *testing.T) {
	tests := []struct {
		name string
		base []string
		// kept are entries that must come out untouched, in this order.
		kept []string
	}{
		{"no base", nil, nil},
		{"nothing of the network in it", []string{"PATH=/usr/bin", "HOME=/home/u"},
			[]string{"PATH=/usr/bin", "HOME=/home/u"}},
		{"the host's proxy, both casings",
			[]string{"PATH=/usr/bin",
				"HTTP_PROXY=http://u:pw@proxy.example:3128", "http_proxy=http://u:pw@proxy.example:3128",
				"HTTPS_PROXY=http://u:pw@proxy.example:3128", "https_proxy=http://u:pw@proxy.example:3128",
				"ALL_PROXY=socks5://proxy.example:1080", "all_proxy=socks5://proxy.example:1080",
				"NO_PROXY=.corp.example", "no_proxy=.corp.example", "LANG=fr_FR.UTF-8"},
			[]string{"PATH=/usr/bin", "LANG=fr_FR.UTF-8"}},
		{"a git protocol list, a locale and a request method",
			[]string{"GIT_ALLOW_PROTOCOL=https:ssh", "LC_ALL=fr_FR.UTF-8", "REQUEST_METHOD=GET", "TERM=xterm"},
			[]string{"TERM=xterm"}},
		{"the user's ssh identity and git commands",
			[]string{"HOME=/home/u", "SSH_AUTH_SOCK=/tmp/agent.1", "GIT_SSH=/usr/bin/ssh", "GIT_SSH_COMMAND=ssh -i /home/u/.ssh/id",
				"GIT_PROXY_COMMAND=/usr/bin/nc", "GIT_TERMINAL_PROMPT=1", "XDG_RUNTIME_DIR=/run/user/1000"},
			[]string{"HOME=/home/u", "XDG_RUNTIME_DIR=/run/user/1000"}},
		{"values holding an equal sign", []string{"A=b=c", "SOME_SOCK=/tmp/x=y"},
			[]string{"A=b=c", "SOME_SOCK=/tmp/x=y"}},
		{"a variable that only looks like one of them", []string{"MY_HTTP_PROXY=x", "GIT_ALLOW_PROTOCOL_X=y", "LC_ALL_X=z", "PROXY=1"},
			[]string{"MY_HTTP_PROXY=x", "GIT_ALLOW_PROTOCOL_X=y", "LC_ALL_X=z", "PROXY=1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := slices.Clone(tt.base)

			got := offlineEnviron(tt.base)

			checkOffline(t, got)
			var kept []string
			for _, kv := range got {
				if slices.Contains(tt.kept, kv) {
					kept = append(kept, kv)
				}
			}
			if !slices.Equal(kept, tt.kept) {
				t.Errorf("the other variables = %q, want %q, in this order", kept, tt.kept)
			}
			if !slices.Equal(tt.base, snapshot) {
				t.Errorf("the base was modified: %q, was %q", tt.base, snapshot)
			}
			for i := range got {
				got[i] = "X=changed"
			}
			if !slices.Equal(tt.base, snapshot) {
				t.Errorf("the result shares memory with the base: %q, was %q", tt.base, snapshot)
			}
		})
	}
}

// A secret the host holds in its proxy variable is not in what the renderer is
// handed, by any name.
func TestOfflineEnvironDoesNotCarryTheHostProxyAnywhere(t *testing.T) {
	got := offlineEnviron([]string{"HTTPS_PROXY=http://alice:s3cr3tpw@proxy.example:3128", "https_proxy=http://alice:s3cr3tpw@proxy.example:3128"})

	for _, kv := range got {
		if strings.Contains(kv, "s3cr3tpw") || strings.Contains(kv, "proxy.example") {
			t.Errorf("%q carries the host's proxy", kv)
		}
	}
}

// ── containerRun.Offline ─────────────────────────────────────────────────────

func TestAnOfflineContainerRunCutsTheNetworkAndForwardsNoProxy(t *testing.T) {
	noProxyEnvironment(t)
	useProxy(t, forwardOf(fullProxy))
	forEachEngine(t, func(t *testing.T, binary string) {
		got := containerRun{
			Flags:   []string{"-v", "/repo:/scan:ro", "-w", "/scan"},
			Image:   "img:1",
			Argv:    []string{"build", "./overlay"},
			Stdin:   []byte("in"),
			Offline: true,
		}.cmd()

		want := []string{"run", "--rm", "-v", "/repo:/scan:ro", "-w", "/scan", "--network", "none", "img:1", "build", "./overlay"}
		if got.Name != binary || !slices.Equal(got.Args, want) {
			t.Errorf("cmd = %s %q, want %s %q", got.Name, got.Args, binary, want)
		}
		for _, name := range netproxy.ForwardedVariables {
			if slices.Contains(got.Args, name) {
				t.Errorf("argv names %s although the container is offline: %q", name, got.Args)
			}
		}
		if got.Env != nil {
			t.Errorf("Env = %q, want nil: nothing to forward and nothing extra", got.Env)
		}
		if string(got.Stdin) != "in" {
			t.Errorf("Stdin = %q, want it carried", got.Stdin)
		}
	})
}

// The extra variables of the caller still travel in the child's environment,
// and the proxy values still do not.
func TestAnOfflineContainerRunKeepsItsOwnEnvironmentButNoProxy(t *testing.T) {
	noProxyEnvironment(t)
	useProxy(t, forwardOf(fullProxy))
	forEachEngine(t, func(t *testing.T, _ string) {
		got := containerRun{
			Flags:   []string{"-e", "TOKEN"},
			Image:   "img:1",
			Env:     []string{"TOKEN=t0k3n"},
			Offline: true,
		}.cmd()

		if want := []string{"run", "--rm", "-e", "TOKEN", "--network", "none", "img:1"}; !slices.Equal(got.Args, want) {
			t.Errorf("Args = %q, want %q", got.Args, want)
		}
		if !slices.Contains(got.Env, "TOKEN=t0k3n") {
			t.Errorf("Env lost the caller's variable: %q", got.Env)
		}
		checkProxyEnv(t, got.Env, nil)
		for _, kv := range got.Env {
			checkNoLeak(t, "Env entry", kv)
		}
	})
}

// Online is what it was: the proxy is forwarded and nothing cuts the network.
func TestAContainerRunThatIsNotOfflineIsUnchanged(t *testing.T) {
	fwd := forwardOf(fullProxy)
	useProxy(t, fwd)
	forEachEngine(t, func(t *testing.T, _ string) {
		got := containerRun{Flags: []string{"-w", "/scan"}, Image: "img:1", Argv: []string{"x"}}.cmd()

		if slices.Contains(got.Args, "--network") || slices.Contains(got.Args, "none") {
			t.Errorf("a run that is not offline cut the network: %q", got.Args)
		}
		want := slices.Concat([]string{"run", "--rm", "-w", "/scan"}, pairsOf(netproxy.ForwardedVariables), []string{"img:1", "x"})
		if !slices.Equal(got.Args, want) {
			t.Errorf("Args = %q, want %q", got.Args, want)
		}
		checkProxyEnv(t, got.Env, bothCasings(fullProxy))
	})
}

// ── What the scan hands to helm, kustomize and kubeconform ───────────────────

// networkRunner answers every tool of the schema stage with a success, or with
// kustomizeErr for `kustomize build`, and records what it was asked. git (asked
// for the mount of a container renderer) has no repository to name.
func networkRunner(t *testing.T, kustomizeErr string) *scriptedRunner {
	t.Helper()
	lint := "==> Linting charts/api\n\n1 chart(s) linted, 0 chart(s) failed\n"
	r := &scriptedRunner{}
	r.reply = func(tc toolCmd) ([]byte, error) {
		if tc.Name == "git" {
			return nil, &exitError{Code: 128, Stderr: "not a git repository"}
		}
		switch subcommandOf(tc) {
		case "lint":
			return []byte(lint), nil
		case "template":
			return []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"), nil
		case "build":
			if kustomizeErr != "" {
				return nil, &exitError{Code: 1, Stderr: kustomizeErr}
			}
			return []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: worker\n"), nil
		}
		return []byte(`{"resources":[]}`), nil
	}
	useRunner(t, r)
	return r
}

// callsTo is the recorded invocations that satisfy pred.
func (r *scriptedRunner) callsTo(pred func(toolCmd) bool) []toolCmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []toolCmd
	for _, c := range r.calls {
		if pred(c) {
			out = append(out, c)
		}
	}
	return out
}

// renderCall is the one invocation of a helm or kustomize subcommand.
func renderCall(t *testing.T, r *scriptedRunner, sub string) toolCmd {
	t.Helper()
	calls := r.callsTo(func(tc toolCmd) bool { return subcommandOf(tc) == sub })
	if len(calls) != 1 {
		t.Fatalf("%d calls of %q, want 1, among %q", len(calls), sub, r.commands())
	}
	return calls[0]
}

// kubeconformCall is the invocation that validates what was rendered.
func kubeconformCall(t *testing.T, r *scriptedRunner) toolCmd {
	t.Helper()
	calls := r.callsTo(func(tc toolCmd) bool { return len(tc.Stdin) > 0 })
	if len(calls) == 0 {
		t.Fatalf("nothing was handed to kubeconform, among %q", r.commands())
	}
	return calls[0]
}

// networkNoneBefore reports whether the argv holds `--network none`, once,
// before the image: after the image it would be the tool's own argument.
func networkNoneBefore(args []string, image string) bool {
	at := slices.Index(args, "--network")
	img := slices.Index(args, image)
	return slices.Index(args[at+1:], "--network") < 0 && at >= 0 && img > at+1 && args[at+1] == "none"
}

func allowRemoteBases(opts ScanOptions) ScanOptions {
	opts.Tools.Kustomize.AllowRemoteBases = true
	return opts
}

// The option reaches the schema stage.
func TestTheSchemaStageIsToldWhetherRemoteBasesAreAllowed(t *testing.T) {
	isolateHome(t)
	deps := renderingDeps()

	if newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).kubeconformOptions().AllowRemoteBases {
		t.Error("remote bases are allowed by default")
	}
	if !newScannerWithDeps(allowRemoteBases(k8sSchemaFor("1.36.0")), deps).kubeconformOptions().AllowRemoteBases {
		t.Error("scan.tools.kustomize.allow_remote_bases did not reach the schema stage")
	}
}

// A renderer that runs on this machine is handed an environment that cuts the
// network: the host's proxy and ssh transport are never reached, whatever this
// process holds.
func TestNativeRenderersRunWithTheNetworkCut(t *testing.T) {
	isolateHome(t)
	// The pieces of the host's proxy value, matched as they stand in a proxy
	// URL. The environment checked below is this process's own, where HOME,
	// USERPROFILE and TMPDIR carry the random digits of a t.TempDir name: a
	// bare port number would match them (DEVDESK-784).
	const hostProxy = "http://alice:s3cr3tpw@proxy.example:3128"
	hostProxyPieces := []string{hostProxy, "alice:", "s3cr3tpw", "proxy.example", ":3128"}
	t.Setenv("HTTPS_PROXY", hostProxy)
	t.Setenv("GIT_ALLOW_PROTOCOL", "https:ssh")
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	r := networkRunner(t, "")

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), renderingDeps()).
		Scan(t.Context(), renderRepo(t), TargetDirectory)

	if len(result.Errors) != 0 {
		t.Fatalf("errors = %v", result.Errors)
	}
	for _, sub := range []string{"build", "lint", "template"} {
		t.Run(sub, func(t *testing.T) {
			call := renderCall(t, r, sub)
			if call.Env == nil {
				t.Fatal("Env is nil: the renderer inherits this process's environment")
			}
			checkOffline(t, call.Env)
			for _, kv := range call.Env {
				checkNoneOf(t, "Env entry", kv, hostProxyPieces)
			}
			if call.Dir == "" {
				t.Error("the renderer no longer runs in the scanned directory")
			}
		})
	}
}

// Only the render is cut: kubeconform downloads the schemas it validates
// against, and is given the network it had.
func TestKubeconformKeepsTheNetworkWhenTheRenderersLoseIt(t *testing.T) {
	isolateHome(t)
	r := networkRunner(t, "")

	_, _ = newScannerWithDeps(k8sSchemaFor("1.36.0"), renderingDeps()).Scan(t.Context(), renderRepo(t), TargetDirectory)

	call := kubeconformCall(t, r)
	if slices.Contains(valuesOf(call.Env, "GIT_ALLOW_PROTOCOL"), "devdesk-none") || slices.Contains(valuesOf(call.Env, "HTTPS_PROXY"), "http://127.0.0.1:0") {
		t.Errorf("kubeconform runs with the network cut: %q", call.Env)
	}
}

// allow_remote_bases opens kustomize alone: helm has no remote base to allow.
func TestAllowRemoteBasesLeavesKustomizeTheNetworkAndNothingElse(t *testing.T) {
	isolateHome(t)
	t.Setenv("GIT_ALLOW_PROTOCOL", "https:ssh")
	r := networkRunner(t, "")

	_, _ = newScannerWithDeps(allowRemoteBases(k8sSchemaFor("1.36.0")), renderingDeps()).
		Scan(t.Context(), renderRepo(t), TargetDirectory)

	build := renderCall(t, r, "build")
	for _, kv := range build.Env {
		if kv == "GIT_ALLOW_PROTOCOL=devdesk-none" || strings.Contains(kv, "127.0.0.1:0") || kv == "LC_ALL=C" {
			t.Errorf("kustomize still runs with the network cut: %q", kv)
		}
	}
	if build.Env != nil && !slices.Equal(valuesOf(build.Env, "GIT_ALLOW_PROTOCOL"), []string{"https:ssh"}) {
		t.Errorf("GIT_ALLOW_PROTOCOL = %q, want this process's own", valuesOf(build.Env, "GIT_ALLOW_PROTOCOL"))
	}
	checkOffline(t, renderCall(t, r, "lint").Env)
	checkOffline(t, renderCall(t, r, "template").Env)
}

func containerRenderers() Report {
	return asContainers(binaries(ToolTrivy, ToolGitleaks), ToolKubeconform, ToolHelm, ToolKustomize)
}

// A renderer from an image is run with `--network none` and no proxy at all;
// kubeconform from an image keeps both.
func TestContainerRenderersRunWithoutANetwork(t *testing.T) {
	isolateHome(t)
	useProxy(t, forwardOf(fullProxy))
	forEachEngine(t, func(t *testing.T, _ string) {
		r := networkRunner(t, "")

		_, _ = newScannerWithDeps(k8sSchemaFor("1.36.0"), containerRenderers()).
			Scan(t.Context(), renderRepo(t), TargetDirectory)

		for sub, image := range map[string]string{"build": DefaultKustomizeImage, "lint": DefaultHelmImage, "template": DefaultHelmImage} {
			call := renderCall(t, r, sub)
			if !networkNoneBefore(call.Args, image) {
				t.Errorf("%s: no `--network none` before %s: %q", sub, image, call.Args)
			}
			for _, name := range netproxy.ForwardedVariables {
				if slices.Contains(call.Args, name) {
					t.Errorf("%s forwards %s although it has no network: %q", sub, name, call.Args)
				}
			}
			checkProxyEnv(t, call.Env, nil)
		}

		kc := kubeconformCall(t, r)
		if slices.Contains(kc.Args, "--network") {
			t.Errorf("kubeconform has no network but needs one for its schemas: %q", kc.Args)
		}
		if !slices.Contains(kc.Args, "HTTP_PROXY") {
			t.Errorf("kubeconform no longer gets the proxy: %q", kc.Args)
		}
	})
}

// With allow_remote_bases the kustomize image has its network and its proxy
// back; helm still has none.
func TestAllowRemoteBasesGivesTheKustomizeImageItsNetworkBack(t *testing.T) {
	isolateHome(t)
	useProxy(t, forwardOf(fullProxy))
	forEachEngine(t, func(t *testing.T, _ string) {
		r := networkRunner(t, "")

		_, _ = newScannerWithDeps(allowRemoteBases(k8sSchemaFor("1.36.0")), containerRenderers()).
			Scan(t.Context(), renderRepo(t), TargetDirectory)

		build := renderCall(t, r, "build")
		if slices.Contains(build.Args, "--network") {
			t.Errorf("kustomize still has its network cut: %q", build.Args)
		}
		if !slices.Contains(build.Args, "HTTP_PROXY") {
			t.Errorf("kustomize does not get the proxy back: %q", build.Args)
		}
		for _, sub := range []string{"lint", "template"} {
			if call := renderCall(t, r, sub); !networkNoneBefore(call.Args, DefaultHelmImage) {
				t.Errorf("helm %s lost its `--network none`: %q", sub, call.Args)
			}
		}
	})
}

// A chart and an overlay that are all in the repository render the same, and
// are reported the same, with the network cut or not.
func TestLocalChartsAndOverlaysAreReportedTheSameOfflineOrNot(t *testing.T) {
	summarize := func(allow bool) []string {
		isolateHome(t)
		renderRunner(t, stageReply{})
		opts := k8sSchemaFor("1.36.0")
		if allow {
			opts = allowRemoteBases(opts)
		}
		result, _ := newScannerWithDeps(opts, renderingDeps()).Scan(t.Context(), renderRepo(t), TargetDirectory)
		var out []string
		for _, f := range result.Findings {
			out = append(out, strings.Join([]string{f.ID, f.Source, f.File, f.Title, string(f.Severity)}, "|"))
		}
		slices.Sort(out)
		if len(result.Errors) != 0 || len(result.K8sUnrendered) != 0 {
			t.Errorf("errors %v, unrendered %v, want none", result.Errors, result.K8sUnrendered)
		}
		return out
	}

	offline, online := summarize(false), summarize(true)

	if len(offline) == 0 {
		t.Fatal("no finding: the fixture lost its schema errors")
	}
	if !slices.Equal(offline, online) {
		t.Errorf("findings differ:\n offline %q\n online  %q", offline, online)
	}
	for _, f := range offline {
		if strings.Contains(f, "remote base blocked") {
			t.Errorf("a local overlay was reported as blocked: %s", f)
		}
	}
}

// ── The finding ──────────────────────────────────────────────────────────────

const remoteBlockedTitle = "Overlay does not build: remote base blocked"

// scanOverlay scans kustomizeRepo with kustomize failing with stderr.
func scanOverlay(t *testing.T, deps Report, opts ScanOptions, stderr string) *Result {
	t.Helper()
	isolateHome(t)
	failingRenderRunner(t, stderr, "")
	result, _ := newScannerWithDeps(opts, deps).Scan(t.Context(), kustomizeRepo(t), TargetDirectory)
	return result
}

func kustomizeBinary() Report {
	return binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolKustomize)
}

func kustomizeImage() Report {
	return asContainers(binaries(ToolTrivy, ToolGitleaks, ToolKubeconform), ToolKustomize)
}

// A remote base that was refused is a finding of its own: it says the network
// was cut, so that nobody reads "does not build" as a broken overlay, and it
// says how to get either outcome.
func TestABlockedRemoteBaseSaysTheNetworkWasCut(t *testing.T) {
	cases := []struct {
		name   string
		deps   Report
		stderr string
		from   ToolSource
	}{
		{"ssh, native", kustomizeBinary(), sshBlockedStderr, ToolSourceBinary},
		{"https git, native", kustomizeBinary(), httpsGitBlockedStderr, ToolSourceBinary},
		{"https resource, native", kustomizeBinary(), urlBlockedStderr, ToolSourceBinary},
		{"no such host, container", kustomizeImage(), containerNoSuchHostStderr, ToolSourceContainer},
		{"could not resolve host, container", kustomizeImage(), containerCouldNotResolveStderr, ToolSourceContainer},
		{"no git program, container", kustomizeImage(), containerNoGitStderr, ToolSourceContainer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := scanOverlay(t, c.deps, k8sSchemaFor("1.36.0"), c.stderr)

			f := renderFindingOf(t, result)
			if !strings.HasPrefix(f.Title, remoteBlockedTitle) {
				t.Errorf("title = %q, want it to start with %q", f.Title, remoteBlockedTitle)
			}
			if f.Severity != SeverityHigh || f.ID != K8sRenderID || f.Source != SourceKustomize || f.File != kustomizationDev+"/kustomization.yaml" {
				t.Errorf("finding = %s %s %s in %s: only the wording changes", f.ID, f.Severity, f.Source, f.File)
			}
			if f.Message != strings.TrimSpace(c.stderr) {
				t.Errorf("the tool's message was not kept whole:\n got %q\nwant %q", f.Message, strings.TrimSpace(c.stderr))
			}
			lower := strings.ToLower(f.Resolution)
			for _, want := range []string{"network", "copy", "scan.tools.kustomize.allow_remote_bases: true"} {
				if !strings.Contains(lower, want) {
					t.Errorf("the Resolution lacks %q:\n%s", want, f.Resolution)
				}
			}
			if f.Resolution == kustomizeRenderResolution {
				t.Error("the Resolution is the generic one")
			}
			if len(f.Steps) == 0 || reflectEqualSteps(f.Steps, renderSteps(SourceKustomize, kustomizationDev, c.from, patchMistakeStderr, false)) {
				t.Errorf("steps = %q, want steps of their own", f.Steps)
			}
			if f.RanFrom != c.from {
				t.Errorf("RanFrom = %q, want %q", f.RanFrom, c.from)
			}
			// A copied `kustomize build` would run the refused fetch again, with the
			// user's ssh agent and proxy, on a repository that is not trusted.
			if f.Repro != "" {
				t.Errorf("repro = %q, want none: it would run the refused fetch with the user's credentials", f.Repro)
			}
		})
	}
}

func reflectEqualSteps(a, b []string) bool { return slices.Equal(a, b) }

// A failure that has nothing to do with the network reads as before, offline
// or not.
func TestAFailureThatIsNotTheNetworkKeepsItsText(t *testing.T) {
	cases := []struct {
		name   string
		deps   Report
		stderr string
	}{
		{"a patch that matches nothing, native", kustomizeBinary(), patchMistakeStderr},
		{"a path that is not there, native", kustomizeBinary(), missingLocalStderr},
		{"a path that is not there, container", kustomizeImage(), containerMissingLocalStderr},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := scanOverlay(t, c.deps, k8sSchemaFor("1.36.0"), c.stderr)

			f := renderFindingOf(t, result)
			first, _, _ := strings.Cut(strings.TrimSpace(c.stderr), "\n")
			if want := "Overlay does not build: " + first; f.Title != want {
				t.Errorf("title = %q, want %q", f.Title, want)
			}
			if strings.Contains(f.Resolution, "allow_remote_bases") || strings.Contains(strings.Join(f.Steps, " "), "network") {
				t.Errorf("a local failure talks about the network:\nResolution %q\nsteps %q", f.Resolution, f.Steps)
			}
			if !strings.HasPrefix(f.Resolution, kustomizeRenderResolution) {
				t.Errorf("Resolution = %q, want it to start with the usual one", f.Resolution)
			}
		})
	}
}

// With the switch on nothing was cut, so a fetch that fails is an overlay that
// does not build, never a blocked one.
func TestWithRemoteBasesAllowedAFailedFetchIsNeverBlocked(t *testing.T) {
	for _, c := range []struct {
		name   string
		deps   Report
		stderr string
	}{
		{"ssh, native", kustomizeBinary(), sshBlockedStderr},
		{"https resource, native", kustomizeBinary(), urlBlockedStderr},
		{"no such host, container", kustomizeImage(), containerNoSuchHostStderr},
	} {
		t.Run(c.name, func(t *testing.T) {
			result := scanOverlay(t, c.deps, allowRemoteBases(k8sSchemaFor("1.36.0")), c.stderr)

			f := renderFindingOf(t, result)
			if strings.Contains(f.Title, "remote base blocked") || strings.Contains(f.Resolution, "allow_remote_bases") {
				t.Errorf("a run with the network on says it was cut:\ntitle %q\nResolution %q", f.Title, f.Resolution)
			}
			if !strings.HasPrefix(f.Title, "Overlay does not build: ") {
				t.Errorf("title = %q", f.Title)
			}
		})
	}
}

// helm never reads a remote base, and its render failures are about its own
// dependencies: the blocked wording belongs to kustomize.
func TestAHelmFailureIsNeverReportedAsABlockedRemoteBase(t *testing.T) {
	isolateHome(t)
	failingRenderRunner(t, "", sshBlockedStderr)
	repo := writeRepo(t, map[string]string{
		"charts/api/Chart.yaml":            "apiVersion: v2\nname: webapp\nversion: 0.1.0\n",
		"charts/api/templates/deploy.yaml": "kind: {{ .Values.kind }}\n",
	})
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolHelm)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), repo, TargetDirectory)

	f := renderFindingOf(t, result)
	if strings.Contains(f.Title, "remote base blocked") || strings.Contains(f.Resolution, "allow_remote_bases") {
		t.Errorf("helm failure: title %q, Resolution %q", f.Title, f.Resolution)
	}
}

// ── The real tools ───────────────────────────────────────────────────────────

// kustomizeWithGit is the kustomize on PATH when it is new enough to have
// printed the markers above (v5.8.3) and git is there to be refused. The test
// is skipped otherwise.
func kustomizeWithGit(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("kustomize")
	if err != nil {
		t.Skip("no kustomize on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH: kustomize reports a missing git, not a blocked transport")
	}
	out, err := exec.CommandContext(t.Context(), path, "version").Output() //nolint:gosec // the kustomize found on PATH, version only
	if err != nil {
		t.Skipf("kustomize version: %v", err)
	}
	m := regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		t.Skipf("cannot read a version in %q", out)
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	if slices.Compare(v[:], []int{5, 8, 3}) < 0 {
		t.Skipf("kustomize %d.%d.%d is older than 5.8.3, whose messages these markers were measured on", v[0], v[1], v[2])
	}
	return path
}

// remoteBaseCases are the references the real tools are asked about: what a
// kustomize that was let through would be told to fetch.
func remoteBaseCases(t *testing.T) []struct{ name, resource string } {
	t.Helper()
	// A git repository of this machine, which a transport of `file` would read.
	local := t.TempDir()
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", local).CombinedOutput(); err != nil { //nolint:gosec // git on PATH, a fresh temp directory
		t.Skipf("git init: %v: %s", err, out)
	}
	return []struct{ name, resource string }{
		{"ssh", "git@unreachable.invalid:o/r"},
		{"https git", "https://unreachable.invalid/o/r.git"},
		{"https resource", "https://unreachable.invalid/m.yaml"},
		{"file", "file://" + filepath.ToSlash(local)},
	}
}

// The second barrier. DEVDESK-771 puts a gate in front of a native kustomize, so
// a run that reaches kustomize with a remote base is no longer what a scan does;
// this test starts kustomize itself, as it would be started if the gate had let
// the overlay through, with the environment of a cut network. With the real
// kustomize and git, a remote base fails at once, with a message that says the
// transport was refused, and never with a name lookup: nothing left the machine.
func TestARealKustomizeIsRefusedItsRemoteBases(t *testing.T) {
	kustomize := kustomizeWithGit(t)
	isolateHome(t)
	for _, c := range remoteBaseCases(t) {
		t.Run(c.name, func(t *testing.T) {
			repo := writeRepo(t, map[string]string{"overlay/kustomization.yaml": "resources:\n  - " + q(c.resource) + "\n"})
			cmd := exec.CommandContext(t.Context(), kustomize, "build", "./overlay") //nolint:gosec // the kustomize found on PATH, a fresh temp directory
			cmd.Dir = repo
			cmd.Env = offlineEnviron(os.Environ())

			start := time.Now()
			out, err := cmd.CombinedOutput()
			elapsed := time.Since(start)

			if err == nil {
				t.Fatalf("kustomize built an overlay whose base is %s:\n%s", c.resource, out)
			}
			if elapsed.Seconds() >= 5 {
				t.Errorf("took %s: something waited on the network", elapsed)
			}
			if strings.Contains(string(out), "lookup unreachable.invalid") {
				t.Errorf("the host was looked up, so the network was not cut:\n%s", out)
			}
			if !strings.Contains(string(out), "not allowed") && !strings.Contains(string(out), "proxyconnect tcp: dial tcp 127.0.0.1:0") {
				t.Errorf("no sign of a refused transport or of the dead proxy in:\n%s", out)
			}
			if !remoteBlocked(string(out), ToolSourceBinary) {
				t.Errorf("remoteBlocked does not recognise what the real kustomize printed:\n%s", out)
			}
		})
	}
}

// End to end, with the real kustomize: the gate refuses the overlay and kustomize
// is not launched, so the finding is the gate's own (it names the file, the
// field and the reference) and carries none of kustomize's words.
func TestARealKustomizeIsNeverLaunchedOnAnOverlayThatNamesARemoteBase(t *testing.T) {
	kustomize := kustomizeWithGit(t)
	isolateHome(t)
	for _, c := range remoteBaseCases(t) {
		t.Run(c.name, func(t *testing.T) {
			repo := writeRepo(t, map[string]string{"overlay/kustomization.yaml": "resources:\n  - " + q(c.resource) + "\n"})

			start := time.Now()
			report, err := RunKubeconform(t.Context(), repo, ToolSpec{Source: ToolSourceBinary, Binary: "kubeconform"},
				KubeconformOptions{Kustomize: ToolSpec{Source: ToolSourceBinary, Binary: kustomize}}, nil)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("RunKubeconform: %v", err)
			}
			if elapsed.Seconds() >= 5 {
				t.Errorf("took %s: something waited on the network", elapsed)
			}
			found := renderFindings(report)
			if len(found) != 1 {
				t.Fatalf("%d render findings, want 1: %+v", len(found), report.Findings)
			}
			f := found[0]
			if f.Title != remoteBlockedHeadline {
				t.Errorf("title = %q, want %q", f.Title, remoteBlockedHeadline)
			}
			for _, want := range []string{"overlay/kustomization.yaml", "resources[0]", c.resource} {
				if !strings.Contains(f.Message, fmt.Sprintf("%q", want)) {
					t.Errorf("the message does not name %q:\n%s", want, f.Message)
				}
			}
			// kustomize did not run: nothing of what it prints is in the message.
			for _, marker := range []string{"accumulating resources", "not allowed", "proxyconnect", "evalsymlink"} {
				if strings.Contains(f.Message, marker) {
					t.Errorf("the message holds %q, which only kustomize writes: it was launched\n%s", marker, f.Message)
				}
			}
			if f.Repro != "" {
				t.Errorf("repro = %q, want none", f.Repro)
			}
		})
	}
}

// countingServer answers 404 to everything and counts the requests, on addr (a
// listener kept open for the whole test: no port is read then closed). A test that
// cannot listen there is skipped.
func countingServer(t *testing.T, addr string) (hits *atomic.Int64, port int) {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("cannot listen on %s: %v", addr, err)
	}
	hits = new(atomic.Int64)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	_ = srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return hits, l.Addr().(*net.TCPAddr).Port
}

// DEVDESK-771, acceptance. Go never applies a proxy to the loopback, so a native
// kustomize that reads `resources: [http://127.0.0.1:PORT/x.yaml]` sends a GET to
// a service of this machine whatever offlineEnviron says. With the gate it sends
// nothing, for every spelling of the loopback; the witness run, with
// allow_remote_bases, shows the server would have seen a request.
func TestARealKustomizeNeverReachesAServiceOfTheLoopback(t *testing.T) {
	kustomize := kustomizeWithGit(t)
	isolateHome(t)
	// The witness run inherits this process's environment: a proxy of the host
	// would take a name Go does not treat as the loopback (x.localhost).
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, "")
	}
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")

	tests := []struct {
		name   string
		listen string // where the server is
		host   string // what the overlay names
		skip   func(t *testing.T)
	}{
		{name: "127.0.0.1", listen: "127.0.0.1:0", host: "127.0.0.1"},
		{name: "localhost", listen: "127.0.0.1:0", host: "localhost"},
		{name: "[::1]", listen: "[::1]:0", host: "[::1]"},
		{name: "127.0.0.2", listen: "127.0.0.2:0", host: "127.0.0.2"},
		{name: "a name under .localhost", listen: "127.0.0.1:0", host: "x.localhost", skip: func(t *testing.T) {
			addrs, err := net.LookupHost("x.localhost")
			if err != nil || !slices.ContainsFunc(addrs, func(a string) bool { ip := net.ParseIP(a); return ip != nil && ip.IsLoopback() }) {
				t.Skipf("x.localhost does not resolve to the loopback here: %v %v", addrs, err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != nil {
				tt.skip(t)
			}
			hits, port := countingServer(t, tt.listen)
			url := "http://" + tt.host + ":" + strconv.Itoa(port) + "/m.yaml"
			repo := writeRepo(t, map[string]string{"overlay/kustomization.yaml": "resources:\n  - " + q(url) + "\n"})
			run := func(allow bool) *KubeconformReport {
				report, err := RunKubeconform(t.Context(), repo, ToolSpec{Source: ToolSourceBinary, Binary: "kubeconform"},
					KubeconformOptions{Kustomize: ToolSpec{Source: ToolSourceBinary, Binary: kustomize}, AllowRemoteBases: allow}, nil)
				if err != nil {
					t.Fatalf("RunKubeconform: %v", err)
				}
				return report
			}

			blocked := run(false)

			if n := hits.Load(); n != 0 {
				t.Errorf("the server of %s got %d request(s) with the network cut, want 0", url, n)
			}
			found := renderFindings(blocked)
			if len(found) != 1 || found[0].Title != remoteBlockedHeadline || !strings.Contains(found[0].Message, fmt.Sprintf("%q", url)) {
				t.Errorf("findings = %+v, want one %q that names %q", found, remoteBlockedHeadline, url)
			}

			// The witness: the same overlay, allowed, does reach the server. Without it a
			// run of 0 would prove nothing (a server nobody can reach looks the same).
			run(true)
			if hits.Load() < 1 {
				t.Fatalf("with allow_remote_bases kustomize did not reach %s either: this test cannot see a request here", url)
			}
		})
	}
}
