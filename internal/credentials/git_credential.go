package credentials

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const (
	// credentialUsername is the username DevDesk stores tokens under. GitLab
	// personal access tokens are used as the password of a fixed user.
	credentialUsername = "oauth2"

	// credentialTimeout bounds a `git credential` call. The configured helper
	// may wait for user input, and an unbounded call would freeze the TUI.
	credentialTimeout = 2 * time.Second

	// credentialWaitDelay bounds how long Wait blocks on the output pipes once
	// the process has been killed. Killing git does not kill the helper git
	// spawned, and that surviving grandchild holds the pipes open.
	credentialWaitDelay = 500 * time.Millisecond
)

// GitCredentialStorage stocke les credentials via git credential manager
type GitCredentialStorage struct {
	context string // Contexte DevDesk pour isoler les credentials
}

// NewGitCredentialStorageWithContext crée un GitCredentialStorage context-aware
// Chaque contexte aura ses propres credentials isolés
func NewGitCredentialStorageWithContext(context string) *GitCredentialStorage {
	if context == "" {
		context = "default"
	}
	return &GitCredentialStorage{
		context: context,
	}
}

// describe builds the credential description git reads on stdin. The DevDesk
// context is carried in the path field, which is what keeps two contexts
// pointing at the same host from overwriting each other — see runCredential for
// why that field needs help to survive.
func (g *GitCredentialStorage) describe(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	return fmt.Sprintf("protocol=%s\nhost=%s\npath=devdesk/context/%s\nusername=%s\n",
		u.Scheme, u.Host, g.context, credentialUsername), nil
}

// Save sauvegarde un token via git credential
func (g *GitCredentialStorage) Save(urlStr, token string) error {
	desc, err := g.describe(urlStr)
	if err != nil {
		return err
	}
	_, err = runCredential("approve", desc+"password="+token+"\n")
	return err
}

// Load charge un token depuis git credential
func (g *GitCredentialStorage) Load(urlStr string) (string, error) {
	desc, err := g.describe(urlStr)
	if err != nil {
		return "", err
	}
	output, err := runCredential("fill", desc)
	if err != nil {
		return "", err
	}
	if token, ok := parsePassword(output); ok {
		return token, nil
	}
	return "", fmt.Errorf("%w for %s (context: %s)", ErrNotFound, urlStr, g.context)
}

// Delete supprime un token de git credential
func (g *GitCredentialStorage) Delete(urlStr string) error {
	desc, err := g.describe(urlStr)
	if err != nil {
		return err
	}
	_, err = runCredential("reject", desc)
	return err
}

// parsePassword extracts the password field from a `git credential fill` reply.
func parsePassword(output string) (string, bool) {
	for line := range strings.SplitSeq(output, "\n") {
		if after, ok := strings.CutPrefix(line, "password="); ok {
			return strings.TrimRight(after, "\r"), true
		}
	}
	return "", false
}

// runCredential feeds description to `git credential <op>` and returns its stdout.
//
// credential.useHttpPath is forced on for this invocation only. Git discards the
// path field by default, which would key every credential on protocol://host
// alone and silently collapse all DevDesk contexts onto a single token — the
// last context to authenticate would win for all of them. Setting the option in
// the user's config instead would change how git resolves credentials for every
// repository on the machine, so it is passed per call.
//
// Interactive prompts are disabled: a helper waiting on a terminal DevDesk does
// not own would hang, and the timeout would be the only thing left to catch it.
func runCredential(op, description string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), credentialTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-c", "credential.useHttpPath=true", "credential", op)
	cmd.Stdin = strings.NewReader(description)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = credentialWaitDelay
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0", // Désactive les prompts interactifs
		"GCM_INTERACTIVE=never", // Git Credential Manager en mode non-interactif
	)

	err := cmd.Run()
	if ctx.Err() != nil {
		return "", fmt.Errorf("git credential %s timed out (not configured or waiting for input)", op)
	}
	if err != nil {
		return "", fmt.Errorf("git credential %s failed: %v: %s", op, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
