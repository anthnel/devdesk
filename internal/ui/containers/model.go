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

// The last two, named for the same reason as the first three: something else
// refers to them. Counting from columnImage at each site is what broke the day
// Ports and Created traded places — the six metric columns sit between, so the
// offsets are right up until the order changes and then wrong in silence.
// TestTheNamedColumnsAreWhereTheirNamesSay is what keeps these honest.
const (
	columnPorts   = columnImage + 9
	columnCreated = columnImage + 10
)

// The state glyph column carries no title — the icons say what they are — and
// it must not sort: `datatable` reserves two cells beyond MinWidth for any
// column with a comparator, to hold its arrow, which is expensive for a glyph.
//
// Its width is datatable.IconColumnWidth, like the workspaces and inventory
// glyph columns. It declared 3 of its own, which put one more cell between the
// glyph and the name here than in the other two tables — a gap nothing chose,
// visible only when the three screens are compared.

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
			Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
			Cell:  func(c docker.Container) string { return stateIcon(c.State) },
			Style: containerStateStyle,
		},
		{
			Title: "Name", Sizing: datatable.SizingContent, MinWidth: 14, Flex: 2,
			Cell:   func(c docker.Container) string { return c.Name },
			Less:   func(a, b docker.Container) bool { return strings.ToLower(a.Name) < strings.ToLower(b.Name) },
			Search: func(c docker.Container) string { return c.Name },
		},
		{
			Title: "Image", Sizing: datatable.SizingContent,
			MinWidth: 20, MaxWidth: 44, Flex: 3, TruncateHead: true,
			Cell:   func(c docker.Container) string { return c.Image },
			Less:   func(a, b docker.Container) bool { return strings.ToLower(a.Image) < strings.ToLower(b.Image) },
			Search: func(c docker.Container) string { return c.Image + " " + c.State },
		},
		{
			Title: "CPU", Sizing: datatable.SizingFixed, MinWidth: 8,
			Cell: func(c docker.Container) string {
				if !running(c) {
					return "-"
				}
				return fmt.Sprintf("%.1f%%", c.CPUPercent)
			},
			Less: func(a, b docker.Container) bool { return a.CPUPercent < b.CPUPercent },
		},
		gaugeColumn("1 core", func(c docker.Container) float64 { return c.CPUPercent }),
		{
			Title: "Mem", Sizing: datatable.SizingFixed, MinWidth: 12,
			Cell: func(c docker.Container) string {
				if !running(c) {
					return "-"
				}
				return formatMemUsage(c.MemUsage)
			},
			Less: func(a, b docker.Container) bool { return a.MemPercent < b.MemPercent },
		},
		gaugeColumn("Limit", func(c docker.Container) float64 { return c.MemPercent }),
		{
			Title: "Net RX", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 9,
			Cell: transfer(netIO, func(c docker.Container) int64 { return c.NetRX }),
			Less: byInt64(func(c docker.Container) int64 { return c.NetRX }),
		},
		{
			Title: "Net TX", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 9,
			Cell: transfer(netIO, func(c docker.Container) int64 { return c.NetTX }),
			Less: byInt64(func(c docker.Container) int64 { return c.NetTX }),
		},
		{
			Title: "Block RX", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 10,
			Cell: transfer(blockIO, func(c docker.Container) int64 { return c.BlockRX }),
			Less: byInt64(func(c docker.Container) int64 { return c.BlockRX }),
		},
		{
			Title: "Block TX", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 10,
			Cell: transfer(blockIO, func(c docker.Container) int64 { return c.BlockTX }),
			Less: byInt64(func(c docker.Container) int64 { return c.BlockTX }),
		},
		{
			Title: "Ports", Sizing: datatable.SizingContent, MinWidth: 16, Flex: 2,
			Cell: portsCell,
			// Search matches what is on screen, icons and all. Keeping the raw
			// docker string here instead is the tempting version and the wrong
			// one: a filter would then find a row the user cannot see matching,
			// which is Rule 122's hazard one layer up.
			Search: portsCell,
		},
		{
			Title: "Created", Sizing: datatable.SizingFixed, Optional: true, MinWidth: 12,
			Cell: func(c docker.Container) string { return relativeTime(c.CreatedAt) },
			// CreatedAt is compared as the string docker printed, so a value
			// that will not parse sorts after every timestamp rather than
			// silently becoming the zero time and leading the list.
			Less: func(a, b docker.Container) bool { return a.CreatedAt < b.CreatedAt },
		},
	}
}

