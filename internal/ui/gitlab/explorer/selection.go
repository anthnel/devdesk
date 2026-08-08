package explorer

import (
	"strings"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// cloneSelection is what the user has picked to clone: a set of **roots**, plus
// the descendants explicitly taken back out (§3.16, decision 11).
//
// It is deliberately not a list of the chosen repositories. Building one would
// mean enumerating a group's children the moment it is ticked — the whole API
// walk, run before the user has confirmed anything — which is the freeze the
// pipelined design exists to remove, moved one screen earlier. "This group,
// minus these" needs to know nothing about what the group contains, so a group
// nobody has expanded can still be selected, displayed and walked.
//
// Paths are `FullPath` throughout: "acme/platform/backend". That is the forge's
// own hierarchy, and by decision 4 it is the directory layout as well, so one
// string serves for membership, display and destination.
type cloneSelection struct {
	roots    map[string]bool
	excluded map[string]bool
}

func newCloneSelection() cloneSelection {
	return cloneSelection{roots: map[string]bool{}, excluded: map[string]bool{}}
}

// includes reports whether a path is part of the selection.
//
// The answer is the **nearest** marked ancestor-or-self: a root includes
// everything below it, an exclusion cuts everything below itself, and a root
// added under an exclusion turns it back on. Nothing marked anywhere above
// means the path was never chosen.
func (s cloneSelection) includes(path string) bool {
	for p := path; p != ""; p = parentPath(p) {
		if s.excluded[p] {
			return false
		}
		if s.roots[p] {
			return true
		}
	}
	return false
}

// toggle flips one node, which is the only way either set is written to.
//
// Excluding something that was never included, or rooting something already
// covered, would both be noise the display could not explain, so each side
// clears the other.
func (s cloneSelection) toggle(path string) {
	if s.includes(path) {
		delete(s.roots, path)
		s.excluded[path] = true
		return
	}
	delete(s.excluded, path)
	s.roots[path] = true
}

// state is what the checkbox shows.
//
// It needs no discovery, which is the point: an exclusion only ever comes from
// a keystroke on a node already on screen, so "is something below this off"
// is answerable from the two sets alone — including for a group whose children
// have never been fetched.
func (s cloneSelection) state(path string) theme.CheckState {
	if s.includes(path) {
		if hasDescendant(s.excluded, path) {
			return theme.CheckSome
		}
		return theme.CheckAll
	}
	if hasDescendant(s.roots, path) {
		return theme.CheckSome
	}
	return theme.CheckNone
}

// isEmpty reports whether anything at all is selected.
func (s cloneSelection) isEmpty() bool { return len(s.roots) == 0 }

// counts is what the header states instead of a repository count: the number
// cannot be known before discovery has run, and discovery only starts once the
// user has confirmed (decision 7).
func (s cloneSelection) counts() (roots, exclusions int) {
	return len(s.roots), len(s.excluded)
}

// rootPaths returns the selected roots, for the walk to start from.
func (s cloneSelection) rootPaths() []string {
	out := make([]string, 0, len(s.roots))
	for p := range s.roots {
		out = append(out, p)
	}
	return out
}

// parentPath drops the last segment, returning "" at the top.
func parentPath(path string) string {
	if i := strings.LastIndex(path, "/"); i > 0 {
		return path[:i]
	}
	return ""
}

// hasDescendant reports whether the set holds anything strictly below path.
// The trailing separator is what keeps "acme/platform-tools" from counting as a
// descendant of "acme/platform".
func hasDescendant(set map[string]bool, path string) bool {
	prefix := path + "/"
	for p := range set {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
