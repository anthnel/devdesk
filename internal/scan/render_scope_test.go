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

// A renderer in a container gets the git repository holding the target, so an
// overlay or a chart can reach a sibling of the target as it does on the host
// (DEVDESK-742) — but never a mount wider than a repository: not the
// filesystem root, not the home directory or anything above it, not a
// directory a symlink leads out of, and not when git did not answer.
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
	if err := os.Symlink(filepath.Join(elsewhere), link); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		top     string
		target  string
		homeErr error
		want    bool
	}{
		{"the repository holding the target", repo, target, nil, true},
		{"the target is the repository", target, target, nil, false},
		{"git printed nothing", "", target, nil, false},
		{"git printed a relative path", "src/repo", target, nil, false},
		{"the filesystem root", string(filepath.Separator), target, nil, false},
		{"the home directory", home, target, nil, false},
		{"above the home directory", filepath.Dir(home), target, nil, false},
		{"the home directory is unknown", repo, target, errors.New("no home"), false},
		{"not an ancestor of the target", elsewhere, target, nil, false},
		{"a symlink leads the target out of it", repo, link, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useHome(t, home, tt.homeErr)

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

// git is asked where the repository is, in the target; when it fails (no
// git, not a repository), the target alone is mounted, as before.
func TestGitDecidesTheMount(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "src", "repo")
	target := filepath.Join(repo, "deploy")
	mkdirs(t, target)
	useHome(t, filepath.Join(base, "home"), nil)

	for _, tt := range []struct {
		name  string
		out   string
		err   error
		mount string
	}{
		{"inside a repository", repo + "\n", nil, repo},
		{"not a repository", "", &exitError{Code: 128, Stderr: "fatal: not a git repository"}, target},
		{"no git", "", errors.New(`exec: "git": executable file not found in $PATH`), target},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
				if tc.Name != "git" || strings.Join(tc.Args, " ") != "rev-parse --show-toplevel" || tc.Dir != target {
					t.Errorf("unexpected invocation %s in %q", tc, tc.Dir)
				}
				return []byte(tt.out), tt.err
			}}
			useRunner(t, r)

			scope := resolveRenderScope(t.Context(), target)

			if scope.mount != tt.mount || scope.target != target {
				t.Errorf("scope = %+v, want %q mounted", scope, tt.mount)
			}
		})
	}
}

// Widened, the container mounts the repository and works in the target, so
// the renderer's arguments (relative to the target) do not change.
func TestAWidenedMountKeepsTheTargetAsTheWorkingDirectory(t *testing.T) {
	scope := renderScope{target: "/src/repo/deploy", mount: "/src/repo", rel: "deploy"}
	tool := ToolSpec{Source: ToolSourceContainer}

	for name, tc := range map[string]toolCmd{
		"kustomize": kustomizeArgs(scope, "overlays/prod", tool),
		"helm":      helmArgs(scope, "charts/api", tool, "template"),
	} {
		got := strings.Join(tc.Args, " ")
		if !strings.Contains(got, " -v /src/repo:/scan:ro -w /scan/deploy ") {
			t.Errorf("%s: %q, want the repository mounted and /scan/deploy as the working directory", name, got)
		}
	}
	binary := kustomizeArgs(scope, "overlays/prod", ToolSpec{Source: ToolSourceBinary})
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
