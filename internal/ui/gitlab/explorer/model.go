package explorer

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/gitlab"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// sortField defines which column to sort by
type sortField int

const (
	sortByType sortField = iota
	sortByName
	sortByVisibility
	sortByCreated
	sortByActivity
)

// sortableColumns lists columns in cycle order for the '.' key
var sortableColumns = []sortField{
	sortByType,
	sortByName,
	sortByVisibility,
	sortByCreated,
	sortByActivity,
}

// ViewMode represents the current mode of the view
type ViewMode int

const (
	ModeNormal ViewMode = iota
	ModePulling
	ModeShowingReport
	ModeLoadingTemplates
	ModeCreatingProject
	ModeConfirmingDelete
)

// Model représente le modèle de la vue GitLab Explorer
type Model struct {
	config *config.Config
	shared *shared.State
	width  int
	height int

	// Table
	table   table.Model
	spinner spinner.Model

	// Tree state
	nodes         []*TreeNode
	loading       bool
	firstLoadDone bool // true after the first load attempt completes (success or error)
	error         string

	// Drill-down navigation
	currentGroupNode *TreeNode   // nil = root level
	navigationStack  []*TreeNode // Stack of parent groups for backspace navigation
	cursorStack      []int       // Cursor positions per level for restoration on drill-up

	// Pull mode state
	mode           ViewMode
	reportModal    *components.ReportModal
	pullTargetNode *TreeNode
	pullStatus     string

	// Creation mode state
	creationForm       *components.CreationForm
	creationParentName string              // Parent stashed during template loading
	creationParentID   int64               // Parent ID stashed during template loading
	templateEntries    []oci.TemplateEntry // Loaded template entries for repo+tag resolution

	// Tab navigation
	activeTabIndex int // Focused tab index (last tab = current level)

	// Delete mode state
	deleteConfirmModal *components.DeleteConfirmModal
	deleteTargetNode   *TreeNode
	footerError        string // transient action error shown in footer (Rule 128)

	// Sorting
	sortColumn sortField
	sortAsc    bool

	// filterBar provides text search for the table (Rule 136)
	filterBar components.FilterBar

	// Path to select after refresh (for newly created items)
	pendingSelectPath string
}

