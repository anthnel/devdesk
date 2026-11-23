package credentials

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitCommand runs git for this package from a neutral directory, never from
// DevDesk's working directory (DEVDESK-745).
//
// Run from the working directory, git reads the .git/config of whatever
// repository DevDesk was started in, and a credential.helper there is a shell
// command chosen by that repository's author: it was selected as the backend,
// handed the token on approve, and run again on every fill at launch. That
// configuration is not the user's. From an empty directory created for the
// call, with GIT_CEILING_DIRECTORIES on its parent, git finds no repository
// and reads only the system, global and environment scopes.
//
// The directory is created per call and removed by the returned cleanup: the
// calls are rare (a probe at launch, a save at login), and nothing is left
// behind when the process exits.
func gitCommand(ctx context.Context, args ...string) (*exec.Cmd, func(), error) {
	dir, err := os.MkdirTemp("", "devdesk-git-")
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(dir)
	return cmd, func() { _ = os.RemoveAll(dir) }, nil
}

// gitEnv is the environment of a git call run from dir: the process
// environment, with dir's parent appended to GIT_CEILING_DIRECTORIES so git
// cannot walk up out of dir into a repository. A value the user set is kept.
func gitEnv(dir string) []string {
	ceiling := filepath.Dir(dir)
	const key = "GIT_CEILING_DIRECTORIES="
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if v, ok := strings.CutPrefix(kv, key); ok {
			if v != "" {
				ceiling = v + string(os.PathListSeparator) + ceiling
			}
			continue
		}
		env = append(env, kv)
	}
	return append(env, key+ceiling)
}
