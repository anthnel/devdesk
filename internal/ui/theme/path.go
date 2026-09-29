package theme

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-runewidth"
)

// FoldHome replaces the home directory prefix with "~". A path outside home is
// returned unchanged.
func FoldHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || path == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~" + string(filepath.Separator) + rel
}

// ShortPath renders path in at most maxWidth terminal columns, for a form field
// or a header value (§3.95).
//
// The order of what gives way is the order of what matters least:
//
//  1. the home prefix, folded to "~" whether or not it is needed — the long
//     form says nothing the short one does not;
//  2. the intermediate segments, reduced to their first rune from the left, the
//     way fish prints its prompt — `~/projects/workspace-entire/anthnel/devdesk`
//     becomes `~/p/workspace-entire/…` then `~/p/w/a/devdesk`, stopping as soon
//     as it fits. A hidden segment keeps its dot (`.devdesk` → `.d`): an
//     initial alone would name a different directory;
//  3. only then the head, via TruncateTailWidth, when even the fully reduced
//     form is too wide.
//
// The last segment is never reduced: it is the name the field is about.
func ShortPath(path string, maxWidth int) string {
	folded := FoldHome(path)
	if maxWidth <= 0 || runewidth.StringWidth(folded) <= maxWidth {
		return folded
	}

	sep := string(filepath.Separator)
	segs := strings.Split(folded, sep)
	// segs[0] is "" for an absolute path, "~" under home, or a volume ("C:");
	// none of those is a segment to reduce, and the last one is the name.
	for i := 1; i < len(segs)-1; i++ {
		segs[i] = initial(segs[i])
		if short := strings.Join(segs, sep); runewidth.StringWidth(short) <= maxWidth {
			return short
		}
	}
	return TruncateTailWidth(strings.Join(segs, sep), maxWidth)
}

// initial reduces a path segment to its first rune, keeping a leading dot.
func initial(seg string) string {
	runes := []rune(seg)
	switch {
	case len(runes) <= 1:
		return seg
	case runes[0] == '.':
		return string(runes[:2])
	default:
		return string(runes[:1])
	}
}
