package templates

import (
	"log"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// Init opens the catalog and, when a registry is configured, lists what it
// holds. The two are independent: a registry that is down leaves the declared
// templates usable.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{loadCatalogCmd(m.path)}
	if discover := m.discoverCmd(); discover != nil {
		cmds = append(cmds, discover, m.spinner.Tick)
	}
	return tea.Batch(cmds...)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case CatalogLoadedMsg:
		return m.handleCatalogLoaded(msg)

	case DiscoveredMsg:
		return m.handleDiscovered(msg)

	case SavedMsg:
		return m.handleSaved(msg)

	case DeletedMsg:
		return m.handleDeleted(msg)

	case FormSubmitMsg:
		return m.handleFormSubmit(msg)

	case FormCancelMsg:
		m.form = nil
		return m, nil

	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleConfirmDelete()

	case sharedcomponents.ConfirmModalNoMsg:
		m.confirmModal = nil
		m.pendingDelete = ""
		return m, nil

	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	m.footer.Handle(msg)
	return m, nil
}

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.resize(msg.Width, msg.Height)
	if m.confirmModal != nil {
		m.confirmModal, _ = m.confirmModal.Update(msg)
	}
	return m, nil
}

// resize lays the table and the form out. The table takes the full viewport
// width, borders included (Rule 116).
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height
	m.table.Resize(width, max(height-1, 1))
	if m.form != nil {
		m.form.SetWidth(width)
	}
}

func (m Model) handleCatalogLoaded(msg CatalogLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [templates] opening the catalog: %v", msg.Err)
		m.storeErr = msg.Err
		return m, m.footer.Error("Could not read the template catalog — check logs")
	}
	m.store = msg.Store
	m.storeErr = nil
	m.declared = msg.Store.List()
	m.rebuild()
	return m, nil
}

func (m Model) handleDiscovered(msg DiscoveredMsg) (tea.Model, tea.Cmd) {
	m.discovering = false
	if msg.Err != nil {
		// Logged by the Cmd; the declared templates are still here, so this is
		// a warning about what is missing rather than an error screen.
		return m, m.footer.Warn("Could not list the registry's templates — check logs")
	}
	m.discovered = msg.Entries
	m.rebuild()
	return m, nil
}

// handleSpinnerTick turns the frame while the registry is being listed, and
// stops the chain when it is not: a tick left running for a view with nothing
// to wait for is a timer that never stops.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.discovering {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	m.footer.SetSpinnerFrame(m.spinner.View())
	return m, cmd
}

func (m Model) handleSaved(msg SavedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [templates] saving %s: %v", msg.Entry.Slug, msg.Err)
		return m, m.footer.Error("Failed to save the template — check logs")
	}
	m.form = nil
	m.declared = m.store.List()
	m.rebuild()
	m.selectSlug(msg.Entry.Slug)
	return m, m.footer.Info("Saved " + msg.Entry.Name)
}

func (m Model) handleDeleted(msg DeletedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [templates] deleting %s: %v", msg.Slug, msg.Err)
		return m, m.footer.Error("Failed to delete the template — check logs")
	}
	m.declared = m.store.List()
	m.rebuild()
	return m, m.footer.Info("Deleted " + msg.Slug)
}

// selectSlug puts the cursor on an entry, so a template just saved is the one
// selected rather than wherever the sort has moved the cursor to.
func (m *Model) selectSlug(slug string) {
	for i, r := range m.table.Visible() {
		if r.Entry.Slug == slug {
			m.table.SetCursor(i)
			return
		}
	}
}

func (m Model) handleFormSubmit(msg FormSubmitMsg) (tea.Model, tea.Cmd) {
	if m.store == nil {
		return m, m.footer.Warn(reasonStillOpening)
	}
	return m, saveCmd(m.store, msg.Entry)
}

