package remediation

import "slices"

// CatalogIDs lists the AVD id of every rule of the catalog, sorted. The slice is
// a copy: a caller may reorder or edit it.
func CatalogIDs() []string {
	ids := make([]string, 0, len(catalog))
	for _, rule := range catalog {
		ids = append(ids, rule.AVDID)
	}
	slices.Sort(ids)
	return ids
}
