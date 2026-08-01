package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// NewFileStorageForContext creates a FileStorage at ~/.devdesk/credentials-<context>.json.
// Used as a reliable fallback when the system credential helper (e.g. GCM on Windows) fails.
func NewFileStorageForContext(contextName string) *FileStorage {
	if contextName == "" {
		contextName = "default"
	}
	homeDir, _ := os.UserHomeDir()
	path := filepath.Join(homeDir, ".devdesk", "credentials-"+contextName+".json")
	return NewFileStorage(path)
}

// ChainStorage tries multiple Storage implementations in order.
// Save writes to all storages (best effort). Load returns the first success. Delete removes from all.
type ChainStorage struct {
	storages []Storage
}

// NewChainStorage creates a ChainStorage that tries each storage in the given order.
func NewChainStorage(storages ...Storage) *ChainStorage {
	return &ChainStorage{storages: storages}
}

// Save saves to all storages, returning an error only if all fail.
func (c *ChainStorage) Save(url, token string) error {
	var lastErr error
	saved := false
	for _, s := range c.storages {
		if err := s.Save(url, token); err != nil {
			lastErr = err
		} else {
			saved = true
		}
	}
	if !saved {
		return lastErr
	}
	return nil
}

// Load tries each storage in order and returns the first token found.
func (c *ChainStorage) Load(url string) (string, error) {
	for _, s := range c.storages {
		token, err := s.Load(url)
		if err == nil && token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("no credentials found for %s", url)
}

// Delete removes credentials from all storages (best effort).
func (c *ChainStorage) Delete(url string) error {
	for _, s := range c.storages {
		_ = s.Delete(url)
	}
	return nil
}

// Storage interface pour stocker/récupérer les credentials
type Storage interface {
	Save(url, token string) error
	Load(url string) (token string, err error)
	Delete(url string) error
}

// FileStorage stocke les credentials dans un fichier JSON
type FileStorage struct {
	filePath string
}

// NewFileStorage crée un nouveau FileStorage
func NewFileStorage(filePath string) *FileStorage {
	return &FileStorage{filePath: filePath}
}

// Save sauvegarde un token
func (f *FileStorage) Save(url, token string) error {
	// Charger les credentials existants
	creds, err := f.loadAll()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if creds == nil {
		creds = make(map[string]string)
	}

	// Ajouter/mettre à jour le token
	creds[url] = token

	// Sauvegarder
	return f.saveAll(creds)
}

// Load charge un token
func (f *FileStorage) Load(url string) (string, error) {
	creds, err := f.loadAll()
	if err != nil {
		return "", err
	}

	token, ok := creds[url]
	if !ok {
		return "", fmt.Errorf("no credentials found for %s", url)
	}

	return token, nil
}

// Delete supprime un token
func (f *FileStorage) Delete(url string) error {
	creds, err := f.loadAll()
	if err != nil {
		return err
	}

	delete(creds, url)

	return f.saveAll(creds)
}

// loadAll charge tous les credentials
func (f *FileStorage) loadAll() (map[string]string, error) {
	data, err := os.ReadFile(f.filePath)
	if err != nil {
		return nil, err
	}

	var creds map[string]string
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}

	return creds, nil
}

// saveAll sauvegarde tous les credentials
func (f *FileStorage) saveAll(creds map[string]string) error {
	// Créer le répertoire parent si nécessaire
	dir := filepath.Dir(f.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	// Écrire avec des permissions restrictives
	return os.WriteFile(f.filePath, data, 0600)
}

// MemoryStorage stocke les credentials en mémoire (session uniquement)
type MemoryStorage struct {
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
	m.creds[url] = token
	return nil
}

// Load charge un token depuis la mémoire
func (m *MemoryStorage) Load(url string) (string, error) {
	token, ok := m.creds[url]
	if !ok {
		return "", fmt.Errorf("no credentials found for %s", url)
	}
	return token, nil
}

// Delete supprime un token de la mémoire
func (m *MemoryStorage) Delete(url string) error {
	delete(m.creds, url)
	return nil
}
