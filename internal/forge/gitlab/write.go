package gitlab

import (
	"context"
	"fmt"
	"time"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/forge"
)

// CreateNamespace creates a group, optionally under a parent.
func (f *Forge) CreateNamespace(ctx context.Context, spec forge.NewNamespace) (forge.Namespace, error) {
	opts := &gitlabclient.CreateGroupOptions{
		Name:        gitlabclient.Ptr(spec.Name),
		Path:        gitlabclient.Ptr(spec.Slug),
		Description: gitlabclient.Ptr(spec.Description),
		Visibility:  gitlabclient.Ptr(gitlabclient.VisibilityValue(spec.Visibility)),
	}

	// ParentID is one of the two places a path will not do — see numericID.
	if spec.ParentID != "" {
		parent, err := numericID(spec.ParentID)
		if err != nil {
			return forge.Namespace{}, err
		}
		opts.ParentID = gitlabclient.Ptr(parent)
	}

	group, _, err := f.client.Groups.CreateGroup(opts, gitlabclient.WithContext(ctx))
	if err != nil {
		return forge.Namespace{}, err
	}
	return f.namespaces(ctx, []*gitlabclient.Group{group}, 0)[0], nil
}

// CreateRepository creates a project inside a namespace.
func (f *Forge) CreateRepository(ctx context.Context, spec forge.NewRepository) (forge.Repository, error) {
	opts := &gitlabclient.CreateProjectOptions{
		Name:        gitlabclient.Ptr(spec.Name),
		Path:        gitlabclient.Ptr(spec.Slug),
		Description: gitlabclient.Ptr(spec.Description),
		Visibility:  gitlabclient.Ptr(gitlabclient.VisibilityValue(spec.Visibility)),
	}

	if spec.NamespaceID != "" {
		ns, err := numericID(spec.NamespaceID)
		if err != nil {
			return forge.Repository{}, err
		}
		opts.NamespaceID = gitlabclient.Ptr(ns)
	}

	project, _, err := f.client.Projects.CreateProject(opts, gitlabclient.WithContext(ctx))
	if err != nil {
		return forge.Repository{}, err
	}
	return f.repositories(ctx, []*gitlabclient.Project{project}, 0)[0], nil
}

// InitialCommit writes the first commit of a repository — what a template is
// applied by.
func (f *Forge) InitialCommit(ctx context.Context, repositoryID string, files []forge.FileChange) error {
	actions := make([]*gitlabclient.CommitActionOptions, len(files))
	for i, file := range files {
		actions[i] = &gitlabclient.CommitActionOptions{
			Action:   gitlabclient.Ptr(gitlabclient.FileActionValue(file.Action)),
			FilePath: gitlabclient.Ptr(file.Path),
			Content:  gitlabclient.Ptr(file.Content),
		}
	}

	opts := &gitlabclient.CreateCommitOptions{
		Branch:        gitlabclient.Ptr("main"),
		CommitMessage: gitlabclient.Ptr("Initial commit from template"),
		Actions:       actions,
	}

	_, _, err := f.client.Commits.CreateCommit(repositoryID, opts, gitlabclient.WithContext(ctx))
	return err
}

// DeleteNamespace deletes a group and everything under it.
//
// A permanent deletion is two calls, and the second does not address what the
// first did: GitLab schedules the deletion by *renaming* the group to
// `<path>-deletion_scheduled-<id>`, so the removal is asked for by that new
// name. Getting it from the value rather than from an ID is why the interface
// takes the whole Namespace.
func (f *Forge) DeleteNamespace(ctx context.Context, ns forge.Namespace, permanently bool) error {
	_, err := f.client.Groups.DeleteGroup(ns.ID, nil, gitlabclient.WithContext(ctx))
	if err != nil || !permanently {
		return err
	}

	id, err := numericID(ns.ID)
	if err != nil {
		return err
	}
	renamed := deletionPath(ns.Path, id)

	opts := &gitlabclient.DeleteGroupOptions{
		PermanentlyRemove: gitlabclient.Ptr(true),
		FullPath:          gitlabclient.Ptr(renamed),
	}
	if _, err := f.client.Groups.DeleteGroup(renamed, opts, gitlabclient.WithContext(ctx)); err != nil {
		return err
	}

	time.Sleep(propagationDelay)
	return nil
}

// DeleteRepository deletes a project. Same two-call reasoning as
// DeleteNamespace.
func (f *Forge) DeleteRepository(ctx context.Context, repo forge.Repository, permanently bool) error {
	_, err := f.client.Projects.DeleteProject(repo.ID, nil, gitlabclient.WithContext(ctx))
	if err != nil || !permanently {
		return err
	}

	id, err := numericID(repo.ID)
	if err != nil {
		return err
	}
	renamed := deletionPath(repo.Path, id)

	opts := &gitlabclient.DeleteProjectOptions{
		PermanentlyRemove: gitlabclient.Ptr(true),
		FullPath:          gitlabclient.Ptr(renamed),
	}
	if _, err := f.client.Projects.DeleteProject(renamed, opts, gitlabclient.WithContext(ctx)); err != nil {
		return err
	}

	time.Sleep(propagationDelay)
	return nil
}

// DashboardStats fetches the five counters.
//
// Each is a separate request and each can fail on its own, so each is filled
// only when its request succeeded. A failed one stays nil — see
// forge.DashboardStats and D52: the int this replaced left a zero that the
// dashboard rendered exactly as it renders "you have none".
//
// It returns no error for the same reason: four counters out of five is a
// better dashboard than none, and which one is missing is on the field rather
// than in a message.
func (f *Forge) DashboardStats(ctx context.Context, user forge.User) (forge.DashboardStats, error) {
	uid, err := numericID(user.ID)
	if err != nil {
		return forge.DashboardStats{}, fmt.Errorf("dashboard stats: %w", err)
	}

	var stats forge.DashboardStats
	one := gitlabclient.ListOptions{PerPage: 1}

	// TotalItems comes from the X-Total header, so one item per page is enough
	// to learn how many there are.
	if _, resp, err := f.client.MergeRequests.ListMergeRequests(&gitlabclient.ListMergeRequestsOptions{
		State:       gitlabclient.Ptr("opened"),
		AssigneeID:  gitlabclient.AssigneeID(uid),
		ListOptions: one,
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.AssignedChangeRequests = forge.Count(int(resp.TotalItems))
	}

	if _, resp, err := f.client.MergeRequests.ListMergeRequests(&gitlabclient.ListMergeRequestsOptions{
		State:       gitlabclient.Ptr("opened"),
		ReviewerID:  gitlabclient.ReviewerID(uid),
		ListOptions: one,
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.ReviewChangeRequests = forge.Count(int(resp.TotalItems))
	}

	if _, resp, err := f.client.Issues.ListIssues(&gitlabclient.ListIssuesOptions{
		State:       gitlabclient.Ptr("opened"),
		AssigneeID:  gitlabclient.AssigneeID(uid),
		ListOptions: one,
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.AssignedIssues = forge.Count(int(resp.TotalItems))
	}

	if _, resp, err := f.client.Projects.ListProjects(&gitlabclient.ListProjectsOptions{
		Membership:  gitlabclient.Ptr(true),
		ListOptions: one,
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.Repositories = forge.Count(int(resp.TotalItems))
	}

	if _, resp, err := f.client.Groups.ListGroups(&gitlabclient.ListGroupsOptions{
		ListOptions: one,
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.Namespaces = forge.Count(int(resp.TotalItems))
	}

	return stats, nil
}
