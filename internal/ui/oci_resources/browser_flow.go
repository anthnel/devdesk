package ociresources

import (
	"context"
	"maps"
	"slices"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/oci"
)

// browserGen numbers the lots of tag requests across every browser instance,
// so an answer from a browser closed and reopened since is never taken for the
// open one's. Zero is never handed out: it means "no lot".
var browserGen atomic.Uint64

// listRegistryTags is the seam the tag search reads a registry through; tests
// swap it.
var listRegistryTags = oci.ListRegistryTagsContext

// nextBrowserGen returns a fresh generation. Called from Update only (Rule 110).
func nextBrowserGen() uint64 { return browserGen.Add(1) }

// beginRequests cancels the lot in flight and opens a new one: a new
// generation, a fresh context, no pending search. Call it only once a request
// is certain to start, so a refused validation leaves the running lot alone.
func (b *RegistryBrowser) beginRequests() context.Context {
	b.cancelInFlight()
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.gen = nextBrowserGen()
	b.pendingSearches = 0
	return ctx
}

// abandonLot cancels the lot in flight and moves to a fresh generation with no
// pending search, so every late answer of that lot is dropped. Called from
// Update only (Rule 110).
func (b *RegistryBrowser) abandonLot() {
	b.cancelInFlight()
	b.gen = nextBrowserGen()
	b.pendingSearches = 0
}

// cancelInFlight cancels the lot's context, if any. Nil-safe.
func (b *RegistryBrowser) cancelInFlight() {
	if b == nil || b.cancel == nil {
		return
	}
	b.cancel()
	b.cancel = nil
}

// RegistryBrowserGen returns the generation the open browser accepts, 0 when it
// is closed. For tests, like ActivityTables.
func (m Model) RegistryBrowserGen() uint64 {
	if m.registryBrowser == nil {
		return 0
	}
	return m.registryBrowser.gen
}

// defaultImageSearcher is what a browser opens with: Docker Hub through its
// search API, any other registry through its catalog (/v2/_catalog), so a
// private registry no longer answers imagesearch.ErrUnsupported. Tests replace
// it before opening the browser.
var defaultImageSearcher imagesearch.Searcher = imagesearch.ByRegistry(imagesearch.HubSearcher{}, imagesearch.CatalogSearcher{})

// imageSearchDeadline bounds one registry's whole search. It is the registry client's
// own delay, so the "no answer within N s" a search reports is the delay it waited.
var imageSearchDeadline = oci.RequestTimeout

// targetFor describes entry for the Searcher, with the same alias, address and
// credentials (D12) the tag flow uses.
func (b *RegistryBrowser) targetFor(entry browserRegistryEntry) imagesearch.Target {
	user, pass := b.credsFor(entry)
	return imagesearch.Target{
		EntryKey: entry.key, Alias: entryAlias(entry), RegistryURL: entry.URL,
		APIURL: registryAPIURL(entry.URL), RepoPrefix: entry.repoPrefix,
		CatalogUnderPrefix: entry.catalogUnderPrefix,
		Username:           user, Password: pass,
	}
}

// groupUnitSuffix follows the group's label on the unit that stands for members
// with no address of their own (searchUnits). It is the one place the label is
// written.
const groupUnitSuffix = " (group)"

// searchUnits turns the entries a search is asked on into what it walks. The
// members of a group that share the group's address (its URL and its prefix, so
// the same catalog read under the same identity) are replaced by ONE entry, the
// group, standing where the first of them stood: a connector of its own answers
// /v2/_catalog with the union of its members' names and no attribution, so
// walking it once per member would repeat every name and credit it to the wrong
// member (DEVDESK-579, 3-A). The rule is "same address", not "Nexus": groups maps
// every group's slug to the group, whatever its provider. Everything else is kept
// as it is, in order. Pure: the input is not modified.
func searchUnits(entries []browserRegistryEntry, groups map[string]config.RegistryItem) []browserRegistryEntry {
	var units []browserRegistryEntry
	placed := map[string]bool{}
	for _, e := range entries {
		g, ok := groups[e.ParentSlug]
		if !ok || e.ParentSlug == "" || e.URL != g.URL || e.repoPrefix != g.RepoPrefix {
			units = append(units, e)
			continue
		}
		if !placed[g.Slug] {
			placed[g.Slug] = true
			units = append(units, groupUnit(e, g))
		}
	}
	return units
}

