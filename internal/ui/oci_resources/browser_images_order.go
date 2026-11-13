package ociresources

import (
	"cmp"
	"slices"
	"strings"

	"github.com/anthnel/devdesk/internal/imagesearch"
)

// matchTier ranks name against query, lowest first, case-insensitive.
func matchTier(query, name string) int { return imagesearch.MatchTier(query, name) }

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

// lessSearchName orders by lower-cased name; names equal ignoring case tie.
func lessSearchName(a, b searchRow) bool {
	return strings.ToLower(a.img.Name) < strings.ToLower(b.img.Name)
}

// lessSearchRegistry orders by lower-cased registry label.
func lessSearchRegistry(a, b searchRow) bool {
	return strings.ToLower(a.label) < strings.ToLower(b.label)
}

// lessSearchStars orders by starsKey: no stars, then zero, then the count.
func lessSearchStars(a, b searchRow) bool { return starsKey(a) < starsKey(b) }

// starsKey is -1 when the registry reports no stars, so those rows stay in one
// block apart from an image that has zero stars.
func starsKey(r searchRow) int {
	if !r.img.HasStars {
		return -1
	}
	return r.img.Stars
}
