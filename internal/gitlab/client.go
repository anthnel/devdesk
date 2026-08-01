package gitlab

import (
	"context"
	"fmt"
	"time"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"
)

// NewClient crée un nouveau client GitLab
func NewClient(url, token string) (*gitlabclient.Client, error) {
	c, err := gitlabclient.NewClient(token, gitlabclient.WithBaseURL(url))
	if err != nil {
		return nil, err
	}

	return c, nil
}

// TestConnection teste la connexion à GitLab
func TestConnection(c *gitlabclient.Client) (*gitlabclient.User, error) {
	user, _, err := c.Users.CurrentUser(gitlabclient.WithContext(context.Background()))
	if err != nil {
		return nil, err
	}

	return user, nil
}

// CreateGroup creates a new GitLab group
func CreateGroup(c *gitlabclient.Client, name, path, description, visibility string, parentID int64) (*gitlabclient.Group, error) {
	opts := &gitlabclient.CreateGroupOptions{
		Name:        gitlabclient.Ptr(name),
		Path:        gitlabclient.Ptr(path),
		Description: gitlabclient.Ptr(description),
		Visibility:  gitlabclient.Ptr(gitlabclient.VisibilityValue(visibility)),
	}

	if parentID > 0 {
		opts.ParentID = gitlabclient.Ptr(parentID)
	}

	group, _, err := c.Groups.CreateGroup(opts, gitlabclient.WithContext(context.Background()))
	if err != nil {
		return nil, err
	}

	return group, nil
}

// CreateProject creates a new GitLab project in a namespace
func CreateProject(c *gitlabclient.Client, name, path, description, visibility string, namespaceID int64) (*gitlabclient.Project, error) {
	opts := &gitlabclient.CreateProjectOptions{
		Name:        gitlabclient.Ptr(name),
		Path:        gitlabclient.Ptr(path),
		Description: gitlabclient.Ptr(description),
		Visibility:  gitlabclient.Ptr(gitlabclient.VisibilityValue(visibility)),
	}

	if namespaceID > 0 {
		opts.NamespaceID = gitlabclient.Ptr(namespaceID)
	}

	project, _, err := c.Projects.CreateProject(opts, gitlabclient.WithContext(context.Background()))
	if err != nil {
		return nil, err
	}

	return project, nil
}

// CommitAction represents a file action for a commit
type CommitAction struct {
	Action   string // "create", "update", "delete"
	FilePath string
	Content  string
}

// InitializeProjectWithFiles creates an initial commit with the given files
func InitializeProjectWithFiles(c *gitlabclient.Client, projectID int, files []CommitAction) error {
	actions := make([]*gitlabclient.CommitActionOptions, len(files))
	for i, f := range files {
		actions[i] = &gitlabclient.CommitActionOptions{
			Action:   gitlabclient.Ptr(gitlabclient.FileActionValue(f.Action)),
			FilePath: gitlabclient.Ptr(f.FilePath),
			Content:  gitlabclient.Ptr(f.Content),
		}
	}

	opts := &gitlabclient.CreateCommitOptions{
		Branch:        gitlabclient.Ptr("main"),
		CommitMessage: gitlabclient.Ptr("Initial commit from template"),
		Actions:       actions,
	}

	_, _, err := c.Commits.CreateCommit(projectID, opts, gitlabclient.WithContext(context.Background()))
	return err
}

// DeleteGroup deletes a GitLab group and all its contents (subgroups, projects)
// If permanentlyRemove is true, the group is deleted immediately without a grace period.
// GitLab requires a two-step process for immediate deletion:
// 1. First mark for deletion (schedule) - GitLab renames the group with suffix
// 2. Then permanently remove using the new path with deletion suffix
func DeleteGroup(c *gitlabclient.Client, groupID int, fullPath string, permanentlyRemove bool) error {
	if !permanentlyRemove {
		// Simple deletion - just schedule for deletion
		_, err := c.Groups.DeleteGroup(groupID, nil)
		return err
	}

	// Step 1: Mark for deletion (schedule)
	// GitLab will rename the group to "{path}-deletion_scheduled-{id}"
	_, err := c.Groups.DeleteGroup(groupID, nil)
	if err != nil {
		return err
	}

	// Step 2: Permanently remove using the renamed path
	// GitLab renames groups pending deletion to: {original_path}-deletion_scheduled-{id}
	deletionPath := fmt.Sprintf("%s-deletion_scheduled-%d", fullPath, groupID)

	opts := &gitlabclient.DeleteGroupOptions{
		PermanentlyRemove: gitlabclient.Ptr(true),
		FullPath:          gitlabclient.Ptr(deletionPath),
	}
	// Use the deletion path as group identifier since the group was renamed
	_, err = c.Groups.DeleteGroup(deletionPath, opts)
	if err != nil {
		return err
	}

	// Small delay to allow GitLab to propagate the deletion
	time.Sleep(500 * time.Millisecond)
	return nil
}

// DeleteProject deletes a GitLab project
// If permanentlyRemove is true, the project is deleted immediately without a grace period.
// GitLab requires a two-step process for immediate deletion:
// 1. First mark for deletion (schedule) - GitLab renames the project with suffix
// 2. Then permanently remove using the new path with deletion suffix
func DeleteProject(c *gitlabclient.Client, projectID int, fullPath string, permanentlyRemove bool) error {
	if !permanentlyRemove {
		_, err := c.Projects.DeleteProject(projectID, nil)
		return err
	}

	// Step 1: Mark for deletion (schedule)
	// GitLab will rename the project to "{path}-deletion_scheduled-{id}"
	_, err := c.Projects.DeleteProject(projectID, nil)
	if err != nil {
		return err
	}

	// Step 2: Permanently remove using the renamed path
	// GitLab renames projects pending deletion to: {original_path}-deletion_scheduled-{id}
	deletionPath := fmt.Sprintf("%s-deletion_scheduled-%d", fullPath, projectID)

	opts := &gitlabclient.DeleteProjectOptions{
		PermanentlyRemove: gitlabclient.Ptr(true),
		FullPath:          gitlabclient.Ptr(deletionPath),
	}
	_, err = c.Projects.DeleteProject(deletionPath, opts)
	if err != nil {
		return err
	}

	time.Sleep(500 * time.Millisecond)
	return nil
}
