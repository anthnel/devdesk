package templates

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/template"
)

// Every Cmd here does its I/O and returns a message; none touches the model
// (Rule 110). What they need is copied out before the closure is built.

// loadCatalogCmd opens the catalog file.
func loadCatalogCmd(path string) tea.Cmd {
	return func() tea.Msg {
		store, err := template.Open(path)
		return CatalogLoadedMsg{Store: store, Err: err}
	}
}

// saveCmd writes one entry.
func saveCmd(store *template.Store, entry template.Entry) tea.Cmd {
	return func() tea.Msg {
		err := store.Put(entry)
		return SavedMsg{Entry: entry, Err: err}
	}
}

// deleteCmd removes one entry.
func deleteCmd(store *template.Store, slug string) tea.Cmd {
	return func() tea.Msg {
		return DeletedMsg{Slug: slug, Err: store.Delete(slug)}
	}
}
