// Package credentials stores the secrets DevDesk holds — forge tokens, registry
// passwords — in the host's secret manager, and never in a file DevDesk writes
// itself. Select resolves which backend a context gets.
package credentials

import (
	"errors"
	"fmt"
	"sync"
)

// ErrNotFound says a store holds nothing under that key. Every Load wraps it,
// and the distinction is not pedantry: a caller that cannot tell "never stored"
// from "the store would not answer" will mint a replacement over a secret that
// is still there, because a locked keyring and an empty one look the same. It
// is what internal/mcp.ResolveToken checks before generating a bearer token.
var ErrNotFound = errors.New("no credentials found")

// Storage interface for storing/retrieving credentials
type Storage interface {
	Save(url, token string) error
	Load(url string) (token string, err error)
	Delete(url string) error
}

// MemoryStorage keeps secrets for this session only. It is what Select falls
// back to when no host store answers, so it has to be worse than a file on
// purpose: a fallback that silently persists is how the old "secure" option
// came to write plaintext (§3.9).
//
// It is shared between the Cmd goroutines of every view, hence the lock.
type MemoryStorage struct {
	mu    sync.RWMutex
	creds map[string]string
}

// NewMemoryStorage creates a new MemoryStorage
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		creds: make(map[string]string),
	}
}

// Save stores a token in memory
func (m *MemoryStorage) Save(url, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds[url] = token
	return nil
}

// Load loads a token from memory
func (m *MemoryStorage) Load(url string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	token, ok := m.creds[url]
	if !ok {
		return "", fmt.Errorf("%w for %s", ErrNotFound, url)
	}
	return token, nil
}

// Delete removes a token from memory
func (m *MemoryStorage) Delete(url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.creds, url)
	return nil
}
