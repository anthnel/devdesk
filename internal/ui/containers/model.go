package containers

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// viewState represents the current display state of the containers view
type viewState int

const (
	stateTable viewState = iota
	stateLogs
)

// sortField defines which column to sort by
type sortField int

const (
	sortByName sortField = iota
	sortByImage
	sortByCPU
	sortByMem
	sortByNetRX
	sortByNetTX
	sortByBlockRX
	sortByBlockTX
	sortByCreated
)

// Model represents the containers view state
type Model struct {
	config         *config.Config
	containers     []docker.Container
	showAll        bool
	filterBar      sharedcomponents.FilterBar
	containerTable table.Model
	spinner        spinner.Model
	loading        bool
	errorMsg       string
	confirmModal   *sharedcomponents.ConfirmModal
	pendingAction  string
	sortColumn     sortField
	sortAsc        bool
	width, height  int

	// Logs view state
	state             viewState
	logsViewport      viewport.Model
	logsContainerID   string
	logsContainerName string
	logsLoading       bool
	logsRawContent    string
	logsWrapEnabled   bool
	logsTimestamps    bool
}

// Messages

// RefreshTickMsg triggers periodic refresh
type RefreshTickMsg time.Time

// ContainersListMsg contains fetched containers
type ContainersListMsg struct {
	Containers []docker.Container
	Err        error
}

// ContainerMetricsMsg contains fetched metrics
type ContainerMetricsMsg struct {
	Metrics map[string]docker.Container
	Err     error
}

// ContainerActionMsg signals result of a container action (stop, restart, remove)
type ContainerActionMsg struct {
	Action string
	ID     string
	Err    error
}

// ContainerPruneMsg signals result of a container prune operation
type ContainerPruneMsg struct {
	Output string
	Err    error
}

// PagerExitMsg signals return from a pager (less) or shell process
type PagerExitMsg struct {
	Err error
}

// ShellWindowOpenedMsg signals the result of launching a shell in a new terminal window
type ShellWindowOpenedMsg struct {
	Err error
}

// ContainerLogsLoadedMsg contains the fetched container logs content
type ContainerLogsLoadedMsg struct {
	Content string
	Err     error
}

// New creates a new containers view
func New(cfg *config.Config) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	columns := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Image", Width: 25},
		{Title: "CPU", Width: 14},
		{Title: "Mem", Width: 14},
		{Title: "Net RX", Width: 10},
		{Title: "Net TX", Width: 10},
		{Title: "Block RX", Width: 10},
		{Title: "Block TX", Width: 10},
		{Title: "Created", Width: 16},
		{Title: "Ports", Width: 20},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(theme.DefaultTableStyles())

	vp := viewport.New(0, 0)

	return Model{
		config:         cfg,
		spinner:        s,
		filterBar:      sharedcomponents.NewFilterBar(),
		containerTable: t,
		loading:        true,
		logsViewport:   vp,
		// Explicit rather than left at the zero value, which would open the
		// list Z→A and disagree with the status view (D9).
		sortColumn: sortByName,
		sortAsc:    true,
	}
}
