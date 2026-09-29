package filebrowser

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/fsbrowse"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/theme"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/anthnel/devdesk/internal/viewer"
)

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case EntriesLoadedMsg:
		return m.handleEntriesLoaded(msg)

	case LoadErrorMsg:
		return m.handleLoadError(msg)

	case createFormSubmitMsg:
		return m.handleCreateSubmit(msg)

	case createFormCancelMsg:
		m.mode, m.form = modeNormal, nil
		return m, nil

	case EntryCreatedMsg:
		return m.handleEntryCreated(msg)

	case DeletePreparedMsg:
		return m.handleDeletePrepared(msg)

	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleConfirmDelete()

	case sharedcomponents.ConfirmModalNoMsg:
		m.mode, m.confirm, m.pending = modeNormal, nil, nil
		return m, nil

	case EntryDeletedMsg:
		return m.handleEntryDeleted(msg)

	case PathCopiedMsg:
		return m.handlePathCopied(msg)

	case jobs.ChangedMsg:
		return m.handleJobsChanged(msg)
	}

	m.footer.Handle(msg)
	if m.mode == modeNormal {
		return m, m.table.Update(msg)
	}
	return m, nil
}

func (m Model) handleEntriesLoaded(msg EntriesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Dir != m.requested {
		return m, nil // a listing of somewhere already left
	}
	m.currentPath = msg.Dir
	m.setEntries(msg.Entries)
	m.table.GotoTop()
	if m.pendingSelect != "" {
		if i, ok := m.indexOf(m.pendingSelect); ok {
			m.table.SetCursor(i)
		}
		m.pendingSelect = ""
	}
	return m, nil
}

// handleLoadError keeps the listing that was on screen: the directory asked for
// could not be read, and the one shown still can. A first load has nothing to
// keep, which leaves an empty table under an error — still a table (Rule 139).
func (m Model) handleLoadError(msg LoadErrorMsg) (tea.Model, tea.Cmd) {
	if msg.Dir != m.requested {
		return m, nil
	}
	log.Printf("ERROR [filebrowser] list %s: %v", msg.Dir, msg.Err)
	m.requested = m.currentPath
	m.pendingSelect = ""
	if errors.Is(msg.Err, os.ErrPermission) {
		return m, m.footer.Error("Permission denied: " + theme.ShortPath(msg.Dir, 60))
	}
	return m, m.footer.Error("Cannot read " + theme.ShortPath(msg.Dir, 60) + " — check logs")
}

// ── Keys ────────────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.table.InEditMode() {
		return m, m.table.Update(msg)
	}
	switch m.mode {
	case modeCreating:
		var cmd tea.Cmd
		m.form, cmd = m.form.Update(msg)
		return m, cmd
	case modeConfirmingDelete:
		var cmd tea.Cmd
		m.confirm, cmd = m.confirm.Update(msg)
		return m, cmd
	}

	a := m.actions()
	switch msg.String() {
	case "/":
		return m, m.table.Update(msg)
	case "enter":
		return m.guard(a.Enter, m.enter)
	case "right":
		return m.guard(a.Right, m.enterDir)
	case "left":
		return m.goUp()
	case "esc":
		if m.pick != nil {
			tag := m.pick.Tag
			return m, func() tea.Msg { return PickCancelledMsg{Tag: tag} }
		}
		return m.goUp()
	case "ctrl+r":
		return m.navigate(m.currentPath, m.selectedPath())
	case keymap.New:
		return m.startCreate()
	case keymap.Delete:
		return m.guard(a.Delete, m.startDelete)
	case keymap.Copy:
		return m.guard(a.Copy, m.copyPath)
	}
	return m, m.table.Update(msg)
}

func (m Model) selectedPath() string {
	if r, ok := m.selected(); ok && !r.self {
		return r.Path
	}
	return ""
}

// enter opens a file in the viewer, or — in a picker — answers the borrower.
func (m Model) enter() (tea.Model, tea.Cmd) {
	r, _ := m.selected()
	if m.pick != nil {
		tag, path := m.pick.Tag, r.Path
		return m, func() tea.Msg { return PathPickedMsg{Tag: tag, Path: path} }
	}
	source := viewer.NewFileSource(r.Path)
	return m, func() tea.Msg { return uiviewer.OpenRequestMsg{Source: source} }
}

func (m Model) enterDir() (tea.Model, tea.Cmd) {
	r, _ := m.selected()
	return m.navigate(r.Path, "")
}

// goUp lists the parent, with the cursor on the directory just left. At a root
// there is nowhere to go, and nothing to say about it.
func (m Model) goUp() (tea.Model, tea.Cmd) {
	if m.currentPath == "" || fsbrowse.IsRoot(m.currentPath) {
		return m, nil
	}
	return m.navigate(filepath.Dir(m.currentPath), m.currentPath)
}

