package docker

import "strings"

// OC-15 (DEVDESK-813): reserved environment names. The child of a launch is the
// engine CLI, and its environment is DevDesk's own plus the entries typed in the
// launch form (launchEnviron). A typed name therefore drives the CLI as well as
// the container: `DOCKER_HOST=tcp://other:2375` would send `docker run` to
// another daemon. A name the client, ssh or the dynamic loader reads is refused,
// by this one rule for the form and the launcher.
//
// The rule is a deny list, not a proof: a name outside it that drives some
// exotic client setting is not seen (docs/architecture/network.md, "A launch
// passes environment values to the child").

// reservedEnvNames are the exact names, upper case; the match ignores case.
// Each one is read by the engine client or by what it starts:
var reservedEnvNames = map[string]struct{}{
	// docker CLI: the daemon to reach, its client configuration directory, the
	// selected context, TLS and the pinned API version.
	"DOCKER_HOST":        {},
	"DOCKER_CONFIG":      {},
	"DOCKER_CONTEXT":     {},
	"DOCKER_TLS_VERIFY":  {},
	"DOCKER_CERT_PATH":   {},
	"DOCKER_API_VERSION": {},
	// podman: the remote service (and remote mode), the three configuration
	// files it loads, and the registry credentials file it reads and writes.
	"CONTAINER_HOST":             {},
	"CONTAINER_CONNECTION":       {},
	"CONTAINERS_CONF":            {},
	"CONTAINERS_STORAGE_CONF":    {},
	"CONTAINERS_REGISTRIES_CONF": {},
	"REGISTRY_AUTH_FILE":         {},
	// Rootless podman and the docker CLI resolve sockets and configuration
	// from these.
	"XDG_RUNTIME_DIR": {},
	"XDG_CONFIG_HOME": {},
	// ~/.docker, ~/.config/containers and ~/.ssh hang off HOME; PATH decides
	// which helper, credential helper or ssh the client starts; SSH_AUTH_SOCK is
	// the agent used by `ssh://` hosts.
	"HOME":          {},
	"PATH":          {},
	"SSH_AUTH_SOCK": {},
}

// reservedEnvPrefixes are the prefixes (upper case) of the dynamic loader
// variables (`LD_PRELOAD`, `LD_LIBRARY_PATH`, `DYLD_INSERT_LIBRARIES`): they run
// code of the typed library inside the engine CLI.
var reservedEnvPrefixes = []string{"LD_", "DYLD_"}

// reservedEnvSuffix is the suffix of the proxy variables (`HTTP_PROXY`,
// `NO_PROXY`, `MY_PROXY`): the client's own connections would go through the
// proxy typed, and Go also reads them for the registry.
const reservedEnvSuffix = "_PROXY"

// ReservedEnvName reports whether name is one the launch form may not set: an
// exact name of reservedEnvNames, a name starting with `LD_` or `DYLD_`, or a
// name ending in `_PROXY`, whatever the case. `DOCKER_HOSTNAME`, `MY_PATH`,
// `PATHS`, `HOMEPAGE`, `PROXY` and `MY_PROXYX` are not reserved. The launch form
// and the launcher both call it, after ValidEnvName.
func ReservedEnvName(name string) bool {
	upper := strings.ToUpper(name)
	if _, ok := reservedEnvNames[upper]; ok {
		return true
	}
	for _, prefix := range reservedEnvPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return strings.HasSuffix(upper, reservedEnvSuffix)
}

// ErrReservedLaunchEnv is returned by LaunchContainer and BuildLaunchCmd for an
// entry whose name ReservedEnvName refuses. It is an ErrInvalidLaunchEnv too
// (errors.Is). Its text is fixed: a name field can hold a pasted secret, so it
// never echoes the name or the value, and the error reaches the log.
var ErrReservedLaunchEnv error = reservedLaunchEnvError{}

type reservedLaunchEnvError struct{}

func (reservedLaunchEnvError) Error() string {
	return "reserved environment variable name: it cannot be set from the launch form"
}

// Is makes ErrReservedLaunchEnv a member of the ErrInvalidLaunchEnv family.
func (reservedLaunchEnvError) Is(target error) bool { return target == ErrInvalidLaunchEnv }
