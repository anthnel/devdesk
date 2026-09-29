// Package filebrowser is the `:files` view (§3.95): a browser over the whole
// filesystem that creates and deletes entries, and — lent by the router — the
// picker a form's path field opens with enter to choose a path.
//
// It is the workspaces view without the git: no enrichment walk, no scan
// columns, no root it cannot leave. What the two share is what a row is, which
// is internal/fsbrowse.
package filebrowser

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/fsbrowse"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
)

// mode is what holds the keyboard. A picker is not a mode: it is the same table
// with a different enter, so it is a field (pick) rather than a state here.
type mode int

const (
	modeNormal mode = iota
	modeCreating
	modeConfirmingDelete
)

// Model is the file browser.
type Model struct {
	config *config.Config
	mode   mode

	// currentPath is the directory listed; requested is the one a load is in
	// flight for. A listing is accepted only for requested — the stale-listing
	// guard ws has as listingPath — and currentPath moves only once it has
	// arrived, so a directory that cannot be read leaves the screen on the one
	// that could.
	currentPath string
	requested   string
	// pendingSelect is the path the cursor lands on once the listing arrives:
	// the directory just left on ←, the entry just created on N.
	pendingSelect string

	table   datatable.Model[row]
	footer  sharedcomponents.FooterMessage
	confirm *sharedcomponents.ConfirmModal
	pending *fsbrowse.Entry // what the confirmation is about
	form    *createForm

	// pick is non-nil when the view is lent as a picker.
	pick *PickRequestMsg

	jobs          []jobs.Run
	width, height int
}

// row is one line of the table. self marks the picker's "." row — the browsed
// directory itself, offered first so an empty directory can still be chosen.
type row struct {
	fsbrowse.Entry
	self bool
}

// New builds the `:files` view, opened on the home directory.
func New(cfg *config.Config) Model {
	return Model{
		config:      cfg,
		requested:   homeDir(),
		currentPath: "",
		table: datatable.New(datatable.Config[row]{
			Columns:    columns(),
			SortColumn: -1, // fsbrowse.List's order: directories first, then by name
		}),
	}
}

// NewPicker builds the view lent to a form: it opens where the field points,
// and enter chooses instead of opening.
func NewPicker(cfg *config.Config, req PickRequestMsg) Model {
	m := New(cfg)
	m.pick = &req
	start, selected := startOf(req)
	m.requested = start
	m.pendingSelect = selected
	return m
}

// startOf decides where a picker opens: the field's value when it is a
// directory, its parent with the cursor on it when it is a file, the nearest
// existing ancestor otherwise, and home when the field is empty.
func startOf(req PickRequestMsg) (dir, selected string) {
	near := fsbrowse.NearestExisting(req.Start)
	if near == "" {
		return homeDir(), ""
	}
	if info, err := os.Stat(near); err == nil && !info.IsDir() {
		return filepath.Dir(near), near
	}
	return near, ""
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return string(filepath.Separator)
}

// Init lists the starting directory.
func (m Model) Init() tea.Cmd {
	return m.load(m.requested)
}

// InEditMode tells the router a form, a modal or the filter holds the keyboard,
// and so does a picker: its esc answers the borrower, it does not leave.
func (m Model) InEditMode() bool {
	return m.mode != modeNormal || m.table.InEditMode() || m.pick != nil
}

// FilterBarVisible implements app.FilterBarView.
func (m Model) FilterBarVisible() bool {
	return m.mode == modeNormal && m.table.FilterBar().IsVisible()
}

// Picking reports whether this instance is lent as a picker — what the router
// asks before dropping it on `:files`.
func (m Model) Picking() bool { return m.pick != nil }

// CurrentPath is the directory listed, for tests and the router.
func (m Model) CurrentPath() string { return m.currentPath }

// load lists dir. Everything the closure needs is copied first (Rule 110).
func (m Model) load(dir string) tea.Cmd {
	showHidden := m.config.App.ShowHiddenFiles
	return func() tea.Msg {
		entries, err := fsbrowse.List(dir, showHidden)
		if err != nil {
			return LoadErrorMsg{Dir: dir, Err: err}
		}
		return EntriesLoadedMsg{Dir: dir, Entries: entries}
	}
}

// navigate asks for dir, selecting selected once it arrives.
func (m Model) navigate(dir, selected string) (tea.Model, tea.Cmd) {
	m.requested = dir
	m.pendingSelect = selected
	return m, m.load(dir)
}

func (m *Model) resize() {
	m.table.Resize(m.width, max(m.height-1, 1))
	if m.form != nil {
		m.form.setWidth(m.width)
	}
}

// setEntries replaces the listing. A picker for a directory gets its "." row.
func (m *Model) setEntries(entries []fsbrowse.Entry) {
	rows := make([]row, 0, len(entries)+1)
	if m.pick != nil && m.pick.Kind == PickDir {
		rows = append(rows, row{Entry: fsbrowse.Entry{Name: ".", Path: m.currentPath, IsDir: true}, self: true})
	}
	for _, e := range entries {
		rows = append(rows, row{Entry: e})
	}
	m.table.SetItems(rows)
	m.table.Remeasure()
}

// selected returns the row under the cursor.
func (m Model) selected() (row, bool) {
	return m.table.Selected()
}

// indexOf finds a path among the visible rows.
func (m Model) indexOf(path string) (int, bool) {
	for i, r := range m.table.Visible() {
		if !r.self && r.Path == path {
			return i, true
		}
	}
	return 0, false
}
