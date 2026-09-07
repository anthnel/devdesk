package explorer

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
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
	// ModeSelecting is the clone selection: the same tree, with a checkbox on
	// every row and `Space` ticking it (§3.16, decision 8).
	ModeSelecting
	// ModeCloning is the discovery-and-clone pipeline: a flat list of what has
	// been found, each row carrying its own state. It replaced the modal that
	// said "Pulling..." for however many minutes the subtree took (decision 9).
	ModeCloning
	ModeLoadingTemplates
	ModeCreatingProject
	ModeConfirmingDelete
)

// Model represents the model of the GitLab Explorer view
type Model struct {
	config *config.Config
	shared *shared.State
	width  int
	height int

	// Table
	table   datatable.Model[explorerRow]
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

	mode ViewMode

	// Clone selection and the run it starts. The selection outlives every
	// drill-down — it names paths, not rows — and survives the round trip
	// through the workspaces view that picks the destination.
	selection cloneSelection
	// selectionNodes is where the walk starts from, one node per root. The
	// selection cannot hold them: it has to answer for paths nobody has fetched,
	// which is the whole reason it is paths and exclusions (decision 11). And a
	// root ticked three levels down is no longer on screen once the user has come
	// back up, so the reference has to be kept when it is made.
	selectionNodes map[string]*TreeNode
	clone          *cloneList

	// Creation mode state
	creationForm       *components.CreationForm
	creationParentName string              // Parent stashed during template loading
	creationParentID   string              // Parent ID stashed during template loading
	templateEntries    []oci.TemplateEntry // Loaded template entries for repo+tag resolution

	// Tab navigation
	activeTabIndex int // Focused tab index (last tab = current level)

	// Delete mode state
	deleteConfirmModal *components.OptionConfirmModal
	deleteTargetNode   *TreeNode
	// jobs is the router's last snapshot of the registry, and jobFrame the
	// spinner frame that goes with it — bare, because it lands in a table cell
	// and a cell is measured before it is styled (Rule 122).
	//
	// The frame comes from the broadcast rather than from this view's own
	// spinner because that chain stops when the tree settles: a create started
	// on a loaded tree would sit on frame zero, which reads as a hang. It is
	// the same reason the clone rows take it (D5).
	jobs     []jobs.Run
	jobFrame string

	// Transient footer messages, both cleared by the same 3s timer (Rule 128).
	// footer is the one line of transient state below the viewport (Rule 128).
	footer components.FooterMessage
}

// New creates a new instance of the explorer model
func New(cfg *config.Config, sharedState *shared.State) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	return Model{
		config:    cfg,
		shared:    sharedState,
		nodes:     []*TreeNode{},
		loading:   false,
		mode:      ModeNormal,
		selection: newCloneSelection(),
		table: datatable.New(datatable.Config[explorerRow]{
			Columns: explorerColumns(),
			// The forge's own order: loadChildren stacks the namespaces, then
			// the repositories. It used to be a sort by node type, which on
			// that list is the identity — so the opening screen is unchanged,
			// and what is gained is that `.` now has "no sort" as a stop, so
			// the forge's order is reachable again after cycling away from it.
			SortColumn: -1,
		}),
		spinner: s,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	// Load the root groups at startup
	if m.shared.IsAuthenticated {
		m.loading = true
		return tea.Batch(m.spinner.Tick, m.loadRootGroups())
	}
	return nil
}

// InEditMode returns true if the view is in an edit mode or filter search.
//
// `m.mode != ModeNormal` already covers both clone modes, which is what it has
// to do: `c`, `space` and `esc` all mean something there, and a bare `:` would
// otherwise open command mode over a selection in progress.
func (m Model) InEditMode() bool {
	return m.creationForm != nil || m.mode != ModeNormal || m.table.InEditMode()
}

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	if m.mode == ModeCloning && m.clone != nil {
		return m.clone.table.FilterBar().IsVisible()
	}
	return m.showsTree() && m.table.FilterBar().IsVisible()
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
	if m.clone != nil {
		m.clone.table.Resize(width, max(height-1, 1))
	}
}