// New crée une nouvelle instance du modèle explorer
func New(cfg *config.Config, sharedState *shared.State) Model {
	columns := []table.Column{
		{Title: "Type", Width: 10},
		{Title: "Name", Width: 25},
		{Title: "Slug", Width: 20},
		{Title: "Visibility", Width: 12},
		{Title: "Role", Width: 12},
		{Title: "Created", Width: 14},
		{Title: "Activity", Width: 14},
		{Title: "CI", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(theme.DefaultTableStyles())

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	return Model{
		config:     cfg,
		shared:     sharedState,
		nodes:      []*TreeNode{},
		loading:    false,
		mode:       ModeNormal,
		table:      t,
		spinner:    s,
		sortColumn: sortByType,
		sortAsc:    true,
		filterBar:  components.NewFilterBar(),
	}
}

// Init initialise le modèle
func (m Model) Init() tea.Cmd {
	// Charger les groupes racine au démarrage
	if m.shared.GitLabClient != nil {
		m.loading = true
		return tea.Batch(m.spinner.Tick, m.loadRootGroups())
	}
	return nil
}

// InEditMode returns true if the view is in an edit mode or filter search
func (m Model) InEditMode() bool {
	return m.creationForm != nil || m.mode != ModeNormal || m.filterBar.InEditMode()
}

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	if !m.filterBar.IsVisible() || m.loading || m.error != "" || len(m.nodes) == 0 {
		return false
	}
	switch m.mode {
	case ModePulling, ModeLoadingTemplates, ModeCreatingProject, ModeConfirmingDelete, ModeShowingReport:
		return false
	}
	return m.creationForm == nil
}

// Update gère les messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize(msg.Width, msg.Height)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case RootGroupsLoadedMsg:
		m.loading = false
		m.firstLoadDone = true
		m.nodes = msg.Nodes
		m.error = ""
		m.updateTableRows()
		m.table.GotoTop()
		// If there's a pending selection, expand path to it
		if m.pendingSelectPath != "" {
			return m.expandToPath(m.pendingSelectPath)
		}

	case ChildrenLoadedMsg:
		return m.handleChildrenLoaded(msg)

	case LoadErrorMsg:
		return m.handleLoadError(msg)

	case PullDestinationSelectedMsg:
		return m.handlePullDestinationSelected(msg)

	case PullSelectionCancelledMsg:
		m.pullTargetNode = nil
		return m, nil

	case PullCompleteMsg:
		return m.handlePullComplete(msg)

	case components.ReportModalCloseMsg:
		m.mode = ModeNormal
		m.reportModal = nil
		return m, nil

	case components.CreationFormSubmitMsg:
		return m.handleCreationSubmit(msg)

	case components.CreationFormCancelMsg:
		m.mode = ModeNormal
		m.creationForm = nil
		return m, nil

	case TemplatesLoadedMsg:
		return m.handleTemplatesLoaded(msg)

	case GroupCreatedMsg:
		return m.handleGroupCreated(msg)

	case ProjectCreatedMsg:
		return m.handleProjectCreated(msg)

	case components.DeleteConfirmModalYesMsg:
		return m.handleDeleteConfirmed(msg.PermanentlyRemove)

	case components.DeleteConfirmModalNoMsg:
		m.mode = ModeNormal
		m.deleteConfirmModal = nil
		m.deleteTargetNode = nil
		return m, nil

	case DeleteCompleteMsg:
		return m.handleDeleteComplete(msg)

	case BrowserOpenedMsg:
		if msg.Error != nil {
			log.Printf("ERROR [explorer] open browser: %v", msg.Error)
			m.footerError = "Failed to open browser — check logs"
			return m, clearFooterErrorCmd()
		}
		return m, nil

	case clearFooterErrorMsg:
		m.footerError = ""
		return m, nil
	}

	return m, nil
}

// View is implemented in view.go

// handleKeyMsg handles keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter bar search mode - prioritaire
	if m.filterBar.InEditMode() {
		var cmd tea.Cmd
		m.filterBar, cmd = m.filterBar.Update(msg)
		m.updateTableRows()
		return m, cmd
	}

	// Handle mode-specific input
	switch m.mode {
	case ModeShowingReport:
		if m.reportModal != nil {
			var cmd tea.Cmd
			m.reportModal, cmd = m.reportModal.Update(msg)
			return m, cmd
		}
	case ModePulling, ModeLoadingTemplates:
		// Ignore input while pulling or loading templates
		return m, nil
	case ModeCreatingProject:
		if m.creationForm != nil {
			var cmd tea.Cmd
			m.creationForm, cmd = m.creationForm.Update(msg)
			return m, cmd
		}
	case ModeConfirmingDelete:
		if m.deleteConfirmModal != nil {
			var cmd tea.Cmd
			m.deleteConfirmModal, cmd = m.deleteConfirmModal.Update(msg)
			return m, cmd
		}
	}

	// Normal mode — resolve actions against exactly what the table displays
	items := m.visibleItems()

	switch msg.String() {
	case "/":
		return m, m.filterBar.ActivateSearch()
	case "left", "h":
		return m.handleDrillUp()
	case "right", "l":
		return m.handleDrillDown(items)
	case "esc":
		return m.handleDrillUp()
	case "ctrl+r":
		return m.handleRefresh()
	case "p":
		return m.handlePullStart(items)
	case "ctrl+n":
		return m.handleCreateResource(items)
	case "ctrl+d":
		return m.handleDeleteStart(items)
	case "ctrl+w":
		return m.handleOpenInBrowser(items)
	case ".":
		return m.cycleSort()
	}

	// Delegate navigation keys (up/down/j/k/pgup/pgdown/home/end) to table
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// tabCount returns the total number of tabs (home + navigation stack + current group)
func (m Model) tabCount() int {
	count := 1 // home tab
	for _, node := range m.navigationStack {
		if node != nil {
			count++
		}
	}
	if m.currentGroupNode != nil {
		count++
	}
	return count
}

