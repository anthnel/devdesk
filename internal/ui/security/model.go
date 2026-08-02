package security

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// ViewState represents the current state of the security view
type ViewState int

const (
	StateInput    ViewState = iota // Form for selecting target and options
	StateScanning                  // Scanning in progress
	StateResults                   // Displaying results summary
	StateDetails                   // Showing detailed findings
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
	findingsTable    table.Model
	selectedIdx      int
	activeTab        int            // 0=CVE, 1=Secrets, 2=Licenses, 3=Misconfig
	severityFilter   string         // "all", "critical", "high", "medium", "low"
	filteredFindings []scan.Finding // Current filtered findings for display

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

// Messages

// ScanCompleteMsg is sent when scanning finishes
type ScanCompleteMsg struct {
	Result *scan.Result
	Error  error
	Gen    int // must match Model.scanGen; stale results (cancelled scans) are discarded
}

// DepsCheckedMsg is sent when dependency check completes
type DepsCheckedMsg struct {
	Deps scan.DependencyStatus
}

// SecretIgnoredMsg is sent when a secret is added to .gitleaksignore
type SecretIgnoredMsg struct {
	Finding scan.Finding
	Error   error
}

// SelectionRequestMsg is sent to the app router to request a selection from another view
type SelectionRequestMsg struct {
	Type    string // "directory" or "image"
	Message string // Context message to display in the selection view footer
}

// SelectionResultMsg is sent back to security view with the selected path/image
type SelectionResultMsg struct {
	Path string // Selected directory path or image name
}

// SelectionCancelledMsg is sent when user cancels the selection
type SelectionCancelledMsg struct{}

// StartScanMsg triggers the scan programmatically (e.g. for viewing cached results)
type StartScanMsg struct{}

// ScanProgressMsg carries a progress update from the scan goroutine to the TUI.
type ScanProgressMsg struct {
	Update scan.ProgressUpdate
}

// BackToOriginMsg is sent when the user presses Esc in StateResults to return to the originating view.
type BackToOriginMsg struct {
	Origin command.ViewType
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

	// Create findings table
	columns := []table.Column{
		{Title: "Severity", Width: 10},
		{Title: "ID", Width: 20},
		{Title: "Title", Width: 40},
		{Title: "Source", Width: 10},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(theme.DefaultTableStyles())

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
		state:               StateInput,
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

// NewWithTarget creates a security view pre-populated with a target path
func NewWithTarget(cfg *config.Config, target string) Model {
	m := New(cfg)
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
	// updateFindingsTable will be called when WindowSizeMsg is received
	return m
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.checkDependencies(),
		m.spinner.Tick,
	)
}

// checkDependencies verifies tool availability
func (m Model) checkDependencies() tea.Cmd {
	trivyImage := m.config.Scan.TrivyImage
	gitleaksImage := m.config.Scan.GitleaksImage
	return func() tea.Msg {
		deps := scan.CheckDependenciesWithImages(trivyImage, gitleaksImage)
		return DepsCheckedMsg{Deps: deps}
	}
}

// InEditMode returns true when the view needs to handle ESC key
func (m Model) InEditMode() bool {
	// Return true when:
	// - typing in text input (StateInput with focused field 1, 7, or 10)
	// - scan is in progress (ESC should cancel the scan)
	// - in detail view (ESC should go back to results)
	// - in file browser (ESC should cancel)
	// - showing confirm modal (ESC should close it)
	isTextInput := m.state == StateInput && (m.focusedField == 1 || m.focusedField == 7 || m.focusedField == 10)
	return isTextInput ||
		m.state == StateScanning ||
		m.state == StateResults ||
		m.state == StateDetails ||
		m.confirmModal != nil
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle confirm modal messages first (before passing to modal)
	switch msg := msg.(type) {
	case sharedcomponents.ConfirmModalYesMsg:
		// User confirmed to ignore the secret
		if m.findingToIgnore != nil {
			finding := *m.findingToIgnore
			targetDir := m.targetPath
			m.confirmModal = nil
			m.findingToIgnore = nil
			return m, func() tea.Msg {
				err := scan.AddToGitleaksIgnore(targetDir, finding)
				return SecretIgnoredMsg{Finding: finding, Error: err}
			}
		}
		m.confirmModal = nil
		m.findingToIgnore = nil
		return m, nil

	case sharedcomponents.ConfirmModalNoMsg:
		// User cancelled
		m.confirmModal = nil
		m.findingToIgnore = nil
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// SetHeight accounts for the header row internally, so pass m.height directly
		m.findingsTable.SetHeight(max(m.height, 5))
		// Always recalculate columns on resize
		m.findingsTable.SetColumns(m.calculateColumns())
		// Rebuild rows if in results or details state (to apply new title width truncation)
		if (m.state == StateResults || m.state == StateDetails) && m.result != nil {
			m.updateFindingsTable()
		}
		// Update details viewport size and refresh content
		m.detailsViewport.Width = msg.Width
		m.detailsViewport.Height = msg.Height
		if m.state == StateDetails {
			m.detailsViewport.SetContent(m.buildDetailsContent())
		}
		return m, nil

	case tea.KeyMsg:
		// Pass key messages to confirm modal if active
		if m.confirmModal != nil {
			var cmd tea.Cmd
			m.confirmModal, cmd = m.confirmModal.Update(msg)
			return m, cmd
		}
		return m.handleKeyMsg(msg)

	case DepsCheckedMsg:
		m.deps = msg.Deps
		return m, nil

	case ScanCompleteMsg:
		return m.handleScanComplete(msg)

	case SecretIgnoredMsg:
		if msg.Error != nil {
			log.Printf("ERROR [security] ignore secret %s: %v", msg.Finding.File, msg.Error)
			m.statusMessage = "Failed to ignore secret — check logs"
		} else {
			m.statusMessage = fmt.Sprintf("Added %s to .gitleaksignore", msg.Finding.File)
		}
		return m, nil

	case SelectionResultMsg:
		m.targetPath = msg.Path
		m.targetInput.SetValue(msg.Path)
		return m, nil

	case SelectionCancelledMsg:
		return m, nil

	case StartScanMsg:
		return m.startScan()

	case ScanProgressMsg:
		return m.handleScanProgress(msg)

	case spinner.TickMsg:
		if m.state == StateScanning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

// handleKeyMsg processes keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case StateInput:
		return m.handleInputState(msg)
	case StateScanning:
		if msg.String() == "esc" || msg.String() == "ctrl+c" {
			return m.cancelCurrentScan()
		}
		return m, nil
	case StateResults:
		return m.handleResultsState(msg)
	case StateDetails:
		return m.handleDetailsState(msg)
	}
	return m, nil
}

// totalFields returns the number of form fields
func (m Model) totalFields() int {
	return 13 // 0-6 (left) + 7-11 (right, with ignoreEOL at 9) + 12 (start button)
}

// isServerMode returns true when a Trivy server URL is configured.
func (m Model) isServerMode() bool {
	return strings.TrimSpace(m.trivyServerInput.Value()) != ""
}

// isServerIncompatibleField returns true for scan options that are not supported
// by the Trivy client-server protocol (misconfig=4, license=5, sbom=6).
func (m Model) isServerIncompatibleField(fieldIdx int) bool {
	return fieldIdx == 4 || fieldIdx == 5 || fieldIdx == 6
}

// isFieldSkipped returns true when a field should be bypassed during Tab navigation.
func (m Model) isFieldSkipped(fieldIdx int) bool {
	return m.isServerMode() && m.isServerIncompatibleField(fieldIdx)
}

// nextField returns the next focusable field index, skipping disabled fields.
func (m Model) nextField() int {
	total := m.totalFields()
	next := (m.focusedField + 1) % total
	for m.isFieldSkipped(next) {
		next = (next + 1) % total
	}
	return next
}

// prevField returns the previous focusable field index, skipping disabled fields.
func (m Model) prevField() int {
	total := m.totalFields()
	prev := (m.focusedField - 1 + total) % total
	for m.isFieldSkipped(prev) {
		prev = (prev - 1 + total) % total
	}
	return prev
}

// applyServerModeConstraints auto-disables options incompatible with Trivy server mode
// and persists the updated config. No-op when not in server mode.
func (m *Model) applyServerModeConstraints() {
	if !m.isServerMode() {
		return
	}
	m.enableMisconfig = false
	m.enableLicense = false
	m.generateSBOM = false
	m.saveOptionsToConfig()
}

// isTextInputField returns true if the field index is a text input
func (m Model) isTextInputField(idx int) bool {
	return idx == 1 || idx == 7 || idx == 10
}

// blurAllTextInputs removes focus from all text inputs
func (m *Model) blurAllTextInputs() {
	m.targetInput.Blur()
	m.trivyServerInput.Blur()
	m.gitleaksConfigInput.Blur()
}

// saveOptionsToConfig syncs all form options to config and persists to disk.
func (m Model) saveOptionsToConfig() {
	m.config.Scan.EnableVuln = m.enableVuln
	m.config.Scan.EnableSecret = m.enableSecret
	m.config.Scan.EnableMisconfig = m.enableMisconfig
	m.config.Scan.EnableLicense = m.enableLicense
	m.config.Scan.GenerateSBOM = m.generateSBOM
	m.config.Scan.IgnoreUnfixed = m.ignoreUnfixed
	m.config.Scan.IgnoreEOL = m.ignoreEOL
	m.config.Scan.GitleaksHistory = m.gitleaksHistory
	m.config.Scan.TrivyServer = m.trivyServerInput.Value()
	m.config.Scan.GitleaksConfig = m.gitleaksConfigInput.Value()
	_ = config.Save(m.config)
}

// focusTextField focuses the text input for the given field index
func (m *Model) focusTextField(idx int) {
	switch idx {
	case 1:
		m.targetInput.Focus()
	case 7:
		m.trivyServerInput.Focus()
	case 10:
		m.gitleaksConfigInput.Focus()
	}
}

// handleInputState processes input in form state
func (m Model) handleInputState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle text input fields (1=target, 7=trivy server, 10=gitleaks config)
	if m.isTextInputField(m.focusedField) {
		switch msg.String() {
		case "down":
			switch m.focusedField {
			case 7:
				m.applyServerModeConstraints()
			case 10:
				m.saveOptionsToConfig()
			}
			m.blurAllTextInputs()
			m.focusedField = m.nextField()
			if m.isTextInputField(m.focusedField) {
				m.focusTextField(m.focusedField)
			}
			return m, nil
		case "up":
			switch m.focusedField {
			case 7:
				m.applyServerModeConstraints()
			case 10:
				m.saveOptionsToConfig()
			}
			m.blurAllTextInputs()
			m.focusedField = m.prevField()
			if m.isTextInputField(m.focusedField) {
				m.focusTextField(m.focusedField)
			}
			return m, nil
		case "b":
			if m.focusedField == 1 {
				if m.targetType == "directory" {
					return m.openFileBrowser()
				}
				if m.targetType == "image" {
					return m.openImageBrowser()
				}
			}
			fallthrough
		default:
			var cmd tea.Cmd
			switch m.focusedField {
			case 1:
				m.targetInput, cmd = m.targetInput.Update(msg)
				m.targetPath = m.targetInput.Value()
			case 7:
				m.trivyServerInput, cmd = m.trivyServerInput.Update(msg)
			case 10:
				m.gitleaksConfigInput, cmd = m.gitleaksConfigInput.Update(msg)
			}
			return m, cmd
		}
	}

	// Handle non-textinput fields
	switch msg.String() {
	case "down":
		m.blurAllTextInputs()
		m.focusedField = m.nextField()
		if m.isTextInputField(m.focusedField) {
			m.focusTextField(m.focusedField)
		}
		return m, nil
	case "up":
		m.blurAllTextInputs()
		m.focusedField = m.prevField()
		if m.isTextInputField(m.focusedField) {
			m.focusTextField(m.focusedField)
		}
		return m, nil
	case "left":
		return m.handleFormLeft()
	case "right":
		return m.handleFormRight()
	case "enter":
		// Enter always starts the scan in this view (Rule 135 — unique action)
		return m.startScan()
	case " ":
		return m.handleFormSpace()
	case "ctrl+s":
		return m.startScan()
	}
	return m, nil
}

// handleFormLeft handles left arrow key for cycle fields (Rule 132)
func (m Model) handleFormLeft() (tea.Model, tea.Cmd) {
	if m.focusedField == 0 {
		return m.cycleTargetType("left"), nil
	}
	return m, nil
}

// handleFormRight handles right arrow key for cycle fields (Rule 132)
func (m Model) handleFormRight() (tea.Model, tea.Cmd) {
	if m.focusedField == 0 {
		return m.cycleTargetType("right"), nil
	}
	return m, nil
}

// cycleTargetType toggles the target type and resets the target input
func (m Model) cycleTargetType(_ string) Model {
	switch m.targetType {
	case "directory":
		m.targetType = "image"
		m.targetInput.Placeholder = "image:tag"
	case "image":
		m.targetType = "directory"
		m.targetInput.Placeholder = "/path/to/scan"
	}
	m.targetPath = ""
	m.targetInput.SetValue("")
	return m
}

// handleFormSpace handles space key in form (same as enter for toggles)
func (m Model) handleFormSpace() (tea.Model, tea.Cmd) {
	// Server-incompatible fields cannot be toggled when a Trivy server is set
	if m.isServerMode() && m.isServerIncompatibleField(m.focusedField) {
		return m, nil
	}
	switch m.focusedField {
	case 2:
		m.enableVuln = !m.enableVuln
		m.saveOptionsToConfig()
	case 3:
		m.enableSecret = !m.enableSecret
		m.saveOptionsToConfig()
	case 4:
		m.enableMisconfig = !m.enableMisconfig
		m.saveOptionsToConfig()
	case 5:
		m.enableLicense = !m.enableLicense
		m.saveOptionsToConfig()
	case 6:
		m.generateSBOM = !m.generateSBOM
		m.saveOptionsToConfig()
	case 8:
		m.ignoreUnfixed = !m.ignoreUnfixed
		m.saveOptionsToConfig()
	case 9:
		m.ignoreEOL = !m.ignoreEOL
		m.saveOptionsToConfig()
	case 11:
		m.gitleaksHistory = !m.gitleaksHistory
		m.saveOptionsToConfig()
	}
	return m, nil
}

// startScan initiates the security scan.
// If returnToOCIImages is set, it sends configured options back to OCI images view instead.
func (m Model) startScan() (tea.Model, tea.Cmd) {
	if m.targetPath == "" {
		m.err = fmt.Errorf("target path is required")
		return m, nil
	}

	opts := scan.ScanOptions{
		EnableVuln:      m.enableVuln,
		EnableSecret:    m.enableSecret,
		EnableMisconfig: m.enableMisconfig,
		EnableLicense:   m.enableLicense,
		GenerateSBOM:    m.generateSBOM,
		SBOMOutputDir:   m.config.Scan.SBOMOutputDir,
		TrivyImage:      m.config.Scan.TrivyImage,
		GitleaksImage:   m.config.Scan.GitleaksImage,
		TrivyServer:     m.trivyServerInput.Value(),
		IgnoreUnfixed:   m.ignoreUnfixed,
		IgnoreEOL:       m.ignoreEOL,
		GitleaksHistory: m.gitleaksHistory,
		GitleaksConfig:  m.gitleaksConfigInput.Value(),
	}

	// Persist all options to config (may already be up to date if user toggled before scanning)
	m.saveOptionsToConfig()

	// When opened from OCI images view, delegate scan execution back to that view
	if m.isImageScan && m.returnToOCIImages {
		if m.targetPath == "all" {
			capturedOpts := opts
			return m, func() tea.Msg { return ociresources.LaunchBatchScanMsg{Opts: capturedOpts} }
		}
		capturedOpts := opts
		capturedName := m.targetPath
		return m, func() tea.Msg {
			return ociresources.LaunchSingleImageScanMsg{ImageName: capturedName, Opts: capturedOpts}
		}
	}

	m.state = StateScanning
	m.err = nil
	m.scanGen++
	m.scanStartTime = time.Now()
	m.scanStages = nil

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelScan = cancel

	ch := make(chan scan.ProgressUpdate, 32)
	m.progressCh = ch
	gen := m.scanGen
	targetPath := m.targetPath
	targetType := m.getTargetType()
	opts.OnProgress = func(update scan.ProgressUpdate) {
		select {
		case ch <- update:
		default: // don't block if buffer full
		}
	}

	return m, tea.Batch(
		m.spinner.Tick,
		purgeScanCacheCmd(targetPath, targetType),
		waitForProgressCmd(ch),
		func() tea.Msg {
			defer cancel()
			defer close(ch)
			scanner := scan.NewScanner(opts)
			result, err := scanner.Scan(ctx, targetPath, targetType)
			return ScanCompleteMsg{Result: result, Error: err, Gen: gen}
		},
	)
}

// waitForProgressCmd returns a Cmd that blocks until the next ProgressUpdate is available on ch.
// Returns nil when the channel is closed (scan complete).
func waitForProgressCmd(ch <-chan scan.ProgressUpdate) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		update, ok := <-ch
		if !ok {
			return nil // channel closed, scan finished
		}
		return ScanProgressMsg{Update: update}
	}
}

