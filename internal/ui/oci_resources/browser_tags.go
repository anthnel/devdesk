package ociresources

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (b *RegistryBrowser) cycleSortTags() {
	if b.tagSortCol == tagSortByName && !b.tagSortDesc {
		b.tagSortDesc = true
	} else if b.tagSortCol == tagSortByName && b.tagSortDesc {
		b.tagSortCol = tagSortByUpdated
		b.tagSortDesc = false
	} else if b.tagSortCol == tagSortByUpdated && !b.tagSortDesc {
		b.tagSortDesc = true
	} else {
		b.tagSortCol = tagSortByName
		b.tagSortDesc = false
	}
	b.rebuildTagTable()
}

// filterStops returns the values `r` cycles through: every group that produced
// results, then every individual registry, then off. The group level is what
// makes "everything from this Nexus" one keystroke rather than eight.
func (b *RegistryBrowser) filterStops() []resultFilter {
	seenGroup, seenURL := map[string]bool{}, map[string]bool{}
	var groups, singles []resultFilter
	for _, t := range b.tags {
		entry := b.entryFor(t.RegistryURL)
		if entry != nil && entry.ParentSlug != "" && !seenGroup[entry.ParentSlug] {
			seenGroup[entry.ParentSlug] = true
			groups = append(groups, resultFilter{groupSlug: entry.ParentSlug})
		}
		if !seenURL[t.RegistryURL] {
			seenURL[t.RegistryURL] = true
			singles = append(singles, resultFilter{url: t.RegistryURL})
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
func (b *RegistryBrowser) entryFor(url string) *browserRegistryEntry {
	for i := range b.entries {
		if b.entries[i].URL == url {
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
	if entry := b.entryFor(b.registryFilter.url); entry != nil && entry.Alias != "" {
		if entry.ParentAlias != "" {
			return entry.ParentAlias + "/" + entry.Alias
		}
		return entry.Alias
	}
	return b.registryFilter.url
}

func (b *RegistryBrowser) pullSelectedTag() (*RegistryBrowser, tea.Cmd) {
	name := b.selectedImageName()
	if name == "" {
		return b, nil
	}
	b.imageName = name
	b.operation = "pull"
	b.state = browserStateStatus
	return b, tea.Batch(
		b.spinner.Tick,
		pullRegistryImageCmd(name),
	)
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

// normalizeRepoForRegistry prepends "library/" for bare names on Docker Hub.
func normalizeRepoForRegistry(registryURL, repo string) string {
	if strings.Contains(repo, "/") {
		return repo
	}
	base := strings.ToLower(strings.TrimSuffix(registryURL, "/"))
	for _, alias := range []string{"docker.io", "registry-1.docker.io"} {
		if base == alias {
			return "library/" + repo
		}
	}
	return repo
}

// multiImageName constructs the full image reference for pull/scan.
func multiImageName(registryURL, repo, tag string) string {
	base := strings.TrimSuffix(registryURL, "/")
	for _, alias := range []string{"docker.io", "registry-1.docker.io"} {
		if strings.EqualFold(base, alias) {
			return repo + ":" + tag
		}
	}
	return base + "/" + repo + ":" + tag
}

func (b *RegistryBrowser) updateFocus() {
	b.repoInput.Blur()
	if b.focusedField == brFieldRepo {
		b.repoInput.Focus()
	}
}

// filteredSortedMultiTags returns tags after applying registry filter, text filter, and sort.
func (b *RegistryBrowser) filteredSortedMultiTags() []MultiRegistryTag {
	query := strings.ToLower(b.filterInput.Value())
	var result []MultiRegistryTag
	for _, t := range b.tags {
		if !b.registryFilter.matches(t.RegistryURL, b.entryFor(t.RegistryURL)) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(t.Tag), query) &&
			!strings.Contains(strings.ToLower(t.Repo), query) {
			continue
		}
		result = append(result, t)
	}

	sortCol := b.tagSortCol
	sortDesc := b.tagSortDesc
	sort.SliceStable(result, func(i, j int) bool {
		var less bool
		switch sortCol {
		case tagSortByUpdated:
			less = result[i].UpdatedAt.After(result[j].UpdatedAt)
		default:
			less = strings.ToLower(result[i].Tag) < strings.ToLower(result[j].Tag)
		}
		if sortDesc {
			return !less
		}
		return less
	})
	return result
}
