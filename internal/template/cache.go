package template

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Cache keeps what a template's source last returned, so a preview, a scan and
// a repository creation do not each go back to the network.
//
// One file per slug, holding the source it answered for and the moment it was
// read. A template edited to point elsewhere therefore misses instead of
// answering with the old content, and a file that cannot be read back is a miss
// too: the cache is an optimisation, and no state of it may keep a template from
// being used.
//
// The cache never notices that the source moved on. A branch, or a local
// repository that gained a commit, is served as it was until Sync reads it
// again — that is what `F` in the templates view is for, and a template pinned
// to a SHA is unaffected.
type Cache struct {
	dir string
}

// CacheDir is ~/.devdesk/cache/templates.
func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".devdesk", "cache", "templates"), nil
}

// NewCache is the cache under CacheDir. A home directory that cannot be found
// yields a cache that stores nothing and always fetches.
func NewCache() Cache {
	dir, err := CacheDir()
	if err != nil {
		log.Printf("ERROR [template] cache directory: %v", err)
		return Cache{}
	}
	return Cache{dir: dir}
}

// NewCacheAt is a cache under dir, for tests.
func NewCacheAt(dir string) Cache { return Cache{dir: dir} }

// record is what is written: the source it answers for, and the files.
type record struct {
	Source    Source    `json:"source"`
	FetchedAt time.Time `json:"fetched_at"`
	Files     []File    `json:"files"`
}

// Fetch returns the cached files when they were read from this exact source,
// and otherwise reads the source and keeps the result.
func (c Cache) Fetch(ctx context.Context, slug string, src Source, creds Credentials) ([]File, error) {
	if rec, ok := c.load(slug, src); ok {
		return rec.Files, nil
	}
	return c.Sync(ctx, slug, src, creds)
}

// Sync reads the source again and replaces whatever was cached, whether or not
// it was current. A failed read leaves the previous copy where it was: a
// template that worked yesterday is not lost to a network outage today.
func (c Cache) Sync(ctx context.Context, slug string, src Source, creds Credentials) ([]File, error) {
	files, err := Fetch(ctx, src, creds)
	if err != nil {
		return nil, err
	}
	if err := c.store(slug, record{Source: src, FetchedAt: time.Now(), Files: files}); err != nil {
		// The files were read and are returned; only the next call pays again.
		log.Printf("ERROR [template] cache %s: %v", slug, err)
	}
	return files, nil
}

// FetchedAt is when the cached copy of a template's source was read, or false
// when there is none for that source.
func (c Cache) FetchedAt(slug string, src Source) (time.Time, bool) {
	rec, ok := c.load(slug, src)
	return rec.FetchedAt, ok
}

// Forget drops a template's cached copy. A template that was never cached is
// not an error.
func (c Cache) Forget(slug string) error {
	path, err := c.path(slug)
	if err != nil || path == "" {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// path is the file for a slug, validated: a slug is joined into a path, so it
// is checked here rather than trusted. An empty path means there is no cache.
func (c Cache) path(slug string) (string, error) {
	if c.dir == "" {
		return "", nil
	}
	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("slug %q must be lowercase letters, digits and dashes", slug)
	}
	return filepath.Join(c.dir, slug+".json"), nil
}

// load reads a record back and answers only for the source it was made for.
// Anything else — no file, unreadable JSON, another source, content over the
// limits a fetch enforces — is a miss.
func (c Cache) load(slug string, src Source) (record, bool) {
	path, err := c.path(slug)
	if err != nil || path == "" {
		return record{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return record{}, false
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil {
		log.Printf("ERROR [template] cache %s unreadable, refetching: %v", slug, err)
		return record{}, false
	}
	if rec.Source != src || checkLimits(rec.Files) != nil {
		return record{}, false
	}
	return rec, true
}

// store writes a record through a temporary file and a rename, so a crash or a
// second writer never leaves half of one behind.
func (c Cache) store(slug string, rec record) error {
	path, err := c.path(slug)
	if err != nil || path == "" {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, slug+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
