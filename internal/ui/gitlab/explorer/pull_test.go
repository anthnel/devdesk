package explorer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/shared"
)

// The recursive pull is run for real against a throwaway git repository and a
// temporary directory: it is what proves the directory tree mirrors the group
// tree and that an existing checkout is skipped rather than clobbered. Only the
// clone URL is synthetic — the "GitLab host" is a local path.

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
}

// seedRemote creates a git repository that can be cloned from, at
// <root>/<fullPath>.git — the layout pullProject builds its HTTPS URL for.
func seedRemote(t *testing.T, root, fullPath string) {
	t.Helper()
	dir := filepath.Join(root, fullPath+".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the remote: %v", err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# seed\n"), 0o600); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.name=devdesk", "-c", "user.email=devdesk@example.com",
		"-c", "commit.gpgsign=false", "commit", "-m", "seed")
}

// pullModel returns a model whose configured GitLab "URL" is a local directory,
// so the clone URLs it builds resolve to the seeded remotes.
func pullModel(t *testing.T, remoteRoot string) Model {
	t.Helper()
	cfg := testConfig()
	cfg.GitLab.URL = remoteRoot
	return New(cfg, &shared.State{})
}

// ── Projects ─────────────────────────────────────────────────────────────────

func TestPullClonesAProject(t *testing.T) {
	requireGit(t)
	remotes := t.TempDir()
	seedRemote(t, remotes, "api")
	target := t.TempDir()
	m := pullModel(t, remotes)

	report := m.recursivePull(nil, &TreeNode{Name: "API", FullPath: "api", Type: NodeTypeProject},
		target, "https", remotes)

	if len(report.Errors) != 0 {
		t.Fatalf("pull reported errors: %v", report.Errors)
	}
	if len(report.Cloned) != 1 || report.Cloned[0] != "api" {
		t.Errorf("Cloned = %v, want the one project", report.Cloned)
	}
	if _, err := os.Stat(filepath.Join(target, "api", "README.md")); err != nil {
		t.Errorf("the clone is missing its contents: %v", err)
	}
}

// A checkout that already exists is left alone: overwriting it would discard
// whatever the user has in progress.
func TestPullSkipsAnExistingCheckout(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "api"), 0o755); err != nil {
		t.Fatalf("seeding the destination: %v", err)
	}
	m := pullModel(t, t.TempDir())

	report := m.recursivePull(nil, &TreeNode{Name: "API", FullPath: "api", Type: NodeTypeProject},
		target, "https", "https://gl.example.com")

	if len(report.Skipped) != 1 || report.Skipped[0] != "api" {
		t.Errorf("Skipped = %v, want the existing checkout", report.Skipped)
	}
	if len(report.Cloned) != 0 {
		t.Errorf("Cloned = %v, want nothing re-cloned", report.Cloned)
	}
}

func TestPullReportsACloneFailure(t *testing.T) {
	requireGit(t)
	m := pullModel(t, t.TempDir())

	report := m.recursivePull(nil, &TreeNode{Name: "API", FullPath: "nope", Type: NodeTypeProject},
		t.TempDir(), "https", filepath.Join(t.TempDir(), "missing"))

	if len(report.Errors) != 1 {
		t.Fatalf("Errors = %v, want the failed clone", report.Errors)
	}
	if !strings.Contains(report.Errors[0], "nope") {
		t.Errorf("the error does not name the project: %q", report.Errors[0])
	}
}