// resize adjusts table dimensions based on terminal size
func (m *Model) resize(width, height int) {
	m.filterBar.Resize(width)
	// Rule 124: footer (tab bar + filter bar) is outside the viewport; subtract table header only
	tableDataHeight := height - 1
	if tableDataHeight < 1 {
		tableDataHeight = 1
	}
	m.table.SetHeight(tableDataHeight)

	// Column widths (Rule 116: contentWidth = width - 2, available = contentWidth - numColumns*2)
	contentWidth := width - 2
	available := contentWidth - numColumns*2

	columns := m.table.Columns()
	if len(columns) >= numColumns {
		columns[0].Width = int(float64(available) * colTypeRatio)
		columns[1].Width = int(float64(available) * colNameRatio)
		columns[2].Width = int(float64(available) * colSlugRatio)
		columns[3].Width = int(float64(available) * colVisibilityRatio)
		columns[4].Width = int(float64(available) * colRoleRatio)
		columns[5].Width = int(float64(available) * colCreatedRatio)
		columns[6].Width = int(float64(available) * colActivityRatio)
		// Last column gets the remainder
		columns[7].Width = available - columns[0].Width - columns[1].Width - columns[2].Width - columns[3].Width - columns[4].Width - columns[5].Width - columns[6].Width
		m.table.SetColumns(columns)
	}
}

// visibleItems returns the nodes the table is showing: the current level, sorted
// and filtered. Every action resolves the cursor through this, so the row the
// user is looking at is the row that gets acted on.
func (m *Model) visibleItems() []*TreeNode {
	items := m.sortedItems(m.currentItems())
	query := strings.ToLower(m.filterBar.SearchQuery())
	if query == "" {
		return items
	}

	visible := make([]*TreeNode, 0, len(items))
	for _, node := range items {
		nameMatch := strings.Contains(strings.ToLower(node.Name), query)
		pathMatch := strings.Contains(strings.ToLower(node.FullPath), query)
		if nameMatch || pathMatch {
			visible = append(visible, node)
		}
	}
	return visible
}

// updateTableRows rebuilds table rows from the visible items
func (m *Model) updateTableRows() {
	items := m.visibleItems()
	rows := make([]table.Row, 0, len(items))
	for _, node := range items {
		rows = append(rows, table.Row{
			nodeTypeLabel(node),
			node.Name,
			nodeSlug(node.FullPath),
			visibilityLabel(node),
			node.AccessLevelName(),
			timeAgo(node.CreatedAt),
			timeAgo(node.LastActivityAt),
			pipelineStatusLabel(node),
		})
	}
	m.table.SetRows(rows)

	// bubbles does not clamp the cursor when the row count shrinks, so narrowing
	// the filter would leave it past the end. Nothing is highlighted then, and
	// every action that resolves the selection silently does nothing.
	if m.table.Cursor() >= len(rows) {
		m.table.SetCursor(max(len(rows)-1, 0))
	}

	m.updateSortIndicators()
}

// cycleSort cycles through sort options: each column asc then desc, then next column
func (m Model) cycleSort() (tea.Model, tea.Cmd) {
	if m.sortAsc {
		m.sortAsc = false
	} else {
		m.sortAsc = true
		nextIdx := 0
		for i, col := range sortableColumns {
			if col == m.sortColumn {
				nextIdx = (i + 1) % len(sortableColumns)
				break
			}
		}
		m.sortColumn = sortableColumns[nextIdx]
	}
	m.updateTableRows()
	return m, nil
}

// sortedItems returns nodes sorted by the current sort column and direction
func (m *Model) sortedItems(items []*TreeNode) []*TreeNode {
	if len(items) == 0 {
		return items
	}

	sorted := make([]*TreeNode, len(items))
	copy(sorted, items)

	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]

		var less bool
		switch m.sortColumn {
		case sortByName:
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case sortByVisibility:
			less = strings.ToLower(a.Visibility) < strings.ToLower(b.Visibility)
		case sortByCreated:
			less = timeBefore(a.CreatedAt, b.CreatedAt)
		case sortByActivity:
			less = timeBefore(a.LastActivityAt, b.LastActivityAt)
		default: // sortByType
			less = string(a.Type) < string(b.Type)
		}

		if m.sortAsc {
			return less
		}
		return !less
	})

	return sorted
}