// groupUnit is the entry that searches group g in place of its member first: the
// group's own address and key, read at the root of its catalog, with what the
// members agreed on (credentials, cache-only). ParentSlug stays g's slug, so the
// group-level `r` stop takes its rows; ParentAlias stays empty, so the label is
// the group's, marked by groupUnitSuffix.
func groupUnit(first browserRegistryEntry, g config.RegistryItem) browserRegistryEntry {
	return browserRegistryEntry{
		URL: g.URL, Alias: browserAlias(g) + groupUnitSuffix, repoPrefix: g.RepoPrefix,
		Slug: g.Slug, ParentSlug: g.Slug, parentURL: g.URL,
		authMode: first.authMode, cachedOnly: first.cachedOnly,
		key: g.Slug,
	}
}

// groupsBySlug maps the slug of every configured group to the group.
func (b *RegistryBrowser) groupsBySlug() map[string]config.RegistryItem {
	groups := map[string]config.RegistryItem{}
	for _, reg := range b.registries {
		if reg.Kind == config.KindGroup {
			groups[reg.Slug] = reg
		}
	}
	return groups
}

// startImageSearch opens a lot and asks the Searcher once per search unit
// (searchUnits) of entries. The units a lot makes up live in lotUnits until the
// next image search, which survives the tag lots opened from its rows so that →
// resolves the unit, and never in b.entries.
func (b *RegistryBrowser) startImageSearch(query string, picked []browserRegistryEntry) (*RegistryBrowser, tea.Cmd) {
	entries := searchUnits(picked, b.groupsBySlug())
	b.lotUnits = map[string]browserRegistryEntry{}
	for _, e := range entries {
		if !slices.Contains(picked, e) {
			b.lotUnits[e.key] = e
		}
	}
	// The walk memo belongs to this lot: it dies with it, and nothing is
	// cached between two searches.
	ctx := imagesearch.WithSharedWalks(b.beginRequests())
	b.images = nil
	b.outcomes = map[string]imagesearch.Outcome{}
	b.searchQuery = query
	b.searchGen = b.gen
	b.searchKeys = make([]string, len(entries))
	for i, entry := range entries {
		b.searchKeys[i] = entry.key
	}
	// A new search starts from the default order and without the previous
	// confirmed `/` filter (DEVDESK-309).
	b.searchTable.FilterBar().ClearSearch()
	b.searchFilter = resultFilter{}
	b.syncSearchFilterToken()
	b.searchTable.SetSort(-1, false)
	b.searchTable.SetItems(nil)

	cmds := make([]tea.Cmd, 0, len(entries))
	for _, entry := range entries {
		b.pendingSearches++
		cmds = append(cmds, searchImagesCmd(ctx, b.gen, b.targetFor(entry), imagesearch.Query{Text: query}, b.searcher, imageSearchDeadline))
	}
	b.state = browserStateImages
	b.searchTable.Focus()
	return b, tea.Batch(cmds...)
}

// searchImagesCmd runs one registry's search under deadline (read from
// imageSearchDeadline by the caller, in Update), inside the lot's context. It
// reads no model state (Rule 110).
func searchImagesCmd(parent context.Context, gen uint64, t imagesearch.Target, q imagesearch.Query, s imagesearch.Searcher, deadline time.Duration) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, deadline)
		defer cancel()
		images, partial, err := s.Search(ctx, t, q)
		return ImageSearchResultMsg{Gen: gen, EntryKey: t.EntryKey, Images: images, Partial: partial, Err: err}
	}
}

// RegistryBrowserOutcomes returns a copy of the verdicts of the open browser's
// current search, nil when the browser is closed. For tests and IS-13.
func (m Model) RegistryBrowserOutcomes() map[string]imagesearch.Outcome {
	if m.registryBrowser == nil {
		return nil
	}
	return maps.Clone(m.registryBrowser.outcomes)
}
