package trust

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// How long a verdict is reused. A digest never changes, but a signature can be
// added to it later, and keys and logs move: Unsigned is asked again sooner.
// Failed is never stored — a block under a user rule would outlast the network
// coming back — and neither is a verdict inferred from a failure (ErrUnproven).
const (
	verifiedTTL = 24 * time.Hour
	unsignedTTL = 6 * time.Hour
)

func ttl(v Verdict) (time.Duration, bool) {
	switch v {
	case Verified, IdentityMismatch:
		return verifiedTTL, true
	case Unsigned:
		return unsignedTTL, true
	}
	return 0, false
}

// Store keeps verdicts between runs.
type Store interface {
	Get(key string) (Verdict, time.Time, bool)
	Set(key string, v Verdict, at time.Time) error
}

// Cached wraps a verifier so a digest already answered under the same rule is
// not asked again. The key is the digest and the rule's fingerprint: editing
// the rule is a miss. Identities are not cached — they are hints, and cheap
// next to the strict checks they lead to, which are.
func Cached(v Verifier, s Store, now func() time.Time) Verifier {
	return cached{inner: v, store: s, now: now}
}

type cached struct {
	inner Verifier
	store Store
	now   func() time.Time
}

func (c cached) Verify(ctx context.Context, ref string, rule Rule) (Verdict, error) {
	digest := Digest(ref)
	if digest == "" {
		return c.inner.Verify(ctx, ref, rule)
	}
	key := digest + " " + rule.Fingerprint()
	if v, at, ok := c.store.Get(key); ok {
		if d, keep := ttl(v); keep && c.now().Sub(at) < d {
			return v, nil
		}
	}
	v, err := c.inner.Verify(ctx, ref, rule)
	// An error beside a storable verdict means it was inferred (ErrUnproven):
	// kept, it would block for hours after the network came back.
	if _, keep := ttl(v); keep && err == nil {
		// A verdict that could not be stored is still the answer.
		_ = c.store.Set(key, v, c.now())
	}
	return v, err
}

func (c cached) Identities(ctx context.Context, ref string) ([]Identity, error) {
	return c.inner.Identities(ctx, ref)
}

// FileStore is the Store on disk: ~/.devdesk/cache/signature-verdicts.json.
//
// It lives here rather than in internal/cache because that package imports
// internal/scan, which implements this package's Verifier.
type FileStore struct {
	// Path is the file; empty is the default location.
	Path string
}

type storeEntry struct {
	Verdict   string    `json:"verdict"`
	CheckedAt time.Time `json:"checked_at"`
}

type storeFile struct {
	Version int                   `json:"version"`
	Entries map[string]storeEntry `json:"entries"`
}

const storeVersion = 1

// storeMu serializes every read-modify-write: verifications run on several Cmds.
var storeMu sync.Mutex

func (s FileStore) path() (string, error) {
	if s.Path != "" {
		return s.Path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".devdesk", "cache", "signature-verdicts.json"), nil
}

// Get returns a stored verdict. A missing or unreadable file is a miss.
func (s FileStore) Get(key string) (Verdict, time.Time, bool) {
	path, err := s.path()
	if err != nil {
		return NoPolicy, time.Time{}, false
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	entries, err := readStore(path)
	if err != nil {
		return NoPolicy, time.Time{}, false
	}
	e, ok := entries[key]
	if !ok {
		return NoPolicy, time.Time{}, false
	}
	v := parseVerdict(e.Verdict)
	return v, e.CheckedAt, v != NoPolicy
}

// Set stores one verdict, keeping the others.
func (s FileStore) Set(key string, v Verdict, at time.Time) error {
	path, err := s.path()
	if err != nil {
		return err
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	entries, err := readStore(path)
	if err != nil {
		// A corrupt file is replaced rather than blocking every later write.
		entries = map[string]storeEntry{}
	}
	entries[key] = storeEntry{Verdict: v.String(), CheckedAt: at}
	data, err := json.MarshalIndent(storeFile{Version: storeVersion, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readStore(path string) (map[string]storeEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]storeEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if file.Entries == nil {
		file.Entries = map[string]storeEntry{}
	}
	return file.Entries, nil
}
