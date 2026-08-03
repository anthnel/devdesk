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

func (b *RegistryBrowser) cycleRegistryFilter() {
	seen := make(map[string]bool)
	var urls []string
	for _, t := range b.tags {
		if !seen[t.RegistryURL] {
			seen[t.RegistryURL] = true
			urls = append(urls, t.RegistryURL)
		}
	}
	if len(urls) <= 1 {
		b.registryFilter = ""
		b.rebuildTagTable()
		return
	}
	if b.registryFilter == "" {
		b.registryFilter = urls[0]
	} else {
		found := false
		for i, u := range urls {
			if u == b.registryFilter {
				next := (i + 1) % (len(urls) + 1)
				if next == len(urls) {
					b.registryFilter = ""
				} else {
					b.registryFilter = urls[next]
				}
				found = true
				break
			}
		}
		if !found {
			b.registryFilter = ""
		}
	}
	b.rebuildTagTable()
}

// registryFilterLabel returns a short display label for the active registry filter.
func (b *RegistryBrowser) registryFilterLabel() string {
	for _, reg := range b.registries {
		if reg.URL == b.registryFilter {
			if reg.Alias != "" {
				return reg.Alias
			}
			return reg.URL
		}
	}
	return b.registryFilter
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
		if b.registryFilter != "" && t.RegistryURL != b.registryFilter {
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
