package explorer

import (
	"context"
	"fmt"
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// handleCreateResource handles 'ctrl+n' key - start unified group/project creation.
// Stashes parent info and loads templates from OCI registry before showing form.
func (m Model) handleCreateResource() (tea.Model, tea.Cmd) {
	// GetShortcuts greys N while the level is loading, and Rule 130 forbids
	// greying a key that acts anyway. It matters more than the tidiness: the
	// parent's ID is read below, and a level still loading has none to give —
	// the form would open under an empty parent and create at the root.
	if m.loading {
		return m, m.footer.Warn(reasonStillLoading)
	}

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

// handleCreationSubmit handles form submission.
//
// The row goes on screen before the request goes out: creating is a network
// call, and the tree used to be emptied by a full refresh for the whole of it —
// the user saw a blank table and a silent footer, with nothing to say the thing
// they had just asked for was on its way. The row is the answer, and the
// registry is what keeps it honest (see jobs.go).
func (m Model) handleCreationSubmit(msg components.CreationFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.creationForm = nil

	if m.shared.Forge == nil {
		m.error = "Not connected to a forge"
		return m, nil
	}

	target := m.createTarget(msg)
	if m.busy(target) {
		return m, m.footer.Warn(busyMessage)
	}

	nodeType := NodeTypeProject
	work := m.createProject(msg, target)
	if msg.FormType == components.FormTypeGroup {
		nodeType = NodeTypeGroup
		work = m.createGroup(msg, target)
	}

	m.insertCreatingNode(msg, target, nodeType)
	return m, jobs.Start(createRun(target, msg.Name), work)
}

// createTarget is the path the row and the run are both keyed on, predicted
// from the parent being browsed and the slug the name becomes.
//
// A prediction, because the forge answers with the real one and is free to
// differ — but the row has to exist before there is an answer, so it needs a
// key that can be computed now. settleCreating is what reconciles the two.
func (m Model) createTarget(msg components.CreationFormSubmitMsg) string {
	slug := slugify(msg.Name)
	if m.creationParentName == "" {
		return slug
	}
	return m.creationParentName + "/" + slug
}

// insertCreatingNode puts the placeholder at the level being browsed.
//
// At the end of the level rather than sorted in: the default is the forge's own
// order, which is insertion order, so the row appears where a reader's eye is
// free — the bottom — instead of displacing rows above the cursor. Under a sort
// the table places it like any other row.
func (m *Model) insertCreatingNode(msg components.CreationFormSubmitMsg, target string, nodeType NodeType) {
	node := &TreeNode{
		Name:       msg.Name,
		FullPath:   target,
		Type:       nodeType,
		Creating:   true,
		Visibility: msg.Visibility,
		Parent:     m.currentGroupNode,
	}
	if m.currentGroupNode == nil {
		m.nodes = append(m.nodes, node)
	} else {
		m.currentGroupNode.Children = append(m.currentGroupNode.Children, node)
	}
	m.updateTableRows()
}

// replaceCreating swaps the placeholder keyed on target for what the forge
// answered, or drops it when replacement is nil.
//
// It walks the loaded tree rather than the current level: the user is free to
// drill elsewhere while the request is in flight, and the row belongs to the
// level it was made in.
func replaceCreating(nodes []*TreeNode, target string, replacement *TreeNode) ([]*TreeNode, bool) {
	for i, node := range nodes {
		if node.Creating && node.FullPath == target {
			if replacement == nil {
				return append(nodes[:i], nodes[i+1:]...), true
			}
			replacement.Parent = node.Parent
			nodes[i] = replacement
			return nodes, true
		}
		if children, ok := replaceCreating(node.Children, target, replacement); ok {
			node.Children = children
			return nodes, true
		}
	}
	return nodes, false
}

// carryOverCreating keeps the placeholders of a level that is being replaced.
//
// A reload swaps the level wholesale — `ctrl+r` at the root, a ChildrenLoadedMsg
// inside a group — and a create still in flight has nothing yet to be replaced
// by. Dropped here, its row would vanish mid-request while the registry went on
// tracking the run: `:jobs` would show work the tree had stopped admitting to,
// and settleCreating would have nothing left to resolve when the answer came.
func carryOverCreating(previous, fresh []*TreeNode) []*TreeNode {
	for _, node := range previous {
		if node.Creating {
			fresh = append(fresh, node)
		}
	}
	return fresh
}

// settleCreating resolves the placeholder and rebuilds the rows.
func (m *Model) settleCreating(target string, replacement *TreeNode) {
	m.nodes, _ = replaceCreating(m.nodes, target, replacement)
	m.updateTableRows()
}

// selectRow puts the cursor on the row for path, if it is on screen. It is what
// replaces the pendingSelectPath round trip: the node is already in the tree,
// so there is no refresh to wait for and no path to chase.
func (m *Model) selectRow(path string) {
	for i, row := range m.table.Visible() {
		if row.node.FullPath == path {
			m.table.SetCursor(i)
			return
		}
	}
}

// createGroup creates a new namespace on the forge.
func (m Model) createGroup(msg components.CreationFormSubmitMsg, target string) tea.Cmd {
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
			return GroupCreatedMsg{Target: target, Error: err}
		}
		return GroupCreatedMsg{Namespace: ns, Target: target}
	}
}

