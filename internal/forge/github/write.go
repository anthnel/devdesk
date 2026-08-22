package github

import (
	"context"
	"errors"
	"fmt"

	gh "github.com/google/go-github/v68/github"

	"github.com/anthnel/devdesk/internal/forge"
)

// errNoOrgAPI is what CreateNamespace and DeleteNamespace return.
//
// GitHub has no REST endpoint for either: an organisation is created and
// deleted from the web, and no token scope changes that. The interface expects
// a backend to refuse what it cannot express rather than to do something
// adjacent and report success — the alternative here would be creating a
// repository under the user's own account and calling it an organisation.
var errNoOrgAPI = errors.New("GitHub organizations are created and deleted from the web, not through the API")

// errNoPermanentDelete is what a permanent delete returns.
//
// GitHub deletes at once and has no grace period, so there is nothing to
// bypass. Deleting normally and reporting success would tell the user a
// distinction was honoured when the platform does not make one — and
// Shape.PermanentDelete is false precisely so the checkbox never gets offered.
var errNoPermanentDelete = errors.New("GitHub deletes immediately; there is no grace period to bypass")

// CreateNamespace is refused. See errNoOrgAPI.
func (f *Forge) CreateNamespace(context.Context, forge.NewNamespace) (forge.Namespace, error) {
	return forge.Namespace{}, errNoOrgAPI
}

// DeleteNamespace is refused. See errNoOrgAPI.
func (f *Forge) DeleteNamespace(context.Context, forge.Namespace, bool) error {
	return errNoOrgAPI
}

// CreateRepository creates a repository inside an organisation.
//
// An empty NamespaceID means the user's own account, which is what go-github's
// empty `org` argument does — so the interface's "empty means the user's own
// namespace" needs no translation here.
func (f *Forge) CreateRepository(ctx context.Context, spec forge.NewRepository) (forge.Repository, error) {
	shape := f.Shape()
	if spec.Visibility != "" && !shape.AllowsVisibility(spec.Visibility) {
		return forge.Repository{}, fmt.Errorf("GitHub does not offer %q visibility", spec.Visibility)
	}

	created, _, err := f.client.Repositories.Create(ctx, spec.NamespaceID, &gh.Repository{
		Name:        gh.Ptr(spec.Slug),
		Description: gh.Ptr(spec.Description),
		Private:     gh.Ptr(spec.Visibility != "public"),
		// AutoInit is deliberately off. InitialCommit is what writes the first
		// commit, and an auto-generated README would give it a parent it does
		// not expect — the two would race to own the initial state.
		AutoInit: gh.Ptr(false),
	})
	if err != nil {
		return forge.Repository{}, err
	}

	return f.repositories(ctx, []*gh.Repository{created}, "")[0], nil
}

// InitialCommit writes the first commit of an empty repository.
//
// It is four calls rather than one per file, and that is the whole design:
// Repositories.CreateFile in a loop would produce one commit per file, so a
// three-file template would arrive as three commits — and the second would need
// the SHA the first returned, which is a chain rather than a batch.
//
// The git data API takes the whole thing at once: a blob per file, one tree, a
// commit with **no parent**, then the ref that makes it the branch. No parent
// is what makes this an *initial* commit; on a repository that already has one
// the ref creation fails, which is the honest outcome — this is not a way to
// overwrite history.
func (f *Forge) InitialCommit(ctx context.Context, repositoryID string, files []forge.FileChange) error {
	owner, repo, ok := splitPath(repositoryID)
	if !ok {
		return fmt.Errorf("not a GitHub repository: %q", repositoryID)
	}
	if len(files) == 0 {
		return nil
	}

	entries := make([]*gh.TreeEntry, 0, len(files))
	for _, file := range files {
		if file.Action == forge.FileDelete {
			// There is nothing to delete in a repository with no commits, and
			// silently dropping it would hide a template that means something
			// else than it says.
			return fmt.Errorf("an initial commit cannot delete %q", file.Path)
		}

		blob, _, err := f.client.Git.CreateBlob(ctx, owner, repo, &gh.Blob{
			Content:  gh.Ptr(file.Content),
			Encoding: gh.Ptr("utf-8"),
		})
		if err != nil {
			return fmt.Errorf("uploading %s: %w", file.Path, err)
		}

		entries = append(entries, &gh.TreeEntry{
			Path: gh.Ptr(file.Path),
			Mode: gh.Ptr("100644"),
			Type: gh.Ptr("blob"),
			SHA:  blob.SHA,
		})
	}

	// No base tree: the repository is empty, and naming one that does not exist
	// is an error rather than an empty starting point.
	tree, _, err := f.client.Git.CreateTree(ctx, owner, repo, "", entries)
	if err != nil {
		return fmt.Errorf("building the tree: %w", err)
	}

	commit, _, err := f.client.Git.CreateCommit(ctx, owner, repo, &gh.Commit{
		Message: gh.Ptr(initialCommitMessage),
		Tree:    tree,
	}, nil)
	if err != nil {
		return fmt.Errorf("creating the commit: %w", err)
	}

	_, _, err = f.client.Git.CreateRef(ctx, owner, repo, &gh.Reference{
		Ref:    gh.Ptr("refs/heads/" + defaultBranch),
		Object: &gh.GitObject{SHA: commit.SHA},
	})
	if err != nil {
		return fmt.Errorf("creating %s: %w", defaultBranch, err)
	}
	return nil
}

