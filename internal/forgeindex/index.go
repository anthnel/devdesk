// Package forgeindex is a list of everything a forge session can see — every
// namespace and every repository, at any depth — built once per context and
// kept on disk.
//
// # Why it exists, when D36 said there should be no such cache
//
// D36 removed `CachedGroups` and `CachedProjects` from the shared state, for
// two reasons: nothing ever wrote them, and a flat slice fetched in one go is
// the full API walk that §3.16 removed because it froze the explorer for
// minutes. This package answers both. It is written — by the router, at every
// session start — and the walk never blocks anything: it runs as a job, the
// explorer shows what the previous walk left on disk meanwhile, and a level on
// screen is re-read from the forge anyway.
//
// # What it holds, and what it does not
//
// An entry is what an *undecorated* listing returns: identity, path, name,
// visibility, dates, web page. The role and the CI status are not in it. On
// GitLab they cost two requests per repository, so a walk that fetched them
// would cost what §3.16 removed; and a pipeline status is stale within the
// minute, which is the wrong thing to keep on disk. The explorer decorates the
// one level it shows, as it always has.
//
// # An index is a value
//
// Every method that changes one returns a new index and leaves the receiver
// alone. The router hands the same index to a Cmd that writes it to disk and
// to the views that read it, and an in-place edit would race the first against
// the second.
package forgeindex

import "time"

// Version is the shape of the file. A file of another version is ignored
// rather than migrated: the next walk rewrites it within seconds.
const Version = 1

// Kind says what an entry is.
type Kind string

const (
	KindNamespace  Kind = "namespace"
	KindRepository Kind = "repository"
)

// Entry is one namespace or one repository.
//
// Parent is the path of the namespace it sits in, empty for a root namespace.
// It is stored rather than derived from Path because the two need not agree:
// GitHub lists a user's own repositories under a namespace whose path is the
// login, and a forge is free to answer with paths that do not nest.
type Entry struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	Kind   Kind   `json:"kind"`

	Visibility        string     `json:"visibility,omitempty"`
	CreatedAt         *time.Time `json:"created_at,omitempty"`
	LastActivityAt    *time.Time `json:"last_activity_at,omitempty"`
	WebURL            string     `json:"web_url,omitempty"`
	DeletionScheduled bool       `json:"deletion_scheduled,omitempty"`
}

// Index is the whole tree one session can see.
//
// Host and User say whose tree it is. A file written for another account on
// the same context — a token replaced by a colleague's — lists what that
// account could see, and Matches is what keeps it off screen.
type Index struct {
	Version int       `json:"version"`
	Host    string    `json:"host"`
	User    string    `json:"user"`
	BuiltAt time.Time `json:"built_at"`
	Entries []Entry   `json:"entries"`

	// Unlisted are the namespaces whose own listing failed. They are in
	// Entries — their parent listed them — but what they contain is unknown,
	// which is a different answer from "empty" and must stay one: Children
	// reports it as not known, and the explorer asks the forge instead.
	Unlisted []string `json:"unlisted,omitempty"`

	byPath   map[string]int
	children map[string][]int
	unlisted map[string]bool
}

// New builds an index from what a walk found. The slice is kept in the order
// given: a level is listed in the order its entries appear, which is the
// forge's own order when Build wrote them.
func New(host, user string, builtAt time.Time, entries []Entry, unlisted []string) *Index {
	ix := &Index{
		Version:  Version,
		Host:     host,
		User:     user,
		BuiltAt:  builtAt,
		Entries:  entries,
		Unlisted: unlisted,
	}
	ix.reindex()
	return ix
}

// reindex rebuilds the lookups from Entries and Unlisted. It runs once per
// value, after New or after a decode.
func (ix *Index) reindex() {
	ix.byPath = make(map[string]int, len(ix.Entries))
	ix.children = make(map[string][]int)
	for i, e := range ix.Entries {
		ix.byPath[e.Path] = i
		ix.children[e.Parent] = append(ix.children[e.Parent], i)
	}
	ix.unlisted = make(map[string]bool, len(ix.Unlisted))
	for _, path := range ix.Unlisted {
		ix.unlisted[path] = true
	}
}

// Matches reports whether the index was built for this host and this user.
func (ix *Index) Matches(host, user string) bool {
	return ix != nil && ix.Host == host && ix.User == user
}

// Len is the number of entries.
func (ix *Index) Len() int {
	if ix == nil {
		return 0
	}
	return len(ix.Entries)
}

// Skipped is the number of namespaces whose content is unknown.
func (ix *Index) Skipped() int {
	if ix == nil {
		return 0
	}
	return len(ix.Unlisted)
}

// All returns every entry, in index order. The slice is the index's own and
// must not be written to.
func (ix *Index) All() []Entry {
	if ix == nil {
		return nil
	}
	return ix.Entries
}

// Lookup finds an entry by path.
func (ix *Index) Lookup(path string) (Entry, bool) {
	if ix == nil {
		return Entry{}, false
	}
	i, ok := ix.byPath[path]
	if !ok {
		return Entry{}, false
	}
	return ix.Entries[i], true
}