// timeBefore compares two nullable times (nil is considered "oldest")
func timeBefore(a, b *time.Time) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil {
		return true
	}
	if b == nil {
		return false
	}
	return a.Before(*b)
}

// updateSortIndicators updates column headers with ▲/▼ sort arrows
func (m *Model) updateSortIndicators() {
	sortColIndex := map[sortField]int{
		sortByType:       0,
		sortByName:       1,
		sortByVisibility: 3,
		sortByCreated:    5,
		sortByActivity:   6,
	}
	baseTitles := map[int]string{
		0: "Type",
		1: "Name",
		3: "Visibility",
		5: "Created",
		6: "Activity",
	}

	columns := m.table.Columns()
	if len(columns) < numColumns {
		return
	}

	// Reset sortable column titles
	for idx, title := range baseTitles {
		columns[idx].Title = title
	}

	// Add arrow to active sort column
	if idx, ok := sortColIndex[m.sortColumn]; ok {
		arrow := " ▲"
		if !m.sortAsc {
			arrow = " ▼"
		}
		columns[idx].Title = baseTitles[idx] + arrow
	}

	m.table.SetColumns(columns)
}

// handleDrillDown handles enter key - navigate into a group
func (m Model) handleDrillDown(items []*TreeNode) (tea.Model, tea.Cmd) {
	cursor := m.table.Cursor()
	if cursor >= len(items) {
		return m, nil
	}
	node := items[cursor]
	if node.Type != NodeTypeGroup {
		return m, nil
	}

	// Save cursor position before navigating down
	m.cursorStack = append(m.cursorStack, m.table.Cursor())
	// Push current group onto navigation stack
	m.navigationStack = append(m.navigationStack, m.currentGroupNode)
	m.currentGroupNode = node
	m.activeTabIndex = m.tabCount() - 1

	// Load children if not yet loaded
	if node.Children == nil {
		node.Loading = true
		m.loading = true
		m.updateTableRows()
		return m, tea.Batch(m.spinner.Tick, m.loadChildren(node))
	}
	m.updateTableRows()
	m.table.GotoTop()
	return m, nil
}

// handleDrillUp handles backspace key - navigate back to parent
func (m Model) handleDrillUp() (tea.Model, tea.Cmd) {
	if len(m.navigationStack) == 0 {
		return m, nil
	}
	// Pop from stack
	m.currentGroupNode = m.navigationStack[len(m.navigationStack)-1]
	m.navigationStack = m.navigationStack[:len(m.navigationStack)-1]
	m.activeTabIndex = m.tabCount() - 1
	m.updateTableRows()
	// Restore cursor position at the parent level
	if len(m.cursorStack) > 0 {
		cursor := m.cursorStack[len(m.cursorStack)-1]
		m.cursorStack = m.cursorStack[:len(m.cursorStack)-1]
		m.table.SetCursor(cursor)
	} else {
		m.table.GotoTop()
	}
	return m, nil
}

// currentItems returns the children of the current drill-down group (or root nodes)
func (m Model) currentItems() []*TreeNode {
	if m.currentGroupNode == nil {
		return m.nodes
	}
	if m.currentGroupNode.Children == nil {
		return nil
	}
	return m.currentGroupNode.Children
}

// handleRefresh handles r key
func (m Model) handleRefresh() (tea.Model, tea.Cmd) {
	m.loading = true
	m.nodes = []*TreeNode{}
	m.currentGroupNode = nil
	m.navigationStack = nil
	m.cursorStack = nil
	m.activeTabIndex = 0
	m.table.SetRows(nil)
	return m, tea.Batch(m.spinner.Tick, m.loadRootGroups())
}