const (
	// initialCommitMessage is the GitLab backend's, word for word: a template
	// applied on either platform should read the same in the log.
	initialCommitMessage = "Initial commit from template"

	// defaultBranch matches what both backends create. GitHub's per-account
	// default is configurable, but a repository created here has no commits
	// yet, so nothing has chosen a branch — naming it is the only way this is
	// deterministic.
	defaultBranch = "main"
)

// DeleteRepository deletes a repository. A permanent delete is refused: see
// errNoPermanentDelete.
func (f *Forge) DeleteRepository(ctx context.Context, repo forge.Repository, permanently bool) error {
	if permanently {
		return errNoPermanentDelete
	}
	owner, name, ok := splitPath(repo.ID)
	if !ok {
		return fmt.Errorf("not a GitHub repository: %q", repo.ID)
	}
	_, err := f.client.Repositories.Delete(ctx, owner, name)
	return err
}

// DashboardStats fetches the five counters.
//
// Four of them come from the **search** API, which is what GitHub offers
// instead of GitLab's X-Total header — and which has its own, much lower rate
// limit. Each is filled only when its request succeeded; a failed one stays nil
// rather than reading as a zero the dashboard would render as "you have none"
// (D52).
func (f *Forge) DashboardStats(ctx context.Context, user forge.User) (forge.DashboardStats, error) {
	if user.Username == "" {
		return forge.DashboardStats{}, errors.New("dashboard stats: no user")
	}

	var stats forge.DashboardStats
	one := &gh.SearchOptions{ListOptions: gh.ListOptions{PerPage: 1}}

	// Total is what these are for; the single result is discarded.
	if result, _, err := f.client.Search.Issues(ctx,
		"is:open is:pr assignee:"+user.Username, one); err == nil {
		stats.AssignedChangeRequests = forge.Count(result.GetTotal())
	}

	if result, _, err := f.client.Search.Issues(ctx,
		"is:open is:pr review-requested:"+user.Username, one); err == nil {
		stats.ReviewChangeRequests = forge.Count(result.GetTotal())
	}

	if result, _, err := f.client.Search.Issues(ctx,
		"is:open is:issue assignee:"+user.Username, one); err == nil {
		stats.AssignedIssues = forge.Count(result.GetTotal())
	}

	// Repositories and organisations have no search counterpart, so they are
	// walked. Both are bounded by what one account belongs to, which is not the
	// unbounded list a search would be.
	if repos, err := listAll(func(page *gh.ListOptions) ([]*gh.Repository, *gh.Response, error) {
		return f.client.Repositories.ListByAuthenticatedUser(ctx,
			&gh.RepositoryListByAuthenticatedUserOptions{ListOptions: *page})
	}); err == nil {
		stats.Repositories = forge.Count(len(repos))
	}

	if orgs, err := listAll(func(page *gh.ListOptions) ([]*gh.Organization, *gh.Response, error) {
		return f.client.Organizations.List(ctx, "", page)
	}); err == nil {
		stats.Namespaces = forge.Count(len(orgs))
	}

	return stats, nil
}
