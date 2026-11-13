package imagesearch

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/url"
	"strings"
	"sync"
)

// Batch memo for catalog walks (DEVDESK-311). Members of one batch that reach
// the same catalog with the same identity share a single walk.
//
// This is a memo, not a singleflight: a key is not forgotten when the walk
// ends, so a later member of the same batch reuses the finished result. The
// memo lives only as long as the context returned by WithSharedWalks.
//
// Known trade-off: the leader runs the walk under its own context, so the
// leader's deadline or cancellation applies to the followers already waiting
// on that key: they receive the leader's context error. That error is not a
// fact about the catalog, so it is not memoized: the key is dropped and a
// member arriving later with a live context walks again. A follower that is
// cancelled itself returns at once and leaves the walk running for the others.
// A panic in the walk propagates to the leader only; everyone else gets
// errWalkAborted (a failure), never an empty success.
//
// A walk is never shared between two identities: the key includes the
// credentials. They only enter the key through an HMAC under a per-batch
// random secret, so no secret value is held or printed by the key (a %v of a
// walkKey shows nothing sensitive).

type walkShareCtxKey struct{}

// walkKey identifies one shareable walk.
type walkKey struct {
	apiURL   string
	identity [sha256.Size]byte
}

// errWalkAborted is what followers see when the leader's walk panicked.
var errWalkAborted = errors.New("catalog walk aborted")

// String keeps the identity digest and any URL userinfo out of formatted
// output. An unparsable URL is not echoed, since it may embed credentials.
func (k walkKey) String() string {
	u, err := url.Parse(k.apiURL)
	if err != nil {
		return "walkKey{<unparsable URL>}"
	}
	u.User = nil
	return "walkKey{" + u.String() + "}"
}

// GoString keeps the identity digest out of %#v output.
func (k walkKey) GoString() string { return k.String() }

// walkCall is one walk: the leader fills the results, then closes done.
type walkCall struct {
	done      chan struct{}
	names     []string // read-only once done is closed
	truncated bool
	err       error
}

// walkShare is the memo attached to a batch context.
type walkShare struct {
	secret [32]byte // per-batch HMAC key; the zero value still works
	mu     sync.Mutex
	calls  map[walkKey]*walkCall
}

// WithSharedWalks returns a context carrying a fresh walk memo. Searches run
// with it share catalog walks; without it every call walks on its own.
func WithSharedWalks(ctx context.Context) context.Context {
	s := &walkShare{}
	_, _ = rand.Read(s.secret[:]) // never fails since Go 1.24
	return context.WithValue(ctx, walkShareCtxKey{}, s)
}

// keyFor builds the memo key of a target: the API URL (trailing slash
// ignored) and an HMAC of the length-prefixed username and password, so
// "ab"+"c" and "a"+"bc" differ.
func (s *walkShare) keyFor(t Target) walkKey {
	mac := hmac.New(sha256.New, s.secret[:])
	var n [8]byte
	for _, part := range []string{t.Username, t.Password} {
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		mac.Write(n[:])
		mac.Write([]byte(part))
	}
	k := walkKey{apiURL: strings.TrimSuffix(t.APIURL, "/")}
	copy(k.identity[:], mac.Sum(nil))
	return k
}

// walk returns the shared result for t's key: the first caller walks, the
// others wait for it or for their own context.
func (s *walkShare) walk(ctx context.Context, t Target, list CatalogLister) ([]string, bool, error) {
	key := s.keyFor(t)
	s.mu.Lock()
	if call, ok := s.calls[key]; ok {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.names, call.truncated, call.err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	call := &walkCall{done: make(chan struct{})}
	if s.calls == nil {
		s.calls = map[walkKey]*walkCall{}
	}
	s.calls[key] = call
	s.mu.Unlock()

	// The walk runs outside the mutex, under the leader's own context. The
	// error is preset so a panic in list leaves a failure for everyone else;
	// the panic itself still propagates to the leader.
	call.err = errWalkAborted
	defer func() {
		if call.err != nil && ctx.Err() != nil && !errors.Is(call.err, errWalkAborted) {
			// The leader's own cancellation or deadline: do not memoize it.
			s.mu.Lock()
			if s.calls[key] == call {
				delete(s.calls, key)
			}
			s.mu.Unlock()
		}
		close(call.done)
	}()
	call.names, call.truncated, call.err = list(ctx, t.APIURL, t.Username, t.Password, CatalogMaxPages, CatalogPageSize)
	return call.names, call.truncated, call.err
}

// walkCatalog walks through the batch memo when ctx carries one, and
// directly otherwise.
func walkCatalog(ctx context.Context, t Target, list CatalogLister) ([]string, bool, error) {
	if s, ok := ctx.Value(walkShareCtxKey{}).(*walkShare); ok {
		return s.walk(ctx, t, list)
	}
	return list(ctx, t.APIURL, t.Username, t.Password, CatalogMaxPages, CatalogPageSize)
}
