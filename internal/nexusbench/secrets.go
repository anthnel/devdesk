package nexusbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	secretsFileMode = 0o600
	secretsDirMode  = 0o700
	// gitCheckTimeout bounds the one git process the store may start.
	gitCheckTimeout = 10 * time.Second
	// redactedSecrets is what a Secrets shows in place of its values.
	redactedSecrets = "Secrets{redacted}"
)

// Secrets are the passwords of the bench. They live in the secrets file and
// nowhere else: not in the descriptor, not in a manifest, not in an error.
type Secrets struct {
	// AdminPassword is the current password of the admin account.
	AdminPassword string `json:"adminPassword"`
	// PendingAdminPassword is the new admin password, written before it is sent
	// to Nexus so that an interrupted change does not lose it.
	PendingAdminPassword string `json:"pendingAdminPassword,omitempty"`
	// PullPassword is the password of the read-only bench-pull account.
	PullPassword string `json:"pullPassword,omitempty"`
}

// String implements fmt.Stringer so that %v, %+v and %s never print a password.
func (Secrets) String() string { return redactedSecrets }

// GoString implements fmt.GoStringer so that %#v never prints a password.
func (Secrets) GoString() string { return redactedSecrets }

// LogValue implements slog.LogValuer so that a logger never records a password.
func (Secrets) LogValue() slog.Value { return slog.StringValue(redactedSecrets) }

// SecretStore keeps the Secrets of one bench (one context and namespace).
type SecretStore interface {
	// Load returns the stored secrets; found is false, without an error, when
	// nothing is stored yet.
	Load() (s Secrets, found bool, err error)
	// Save replaces the stored secrets atomically.
	Save(Secrets) error
	// Remove deletes the stored secrets. It succeeds when there are none.
	Remove() error
}

// DefaultSecretsPath returns where the secrets of a bench live:
// <state>/devdesk/nexus-bench/<context>/<namespace>/secrets.json, with <state>
// the XDG_STATE_HOME variable, else ~/.local/state. The folder is outside every
// repository and shared by all the worktrees of the project. getenv is os.Getenv
// in production; an empty variable counts as unset.
func DefaultSecretsPath(kubeContext, namespace string, getenv func(string) string) (string, error) {
	for _, part := range []string{kubeContext, namespace} {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, `/\`) {
			return "", fmt.Errorf("%q cannot name a folder of the secrets path", part)
		}
	}
	state := getenv("XDG_STATE_HOME")
	if state == "" {
		home := getenv("HOME")
		if home == "" {
			home = getenv("USERPROFILE")
		}
		if home == "" {
			return "", errors.New("cannot place the secrets file: neither XDG_STATE_HOME nor HOME is set; set NEXUS_BENCH_SECRETS to a path outside the repository")
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "devdesk", "nexus-bench", kubeContext, namespace, "secrets.json"), nil
}

// fileStore is the SecretStore backed by one JSON file.
type fileStore struct{ path string }

// NewFileStore returns the store of the file at path. It touches nothing on
// disk. A path inside a git working tree is refused unless git ignores it, so
// that a secret cannot be committed by accident; a path outside every
// repository is accepted without asking git.
func NewFileStore(path string) (SecretStore, error) {
	if path == "" {
		return nil, errors.New("the secrets path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve the secrets path: %w", err)
	}
	if root, inRepo := findGitRoot(filepath.Dir(abs)); inRepo {
		ignored, err := gitIgnores(root, abs)
		if err != nil {
			return nil, &BenchError{
				Cause: fmt.Sprintf("the secrets path %s is inside the git repository %s and git could not tell whether it is ignored", abs, root),
				Check: "run git check-ignore -v on that path, or set NEXUS_BENCH_SECRETS to a path outside the repository",
				Err:   err,
			}
		}
		if !ignored {
			return nil, &BenchError{
				Cause: fmt.Sprintf("the secrets path %s is inside the git repository %s and is not ignored by git, so the passwords could be committed", abs, root),
				Check: "set NEXUS_BENCH_SECRETS to a path outside the repository, or add the file to .gitignore",
			}
		}
	}
	return &fileStore{path: abs}, nil
}

// findGitRoot walks up from dir (which may not exist yet) to the first folder
// that holds a .git entry, a directory or a file as in a worktree.
func findGitRoot(dir string) (root string, ok bool) {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// gitIgnores reports whether git ignores path inside the working tree at root.
// It is a variable so a test can replace the one process the package starts for
// the guard; kubectl.go stays the only file that starts one otherwise.
var gitIgnores = func(root, path string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "-q", "--", path) //nolint:gosec // fixed arguments, the paths come from the caller's own configuration
	cmd.Env = gitEnvironment()
	err := cmd.Run()
	if err == nil {
		return true, nil // exit 0: the path is ignored
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil // exit 1: the path is not ignored
	}
	return false, err // git missing, exit 128 (not a repository, bad path), timeout
}

// gitEnvironment is the environment of the git process: the caller's, without
// the variables that point git at a repository. Inherited, GIT_DIR and its
// siblings would make git judge another repository than the one holding the
// path, and the guard would answer for the wrong one. Names are compared in
// upper case because Windows environment names are case-insensitive.
func gitEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_NAMESPACE":
			continue
		}
		env = append(env, kv)
	}
	return env
}

// Load implements SecretStore. The error never carries the content of the
// file: a corrupt file may hold a password.
func (s *fileStore) Load() (Secrets, bool, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return Secrets{}, false, nil
	}
	if err != nil {
		return Secrets{}, false, fmt.Errorf("read the secrets file: %w", err)
	}
	var out Secrets
	if err := json.Unmarshal(data, &out); err != nil {
		return Secrets{}, false, &BenchError{
			Cause: fmt.Sprintf("the secrets file %s is not valid", s.path),
			Check: "delete the file and run the bench again, or restore it from a copy; its content is not shown",
			Err:   err,
		}
	}
	return out, true, nil
}

// Save implements SecretStore.
func (s *fileStore) Save(secrets Secrets) error {
	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the secrets: %w", err)
	}
	return writeFileAtomic(s.path, append(data, '\n'), secretsFileMode, secretsDirMode)
}

// Remove implements SecretStore. It deletes the file, then its folder when
// that is empty; a folder that holds anything else stays.
func (s *fileStore) Remove() error {
	if err := os.Remove(s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the secrets file: %w", err)
	}
	dir := filepath.Dir(s.path)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && len(entries) > 0) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read the secrets folder: %w", err)
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the secrets folder: %w", err)
	}
	return nil
}

// writeFileAtomic writes data to path through a temporary file of the same
// folder and a rename, so a reader sees the old file or the new one, never a
// partial one. Missing folders are created with dirMode. The temporary file is
// removed when anything fails. The error text holds paths, never data.
func writeFileAtomic(path string, data []byte, fileMode, dirMode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create the folder %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err = tmp.Chmod(fileMode); err != nil {
		return fmt.Errorf("set the mode of %s: %w", tmp.Name(), err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", tmp.Name(), err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	syncDir(dir)
	return nil
}

// syncDir flushes the folder entry written by the rename. Syncing the file
// alone does not make the new name durable: after a power cut the folder could
// still list the old file, and a password written just before it was sent to
// Nexus would be lost. It is best effort: Windows cannot open a folder for
// syncing and some file systems do not support it, and the file is already
// replaced either way, so every error is ignored.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
