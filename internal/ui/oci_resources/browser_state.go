package ociresources

import (
	"slices"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
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

// SetJobs takes the router's snapshot — what is running or remembered as
// failed, and the context it is read in — and rebuilds the rows.
//
// A snapshot rather than one name at a time: it says what is running rather
// than what changed, and reconciling a whole answer against per-name edits is
// how the two drift.
func (b *RegistryBrowser) SetJobs(runs []jobs.Run, failures []jobs.Failure, context string) {
	b.jobs = runs
	b.failures = slices.Clone(failures)
	b.contextName = context
	if b.state == browserStateTags {
		b.rebuildTagTable()
	}
}

// SetBlinkPhase hands the router's blink phase to both tables. Nothing is
// rebuilt: the table reads the phase at render.
func (b *RegistryBrowser) SetBlinkPhase(on bool) {
	b.tagTable.SetBlinkPhase(on)
	b.searchTable.SetBlinkPhase(on)
}

// WantsBlink reports whether a visible tag row has a running operation.
func (b *RegistryBrowser) WantsBlink() bool {
	if b.state != browserStateTags {
		return false
	}
	for _, r := range b.tagTable.Items() {
		if r.Activity.State == indicator.Running {
			return true
		}
	}
	return false
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
