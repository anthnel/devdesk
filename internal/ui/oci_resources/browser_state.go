package ociresources

import (
	"time"

	"github.com/anthnel/devdesk/internal/cache"
)

// AddRegistryTags incorporates results from one registry into the combined tag list.
// Always decrements pendingSearches, even on error (tags will be nil).
func (b *RegistryBrowser) AddRegistryTags(entryKey, registryURL, alias, repo string, tags []string) {
	// Floored: a duplicate or late response would otherwise drive the counter
	// negative, and the next search would start from that base — IsSearching()
	// would stay false while requests were genuinely in flight, so the spinner
	// never showed.
	if b.pendingSearches > 0 {
		b.pendingSearches--
	}
	for _, tag := range tags {
		b.tags = append(b.tags, MultiRegistryTag{
			EntryKey:    entryKey,
			RegistryURL: registryURL,
			Alias:       alias,
			Repo:        repo,
			Tag:         tag,
		})
	}
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// SetMultiTagsMeta stores last-updated metadata for tags belonging to one entry.
//
// Keyed on the entry, not the host: several entries share one host once a
// registry is a host plus a repo prefix, so a URL would stamp one proxy's dates
// onto every proxy of that instance (§3.18).
func (b *RegistryBrowser) SetMultiTagsMeta(entryKey string, meta map[string]time.Time) {
	for i := range b.tags {
		if b.tags[i].EntryKey != entryKey {
			continue
		}
		if t, ok := meta[b.tags[i].Tag]; ok {
			b.tags[i].UpdatedAt = t
		}
	}
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// SetTagScanningSet replaces the in-progress scans the tag rows decorate.
//
// A set rather than one name at a time: the source is the registry snapshot,
// which says what is running rather than what changed, and reconciling a whole
// answer against per-name edits is how the two drift.
func (b *RegistryBrowser) SetTagScanningSet(scanning map[string]bool) {
	b.scanningTags = scanning
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// HasSelectedTagScanResults returns true when the selected tag has cached scan results.
func (b *RegistryBrowser) HasSelectedTagScanResults() bool {
	name := b.selectedImageName()
	if name == "" {
		return false
	}
	_, ok := b.scanCache[name]
	return ok
}

// SetScanCache stores the model's scan cache for CVE column display.
func (b *RegistryBrowser) SetScanCache(sc map[string]cache.ImageScanEntry) {
	b.scanCache = sc
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// SetOperationError, SetOperationSuccess and OperationImageName went with the
// status screen (§3.60): the browser is closed by the time a pull answers, and
// the answer goes to the footer and the Images list.

// selectedImageName returns the full image reference for the focused table row.
func (b *RegistryBrowser) selectedImageName() string {
	t := b.getSelectedMultiTag()
	if t == nil {
		return ""
	}
	return multiImageName(t.RegistryURL, t.Repo, t.Tag)
}

// getSelectedMultiTag returns the MultiRegistryTag under the cursor, or nil.
//
// It used to replay the filter and the sort to map the cursor back to a tag,
// with nothing tying that ordering to the one the rows were built from. The
// table keeps the ordered slice it drew, so the two cannot disagree.
func (b *RegistryBrowser) getSelectedMultiTag() *MultiRegistryTag {
	row, ok := b.tagTable.Selected()
	if !ok {
		return nil
	}
	return &row.tag
}