// Children lists what a namespace directly contains, empty parent meaning the
// roots. The second result says whether the index knows: false for a nil
// index, for a path that is not a namespace in it, and for a namespace whose
// listing failed. A known empty namespace answers (nil, true).
func (ix *Index) Children(parent string) ([]Entry, bool) {
	if ix == nil {
		return nil, false
	}
	if parent != "" {
		e, ok := ix.Lookup(parent)
		if !ok || e.Kind != KindNamespace || ix.unlisted[parent] {
			return nil, false
		}
	}
	idx := ix.children[parent]
	out := make([]Entry, len(idx))
	for i, j := range idx {
		out[i] = ix.Entries[j]
	}
	return out, true
}

// Ancestors returns the namespaces from the root down to the one directly
// holding path, following Parent. It stops short rather than looping on a
// parent the index does not hold, and the second result says whether the
// chain reached a root.
func (ix *Index) Ancestors(path string) ([]Entry, bool) {
	e, ok := ix.Lookup(path)
	if !ok {
		return nil, false
	}
	var chain []Entry
	seen := map[string]bool{path: true}
	for e.Parent != "" {
		if seen[e.Parent] {
			return nil, false
		}
		seen[e.Parent] = true
		parent, ok := ix.Lookup(e.Parent)
		if !ok {
			return nil, false
		}
		chain = append(chain, parent)
		e = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, true
}

// With returns a copy holding entry, replacing one of the same path. A new
// entry goes last in its level — where the explorer puts a created row.
func (ix *Index) With(entry Entry) *Index {
	if ix == nil {
		return nil
	}
	entries := make([]Entry, 0, len(ix.Entries)+1)
	replaced := false
	for _, e := range ix.Entries {
		if e.Path == entry.Path {
			entries = append(entries, entry)
			replaced = true
			continue
		}
		entries = append(entries, e)
	}
	if !replaced {
		entries = append(entries, entry)
	}
	return New(ix.Host, ix.User, ix.BuiltAt, entries, ix.Unlisted)
}

// Without returns a copy with path and everything under it removed — a
// deleted namespace takes its content with it.
func (ix *Index) Without(path string) *Index {
	if ix == nil {
		return nil
	}
	gone := map[string]bool{path: true}
	// Entries are not ordered parent-first across levels, so collect the
	// subtree by walking the lookups rather than in one pass.
	queue := []string{path}
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		for _, i := range ix.children[head] {
			child := ix.Entries[i].Path
			if !gone[child] {
				gone[child] = true
				queue = append(queue, child)
			}
		}
	}
	entries := make([]Entry, 0, len(ix.Entries))
	for _, e := range ix.Entries {
		if !gone[e.Path] {
			entries = append(entries, e)
		}
	}
	unlisted := make([]string, 0, len(ix.Unlisted))
	for _, p := range ix.Unlisted {
		if !gone[p] {
			unlisted = append(unlisted, p)
		}
	}
	return New(ix.Host, ix.User, ix.BuiltAt, entries, unlisted)
}

// ReplaceLevel returns a copy whose children of parent are exactly fresh, in
// that order: what the forge answered when the level was read again. An entry
// that has gone takes its subtree with it; one that stayed keeps its subtree.
//
// It is how a level the explorer decorated corrects the index, so a repository
// deleted since the walk stops being offered by the fuzzy finder as soon as
// anyone has looked at where it was.
func (ix *Index) ReplaceLevel(parent string, fresh []Entry) *Index {
	if ix == nil {
		return nil
	}
	// A level the index has no parent for is not one it can place: the walk
	// never reached it, and grafting it on would invent the path above it.
	if _, ok := ix.Lookup(parent); parent != "" && !ok {
		return ix
	}
	keep := make(map[string]bool, len(fresh))
	for _, e := range fresh {
		keep[e.Path] = true
	}
	next := ix
	for _, e := range ix.childrenOf(parent) {
		if !keep[e.Path] {
			next = next.Without(e.Path)
		}
	}

	// Rebuild with the level in the order the forge gave it, every other
	// entry where it was.
	entries := make([]Entry, 0, len(next.Entries)+len(fresh))
	for _, e := range next.Entries {
		if e.Parent != parent {
			entries = append(entries, e)
		}
	}
	for _, e := range fresh {
		e.Parent = parent
		entries = append(entries, e)
	}
	unlisted := make([]string, 0, len(next.Unlisted))
	for _, p := range next.Unlisted {
		if p != parent {
			unlisted = append(unlisted, p)
		}
	}
	return New(next.Host, next.User, next.BuiltAt, entries, unlisted)
}

// childrenOf is Children without the "is it known" answer.
func (ix *Index) childrenOf(parent string) []Entry {
	idx := ix.children[parent]
	out := make([]Entry, len(idx))
	for i, j := range idx {
		out[i] = ix.Entries[j]
	}
	return out
}
