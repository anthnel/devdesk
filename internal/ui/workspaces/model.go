package workspaces

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ViewMode represents the current mode of the view
type ViewMode int

const (
	ModeNormal ViewMode = iota
	ModeAdding
	ModeConfirmingDelete
	ModeRenaming
	ModeSelecting         // Selecting a directory for another view (e.g., security)
	ModeConfirmingScanAll // A asked to scan everything; the modal carries the purge option
)

// Model représente le modèle de la vue Workspaces
type Model struct {
	config *config.Config
	width  int
	height int

	table datatable.Model[workspaceRow]
	error string

	// Navigation state (drill-down like explorer)
	currentPath string // Empty = root (workspaces list), otherwise = current directory path
	// listingPath is the directory the rows currently in the table were read
	// from. It equals currentPath exactly when the table shows where the view
	// says it is; between a navigation and the load landing, it does not — and
	// that difference is the one thing that says "these rows are not this
	// directory's".
	listingPath     string
	navigationStack []string // Stack of parent paths for breadcrumb tabs
	cursorStack     []int    // Cursor positions per level for restoration on navigate-up
	pendingCursor   int      // Cursor to restore after async loadEntries (-1 = none)

	// Tab navigation
	activeTabIndex int

	// Mode and components
	mode         ViewMode
	input        *WorkspaceInput
	confirmModal *sharedcomponents.ConfirmModal
	// scanAllModal carries A's purge checkbox. Separate from confirmModal
	// because the two answer different messages, and one field holding either
	// would make the handler guess which question was asked.
	scanAllModal *sharedcomponents.OptionConfirmModal
	// pendingEntry is what the open modal or rename form is about. It holds the
	// entry rather than its row index: the index meant nothing once the list it
	// indexed stopped being the list on screen (D24).
	pendingEntry *Entry

	// Spinner for scanning animation
	spinner         spinner.Model
	spinnerFrameIdx int

	// Scan cache: keyed by absolute repo path
	scanCache map[string]cache.WorkspaceScanEntry

	// Paths currently being scanned (keyed by absolute path)
	scanningPaths map[string]bool

	// Paths currently being synced (keyed by absolute path). Separate from
	// scanningPaths because the two are mutually exclusive per repository, and
	// knowing which one holds it is what lets the view say so.
	syncingPaths map[string]bool

	// Paths currently being deleted (keyed by absolute path). A third map for
	// the same reason the first two are separate: the view says which
	// operation holds the row, and a delete is the one the user must not
	// re-issue — os.RemoveAll on an already-removed path fails, and reporting
	// that failure would deny a deletion that in fact succeeded.
	deletingPaths map[string]bool
	// sync is the batch in flight, or the summary of the last one until it is
	// cleared. Nil when neither.
	sync *syncRun

	// secrets is the context's secret store, the one the router resolved. A
	// sync fetches, and a fetch against the configured GitLab needs the token —
	// with the credential helper shut out, it is the only way in (§3.16).
	secrets credentials.Storage

	// footer is the one line of transient state below the viewport (Rule 128).
	footer sharedcomponents.FooterMessage

	// selectionMessage is displayed in the footer when in ModeSelecting
	selectionMessage string

	// deps is where the scanners resolve from on this machine, or nil while
	// nobody has looked yet. A pointer because "not yet known" and "neither
	// scanner is installed" are different answers, and a zero DependencyStatus
	// says the second — the *bool of Result.SecretVerdict, one screen over.
	//
	// It is read by actions() to decide whether S and A apply at all, and it is
	// filled by a Cmd: scan.CheckDependencies runs exec.LookPath, a --version
	// and a docker images -q, none of which may happen in New or View
	// (Rule 110).
	deps *scan.DependencyStatus
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
	// SubRepoSkipped counts the directories the walk could not read or resolve.
	// Without it the footer could say how many repositories were about to be
	// scanned and nothing at all about how many it had failed to look for,
	// which is what made D59 silent rather than merely annoying.
	SubRepoSkipped int
	GitBranch      string
	GitRemote      string // remote path without server URL (e.g. "group/project")
	GitRemoteURL   string // full remote URL normalized for browser opening
	GitModified    int
	GitUntracked   int
	GitUnpushed    int
	GitUnpulled    int
}

// New crée une nouvelle instance du modèle workspaces.
//
// secrets may be nil for a view that will never sync — the borrowed selection
// mode is the one such case — and a nil store simply means no token is offered.
func New(cfg *config.Config, secrets credentials.Storage) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	return Model{
		config:  cfg,
		secrets: secrets,
		table: datatable.New(datatable.Config[workspaceRow]{
			Columns:    workspaceColumns(cfg.Scan.EnableCIScore),
			SortColumn: -1, // the order the directory listing gave
		}),
		mode:          ModeNormal,
		pendingCursor: -1,
		spinner:       s,
		scanCache:     make(map[string]cache.WorkspaceScanEntry),
		scanningPaths: make(map[string]bool),
		syncingPaths:  make(map[string]bool),
		deletingPaths: make(map[string]bool),
	}
}

// NewForSelection creates a workspaces view in selection mode for picking a
// directory. It is lent to another view to answer one question and never syncs,
// so it carries no secret store.
func NewForSelection(cfg *config.Config, message string) Model {
	m := New(cfg, nil)
	m.mode = ModeSelecting
	m.selectionMessage = message
	return m
}
