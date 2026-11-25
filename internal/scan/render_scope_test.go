package scan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useHome swaps the home directory the mount guard reads.
func useHome(t *testing.T, dir string, err error) {
	t.Helper()
	previous := userHomeDir
	userHomeDir = func() (string, error) { return dir, err }
	t.Cleanup(func() { userHomeDir = previous })
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// writeFile writes content at p, creating its directory.
func writeFile(t *testing.T, p, content string) {
	t.Helper()
	mkdirs(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A renderer in a container gets the git repository holding the target, so an
// overlay or a chart can reach a sibling of the target as it does on the host
// (DEVDESK-742) — but never a mount wider than a repository: not the
// filesystem root, not the home directory or anything above it, not a
// directory a symlink leads out of, not when git did not answer, and not a
// root git was redirected to: the announced root must hold the .git itself, a
// directory or the file of a worktree whose git directory points back at it.
func TestTheMountWidensToTheGitRepositoryOnlyWhenSafe(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home", "u")
	repo := filepath.Join(home, "src", "repo")
	target := filepath.Join(repo, "deploy", "overlays")
	elsewhere := filepath.Join(base, "elsewhere")
	mkdirs(t, target, elsewhere)
	link := filepath.Join(home, "src", "link")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the repository that leads out of it: judged by its
	// path, it is under the repository; resolved, it is not.
	inside := filepath.Join(repo, "out")
	if err := os.Symlink(elsewhere, inside); err != nil {
		t.Fatal(err)
	}

	dotGit := filepath.Join(repo, ".git")
	gitDir := func(t *testing.T) { mkdirs(t, dotGit) }
	main := filepath.Join(base, "main", ".git")
	worktree := func(back string) func(t *testing.T) {
		return func(t *testing.T) {
			wt := filepath.Join(main, "worktrees", "repo")
			writeFile(t, filepath.Join(wt, "gitdir"), back+"\n")
			writeFile(t, dotGit, "gitdir: "+wt+"\n")
		}
	}
	tests := []struct {
		name    string
		top     string
		target  string
		homeErr error
		dotGit  func(t *testing.T)
		want    bool
	}{
		{"the repository holding the target", repo, target, nil, gitDir, true},
		{"a worktree whose git directory points back", repo, target, nil, worktree(dotGit), true},
		{"the target is the repository", target, target, nil, gitDir, false},
		{"git printed nothing", "", target, nil, gitDir, false},
		{"git printed a relative path", "src/repo", target, nil, gitDir, false},
		{"the filesystem root", string(filepath.Separator), target, nil, gitDir, false},
		{"the home directory", home, target, nil, gitDir, false},
		{"above the home directory", filepath.Dir(home), target, nil, gitDir, false},
		{"the home directory is unknown", repo, target, errors.New("no home"), gitDir, false},
		{"not an ancestor of the target", elsewhere, target, nil, gitDir, false},
		{"a symlink outside leads the target elsewhere", repo, link, nil, gitDir, false},
		{"a symlink inside leads the target out", repo, inside, nil, gitDir, false},
		{"no .git at the announced root", repo, target, nil, func(*testing.T) {}, false},
		{"a .git file pointing at an outside git directory", repo, target, nil,
			worktree(filepath.Join(elsewhere, ".git")), false},
		{"a .git file that is not a gitdir line", repo, target, nil,
			func(t *testing.T) { writeFile(t, dotGit, "[core]\n") }, false},
		{"a .git symlink", repo, target, nil,
			func(t *testing.T) {
				if err := os.Symlink(main, dotGit); err != nil {
					t.Fatal(err)
				}
			}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useHome(t, home, tt.homeErr)
			if err := os.RemoveAll(dotGit); err != nil {
				t.Fatal(err)
			}
			tt.dotGit(t)

			scope, ok := widerScope(tt.target, tt.top)

			if ok != tt.want {
				t.Fatalf("widened = %v (%+v), want %v", ok, scope, tt.want)
			}
			if ok && (scope.mount != repo || scope.rel != "deploy/overlays") {
				t.Errorf("scope = %+v, want the repository mounted and deploy/overlays as the target", scope)
			}
		})
	}
}

// git is asked, in the target, where the repository is and whether
// core.worktree redirects it; a redirected repository, or a git that does not
// answer (absent, not a repository), mounts the target alone, as before.
func TestGitDecidesTheMount(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "src", "repo")
	target := filepath.Join(repo, "deploy")
	mkdirs(t, target, filepath.Join(repo, ".git"))
	useHome(t, filepath.Join(base, "home"), nil)
	unset := &exitError{Code: 1}

	for _, tt := range []struct {
		name      string
		out       string
		err       error
		worktree  string
		worktreeE error
		mount     string
	}{
		{"inside a repository", repo + "\n", nil, "", unset, repo},
		{"core.worktree is set", repo + "\n", nil, "/etc\n", nil, target},
		{"core.worktree cannot be read", repo + "\n", nil, "", &exitError{Code: 3, Stderr: "fatal: bad config"}, target},
		{"not a repository", "", &exitError{Code: 128, Stderr: "fatal: not a git repository"}, "", unset, target},
		{"no git", "", errors.New(`exec: "git": executable file not found in $PATH`), "", unset, target},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
				if tc.Name != "git" || tc.Dir != target {
					t.Errorf("unexpected invocation %s in %q", tc, tc.Dir)
				}
				switch strings.Join(tc.Args, " ") {
				case "rev-parse --show-toplevel":
					return []byte(tt.out), tt.err
				case "config --get core.worktree":
					return []byte(tt.worktree), tt.worktreeE
				}
				t.Errorf("unexpected git %s", tc)
				return nil, errors.New("unexpected")
			}}
			useRunner(t, r)

			scope := resolveRenderScope(t.Context(), target)

			if scope.mount != tt.mount || scope.target != target {
				t.Errorf("scope = %+v, want %q mounted", scope, tt.mount)
			}
		})
	}
}