// purgeScanCacheCmd deletes the existing cache entries for the target before re-scanning,
// so that stale results don't persist if the scan is interrupted or fails.
// For workspace (directory) targets, both the counts cache and the full result file are removed.
func purgeScanCacheCmd(target string, targetType scan.TargetType) tea.Cmd {
	return func() tea.Msg {
		switch targetType {
		case scan.TargetDirectory:
			if c, err := cache.NewWorkspaceScanCache(); err == nil {
				_ = c.Delete(target)
			}
			_ = cache.DeleteWorkspaceScanResult(target)
		case scan.TargetImage:
			if c, err := cache.NewImageScanCache(); err == nil {
				_ = c.Delete(target)
			}
		}
		return nil
	}
}

// getTargetType converts string to TargetType
func (m Model) getTargetType() scan.TargetType {
	switch m.targetType {
	case "image":
		return scan.TargetImage
	default:
		return scan.TargetDirectory
	}
}

// cancelCurrentScan cancels the running scan and returns to StateInput.
// The goroutine will still complete and send ScanCompleteMsg, but it will be
// discarded because scanGen is incremented here.
func (m Model) cancelCurrentScan() (tea.Model, tea.Cmd) {
	if m.cancelScan != nil {
		m.cancelScan()
		m.cancelScan = nil
	}
	m.scanGen++ // invalidate any pending ScanCompleteMsg from the cancelled goroutine
	m.state = StateInput
	return m, nil
}

