package ociresources

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// filterStops returns the values `r` cycles through: every group that produced
// results, then every individual registry, then off. The group level is what
// makes "everything from this Nexus" one keystroke rather than eight.
func (b *RegistryBrowser) filterStops() []resultFilter {
	seenGroup, seenEntry := map[string]bool{}, map[string]bool{}
	var groups, singles []resultFilter
	for _, t := range b.tags {
		entry := b.entryFor(t.EntryKey)
		if entry != nil && entry.ParentSlug != "" && !seenGroup[entry.ParentSlug] {
			seenGroup[entry.ParentSlug] = true
			groups = append(groups, resultFilter{groupSlug: entry.ParentSlug})
		}
		if !seenEntry[t.EntryKey] {
			seenEntry[t.EntryKey] = true
			singles = append(singles, resultFilter{entryKey: t.EntryKey})
		}
	}
	// A single source has nothing to filter down to.
	if len(singles) <= 1 {
		return nil
	}
	// A group with every result under it says the same thing as "off".
	if len(groups) == 1 && len(groups) == len(seenGroup) && len(singles) == countMembers(b.entries, groups[0].groupSlug) {
		groups = nil
	}
	return append(groups, singles...)
}

// countMembers returns how many entries belong to a group.
func countMembers(entries []browserRegistryEntry, slug string) int {
	n := 0
	for _, e := range entries {
		if e.ParentSlug == slug {
			n++
		}
	}
	return n
}

func (b *RegistryBrowser) cycleRegistryFilter() {
	stops := b.filterStops()
	if len(stops) == 0 {
		b.registryFilter = resultFilter{}
		b.rebuildTagTable()
		return
	}
	if b.registryFilter.isEmpty() {
		b.registryFilter = stops[0]
		b.rebuildTagTable()
		return
	}
	for i, s := range stops {
		if s == b.registryFilter {
			if i+1 < len(stops) {
				b.registryFilter = stops[i+1]
			} else {
				b.registryFilter = resultFilter{} // back to everything
			}
			b.rebuildTagTable()
			return
		}
	}
	b.registryFilter = resultFilter{}
	b.rebuildTagTable()
}

// entryFor returns the browser entry a result came from, or nil.
//
// It resolves by entry key, not by URL: every member of a path-routed group
// answers to the same host, so a URL identifies a whole instance and a result
// would be unattributable (§3.18, and D40 one screen along).
func (b *RegistryBrowser) entryFor(key string) *browserRegistryEntry {
	for i := range b.entries {
		if b.entries[i].key == key {
			return &b.entries[i]
		}
	}
	return nil
}

// registryFilterLabel returns a short display label for the active filter.
//
// D14: it used to search only the configured top-level entries, so filtering to
// a discovered member fell through to the synthesised URL — the one row in the
// view showing a URL where every other showed an alias. Members are entries now,
// so they resolve like anything else.
func (b *RegistryBrowser) registryFilterLabel() string {
	if b.registryFilter.groupSlug != "" {
		for _, reg := range b.registries {
			if reg.Slug == b.registryFilter.groupSlug {
				return browserAlias(reg)
			}
		}
		return b.registryFilter.groupSlug
	}
	if entry := b.entryFor(b.registryFilter.entryKey); entry != nil {
		if entry.ParentAlias != "" {
			return entry.ParentAlias + "/" + entry.Alias
		}
		if entry.Alias != "" {
			return entry.Alias
		}
		return registryRef(entry.URL, entry.repoPrefix)
	}
	return b.registryFilter.entryKey
}

// pullSelectedTag asks the parent to admit the pull as a job, mirroring
// requestDirectScan below.
//
// It holds no state of its own for it any more (§3.60). The browser used to
// take over the screen with a spinner of its own until the pull answered,
// which said less than the Images tab now does — the row is there, spinning,
// next to every other image — and said it on the one screen from which the
// result could not be seen.
func (b *RegistryBrowser) pullSelectedTag() (*RegistryBrowser, tea.Cmd) {
	name := b.selectedImageName()
	if name == "" {
		return b, nil
	}
	return b, func() tea.Msg { return RegistryPullRequestedMsg{ImageName: name} }
}

