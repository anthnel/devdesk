package fakekeyring

import (
	"fmt"
	"maps"
	"strconv"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/credentials"
)

// Store is an in-memory credentials.HostStore, safe for concurrent use. A store
// built by NewUnreachableStore fails every operation instead of holding entries.
type Store struct {
	mu      sync.Mutex
	entries map[string]string
	err     error // returned by every operation when set
}

// NewStore returns an empty, reachable store.
func NewStore() *Store {
	return &Store{entries: map[string]string{}}
}

// NewUnreachableStore returns a store whose Get, Set and Delete all return err
// as given: a locked keyring, no session bus. err is never wrapped in
// credentials.ErrNotFound, so KeyringAvailable sees the store as unreachable.
func NewUnreachableStore(err error) *Store {
	s := NewStore()
	s.err = err
	return s
}

// entryKey joins service and account without ambiguity: quoting escapes any
// separator-looking byte, so two different pairs never share a key.
func entryKey(service, account string) string {
	return strconv.Quote(service) + " " + strconv.Quote(account)
}

// Get returns the secret, or an error for which errors.Is(err,
// credentials.ErrNotFound) when there is none.
func (s *Store) Get(service, account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	secret, ok := s.entries[entryKey(service, account)]
	if !ok {
		return "", fmt.Errorf("fakekeyring: %w", credentials.ErrNotFound)
	}
	return secret, nil
}

// Set stores secret, replacing any previous one.
func (s *Store) Set(service, account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.entries[entryKey(service, account)] = secret
	return nil
}

// Delete removes the entry. A missing entry is not an error, as in the
// production adapter.
func (s *Store) Delete(service, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	delete(s.entries, entryKey(service, account))
	return nil
}

// Entries returns a copy of what the store holds, keyed by an opaque string.
func (s *Store) Entries() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.entries)
}

// Install puts a new empty store in use for the whole test binary and returns
// it. It is never restored: call it from TestMain.
func Install() *Store {
	s := NewStore()
	credentials.SwapHostStoreForTest(s)
	return s
}

// Fresh puts a new empty store in use for the test and restores the previous
// one at cleanup, so nested calls unwind in order.
func Fresh(t testing.TB) *Store {
	t.Helper()
	return use(t, NewStore())
}

// FreshUnreachable is Fresh with a store that fails every operation with err.
func FreshUnreachable(t testing.TB, err error) *Store {
	t.Helper()
	return use(t, NewUnreachableStore(err))
}

func use(t testing.TB, s *Store) *Store {
	t.Helper()
	prev := credentials.SwapHostStoreForTest(s)
	t.Cleanup(func() { credentials.SwapHostStoreForTest(prev) })
	return s
}
