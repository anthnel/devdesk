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

// CatalogLister walks a registry's catalog; oci.ListCatalogUnderContext in
// production. catalogPrefix is the repository path the catalog is served under
// (/v2/<catalogPrefix>/_catalog); empty means the root catalog.
type CatalogLister func(ctx context.Context, apiURL, catalogPrefix, username, password string,
	maxPages, pageSize int) (names []string, truncated bool, err error)

// CatalogSearcher searches a private registry by walking /v2/_catalog and
// filtering names client-side; the query is never sent to the registry.
type CatalogSearcher struct {
	// List overrides oci.ListCatalogUnderContext when set (tests).
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
		list = oci.ListCatalogUnderContext
	}
	if t.CatalogUnderPrefix && !validRepoPath(t.RepoPrefix) {
		// The catalog path is built from the prefix: nothing is sent for one that
		// is not a repository path.
		return nil, false, fmt.Errorf("%w (catalog prefix %q)", ErrUnsupported, t.RepoPrefix)
	}
	names, truncated, err := walkCatalog(ctx, t, list)
	if err != nil {
		return nil, false, err
	}
	if len(names) == 0 {
		return nil, truncated, nil
	}
	var all []Image
	if t.CatalogUnderPrefix {
		all = underPrefixRows(t, names, text)
	} else {
		var sawPrefix bool
		all, sawPrefix = rootRows(t, names, text)
		if prefix := strings.Trim(t.RepoPrefix, "/"); prefix != "" && !sawPrefix {
			if truncated {
				return nil, true, nil
			}
			return nil, false, fmt.Errorf("%w (prefix %q)", ErrPrefixNotInCatalog, prefix)
		}
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

// rootRows filters a root catalog: names must carry the entry's prefix (R1),
// which is stripped from Name. sawPrefix tells whether any name carried it.
func rootRows(t Target, names []string, text string) (rows []Image, sawPrefix bool) {
	prefix := strings.Trim(t.RepoPrefix, "/")
	needle := strings.ToLower(text)
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
			rows = append(rows, Image{EntryKey: t.EntryKey, Name: rest, Repo: name})
		}
	}
	return rows, sawPrefix
}

// underPrefixRows filters the catalog of a member read under its own path: the
// names are relative to RepoPrefix, so none is expected to carry it (no R1) and
// Repo is rebuilt as "<prefix>/<name>".
func underPrefixRows(t Target, names []string, text string) []Image {
	needle := strings.ToLower(text)
	var rows []Image
	for _, name := range names {
		if !validRepoPath(name) {
			continue
		}
		if strings.Contains(strings.ToLower(name), needle) {
			rows = append(rows, Image{EntryKey: t.EntryKey, Name: name, Repo: t.RepoPrefix + "/" + name})
		}
	}
	return rows
}
