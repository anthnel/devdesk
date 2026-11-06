package git

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain cuts every git subprocess of this package off from the machine's own
// git configuration. Both sides inherit os.Environ — the test helpers through
// cLocaleEnv, the code under test through nonInteractiveEnv — so setting it once
// here covers them all.
//
// Without it the suite answered for the developer's ~/.gitconfig rather than
// for the code: a global `fetch.pruneTags = true` failed two tag tests locally
// while CI stayed green (DEVDESK-135). A behavior a configuration option
// changes gets its own test that sets the option on the repository.
//
// An empty file rather than /dev/null, which does not exist on Windows.
// GIT_CONFIG_GLOBAL needs git 2.32; HOME and XDG_CONFIG_HOME point at the same
// empty directory so an older git finds no global file either. Nothing in this
// package writes under HOME, so sharing it across -count=N runs is harmless.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	dir, err := os.MkdirTemp("", "devdesk-git-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating the isolated git home: %v\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	empty := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "creating the empty git config: %v\n", err)
		return 1
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   empty,
		"GIT_CONFIG_SYSTEM":   empty,
		"GIT_CONFIG_NOSYSTEM": "1",
		"HOME":                dir,
		"USERPROFILE":         dir,
		"XDG_CONFIG_HOME":     dir,
	} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(os.Stderr, "setting %s: %v\n", k, err)
			return 1
		}
	}
	return m.Run()
}
