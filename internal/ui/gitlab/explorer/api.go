package explorer

import (
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	tea "github.com/charmbracelet/bubbletea"
)

// fetchGroupChildren fetches children (subgroups + projects) from GitLab API
func (m Model) fetchGroupChildren(client *gitlabclient.Client, parentNode *TreeNode) ([]*TreeNode, error) {
	groupID := int(parentNode.ID)
	var currentUserID int64
	if m.shared.CurrentUser != nil {
		currentUserID = m.shared.CurrentUser.ID
	}
	children := []*TreeNode{}

	// Fetch direct subgroups only
	subgroupsOpts := &gitlabclient.ListSubGroupsOptions{
		ListOptions: gitlabclient.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}
	subgroups, _, err := client.Groups.ListSubGroups(groupID, subgroupsOpts)
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
	projectsOpts := &gitlabclient.ListGroupProjectsOptions{
		ListOptions: gitlabclient.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}
	projects, _, err := client.Groups.ListGroupProjects(groupID, projectsOpts)
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
			ListOptions: gitlabclient.ListOptions{
				PerPage: 100,
				Page:    1,
			},
			TopLevelOnly: gitlabclient.Ptr(true),
		}

		groups, _, err := client.Groups.ListGroups(opts)
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
		subgroupsOpts := &gitlabclient.ListSubGroupsOptions{
			ListOptions: gitlabclient.ListOptions{
				PerPage: 100,
				Page:    1,
			},
		}
		subgroups, _, err := client.Groups.ListSubGroups(groupID, subgroupsOpts)
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
		projectsOpts := &gitlabclient.ListGroupProjectsOptions{
			ListOptions: gitlabclient.ListOptions{
				PerPage: 100,
				Page:    1,
			},
		}
		projects, _, err := client.Groups.ListGroupProjects(groupID, projectsOpts)
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
