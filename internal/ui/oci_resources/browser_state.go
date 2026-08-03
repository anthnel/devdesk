package ociresources

import (
	"time"

	"github.com/anthnel/devdesk/internal/cache"
)

// AddRegistryTags incorporates results from one registry into the combined tag list.
// Always decrements pendingSearches, even on error (tags will be nil).
func (b *RegistryBrowser) AddRegistryTags(registryURL, alias, repo string, tags []string) {
	// Floored: a duplicate or late response would otherwise drive the counter
	// negative, and the next search would start from that base — IsSearching()
	// would stay false while requests were genuinely in flight, so the spinner
	// never showed.
	if b.pendingSearches > 0 {
		b.pendingSearches--
	}
	for _, tag := range tags {
		b.tags = append(b.tags, MultiRegistryTag{
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

// SetMultiTagsMeta stores last-updated metadata for tags belonging to one registry.
func (b *RegistryBrowser) SetMultiTagsMeta(registryURL string, meta map[string]time.Time) {
	for i := range b.tags {
		if b.tags[i].RegistryURL != registryURL {
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

// SetTagScanning marks or clears the in-progress scan state for an image tag.
func (b *RegistryBrowser) SetTagScanning(imageName string, scanning bool) {
	if scanning {
		b.scanningTags[imageName] = true
	} else {
		delete(b.scanningTags, imageName)
	}
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

// SetOperationError returns to tags state (error shown in model footer).
func (b *RegistryBrowser) SetOperationError(_ string) {
	b.state = browserStateTags
	b.imageName = ""
	b.operation = ""
}

// SetOperationSuccess clears the operation and returns to the tags screen.
func (b *RegistryBrowser) SetOperationSuccess() {
	b.state = browserStateTags
	b.imageName = ""
	b.operation = ""
}

// OperationImageName returns the image currently being operated on.
func (b *RegistryBrowser) OperationImageName() string {
	return b.imageName
}

// selectedImageName returns the full image reference for the focused table row.
func (b *RegistryBrowser) selectedImageName() string {
	t := b.getSelectedMultiTag()
	if t == nil {
		return ""
	}
	return multiImageName(t.RegistryURL, t.Repo, t.Tag)
}

// getSelectedMultiTag returns the MultiRegistryTag under the cursor, or nil.
func (b *RegistryBrowser) getSelectedMultiTag() *MultiRegistryTag {
	displayed := b.filteredSortedMultiTags()
	cursor := b.tagTable.Cursor()
	if cursor < 0 || cursor >= len(displayed) {
		return nil
	}
	return &displayed[cursor]
}
