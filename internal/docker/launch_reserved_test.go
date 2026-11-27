package docker

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-15 (DEVDESK-813): the names typed in the launch form also drive the engine
// CLI, because the child's environment is DevDesk's own plus the typed entries
// (OC-14). `DOCKER_HOST=tcp://other:2375` would send `docker run` to another
// daemon. A reserved name is refused by the one rule the form and the launcher
// share, whatever the case, before anything is started.
//
// Expected production API, pinned by these tests:
//
//	func ReservedEnvName(name string) bool
//	    reports whether the engine client or the loader reads that name: an exact
//	    name, a prefix (`LD_`, `DYLD_`) or the suffix `_PROXY`, case-insensitive.
//	var ErrReservedLaunchEnv error
//	    returned by LaunchContainer and BuildLaunchCmd for a reserved name; it also
//	    satisfies errors.Is(err, ErrInvalidLaunchEnv). Its text is fixed: it holds
//	    neither the name nor the value (a name field can hold a pasted secret).

var reservedNameCases = []struct {
	name  string
	input string
	want  bool
}{
	// Exact names the docker client reads.
	{"DOCKER_HOST", "DOCKER_HOST", true},
	{"DOCKER_CONFIG", "DOCKER_CONFIG", true},
	{"DOCKER_CONTEXT", "DOCKER_CONTEXT", true},
	{"DOCKER_TLS_VERIFY", "DOCKER_TLS_VERIFY", true},
	{"DOCKER_CERT_PATH", "DOCKER_CERT_PATH", true},
	{"DOCKER_API_VERSION", "DOCKER_API_VERSION", true},
	// Exact names added after the first list: docker TLS and headers, podman SSH
	// key, connections file, last-loaded config, and rootless data directory.
	{"DOCKER_TLS", "DOCKER_TLS", true},
	{"DOCKER_CUSTOM_HEADERS", "DOCKER_CUSTOM_HEADERS", true},
	{"CONTAINER_SSHKEY", "CONTAINER_SSHKEY", true},
	{"PODMAN_CONNECTIONS_CONF", "PODMAN_CONNECTIONS_CONF", true},
	{"CONTAINERS_CONF_OVERRIDE", "CONTAINERS_CONF_OVERRIDE", true},
	{"XDG_DATA_HOME", "XDG_DATA_HOME", true},
	{"lower case docker_tls", "docker_tls", true},
	{"mixed case Container_SshKey", "Container_SshKey", true},
	{"DOCKER_TLS with a suffix", "DOCKER_TLS_X", false},
	{"DOCKER_CUSTOM_HEADERS_X", "DOCKER_CUSTOM_HEADERS_X", false},
	{"PODMAN_CONNECTIONS", "PODMAN_CONNECTIONS", false},
	{"CONTAINER_SSHKEYS", "CONTAINER_SSHKEYS", false},
	{"XDG_DATA_DIRS", "XDG_DATA_DIRS", false},
	// Third list: Windows, storage, ssh/TLS/Go, telemetry and trust, directories.
	{"USERPROFILE", "USERPROFILE", true},
	{"APPDATA", "APPDATA", true},
	{"LOCALAPPDATA", "LOCALAPPDATA", true},
	{"PATHEXT", "PATHEXT", true},
	{"SYSTEMROOT", "SYSTEMROOT", true},
	{"WINDIR", "WINDIR", true},
	{"COMSPEC", "COMSPEC", true},
	{"STORAGE_OPTS", "STORAGE_OPTS", true},
	{"STORAGE_DRIVER", "STORAGE_DRIVER", true},
	{"CONTAINERS_HELPER_BINARY_DIR", "CONTAINERS_HELPER_BINARY_DIR", true},
	{"CONTAINER_PASSPHRASE", "CONTAINER_PASSPHRASE", true},
	{"SSH_ASKPASS", "SSH_ASKPASS", true},
	{"SSH_ASKPASS_REQUIRE", "SSH_ASKPASS_REQUIRE", true},
	{"GODEBUG", "GODEBUG", true},
	{"SSL_CERT_FILE", "SSL_CERT_FILE", true},
	{"SSL_CERT_DIR", "SSL_CERT_DIR", true},
	{"DOCKER_CLI_OTEL_EXPORTER_OTLP_ENDPOINT", "DOCKER_CLI_OTEL_EXPORTER_OTLP_ENDPOINT", true},
	{"DOCKER_CONTENT_TRUST", "DOCKER_CONTENT_TRUST", true},
	{"DOCKER_CONTENT_TRUST_SERVER", "DOCKER_CONTENT_TRUST_SERVER", true},
	{"NOTIFY_SOCKET", "NOTIFY_SOCKET", true},
	{"TMPDIR", "TMPDIR", true},
	{"DOCKER_DEFAULT_PLATFORM", "DOCKER_DEFAULT_PLATFORM", true},
	{"DOCKER_AUTH_CONFIG", "DOCKER_AUTH_CONFIG", true},
	{"XDG_CACHE_HOME", "XDG_CACHE_HOME", true},
	{"XDG_STATE_HOME", "XDG_STATE_HOME", true},
	{"lower case userprofile", "userprofile", true},
	{"mixed case Ssl_Cert_File", "Ssl_Cert_File", true},
	{"the OTEL_ prefix", "OTEL_EXPORTER_OTLP_ENDPOINT", true},
	{"the OTEL_ prefix, lower case", "otel_sdk_disabled", true},
	{"the _CONTAINERS_ prefix", "_CONTAINERS_USERNS_CONFIGURED", true},
	{"the _CONTAINERS_ prefix, lower case", "_containers_x", true},
	{"TMPDIRS", "TMPDIRS", false},
	{"SSH_ASKPASS_X", "SSH_ASKPASS_X", false},
	{"OTELX", "OTELX", false},
	{"MY_OTEL_X", "MY_OTEL_X", false},
	{"CONTAINERS_X", "CONTAINERS_X", false},
	{"_CONTAINERSX", "_CONTAINERSX", false},
	{"STORAGE_X", "STORAGE_X", false},
	{"GODEBUGX", "GODEBUGX", false},
	{"WINDIRS", "WINDIRS", false},
	{"USER", "USER", false},
	{"DOCKER_BUILDKIT is not reserved", "DOCKER_BUILDKIT", false},
	{"BUILDKIT_PROGRESS is not reserved", "BUILDKIT_PROGRESS", false},
	{"COMPOSE_PROJECT_NAME is not reserved", "COMPOSE_PROJECT_NAME", false},
	{"GIT_SSH_COMMAND is not reserved", "GIT_SSH_COMMAND", false},
	{"LANG is not reserved", "LANG", false},
	{"LC_ALL is not reserved", "LC_ALL", false},
	// Exact names the podman client reads.
	{"CONTAINER_HOST", "CONTAINER_HOST", true},
	{"CONTAINER_CONNECTION", "CONTAINER_CONNECTION", true},
	{"CONTAINERS_CONF", "CONTAINERS_CONF", true},
	{"CONTAINERS_STORAGE_CONF", "CONTAINERS_STORAGE_CONF", true},
	{"CONTAINERS_REGISTRIES_CONF", "CONTAINERS_REGISTRIES_CONF", true},
	{"REGISTRY_AUTH_FILE", "REGISTRY_AUTH_FILE", true},
	{"XDG_RUNTIME_DIR", "XDG_RUNTIME_DIR", true},
	{"XDG_CONFIG_HOME", "XDG_CONFIG_HOME", true},
	// Exact names both clients, ssh and the loader resolve things through.
	{"HOME", "HOME", true},
	{"PATH", "PATH", true},
	{"SSH_AUTH_SOCK", "SSH_AUTH_SOCK", true},
	// Case does not matter.
	{"mixed case DOCKER_HOST", "Docker_Host", true},
	{"lower case docker_host", "docker_host", true},
	{"lower case path", "path", true},
	{"lower case home", "home", true},
	{"lower case container_host", "container_host", true},
	// Prefixes.
	{"LD_PRELOAD", "LD_PRELOAD", true},
	{"LD_LIBRARY_PATH", "LD_LIBRARY_PATH", true},
	{"lower case ld_preload", "ld_preload", true},
	{"the prefix alone", "LD_", true},
	{"DYLD_INSERT_LIBRARIES", "DYLD_INSERT_LIBRARIES", true},
	{"lower case dyld_library_path", "dyld_library_path", true},
	// The suffix _PROXY.
	{"HTTP_PROXY", "HTTP_PROXY", true},
	{"lower case http_proxy", "http_proxy", true},
	{"HTTPS_PROXY", "HTTPS_PROXY", true},
	{"ALL_PROXY", "ALL_PROXY", true},
	{"NO_PROXY", "NO_PROXY", true},
	{"any name ending in _PROXY", "MY_PROXY", true},
	{"mixed case suffix", "My_Proxy", true},
	{"the suffix alone", "_PROXY", true},
	// Accepted: an exact match outside the list, a longer name, a lookalike.
	{"DOCKER_HOSTNAME", "DOCKER_HOSTNAME", false},
	{"DOCKER_HOST with a digit", "DOCKER_HOST2", false},
	{"DOCKER_TLS_VERIFYX", "DOCKER_TLS_VERIFYX", false},
	{"MY_PATH", "MY_PATH", false},
	{"PATHS", "PATHS", false},
	{"PATH with a prefix", "XPATH", false},
	{"HOMEPAGE", "HOMEPAGE", false},
	{"MY_HOME", "MY_HOME", false},
	{"a CONTAINERS name that is not listed", "CONTAINERS_X_Y", false},
	{"CONTAINER_NAME", "CONTAINER_NAME", false},
	{"LD without the underscore", "LD", false},
	{"LDAP_URL", "LDAP_URL", false},
	{"DYLD without the underscore", "DYLD", false},
	{"DYLDX", "DYLDX", false},
	{"PROXY alone", "PROXY", false},
	{"a name that only starts with PROXY", "PROXY_URL", false},
	{"a suffix followed by a letter", "MY_PROXYX", false},
	{"a dash is not the suffix", "MY-PROXY", false},
	{"API_TOKEN", "API_TOKEN", false},
	{"empty", "", false},
}

