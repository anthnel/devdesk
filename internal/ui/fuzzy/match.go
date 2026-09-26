// Package fuzzy is the "g" prompt two views share — the workspaces tree and
// the forge explorer — and the scorer behind it. A view hands it candidates;
// it ranks them against what the user types and says which one was picked.
package fuzzy

import "strings"

// Match reports whether query is a subsequence of candidate
// (case-insensitive) and, when it is, a score where higher is a better
// match. It favors consecutive runs of matched characters and a match
// starting right after a path separator, so "wsd" scores
// "workspaces/devdesk" higher than a path where the three letters are
// scattered across unrelated segments.
//
// It started in the workspaces view and moved here when the forge explorer
// gained the same prompt: one scorer, so the two rank the same query the same
// way.
func Match(candidate, query string) (int, bool) {
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
