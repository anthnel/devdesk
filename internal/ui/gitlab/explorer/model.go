package explorer

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
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