func (m Model) handleConfirmDelete() (tea.Model, tea.Cmd) {
	slug := m.pendingDelete
	m.confirmModal = nil
	m.pendingDelete = ""
	if m.store == nil || slug == "" {
		return m, nil
	}
	return m, deleteCmd(m.store, slug)
}

// handleKeyMsg routes a key. A mode takes it first (the modal, then the form),
// then the filter while it has the keyboard (Rule 136), then the actions.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmModal != nil {
		var cmd tea.Cmd
		m.confirmModal, cmd = m.confirmModal.Update(msg)
		return m, cmd
	}
	if m.form != nil {
		return m, m.form.Update(msg)
	}
	if m.table.InEditMode() {
		return m, m.table.Update(msg)
	}

	switch msg.String() {
	case keymap.New:
		return m.startCreate()
	case keymap.Edit:
		return m.startEdit()
	case keymap.Delete:
		return m.startDelete()
	case keymap.Pager:
		return m.startPreview()
	case ".":
		m.table.CycleSort()
		return m, nil
	case "/":
		return m, m.table.FilterBar().ActivateSearch()
	}
	return m, m.table.Update(msg)
}

// isTaken reports whether a slug names a declared entry — a discovered one has
// a slug of its own that a declared one cannot collide with (Merge keys on the
// source, not the slug), so only the catalog counts.
func (m Model) isTaken(slug string) bool {
	_, err := m.store.Get(slug)
	return err == nil
}

func (m Model) startCreate() (tea.Model, tea.Cmd) {
	if refusal := m.refusal(m.availability().New); refusal != "" {
		return m, m.footer.Warn(refusal)
	}
	m.form = newEntryForm(nil, m.isTaken)
	m.form.SetWidth(m.width)
	return m, nil
}

// startEdit opens the form on the selected entry. On a discovered one it is what
// adopts it: saving writes a declared entry with the same source, and Merge
// stops listing the registry's.
func (m Model) startEdit() (tea.Model, tea.Cmd) {
	if refusal := m.refusal(m.availability().Edit); refusal != "" {
		return m, m.footer.Warn(refusal)
	}
	entry, _ := m.selectedEntry()
	m.form = newEntryForm(&entry, m.isTaken)
	m.form.SetWidth(m.width)
	return m, nil
}

func (m Model) startDelete() (tea.Model, tea.Cmd) {
	if refusal := m.refusal(m.availability().Delete); refusal != "" {
		return m, m.footer.Warn(refusal)
	}
	entry, _ := m.selectedEntry()
	m.pendingDelete = entry.Slug
	m.confirmModal = sharedcomponents.NewConfirmModal(
		"Delete template",
		"Remove \""+entry.Name+"\" from the catalog?\nThe template's own content is not touched.",
	)
	m.confirmModal.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	return m, nil
}

// refusal is why a key cannot act, or "" when it can. The catalog not being
// open yet is refused here rather than greyed: it is not knowing, and greying a
// key for the length of one read would read as a glitch (Rule 130).
func (m Model) refusal(a shortcut.Availability) string {
	switch {
	case !a.Enabled():
		return a.Reason
	case m.store == nil && m.storeErr == nil:
		return reasonStillOpening
	}
	return ""
}

// startPreview opens the list of files the selected template would put in a new
// repository, in the viewer.
//
// It asks the router rather than opening anything itself, like every other
// producer: the fetch happens in the viewer's own Init, so one place reports
// what went wrong. The credentials are resolved here, in Update, and carried in
// the source — the source runs on another goroutine and must not reach back
// into the model.
func (m Model) startPreview() (tea.Model, tea.Cmd) {
	if reason := m.availability().Preview.Reason; reason != "" {
		return m, m.footer.Warn(reason)
	}
	entry, _ := m.selectedEntry()
	source := previewSource{entry: entry, creds: m.credentialsFor(entry.Source)}
	return m, func() tea.Msg { return uiviewer.OpenRequestMsg{Source: source} }
}