// handlePullStart handles p key - start pull operation
func (m Model) handlePullStart(flatNodes []*TreeNode) (tea.Model, tea.Cmd) {
	cursor := m.table.Cursor()
	if cursor >= len(flatNodes) {
		return m, nil
	}

	node := flatNodes[cursor]
	m.pullTargetNode = node
	return m, func() tea.Msg { return PullSelectionRequestMsg{} }
}

// handleChildrenLoaded handles ChildrenLoadedMsg
func (m Model) handleChildrenLoaded(msg ChildrenLoadedMsg) (tea.Model, tea.Cmd) {
	msg.ParentNode.Children = msg.Children
	msg.ParentNode.Expanded = true
	msg.ParentNode.Loading = false
	m.loading = false

	m.updateTableRows()
	m.table.GotoTop()

	// Continue expanding if there's a pending path
	if m.pendingSelectPath != "" {
		return m.expandToPath(m.pendingSelectPath)
	}

	return m, nil
}

// handleLoadError handles LoadErrorMsg
func (m Model) handleLoadError(msg LoadErrorMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m.firstLoadDone = true
	m.error = msg.Error.Error()
	if msg.ParentNode != nil {
		msg.ParentNode.Loading = false
	}
	return m, nil
}

// handlePullDestinationSelected handles workspace directory selection result from app
func (m Model) handlePullDestinationSelected(msg PullDestinationSelectedMsg) (tea.Model, tea.Cmd) {
	m.mode = ModePulling
	m.pullStatus = "Pulling..."

	node := m.pullTargetNode
	targetPath := msg.Path
	client := m.shared.GitLabClient
	cloneMethod := m.config.GitLab.CloneMethod
	gitlabURL := m.config.GitLab.URL

	return m, func() tea.Msg {
		report := m.recursivePull(client, node, targetPath, cloneMethod, gitlabURL)
		return PullCompleteMsg{Report: report}
	}
}

// handlePullComplete handles PullCompleteMsg
func (m Model) handlePullComplete(msg PullCompleteMsg) (tea.Model, tea.Cmd) {
	m.mode = ModeShowingReport
	m.pullTargetNode = nil
	m.pullStatus = ""
	m.reportModal = components.NewReportModal("Pull Complete", msg.Report)
	return m, nil
}

// recursivePull performs a recursive pull/clone operation
func (m Model) recursivePull(client *gitlabclient.Client, node *TreeNode, targetPath, cloneMethod, gitlabURL string) components.PullReport {
	report := components.PullReport{
		Cloned:  []string{},
		Skipped: []string{},
		Errors:  []string{},
	}

	m.recursivePullNode(client, node, targetPath, cloneMethod, gitlabURL, &report)
	return report
}

// recursivePullNode recursively processes a node
func (m Model) recursivePullNode(client *gitlabclient.Client, node *TreeNode, basePath, cloneMethod, gitlabURL string, report *components.PullReport) {
	if node.Type == NodeTypeProject {
		m.pullProject(node, basePath, cloneMethod, gitlabURL, report)
		return
	}

	// It's a group - create directory and process children using slug
	groupPath := filepath.Join(basePath, nodeSlug(node.FullPath))

	// Create directory
	if err := os.MkdirAll(groupPath, 0755); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("mkdir %s: %v", groupPath, err))
		return
	}

	// Load children if not already loaded
	if node.Children == nil {
		children, err := m.fetchGroupChildren(client, node)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("fetch %s: %v", node.FullPath, err))
			return
		}
		node.Children = children
	}

	// Process children recursively
	for _, child := range node.Children {
		m.recursivePullNode(client, child, groupPath, cloneMethod, gitlabURL, report)
	}
}

// pullProject clones or skips a single project
func (m Model) pullProject(node *TreeNode, basePath, cloneMethod, gitlabURL string, report *components.PullReport) {
	projectPath := filepath.Join(basePath, nodeSlug(node.FullPath))

	// Check if already exists
	if gitlab.DirExists(projectPath) {
		report.Skipped = append(report.Skipped, node.FullPath)
		return
	}

	// Build clone URL
	var cloneURL string
	cleanURL := strings.TrimSuffix(gitlabURL, "/")
	if cloneMethod == "ssh" {
		// Extract host from URL
		host := strings.TrimPrefix(cleanURL, "https://")
		host = strings.TrimPrefix(host, "http://")
		cloneURL = fmt.Sprintf("git@%s:%s.git", host, node.FullPath)
	} else {
		cloneURL = fmt.Sprintf("%s/%s.git", cleanURL, node.FullPath)
	}

	// Clone
	if err := gitlab.Clone(cloneURL, projectPath); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("clone %s: %v", node.FullPath, err))
		return
	}

	report.Cloned = append(report.Cloned, node.FullPath)
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

