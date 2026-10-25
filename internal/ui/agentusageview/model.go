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

// Model is the agent usage view.
type Model struct {
	table   datatable.Model[sessionRow]
	spinner spinner.Model
	footer  sharedcomponents.FooterMessage

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
	}
}

// Init starts the first read and the spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(listSessionsCmd(m.gen), m.spinner.Tick)
}
