package cache

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// What the browser remembers between visits is which registries were
// **un**checked, not which were checked. A group discovered since the last
// visit therefore arrives selected like every other new entry, instead of
// silently sitting out of every search because it did not exist when the
// selection was saved.
//
// Kept per context, because registries are.

// BrowserSelectionCache remembers the registries excluded from the browser
// search, per configuration context.
type BrowserSelectionCache struct {
	mu       sync.RWMutex
	path     string
	excluded map[string][]string // context name → entry URLs left unchecked
}

// NewBrowserSelectionCache creates or loads the cache from
// ~/.devdesk/cache/browser-selection.json
func NewBrowserSelectionCache() (*BrowserSelectionCache, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(homeDir, ".devdesk", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	c := &BrowserSelectionCache{
		path:     filepath.Join(cacheDir, "browser-selection.json"),
		excluded: make(map[string][]string),
	}

	c.load()
	return c, nil
}

func (c *BrowserSelectionCache) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &c.excluded); err != nil {
		log.Printf("ERROR [cache/browser_selection] unmarshal cache file %s: %v", c.path, err)
	}
}

func (c *BrowserSelectionCache) save() error {
	data, err := json.MarshalIndent(c.excluded, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// Deselected returns the entry URLs left unchecked in a context, as a set.
func (c *BrowserSelectionCache) Deselected(context string) map[string]bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]bool, len(c.excluded[context]))
	for _, url := range c.excluded[context] {
		out[url] = true
	}
	return out
}

// SetDeselected records the entry URLs left unchecked in a context. An empty
// list removes the context rather than storing one, so a file that nobody has
// excluded anything in stays empty.
func (c *BrowserSelectionCache) SetDeselected(context string, urls []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(urls) == 0 {
		delete(c.excluded, context)
	} else {
		c.excluded[context] = urls
	}
	return c.save()
}
