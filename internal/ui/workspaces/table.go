package workspaces

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// updateTableSize adjusts table dimensions.
//
// Rule 124: the footer (tab bar + filter bar) is outside the viewport, so only
// the table's own header row is subtracted. The Rule 116 arithmetic, the
// selected row's width and the cursor clamp are the component's now.
func (m *Model) updateTableSize() {
	m.table.Resize(m.width, max(m.height-1, 1))
}

// setEntries replaces the listing and rebuilds the decoration over it.
//
// A new listing is a new population, so the columns are measured again: this is
// the directory changing under the user, not a refresh of the same one.
func (m *Model) setEntries(entries []Entry) {
	m.table.SetItems(m.rowsFor(entries))
	m.table.Remeasure()
}

// refreshRows redecorates the entries the table already holds. Callers reach it
// after a scan starts, finishes or arrives from the cache: the listing has not
// changed, only what the six scan columns say about it.
//
// It deliberately does not go through setEntries any more. A spinner frame
// arrives several times a second, and remeasuring on those would let the
// columns shift while a scan runs — which is the one thing the measurement is
// not allowed to cause.
func (m *Model) refreshRows() {
	m.table.SetItems(m.rowsFor(m.entries()))
}

// entries returns the current directory listing, filter or no filter.
func (m Model) entries() []Entry {
	out := make([]Entry, 0, len(m.table.Items()))
	for _, row := range m.table.Items() {
		out = append(out, row.Entry)
	}
	return out
}

// selectedEntry returns the entry under the cursor.
//
// Every action used to index the *unfiltered* list with a cursor into the
// filtered rows, so under a filter they acted on a different directory from the
// one highlighted, and ctrl+d asked to delete it (D24). The table resolves the
// cursor against the slice its rows were built from, so the two cannot part.
func (m Model) selectedEntry() (Entry, bool) {
	row, ok := m.table.Selected()
	return row.Entry, ok
}

// loadEntries loads the contents of the current directory with enriched metadata
func (m Model) loadEntries() tea.Cmd {
	currentPath := m.currentPath
	workspacesDir := m.getExpandedWorkspacesDir()
	// Copied here rather than read inside the closure: a Cmd runs on its own
	// goroutine and must not touch the model (Rule 110).
	showHidden := m.config.App.ShowHiddenFiles

	return func() tea.Msg {
		targetDir := workspacesDir
		if currentPath != "" {
			targetDir = currentPath
		}

		// Check if directory exists, create it if it doesn't (only for workspaces root)
		if _, err := os.Stat(targetDir); os.IsNotExist(err) {
			if currentPath == "" {
				if err := os.MkdirAll(targetDir, 0755); err != nil {
					return LoadErrorMsg{Path: currentPath, Error: err}
				}
				return EntriesLoadedMsg{Path: currentPath}
			}
			return LoadErrorMsg{Path: currentPath, Error: err}
		}

		dirEntries, err := os.ReadDir(targetDir)
		if err != nil {
			return LoadErrorMsg{Path: currentPath, Error: err}
		}

		entries := make([]Entry, 0, len(dirEntries))
		for _, dirEntry := range dirEntries {
			if isHidden(dirEntry.Name(), showHidden) {
				continue
			}

			info, err := dirEntry.Info()
			if err != nil {
				continue
			}

			entry := Entry{
				Name:    dirEntry.Name(),
				Path:    filepath.Join(targetDir, dirEntry.Name()),
				ModTime: info.ModTime(),
				IsDir:   dirEntry.IsDir(),
			}

			// Enrich directory entries with git and project type info
			if entry.IsDir {
				enrichEntry(&entry, showHidden)
			}

			entries = append(entries, entry)
		}

		return EntriesLoadedMsg{Path: currentPath, Entries: entries}
	}
}