// handleScanProgress updates the per-stage progress state and schedules the next read.
func (m Model) handleScanProgress(msg ScanProgressMsg) (tea.Model, tea.Cmd) {
	u := msg.Update
	for i, s := range m.scanStages {
		if s.Stage == u.Stage {
			m.scanStages[i] = u
			return m, waitForProgressCmd(m.progressCh)
		}
	}
	// New stage: append in arrival order
	m.scanStages = append(m.scanStages, u)
	return m, waitForProgressCmd(m.progressCh)
}

// handleScanComplete processes scan results
func (m Model) handleScanComplete(msg ScanCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Gen != m.scanGen {
		return m, nil // stale result from a cancelled scan, discard
	}
	if msg.Error != nil {
		m.err = msg.Error
		m.state = StateInput
		return m, nil
	}

	m.result = msg.Result
	m.state = StateResults
	m.updateFindingsTable()

	// Cache CVE counts and full result for image scans (Rule 126: Enter loads from cache)
	if m.targetType == "image" && m.result != nil {
		result := m.result
		path := m.targetPath
		go func() {
			saveImageScanToCache(path, result)
			_ = cache.SaveImageScanResult(path, result)
		}()
	}

	// For workspace scans: persist full result and send summary back to workspaces view
	if !m.isImageScan && m.returnToWorkspaces && m.result != nil {
		result := m.result
		path := m.targetPath
		go func() { _ = cache.SaveWorkspaceScanResult(path, result) }()
		sensitive := hasScanSource(result, "gitleaks")
		return m, func() tea.Msg {
			return workspaces.WorkspaceScanCompleteMsg{
				RepoPath:  path,
				Critical:  result.Counts.Critical,
				High:      result.Counts.High,
				Medium:    result.Counts.Medium,
				Low:       result.Counts.Low,
				Sensitive: sensitive,
				ScannedAt: result.EndTime,
			}
		}
	}

	return m, nil
}

// hasScanSource returns true if any finding in the result comes from the given source
func hasScanSource(result *scan.Result, source string) bool {
	for _, f := range result.Findings {
		if f.Source == source {
			return true
		}
	}
	return false
}

// saveImageScanToCache persists CVE severity counts to the image scan cache
func saveImageScanToCache(imageName string, result *scan.Result) {
	c, err := cache.NewImageScanCache()
	if err != nil {
		return
	}
	_ = c.Set(imageName, cache.ImageScanEntry{
		Critical:  result.Counts.Critical,
		High:      result.Counts.High,
		Medium:    result.Counts.Medium,
		Low:       result.Counts.Low,
		ScannedAt: result.EndTime,
	})
}

// Tab constants for results view
const (
	TabCVE       = 0
	TabSecrets   = 1
	TabLicense   = 2
	TabMisconfig = 3
)

