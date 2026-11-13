package imagesearch

import (
	"regexp"
	"strings"
)

// repoComponent is one path component of a repository name: lowercase
// alphanumerics joined by '.', '_', '__' or runs of '-' (OCI distribution spec).
var repoComponent = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)

// validRepoPath reports whether every '/'-separated component of name is a
// valid OCI repository component. Empty is invalid.
func validRepoPath(name string) bool {
	if name == "" {
		return false
	}
	for _, p := range strings.Split(name, "/") {
		if !repoComponent.MatchString(p) {
			return false
		}
	}
	return true
}

// The ranks of MatchTier, lowest first.
const (
	TierExact      = iota // the name is the query
	TierLastEquals        // the last path segment is the query
	TierLastPrefix        // the last path segment starts with the query
	TierContains          // the name contains the query
	TierNoMatch           // nothing in the name matches
)

// MatchTier ranks name against query, lowest first, case-insensitive.
func MatchTier(query, name string) int {
	q, n := strings.ToLower(query), strings.ToLower(name)
	last := n[strings.LastIndex(n, "/")+1:]
	switch {
	case n == q:
		return TierExact
	case last == q:
		return TierLastEquals
	case strings.HasPrefix(last, q):
		return TierLastPrefix
	case strings.Contains(n, q):
		return TierContains
	}
	return TierNoMatch
}