// requestDirectScan emits RegistryTagDirectScanMsg for a remote Trivy scan (no pull).
func (b *RegistryBrowser) requestDirectScan() (*RegistryBrowser, tea.Cmd) {
	name := b.selectedImageName()
	if name == "" {
		return b, nil
	}
	return b, func() tea.Msg { return RegistryTagDirectScanMsg{ImageName: name} }
}

// openTagScanDetails emits ScanDetailsRequestMsg when the selected tag has cached results.
func (b *RegistryBrowser) openTagScanDetails() (*RegistryBrowser, tea.Cmd) {
	name := b.selectedImageName()
	if name == "" {
		return b, nil
	}
	if _, ok := b.scanCache[name]; !ok {
		return b, nil
	}
	return b, func() tea.Msg { return ScanDetailsRequestMsg{ImageName: name} }
}

// joinRepoPrefix puts an entry's prefix in front of the repository name.
//
// One slash, always: the prefix is a path segment and normalization has already
// refused one carrying a slash at either end, so there is nothing here to guess
// about (§3.18).
func joinRepoPrefix(prefix, repo string) string {
	if prefix == "" {
		return repo
	}
	return prefix + "/" + repo
}

// normalizeRepoForRegistry prepends "library/" for bare names on Docker Hub.
func normalizeRepoForRegistry(registryURL, repo string) string {
	if strings.Contains(repo, "/") {
		return repo
	}
	if isDockerHub(registryURL) {
		return "library/" + repo
	}
	return repo
}

// registryHost strips the scheme from a configured registry URL, leaving what
// Docker itself is keyed on: `host[:port]`, plus whatever path a repository
// manager serves the registry under.
//
// A registry URL is written either way in practice — the form does not
// normalize it, `registryAPIURL` explicitly accepts both, and this package's own
// examples write `https://nexus.example.com/repository/docker-group`. Every
// Docker-facing use has to strip it, and none of the three that needed to did
// (D39): a `docker pull` reference carrying `https://` is not a reference at
// all, and a hub alias compared against a scheme-bearing string never matched,
// so images from a Hub configured as `https://docker.io` lost their `library/`
// prefix.
func registryHost(registryURL string) string {
	base := strings.TrimSuffix(strings.TrimSpace(registryURL), "/")
	for _, scheme := range []string{"https://", "http://"} {
		if len(base) >= len(scheme) && strings.EqualFold(base[:len(scheme)], scheme) {
			return base[len(scheme):]
		}
	}
	return base
}

// isDockerHub reports whether a registry URL names Docker Hub, under either of
// the two spellings and with or without a scheme. Hub is the one registry whose
// references carry no host, so every caller has to recognise it.
func isDockerHub(registryURL string) bool {
	host := registryHost(registryURL)
	return strings.EqualFold(host, "docker.io") || strings.EqualFold(host, "registry-1.docker.io")
}

// multiImageName constructs the full image reference for pull/scan.
func multiImageName(registryURL, repo, tag string) string {
	if isDockerHub(registryURL) {
		return repo + ":" + tag
	}
	return registryHost(registryURL) + "/" + repo + ":" + tag
}

func (b *RegistryBrowser) updateFocus() {
	b.repoInput.Blur()
	if b.focusedField == brFieldRepo {
		b.repoInput.Focus()
	}
}

// filteredMultiTags returns the tags the registry filter and the text filter let
// through. The order is the table's — it is what holds the cursor and the
// header arrow together.
func (b *RegistryBrowser) filteredMultiTags() []MultiRegistryTag {
	query := strings.ToLower(b.filterInput.Value())
	var result []MultiRegistryTag
	for _, t := range b.tags {
		if !b.registryFilter.matches(t.EntryKey, b.entryFor(t.EntryKey)) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(t.Tag), query) &&
			!strings.Contains(strings.ToLower(t.Repo), query) {
			continue
		}
		result = append(result, t)
	}
	return result
}
