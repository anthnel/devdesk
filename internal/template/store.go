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

	// rejected are the entries of the file that cannot be used, kept as they
	// were so that saving another entry does not silently delete a hand-edited
	// one, each with the reason it was left out.
	rejected []rejectedEntry
}

type rejectedEntry struct {
	entry Entry
	err   error
}

// catalogFile is the on-disk shape. A wrapping key leaves room to add settings
// beside the list without breaking older files.
type catalogFile struct {
	Templates []Entry `yaml:"templates"`
}

// Open loads the catalog at path. A file that does not exist is an empty
// catalog, not an error: nobody has declared a template yet. An entry that
// fails validation is set aside (see Problems) rather than failing the whole
// catalog: one bad line in a hand-edited file must not take the others down.
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
	seen := make(map[string]bool, len(file.Templates))
	for _, e := range file.Templates {
		err := e.Validate()
		if err == nil && seen[e.Slug] {
			err = fmt.Errorf("template %q is declared twice", e.Slug)
		}
		if err != nil {
			s.rejected = append(s.rejected, rejectedEntry{e, fmt.Errorf("%s: %w", path, err)})
			continue
		}
		seen[e.Slug] = true
		s.entries = append(s.entries, e)
	}
	return s, nil
}

// Problems says why entries of the file were left out of the catalog: one that
// does not validate (a dangerous URL, a bad slug) or a slug declared twice. The
// rest of the catalog stays usable, and the rejected entries stay in the file.
func (s *Store) Problems() []error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]error, len(s.rejected))
	for i, r := range s.rejected {
		out[i] = r.err
	}
	return out
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
	return s.commit(next, s.withoutRejected(e.Slug))
}

// withoutRejected is the rejected entries but those of slug, which a new entry
// replaces.
func (s *Store) withoutRejected(slug string) []rejectedEntry {
	var out []rejectedEntry
	for _, r := range s.rejected {
		if r.entry.Slug != slug {
			out = append(out, r)
		}
	}
	return out
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
	return s.commit(next, s.rejected)
}

// commit writes next and, only if that worked, makes it the current list — so
// a failed save leaves memory and disk agreeing.
//
// The write goes to a sibling file and is renamed over the catalog, so a crash
// mid-write cannot leave a half-written catalog behind.
func (s *Store) commit(next []Entry, rejected []rejectedEntry) error {
	all := append([]Entry(nil), next...)
	for _, r := range rejected {
		all = append(all, r.entry)
	}
	raw, err := yaml.Marshal(catalogFile{Templates: all})
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
	s.rejected = rejected
	return nil
}
