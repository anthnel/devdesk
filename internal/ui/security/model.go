package security

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ViewState represents the current state of the security view
type ViewState int

const (
	StateInput     ViewState = iota // Form for selecting target and options
	StateScanning                   // Scanning in progress
	StateResults                    // Displaying results summary
	StateDetails                    // Showing detailed findings
	StateInventory                  // What this context has scanned, read from the caches
)

// Model represents the security scanner view
type Model struct {
	config *config.Config
	width  int
	height int

	state  ViewState
	deps   scan.DependencyStatus
	result *scan.Result
	err    error

	// homeState is where esc and ctrl+r return to from the results: the
	// inventory for a view opened on ":sec", the form for one opened with a
	// target already filled in. It goes away with the form in phase 3, when
	// there is only one answer left.
	homeState ViewState

	// Inventory state
	inventory datatable.Model[scanTarget]

	// Form state
	targetType      string
	targetPath      string
	enableVuln      bool
	enableSecret    bool
	enableMisconfig bool
	enableLicense   bool
	generateSBOM    bool
	targetInput     textinput.Model
	focusedField    int

	// Advanced options
	trivyServerInput    textinput.Model
	gitleaksConfigInput textinput.Model
	ignoreUnfixed       bool
	ignoreEOL           bool
	gitleaksHistory     bool

	// Scanning state
	spinner       spinner.Model
	scanStartTime time.Time
	scanStages    []scan.ProgressUpdate    // per-stage progress, in arrival order
	progressCh    chan scan.ProgressUpdate // receives progress updates from scan goroutine
	cancelScan    func()                   // cancels the running scan goroutine; nil when not scanning
	scanGen       int                      // incremented on each startScan; used to discard stale ScanCompleteMsg

	// Results state
	findingsTable datatable.Model[scan.Finding]
	// selectedFinding is what the details view is showing. It holds the finding
	// rather than its row index, so a list that changes underneath cannot make
	// the details describe a different one.
	selectedFinding *scan.Finding
	activeTab       int    // 0=CVE, 1=Secrets, 2=Licenses, 3=Misconfig
	severityFilter  string // "all", "critical", "high", "medium", "low"

	// Pre-populated target (from workspace view)
	prefilledTarget string

	// OCI images integration: when true, launching the scan returns to OCI images view
	isImageScan       bool
	returnToOCIImages bool

	// Workspaces integration: when true, scan completion sends result back to workspaces view
	returnToWorkspaces bool

	// OriginView is the view to return to when Esc is pressed in StateResults.
	// Set by the app router when opening this view from workspaces or oci_resources.
	OriginView command.ViewType

	// Status message (temporary feedback)
	statusMessage string

	// Details view scrollable viewport
	detailsViewport viewport.Model

	// Ignore secret confirmation
	confirmModal    *sharedcomponents.ConfirmModal
	findingToIgnore *scan.Finding
}

// New creates a new security scanner view
func New(cfg *config.Config) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	ti := textinput.New()
	ti.Placeholder = "/path/to/scan"
	ti.CharLimit = 256
	ti.Width = 50
	theme.StyleTextInput(&ti)

	t := datatable.New(datatable.Config[scan.Finding]{
		Columns:        findingColumns(),
		SortColumn:     -1, // the order the scanner reported
		SelectedStyles: findingSelectedStyles,
	})

	// Advanced options textinputs
	trivyServer := textinput.New()
	trivyServer.Placeholder = "https://trivy-server:4954"
	trivyServer.CharLimit = 256
	trivyServer.Width = 40
	theme.StyleTextInput(&trivyServer)
	if cfg.Scan.TrivyServer != "" {
		trivyServer.SetValue(cfg.Scan.TrivyServer)
	}

	gitleaksConfig := textinput.New()
	gitleaksConfig.Placeholder = "/path/to/.gitleaks.toml"
	gitleaksConfig.CharLimit = 256
	gitleaksConfig.Width = 40
	theme.StyleTextInput(&gitleaksConfig)

	return Model{
		config:              cfg,
		state:               StateInventory,
		homeState:           StateInventory,
		inventory:           newInventoryTable(),
		targetType:          "directory",
		enableVuln:          cfg.Scan.EnableVuln,
		enableSecret:        cfg.Scan.EnableSecret,
		enableMisconfig:     cfg.Scan.EnableMisconfig,
		enableLicense:       cfg.Scan.EnableLicense,
		generateSBOM:        cfg.Scan.GenerateSBOM,
		ignoreUnfixed:       cfg.Scan.IgnoreUnfixed,
		ignoreEOL:           cfg.Scan.IgnoreEOL,
		gitleaksHistory:     cfg.Scan.GitleaksHistory,
		targetInput:         ti,
		spinner:             s,
		findingsTable:       t,
		focusedField:        0,
		severityFilter:      "all",
		trivyServerInput:    trivyServer,
		gitleaksConfigInput: gitleaksConfig,
	}
}

