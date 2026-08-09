// Package git runs the git binary.
//
// It lives outside internal/gitlab on purpose. Cloning was a GitLab operation
// and could take the configured token for granted; syncing is not — the
// workspaces view reconciles whatever is on disk, and a repository there may
// have any remote at all. A package named after one forge is the wrong place to
// decide which host a credential may be sent to, and the wrong place to tempt
// anyone into deciding it by default.
package git

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// CloneOptions is what a clone needs beyond the URL.
type CloneOptions struct {
	// Token authenticates over HTTPS. Empty means the clone is expected to
	// succeed without one — SSH, or a public repository.
	Token string
}

// Clone clones a Git repository to the target path.
//
// repoURL is the full URL to clone (HTTPS or SSH); targetPath is the absolute
// path where the repo should be cloned.
//
// **Nothing here may prompt.** DevDesk owns the terminal, and a `git` that
// wants credentials does not simply fail: stdin is the null device, so git's
// own prompt is skipped and the *credential helper* takes over. A helper is a
// separate process — Git Credential Manager on Windows — which writes its
// `info: please complete authentication in your browser` to the console
// directly, over the top of the rendered frame, and then waits. The frame is
// corrupted, the row spins for ever, and nothing on screen says why.
//
// So the environment forbids every interactive path, and the token DevDesk
// already holds is passed in instead. A clone that still cannot authenticate
// fails immediately, with git's own reason.
func Clone(repoURL, targetPath string, opts CloneOptions) error {
	cmd := exec.Command("git", "clone", repoURL, targetPath)
	cmd.Env = nonInteractiveEnv(opts.Token)

	// stdin is the null device, and stdout/stderr are captured rather than
	// merely discarded: git's reason for failing is the only thing the row's
	// Detail column has to show.
	var stderr bytes.Buffer
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if reason := lastLine(stderr.String()); reason != "" {
			return fmt.Errorf("%s", reason)
		}
		return err
	}
	return nil
}

// nonInteractiveEnv builds the environment every git subprocess runs under.
//
// Clone and Sync share it because they share the hazard: both reach the
// network, and either can end up in front of a credential helper. token is the
// credential to offer, or "" for none.
func nonInteractiveEnv(token string) []string {
	env := append(os.Environ(),
		// git's own prompt, the credential helper's browser flow, and the two
		// askpass hooks. All four are ways for a child process to take the
		// terminal, and all four are off.
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		// SSH has prompts of its own — a key passphrase, an unknown host key.
		// BatchMode turns both into an immediate failure, which is a row that
		// says so rather than a row that never moves.
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)

	// A stalled transfer is aborted by git itself rather than by killing the
	// process. That matters: git cleans up the directory it was writing into,
	// so a clone still never leaves half a repository behind (§3.16,
	// decision 12).
	config := [][2]string{
		{"http.lowSpeedLimit", "1000"},
		{"http.lowSpeedTime", "60"},
	}
	if token != "" {
		// Basic with `oauth2` as the username is GitLab's documented form for a
		// personal access token over HTTPS.
		//
		// It travels in the environment, not in argv: a command line is
		// readable from the process list by anyone on the machine, and it would
		// also be the token, in the clear, once per clone. Nor does it go in the
		// URL — that form is written into every cloned repository's
		// .git/config and stays there.
		credential := base64.StdEncoding.EncodeToString([]byte("oauth2:" + token))
		config = append(config, [2]string{"http.extraHeader", "Authorization: Basic " + credential})
	}

	env = append(env, fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(config)))
	for i, kv := range config {
		env = append(env,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, kv[0]),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, kv[1]),
		)
	}
	return env
}

// lastLine returns the final non-empty line of git's stderr, which is where it
// puts the reason. The lines above it are progress.
func lastLine(output string) string {
	lines := strings.Split(strings.ReplaceAll(output, "\r", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// DirExists checks if a directory exists at the given path
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
