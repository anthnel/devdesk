package status

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/status/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Tab constants
const (
	TabMonitors     = 0
	TabCertificates = 1
)

// sortField defines which column to sort by
type sortField int

const (
	sortByName sortField = iota
	sortByTarget
	sortByType
	sortByResponse
)

// Model représente l'état de la vue status
type Model struct {
	// Configuration
	config          *config.Config
	refreshInterval time.Duration

	// État du monitoring
	components []status.ComponentStatus
	lastCheck  time.Time
	nextCheck  time.Time

	// Flags d'état
	paused     bool
	checking   bool
	firstCheck bool
	error      string

	// Tables
	monitorTable table.Model
	sslTable     table.Model
	activeTab    int // TabMonitors or TabCertificates

	// Composants Bubbles
	spinner       spinner.Model
	componentForm *components.ComponentForm
	confirmModal  *sharedcomponents.ConfirmModal

	// Sorting
	sortColumn sortField
	sortAsc    bool

	// CRUD modes
	selectedIdx int // Index du composant sélectionné pour edit/delete

	// filterBar provides text search for the active table (Rule 136)
	filterBar sharedcomponents.FilterBar

	// Dimensions du terminal
	width  int
	height int
}

// New crée un nouveau model
func New(cfg *config.Config) Model {
	// Spinner pour le chargement
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	// Monitor table columns (existing monitors)
	monitorColumns := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Target", Width: 36},
		{Title: "Status", Width: 10},
		{Title: "Type", Width: 8},
		{Title: "Response", Width: 12},
	}

	// SSL table columns
	sslColumns := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Host", Width: 30},
		{Title: "Status", Width: 12},
		{Title: "Days Left", Width: 12},
		{Title: "Expires", Width: 20},
		{Title: "Issuer", Width: 30},
	}

	monitorTable := table.New(
		table.WithColumns(monitorColumns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	monitorTable.SetStyles(theme.DefaultTableStyles())

	sslTable := table.New(
		table.WithColumns(sslColumns),
		table.WithFocused(false),
		table.WithHeight(10),
	)
	sslTable.SetStyles(theme.BlurredTableStyles())

	return Model{
		config:          cfg,
		refreshInterval: time.Duration(cfg.Status.RefreshInterval) * time.Second,
		paused:          !cfg.Status.AutoRefresh,
		checking:        false,
		firstCheck:      true,
		spinner:         s,
		monitorTable:    monitorTable,
		sslTable:        sslTable,
		activeTab:       TabMonitors,
		sortColumn:      sortByName,
		sortAsc:         true,
		error:           "",
		filterBar:       sharedcomponents.NewFilterBar(),
	}
}

// Messages Bubble Tea

// TickMsg - Tick toutes les secondes pour mettre à jour le countdown
type TickMsg time.Time

// CheckStartedMsg - Un check vient de démarrer
type CheckStartedMsg struct{}

// CheckCompleteMsg - Résultat d'un check de tous les composants
type CheckCompleteMsg struct {
	Components []status.ComponentStatus
	Timestamp  time.Time
	Err        error
}

// ComponentSavedMsg - Un composant a été sauvegardé
type ComponentSavedMsg struct {
	Success bool
	Error   error
}

// ComponentDeletedMsg - Un composant a été supprimé
type ComponentDeletedMsg struct {
	Success bool
	Error   error
}
