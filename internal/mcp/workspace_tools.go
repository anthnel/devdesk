package mcp

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/git"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxWorkspaceDepth bounds the walk under workspaces_dir. It is the workspaces
// view's own depth for nested repositories: a repository three levels down is a
// repository, and anything deeper is somebody's vendored tree.
const maxWorkspaceDepth = 3

type workspaceRepo struct {
	Name string `json:"name"`
	Path string `json:"path" jsonschema:"absolute; this is what scan_result takes as its target for a repository"`

	Branch string `json:"branch,omitempty"`
	Remote string `json:"remote,omitempty" jsonschema:"the origin URL as git stores it"`

	Modified  int `json:"modified" jsonschema:"tracked files with uncommitted changes"`
	Untracked int `json:"untracked"`

	HasUpstream bool `json:"has_upstream" jsonschema:"false means the branch tracks nothing, so ahead and behind say nothing"`
	Ahead       int  `json:"ahead" jsonschema:"local commits the upstream does not have"`
	Behind      int  `json:"behind" jsonschema:"upstream commits this copy does not have, as of the last fetch — see behind_is_stale"`

	// BehindIsStale is always true, and it is a field rather than a line of
	// documentation because that is the only form an agent reads (D35).
	BehindIsStale bool `json:"behind_is_stale" jsonschema:"always true: behind is read from the local tracking ref and this server never fetches, so a repository that has not been synced since it was last opened can report 0 while being far behind"`

	Scanned   bool      `json:"scanned"`
	ScannedAt time.Time `json:"scanned_at,omitempty"`
	Critical  int       `json:"critical,omitempty"`
	High      int       `json:"high,omitempty"`
	Medium    int       `json:"medium,omitempty"`
	Low       int       `json:"low,omitempty"`
	Sensitive *bool     `json:"sensitive,omitempty" jsonschema:"whether a secret was found; absent means no stage looked which is not the same as none found"`
}

type workspacesListOut struct {
	Context       string          `json:"context"`
	WorkspacesDir string          `json:"workspaces_dir"`
	Repositories  []workspaceRepo `json:"repositories"`
}

func registerWorkspacesList(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "workspaces_list",
		Description: toolDescription("workspaces_list"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, workspacesListOut, error) {
		root := env.Config.App.WorkspacesDir
		return nil, workspacesListOut{
			Context:       env.Context,
			WorkspacesDir: root,
			Repositories:  listRepositories(root, env),
		}, nil
	})
}

// listRepositories walks workspaces_dir and reports every repository under it,
// with its git state and what the scan cache knows about it.
//
// It lists **repositories**, where the view lists directories and marks which
// are repositories. That is the difference between a screen someone navigates
// and an answer to a question: an agent asking what is checked out wants the
// repositories, at whatever depth, not one level of a tree it would then have to
// walk a call at a time.
//
// It reads and runs git, and it never fetches. Everything about the upstream is
// therefore as fresh as the last fetch somebody else made — see
// workspaceRepo.BehindIsStale.
func listRepositories(root string, env *Env) []workspaceRepo {
	repos := make([]workspaceRepo, 0)
	if root == "" {
		return repos
	}

	scanned, err := cache.ReadWorkspaceScanEntries(env.Context)
	if err != nil {
		log.Printf("ERROR [mcp/workspaces] read workspace scan cache: %v", err)
		scanned = nil
	}

	showHidden := env.Config.App.ShowHiddenFiles
	for _, path := range findRepos(root, showHidden) {
		repo := workspaceRepo{
			Name:          filepath.Base(path),
			Path:          path,
			BehindIsStale: true,
		}

		// A repository git refuses to read is still a repository — it is
		// reported without its git fields rather than left out, because the
		// scan cache may well have something to say about it.
		if status, err := git.ReadStatus(path); err == nil {
			repo.Branch = status.Branch
			repo.Remote = status.Remote
			repo.Modified = status.Modified
			repo.Untracked = status.Untracked
			repo.HasUpstream = status.HasUpstream
			repo.Ahead = status.Ahead
			repo.Behind = status.Behind
		}

		if entry, ok := scanned[path]; ok {
			repo.Scanned = true
			repo.ScannedAt = entry.ScannedAt
			repo.Critical, repo.High = entry.Critical, entry.High
			repo.Medium, repo.Low = entry.Medium, entry.Low
			repo.Sensitive = entry.Sensitive
		}

		repos = append(repos, repo)
	}

	sort.Slice(repos, func(i, j int) bool {
		return strings.ToLower(repos[i].Path) < strings.ToLower(repos[j].Path)
	})
	return repos
}

// findRepos walks up to maxWorkspaceDepth and returns every repository it
// finds. It does not descend into one: a `.git` inside a repository is a
// submodule or a vendored copy, and neither is a row of its own here.
func findRepos(root string, showHidden bool) []string {
	var repos []string
	walkRepos(root, 0, showHidden, &repos)
	return repos
}

func walkRepos(dir string, depth int, showHidden bool, repos *[]string) {
	if depth > maxWorkspaceDepth {
		return
	}
	if isRepo(dir) {
		*repos = append(*repos, dir)
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// The same rule the view follows, and for the same reason: without it
		// the walk descends into .venv, .terraform and .cache, and reports
		// whatever they vendor as the user's own work.
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		walkRepos(filepath.Join(dir, name), depth+1, showHidden, repos)
	}
}

func isRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}
