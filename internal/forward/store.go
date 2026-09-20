package forward

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// storeVersion is written into the file so that one produced by a later build
// can be told apart from one this build understands.
const storeVersion = 1

// FileName is the name of the file, inside the configuration directory.
const FileName = "forwards.yaml"

// ErrUnreadable is a forwards file that exists but cannot be parsed. Load
// refuses it rather than treating it as empty: the next Save would otherwise
// replace what the user could still repair by hand with a list that is missing
// everything they had.
var ErrUnreadable = errors.New("the forwards file cannot be read")

// Entry is what the file keeps of one forward: the intent, not the state. A
// forward that was live and one that could not be bound at the last launch are
// the same entry — both are retried at the next.
type Entry struct {
	// LocalPort is a TCP forward's port. A named route has none — it is served
	// on network.proxy_port — so it is left out of the file.
	LocalPort int `yaml:"local_port,omitempty"`
	// Name makes the entry a route (§3.74): the type is deduced from it rather
	// than stored, so there is no state where a route has no name.
	Name   string `yaml:"name,omitempty"`
	Target string `yaml:"target"`
	// Paused is the one thing the user decided about an entry beyond having it:
	// the route is kept and its port is left alone.
	Paused bool `yaml:"paused,omitempty"`
}

type storeFile struct {
	Version  int     `yaml:"version"`
	Forwards []Entry `yaml:"forwards"`
}

// Store reads and writes the forwards file.
//
// It is separate from Registry on purpose: the registry describes what is
// wanted (Entries) and knows nothing of a path, the router decides when to
// write, and a test points a Store at a temporary directory without the
// registry being any the wiser.
//
// Saves are serialised. Two of them can be in flight, because each one is a Cmd
// launched from Update, and an unserialised pair could land out of order and
// leave the older list on disk.
type Store struct {
	path string

	mu sync.Mutex
	// unreadable is set when Load found a file it could not parse, so that the
	// next Save moves it aside instead of overwriting it.
	unreadable bool
}

// NewStore returns a store for the file at path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// StorePath is where the file lives for a configuration directory.
func StorePath(configDir string) string {
	return filepath.Join(configDir, FileName)
}

// Path is the file the store reads and writes.
func (s *Store) Path() string { return s.path }

// Load returns the saved entries. A missing file is an empty list — the state
// of every installation before its first forward — and only an unparseable one
// is an error.
func (s *Store) Load() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}

	var f storeFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		s.unreadable = true
		return nil, fmt.Errorf("%w: %s: %v", ErrUnreadable, s.path, err)
	}
	s.unreadable = false
	return f.Forwards, nil
}

// Save writes the entries. The write is atomic — a temporary file in the same
// directory, flushed, then renamed over the target — so a crash mid-write leaves
// the previous list rather than half of a new one.
//
// A file Load could not parse is renamed to <path>.unreadable first, once, so
// that persisting again does not destroy it.
func (s *Store) Save(entries []Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(s.path), err)
	}

	if s.unreadable {
		if err := os.Rename(s.path, s.path+".unreadable"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("move the unreadable file aside: %w", err)
		}
		s.unreadable = false
	}

	data, err := yaml.Marshal(storeFile{Version: storeVersion, Forwards: entries})
	if err != nil {
		return fmt.Errorf("encode forwards: %w", err)
	}
	return writeFileAtomic(s.path, data)
}

// writeFileAtomic replaces path with data without ever exposing a partial file.
func writeFileAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("flush %s: %w", tmp.Name(), err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp.Name(), err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