// updateFindingsTable populates the findings table based on active tab and filters
func (m *Model) updateFindingsTable() {
	if m.result == nil {
		return
	}

	// Filter findings by tab type
	m.filteredFindings = m.filterFindingsByTab()

	// Apply severity filter (for CVE, License, and Misconfig tabs)
	if (m.activeTab == TabCVE || m.activeTab == TabLicense || m.activeTab == TabMisconfig) && m.severityFilter != "all" {
		m.filteredFindings = m.filterFindingsBySeverity(m.filteredFindings)
	}

	// Calculate dynamic column widths based on available width
	columns := m.calculateColumns()
	m.findingsTable.SetColumns(columns)

	// Build rows from filtered findings
	rows := make([]table.Row, 0, len(m.filteredFindings))
	titleWidth := m.getTitleColumnWidth()
	for _, f := range m.filteredFindings {
		rows = append(rows, table.Row{
			string(f.Severity),
			f.ID,
			theme.TruncateWidth(f.Title, titleWidth-3),
			m.getSourceDisplay(f),
		})
	}
	m.findingsTable.SetRows(rows)
	// Reset cursor to first row when data changes
	m.findingsTable.GotoTop()
	m.refreshSelectionStyle()
}

// filterFindingsByTab returns findings filtered by the active tab
func (m *Model) filterFindingsByTab() []scan.Finding {
	if m.result == nil {
		return nil
	}

	var filtered []scan.Finding
	for _, f := range m.result.Findings {
		switch m.activeTab {
		case TabCVE:
			// CVE: vulnerabilities from trivy (not secrets, not licenses)
			if f.Source == "trivy" && f.PkgName != "" && f.Match == "" {
				filtered = append(filtered, f)
			}
		case TabSecrets:
			// Secrets: from gitleaks or trivy secrets
			if f.Source == "gitleaks" || (f.Source == "trivy" && f.Match != "") {
				filtered = append(filtered, f)
			}
		case TabLicense:
			// Licenses: from trivy-license source
			if f.Source == "trivy-license" {
				filtered = append(filtered, f)
			}
		case TabMisconfig:
			// Misconfigurations: from trivy-misconfig source
			if f.Source == "trivy-misconfig" {
				filtered = append(filtered, f)
			}
		}
	}
	return filtered
}

// filterFindingsBySeverity filters findings by selected severity level
func (m *Model) filterFindingsBySeverity(findings []scan.Finding) []scan.Finding {
	if m.severityFilter == "all" {
		return findings
	}

	var filtered []scan.Finding
	for _, f := range findings {
		switch m.severityFilter {
		case "critical":
			if f.Severity == scan.SeverityCritical {
				filtered = append(filtered, f)
			}
		case "high":
			if f.Severity == scan.SeverityCritical || f.Severity == scan.SeverityHigh {
				filtered = append(filtered, f)
			}
		case "medium":
			if f.Severity == scan.SeverityCritical || f.Severity == scan.SeverityHigh || f.Severity == scan.SeverityMedium {
				filtered = append(filtered, f)
			}
		case "low":
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// calculateColumns returns table columns with dynamic widths
func (m *Model) calculateColumns() []table.Column {
	// Minimum widths
	severityWidth := 10
	idWidth := 18
	sourceWidth := 14

	// Calculate title width using remaining space
	titleWidth := m.getTitleColumnWidth()

	return []table.Column{
		{Title: "Severity", Width: severityWidth},
		{Title: "ID", Width: idWidth},
		{Title: "Title", Width: titleWidth},
		{Title: "Source", Width: sourceWidth},
	}
}

// getTitleColumnWidth calculates the title column width based on terminal width
func (m *Model) getTitleColumnWidth() int {
	// Fixed widths: severity(10) + id(18) + source(14)
	// Overhead: viewport borders(2) + cell padding(4 columns × 2 = 8) = 10
	fixedWidth := 10 + 18 + 14 + 10
	titleWidth := max(m.width-fixedWidth, 20)
	return titleWidth
}

// getSourceDisplay returns a display string for the source column
func (m *Model) getSourceDisplay(f scan.Finding) string {
	switch f.Source {
	case "trivy-license":
		return "license"
	case "trivy-misconfig":
		return "misconfig"
	case "gitleaks":
		return "secret"
	case "trivy":
		if f.Match != "" {
			return "secret"
		}
		return "vuln"
	default:
		return f.Source
	}
}

// countFindingsByTab returns the count of findings for each tab
func (m *Model) countFindingsByTab() (cve, secrets, licenses, misconfigs int) {
	if m.result == nil {
		return 0, 0, 0, 0
	}

	for _, f := range m.result.Findings {
		switch {
		case f.Source == "trivy" && f.PkgName != "" && f.Match == "":
			cve++
		case f.Source == "gitleaks" || (f.Source == "trivy" && f.Match != ""):
			secrets++
		case f.Source == "trivy-license":
			licenses++
		case f.Source == "trivy-misconfig":
			misconfigs++
		}
	}
	return cve, secrets, licenses, misconfigs
}

// switchTab switches to the given tab index and refreshes the table
func (m *Model) switchTab(tab int) {
	m.activeTab = tab
	m.statusMessage = ""
	m.updateFindingsTable()
}

// handleIgnoreSecret prompts confirmation to ignore a secret finding
func (m *Model) handleIgnoreSecret() {
	if m.activeTab != TabSecrets || len(m.filteredFindings) == 0 {
		return
	}
	idx := m.findingsTable.Cursor()
	if idx < len(m.filteredFindings) {
		finding := m.filteredFindings[idx]
		m.findingToIgnore = &finding
		m.confirmModal = sharedcomponents.NewConfirmModal(
			"Ignore Secret",
			fmt.Sprintf("Add this secret to .gitleaksignore?\n\nFile: %s\nRule: %s", finding.File, finding.ID),
		)
	}
}

// handleResultsState processes input in results state
func (m Model) handleResultsState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// If no origin view is set (opened directly), go back to the scan form
		if m.OriginView == "" {
			m.state = StateInput
			return m, nil
		}
		return m, func() tea.Msg { return BackToOriginMsg{Origin: m.OriginView} }
	case "enter":
		if len(m.filteredFindings) > 0 {
			m.selectedIdx = m.findingsTable.Cursor()
			m.state = StateDetails
			m.detailsViewport.Width = m.width
			m.detailsViewport.Height = m.height
			m.detailsViewport.SetContent(m.buildDetailsContent())
			m.detailsViewport.GotoTop()
		}
		return m, nil
	case "ctrl+r":
		m.state = StateInput
		m.activeTab = TabCVE
		m.severityFilter = "all"
		return m, nil
	case "tab":
		m.switchTab((m.activeTab + 1) % 4)
		return m, nil
	case "shift+tab":
		m.switchTab((m.activeTab + 3) % 4)
		return m, nil
	case "1":
		m.switchTab(TabCVE)
		return m, nil
	case "2":
		m.switchTab(TabSecrets)
		return m, nil
	case "3":
		m.switchTab(TabLicense)
		return m, nil
	case "4":
		m.switchTab(TabMisconfig)
		return m, nil
	case ".":
		if m.activeTab == TabCVE || m.activeTab == TabLicense || m.activeTab == TabMisconfig {
			m.cycleSeverityFilter()
			m.updateFindingsTable()
		}
		return m, nil
	case "i":
		m.handleIgnoreSecret()
		return m, nil
	case "up", "down", "k", "j":
		var cmd tea.Cmd
		m.findingsTable, cmd = m.findingsTable.Update(msg)
		m.refreshSelectionStyle()
		return m, cmd
	case "g", "home":
		m.findingsTable.GotoTop()
		m.refreshSelectionStyle()
		return m, nil
	case "G", "end":
		m.findingsTable.GotoBottom()
		m.refreshSelectionStyle()
		return m, nil
	}
	return m, nil
}

// cycleSeverityFilter cycles through severity filter options
func (m *Model) cycleSeverityFilter() {
	filters := []string{"all", "critical", "high", "medium", "low"}
	for i, f := range filters {
		if f == m.severityFilter {
			m.severityFilter = filters[(i+1)%len(filters)]
			return
		}
	}
	m.severityFilter = "all"
}

// handleDetailsState processes input in details state
func (m Model) handleDetailsState(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.state = StateResults
		return m, nil
	case "o":
		return m.handleDetailsOpenReference()
	case "up", "k":
		m.detailsViewport.ScrollUp(1)
	case "down", "j":
		m.detailsViewport.ScrollDown(1)
	case "pgup", "b":
		m.detailsViewport.HalfPageUp()
	case "pgdown", "f":
		m.detailsViewport.HalfPageDown()
	case "g", "home":
		m.detailsViewport.GotoTop()
	case "G", "end":
		m.detailsViewport.GotoBottom()
	}
	return m, nil
}

