package ociresources

import (
	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/keymap"
)

// InEditMode returns true when a text input is active (blocks command mode).
func (b *RegistryBrowser) InEditMode() bool {
	if b.state == browserStateInput && b.focusedField == brFieldRepo {
		return true
	}
	if b.state == browserStateImages {
		return b.searchTable.InEditMode()
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
	case tea.KeyPressMsg:
		return b.handleKeyMsg(msg)
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
	if b.state == browserStateImages {
		return b, b.searchTable.Update(msg)
	}
	return b, nil
}

func (b *RegistryBrowser) handleKeyMsg(msg tea.KeyPressMsg) (*RegistryBrowser, tea.Cmd) {
	switch b.state {
	case browserStateInput:
		return b.handleInputKeyMsg(msg)
	case browserStateTags:
		return b.handleTagsKeyMsg(msg)
	case browserStateImages:
		return b.handleImagesKeyMsg(msg)
	}
	return b, nil
}

func (b *RegistryBrowser) handleInputKeyMsg(msg tea.KeyPressMsg) (*RegistryBrowser, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return b, func() tea.Msg { return RegistryBrowserCloseMsg{} }
	case "down":
		b.focusedField = min(b.focusedField+1, b.brFieldLast())
		b.updateFocus()
		return b, nil
	case "up":
		b.focusedField = max(b.focusedField-1, brFieldRepo)
		b.updateFocus()
		return b, nil
	case keymap.Space:
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
		if b.focusedField == brFieldRepo {
			return b.submit(actionSearch)
		}
		if btn, ok := b.focusedButton(); ok {
			return b.submit(btn.action)
		}
		b.focusedField = min(b.focusedField+1, b.brFieldLast())
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

// entryAlias is the label a result of entry carries: the alias, behind its
// group's when it is a member.
func entryAlias(entry browserRegistryEntry) string {
	if entry.ParentAlias != "" {
		return entry.ParentAlias + "/" + entry.Alias
	}
	return entry.Alias
}

// tagRequest is one tag listing to send: the entry it goes to, and what the
// request carries. repo is final: nothing downstream normalizes it again.
type tagRequest struct {
	entry      browserRegistryEntry
	alias      string
	apiURL     string
	repo       string
	user, pass string
}

// newTagRequest describes entry for a tag listing of repo, with the same alias,
// API address and credentials (credsFor, D12) as every tag request.
func (b *RegistryBrowser) newTagRequest(entry browserRegistryEntry, repo string) tagRequest {
	username, storedPass := b.credsFor(entry)
	return tagRequest{entry, entryAlias(entry), registryAPIURL(entry.URL), repo, username, storedPass}
}

// startTagLookup lists the tags of repo on every entry.
func (b *RegistryBrowser) startTagLookup(repo string, entries []browserRegistryEntry) (*RegistryBrowser, tea.Cmd) {
	// Gather the requests first, then begin the lot: nothing is cancelled
	// unless a request really starts.
	reqs := make([]tagRequest, 0, len(entries))
	for _, entry := range entries {
		// The prefix is applied here and nowhere else: it is then already part
		// of MultiRegistryTag.Repo, so multiImageName and registryAPIURL need to
		// know nothing about it. Teaching both would be two places free to
		// drift, which is the disagreement D39 already is (§3.18).
		normalizedRepo := joinRepoPrefix(entry.repoPrefix, normalizeRepoForRegistry(entry.URL, repo))
		reqs = append(reqs, b.newTagRequest(entry, normalizedRepo))
	}
	return b.startTagRequests(reqs, false)
}

// startTagRequests opens a lot, resets the tag table (tags, registry filter,
// text filter, default sort), records whether the lot comes from level 1, and
// sends one searchRegistryTagsCmd per request.
func (b *RegistryBrowser) startTagRequests(reqs []tagRequest, fromSearch bool) (*RegistryBrowser, tea.Cmd) {
	ctx := b.beginRequests()
	b.tagsFromSearch = fromSearch
	b.tagsImage = ""
	b.tags = nil
	b.registryFilter = resultFilter{}
	b.filterInput.SetValue("")
	b.filterActive = false
	// A new search starts from the default order, not wherever the last one's
	// `.` presses left the table.
	b.tagTable.SetSort(tagColumnTag, false)

	cmds := make([]tea.Cmd, 0, len(reqs))
	for _, r := range reqs {
		b.pendingSearches++
		cmds = append(cmds, searchRegistryTagsCmd(ctx, b.gen, r.entry.key, r.entry.URL, r.alias, r.apiURL, r.repo, r.user, r.pass))
	}

	b.state = browserStateTags
	b.searchTable.Blur()
	b.tagTable.Focus()
	b.rebuildTagTable()
	return b, tea.Batch(cmds...)
}

func (b *RegistryBrowser) handleTagsKeyMsg(msg tea.KeyPressMsg) (*RegistryBrowser, tea.Cmd) {
	if b.filterActive {
		switch msg.String() {
		case "esc", "enter":
			b.filterActive = false
			b.filterInput.Blur()
			b.rebuildTagTable()
			b.resizeTables()
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
		b.resizeTables()
		return b, nil
	case "r":
		b.cycleRegistryFilter()
		return b, nil
	case "enter":
		return b.openTagScanDetails()
	case keymap.Get:
		return b.pullSelectedTag()
	case keymap.Scan:
		return b.requestDirectScan()
	}
	// Navigation and `.` are the table's. `/` never reaches it: the browser's
	// own filter took the key two cases above, and the table declares no
	// searchable column, so it would decline it anyway.
	return b, b.tagTable.Update(msg)
}