// gauge is what the two bar columns share: everything except which percentage
// they read and what its full scale is called.
//
// Both are Optional *and* DropFirst. Optional because a bar is never the only
// carrier of what it shows — the number it sits beside survives it, and
// survives the selected row too, where Style is not consulted at all
// (Rule 122). DropFirst because a gauge sits where the number it illustrates
// is, in the middle of the table: left to position alone it would outlive the
// I/O counters to its right, which is the opposite of what a decoration
// deserves (§3.71).
//
// It carries no Less and no Search. A comparator would duplicate the sort the
// number column already offers, and cost two more cells to fit its arrow; a
// bar is not text anyone can type.
func gaugeColumn(scale string, pct func(docker.Container) float64) datatable.Column[docker.Container] {
	return datatable.Column[docker.Container]{
		Title: scale, Sizing: datatable.SizingFixed, MinWidth: theme.GaugeWidth,
		Optional: true, DropFirst: true,
		Cell: func(c docker.Container) string {
			if c.State != "running" {
				return "-"
			}
			return theme.Gauge(pct(c), theme.GaugeWidth)
		},
		Style: func(c docker.Container) lipgloss.Style {
			if c.State != "running" {
				// A placeholder is dim, like every other "-" in this table.
				return theme.DimStyle
			}
			return theme.LoadTextStyle(pct(c))
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
		// No opinion here: the table sets the theme's text color.
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

// The state filter tokens (Rule 136). They replace `a`, a two-position switch
// — running only, or everything — whose one piece of feedback was a word in the
// header, which is precisely where Rule 136 says a filter does not go. The
// ports tab already had the mechanism; this is the same one.
//
// There are four rather than one per docker state because the mapping has to be
// total: a state named by no token would be a container hidden with nothing on
// screen saying which filter hid it. They are exactly the four groups
// containerStateStyle already colours, so the bar and the status column say the
// same thing.
const (
	filterTokenRunning   = "running"
	filterTokenPaused    = "paused"
	filterTokenStopped   = "stopped"
	filterTokenTransient = "transient"
)

// stateTokens is the declaration order, which is the order the bar shows them
// in and the order `z` walks to clear them.
var stateTokens = []string{
	filterTokenRunning, filterTokenPaused, filterTokenStopped, filterTokenTransient,
}

// defaultStateToken is what an empty selection shows.
//
// It is the view's resting state rather than a token that starts lit: a lit
// token keeps the filter bar on screen from the first frame, and the bar is not
// the point — the running containers are. So the view opens with no token, the
// bar hidden, and this is the set. `z` returns here, which is why arriving and
// pressing `z` now land on the same screen instead of two different ones.
//
// The cost, stated rather than discovered: the opening scope is not written
// anywhere on screen. It is the one thing the old `a` got wrong, and it is kept
// on purpose — the difference is that the moment the user has an opinion, the
// bar appears and names every state it is showing, which `a` never did.
const defaultStateToken = filterTokenRunning

// stateToken names the token a docker state belongs to.
//
// The default branch is what makes it total: `exited` and `dead` land there
// today, and so does anything docker starts reporting tomorrow — a state the
// application does not recognise still belongs to a container that is not
// running, which is a row the user can reach rather than one that has vanished.
func stateToken(state string) string {
	switch state {
	case "running":
		return filterTokenRunning
	case "paused":
		return filterTokenPaused
	case "created", "restarting", "removing":
		return filterTokenTransient
	default: // exited, dead, and whatever comes next
		return filterTokenStopped
	}
}

// matchContainerTokens applies the state filter: OR between the tokens that are
// on, and defaultStateToken when none of them is.
//
// The empty selection is the view's default rather than "everything", which is
// where this parts company with the ports tab: there, nothing on means no
// opinion and shows every socket. Here the resting state is an opinion — the
// running containers — and it has to be, because it is what keeps the bar
// hidden on arrival. "Everything" is therefore the four tokens together, which
// is what it literally is; there is no `all` token, because that would be a
// fifth state to select alongside four real ones.
func matchContainerTokens(c docker.Container, active map[string]bool) bool {
	token := stateToken(c.State)
	anyOn := false
	for _, label := range stateTokens {
		if !active[label] {
			continue
		}
		anyOn = true
		if token == label {
			return true
		}
	}
	if anyOn {
		return false
	}
	return token == defaultStateToken
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
			// None of them starts on: the bar is hidden on arrival, and
			// matchContainerTokens reads the empty selection as
			// defaultStateToken. See the note there.
			Tokens: []sharedcomponents.FilterToken{
				{Label: filterTokenRunning},
				{Label: filterTokenPaused},
				{Label: filterTokenStopped},
				{Label: filterTokenTransient},
			},
			TokenMatch: matchContainerTokens,
			// The ID rather than the name: an action is issued against the ID,
			// and two containers can carry the same name across a recreate.
			Key:          func(c docker.Container) string { return c.ID },
			StatusColumn: columnStatus,
		}),
		loading: true,
	}
}