// The clone method decides the URL shape. SSH takes the host alone, so the
// scheme has to come off, and a trailing slash on the configured URL must not
// produce a double slash in the HTTPS form.
func TestCloneURL(t *testing.T) {
	tests := []struct {
		name      string
		gitlabURL string
		method    string
		want      string
	}{
		{"https", "https://gl.example.com", "https", "https://gl.example.com/infra/api.git"},
		{"https with a trailing slash", "https://gl.example.com/", "https", "https://gl.example.com/infra/api.git"},
		{"an unset method defaults to https", "https://gl.example.com", "", "https://gl.example.com/infra/api.git"},
		{"ssh", "https://gl.example.com", "ssh", "git@gl.example.com:infra/api.git"},
		{"ssh over plain http", "http://gl.example.com/", "ssh", "git@gl.example.com:infra/api.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cloneURL(tt.gitlabURL, tt.method, "infra/api"); got != tt.want {
				t.Errorf("cloneURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── Groups ───────────────────────────────────────────────────────────────────

// Pulling a group mirrors the tree on disk, one directory per level, with the
// slug rather than the full path as the directory name.
func TestPullMirrorsTheGroupTree(t *testing.T) {
	requireGit(t)
	remotes := t.TempDir()
	seedRemote(t, remotes, "infra/api")
	seedRemote(t, remotes, "infra/tools/cli")
	target := t.TempDir()
	m := pullModel(t, remotes)

	tools := &TreeNode{Name: "Tools", FullPath: "infra/tools", Type: NodeTypeGroup}
	tools.Children = []*TreeNode{{Name: "CLI", FullPath: "infra/tools/cli", Type: NodeTypeProject, Parent: tools}}
	infra := &TreeNode{Name: "Infra", FullPath: "infra", Type: NodeTypeGroup}
	infra.Children = []*TreeNode{
		{Name: "API", FullPath: "infra/api", Type: NodeTypeProject, Parent: infra},
		tools,
	}

	report := m.recursivePull(nil, infra, target, "https", remotes)

	if len(report.Errors) != 0 {
		t.Fatalf("pull reported errors: %v", report.Errors)
	}
	if len(report.Cloned) != 2 {
		t.Errorf("Cloned = %v, want both projects", report.Cloned)
	}
	for _, want := range []string{"infra/api", "infra/tools/cli"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(want), "README.md")); err != nil {
			t.Errorf("%s was not cloned into place: %v", want, err)
		}
	}
}

// A group whose children have not been browsed yet is fetched during the pull,
// so pulling the root does not require walking the tree by hand first.
func TestPullFetchesUnloadedChildren(t *testing.T) {
	requireGit(t)
	remotes := t.TempDir()
	seedRemote(t, remotes, "infra/api")

	f := newFakeGitLab(t, map[string]string{
		"/api/v4/groups/1/subgroups": `[]`,
		"/api/v4/groups/1/projects":  `[{"id":11,"name":"API","path_with_namespace":"infra/api"}]`,
		"/api/v4/projects/11":        `[]`,
	})
	m := serverModel(t, f)
	m.config.GitLab.URL = remotes
	target := t.TempDir()

	node := &TreeNode{ID: 1, Name: "Infra", FullPath: "infra", Type: NodeTypeGroup} // Children nil
	report := m.recursivePull(m.shared.GitLabClient, node, target, "https", remotes)

	if len(report.Errors) != 0 {
		t.Fatalf("pull reported errors: %v", report.Errors)
	}
	if len(report.Cloned) != 1 {
		t.Errorf("Cloned = %v, want the project fetched during the pull", report.Cloned)
	}
	// And they stay off the tree. Discovery nodes carry a path and a type and
	// none of the decoration the view renders, so caching them here blanked the
	// role and CI columns for every group a clone walked through — and the write
	// happened inside a Cmd, racing the Update that reads it (Rule 110).
	if node.Children != nil {
		t.Error("discovery nodes were stored on the tree the view renders")
	}
}

func TestPullReportsAFetchFailure(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil)) // everything 404s

	report := m.recursivePull(m.shared.GitLabClient,
		&TreeNode{ID: 1, FullPath: "infra", Type: NodeTypeGroup}, t.TempDir(), "https", "https://gl")

	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "fetch infra") {
		t.Errorf("Errors = %v, want the failed fetch", report.Errors)
	}
}

// A directory that cannot be created stops that branch and is reported rather
// than crashing the pull.
func TestPullReportsAMkdirFailure(t *testing.T) {
	target := t.TempDir()
	blocker := filepath.Join(target, "infra")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seeding the blocker: %v", err)
	}
	m := pullModel(t, t.TempDir())

	report := m.recursivePull(nil, &TreeNode{FullPath: "infra", Type: NodeTypeGroup}, target, "https", "https://gl")

	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "mkdir") {
		t.Errorf("Errors = %v, want the failed mkdir", report.Errors)
	}
}

// An empty report is still a report: the modal says "nothing to do" rather than
// rendering nil slices.
func TestPullReportStartsEmptyNotNil(t *testing.T) {
	m := pullModel(t, t.TempDir())

	report := m.recursivePull(nil, &TreeNode{FullPath: "empty", Type: NodeTypeGroup,
		Children: []*TreeNode{}}, t.TempDir(), "https", "https://gl")

	if report.Cloned == nil || report.Skipped == nil || report.Errors == nil {
		t.Errorf("report holds nil slices: %+v", report)
	}
}
