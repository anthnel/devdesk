package templates

import (
	"log"

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

// deleteCmd removes one entry, and the copy of its files that was cached.
func deleteCmd(store *template.Store, cache template.Cache, slug string) tea.Cmd {
	return func() tea.Msg {
		err := store.Delete(slug)
		if err == nil {
			// Best effort: the entry is gone, and a copy left behind is only
			// disk that a later template of the same name would overwrite.
			if ferr := cache.Forget(slug); ferr != nil {
				log.Printf("ERROR [templates] forget cache %s: %v", slug, ferr)
			}
		}
		return DeletedMsg{Slug: slug, Err: err}
	}
}