// NewWithTarget creates a security view pre-populated with a target path.
//
// The form rather than the inventory: the caller already knows what is to be
// scanned, so listing everything that has been would be a step backwards. It is
// also the state whose fields the prefill is for. This constructor goes with the
// form in phase 3.
func NewWithTarget(cfg *config.Config, target string) Model {
	m := New(cfg)
	m.state = StateInput
	m.homeState = StateInput
	m.prefilledTarget = target
	m.targetPath = target
	m.targetInput.SetValue(target)
	return m
}

// NewWithImageTarget creates a security view pre-populated with an image target.
// Set returnToOCI=true when opened from the OCI images view: launching the scan will send
// the configured options back to OCI images view instead of running the scan in-place.
func NewWithImageTarget(cfg *config.Config, imageName string, returnToOCI bool) Model {
	m := New(cfg)
	m.state = StateInput
	m.homeState = StateInput
	m.prefilledTarget = imageName
	m.targetPath = imageName
	m.targetType = "image"
	m.isImageScan = true
	m.returnToOCIImages = returnToOCI
	if imageName == "all" {
		m.targetInput.SetValue("all")
		m.targetInput.Placeholder = "All OCI Images"
	} else {
		m.targetInput.SetValue(imageName)
	}
	return m
}

// NewWithTargetReturnToWorkspaces creates a security view pre-populated with a target path.
// After the scan completes, results are sent back to the workspaces view.
func NewWithTargetReturnToWorkspaces(cfg *config.Config, target string) Model {
	m := NewWithTarget(cfg, target)
	m.returnToWorkspaces = true
	return m
}

// NewWithPreloadedResult creates a security view in StateDetails with a pre-loaded scan result.
// Used to display cached workspace scan results without re-scanning.
func NewWithPreloadedResult(cfg *config.Config, result *scan.Result) Model {
	m := New(cfg)
	m.state = StateResults
	m.result = result
	m.targetPath = result.Target
	m.activeTab = TabCVE
	m.severityFilter = "all"
	// Filled here rather than waiting for the first WindowSizeMsg: the rows do
	// not depend on the width any more, so nothing was gained by deferring and
	// the table was empty until the terminal happened to report its size.
	m.updateFindingsTable()
	return m
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.checkDependencies(),
		m.spinner.Tick,
		// Loaded whatever the opening state: a view opened on a result returns
		// to the inventory on ctrl+r, and reading two small files is cheaper
		// than the branch that would decide not to.
		loadInventoryCmd(),
	)
}

// checkDependencies verifies tool availability
func (m Model) checkDependencies() tea.Cmd {
	// Copied out of the model before the Cmd runs: a Cmd must not read state
	// Update() may be writing (Rule 110).
	scanCfg := m.config.Scan
	return func() tea.Msg {
		return DepsCheckedMsg{Deps: scan.CheckDependencies(scanCfg)}
	}
}

// InEditMode reports whether a field or a modal has the keyboard, which is what
// stops the router claiming ":", "q" and "?" for itself.
//
// The scanning, results and details states used to be listed here too, for one
// reason: it was the only way to be handed esc. The router forwards esc
// unconditionally now (§1.3 D15), so those states are ordinary again — and get
// the command line, the help overlay and quit back with them.
func (m Model) InEditMode() bool {
	isTextInput := m.state == StateInput && (m.focusedField == 1 || m.focusedField == 7 || m.focusedField == 10)
	isFiltering := m.state == StateInventory && m.inventory.InEditMode()
	return isTextInput || isFiltering || m.confirmModal != nil
}