func TestReservedEnvName(t *testing.T) {
	for _, tt := range reservedNameCases {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReservedEnvName(tt.input); got != tt.want {
				t.Errorf("ReservedEnvName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// Every reserved name is a syntactically valid one: the reserved rule is a
// second refusal on top of ValidEnvName, not a replacement for it.
func TestEveryReservedExampleIsAValidName(t *testing.T) {
	for _, tt := range reservedNameCases {
		if tt.want && !ValidEnvName(tt.input) {
			t.Errorf("%q is reserved but not a valid name: the two rules would overlap", tt.input)
		}
	}
}

// The refusal: both entry points, both engines, before anything runs. The
// engine stub is strict, so any call fails the test with its argv. A reserved
// name among good entries refuses the whole launch.
func TestAReservedNameIsRefusedBeforeAnythingRuns(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, tt := range reservedNameCases {
			if !tt.want {
				continue
			}
			entry := tt.input + "=tcp://other:2375"
			t.Run(tt.name, func(t *testing.T) {
				s := stubStrict(t) // any engine call fails the test

				id, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Env: []string{"REGION=eu", entry}})
				if !errors.Is(err, ErrReservedLaunchEnv) || !errors.Is(err, ErrInvalidLaunchEnv) || id != "" {
					t.Errorf("LaunchContainer() = %q, %v, want ErrReservedLaunchEnv (an ErrInvalidLaunchEnv) and no container", id, err)
				}
				cmd, err := BuildLaunchCmd(ContainerLaunchOptions{Image: "img", Env: []string{entry}, Interactive: true, TTY: true})
				if !errors.Is(err, ErrReservedLaunchEnv) || !errors.Is(err, ErrInvalidLaunchEnv) || cmd != nil {
					t.Errorf("BuildLaunchCmd() = %v, %v, want ErrReservedLaunchEnv and no command", cmd, err)
				}
				if len(s.calls) != 0 {
					t.Errorf("the engine was reached for a refused launch: %v", s.calls)
				}
				if err != nil {
					// A name field can hold a pasted secret: the text is fixed.
					for _, leak := range []string{tt.input, "tcp://other", "2375", "="} {
						if strings.Contains(err.Error(), leak) {
							t.Errorf("error %q repeats %q", err, leak)
						}
					}
				}
			})
		}
	})
}