// loadTemplates loads available templates from the OCI registry catalog.
// Degrades gracefully: returns empty list if registry is not configured or on error.
func (m Model) loadTemplates() tea.Cmd {
	registryURL := m.config.Registry.URL
	basePath := m.config.Registry.TemplatesRepository
	username := m.config.Registry.Username
	password := m.config.Registry.Password

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
	password := m.config.Registry.Password

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

// expandToPath navigates to and selects a node at the given path after refresh
func (m Model) expandToPath(targetPath string) (tea.Model, tea.Cmd) {
	// Check if the target is visible in current items
	items := m.currentItems()
	for i, node := range items {
		if node.FullPath == targetPath {
			m.table.SetCursor(i)
			m.pendingSelectPath = ""
			return m, nil
		}
	}

	// Find an ancestor that needs to be drilled into
	for _, node := range items {
		if node.Type == NodeTypeGroup && strings.HasPrefix(targetPath, node.FullPath+"/") {
			// Drill into this group
			m.navigationStack = append(m.navigationStack, m.currentGroupNode)
			m.currentGroupNode = node

			if node.Children == nil {
				node.Loading = true
				m.updateTableRows()
				return m, m.loadChildren(node)
			}
			node.Expanded = true
			m.updateTableRows()
			return m.expandToPath(targetPath)
		}
	}

	// Could not find path - clear pending and stay where we are
	m.pendingSelectPath = ""
	return m, nil
}

// handleDeleteStart handles ctrl+d key - start delete operation
func (m Model) handleDeleteStart(flatNodes []*TreeNode) (tea.Model, tea.Cmd) {
	cursor := m.table.Cursor()
	if cursor >= len(flatNodes) {
		return m, nil
	}

	node := flatNodes[cursor]
	m.deleteTargetNode = node
	m.mode = ModeConfirmingDelete

	// Project already scheduled for deletion: only permanent removal is possible.
	if node.Type == NodeTypeProject && node.MarkedForDeletion {
		title := "Permanently Delete Project"
		message := fmt.Sprintf("'%s' is already scheduled for deletion.\n\nConfirm to permanently delete it now.", node.Name)
		m.deleteConfirmModal = components.NewDeleteConfirmModalPermanent(title, message)
		return m, nil
	}

	var title, message string
	if node.Type == NodeTypeGroup {
		title = "Delete Group"
		message = fmt.Sprintf("Are you sure you want to delete the group '%s'?\n\nThis will delete the group and ALL its contents\n(subgroups, projects, issues, etc.).", node.Name)
	} else {
		title = "Delete Project"
		message = fmt.Sprintf("Are you sure you want to delete the project '%s'?\n\nThis will delete the project and all its data\n(code, issues, merge requests, etc.).", node.Name)
	}

	m.deleteConfirmModal = components.NewDeleteConfirmModal(title, message)
	return m, nil
}

// handleDeleteConfirmed handles confirmation of delete
func (m Model) handleDeleteConfirmed(permanentlyRemove bool) (tea.Model, tea.Cmd) {
	m.mode = ModeNormal
	m.deleteConfirmModal = nil

	node := m.deleteTargetNode
	m.deleteTargetNode = nil

	if node == nil {
		return m, nil
	}

	client := m.shared.GitLabClient
	if client == nil {
		m.error = "GitLab client not initialized"
		return m, nil
	}

	nodeID := int(node.ID)
	nodeType := node.Type
	fullPath := node.FullPath

	return m, func() tea.Msg {
		var err error
		if nodeType == NodeTypeGroup {
			err = gitlab.DeleteGroup(client, nodeID, fullPath, permanentlyRemove)
		} else {
			err = gitlab.DeleteProject(client, nodeID, fullPath, permanentlyRemove)
		}
		return DeleteCompleteMsg{Error: err, DeletedNode: node}
	}
}

// handleDeleteComplete handles DeleteCompleteMsg
func (m Model) handleDeleteComplete(msg DeleteCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		log.Printf("ERROR [explorer] delete: %v", msg.Error)
		m.footerError = "Delete failed — check logs"
		return m, clearFooterErrorCmd()
	}
	m.footerError = ""

	// Remove deleted node from local tree and stay in current group
	if msg.DeletedNode != nil {
		if m.currentGroupNode != nil {
			children := m.currentGroupNode.Children
			for i, child := range children {
				if child.ID == msg.DeletedNode.ID {
					m.currentGroupNode.Children = append(children[:i], children[i+1:]...)
					break
				}
			}
		} else {
			for i, n := range m.nodes {
				if n.ID == msg.DeletedNode.ID {
					m.nodes = append(m.nodes[:i], m.nodes[i+1:]...)
					break
				}
			}
		}
	}
	m.updateTableRows()
	return m, nil
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

// Messages

// DeleteCompleteMsg is sent when a delete operation completes
type DeleteCompleteMsg struct {
	Error       error
	DeletedNode *TreeNode
}

// RootGroupsLoadedMsg est envoyé quand les groupes racine sont chargés
type RootGroupsLoadedMsg struct {
	Nodes []*TreeNode
}

// GroupCreatedMsg est envoyé quand un groupe est créé
type GroupCreatedMsg struct {
	Group *gitlabclient.Group
	Error error
}

// ProjectCreatedMsg est envoyé quand un projet est créé
type ProjectCreatedMsg struct {
	Project       *gitlabclient.Project
	Error         error
	TemplateError error // Non-nil if template application failed (project still exists)
}

// TemplatesLoadedMsg est envoyé quand les templates OCI sont chargées
type TemplatesLoadedMsg struct {
	Templates []oci.TemplateEntry
	Error     error
}

// ChildrenLoadedMsg est envoyé quand les enfants d'un nœud sont chargés
type ChildrenLoadedMsg struct {
	ParentNode *TreeNode
	Children   []*TreeNode
}

// LoadErrorMsg est envoyé en cas d'erreur de chargement
type LoadErrorMsg struct {
	Error      error
	ParentNode *TreeNode // Optional: node that was loading when error occurred
}

// PullCompleteMsg est envoyé quand l'opération de pull est terminée
type PullCompleteMsg struct {
	Report components.PullReport
}

// PullSelectionRequestMsg is sent to the app to open workspace selection for pull destination
type PullSelectionRequestMsg struct{}

// PullDestinationSelectedMsg is sent by the app when the user selected a workspace directory
type PullDestinationSelectedMsg struct {
	Path string
}

// PullSelectionCancelledMsg is sent by the app when the user cancelled workspace selection
type PullSelectionCancelledMsg struct{}

// handleOpenInBrowser opens the selected node's web URL in the default browser
func (m Model) handleOpenInBrowser(items []*TreeNode) (tea.Model, tea.Cmd) {
	cursor := m.table.Cursor()
	if cursor >= len(items) {
		return m, nil
	}
	url := items[cursor].WebURL
	if url == "" {
		return m, nil
	}
	return m, func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		return BrowserOpenedMsg{Error: cmd.Start()}
	}
}

// clearFooterErrorCmd clears the footer error after 3 seconds (Rule 128)
func clearFooterErrorCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearFooterErrorMsg{}
	})
}

// clearFooterErrorMsg is sent to clear the footer error after a delay
type clearFooterErrorMsg struct{}

// BrowserOpenedMsg is sent when the browser launch command has been started
type BrowserOpenedMsg struct {
	Error error
}

// nodeSlug returns the last path segment of a GitLab FullPath (the URL slug)
func nodeSlug(fullPath string) string {
	parts := strings.Split(fullPath, "/")
	return parts[len(parts)-1]
}
