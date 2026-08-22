package explorer

import (
	"context"
	"fmt"
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// handleCreateResource handles 'ctrl+n' key - start unified group/project creation.
// Stashes parent info and loads templates from OCI registry before showing form.
func (m Model) handleCreateResource() (tea.Model, tea.Cmd) {
	// Use the currently browsed group as parent, not the selected item.
	// currentGroupNode is nil at root level.
	parentName := ""
	parentID := ""

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
		0, // defaultResourceType = namespace (user can switch with ←→)
		m.creationParentName,
		m.creationParentID,
		m.config.Forge.DefaultVisibility,
		names,
		m.vocab(),
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

	if m.shared.Forge == nil {
		m.error = "Not connected to a forge"
		return m, nil
	}

	if msg.FormType == components.FormTypeGroup {
		return m, m.createGroup(msg)
	}
	return m, m.createProject(msg)
}

// createGroup creates a new GitLab group
func (m Model) createGroup(msg components.CreationFormSubmitMsg) tea.Cmd {
	backend := m.shared.Forge
	spec := forge.NewNamespace{
		Name:        msg.Name,
		Slug:        slugify(msg.Name),
		Description: msg.Description,
		Visibility:  msg.Visibility,
		ParentID:    msg.ParentID,
	}

	return func() tea.Msg {
		ns, err := backend.CreateNamespace(context.Background(), spec)
		if err != nil {
			return GroupCreatedMsg{Error: err}
		}
		return GroupCreatedMsg{Namespace: ns}
	}
}

// slugify turns a display name into the path segment a forge wants.
func slugify(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

// createProject creates a new GitLab project and optionally applies a template
func (m Model) createProject(msg components.CreationFormSubmitMsg) tea.Cmd {
	backend := m.shared.Forge
	spec := forge.NewRepository{
		Name:        msg.Name,
		Slug:        slugify(msg.Name),
		Description: msg.Description,
		Visibility:  msg.Visibility,
		NamespaceID: msg.ParentID,
	}
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
		repo, err := backend.CreateRepository(context.Background(), spec)
		if err != nil {
			return ProjectCreatedMsg{Error: err}
		}

		// Apply template if one was selected and resolved
		if templateRepo != "" {
			templateErr := applyTemplate(backend, repo.ID, registryURL, username, password, templateRepo, templateTag)
			if templateErr != nil {
				return ProjectCreatedMsg{Repository: repo, TemplateError: templateErr}
			}
		}

		return ProjectCreatedMsg{Repository: repo}
	}
}

// applyTemplate downloads an OCI template and commits its files to the project.
// This is a standalone function (not a method) because it runs inside a goroutine.
func applyTemplate(backend forge.Forge, repositoryID string, registryURL, username, password, repository, tag string) error {
	ociClient := oci.NewClient(registryURL, username, password)

	tmpl, err := ociClient.DownloadTemplate(repository, tag)
	if err != nil {
		return fmt.Errorf("download template: %w", err)
	}

	// Convert template files to commit actions
	files := make([]forge.FileChange, 0, len(tmpl.Files))
	for filePath, content := range tmpl.Files {
		files = append(files, forge.FileChange{
			Action:  forge.FileCreate,
			Path:    filePath,
			Content: content,
		})
	}

	if len(files) == 0 {
		return nil
	}

	return backend.InitialCommit(context.Background(), repositoryID, files)
}

// handleGroupCreated handles GroupCreatedMsg
func (m Model) handleGroupCreated(msg GroupCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.error = msg.Error.Error()
		return m, nil
	}
	// Store path to select after refresh
	m.pendingSelectPath = msg.Namespace.Path
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
	m.pendingSelectPath = msg.Repository.Path
	// Refresh tree to show new project (even if template failed)
	return m.handleRefresh()
}
