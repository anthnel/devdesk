package dockerfile

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// What a build leaves out of its context (§3.81).
//
// This reads a .dockerignore for one purpose: to say that a path **will** be
// sent to the builder, so a check can report it. The answer therefore comes
// with a second one — whether it is certain — and a caller reports only an
// exposure that is. A pattern this cannot evaluate, or a negation that might
// re-include part of a tree, makes the answer uncertain rather than a guess in
// either direction: a guess towards "exposed" is a false finding, which is
// what the whole check exists not to produce.
//
// The syntax is Docker's (moby/patternmatcher): one pattern per line, `#`
// comments, `!` to re-include, paths relative to the context root with a
// leading `/` dropped, `*` and `?` within one path segment, `**` across any
// number of them, and the last matching pattern winning. A pattern that
// matches a directory matches everything under it.

const (
	// ignoreSuffix is what BuildKit appends to a Dockerfile's path to find the
	// ignore file meant for that Dockerfile alone.
	ignoreSuffix = ".dockerignore"
	// RootIgnoreFile is the context's own ignore file, which every builder reads.
	RootIgnoreFile = ".dockerignore"
)

// IgnoreFiles returns the ignore files that may apply to a build of the
// Dockerfile at dockerfileRel with root as its context, as slash paths relative
// to root, among those that exist.
//
// There can be two. BuildKit reads `<Dockerfile>.dockerignore` first, and only
// falls back to the context's `.dockerignore` when there is none; the legacy
// builder never reads the former. Which one a build uses therefore depends on
// the builder, which a file cannot tell — so a caller treats a path as sent
// only when no file that may apply leaves it out.
func IgnoreFiles(root, dockerfileRel string) []string {
	var out []string
	for _, rel := range []string{dockerfileRel + ignoreSuffix, RootIgnoreFile} {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil && info.Mode().IsRegular() {
			out = append(out, rel)
		}
	}
	return out
}

// Ignore is a parsed .dockerignore.
type Ignore struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	negate bool
	segs   []string
	// bad is a pattern this cannot evaluate. Docker would refuse the build on
	// some of them; either way, nothing it says can be relied on.
	bad bool
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ParseIgnore reads a .dockerignore the way Docker does.
func ParseIgnore(content []byte) Ignore {
	var ig Ignore
	content = bytes.TrimPrefix(content, utf8BOM)
	for _, raw := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(raw, "#") {
			continue // a comment only counts in the first column, as in Docker
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		p := ignorePattern{}
		if line[0] == '!' {
			p.negate = true
			line = strings.TrimSpace(line[1:])
		}
		if line == "" {
			continue
		}
		line = path.Clean(filepath.ToSlash(line))
		if len(line) > 1 && line[0] == '/' {
			line = line[1:]
		}
		p.segs = strings.Split(line, "/")
		for _, s := range p.segs {
			if s == "" || (s != "**" && strings.Contains(s, "**")) {
				p.bad = true
			} else if _, err := path.Match(s, ""); err != nil {
				p.bad = true
			}
		}
		ig.patterns = append(ig.patterns, p)
	}
	return ig
}

// Excluded reports whether the slash path rel, relative to the context root,
// is left out of the context, and whether that answer is certain.
func (ig Ignore) Excluded(rel string) (excluded, certain bool) {
	name := splitRel(rel)
	for _, p := range ig.patterns {
		if p.bad {
			return false, false
		}
		if p.matchesOrParent(name) {
			excluded = !p.negate
		}
	}
	return excluded, true
}

// ExcludesTree reports whether rel and everything under it are left out, and
// whether that answer is certain.
//
// Excluding a directory is not enough on its own: a `!.git/config` after the
// pattern that excludes it sends that one file after all. A negation that could
// reach below rel makes the answer uncertain — whether it matches anything
// depends on what the directory holds, and "some of it" is not what a finding
// about the whole of it would say.
func (ig Ignore) ExcludesTree(rel string) (excluded, certain bool) {
	excluded, certain = ig.Excluded(rel)
	if !excluded || !certain {
		return excluded, certain
	}
	// Only a negation after the last pattern that excludes rel can undo it: an
	// earlier one is itself overridden, below rel as at rel.
	name := splitRel(rel)
	last := -1
	for i, p := range ig.patterns {
		if !p.negate && p.matchesOrParent(name) {
			last = i
		}
	}
	for _, p := range ig.patterns[last+1:] {
		if p.negate && couldMatchBelow(p.segs, name) {
			return false, false
		}
	}
	return true, true
}

// matchesOrParent is Docker's rule that a pattern matching a directory matches
// everything under it.
func (p ignorePattern) matchesOrParent(name []string) bool {
	for n := len(name); n > 0; n-- {
		if matchSegs(p.segs, name[:n]) {
			return true
		}
	}
	return false
}

// matchSegs matches pattern segments against path segments, `**` standing for
// any number of them, zero included.
func matchSegs(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegs(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	// ParseIgnore marked every pattern path.Match rejects as bad, and Excluded
	// never gets here with one.
	if ok, _ := path.Match(pat[0], name[0]); !ok {
		return false
	}
	return matchSegs(pat[1:], name[1:])
}

// couldMatchBelow reports whether a pattern may match some path strictly under
// name. It answers yes whenever it cannot rule it out.
func couldMatchBelow(pat, name []string) bool {
	for i := 0; ; i++ {
		switch {
		case i == len(pat):
			return false // the pattern ends at or above name
		case pat[i] == "**", i == len(name):
			return true
		}
		if ok, _ := path.Match(pat[i], name[i]); !ok {
			return false
		}
	}
}

func splitRel(rel string) []string {
	return strings.Split(path.Clean(filepath.ToSlash(rel)), "/")
}
