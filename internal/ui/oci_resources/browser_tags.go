package ociresources

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/oci"
)

// filterStops returns the values `r` cycles through for the tags tab.
func (b *RegistryBrowser) filterStops() []resultFilter {
	keys := make([]string, len(b.tags))
	for i, t := range b.tags {
		keys[i] = t.EntryKey
	}
	return b.filterStopsFor(keys)
}

// filterStopsFor returns the values `r` cycles through for results coming from
// the given entry keys, in order of first appearance: every group that
// produced results, then every individual registry, then off. The group level
// is what makes "everything from this Nexus" one keystroke rather than eight.
// Repeated keys collapse.
func (b *RegistryBrowser) filterStopsFor(keys []string) []resultFilter {
	seenGroup, seenEntry := map[string]bool{}, map[string]bool{}
	var groups, singles []resultFilter
	for _, key := range keys {
		entry := b.entryFor(key)
		if entry != nil && entry.ParentSlug != "" && !seenGroup[entry.ParentSlug] {
			seenGroup[entry.ParentSlug] = true
			groups = append(groups, resultFilter{groupSlug: entry.ParentSlug})
		}
		if !seenEntry[key] {
			seenEntry[key] = true
			singles = append(singles, resultFilter{entryKey: key})
		}
	}
	// A single source has nothing to filter down to.
	if len(singles) <= 1 {
		return nil
	}
	// A group holding every result and every member says the same as "off".
	if len(groups) == 1 {
		n := countMembers(b.entries, groups[0].groupSlug)
		inGroup := 0
		for _, f := range singles {
			if e := b.entryFor(f.entryKey); e != nil && e.ParentSlug == groups[0].groupSlug {
				inGroup++
			}
		}
		if inGroup == len(singles) && len(singles) == n {
			groups = nil
		}
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
	b.registryFilter = nextFilter(b.filterStops(), b.registryFilter)
	b.rebuildTagTable()
}

// nextFilter returns the stop after cur: off goes to the first stop, the last
// stop and any unknown filter go back to off.
func nextFilter(stops []resultFilter, cur resultFilter) resultFilter {
	if len(stops) == 0 {
		return resultFilter{}
	}
	if cur.isEmpty() {
		return stops[0]
	}
	for i, s := range stops {
		if s == cur {
			if i+1 < len(stops) {
				return stops[i+1]
			}
			return resultFilter{}
		}
	}
	return resultFilter{}
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
	return b.filterLabel(b.registryFilter)
}

// filterLabel returns the display label of any filter value.
func (b *RegistryBrowser) filterLabel(f resultFilter) string {
	if f.groupSlug != "" {
		for _, reg := range b.registries {
			if reg.Slug == f.groupSlug {
				return browserAlias(reg)
			}
		}
		return f.groupSlug
	}
	if entry := b.entryFor(f.entryKey); entry != nil {
		if entry.ParentAlias != "" {
			return entry.ParentAlias + "/" + entry.Alias
		}
		if entry.Alias != "" {
			return entry.Alias
		}
		return registryRef(entry.URL, entry.repoPrefix)
	}
	return f.entryKey
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
	if oci.IsDockerHub(registryURL) {
		return "library/" + repo
	}
	return repo
}

// multiImageName constructs the full image reference for pull/scan.
//
// A Hub reference carries no host under docker: `docker pull nginx` leaves
// `nginx` as the local repository name, and qualifying it would only make the
// Images tab show `docker.io/library/nginx` next to a `nginx` pulled by hand.
// Podman disagrees, and not cosmetically — measured against a real podman
// with no `unqualified-search-registries` configured (§3.67), which is the
// package default on Debian/Ubuntu and Fedora alike: `podman pull
// library/nginx:tag` refuses outright ("short-name ... did not resolve to an
// alias and no unqualified-search registries are defined"), where docker
// would have silently assumed Hub. So the bare form is kept for docker, where
// it has always worked, and only podman gets the explicit host — which is
// also the form its own `images` list already uses (§3.67), so nothing about
// this makes a podman-pulled image look different from one already there.
func multiImageName(registryURL, repo, tag string) string {
	if oci.IsDockerHub(registryURL) {
		if engine.Current().Name == engine.Podman {
			return "docker.io/" + repo + ":" + tag
		}
		return repo + ":" + tag
	}
	return oci.RegistryHost(registryURL) + "/" + repo + ":" + tag
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
