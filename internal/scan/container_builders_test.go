package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/trust"
)

// Characterisation of every container launch (DEVDESK-583, AC4): with no proxy
// variable set, the argv and the environment each builder produces are exactly
// what they were before the proxy was forwarded. Each line below is frozen on
// purpose; a change here is a change of what DevDesk runs.

// proxyVariables are the six names the container launches may forward.
var proxyVariables = []string{
	"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy",
}

// noProxyEnvironment removes the six variables from the process, restoring
// them when the test ends, so the result does not depend on the machine.
func noProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range proxyVariables {
		t.Setenv(name, "") // registers the restore
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

// useEngine makes the given shape the current engine for one test.
func useEngine(t *testing.T, shape engine.Shape) {
	t.Helper()
	previous := engine.Current()
	engine.SetCurrent(shape)
	t.Cleanup(func() { engine.SetCurrent(previous) })
}

const characterizationDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// builderCase is one launch: how to build it, and the argv after the engine
// binary. Args are written as one string split on spaces; no value here
// contains a space.
type builderCase struct {
	name  string
	build func() toolCmd
	args  string
	// env says the child's environment is os.Environ() plus these entries;
	// empty means the builder leaves Env nil.
	env []string
	// stdin is what the builder feeds the process, when it does.
	stdin string
}

func containerBuilders() []builderCase {
	tool := ToolSpec{Source: ToolSourceContainer}
	withConfig := ToolSpec{Source: ToolSourceContainer, Config: "/home/u/trivy.yaml", Args: []string{"--timeout", "5m"}}
	withSocket := ToolSpec{Source: ToolSourceContainer, HostSocket: "/run/user/1000/docker.sock"}
	custom := ToolSpec{Source: ToolSourceContainer, Image: "registry.example.com/tools/img:1"}
	creds := func(string) (string, string) { return "u", "p" }
	ref := "registry.example.com/team/app@" + characterizationDigest
	local := "localhost:5000/app@" + characterizationDigest

	cosign := func(spec ToolSpec, c func(string) (string, string)) CosignVerifier {
		return CosignVerifier{Tool: spec, Creds: c}
	}
	trivy := func(cmd toolCmd, err error) toolCmd {
		if err != nil {
			panic(err)
		}
		return cmd
	}
	credEnv := []string{"COSIGN_REGISTRY_USERNAME=u", "COSIGN_REGISTRY_PASSWORD=p"}

	return []builderCase{
		// trivy_args.go
		{name: "trivy fs directory",
			build: func() toolCmd {
				return trivy(trivyArgs("/repo", TargetDirectory, false, tool, "", false, false))
			},
			args: "run --rm -v /repo:/scan:ro aquasec/trivy fs --format json --scanners vuln /scan"},
		{name: "trivy fs license with config, user args, server and filters",
			build: func() toolCmd {
				return trivy(trivyArgs("/repo", TargetDirectory, true, withConfig, "http://trivy:4954", true, true))
			},
			args: "run --rm -v /home/u/trivy.yaml:/trivy.yaml:ro -v /repo:/scan:ro aquasec/trivy " +
				"fs --config /trivy.yaml --timeout 5m --format json --scanners license " +
				"--server http://trivy:4954 --ignore-unfixed --ignore-status end_of_life /scan"},
		{name: "trivy image with the engine socket",
			build: func() toolCmd {
				return trivy(trivyArgs("img:1", TargetImage, false, withSocket, "", false, false))
			},
			args: "run --rm -v /run/user/1000/docker.sock:/var/run/docker.sock:ro aquasec/trivy " +
				"image --format json --scanners vuln img:1"},
		{name: "trivy image through a server mounts no socket",
			build: func() toolCmd {
				return trivy(trivyArgs("img:1", TargetImage, false, withSocket, "http://trivy:4954", false, false))
			},
			args: "run --rm aquasec/trivy image --format json --scanners vuln --server http://trivy:4954 img:1"},
		{name: "trivy remote image",
			build: func() toolCmd {
				return trivy(trivyRemoteImageArgs("registry.example.com/app:1", withSocket, "", true, true))
			},
			args: "run --rm aquasec/trivy image --format json --scanners vuln --image-src remote " +
				"--ignore-unfixed --ignore-status end_of_life registry.example.com/app:1"},
		{name: "trivy secret directory",
			build: func() toolCmd {
				return trivy(trivySecretArgs("/repo", TargetDirectory, custom, ""))
			},
			args: "run --rm -v /repo:/scan:ro registry.example.com/tools/img:1 fs --format json --scanners secret /scan"},
		{name: "trivy secret image",
			build: func() toolCmd {
				return trivy(trivySecretArgs("img:1", TargetImage, withSocket, ""))
			},
			args: "run --rm -v /run/user/1000/docker.sock:/var/run/docker.sock:ro aquasec/trivy " +
				"image --format json --scanners secret img:1"},
		{name: "trivy misconfig directory",
			build: func() toolCmd {
				return trivy(trivyMisconfigArgs("/repo", TargetDirectory, tool, "", true))
			},
			args: "run --rm -v /repo:/scan:ro aquasec/trivy fs --format json --scanners misconfig " +
				"--ignore-status end_of_life /scan"},
		{name: "trivy misconfig image",
			build: func() toolCmd {
				return trivy(trivyMisconfigArgs("img:1", TargetImage, withSocket, "", false))
			},
			args: "run --rm -v /run/user/1000/docker.sock:/var/run/docker.sock:ro aquasec/trivy " +
				"image --format json --scanners misconfig img:1"},

		// gitleaks.go
		{name: "gitleaks without config",
			build: func() toolCmd { return gitleaksArgs("/repo", tool, false, "", "") },
			args: "run --rm -v /repo:/scan:ro zricethezav/gitleaks detect --source /scan " +
				"--gitleaks-ignore-path /scan --report-format json --report-path /dev/fd/1 --no-git"},
		{name: "gitleaks with config, history and user args",
			build: func() toolCmd {
				return gitleaksArgs("/repo", ToolSpec{Source: ToolSourceContainer, Args: []string{"--verbose"}},
					true, "/home/u/gitleaks.toml", "")
			},
			args: "run --rm -v /repo:/scan:ro -v /home/u/gitleaks.toml:/gitleaks.toml:ro zricethezav/gitleaks " +
				"detect --verbose --source /scan --gitleaks-ignore-path /scan --report-format json " +
				"--report-path /dev/fd/1 --config /gitleaks.toml"},

		// cosign.go
		{name: "cosign verify anonymous",
			build: func() toolCmd {
				return cosign(tool, nil).verifyCmd(ref, []string{"--key", "k"}, "")
			},
			args: "run --rm " + DefaultCosignImage + " verify " + ref + " --key k --experimental-oci11"},
		{name: "cosign verify with credentials",
			build: func() toolCmd {
				return cosign(tool, creds).verifyCmd(ref, keylessArgs(trust.Rule{Issuer: "https://issuer.example.com", Subject: "https://example.com/id"}), "")
			},
			args: "run --rm -e COSIGN_REGISTRY_USERNAME -e COSIGN_REGISTRY_PASSWORD " + DefaultCosignImage +
				" verify " + ref + " --certificate-oidc-issuer https://issuer.example.com " +
				"--certificate-identity https://example.com/id --experimental-oci11",
			env: credEnv},
		{name: "cosign verify with a key file and credentials",
			build: func() toolCmd {
				return cosign(tool, creds).verifyCmd(ref, []string{"--key", "/tmp/k.pub"}, "/tmp/k.pub")
			},
			args: "run --rm -e COSIGN_REGISTRY_USERNAME -e COSIGN_REGISTRY_PASSWORD " +
				"-v /tmp/k.pub:/devdesk-cosign.pub:ro " + DefaultCosignImage +
				" verify " + ref + " --key /devdesk-cosign.pub --experimental-oci11",
			env: credEnv},
		{name: "cosign verify against a local registry",
			build: func() toolCmd {
				return cosign(custom, nil).verifyCmd(local, nil, "")
			},
			args: "run --rm registry.example.com/tools/img:1 verify " + local +
				" --experimental-oci11 --allow-http-registry"},
		{name: "cosign download anonymous",
			build: func() toolCmd { return cosign(tool, nil).downloadCmd(ref) },
			args:  "run --rm " + DefaultCosignImage + " download attestation " + ref},
		{name: "cosign download with credentials",
			build: func() toolCmd { return cosign(tool, creds).downloadCmd(ref) },
			args: "run --rm -e COSIGN_REGISTRY_USERNAME -e COSIGN_REGISTRY_PASSWORD " + DefaultCosignImage +
				" download attestation " + ref,
			env: credEnv},

		// plumber.go
		{name: "plumber without token or config",
			build: func() toolCmd { return plumberArgs("/repo", tool, PlumberOptions{}, "/out.json") },
			args: "run --rm -v /repo:/scan:ro -e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory " +
				"-e GIT_CONFIG_VALUE_0=/scan -w /scan getplumber/plumber analyze --score --print=false --output /out.json"},
		{name: "plumber with token, config and options",
			build: func() toolCmd {
				return plumberArgs("/repo", tool, PlumberOptions{
					Provider: providerGitLab, Branch: "main", GitLabURL: "https://gitlab.example.com",
					ConfigPath: "/home/u/plumber.yaml", Token: "t0k3n",
				}, "/out.json")
			},
			args: "run --rm -v /repo:/scan:ro -v /home/u/plumber.yaml:/plumber.yaml:ro " +
				"-e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0=/scan " +
				"-e GITLAB_TOKEN=t0k3n -w /scan getplumber/plumber analyze --score --print=false --output /out.json " +
				"--provider " + providerGitLab + " --branch main --gitlab-url https://gitlab.example.com " +
				"--config /plumber.yaml"},

		// kubeconform.go
		{name: "kubeconform without cache",
			build: func() toolCmd {
				return kubeconformArgs("/repo", []string{"k8s/a.yaml", "k8s/b.yaml"}, tool, KubeconformOptions{})
			},
			args: "run --rm -v /repo:/scan:ro -w /scan ghcr.io/yannh/kubeconform -output json -strict k8s/a.yaml k8s/b.yaml"},
		{name: "kubeconform with cache and version",
			build: func() toolCmd {
				return kubeconformArgs("/repo", []string{"k8s/a.yaml"}, tool,
					KubeconformOptions{CacheDir: "/home/u/cache", KubernetesVersion: "1.30.0"})
			},
			args: "run --rm -v /repo:/scan:ro -v /home/u/cache:/cache -w /scan ghcr.io/yannh/kubeconform " +
				"-output json -strict -kubernetes-version 1.30.0 -cache /cache k8s/a.yaml"},

		// kubeconform_render.go
		{name: "kubeconform stdin without cache",
			build: func() toolCmd { return kubeconformStdinArgs([]byte("rendered"), tool, KubeconformOptions{}) },
			args:  "run --rm -i ghcr.io/yannh/kubeconform -output json -strict -",
			stdin: "rendered"},
		{name: "kubeconform stdin with cache",
			build: func() toolCmd {
				return kubeconformStdinArgs([]byte("rendered"), tool, KubeconformOptions{CacheDir: "/home/u/cache"})
			},
			args: "run --rm -i -v /home/u/cache:/cache ghcr.io/yannh/kubeconform " +
				"-output json -strict -cache /cache -",
			stdin: "rendered"},
		{name: "helm template",
			build: func() toolCmd { return helmArgs(targetOnly("/repo"), "charts/app", tool, "template") },
			args:  "run --rm -v /repo:/scan:ro -w /scan alpine/helm template charts/app"},
		{name: "helm lint with user args",
			build: func() toolCmd {
				return helmArgs(targetOnly("/repo"), "charts/app", ToolSpec{Source: ToolSourceContainer, Args: []string{"--set", "a=b"}}, "lint")
			},
			args: "run --rm -v /repo:/scan:ro -w /scan alpine/helm lint --set a=b charts/app"},
		{name: "kustomize build",
			build: func() toolCmd { return kustomizeArgs(targetOnly("/repo"), "overlays/prod", tool) },
			args: "run --rm -v /repo:/scan:ro -w /scan registry.k8s.io/kustomize/kustomize:v5.8.1 " +
				"build overlays/prod"},

		// tool_source.go: the version probe (containerVersionCmd, DEVDESK-583)
		{name: "tool version",
			build: func() toolCmd { return containerVersionCmd("aquasec/trivy", "--version") },
			args:  "run --rm aquasec/trivy --version"},
	}
}

func TestContainerBuildersWithoutProxyAreUnchanged(t *testing.T) {
	noProxyEnvironment(t)
	for _, name := range []string{engine.Docker, engine.Podman} {
		t.Run(name, func(t *testing.T) {
			useEngine(t, engine.ShapeFor(name))
			for _, tc := range containerBuilders() {
				t.Run(tc.name, func(t *testing.T) {
					got := tc.build()
					if got.Name != name {
						t.Errorf("Name = %q, want %q", got.Name, name)
					}
					if want := strings.Fields(tc.args); !slices.Equal(got.Args, want) {
						t.Errorf("Args =\n  %q\nwant\n  %q", got.Args, want)
					}
					if len(tc.env) == 0 {
						if got.Env != nil {
							t.Errorf("Env = %q, want nil", got.Env)
						}
					} else if want := append(os.Environ(), tc.env...); !slices.Equal(got.Env, want) {
						t.Errorf("Env is not os.Environ() plus the credentials:\n  %q\nwant\n  %q", got.Env, want)
					}
					for _, e := range got.Env {
						for _, v := range proxyVariables {
							if strings.HasPrefix(e, v+"=") {
								t.Errorf("Env carries %s although none is set", v)
							}
						}
					}
					if got.Dir != "" {
						t.Errorf("Dir = %q, want none for a container", got.Dir)
					}
					if string(got.Stdin) != tc.stdin {
						t.Errorf("Stdin = %q, want %q", got.Stdin, tc.stdin)
					}
				})
			}
		})
	}
}

// TestContainerBuildersCoverEveryLauncher checks the table against itself: each
// case is named once, and the table keeps a case for every launcher family
// listed below. It does not find a launcher added elsewhere in the code, since
// the list is a literal in this file; that guard is
// internal/netproxy/container_guard_test.go (DEVDESK-583, step 4).
func TestContainerBuildersCoverEveryLauncher(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range containerBuilders() {
		if seen[tc.name] {
			t.Errorf("duplicate case %q", tc.name)
		}
		seen[tc.name] = true
	}
	for _, prefix := range []string{"trivy fs", "trivy image", "trivy remote", "trivy secret", "trivy misconfig",
		"gitleaks", "cosign verify", "cosign download", "plumber", "kubeconform ", "kubeconform stdin",
		"helm", "kustomize", "tool version"} {
		found := false
		for name := range seen {
			if strings.HasPrefix(name, prefix) {
				found = true
			}
		}
		if !found {
			t.Errorf("no container builder case starts with %q", prefix)
		}
	}
}

// TestContainerToolVersionCharacterization runs containerToolVersion through
// the engine binary the shape names, which is the one existing injection
// point: a script records its argv and prints it back.
func TestContainerToolVersionCharacterization(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake engine is a shell script")
	}
	noProxyEnvironment(t)
	script := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\n"), 0o755); err != nil { //nolint:gosec // a test script must be executable
		t.Fatal(err)
	}
	shape := engine.ShapeFor(engine.Docker)
	shape.Binary = script
	useEngine(t, shape)

	got := containerToolVersion("aquasec/trivy", "--version")
	want := "docker:run\n--rm\naquasec/trivy\n--version\n"
	if got != want {
		t.Errorf("containerToolVersion = %q, want %q", got, want)
	}
}
