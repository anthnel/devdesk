package explorer

import (
	"fmt"
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/gitlab"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// handleCreateResource handles 'ctrl+n' key - start unified group/project creation.
// Stashes parent info and loads templates from OCI registry before showing form.
func (m Model) handleCreateResource(flatNodes []*TreeNode) (tea.Model, tea.Cmd) {
	// Use the currently browsed group as parent, not the selected item.
	// currentGroupNode is nil at root level.
	parentName := ""
	var parentID int64 = 0

	if m.currentGroupNode != nil {
		parentName = m.currentGroupNode.FullPath
		parentID = m.currentGroupNode.ID
	}

	// Stash parent info for after template loading
	m.creationParentName = parentName
	m.creationParentID = parentID
	m.mode = ModeLoadingTemplates

	return m, m.loadTemplates()
}

// registryPassword reads the template registry's password from the secret
// store, keyed by the registry URL.
//
// It used to be config.Registry.Password — plaintext YAML, read on every
// template listing. The field is gone; a value left there by an older version
// was moved into the store at startup (§3.9).
//
// An empty result is not an error: an anonymous registry is the common case,
// and oci.NewClient treats empty credentials as "do not authenticate".
func (m Model) registryPassword() string {
	if m.shared == nil || m.shared.Secrets.Storage == nil || m.config.Registry.URL == "" {
		return ""
	}
	password, err := m.shared.Secrets.Storage.Load(m.config.Registry.URL)
	if err != nil {
		return ""
	}
	return password
}

// loadTemplates loads available templates from the OCI registry catalog.
// Degrades gracefully: returns empty list if registry is not configured or on error.
func (m Model) loadTemplates() tea.Cmd {
	registryURL := m.config.Registry.URL
	basePath := m.config.Registry.TemplatesRepository
	username := m.config.Registry.Username
	password := m.registryPassword()

	return func() tea.Msg {
		// If registry not configured, return empty (graceful degradation)
		if registryURL == "" || basePath == "" {
			return TemplatesLoadedMsg{}
		}

		client := oci.NewClient(registryURL, username, password)
		entries, err := client.ListTemplates(basePath)
		if err != nil {
			log.Printf("ERROR: failed to load templates from OCI registry: %v", err)
			return TemplatesLoadedMsg{Error: err}
		}

		return TemplatesLoadedMsg{Templates: entries}
	}
}

// handleTemplatesLoaded handles TemplatesLoadedMsg - creates the project form with loaded templates
func (m Model) handleTemplatesLoaded(msg TemplatesLoadedMsg) (tea.Model, tea.Cmd) {
	m.mode = ModeCreatingProject
	m.templateEntries = msg.Templates

	// Extract display names for the form
	names := make([]string, len(msg.Templates))
	for i, t := range msg.Templates {
		names[i] = t.Name
	}

	m.creationForm = components.NewCreationForm(
		0, // defaultResourceType = Group (user can switch with ←→)
		m.creationParentName,
		m.creationParentID,
		m.config.GitLab.DefaultVisibility,
		names,
	)
	if msg.Error != nil {
		m.creationForm.SetTemplateWarning(fmt.Sprintf("Registry error: %v", msg.Error))
	}
	return m, nil
}

// handleCreationSubmit handles form submission
func (m Model) handleCreationSubmit(msg components.CreationFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.creationForm = nil

	client := m.shared.GitLabClient
	if client == nil {
		m.error = "GitLab client not initialized"
		return m, nil
	}

	if msg.FormType == components.FormTypeGroup {
		return m, m.createGroup(msg)
	}
	return m, m.createProject(msg)
}

// createGroup creates a new GitLab group
func (m Model) createGroup(msg components.CreationFormSubmitMsg) tea.Cmd {
	client := m.shared.GitLabClient
	name := msg.Name
	description := msg.Description
	visibility := msg.Visibility
	parentID := msg.ParentID

	return func() tea.Msg {
		// Use name as path (slug)
		path := strings.ToLower(strings.ReplaceAll(name, " ", "-"))

		group, err := gitlab.CreateGroup(client, name, path, description, visibility, parentID)
		if err != nil {
			return GroupCreatedMsg{Error: err}
		}
		return GroupCreatedMsg{Group: group}
	}
}

// createProject creates a new GitLab project and optionally applies a template
func (m Model) createProject(msg components.CreationFormSubmitMsg) tea.Cmd {
	client := m.shared.GitLabClient
	name := msg.Name
	description := msg.Description
	visibility := msg.Visibility
	namespaceID := msg.ParentID
	registryURL := m.config.Registry.URL
	username := m.config.Registry.Username
	password := m.registryPassword()

	// Resolve template entry from display name
	var templateRepo, templateTag string
	if msg.Template != "" {
		for _, entry := range m.templateEntries {
			if entry.Name == msg.Template {
				templateRepo = entry.Repository
				templateTag = entry.Tag
				break
			}
		}
	}

	return func() tea.Msg {
		// Use name as path (slug)
		path := strings.ToLower(strings.ReplaceAll(name, " ", "-"))

		project, err := gitlab.CreateProject(client, name, path, description, visibility, namespaceID)
		if err != nil {
			return ProjectCreatedMsg{Error: err}
		}

		// Apply template if one was selected and resolved
		if templateRepo != "" {
			templateErr := applyTemplate(client, int(project.ID), registryURL, username, password, templateRepo, templateTag)
			if templateErr != nil {
				return ProjectCreatedMsg{Project: project, TemplateError: templateErr}
			}
		}

		return ProjectCreatedMsg{Project: project}
	}
}

// applyTemplate downloads an OCI template and commits its files to the project.
// This is a standalone function (not a method) because it runs inside a goroutine.
func applyTemplate(client *gitlabclient.Client, projectID int, registryURL, username, password, repository, tag string) error {
	ociClient := oci.NewClient(registryURL, username, password)

	tmpl, err := ociClient.DownloadTemplate(repository, tag)
	if err != nil {
		return fmt.Errorf("download template: %w", err)
	}

	// Convert template files to commit actions
	actions := make([]gitlab.CommitAction, 0, len(tmpl.Files))
	for filePath, content := range tmpl.Files {
		actions = append(actions, gitlab.CommitAction{
			Action:   "create",
			FilePath: filePath,
			Content:  content,
		})
	}

	if len(actions) == 0 {
		return nil
	}

	return gitlab.InitializeProjectWithFiles(client, projectID, actions)
}

// handleGroupCreated handles GroupCreatedMsg
func (m Model) handleGroupCreated(msg GroupCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.error = msg.Error.Error()
		return m, nil
	}
	// Store path to select after refresh
	if msg.Group != nil {
		m.pendingSelectPath = msg.Group.FullPath
	}
	// Refresh tree to show new group
	return m.handleRefresh()
}

// handleProjectCreated handles ProjectCreatedMsg
func (m Model) handleProjectCreated(msg ProjectCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.error = msg.Error.Error()
		return m, nil
	}

	// Template application failed but project was created successfully
	if msg.TemplateError != nil {
		log.Printf("ERROR: template application failed: %v", msg.TemplateError)
		m.error = fmt.Sprintf("Project created but template failed: %v", msg.TemplateError)
	}

	// Store path to select after refresh
	if msg.Project != nil {
		m.pendingSelectPath = msg.Project.PathWithNamespace
	}
	// Refresh tree to show new project (even if template failed)
	return m.handleRefresh()
}
