package imagesearch

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/oci"
)

const (
	// CatalogMaxPages is the number of catalog pages followed per walk.
	CatalogMaxPages = 10
	// CatalogPageSize is the number of names asked per page (?n=).
	CatalogPageSize = 1000
	// CatalogMaxRows is the number of rows one registry may show.
	CatalogMaxRows = 100
	// CatalogMinQueryRunes is the shortest query that walks a catalog.
	CatalogMinQueryRunes = 2
)

// CatalogLister walks a registry's catalog; oci.ListCatalogContext in production.
type CatalogLister func(ctx context.Context, apiURL, username, password string,
	maxPages, pageSize int) (names []string, truncated bool, err error)

// CatalogSearcher searches a private registry by walking /v2/_catalog and
// filtering names client-side; the query is never sent to the registry.
type CatalogSearcher struct {
	// List overrides oci.ListCatalogContext when set (tests).
	List CatalogLister
}

// Search implements Searcher.
func (c CatalogSearcher) Search(ctx context.Context, t Target, q Query) ([]Image, bool, error) {
	text := strings.TrimSpace(q.Text)
	if utf8.RuneCountInString(text) < CatalogMinQueryRunes {
		return nil, false, ErrQueryTooShort
	}
	list := c.List
	if list == nil {
		list = oci.ListCatalogContext
	}
	names, truncated, err := walkCatalog(ctx, t, list)
	if err != nil {
		return nil, false, err
	}
	if len(names) == 0 {
		return nil, truncated, nil
	}
	prefix := strings.Trim(t.RepoPrefix, "/")
	needle := strings.ToLower(text)
	var all []Image
	sawPrefix := false
	for _, name := range names {
		rest := name
		if prefix != "" {
			var ok bool
			if rest, ok = strings.CutPrefix(name, prefix+"/"); !ok {
				continue
			}
			sawPrefix = true
		}
		if !validRepoPath(name) {
			continue
		}
		if strings.Contains(strings.ToLower(rest), needle) {
			all = append(all, Image{EntryKey: t.EntryKey, Name: rest, Repo: name})
		}
	}
	if prefix != "" && !sawPrefix {
		if truncated {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("%w (prefix %q)", ErrPrefixNotInCatalog, prefix)
	}
	slices.SortStableFunc(all, func(a, b Image) int {
		if d := MatchTier(text, a.Name) - MatchTier(text, b.Name); d != 0 {
			return d
		}
		if d := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); d != 0 {
			return d
		}
		return strings.Compare(a.Repo, b.Repo)
	})
	limit := CatalogMaxRows
	if q.Limit > 0 && q.Limit < limit {
		limit = q.Limit
	}
	cut := len(all) > limit
	if cut {
		all = all[:limit]
	}
	return all, truncated || cut, nil
}
