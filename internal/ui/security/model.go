package security

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ViewState represents the current state of the security view.
//
// There were five. StateInput was the form, and StateScanning the screen that
// ran one scan in place while the user watched it — both went with the form
// (phase 3). What replaced them is the inventory, which rescans in the
// background with a spinner on the row, so nothing waits on a whole screen.
type ViewState int

const (
	StateInventory ViewState = iota // What this context has scanned, read from the caches
	StateResults                    // Displaying results summary
	StateDetails                    // Showing detailed findings
)

// Model represents the security scanner view
type Model struct {
	config *config.Config
	width  int
	height int

	state  ViewState
	result *scan.Result

	// Inventory state
	inventory datatable.Model[scanTarget]
	// spinner animates the rows being rescanned; it is stamped onto them by
	// setInventory, because a Cell function is built once and cannot reach here.
	//
	// It survives only to produce the tick Cmd. What lands in a cell is
	// spinnerFrameIdx, because spinner.View() renders through a style and a
	// table cell carries no escape sequence (Rule 122).
	spinner         spinner.Model
	spinnerFrameIdx int

	// targetPath is what the result on screen is about — an image reference or a
	// repository path. It names the target in the title, and it is the directory
	// .gitleaksignore is written into.
	targetPath string

	// Results state
	findingsTable datatable.Model[scan.Finding]
	// selectedFinding is what the details view is showing. It holds the finding
	// rather than its row index, so a list that changes underneath cannot make
	// the details describe a different one.
	selectedFinding *scan.Finding
	activeTab       int // 0=CVE, 1=Secrets, 2=Licenses, 3=Misconfig

	// OriginView is the view to return to when Esc is pressed in StateResults.
	// Set by the app router when opening this view from workspaces or
	// oci_resources. Empty when the results were opened from the inventory,
	// which is inside this view and needs no round trip.
	OriginView command.ViewType

	// Status message (temporary feedback)
	statusMessage string

	// Details view scrollable viewport
	detailsViewport viewport.Model

	// Ignore secret confirmation
	confirmModal    *sharedcomponents.ConfirmModal
	findingToIgnore *scan.Finding

	// scanAllModal carries the purge checkbox for A. It is separate from
	// confirmModal because the two answer different messages, and one field
	// holding either would make the handler guess which question was asked.
	scanAllModal *sharedcomponents.OptionConfirmModal
}

// New creates a new security scanner view, on the inventory.
func New(cfg *config.Config) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	t := datatable.New(datatable.Config[scan.Finding]{
		Columns:        findingColumns(),
		SortColumn:     -1, // the order the scanner reported
		SelectedStyles: findingSelectedStyles,
		Tokens:         severityTokens(),
		TokenMatch:     matchesSeverity,
	})

	return Model{
		config:        cfg,
		state:         StateInventory,
		inventory:     newInventoryTable(),
		spinner:       s,
		findingsTable: t,
	}
}

// severityTokens are the four cumulative severity filters (Rule 136).
//
// They replaced a cycle on `.` — which also cost the table its sort key. A
// cycle can only ask "at least this severe", and the question actually asked is
// "CRITICAL and HIGH", which is not a threshold: it excludes MEDIUM while
// including CRITICAL. Four toggles can say it and a cycle never could.
func severityTokens() []sharedcomponents.FilterToken {
	return []sharedcomponents.FilterToken{
		{Label: "critical"}, {Label: "high"}, {Label: "medium"}, {Label: "low"},
	}
}

// severityToken maps a finding's severity onto its token label. UNKNOWN has no
// token, so it is only ever hidden by an explicit choice of others.
var severityToken = map[scan.SeverityLevel]string{
	scan.SeverityCritical: "critical",
	scan.SeverityHigh:     "high",
	scan.SeverityMedium:   "medium",
	scan.SeverityLow:      "low",
}

// matchesSeverity keeps everything while no token is active: an empty filter
// means "no opinion", not "nothing".
func matchesSeverity(f scan.Finding, active map[string]bool) bool {
	if len(active) == 0 {
		return true
	}
	return active[severityToken[f.Severity]]
}

// NewWithPreloadedResult creates a security view showing a stored scan result.
// Used to display cached results without re-scanning (Rule 126).
func NewWithPreloadedResult(cfg *config.Config, result *scan.Result) Model {
	m := New(cfg)
	m.state = StateResults
	m.result = result
	m.targetPath = result.Target
	m.activeTab = TabCVE
	// Filled here rather than waiting for the first WindowSizeMsg: the rows do
	// not depend on the width any more, so nothing was gained by deferring and
	// the table was empty until the terminal happened to report its size.
	m.updateFindingsTable()
	return m
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		// Loaded whatever the opening state: a view opened on a result returns
		// to the inventory on ctrl+r, and reading two small files is cheaper
		// than the branch that would decide not to.
		loadInventoryCmd(),
	)
}

// InEditMode reports whether a field or a modal has the keyboard, which is what
// stops the router claiming ":", "q" and "?" for itself.
//
// The results and details states used to be listed here too, for one reason: it
// was the only way to be handed esc. The router forwards esc unconditionally now
// (§1.3 D15), so those states are ordinary again — and get the command line, the
// help overlay and quit back with them.
func (m Model) InEditMode() bool {
	switch {
	case m.confirmModal != nil, m.scanAllModal != nil:
		return true
	case m.state == StateInventory:
		return m.inventory.InEditMode()
	case m.state == StateResults:
		// The findings table gained a filter bar with the severity tokens, so
		// this state has a focused field to declare like any other.
		return m.findingsTable.InEditMode()
	}
	return false
}