// ── N ───────────────────────────────────────────────────────────────────────

func (m Model) startCreate() (tea.Model, tea.Cmd) {
	isDir := m.pick == nil || m.pick.Kind == PickDir
	m.form = newCreateForm(isDir, m.width)
	m.mode = modeCreating
	return m, textinput.Blink
}

// handleCreateSubmit validates before leaving the form: a refused name keeps
// what was typed, and the footer says why (a validation message is written to
// be read — Rule 128).
func (m Model) handleCreateSubmit(msg createFormSubmitMsg) (tea.Model, tea.Cmd) {
	if err := fsbrowse.ValidName(msg.Name); err != nil {
		return m, m.footer.Warn(capitalize(err.Error()))
	}
	m.mode, m.form = modeNormal, nil
	dir, name, isDir := m.currentPath, msg.Name, msg.IsDir
	return m, func() tea.Msg {
		path, err := fsbrowse.Create(dir, name, isDir)
		return EntryCreatedMsg{Path: path, Err: err}
	}
}

func (m Model) handleEntryCreated(msg EntryCreatedMsg) (tea.Model, tea.Cmd) {
	name := filepath.Base(msg.Path)
	switch {
	case errors.Is(msg.Err, os.ErrExist):
		return m, m.footer.Warn(name + " already exists")
	case msg.Err != nil:
		log.Printf("ERROR [filebrowser] create %s: %v", msg.Path, msg.Err)
		return m, m.footer.Error("Could not create " + name + " — check logs")
	}
	next, load := m.navigate(m.currentPath, msg.Path)
	nm := next.(Model)
	info := nm.footer.Info("Created " + name)
	return nm, tea.Batch(load, info)
}

// ── D ───────────────────────────────────────────────────────────────────────

// startDelete reads how much a directory holds before asking: "Delete
// Directory" over a tree of four hundred entries should say so.
func (m Model) startDelete() (tea.Model, tea.Cmd) {
	r, _ := m.selected()
	entry := r.Entry
	return m, func() tea.Msg {
		count := 0
		if entry.IsDir {
			count, _ = fsbrowse.Count(entry.Path) // unreadable: the modal just says less
		}
		return DeletePreparedMsg{Entry: entry, Count: count}
	}
}

func (m Model) handleDeletePrepared(msg DeletePreparedMsg) (tea.Model, tea.Cmd) {
	entry := msg.Entry
	body := "Delete " + theme.ShortPath(entry.Path, 50) + "?"
	if entry.IsDir && msg.Count > 0 {
		body = fmt.Sprintf("Delete %s and the %d entries it holds, recursively?", theme.ShortPath(entry.Path, 50), msg.Count)
	}
	m.pending = &entry
	m.confirm = sharedcomponents.NewConfirmModal("Delete "+fsbrowse.Describe(entry.IsDir), body)
	m.mode = modeConfirmingDelete
	return m, nil
}

func (m Model) handleConfirmDelete() (tea.Model, tea.Cmd) {
	m.mode, m.confirm = modeNormal, nil
	if m.pending == nil {
		return m, nil
	}
	path := m.pending.Path
	m.pending = nil
	// Checked again here: this is where the irreversible call is issued.
	if m.deleting(path) {
		return m, m.footer.Warn(reasonDeleting)
	}
	return m, jobs.Start(deleteRun(path), func() tea.Msg {
		return EntryDeletedMsg{Path: path, Err: fsbrowse.Remove(path)}
	})
}

func (m Model) handleEntryDeleted(msg EntryDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [filebrowser] delete %s: %v", msg.Path, msg.Err)
		return m, m.footer.Error("Could not delete " + filepath.Base(msg.Path) + " — check logs")
	}
	// The listing is reread only when the deletion happened where the user is
	// looking; one finishing after they moved on changes nothing on screen.
	if filepath.Dir(msg.Path) != m.currentPath {
		return m, nil
	}
	next, load := m.navigate(m.currentPath, "")
	nm := next.(Model)
	info := nm.footer.Info("Deleted " + filepath.Base(msg.Path))
	return nm, tea.Batch(load, info)
}

// ── Y ───────────────────────────────────────────────────────────────────────

func (m Model) copyPath() (tea.Model, tea.Cmd) {
	r, _ := m.selected()
	path := r.Path
	return m, func() tea.Msg { return PathCopiedMsg{Path: path, Err: clipboard.WriteAll(path)} }
}

func (m Model) handlePathCopied(msg PathCopiedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [filebrowser] copy path %s to clipboard: %v", msg.Path, msg.Err)
		return m, m.footer.Error("Could not reach the clipboard — check logs")
	}
	return m, m.footer.Info("Full path copied to the clipboard")
}

// capitalize starts a validation message with a capital, as a footer line does.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