// handleDetailsOpenReference opens the first reference URL in the default browser.
func (m Model) handleDetailsOpenReference() (tea.Model, tea.Cmd) {
	if m.selectedIdx >= len(m.filteredFindings) {
		return m, nil
	}
	f := m.filteredFindings[m.selectedIdx]
	if len(f.References) == 0 {
		m.statusMessage = "No references available"
		return m, nil
	}
	url := f.References[0]
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
		if err := cmd.Start(); err != nil {
			log.Printf("ERROR [security] open URL %s: %v", url, err)
		}
		return nil
	}
}

// View renders the UI
func (m Model) View() string {
	// Show confirm modal if active
	if m.confirmModal != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}

	switch m.state {
	case StateInput:
		return m.renderInputView()
	case StateScanning:
		return m.renderScanningView()
	case StateResults:
		return m.renderResultsView()
	case StateDetails:
		return m.renderDetailsView()
	}
	return ""
}

// renderInputView renders the form in a 2-column layout with padding
func (m Model) renderInputView() string {
	var b strings.Builder

	// Error message
	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError).Render("Error: "+m.err.Error()) + "\n\n")
	}

	// Build left column lines
	var left []string
	left = append(left, theme.SubTitleStyle.Render(theme.IconTarget+" Target"))
	left = append(left, "")
	left = append(left, m.renderTargetTypeField())
	left = append(left, m.renderTargetPathField())
	left = append(left, "")
	left = append(left, theme.SubTitleStyle.Render(theme.IconConfig+" Scan Options"))
	left = append(left, m.renderCheckbox(m.enableVuln, "Vulnerability Scan (Trivy)", 2))
	left = append(left, m.renderCheckbox(m.enableSecret, "Secret Scan (Gitleaks)", 3))
	left = append(left, m.renderCheckbox(m.enableMisconfig, "Misconfig Scan (Trivy)", 4))
	left = append(left, m.renderCheckbox(m.enableLicense, "License Scan (Trivy)", 5))
	left = append(left, m.renderCheckbox(m.generateSBOM, "Generate SBOM (CycloneDX)", 6))

	// Build right column lines (advanced options)
	var right []string
	right = append(right, theme.SubTitleStyle.Render(theme.IconConfig+" Trivy Options"))
	right = append(right, "")
	right = append(right, m.renderAdvancedTextInput(theme.IconServer+" Server ", m.trivyServerInput, 7))
	if m.isServerMode() {
		right = append(right, theme.DimStyle.Render("  Server mode: misconfig, license, SBOM unavailable"))
	}
	right = append(right, m.renderCheckbox(m.ignoreUnfixed, "Ignore Unfixed", 8))
	right = append(right, m.renderCheckbox(m.ignoreEOL, "Ignore EOL", 9))
	right = append(right, "")
	right = append(right, theme.SubTitleStyle.Render(theme.IconConfig+" Gitleaks Options"))
	right = append(right, "")
	right = append(right, m.renderAdvancedTextInput(theme.IconToml+" Config ", m.gitleaksConfigInput, 10))
	right = append(right, m.renderCheckbox(m.gitleaksHistory, "Scan Git History", 11))

	// Combine left and right columns line-by-line
	leftWidth := max(m.width/2-2, 40) // -2 for outer padding
	totalLines := max(len(left), len(right))
	for i := 0; i < totalLines; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		b.WriteString(theme.PadWithBg(l, leftWidth) + theme.Bg("    ") + r + "\n")
	}

	// Start button below both columns
	b.WriteString("\n")
	b.WriteString(" " + m.renderStartButton())

	return lipgloss.NewStyle().
		Padding(1).
		Background(theme.ColorBackground).
		Render(b.String())
}

// renderAdvancedTextInput renders a text input field for advanced options
func (m Model) renderAdvancedTextInput(label string, input textinput.Model, fieldIdx int) string {
	highlightStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true)
	if m.focusedField == fieldIdx {
		return highlightStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + input.View()
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + input.View()
}

// renderTargetTypeField renders target type selection
func (m Model) renderTargetTypeField() string {
	label := "Target Type " + theme.IconSelect + " "
	textStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
	value := textStyle.Render(m.targetType)

	highlightStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true)
	if m.focusedField == 0 {
		return highlightStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}

// renderTargetPathField renders target path input
func (m Model) renderTargetPathField() string {
	label := "Target "
	highlightStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorHighlight).Bold(true)
	if m.focusedField == 1 {
		return highlightStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + m.targetInput.View()
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + m.targetInput.View()
}

// renderCheckbox renders a checkbox. When the field is incompatible with Trivy server mode
// and a server is configured, it renders as a locked/disabled option.
func (m Model) renderCheckbox(checked bool, label string, fieldIdx int) string {
	if m.isServerMode() && m.isServerIncompatibleField(fieldIdx) {
		return theme.RenderCheckboxDisabled(label)
	}
	return theme.RenderCheckbox(checked, label, m.focusedField == fieldIdx)
}

// renderStartButton renders the start scan button
func (m Model) renderStartButton() string {
	canStart := m.deps.TrivyAvailable || m.deps.GitleaksAvailable
	if !canStart {
		return theme.DimStyle.Render("[ No scanners available ]")
	}
	return " " + theme.RenderButton("Start Scan", m.focusedField == 12, "primary")
}

