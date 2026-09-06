package status

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/status/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Tab constants
const (
	TabMonitors     = 0
	TabCertificates = 1
)

// Model represents the state of the status view
type Model struct {
	// Configuration
	config          *config.Config
	refreshInterval time.Duration

	// Monitoring state
	components []status.ComponentStatus
	lastCheck  time.Time
	nextCheck  time.Time

	// State flags
	//
	// autoRefresh comes from status.auto_refresh and has no other writer: the
	// setting belongs to the configuration view, and this view had a `space`
	// toggle that shadowed it in memory only — same shape as the +/- interval
	// it lost for the same reason.
	autoRefresh bool
	checking    bool
	// footer is the one line of transient state below the tab bar (Rule 128).
	// This view raises no message; it carries the spinner the check is reported
	// with.
	footer     sharedcomponents.FooterMessage
	firstCheck bool
	error      string

	// Tables. Two datatables plus a focus helper, not a multi-table
	// abstraction: they share a viewport and alternate focus, and that is all
	// they share.
	monitorTable datatable.Model[status.ComponentStatus]
	sslTable     datatable.Model[status.ComponentStatus]
	activeTab    int // TabMonitors or TabCertificates

	// Bubbles components
	spinner       spinner.Model
	componentForm *components.ComponentForm
	confirmModal  *sharedcomponents.ConfirmModal

	// CRUD modes
	selectedIdx int // Index of the component selected for edit/delete

	// filterBar provides text search for the active table (Rule 136)
	filterBar sharedcomponents.FilterBar

	// Terminal dimensions
	width  int
	height int
}

// New creates a new model
func New(cfg *config.Config) Model {
	// Spinner for loading
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	monitorTable := datatable.New(datatable.Config[status.ComponentStatus]{
		Columns:    monitorColumns(),
		SortColumn: columnName,
	})

	sslTable := datatable.New(datatable.Config[status.ComponentStatus]{
		Columns:    sslColumns(),
		SortColumn: -1, // config order, which is the order the user wrote
	})
	sslTable.Blur()

	return Model{
		config:          cfg,
		refreshInterval: time.Duration(cfg.Status.RefreshInterval) * time.Second,
		autoRefresh:     cfg.Status.AutoRefresh,
		checking:        false,
		firstCheck:      true,
		spinner:         s,
		monitorTable:    monitorTable,
		sslTable:        sslTable,
		activeTab:       TabMonitors,
		error:           "",
		filterBar:       sharedcomponents.NewFilterBar(),
	}
}

// Messages Bubble Tea

// TickMsg - Ticks every second to update the countdown
type TickMsg time.Time

// CheckStartedMsg - A check has just started
type CheckStartedMsg struct{}

// CheckCompleteMsg - Result of a check of all components
type CheckCompleteMsg struct {
	Components []status.ComponentStatus
	Timestamp  time.Time
	Err        error
}

// ComponentSavedMsg - A component has been saved
type ComponentSavedMsg struct {
	Success bool
	Error   error
}

// ComponentDeletedMsg - A component has been deleted
type ComponentDeletedMsg struct {
	Success bool
	Error   error
}
