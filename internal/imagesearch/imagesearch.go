// Package imagesearch defines the types and the interface of an image search
// against one registry: what to search (Query), where (Target) and what comes
// back (Image). It is pure Go, with no UI and no network code of its own; the
// adapters that talk to a registry implement Searcher elsewhere.
//
// Anonymous Docker Hub search is not covered here (see IS-02).
package imagesearch

import "context"

// Target describes the registry entry a search runs against.
type Target struct {
	EntryKey    string
	Alias       string
	RegistryURL string
	APIURL      string
	RepoPrefix  string
	// CatalogUnderPrefix tells that the catalog is served under RepoPrefix, at
	// /v2/<RepoPrefix>/_catalog, and lists names relative to it (a Nexus group
	// member, DEVDESK-579). Without it the catalog is the root one and
	// RepoPrefix is expected inside its names.
	CatalogUnderPrefix bool
	// Username and Password are empty for an anonymous entry (D12).
	Username string
	Password string
}

// Query is a search request. Limit 0 means the adapter's default.
type Query struct {
	Text  string
	Limit int
}

// Image is one search result.
type Image struct {
	EntryKey string
	Name     string
	// Repo is the final repository path, with the prefix and the library/
	// namespace applied exactly once (D39).
	Repo        string
	Description string
	Stars       int
	// HasStars tells whether Stars is meaningful for this registry.
	HasStars bool
	Official bool
}

// Searcher searches one registry. partial is true when the result is cut short
// (limit, deadline or a page that failed) but still usable.
type Searcher interface {
	Search(ctx context.Context, t Target, q Query) (images []Image, partial bool, err error)
}

// SearcherFunc adapts a function to the Searcher interface.
type SearcherFunc func(ctx context.Context, t Target, q Query) ([]Image, bool, error)

// Search calls f.
func (f SearcherFunc) Search(ctx context.Context, t Target, q Query) ([]Image, bool, error) {
	return f(ctx, t, q)
}