// slugify turns a display name into the path segment a forge wants.
func slugify(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

// createProject creates a new repository and optionally applies a template.
//
// The template apply is inside the same command, and therefore inside the same
// run item: it is several more requests — a download and an initial commit — and
// splitting them would put a row back to idle while the slower half was still
// going.
func (m Model) createProject(msg components.CreationFormSubmitMsg, target string) tea.Cmd {
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
			return ProjectCreatedMsg{Target: target, Error: err}
		}

		// Apply template if one was selected and resolved
		if templateRepo != "" {
			templateErr := applyTemplate(backend, repo.ID, registryURL, username, password, templateRepo, templateTag)
			if templateErr != nil {
				return ProjectCreatedMsg{Repository: repo, Target: target, TemplateError: templateErr}
			}
		}

		return ProjectCreatedMsg{Repository: repo, Target: target}
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

// handleGroupCreated resolves the placeholder row in place.
//
// No refresh: the forge has just told us what it made, so re-listing the whole
// tree to learn it would empty the table for the length of a second round trip.
// It is also what stops a create outliving a context switch from reloading the
// tree of the context it switched to.
//
// A failure goes to the footer rather than to m.error, which would replace the
// tree with an error screen — the one place the removed row cannot be seen.
func (m Model) handleGroupCreated(msg GroupCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [explorer] create namespace: %v", msg.Error)
		m.settleCreating(msg.Target, nil)
		return m, m.footer.Error("Failed to create " + msg.Target + " — check logs")
	}

	node := nodeFromNamespace(msg.Namespace, nil)
	m.settleCreating(msg.Target, node)
	m.selectRow(node.FullPath)
	return m, nil
}

// handleProjectCreated resolves the placeholder row in place. See
// handleGroupCreated for why there is no refresh.
func (m Model) handleProjectCreated(msg ProjectCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [explorer] create repository: %v", msg.Error)
		m.settleCreating(msg.Target, nil)
		return m, m.footer.Error("Failed to create " + msg.Target + " — check logs")
	}

	// The project exists whatever the template did, so the row settles either
	// way; only the message differs.
	node := nodeFromRepository(msg.Repository, nil)
	m.settleCreating(msg.Target, node)
	m.selectRow(node.FullPath)

	if msg.TemplateError != nil {
		log.Printf("ERROR [explorer] apply template: %v", msg.TemplateError)
		return m, m.footer.Warn(fmt.Sprintf("%s created, but the template did not apply — check logs", node.Name))
	}
	return m, nil
}