// The variables that point git at another repository, another work tree or
// another configuration are not passed to the git DevDesk asks about the
// mount: the answer has to come from the target itself. GIT_CEILING_DIRECTORIES
// is kept: it can only stop git's search earlier, and a user who set it on
// /mnt/share must not see DevDesk mount /mnt/share anyway.
func TestGitIsAskedWithoutRedirectingVariables(t *testing.T) {
	redirecting := []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CONFIG_GLOBAL",
		"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_PARAMETERS"}
	for _, name := range redirecting {
		t.Setenv(name, "/redirected")
	}
	t.Setenv("DEVDESK_KEPT", "1")
	t.Setenv("GIT_CEILING_DIRECTORIES", "/mnt/share")
	target := t.TempDir()
	r := &scriptedRunner{reply: func(toolCmd) ([]byte, error) { return nil, &exitError{Code: 128} }}
	useRunner(t, r)

	resolveRenderScope(t.Context(), target)

	if len(r.calls) == 0 {
		t.Fatal("git was not asked")
	}
	for _, tc := range r.calls {
		if tc.Env == nil {
			t.Fatalf("%s inherits the whole environment", tc)
		}
		env := strings.Join(tc.Env, "\n")
		for _, name := range redirecting {
			if strings.Contains("\n"+env, "\n"+name+"=") {
				t.Errorf("%s reaches %s", name, tc)
			}
		}
		for _, kept := range []string{"DEVDESK_KEPT=1", "GIT_CEILING_DIRECTORIES=/mnt/share"} {
			if !strings.Contains("\n"+env, "\n"+kept) {
				t.Errorf("%s was dropped from %s", kept, tc)
			}
		}
	}
}

// Widened, the container mounts the repository and works in the target, so
// the renderer's arguments (relative to the target) do not change.
func TestAWidenedMountKeepsTheTargetAsTheWorkingDirectory(t *testing.T) {
	scope := renderScope{target: "/src/repo/deploy", mount: "/src/repo", rel: "deploy"}
	tool := ToolSpec{Source: ToolSourceContainer}

	for name, tc := range map[string]toolCmd{
		"kustomize": kustomizeArgs(scope, "overlays/prod", tool, false),
		"helm":      helmArgs(scope, "charts/api", tool, "template"),
	} {
		got := strings.Join(tc.Args, " ")
		if !strings.Contains(got, " -v /src/repo:/scan:ro -w /scan/deploy ") {
			t.Errorf("%s: %q, want the repository mounted and /scan/deploy as the working directory", name, got)
		}
	}
	binary := kustomizeArgs(scope, "overlays/prod", ToolSpec{Source: ToolSourceBinary}, false)
	if binary.Dir != "/src/repo/deploy" {
		t.Errorf("a binary runs in %q, want the target", binary.Dir)
	}
}

// helm lint names a chart by its absolute path in the container, under the
// widened mount too: it comes back relative to the target.
func TestHelmLintPathsUnderAWidenedMount(t *testing.T) {
	scope := renderScope{target: "/src/repo/deploy", mount: "/src/repo", rel: "deploy"}
	for in, want := range map[string]string{
		"/scan/deploy/charts/api": "charts/api",
		"/scan/deploy":            ".",
		"templates/cm.yaml":       "charts/api/templates/cm.yaml",
	} {
		if got := helmLintPath(in, "charts/api", scope); got != want {
			t.Errorf("helmLintPath(%q) = %q, want %q", in, got, want)
		}
	}
}
