package template

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// fileName is the catalog's name under ~/.devdesk.
//
// It is global, not per context: a Spring Boot template is as useful to a
// GitHub context as to a GitLab one. Only entries discovered in a registry
// depend on the active context, and those are not stored here.
const fileName = "templates.yaml"

// DefaultPath is ~/.devdesk/templates.yaml.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".devdesk", fileName), nil
}

// ErrNotFound is returned when a slug is not in the catalog.
var ErrNotFound = errors.New("template not found")

// Store is the catalog file. It is shared between the Cmd goroutines of every
// view, hence the lock.
type Store struct {
	mu      sync.RWMutex
	path    string
	entries []Entry
}

// catalogFile is the on-disk shape. A wrapping key leaves room to add settings
// beside the list without breaking older files.
type catalogFile struct {
	Templates []Entry `yaml:"templates"`
}

// Open loads the catalog at path. A file that does not exist is an empty
// catalog, not an error: nobody has declared a template yet.
func Open(path string) (*Store, error) {
	s := &Store{path: path}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}

	var file catalogFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	for _, e := range file.Templates {
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	s.entries = file.Templates
	return s, nil
}

// List returns the declared entries, sorted by name. The slice is a copy.
func (s *Store) List() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the entry with the given slug.
func (s *Store) Get(slug string) (Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, e := range s.entries {
		if e.Slug == slug {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

// Put adds an entry, or replaces the one with the same slug, and saves.
// Tags are normalized on the way in.
func (s *Store) Put(e Entry) error {
	e.Tags = NormalizeTags(e.Tags)
	e.Discovered = false
	if err := e.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	next := append([]Entry(nil), s.entries...)
	replaced := false
	for i := range next {
		if next[i].Slug == e.Slug {
			next[i], replaced = e, true
		}
	}
	if !replaced {
		next = append(next, e)
	}
	return s.commit(next)
}

// Delete removes the entry with the given slug and saves.
func (s *Store) Delete(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		if e.Slug != slug {
			next = append(next, e)
		}
	}
	if len(next) == len(s.entries) {
		return ErrNotFound
	}
	return s.commit(next)
}

// commit writes next and, only if that worked, makes it the current list — so
// a failed save leaves memory and disk agreeing.
//
// The write goes to a sibling file and is renamed over the catalog, so a crash
// mid-write cannot leave a half-written catalog behind.
func (s *Store) commit(next []Entry) error {
	raw, err := yaml.Marshal(catalogFile{Templates: next})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), fileName+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return err
	}
	s.entries = next
	return nil
}