// The refusal is the same for a reserved name with an empty value (`HOME=`): the
// launcher accepts `NAME=` for an ordinary name, not for a reserved one.
func TestAReservedNameWithAnEmptyValueIsRefusedToo(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		s := stubStrict(t)

		_, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Env: []string{"HOME="}})
		if !errors.Is(err, ErrReservedLaunchEnv) {
			t.Errorf("LaunchContainer() error = %v, want ErrReservedLaunchEnv", err)
		}
		if len(s.calls) != 0 {
			t.Errorf("the engine was reached: %v", s.calls)
		}
	})
}

// What is not reserved still launches, on both entry points: the rule is an
// exact match (outside the prefixes and the suffix), so DOCKER_HOSTNAME and
// MY_PATH go through with their value in the child's environment.
func TestANameThatLooksReservedButIsNotIsLaunched(t *testing.T) {
	accepted := []string{"DOCKER_HOSTNAME", "MY_PATH", "PATHS", "HOMEPAGE", "CONTAINERS_X_Y", "LDAP_URL", "PROXY", "MY_PROXYX"}
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, name := range accepted {
			t.Run(name, func(t *testing.T) {
				argv := wantEnvArgv([]string{name})
				s := stubStrict(t, reply{argv: argv, out: "9f2e1c8b7a6d\n"})

				id, err := LaunchContainer(ContainerLaunchOptions{Image: "img", Env: []string{name + "=value"}})
				if err != nil || id != "9f2e1c8b7a6d" {
					t.Fatalf("LaunchContainer() = %q, %v, want the container", id, err)
				}
				cmd, err := BuildLaunchCmd(ContainerLaunchOptions{Image: "img", Env: []string{name + "=value"}})
				if err != nil {
					t.Fatalf("BuildLaunchCmd() error = %v", err)
				}
				if got := envMap(cmd.Env)[name]; got != "value" {
					t.Errorf("cmd.Env %s = %q, want the typed value", name, got)
				}
				if len(s.calls) != 2 {
					t.Errorf("engine calls = %d, want 2 (one launch, one build)", len(s.calls))
				}
			})
		}
	})
}

