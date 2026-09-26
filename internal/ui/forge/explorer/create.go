package explorer

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// handleCreateResource handles 'ctrl+n' key - start unified group/project creation.
// Stashes the parent info and opens the form.
func (m Model) handleCreateResource() (tea.Model, tea.Cmd) {
	// Same reasoning as below, one step earlier: without a session there is no
	// forge to create against, and the key is greyed for it.
	if a := m.connected(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}

	// GetShortcuts greys N while the level is loading, and Rule 130 forbids
	// greying a key that acts anyway. It matters more than the tidiness: the
	// parent's ID is read below, and a level still loading has none to give —
	// the form would open under an empty parent and create at the root.
	if m.loading {
		return m, m.footer.Warn(reasonStillLoading)
	}

	// Use the currently browsed group as parent, not the selected item.
	// currentGroupNode is nil at root level.
	m.creationParentName = ""
	m.creationParentID = ""
	m.creationParentVisibility = ""

	if m.currentGroupNode != nil {
		m.creationParentName = m.currentGroupNode.FullPath
		m.creationParentID = m.currentGroupNode.ID
		m.creationParentVisibility = m.currentGroupNode.Visibility
	}

	return m.openCreationForm(), nil
}

// openCreationForm builds the form from the stashed parent.
//
// It opens at once: the templates used to be listed from a registry first, which
// put a loading screen in front of every creation. The catalog is a local file
// read only when a template is chosen or applied, so there is nothing to wait for.
func (m Model) openCreationForm() Model {
	m.mode = ModeCreatingProject

	shape := m.shape()
	visibilities := shape.VisibilitiesUnder(m.creationParentVisibility)

	m.creationForm = components.NewCreationForm(
		0, // defaultResourceType = namespace (user can switch with ←→)
		m.creationParentName,
		m.creationParentID,
		m.config.Forge.DefaultVisibility,
		visibilities,
		m.vocab(),
	)
	// Narrower than the forge's own list means the parent did the narrowing
	// (VisibilitiesUnder is the identity otherwise), so say so — a single
	// remaining choice with no explanation reads as a control that is stuck
	// rather than one with nothing else to offer.
	if len(visibilities) < len(shape.Visibilities) {
		m.creationForm.SetVisibilityNote(fmt.Sprintf(
			"limited by the parent %s's visibility (%s)",
			strings.ToLower(m.vocab().Namespace), m.creationParentVisibility,
		))
	}
	return m
}

// handleTemplateChosen puts the picked template in the form that asked for it.
// A form that is no longer open — cancelled while the catalog was on screen is
// not possible, but a context switch drops the view — has nothing to receive it.
func (m Model) handleTemplateChosen(msg TemplateChosenMsg) (tea.Model, tea.Cmd) {
	if m.creationForm != nil {
		m.creationForm.SetTemplate(msg.Slug, msg.Name)
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
		ctx, cancel := context.WithTimeout(context.Background(), forgeCallTimeout)
		defer cancel()
		ns, err := backend.CreateNamespace(ctx, spec)
		if err != nil {
			return GroupCreatedMsg{Target: target, Error: err}
		}
		return GroupCreatedMsg{Namespace: ns, Target: target}
	}
}

// What a forge call may take before the run gives up on it. A create is not
// cancellable by the user (jobs.Kind.Cancellable — a request already sent cannot
// be un-sent), so the bound is what keeps a stalled connection from holding the
// row, and the target, busy for the life of the session.
//
// The initial commit gets far longer than the rest: GitHub takes one request per
// file, and a template may hold template.MaxFiles of them.
const (
	forgeCallTimeout     = time.Minute
	initialCommitTimeout = 10 * time.Minute
)

// slugify turns a display name into the path segment a forge wants.
func slugify(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

// createProject creates a new repository and, when a template was chosen, fills
// it with the template's files.
//
// The template is fetched **before** the repository is created. A bad ref, a
// network failure, a refused login or a template over the size limits are the
// ordinary ways this fails, and each of them used to leave an empty repository
// behind and a message about the template. Fetching first means those failures
// create nothing. What can still fail after the repository exists is the
// initial commit itself, and that keeps the old outcome: the repository stays,
// empty, and the footer says the template did not apply.
//
// It is all inside the same command, and therefore inside the same run item:
// splitting it would put a row back to idle while the slower half was going.
func (m Model) createProject(msg components.CreationFormSubmitMsg, target string) tea.Cmd {
	backend := m.shared.Forge
	spec := forge.NewRepository{
		Name:        msg.Name,
		Slug:        slugify(msg.Name),
		Description: msg.Description,
		Visibility:  msg.Visibility,
		NamespaceID: msg.ParentID,
	}
	fetcher := m.templateFetcher(msg.Template)

	return func() tea.Msg {
		ctx := context.Background()

		files, err := fetcher(ctx)
		if err != nil {
			log.Printf("ERROR [explorer] fetch template: %v", err)
			return ProjectCreatedMsg{Target: target, Error: err, TemplateUnavailable: true}
		}

		createCtx, cancelCreate := context.WithTimeout(ctx, forgeCallTimeout)
		defer cancelCreate()
		repo, err := backend.CreateRepository(createCtx, spec)
		if err != nil {
			return ProjectCreatedMsg{Target: target, Error: err}
		}

		if len(files) > 0 {
			commitCtx, cancelCommit := context.WithTimeout(ctx, initialCommitTimeout)
			defer cancelCommit()
			if err := backend.InitialCommit(commitCtx, repo.ID, files); err != nil {
				return ProjectCreatedMsg{Repository: repo, Target: target, TemplateError: err}
			}
		}
		return ProjectCreatedMsg{Repository: repo, Target: target}
	}
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
	return m, indexCreated(node)
}

// handleProjectCreated resolves the placeholder row in place. See
// handleGroupCreated for why there is no refresh.
func (m Model) handleProjectCreated(msg ProjectCreatedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [explorer] create repository: %v", msg.Error)
		m.settleCreating(msg.Target, nil)
		if msg.TemplateUnavailable {
			return m, m.footer.Error("Could not fetch the template — " + msg.Target + " was not created. Check logs")
		}
		return m, m.footer.Error("Failed to create " + msg.Target + " — check logs")
	}

	// The project exists whatever the template did, so the row settles either
	// way; only the message differs.
	node := nodeFromRepository(msg.Repository, nil)
	m.settleCreating(msg.Target, node)
	m.selectRow(node.FullPath)

	if msg.TemplateError != nil {
		log.Printf("ERROR [explorer] apply template: %v", msg.TemplateError)
		return m, tea.Batch(indexCreated(node),
			m.footer.Warn(fmt.Sprintf("%s created empty: the template's commit failed — check logs", node.Name)))
	}
	return m, indexCreated(node)
}

// indexCreated tells the forge index about a node the forge just created, so
// "g" finds it without waiting for the next walk. replaceCreating has already
// given it the placeholder's parent.
func indexCreated(node *TreeNode) tea.Cmd {
	entry := entryFromNode(node, pathOf(node.Parent))
	return editIndex(func(ix *forgeindex.Index) *forgeindex.Index { return ix.With(entry) })
}
