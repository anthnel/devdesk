package ociresources

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
)

// InEditMode returns true when a text input is active (blocks command mode).
func (b *RegistryBrowser) InEditMode() bool {
	if b.state == browserStateInput && b.focusedField == brFieldRepo {
		return true
	}
	return b.state == browserStateTags && b.filterActive
}

// IsSearching returns true while registry tag queries are still in flight.
func (b *RegistryBrowser) IsSearching() bool {
	return b.pendingSearches > 0
}

// Update handles all messages for the browser.
func (b *RegistryBrowser) Update(msg tea.Msg) (*RegistryBrowser, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return b.handleKeyMsg(msg)
	case spinner.TickMsg:
		if b.state == browserStateStatus || len(b.scanningTags) > 0 || b.pendingSearches > 0 {
			var cmd tea.Cmd
			b.spinner, cmd = b.spinner.Update(msg)
			if b.state == browserStateTags && len(b.scanningTags) > 0 {
				b.tagScanSpinnerIdx++
				b.rebuildTagTable()
			}
			return b, cmd
		}
		return b, nil
	}
	return b.delegateUpdate(msg)
}

func (b *RegistryBrowser) delegateUpdate(msg tea.Msg) (*RegistryBrowser, tea.Cmd) {
	if b.state == browserStateInput && b.focusedField == brFieldRepo {
		var cmd tea.Cmd
		b.repoInput, cmd = b.repoInput.Update(msg)
		return b, cmd
	}
	if b.state == browserStateTags {
		if b.filterActive {
			var cmd tea.Cmd
			b.filterInput, cmd = b.filterInput.Update(msg)
			b.rebuildTagTable()
			return b, cmd
		}
		return b, b.tagTable.Update(msg)
	}
	return b, nil
}

func (b *RegistryBrowser) handleKeyMsg(msg tea.KeyMsg) (*RegistryBrowser, tea.Cmd) {
	switch b.state {
	case browserStateInput:
		return b.handleInputKeyMsg(msg)
	case browserStateTags:
		return b.handleTagsKeyMsg(msg)
	case browserStateStatus:
		return b, nil
	}
	return b, nil
}

func (b *RegistryBrowser) handleInputKeyMsg(msg tea.KeyMsg) (*RegistryBrowser, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return b, func() tea.Msg { return RegistryBrowserCloseMsg{} }
	case "down":
		b.focusedField = min(b.focusedField+1, b.brFieldSubmit())
		b.updateFocus()
		return b, nil
	case "up":
		b.focusedField = max(b.focusedField-1, brFieldRepo)
		b.updateFocus()
		return b, nil
	case " ":
		if b.focusedField >= 1 && b.focusedField <= len(b.rows) {
			row := b.rows[b.focusedField-1]
			if row.groupSlug != "" {
				b.toggleGroup(row.groupSlug)
				return b, nil
			}
			key := b.entries[row.entry].key
			b.selectedRegs[key] = !b.selectedRegs[key]
			return b, nil
		}
	case "enter":
		if b.focusedField == brFieldRepo || b.focusedField == b.brFieldSubmit() {
			return b.submitSearch()
		}
		b.focusedField = min(b.focusedField+1, b.brFieldSubmit())
		b.updateFocus()
		return b, nil
	}
	return b.delegateUpdate(msg)
}

// credsFor returns the credentials to search entry with, and nothing at all
// when the registry — or the group it belongs to — is marked anonymous (D12).
// Docker keys credentials by host, so a Nexus instance with one repository
// logged into would otherwise send that login to every repository it serves.
func (b *RegistryBrowser) credsFor(entry browserRegistryEntry) (string, string) {
	if !config.UsesCredentials(entry.authMode) {
		return "", ""
	}
	// Credentials come from the parent registry, or the entry itself for a
	// registry that is not a group member.
	credURL := entry.URL
	if entry.parentURL != "" {
		credURL = entry.parentURL
	}
	var username string
	for _, reg := range b.registries {
		if reg.URL == credURL {
			username = reg.Username
			break
		}
	}
	storedUser, storedPass, _ := docker.GetStoredCreds(credURL)
	if username == "" {
		username = storedUser
	}
	return username, storedPass
}

func (b *RegistryBrowser) submitSearch() (*RegistryBrowser, tea.Cmd) {
	repo := strings.TrimSpace(b.repoInput.Value())
	if repo == "" {
		return b, nil
	}

	b.tags = nil
	b.registryFilter = resultFilter{}
	b.pendingSearches = 0
	b.filterInput.SetValue("")
	b.filterActive = false
	// A new search starts from the default order, not wherever the last one's
	// `.` presses left the table.
	b.tagTable.SetSort(tagColumnTag, false)

	var cmds []tea.Cmd
	for _, entry := range b.entries {
		if !b.selected(entry) {
			continue
		}
		username, storedPass := b.credsFor(entry)
		alias := entry.Alias
		if entry.ParentAlias != "" {
			alias = entry.ParentAlias + "/" + entry.Alias
		}
		normalizedRepo := normalizeRepoForRegistry(entry.URL, repo)
		apiURL := registryAPIURL(entry.URL)
		b.pendingSearches++
		cmds = append(cmds, searchRegistryTagsCmd(entry.URL, alias, apiURL, normalizedRepo, username, storedPass))
	}

	if len(cmds) == 0 {
		return b, nil
	}

	b.state = browserStateTags
	b.tagTable.Focus()
	b.rebuildTagTable()
	cmds = append(cmds, b.spinner.Tick)
	return b, tea.Batch(cmds...)
}

func (b *RegistryBrowser) handleTagsKeyMsg(msg tea.KeyMsg) (*RegistryBrowser, tea.Cmd) {
	if b.filterActive {
		switch msg.String() {
		case "esc", "enter":
			b.filterActive = false
			b.filterInput.Blur()
			b.rebuildTagTable()
			b.resizeTagTable()
			return b, nil
		}
		return b.delegateUpdate(msg)
	}

	switch msg.String() {
	case "esc":
		b.state = browserStateInput
		b.tagTable.Blur()
		b.updateFocus()
		return b, nil
	case "/":
		b.filterActive = true
		b.filterInput.Focus()
		b.resizeTagTable()
		return b, nil
	case "r":
		b.cycleRegistryFilter()
		return b, nil
	case "enter":
		return b.openTagScanDetails()
	case "p":
		return b.pullSelectedTag()
	case "ctrl+s":
		return b.requestDirectScan()
	}
	// Navigation and `.` are the table's. `/` never reaches it: the browser's
	// own filter took the key two cases above, and the table declares no
	// searchable column, so it would decline it anyway.
	return b, b.tagTable.Update(msg)
}
