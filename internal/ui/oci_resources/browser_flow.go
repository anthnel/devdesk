package ociresources

import (
	"context"
	"maps"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

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

// imageSearchDeadline bounds one registry's whole search.
var imageSearchDeadline = 15 * time.Second

// targetFor describes entry for the Searcher, with the same alias, address and
// credentials (D12) the tag flow uses.
func (b *RegistryBrowser) targetFor(entry browserRegistryEntry) imagesearch.Target {
	user, pass := b.credsFor(entry)
	return imagesearch.Target{
		EntryKey: entry.key, Alias: entryAlias(entry), RegistryURL: entry.URL,
		APIURL: registryAPIURL(entry.URL), RepoPrefix: entry.repoPrefix,
		Username: user, Password: pass,
	}
}

// startImageSearch opens a lot and asks the Searcher once per entry.
func (b *RegistryBrowser) startImageSearch(query string, entries []browserRegistryEntry) (*RegistryBrowser, tea.Cmd) {
	ctx := b.beginRequests()
	b.images = nil
	b.outcomes = map[string]imagesearch.Outcome{}
	b.searchQuery = query
	b.searchGen = b.gen
	b.searchKeys = make([]string, len(entries))
	for i, entry := range entries {
		b.searchKeys[i] = entry.key
	}
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
