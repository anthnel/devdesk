package explorer

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

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
	table   datatable.Model[*TreeNode]
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

	// Path to select after refresh (for newly created items)
	pendingSelectPath string
}

// New crée une nouvelle instance du modèle explorer
func New(cfg *config.Config, sharedState *shared.State) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	return Model{
		config:  cfg,
		shared:  sharedState,
		nodes:   []*TreeNode{},
		loading: false,
		mode:    ModeNormal,
		table: datatable.New(datatable.Config[*TreeNode]{
			Columns:    explorerColumns(),
			SortColumn: columnType,
		}),
		spinner: s,
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
	return m.creationForm != nil || m.mode != ModeNormal || m.table.InEditMode()
}

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	if !m.table.FilterBar().IsVisible() || m.loading || m.error != "" || len(m.nodes) == 0 {
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
	// Rule 124: the footer (tab bar + filter bar) is outside the viewport, so
	// only the table's own header row is subtracted. The Rule 116 arithmetic is
	// the component's — seven ratios and a remainder used to live here.
	m.table.Resize(width, max(height-1, 1))
}
