// Package agentusageview is the `:agent-usage` screen: the Claude Code
// sessions found on this machine.
//
// It reads transcripts' working directory and date only, never their content
// (internal/agentusage owns that guarantee).
package agentusageview

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// sessionRow is one table row. project is computed once, in Update, so a Cell
// never touches the file system or the environment.
type sessionRow struct {
	agentusage.Session
	project string // theme.FoldHome(Project), or "-" when unknown
}

// sessionListLoadedMsg carries one listing. gen drops an answer to a read that
// a newer one has superseded.
type sessionListLoadedMsg struct {
	gen     int
	root    string
	listing agentusage.Listing
	err     error
}

// sessionUsageLoadedMsg carries one parsed session. gen is agentGen at the time
// the parse started: it drops the answer for a session left or re-read since.
type sessionUsageLoadedMsg struct {
	gen   int
	usage agentusage.Usage
	err   error
}

// level is the screen on view: the sessions, or the agents of one session.
type level int

const (
	levelSessions level = iota
	levelAgents
)

// Model is the agent usage view.
type Model struct {
	table   datatable.Model[sessionRow]
	spinner spinner.Model
	footer  sharedcomponents.FooterMessage

	// level says which table is on screen. The sessions table keeps its cursor
	// and filter while the agents level is open.
	level level
	// agentTable lists the agents of open.
	agentTable datatable.Model[agentRow]
	open       agentusage.Session
	// usage is the last parse of open, kept for what the footer says about it.
	usage agentusage.Usage
	// agentGen numbers the parses, as gen numbers the listings.
	agentGen      int
	agentsLoading bool
	// agentsErr says the most recent parse failed (a state, like loadErr).
	agentsErr bool
	// width and height are the last viewport size, for both tables.
	width, height int

	// gen numbers the reads; only the answer to the latest one is kept.
	gen     int
	loading bool
	// loadErr says the most recent read failed. It is a state rather than a
	// timed message so the empty-listing sentence cannot take its place.
	loadErr bool
	// root is the projects directory the last successful read listed.
	root    string
	omitted int
}

// New builds the view with its first read pending.
func New(_ *config.Config) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle()

	return Model{
		loading: true,
		spinner: sp,
		table: datatable.New(datatable.Config[sessionRow]{
			Columns: sessionColumns(),
			// The listing arrives newest first, which is the order wanted.
			SortColumn: -1,
		}),
		agentTable: datatable.New(datatable.Config[agentRow]{
			Columns: agentColumns(),
			// Opens as the tree (orchestrator, then sub-agents); `.` sorts by Total.
			SortColumn: -1,
		}),
	}
}

// Init starts the first read and the spinner.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{listSessionsCmd(m.gen), m.spinner.Tick}
	if m.level == levelAgents {
		cmds = append(cmds, parseSessionCmd(m.agentGen, m.open.Parent))
	}
	return tea.Batch(cmds...)
}