// renderScanningView renders the scanning progress with per-stage status rows.
func (m Model) renderScanningView() string {
	elapsed := time.Since(m.scanStartTime).Round(time.Second)

	var b strings.Builder
	b.WriteString(theme.DimStyle.Render(fmt.Sprintf("Scanning %s — elapsed: %s", m.targetPath, elapsed)))
	b.WriteString("\n\n")

	if len(m.scanStages) == 0 {
		b.WriteString(theme.SpinnerMessage(m.spinner.View(), "Initializing..."))
		b.WriteString("\n")
	} else {
		for _, stage := range m.scanStages {
			b.WriteString(m.renderScanStageRow(stage))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(theme.HelpStyle.Render("[esc] cancel scan"))

	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Padding(1).
		Render(b.String())
}

// renderScanStageRow renders a single stage row with icon, label, and optional detail.
// Each segment uses theme.Bg() for gaps to avoid black backgrounds (Rule 115).
// The icon column is fixed at width 2 to align spinner (1 cell) with Nerd Font icons (2 cells).
func (m Model) renderScanStageRow(stage scan.ProgressUpdate) string {
	var iconContent string
	switch stage.Status {
	case scan.StageDone:
		iconContent = theme.StatusOKStyle.Render(theme.IconOK) + theme.Bg(" ") // match spinner's built-in trailing space
	case scan.StageError:
		iconContent = theme.StatusErrorStyle.Render(theme.IconError) + theme.Bg(" ") // match spinner's built-in trailing space
	default: // StageRunning
		iconContent = m.spinner.View() // spinner frames (e.g. "⡿ ") already include a trailing space
	}

	labelStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Width(20)
	label := labelStyle.Render(stage.Label)

	row := theme.Bg("  ") + iconContent + theme.Bg("  ") + label
	if stage.Detail != "" {
		row += theme.Bg("  ") + theme.DimStyle.Render(truncateScanDetail(stage.Detail))
	}
	return row
}

// truncateScanDetail extracts the most meaningful part of a trivy/gitleaks stderr line
// and truncates it to fit on one line.
func truncateScanDetail(line string) string {
	// Try to extract the message from a structured log line (timestamp + level + msg)
	// Format: "2006-01-02T... LEVEL  message"
	if len(line) > 10 && line[4] == '-' && line[7] == '-' {
		// Skip timestamp
		rest := line
		if idx := strings.IndexAny(rest, " \t"); idx >= 0 {
			rest = strings.TrimLeft(rest[idx:], " \t")
			// Skip level
			if idx2 := strings.IndexAny(rest, " \t"); idx2 >= 0 {
				msg := strings.TrimLeft(rest[idx2:], " \t")
				if msg != "" {
					line = msg
				}
			}
		}
	}
	// Skip download progress bar lines
	if strings.Contains(line, " MiB /") || strings.Contains(line, " p/s ") {
		return ""
	}
	// Skip all trivy component log messages (e.g. "[vuln] ...", "[checks-client] ...")
	if strings.HasPrefix(line, "[") {
		return ""
	}
	const maxLen = 55
	if len(line) > maxLen {
		return line[:maxLen-3] + "..."
	}
	return line
}

// renderResultsView renders the results summary
func (m Model) renderResultsView() string {
	if m.result == nil {
		return "No results"
	}

	var b strings.Builder

	// Warnings replace the table and hide the tab bar (no partial results to browse)
	if len(m.result.Errors) > 0 {
		b.WriteString(m.renderWarningsPanel())
		if m.statusMessage != "" {
			b.WriteString("\n")
			b.WriteString(theme.StatusOKStyle.Render(m.statusMessage))
		}
		return b.String()
	}

	b.WriteString(m.findingsTable.View())

	return b.String()
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	if m.state == StateResults && m.result != nil && len(m.result.Errors) == 0 {
		return 3 // tab bar + empty line + info line
	}
	return 2 // empty line + info line
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	if m.state == StateResults && m.result != nil && len(m.result.Errors) == 0 {
		infoLine := theme.EmptyLineBg(width)
		if m.statusMessage != "" {
			infoLine = theme.PadWithBg(theme.StatusOKStyle.Render(m.statusMessage), width)
		}
		return theme.PadWithBg(theme.Bg(" ")+m.renderTabs(), width) + "\n" + theme.EmptyLineBg(width) + "\n" + infoLine
	}
	infoLine := theme.EmptyLineBg(width)
	if m.state == StateDetails && m.statusMessage != "" {
		infoLine = theme.PadWithBg(theme.StatusOKStyle.Render(m.statusMessage), width)
	}
	return theme.EmptyLineBg(width) + "\n" + infoLine
}

// renderWarningsPanel renders scan errors in the table area so they are always fully visible.
// Only FATAL/ERROR/WARN log lines are shown — INFO lines and progress bars are stripped.
func (m Model) renderWarningsPanel() string {
	var b strings.Builder

	wrapWidth := max(m.width-6, 40)
	wrapStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Width(wrapWidth)

	b.WriteString(theme.StatusWarningStyle.Render("⚠ Scan Warnings") + "\n\n")
	for _, errMsg := range m.result.Errors {
		source, detail := parseScanError(errMsg)
		if source != "" {
			b.WriteString(theme.SubTitleStyle.Render("  "+source) + "\n")
		}
		b.WriteString(wrapStyle.Render("  "+detail) + "\n\n")
	}

	return b.String()
}

// parseScanError splits a scan error string into a source label (e.g. "trivy vuln") and
// a cleaned detail message with only FATAL/ERROR/WARN lines from tool stderr.
func parseScanError(errMsg string) (source, detail string) {
	// Format: "source: tool failed: <stderr>"
	idx := strings.Index(errMsg, ": ")
	if idx == -1 {
		return "", extractMeaningfulLines(errMsg)
	}
	source = errMsg[:idx]
	rest := errMsg[idx+2:]

	// Strip the intermediate "tool failed: " prefix
	for _, prefix := range []string{
		"trivy failed: ",
		"trivy misconfig failed: ",
		"sbom generation failed: ",
		"gitleaks failed: ",
	} {
		if strings.HasPrefix(rest, prefix) {
			rest = rest[len(prefix):]
			break
		}
	}

	return source, extractMeaningfulLines(rest)
}

// extractMeaningfulLines filters tool stderr output, keeping only FATAL/ERROR/WARN lines
// and discarding INFO log lines and progress bar output.
// Handles both tab (\t) and multi-space separators (zerolog vs logrus formats).
func extractMeaningfulLines(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Skip progress bar lines (download indicators from trivy-db)
		if strings.Contains(line, " MiB /") || strings.Contains(line, " p/s ") || strings.Contains(line, "[---") {
			continue
		}
		// Detect timestamped log line: starts with YYYY-MM-DD
		if len(line) > 10 && line[4] == '-' && line[7] == '-' {
			level, message := splitLogLine(line)
			switch strings.ToUpper(level) {
			case "INFO":
				continue // discard
			case "FATAL", "ERROR", "WARN", "WARNING":
				kept = append(kept, formatScanErrorLine(level, message))
				continue
			}
		}
		// Non-structured line (plain error text) — keep as-is
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return strings.TrimSpace(text) // fallback: original message
	}
	return strings.Join(kept, "\n")
}

// splitLogLine splits a timestamped log line (YYYY-MM-DDThh:mm:ssZ<sep>LEVEL<sep>message)
// into level and message. Handles tab and multi-space separators.
func splitLogLine(line string) (level, message string) {
	// Skip past the timestamp — find first whitespace after opening chars
	tEnd := strings.IndexAny(line, " \t")
	if tEnd < 0 {
		return "", line
	}
	rest := strings.TrimLeft(line[tEnd:], " \t")
	lEnd := strings.IndexAny(rest, " \t")
	if lEnd < 0 {
		return rest, ""
	}
	return rest[:lEnd], strings.TrimLeft(rest[lEnd:], " \t")
}

// formatScanErrorLine formats a FATAL/ERROR/WARN log message as "Error : <message>".
// For FATAL lines, Trivy prepends "Fatal error <sep> run error: " which is stripped.
func formatScanErrorLine(level, message string) string {
	if strings.ToUpper(level) == "FATAL" {
		if idx := strings.Index(message, "run error: "); idx >= 0 {
			message = message[idx+len("run error: "):]
		} else {
			message = strings.TrimLeft(strings.TrimPrefix(message, "Fatal error"), " \t")
		}
	}
	return "Error : " + message
}

// renderTabs renders the tab bar
func (m Model) renderTabs() string {
	cve, secrets, licenses, misconfigs := m.countFindingsByTab()

	return theme.RenderTabs([]theme.TabItem{
		{Label: fmt.Sprintf("CVE (%d)", cve)},
		{Label: fmt.Sprintf("Secrets (%d)", secrets)},
		{Label: fmt.Sprintf("Licenses (%d)", licenses)},
		{Label: fmt.Sprintf("Misconfig (%d)", misconfigs)},
	}, m.activeTab)
}

// buildDetailsContent builds the full scrollable content for the details view.
func (m Model) buildDetailsContent() string {
	if len(m.filteredFindings) == 0 || m.selectedIdx >= len(m.filteredFindings) {
		return theme.Bg("No finding selected")
	}

	f := m.filteredFindings[m.selectedIdx]
	var b strings.Builder

	// Available width inside padding (1 left + 1 right)
	contentWidth := max(m.width-6, 40)
	textStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText)
	wrapStyle := textStyle.Width(contentWidth)

	// Header: severity + ID on same line with proper background
	severityStyle := m.getSeverityStyle(f.Severity)
	b.WriteString(severityStyle.Render(string(f.Severity)) + theme.Bg(" ") + theme.TitleStyle.Render(f.ID) + "\n\n")

	// Title with wrapping
	b.WriteString(theme.SubTitleStyle.Render("Title:") + "\n")
	b.WriteString(wrapStyle.Render(f.Title) + "\n\n")

	// Description with wrapping
	if f.Description != "" {
		b.WriteString(theme.SubTitleStyle.Render("Description:") + "\n")
		b.WriteString(wrapStyle.Render(f.Description) + "\n\n")
	}

	b.WriteString(theme.SubTitleStyle.Render("Source: ") + textStyle.Render(f.Source) + "\n")

	// File path with wrapping (can be long)
	if f.File != "" {
		b.WriteString(theme.SubTitleStyle.Render("File: ") + textStyle.Width(contentWidth-6).Render(f.File) + "\n")
	}

	if f.Line > 0 {
		b.WriteString(theme.SubTitleStyle.Render("Line: ") + textStyle.Render(fmt.Sprintf("%d", f.Line)) + "\n")
	}

	if f.PkgName != "" {
		b.WriteString(theme.SubTitleStyle.Render("Package: ") + textStyle.Render(f.PkgName) + "\n")
		if f.Version != "" {
			b.WriteString(theme.SubTitleStyle.Render("Version: ") + textStyle.Render(f.Version) + "\n")
		}
		if f.FixedIn != "" {
			b.WriteString(theme.SubTitleStyle.Render("Fixed In: ") + theme.StatusOKStyle.Render(f.FixedIn) + "\n")
		}
	}

	if f.Match != "" {
		b.WriteString(theme.SubTitleStyle.Render("Match: ") + textStyle.Render(f.Match) + "\n")
	}

	// Remediation section
	if f.Resolution != "" || f.FixCommand != "" {
		b.WriteString("\n")
		b.WriteString(theme.SubTitleStyle.Render("Remediation:") + "\n")
		if f.Resolution != "" {
			b.WriteString(wrapStyle.Render(f.Resolution) + "\n")
		}
		if f.FixCommand != "" {
			b.WriteString(theme.DimStyle.Render("Run: ") + textStyle.Render(f.FixCommand) + "\n")
		}
	}

	// References section
	if len(f.References) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.SubTitleStyle.Render("References:") + "\n")
		for _, ref := range f.References {
			b.WriteString(textStyle.Width(contentWidth-2).Render("  "+ref) + "\n")
		}
	}

	return lipgloss.NewStyle().
		Padding(1).
		Background(theme.ColorBackground).
		Render(b.String())
}

