package containers

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// viewState represents the current display state of the containers view
type viewState int

const (
	stateTable viewState = iota
	stateLogs
)

// Model represents the containers view state
type Model struct {
	config         *config.Config
	showAll        bool
	containerTable datatable.Model[docker.Container]
	spinner        spinner.Model
	loading        bool
	errorMsg       string
	confirmModal   *sharedcomponents.ConfirmModal
	pendingAction  string
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

// columnName indexes containerColumns. Only the ones something else refers to
// are named; the sort cycle walks them by position.
const (
	columnName = iota
	columnImage
	columnPorts = 9
)

// containerColumns describes the containers table.
//
// The state is not a column of its own — it is the icon prefixed to the image —
// but the filter has always matched it, so the Image column searches both.
func containerColumns() []datatable.Column[docker.Container] {
	// Metrics only mean anything while the container runs: docker reports the
	// last values it saw for the rest, which is why they read "-" rather than a
	// number that stopped being true when the container did.
	running := func(c docker.Container) bool { return c.State == "running" }
	transfer := func(io func(docker.Container) string, bytes func(docker.Container) int64) func(docker.Container) string {
		return func(c docker.Container) string {
			if !running(c) || io(c) == "" {
				return "-"
			}
			return formatNetBytes(bytes(c))
		}
	}
	netIO := func(c docker.Container) string { return c.NetIO }
	blockIO := func(c docker.Container) string { return c.BlockIO }
	byInt64 := func(get func(docker.Container) int64) func(a, b docker.Container) bool {
		return func(a, b docker.Container) bool { return get(a) < get(b) }
	}

	return []datatable.Column[docker.Container]{
		{
			Title: "Name", MinWidth: 14, Flex: 2,
			Cell:   func(c docker.Container) string { return c.Name },
			Less:   func(a, b docker.Container) bool { return strings.ToLower(a.Name) < strings.ToLower(b.Name) },
			Search: func(c docker.Container) string { return c.Name },
		},
		{
			Title: "Image", MinWidth: 20, Flex: 3,
			Cell:   func(c docker.Container) string { return stateIcon(c.State) + " " + c.Image },
			Style:  containerStateStyle,
			Less:   func(a, b docker.Container) bool { return strings.ToLower(a.Image) < strings.ToLower(b.Image) },
			Search: func(c docker.Container) string { return c.Image + " " + c.State },
		},
		{
			Title: "CPU", MinWidth: 8,
			Cell: func(c docker.Container) string {
				if !running(c) {
					return "-"
				}
				return fmt.Sprintf("%.1f%%", c.CPUPercent)
			},
			Less: func(a, b docker.Container) bool { return a.CPUPercent < b.CPUPercent },
		},
		{
			Title: "Mem", MinWidth: 12,
			Cell: func(c docker.Container) string {
				if !running(c) {
					return "-"
				}
				return formatMemUsage(c.MemUsage)
			},
			Less: func(a, b docker.Container) bool { return a.MemPercent < b.MemPercent },
		},
		{
			Title: "Net RX", MinWidth: 9,
			Cell: transfer(netIO, func(c docker.Container) int64 { return c.NetRX }),
			Less: byInt64(func(c docker.Container) int64 { return c.NetRX }),
		},
		{
			Title: "Net TX", MinWidth: 9,
			Cell: transfer(netIO, func(c docker.Container) int64 { return c.NetTX }),
			Less: byInt64(func(c docker.Container) int64 { return c.NetTX }),
		},
		{
			Title: "Block RX", MinWidth: 10,
			Cell: transfer(blockIO, func(c docker.Container) int64 { return c.BlockRX }),
			Less: byInt64(func(c docker.Container) int64 { return c.BlockRX }),
		},
		{
			Title: "Block TX", MinWidth: 10,
			Cell: transfer(blockIO, func(c docker.Container) int64 { return c.BlockTX }),
			Less: byInt64(func(c docker.Container) int64 { return c.BlockTX }),
		},
		{
			Title: "Created", MinWidth: 12,
			Cell: func(c docker.Container) string { return relativeTime(c.CreatedAt) },
			// CreatedAt is compared as the string docker printed, so a value
			// that will not parse sorts after every timestamp rather than
			// silently becoming the zero time and leading the list.
			Less: func(a, b docker.Container) bool { return a.CreatedAt < b.CreatedAt },
		},
		{
			Title: "Ports", MinWidth: 16, Flex: 2,
			Cell: func(c docker.Container) string { return c.Ports },
		},
	}
}

// containerStateStyle colours the Image cell, which is where the state icon is,
// by that state.
//
// A running container is left in the default text colour rather than painted
// green. Almost every row is running, so colouring them would put a colour on
// the whole table and a signal on none of it — the colour is here to pick out
// the containers that stopped, or that are on their way somewhere.
func containerStateStyle(c docker.Container) lipgloss.Style {
	switch c.State {
	case "exited", "dead":
		return theme.StatusErrorStyle
	case "paused":
		return theme.StatusWarningStyle
	case "created", "restarting":
		return lipgloss.NewStyle().Foreground(theme.ColorHighlight)
	default:
		return lipgloss.NewStyle().Foreground(theme.ColorText)
	}
}

// containerSelectedStyles paints the selected row as an error when the
// container is not coming back on its own. It is the whole of what
// refreshSelectionStyle used to do, minus its replay of filter-then-sort to
// find out what the cursor was on.
func containerSelectedStyles(c docker.Container) table.Styles {
	if c.State == "exited" || c.State == "dead" {
		return theme.TableStylesForState("error")
	}
	return theme.TableStylesForState("normal")
}

// New creates a new containers view
func New(cfg *config.Config) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = theme.SpinnerStyle()

	return Model{
		config:  cfg,
		spinner: s,
		containerTable: datatable.New(datatable.Config[docker.Container]{
			Columns: containerColumns(),
			// Explicit rather than left at the zero value, which would open the
			// list Z→A and disagree with the status view (D9).
			SortColumn:     columnName,
			SelectedStyles: containerSelectedStyles,
		}),
		loading:      true,
		logsViewport: viewport.New(0, 0),
	}
}
