package workspaces

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ViewMode represents the current mode of the view
type ViewMode int

const (
	ModeNormal ViewMode = iota
	ModeAdding
	ModeConfirmingDelete
	ModeRenaming
	ModeSelecting // Selecting a directory for another view (e.g., security)
)

// Model représente le modèle de la vue Workspaces
type Model struct {
	config *config.Config
	width  int
	height int

	entries []Entry
	table   table.Model
	error   string

	// Navigation state (drill-down like explorer)
	currentPath     string   // Empty = root (workspaces list), otherwise = current directory path
	navigationStack []string // Stack of parent paths for breadcrumb tabs
	cursorStack     []int    // Cursor positions per level for restoration on navigate-up
	pendingCursor   int      // Cursor to restore after async loadEntries (-1 = none)

	// Tab navigation
	activeTabIndex int

	// Mode and components
	mode         ViewMode
	input        *WorkspaceInput
	confirmModal *sharedcomponents.ConfirmModal
	selectedIdx  int

	// Spinner for scanning animation
	spinner         spinner.Model
	spinnerFrameIdx int

	// Scan cache: keyed by absolute repo path
	scanCache map[string]cache.WorkspaceScanEntry

	// Paths currently being scanned (keyed by absolute path)
	scanningPaths map[string]bool

	// footerError holds a short scan error message for the footer info line (Rule 128)
	footerError string
	// footerInfo holds a transient informational message for the footer info line
	footerInfo string

	// selectionMessage is displayed in the footer when in ModeSelecting
	selectionMessage string

	// filterBar provides text search for the table (Rule 136)
	filterBar sharedcomponents.FilterBar
}

// Entry represents a file system entry with enriched metadata
type Entry struct {
	Name    string
	Path    string
	ModTime time.Time
	IsDir   bool

	// Git metadata (populated only for git repos)
	IsGitRepo    bool
	SubRepoPaths []string // git repos nested within this directory (if !IsGitRepo)
	GitBranch    string
	GitRemote    string // remote path without server URL (e.g. "group/project")
	GitRemoteURL string // full remote URL normalized for browser opening
	GitModified  int
	GitUntracked int
	GitUnpushed  int
	GitUnpulled  int

	// Project metadata
	ProjectType string // "Go", "Node", "Python", "Rust", "Java", etc.
}

// New crée une nouvelle instance du modèle workspaces
func New(cfg *config.Config) Model {
	columns := defaultColumns()

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
		config:        cfg,
		entries:       []Entry{},
		table:         t,
		mode:          ModeNormal,
		pendingCursor: -1,
		spinner:       s,
		scanCache:     make(map[string]cache.WorkspaceScanEntry),
		scanningPaths: make(map[string]bool),
		filterBar:     sharedcomponents.NewFilterBar(),
	}
}

// NewForSelection creates a workspaces view in selection mode for picking a directory
func NewForSelection(cfg *config.Config, message string) Model {
	m := New(cfg)
	m.mode = ModeSelecting
	m.selectionMessage = message
	return m
}
