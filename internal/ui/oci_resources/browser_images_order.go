package ociresources

import (
	"cmp"
	"slices"
	"strings"
)

// The ranks of matchTier, lowest first.
const (
	tierExact      = iota // the name is the query
	tierLastEquals        // the last path segment is the query
	tierLastPrefix        // the last path segment starts with the query
	tierContains          // the name contains the query
	tierNoMatch           // nothing in the name matches
)

// matchTier ranks name against query, lowest first, case-insensitive.
func matchTier(query, name string) int {
	q, n := strings.ToLower(query), strings.ToLower(name)
	last := n[strings.LastIndex(n, "/")+1:]
	switch {
	case n == q:
		return tierExact
	case last == q:
		return tierLastEquals
	case strings.HasPrefix(last, q):
		return tierLastPrefix
	case strings.Contains(n, q):
		return tierContains
	}
	return tierNoMatch
}

// sortSearchRows orders rows by matchTier, then lower-cased name, then
// registry alias (label), then EntryKey, then Repo. The result depends only on the rows
// and the query, never on which registry answered first (DEVDESK-302). Stars
// play no part.
func sortSearchRows(query string, rows []searchRow) {
	slices.SortStableFunc(rows, func(a, b searchRow) int {
		return cmp.Or(
			cmp.Compare(matchTier(query, a.img.Name), matchTier(query, b.img.Name)),
			cmp.Compare(strings.ToLower(a.img.Name), strings.ToLower(b.img.Name)),
			cmp.Compare(a.label, b.label),
			cmp.Compare(a.img.EntryKey, b.img.EntryKey),
			cmp.Compare(a.img.Repo, b.img.Repo),
		)
	})
}
