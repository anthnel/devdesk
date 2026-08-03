// Package credentials stores the secrets DevDesk holds — forge tokens, registry
// passwords — in the host's secret manager, and never in a file DevDesk writes
// itself. Select resolves which backend a context gets.
package credentials

import (
	"fmt"
	"sync"
)

// Storage interface pour stocker/récupérer les credentials
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

// NewMemoryStorage crée un nouveau MemoryStorage
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		creds: make(map[string]string),
	}
}

// Save sauvegarde un token en mémoire
func (m *MemoryStorage) Save(url, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds[url] = token
	return nil
}

// Load charge un token depuis la mémoire
func (m *MemoryStorage) Load(url string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	token, ok := m.creds[url]
	if !ok {
		return "", fmt.Errorf("no credentials found for %s", url)
	}
	return token, nil
}

// Delete supprime un token de la mémoire
func (m *MemoryStorage) Delete(url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.creds, url)
	return nil
}