// renderDetailsView renders the scrollable details viewport.
func (m Model) renderDetailsView() string {
	return m.detailsViewport.View()
}

// getSeverityStyle returns style for severity level
func (m Model) getSeverityStyle(sev scan.SeverityLevel) lipgloss.Style {
	switch sev {
	case scan.SeverityCritical:
		return lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError).Bold(true)
	case scan.SeverityHigh:
		return theme.StatusErrorStyle
	case scan.SeverityMedium:
		return theme.StatusWarningStyle
	default:
		return theme.DimStyle
	}
}

// HeaderView interface implementation

func (m Model) GetShortcuts() shortcut.Shortcuts {
	switch m.state {
	case StateInput:
		shortcuts := []shortcut.Shortcut{
			{Key: "space", Description: "Toggle"},
			{Key: "←→", Description: "Cycle value"},
			{Key: "enter / ctrl+s", Description: "Scan"},
		}
		// Show 'b' shortcut for directory and image modes on target field
		if m.focusedField == 1 {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "b", Description: "Browse"})
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: ":", Description: "Command"},
			shortcut.Shortcut{Key: "?", Description: "Help"},
		)
		return shortcuts
	case StateScanning:
		return []shortcut.Shortcut{
			// {Key: "scanning...", Description: ""},
		}
	case StateResults:
		shortcuts := []shortcut.Shortcut{
			{Key: "tab", Description: "Switch tab"},
			{Key: "enter", Description: "Details"},
		}
		if m.activeTab == TabCVE || m.activeTab == TabLicense || m.activeTab == TabMisconfig {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: ".", Description: "Filter"})
		}
		if m.activeTab == TabSecrets {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "i", Description: "Ignore"})
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "ctrl+r", Description: "New scan"},
			shortcut.Shortcut{Key: ":", Description: "Command"},
			shortcut.Shortcut{Key: "?", Description: "Help"},
		)
		return shortcuts
	case StateDetails:
		shortcuts := []shortcut.Shortcut{
			{Key: "esc/⌫", Description: "Back"},
		}
		if m.selectedIdx < len(m.filteredFindings) {
			f := m.filteredFindings[m.selectedIdx]
			if len(f.References) > 0 {
				shortcuts = append(shortcuts, shortcut.Shortcut{Key: "o", Description: "Open ref"})
			}
		}
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: ":", Description: "Command"})
		return shortcuts
	}
	return nil
}

func (m Model) GetTitle() string {
	base := theme.IconSecurity + " Security Scanner"
	if m.state == StateInput {
		return base + " " + theme.IconChevronRight + " Scan Configuration"
	}
	if m.targetPath != "" && (m.state == StateScanning || m.state == StateResults || m.state == StateDetails) {
		annotation := lipgloss.NewStyle().
			Foreground(theme.ColorSecondary).
			Background(theme.ColorBackground).
			Render("(" + m.targetPath + ")")
		return base + " " + annotation
	}
	return base
}