// Criterion 3: for any input, what BuildLaunchCmd adds to DevDesk's own
// environment holds no reserved name. The seeds run in `go test`; the fuzz
// target explores more with `go test -fuzz=FuzzLaunchEnvNeverCarriesAReservedName`.
func FuzzLaunchEnvNeverCarriesAReservedName(f *testing.F) {
	for _, tt := range reservedNameCases {
		f.Add(tt.input + "=x")
	}
	for _, seed := range []string{"A=b", "DOCKER_HOST=", "=DOCKER_HOST", "x=DOCKER_HOST=y", "DOCKER_HOST", "Docker_Host=1", " DOCKER_HOST=1", "DOCKER_HOST\x00=1", "DKER=1", "docker_hoſt=1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, entry string) {
		stub(t, &stubRunner{}) // a refused launch never builds; an accepted one only builds
		base := os.Environ()

		cmd, err := BuildLaunchCmd(ContainerLaunchOptions{Image: "img", Env: []string{entry}})
		// The refusal must not rely on the filtering of launchEnviron and
		// buildLaunchArgs: a well-formed entry with a reserved name is refused.
		if name, _, ok := strings.Cut(entry, "="); ok && ValidEnvName(name) && ReservedEnvName(name) {
			if !errors.Is(err, ErrReservedLaunchEnv) || cmd != nil {
				t.Errorf("entry %q has the reserved name %q: BuildLaunchCmd() = %v, %v, want ErrReservedLaunchEnv and no command", entry, name, cmd, err)
			}
		}
		if err != nil {
			if cmd != nil {
				t.Errorf("a refused launch built a command")
			}
			return
		}
		for i, arg := range cmd.Args {
			if arg == "-e" && i+1 < len(cmd.Args) && ReservedEnvName(cmd.Args[i+1]) {
				t.Errorf("entry %q put -e %s in argv", entry, cmd.Args[i+1])
			}
		}
		if len(cmd.Env) < len(base) {
			t.Fatalf("cmd.Env is shorter than DevDesk's environment")
		}
		for _, e := range cmd.Env[len(base):] {
			name, _, _ := strings.Cut(e, "=")
			if ReservedEnvName(name) {
				t.Errorf("entry %q reached cmd.Env with the reserved name %q", entry, name)
			}
		}
	})
}

// The two builders hold the line on their own, even when a caller skips the
// validation: a reserved entry is neither named in argv nor put in the child's
// environment.
func TestTheBuildersDropAReservedEntryEvenWithoutTheValidation(t *testing.T) {
	opts := ContainerLaunchOptions{Image: "img", Env: []string{"DOCKER_HOST=tcp://other:2375", "REGION=eu", "http_proxy=x"}}

	args := buildLaunchArgs(opts)
	if want := wantEnvArgv([]string{"REGION"}); !slices.Equal(args, want) {
		t.Errorf("buildLaunchArgs() = %q, want %q", args, want)
	}
	for _, e := range launchEnviron(opts.Env)[len(os.Environ()):] {
		if name, _, _ := strings.Cut(e, "="); ReservedEnvName(name) {
			t.Errorf("launchEnviron() appended the reserved entry for %q", name)
		}
	}
}
