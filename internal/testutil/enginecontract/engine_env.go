package enginecontract

import (
	"fmt"
	"regexp"
	"strings"
)

// EngineEnvVars are the variables that send the docker or podman CLI to another
// engine than the local one: a remote daemon, a context, a system connection. A
// contract creates and removes resources, and records what the engine answered, so
// it must talk to the engine of the test VM and to no other.
var EngineEnvVars = []string{"DOCKER_HOST", "DOCKER_CONTEXT", "CONTAINER_HOST", "CONTAINER_CONNECTION"}

// CheckEngineEnv refuses an environment in which one of EngineEnvVars is set (even
// to an empty value counts as unset). contract.sh runs the contracts under
// `env -i`, so nothing of the VM's own reaches them; this is for a contract run by
// hand.
//
// Limit: the current context a docker CLI keeps in ~/.docker/config.json
// ("currentContext"), podman's system connections in containers.conf and
// DOCKER_CONFIG are not read here: only the four variables are covered.
func CheckEngineEnv(getenv func(string) string) error {
	var set []string
	for _, name := range EngineEnvVars {
		if getenv(name) != "" {
			set = append(set, name)
		}
	}
	if len(set) == 0 {
		return nil
	}
	verb := "are"
	if len(set) == 1 {
		verb = "is"
	}
	return fmt.Errorf("contract refused: %s %s set, and would send the engine commands of a contract to another engine than "+
		"the local one; unset the variable (mise run engine-contract runs the contracts in the test VMs with a clean "+
		"environment; ~/.docker/config.json currentContext and podman system connections are not checked)",
		strings.Join(set, ", "), verb)
}

// imagePattern is the shape of the image a contract starts: a repository and a
// digest, never a tag, because a tag moves and the contracts must not depend on
// what a registry says today. contract.sh pins the exact image of versions.env.
var imagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]*@sha256:[0-9a-f]{64}$`)

// ValidImage reports whether ref is a repository pinned by a sha256 digest. The
// Go side accepts any repository (a contract may need another image one day);
// contract.sh is stricter and checks the one it pulls.
func ValidImage(ref string) bool { return imagePattern.MatchString(ref) }
