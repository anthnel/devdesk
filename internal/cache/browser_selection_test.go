package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestSelectionCache(t *testing.T) *BrowserSelectionCache {
	t.Helper()
	return &BrowserSelectionCache{
		path:     filepath.Join(t.TempDir(), "browser-selection.json"),
		excluded: make(map[string][]string),
	}
}

func TestSelectionCache_EmptyContextExcludesNothing(t *testing.T) {
	if got := newTestSelectionCache(t).Deselected("default"); len(got) != 0 {
		t.Errorf("Deselected on an empty cache = %v", got)
	}
}

func TestSelectionCache_RoundTrip(t *testing.T) {
	c := newTestSelectionCache(t)

	if err := c.SetDeselected("work", []string{"docker.io", "nexus/dhi"}); err != nil {
		t.Fatalf("SetDeselected: %v", err)
	}

	got := c.Deselected("work")
	if !got["docker.io"] || !got["nexus/dhi"] {
		t.Errorf("Deselected = %v, want both", got)
	}
	if len(got) != 2 {
		t.Errorf("Deselected = %v, want exactly the two", got)
	}
}

// Registries are per context, so what was excluded from a search is too.
func TestSelectionCache_KeepsContextsApart(t *testing.T) {
	c := newTestSelectionCache(t)
	_ = c.SetDeselected("work", []string{"docker.io"})
	_ = c.SetDeselected("home", []string{"nexus/dhi"})

	if got := c.Deselected("work"); got["nexus/dhi"] {
		t.Errorf("work sees home's exclusions: %v", got)
	}
	if got := c.Deselected("home"); !got["nexus/dhi"] {
		t.Errorf("home lost its own: %v", got)
	}
}

// Re-checking everything must leave nothing behind, or the file grows a context
// key that says the same as its absence.
func TestSelectionCache_AnEmptyListRemovesTheContext(t *testing.T) {
	c := newTestSelectionCache(t)
	_ = c.SetDeselected("work", []string{"docker.io"})

	if err := c.SetDeselected("work", nil); err != nil {
		t.Fatalf("SetDeselected: %v", err)
	}

	if len(c.excluded) != 0 {
		t.Errorf("the cache still holds %v", c.excluded)
	}
}

func TestSelectionCache_SurvivesAReopen(t *testing.T) {
	c := newTestSelectionCache(t)
	_ = c.SetDeselected("work", []string{"docker.io"})

	reopened := &BrowserSelectionCache{path: c.path, excluded: make(map[string][]string)}
	reopened.load()

	if got := reopened.Deselected("work"); !got["docker.io"] {
		t.Errorf("Deselected = %v after reopening, want what was saved", got)
	}
}

func TestSelectionCache_LoadsNothingFromABrokenFile(t *testing.T) {
	c := newTestSelectionCache(t)
	if err := os.WriteFile(c.path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing the broken file: %v", err)
	}

	c.load()

	if len(c.excluded) != 0 {
		t.Errorf("a broken cache file yielded %v", c.excluded)
	}
}