func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	trivyVal := "not found"
	if m.deps.TrivyAvailable {
		trivyVal = m.parseVersion(m.deps.TrivyVersion)
	}
	gitleaksVal := "not found"
	if m.deps.GitleaksAvailable {
		gitleaksVal = m.parseVersion(m.deps.GitleaksVersion)
	}

	info := []shortcut.HeaderInfo{
		{Key: "Trivy", Value: trivyVal, Style: theme.HeaderValueStyle},
		{Key: "Gitleaks", Value: gitleaksVal, Style: theme.HeaderValueStyle},
	}

	// If we have results, add scan info to header
	if m.result != nil {
		info = append(info, shortcut.HeaderInfo{
			Key:   "Duration",
			Value: m.result.Duration.Round(1e8).String(),
			Style: theme.HeaderValueStyle,
		})

		info = append(info, shortcut.HeaderInfo{
			Key:   "Filter",
			Value: strings.ToUpper(m.severityFilter),
			Style: theme.HeaderValueStyle,
		})

		// Secrets count
		info = append(info, shortcut.HeaderInfo{
			Key:   "Secrets",
			Value: fmt.Sprintf("%d", m.result.SecretCount),
			Style: theme.HeaderValueStyle,
		})

		// Licenses count
		info = append(info, shortcut.HeaderInfo{
			Key:   "Licenses",
			Value: fmt.Sprintf("%d", m.result.LicenseCount),
			Style: theme.HeaderValueStyle,
		})

		// CVE breakdown as a pill bar (vulnerabilities only) — pre-rendered, no style override
		info = append(info, shortcut.HeaderInfo{
			Key:   "CVEs",
			Value: renderSeverityBar(m.result.Counts),
		})

	}

	return info
}

// renderSeverityBar renders the color-coded CVE pill bar
func renderSeverityBar(c scan.SeverityCounts) string {
	renderBlock := func(count int, bg, fg lipgloss.Color) string {
		s := lipgloss.NewStyle().
			Background(bg).
			Foreground(fg).
			Width(4).
			Align(lipgloss.Center)
		if count == 0 {
			s = s.Foreground(theme.ColorDim)
		}
		return s.Render(fmt.Sprintf("%d", count))
	}

	return renderBlock(c.Critical, theme.ColorSeverityCritical, theme.ColorSeverityCriticalFg) +
		renderBlock(c.High, theme.ColorSeverityHigh, theme.ColorSeverityHighFg) +
		renderBlock(c.Medium, theme.ColorSeverityMedium, theme.ColorSeverityMediumFg) +
		renderBlock(c.Low, theme.ColorSeverityLow, theme.ColorSeverityLowFg) +
		renderBlock(c.Unknown, theme.ColorSeverityInfo, theme.ColorSeverityInfoFg)
}

// refreshSelectionStyle updates the table selection color to match the currently selected row's severity
func (m *Model) refreshSelectionStyle() {
	if len(m.filteredFindings) == 0 {
		return
	}
	cursor := m.findingsTable.Cursor()
	if cursor >= 0 && cursor < len(m.filteredFindings) {
		m.findingsTable.SetStyles(theme.TableStylesForSeverity(string(m.filteredFindings[cursor].Severity)))
	}
}

// parseVersion extracts version number from tool output
func (m Model) parseVersion(versionOutput string) string {
	if versionOutput == "" {
		return ""
	}

	isDocker := false
	output := versionOutput

	// Handle docker versions (format: "docker:Version X.Y.Z...")
	if strings.HasPrefix(versionOutput, "docker:") {
		isDocker = true
		output = strings.TrimPrefix(versionOutput, "docker:")
		if output == "" {
			return "(docker)"
		}
	}

	// Extract first line and try to find version number
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		if isDocker {
			return "(docker)"
		}
		return ""
	}

	firstLine := strings.TrimSpace(lines[0])
	// Try to extract version pattern (e.g., "v0.50.1" or "0.50.1")
	for _, part := range strings.Fields(firstLine) {
		if strings.HasPrefix(part, "v") || (len(part) > 0 && part[0] >= '0' && part[0] <= '9') {
			if isDocker {
				return part + " (docker)"
			}
			return part
		}
	}

	// Return first line if no version pattern found (truncated)
	result := firstLine
	if len(result) > 15 {
		result = result[:15] + "..."
	}
	if isDocker {
		return result + " (docker)"
	}
	return result
}

// openFileBrowser sends a selection request to browse directories via the workspaces view
func (m Model) openFileBrowser() (tea.Model, tea.Cmd) {
	return m, func() tea.Msg {
		return SelectionRequestMsg{Type: "directory", Message: "Select a directory to scan"}
	}
}

// openImageBrowser sends a selection request to browse images via the OCI images view
func (m Model) openImageBrowser() (tea.Model, tea.Cmd) {
	return m, func() tea.Msg {
		return SelectionRequestMsg{Type: "image", Message: "Select an image to scan"}
	}
}

// GetHelpContent retourne le contenu d'aide de la vue Security
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Security Scanner",
		Description: "This view scans directories or Docker images for vulnerabilities, exposed secrets, license issues, and IaC misconfigurations. Optionally generates SBOM in CycloneDX format. Scans use Trivy and Gitleaks via Docker or local binary.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: "g/Home", Description: "Go to top of list"},
			{Key: "G/End", Description: "Go to bottom of list"},
			{Key: "enter / ctrl+s", Description: "Start scan (from input form) / view details (in results)"},
			{Key: "space", Description: "Toggle a scan option (only key that toggles checkboxes)"},
			{Key: "ctrl+s", Description: "Start scan (from input form)"},
			{Key: "b", Description: "Browse directories (workspaces view) or Docker images (on Target field)"},
			{Key: "i", Description: "Ignore a secret (add to .gitleaksignore, in Secrets results tab)"},
			{Key: "o", Description: "Open first reference URL in the default browser (detail view)"},
			{Key: "tab / shift+tab", Description: "Switch tabs in results (CVE, Secrets, Licenses, Misconfig)"},
			{Key: "1 / 2 / 3 / 4", Description: "Jump directly to a tab"},
			{Key: ".", Description: "Cycle severity filter (in CVE/Licenses/Misconfig results)"},
			{Key: "ctrl+r", Description: "New scan (from results)"},
			{Key: "esc", Description: "Back / cancel"},
			{Key: ":", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Scan Types",
				Body:  "Vulnerability Scan: detects CVEs in dependencies and packages (Trivy).\nSecret Scan: detects exposed secrets and API keys in code (Gitleaks + Trivy).\nMisconfig Scan: detects IaC misconfigurations in Dockerfiles, Terraform, K8s manifests (Trivy).\nLicense Scan: analyzes dependency licenses (Trivy).\nSBOM Generation: generates a CycloneDX SBOM report (Trivy).",
			},
			{
				Title: "Target Types",
				Body:  "Directory: scans a local directory. Use 'b' to browse via the Workspaces view.\nImage: scans a Docker image. Use 'b' to browse via the OCI Images view, or type the name manually (e.g., nginx:latest).",
			},
			{
				Title: "Advanced Options",
				Body:  "Trivy Server: use a remote Trivy server (client-server mode). Persisted to config.\nIgnore Unfixed: only show vulnerabilities that have available fixes.\nScan Git History: scan the full git history for secrets (slower but more thorough).\nGitleaks Config: specify a custom .gitleaks.toml configuration file.",
			},
			{
				Title: "Results",
				Body:  "Results are displayed by tab (CVE, Secrets, Licenses, Misconfig). Use '.' to cycle the severity filter. Press Enter to view finding details. For secrets, 'i' adds a finding to .gitleaksignore. If SBOM was generated, its path is shown above the tabs.",
			},
			{
				Title: "Command Logging",
				Body:  "Executed scanner commands are logged to the application log file (~/.devdesk/devdesk.log).",
			},
			{
				Title: "Prerequisites",
				Body:  "Trivy and Gitleaks must be installed (local binary or Docker image). Tool status is displayed in the header status bar.",
			},
		},
	}
}
