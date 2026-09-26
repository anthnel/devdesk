package forgeindex

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/anthnel/devdesk/internal/forge"
)

// walkConcurrency bounds the listings in flight. The walk is one request (or
// one page series) per namespace, and a burst of fifty at session start is
// what a rate limiter is there to refuse.
const walkConcurrency = 4

// Build walks everything the session can see and returns it as an index.
//
// Nothing is decorated — see the package doc — and archived repositories are
// listed, because the explorer shows them. A root listing that fails is an
// error: there is nothing to return. A namespace whose own listing fails is
// kept and marked Unlisted instead, so one unreadable group costs its own
// content rather than the whole index (the same reasoning as D59: a silently
// incomplete result is worse than a slow one, and an explicitly incomplete one
// is better than none).
//
// A cancelled context stops the walk and returns its error.
func Build(ctx context.Context, backend forge.Forge, host, user string, now time.Time) (*Index, error) {
	opts := forge.BrowseOptions{IncludeArchived: true}
	roots, err := backend.RootNamespaces(ctx, opts)
	if err != nil {
		return nil, err
	}

	w := &walker{
		ctx:     ctx,
		backend: backend,
		opts:    opts,
		sem:     make(chan struct{}, walkConcurrency),
	}
	rootEntries := make([]Entry, len(roots))
	for i, ns := range roots {
		rootEntries[i] = FromNamespace(ns, "")
	}
	w.add(rootEntries)
	for _, e := range rootEntries {
		w.visit(e.ID, e.Path)
	}
	w.wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Failures land in whatever order the goroutines finished; sorted, the
	// file does not change between two walks that saw the same forge.
	sort.Strings(w.unlisted)
	return New(host, user, now, w.entries, w.unlisted), nil
}

// walker is the state of one Build. Each level is appended as one block, under
// the lock, so a level keeps the forge's order however the goroutines
// interleave.
type walker struct {
	ctx     context.Context
	backend forge.Forge
	opts    forge.BrowseOptions
	sem     chan struct{}
	wg      sync.WaitGroup

	mu       sync.Mutex
	entries  []Entry
	unlisted []string
}

// visit lists one namespace in the background and recurses into what it
// holds. The semaphore is held around the request only: holding it across the
// recursion would let four deep branches starve every other one.
func (w *walker) visit(id, path string) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()

		select {
		case w.sem <- struct{}{}:
		case <-w.ctx.Done():
			return
		}
		children, err := w.backend.Children(w.ctx, id, w.opts)
		<-w.sem

		if err != nil {
			if w.ctx.Err() == nil {
				w.mu.Lock()
				w.unlisted = append(w.unlisted, path)
				w.mu.Unlock()
			}
			return
		}

		level := make([]Entry, 0, len(children.Namespaces)+len(children.Repositories))
		for _, ns := range children.Namespaces {
			level = append(level, FromNamespace(ns, path))
		}
		for _, repo := range children.Repositories {
			level = append(level, FromRepository(repo, path))
		}
		w.add(level)
		for _, ns := range children.Namespaces {
			w.visit(ns.ID, ns.Path)
		}
	}()
}

func (w *walker) add(level []Entry) {
	w.mu.Lock()
	w.entries = append(w.entries, level...)
	w.mu.Unlock()
}

// FromNamespace converts what the forge answered for a namespace.
func FromNamespace(ns forge.Namespace, parent string) Entry {
	return Entry{
		ID:         ns.ID,
		Path:       ns.Path,
		Name:       ns.Name,
		Parent:     parent,
		Kind:       KindNamespace,
		Visibility: ns.Visibility,
		CreatedAt:  ns.CreatedAt,
		WebURL:     ns.WebURL,
	}
}

// FromRepository converts what the forge answered for a repository.
func FromRepository(repo forge.Repository, parent string) Entry {
	return Entry{
		ID:                repo.ID,
		Path:              repo.Path,
		Name:              repo.Name,
		Parent:            parent,
		Kind:              KindRepository,
		Visibility:        repo.Visibility,
		CreatedAt:         repo.CreatedAt,
		LastActivityAt:    repo.LastActivityAt,
		WebURL:            repo.WebURL,
		DeletionScheduled: repo.DeletionScheduled,
	}
}
