package workspaces

import "strings"

// matchFuzzy reports whether query is a subsequence of candidate
// (case-insensitive) and, when it is, a score where higher is a better
// match. It favors consecutive runs of matched characters and a match
// starting right after a path separator, so "wsd" scores
// "workspaces/devdesk" higher than a path where the three letters are
// scattered across unrelated segments.
//
// This is new code: no fuzzy-matching library or algorithm exists anywhere
// else in this repository.
func matchFuzzy(candidate, query string) (int, bool) {
	if query == "" {
		return 0, false
	}
	c := []rune(strings.ToLower(candidate))
	q := []rune(strings.ToLower(query))

	score := 0
	ci := 0
	consecutive := false
	for _, qr := range q {
		found := false
		for ; ci < len(c); ci++ {
			if c[ci] != qr {
				consecutive = false
				continue
			}
			found = true
			score++
			if consecutive {
				score += 3
			}
			if ci == 0 || c[ci-1] == '/' {
				score += 5
			}
			consecutive = true
			ci++
			break
		}
		if !found {
			return 0, false
		}
	}
	// A tighter match — fewer candidate characters spanned for the same
	// query — ranks above a looser one that happens to match the same set
	// of letters.
	score -= ci - len(q)
	return score, true
}
