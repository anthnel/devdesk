package templates

import (
	"log"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/oci"
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

// discoverCmd lists the templates the configured registry holds.
//
// It degrades to nothing when no registry is configured, the way the
// explorer's own listing does: an empty answer is the ordinary case, not an
// error worth a footer line.
func (m Model) discoverCmd() tea.Cmd {
	registryURL := m.config.Registry.URL
	basePath := m.config.Registry.TemplatesRepository
	if registryURL == "" || basePath == "" {
		return nil
	}
	creds := m.credentialsFor(template.Source{Kind: template.KindOCI, URL: registryURL})

	return func() tea.Msg {
		listed, err := oci.NewClient(registryURL, creds.Username, creds.Password).ListTemplates(basePath)
		if err != nil {
			log.Printf("ERROR [templates] listing the registry: %v", err)
			return DiscoveredMsg{Err: err}
		}
		return DiscoveredMsg{Entries: template.FromOCI(registryURL, listed)}
	}
}

// credentialsFor resolves what fetching src may use — and only for the host it
// belongs to.
//
// A personal access token authenticates one host. Handing the forge's token to
// a template that lives elsewhere would send it to whoever that URL names, so a
// source on any other host is fetched anonymously (git.SameHost's rule, which
// treats "could not tell" as "no"). The catalog file can be shared, which is
// exactly why this cannot be decided by the entry.
func (m Model) credentialsFor(src template.Source) template.Credentials {
	if m.secrets == nil {
		return template.Credentials{}
	}

	switch src.Kind {
	case template.KindGit:
		if git.SameHost(src.URL, m.config.Forge.URL) {
			token, _ := m.secrets.Load(m.config.Forge.URL)
			return template.Credentials{Token: token}
		}
	case template.KindOCI:
		if sameRegistryHost(src.URL, m.config.Registry.URL) {
			password, _ := m.secrets.Load(m.config.Registry.URL)
			return template.Credentials{Username: m.config.Registry.Username, Password: password}
		}
	}
	return template.Credentials{}
}

// sameRegistryHost compares two registry URLs by host. An empty side never
// matches.
func sameRegistryHost(a, b string) bool {
	ha, hb := git.HostOf(a), git.HostOf(b)
	return ha != "" && strings.EqualFold(ha, hb)
}
