package ociresources

import (
	"context"
	"sync/atomic"

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
