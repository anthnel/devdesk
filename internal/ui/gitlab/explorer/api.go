package explorer

import (
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	tea "github.com/charmbracelet/bubbletea"
)

// perPage is what every list endpoint asks for. GitLab caps it at 100.
const perPage = 100

// listAll walks every page of a list endpoint and returns the lot (D34).
//
// Each caller used to pass `Page: 1` and keep only what came back, so a group
// with more than perPage subgroups or projects was silently cut: the explorer
// showed fewer than it had and a recursive clone skipped repositories without
// saying so. The loop lives here rather than at each of the four call sites
// because four copies of it is how one of them would end up wrong.
//
// opts is the caller's embedded ListOptions, mutated between calls, so the
// fetch closure sees the new page without the caller rebuilding its options.
func listAll[T any](opts *gitlabclient.ListOptions, fetch func() ([]T, *gitlabclient.Response, error)) ([]T, error) {
	opts.PerPage = perPage
	opts.Page = 1

	var all []T
	for {
		page, resp, err := fetch()
		if err != nil {
			return nil, err
		}
		all = append(all, page...)

		// NextPage is 0 on the last page. Comparing against the page just
		// fetched rather than against 0 alone also stops a server that keeps
		// pointing back at it, which would otherwise spin for ever — and it is
		// a bound that cannot cut a legitimate response short, which a page
		// limit would be.
		if resp == nil || resp.NextPage <= opts.Page {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

// fetchGroupChildren fetches children (subgroups + projects) from GitLab API
func (m Model) fetchGroupChildren(client *gitlabclient.Client, parentNode *TreeNode) ([]*TreeNode, error) {
	groupID := int(parentNode.ID)
	var currentUserID int64
	if m.shared.CurrentUser != nil {
		currentUserID = m.shared.CurrentUser.ID
	}
	children := []*TreeNode{}

	// Fetch direct subgroups only
	subgroupsOpts := &gitlabclient.ListSubGroupsOptions{}
	subgroups, err := listAll(&subgroupsOpts.ListOptions, func() ([]*gitlabclient.Group, *gitlabclient.Response, error) {
		return client.Groups.ListSubGroups(groupID, subgroupsOpts)
	})
	if err != nil {
		return nil, err
	}

	for _, group := range subgroups {
		children = append(children, &TreeNode{
			ID:         group.ID,
			Name:       group.Name,
			FullPath:   group.FullPath,
			Type:       NodeTypeGroup,
			Parent:     parentNode,
			Expanded:   false,
			Visibility: string(group.Visibility),
			CreatedAt:  group.CreatedAt,
			WebURL:     group.WebURL,
		})
	}

	// Fetch projects
	projectsOpts := &gitlabclient.ListGroupProjectsOptions{}
	projects, err := listAll(&projectsOpts.ListOptions, func() ([]*gitlabclient.Project, *gitlabclient.Response, error) {
		return client.Groups.ListGroupProjects(groupID, projectsOpts)
	})
	if err != nil {
		return nil, err
	}

	for _, project := range projects {
		ciStatus := fetchLastPipelineStatus(client, project.ID)
		accessLevel := fetchProjectAccessLevel(client, int64(project.ID), currentUserID)
		children = append(children, projectToTreeNode(project, parentNode, ciStatus, accessLevel))
	}

	return children, nil
}

// loadRootGroups charge les groupes racine
func (m Model) loadRootGroups() tea.Cmd {
	client := m.shared.GitLabClient
	var currentUserID int64
	if m.shared.CurrentUser != nil {
		currentUserID = m.shared.CurrentUser.ID
	}

	return func() tea.Msg {
		// Récupérer les groupes racine (top-level groups)
		opts := &gitlabclient.ListGroupsOptions{
			TopLevelOnly: gitlabclient.Ptr(true),
		}

		groups, err := listAll(&opts.ListOptions, func() ([]*gitlabclient.Group, *gitlabclient.Response, error) {
			return client.Groups.ListGroups(opts)
		})
		if err != nil {
			return LoadErrorMsg{Error: err}
		}

		// Convertir en TreeNodes
		nodes := make([]*TreeNode, len(groups))
		for i, group := range groups {
			nodes[i] = &TreeNode{
				ID:          group.ID,
				Name:        group.Name,
				FullPath:    group.FullPath,
				Type:        NodeTypeGroup,
				Children:    nil, // Lazy load
				Expanded:    false,
				Visibility:  string(group.Visibility),
				CreatedAt:   group.CreatedAt,
				AccessLevel: fetchGroupAccessLevel(client, group.ID, currentUserID),
				WebURL:      group.WebURL,
			}
		}

		return RootGroupsLoadedMsg{Nodes: nodes}
	}
}

// loadChildren charge les sous-groupes et projets d'un groupe
func (m Model) loadChildren(parentNode *TreeNode) tea.Cmd {
	client := m.shared.GitLabClient
	groupID := int(parentNode.ID)
	var currentUserID int64
	if m.shared.CurrentUser != nil {
		currentUserID = m.shared.CurrentUser.ID
	}

	return func() tea.Msg {
		children := []*TreeNode{}

		// Charger les sous-groupes directs (pas les descendants)
		subgroupsOpts := &gitlabclient.ListSubGroupsOptions{}
		subgroups, err := listAll(&subgroupsOpts.ListOptions, func() ([]*gitlabclient.Group, *gitlabclient.Response, error) {
			return client.Groups.ListSubGroups(groupID, subgroupsOpts)
		})
		if err != nil {
			return LoadErrorMsg{Error: err, ParentNode: parentNode}
		}

		for _, group := range subgroups {
			children = append(children, &TreeNode{
				ID:          group.ID,
				Name:        group.Name,
				FullPath:    group.FullPath,
				Type:        NodeTypeGroup,
				Parent:      parentNode,
				Expanded:    false,
				Visibility:  string(group.Visibility),
				CreatedAt:   group.CreatedAt,
				AccessLevel: fetchGroupAccessLevel(client, group.ID, currentUserID),
				WebURL:      group.WebURL,
			})
		}

		// Charger les projets du groupe
		projectsOpts := &gitlabclient.ListGroupProjectsOptions{}
		projects, err := listAll(&projectsOpts.ListOptions, func() ([]*gitlabclient.Project, *gitlabclient.Response, error) {
			return client.Groups.ListGroupProjects(groupID, projectsOpts)
		})
		if err != nil {
			return LoadErrorMsg{Error: err, ParentNode: parentNode}
		}

		for _, project := range projects {
			ciStatus := fetchLastPipelineStatus(client, project.ID)
			accessLevel := fetchProjectAccessLevel(client, int64(project.ID), currentUserID)
			children = append(children, projectToTreeNode(project, parentNode, ciStatus, accessLevel))
		}

		return ChildrenLoadedMsg{
			ParentNode: parentNode,
			Children:   children,
		}
	}
}

// projectToTreeNode converts a GitLab project to a TreeNode with metadata
func projectToTreeNode(project *gitlabclient.Project, parent *TreeNode, pipelineStatus string, accessLevel int) *TreeNode {
	return &TreeNode{
		ID:                int64(project.ID),
		Name:              project.Name,
		FullPath:          project.PathWithNamespace,
		Type:              NodeTypeProject,
		Parent:            parent,
		Expanded:          false,
		Visibility:        string(project.Visibility),
		CreatedAt:         project.CreatedAt,
		LastActivityAt:    project.LastActivityAt,
		PipelineStatus:    pipelineStatus,
		AccessLevel:       accessLevel,
		MarkedForDeletion: project.MarkedForDeletionOn != nil,
		WebURL:            project.WebURL,
	}
}

// fetchLastPipelineStatus fetches the latest pipeline status for a project
func fetchLastPipelineStatus(client *gitlabclient.Client, projectID int64) string {
	pipelines, _, err := client.Pipelines.ListProjectPipelines(projectID, &gitlabclient.ListProjectPipelinesOptions{
		ListOptions: gitlabclient.ListOptions{PerPage: 1, Page: 1},
	})
	if err != nil || len(pipelines) == 0 {
		return ""
	}
	return pipelines[0].Status
}

// fetchGroupAccessLevel returns the current user's access level in a group (0 if unknown)
func fetchGroupAccessLevel(client *gitlabclient.Client, groupID int64, userID int64) int {
	if userID == 0 {
		return 0
	}
	member, _, err := client.GroupMembers.GetInheritedGroupMember(int(groupID), userID)
	if err != nil {
		return 0
	}
	return int(member.AccessLevel)
}

// fetchProjectAccessLevel returns the current user's access level in a project (0 if unknown)
func fetchProjectAccessLevel(client *gitlabclient.Client, projectID int64, userID int64) int {
	if userID == 0 {
		return 0
	}
	member, _, err := client.ProjectMembers.GetInheritedProjectMember(int(projectID), userID)
	if err != nil {
		return 0
	}
	return int(member.AccessLevel)
}
