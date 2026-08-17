package containers

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The logs pane used to live here — a viewState, a viewport, soft wrap, ANSI
// stripping, scroll keys, reload, follow, a timestamps toggle and an external
// pager. None of it was specific to containers except the last three, and those
// are properties of where the text came from rather than of the pane, so the
// whole thing moved into the document viewer and the three became a Source's
// optional capabilities (see sources.go). `l` and `i` now emit one message and
// this view goes back to being a table.

// Model represents the containers view state
type Model struct {
	config         *config.Config
	showAll        bool
	containerTable datatable.Model[docker.Container]
	spinner        spinner.Model
	loading        bool
	// footer is the one line of transient state below the viewport (Rule 128).
	footer       sharedcomponents.FooterMessage
	confirmModal *sharedcomponents.ConfirmModal
	// choiceModal is K's Stop / Restart / Cancel. Separate from confirmModal
	// because the two answer different messages, and one field holding either
	// would make the handler guess which question was asked.
	choiceModal *sharedcomponents.ChoiceModal
	// pendingAction is the confirm modal's routing key, and only that.
	//
	// It used to be two fields in one: the routing key ("confirm-delete",
	// "confirm-prune") that handleConfirmYes reads, and a sentence for the user
	// ("Stopping web") that nothing ever rendered. Carrying both is why nobody
	// noticed the second had no reader — the field was plainly in use. What the
	// user is told now comes from the table's busy set.
	pendingAction string
	// pruning is the one action that belongs to no row: it acts on the whole
	// list, so marking every row would say something false. It gets a footer
	// line of its own instead.
	pruning       bool
	width, height int
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

// ContainerActionMsg signals result of a container action (stop, restart, remove).
//
// ID used to be the container's *name*: every command filled it from its `name`
// argument. Nothing caught it because the only reader was a log line, where a
// name reads perfectly well. It matters now — the busy marker is keyed on the
// ID, so a message carrying a name would never lift it and the row would spin
// for the life of the view.
type ContainerActionMsg struct {
	Action string
	ID     string
	Name   string
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

// columnStatus indexes containerColumns. Only the ones something else refers to
// are named; the sort cycle walks them by position.
const (
	columnStatus = iota
	columnName
	columnImage
)

// statusColumnWidth is the state glyph and nothing else. It carries no title —
// the icons say what they are — and it must not sort: `datatable` reserves two
// cells beyond MinWidth for any column with a comparator, to hold its arrow,
// which is expensive for a glyph.
const statusColumnWidth = 3

// containerColumns describes the containers table.
//
// The state has a column of its own (§3.22). It used to be an icon prefixed to
// the Image cell, which is why that column had to search `Image + State` to
// compensate for the icon not being where it belonged; the search still matches
// both, because a container is looked for by either.
//
// That column is also where a running action shows its spinner. The two are one
// glyph deliberately: a row answers "what is this" and "what is happening to
// it" in the same place, and `datatable` owns the precedence — busy wins.
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
			Title: "", MinWidth: statusColumnWidth,
			Cell:  func(c docker.Container) string { return stateIcon(c.State) },
			Style: containerStateStyle,
		},
		{
			Title: "Name", MinWidth: 14, Flex: 2,
			Cell:   func(c docker.Container) string { return c.Name },
			Less:   func(a, b docker.Container) bool { return strings.ToLower(a.Name) < strings.ToLower(b.Name) },
			Search: func(c docker.Container) string { return c.Name },
		},
		{
			Title: "Image", MinWidth: 20, Flex: 3,
			Cell:   func(c docker.Container) string { return c.Image },
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

// containerStateStyle colours the status column by the state its glyph shows.
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
		// Aucune opinion : c'est la table qui pose la couleur de texte du thème.
		return lipgloss.NewStyle()
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
			// The ID rather than the name: an action is issued against the ID,
			// and two containers can carry the same name across a recreate.
			Key:          func(c docker.Container) string { return c.ID },
			StatusColumn: columnStatus,
		}),
		loading: true,
	}
}
