package dashboard

import (
	"log"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
)

// posture is what the scan caches say about the current context, without
// running anything: this is what connects the dashboard to §3.11's inventory
// without running a single scan (Rule 126).
type posture struct {
	// Read tells "the caches were consulted" apart from "there is nothing in
	// them" — without it, an empty cache and a never-read cache display the
	// same zero, and one of the two would be a lie.
	Read bool

	// The two caches are kept separate because they don't get fixed the same
	// way: a CRITICAL in an image is fixed by changing tag, in a repository
	// by changing code. Summing them gave a total nobody could act on.
	Images       postureSide
	Repositories postureSide
}

// postureSide is one family's tally.
//
// The HIGH count left along with its display: the box now shows how many
// targets have never been scanned, which is actionable (run a scan) where
// one more HIGH wasn't. A field nobody reads anymore reads like data someone
// forgot to display.
type postureSide struct {
	Targets  int
	Critical int
	// Secrets counts the **targets** that carry any, not the secrets: two
	// repositories are two decisions, forty leaks in the same one are only one.
	Secrets int
	// SecretsKnown counts the ones whose verdict is known. Without it, a
	// whole inventory scanned with no secrets step would display `0`, i.e.
	// "no target carries any", for a set nobody has looked at.
	SecretsKnown int
	// Oldest is the age of the least recently scanned target: this is the
	// useful question, because a CRITICAL count three weeks old is a count
	// on code that no longer exists.
	Oldest time.Time
}

// Total folds both families, for the tiers too narrow to show them apart.
func (p posture) Total() postureSide {
	total := p.Images
	total.Targets += p.Repositories.Targets
	total.Critical += p.Repositories.Critical
	total.Secrets += p.Repositories.Secrets
	total.SecretsKnown += p.Repositories.SecretsKnown
	if o := p.Repositories.Oldest; !o.IsZero() && (total.Oldest.IsZero() || o.Before(total.Oldest)) {
		total.Oldest = o
	}
	return total
}

// readPosture sums both caches for one context. An unreadable cache is not
// an empty posture: it yields Read=false, hence `-` and not `0`.
//
// Vanished repositories are discarded, and it's the same rule the inventory
// applies to the same reading (cache.RepositoryGone). Without it the
// dashboard counted the CRITICALs of repositories deleted since their scan:
// `ws` didn't list them — it lists the disk — and `:sec` already discarded
// them, so the only screen announcing them was the only one where you
// couldn't go look. A count that no view can break down is not a count, it's
// a dead end.
//
// Images are too, and that's what explains the signature. The daemon's
// enumeration is done by the Cmd and passed in here (Rule 110: the Cmd does
// the I/O), whereas `os.Stat` stays inside: reading a path is the same kind
// of thing as reading the two cache files, querying a service that may be
// stopped is not. `known` carries this difference — a stopped daemon keeps
// all entries, otherwise the Images box would display `0 CRITICAL`, i.e. the
// one wrong answer nobody would go check.
func readPosture(context string, images map[string]struct{}, imagesKnown bool) posture {
	imageCache, errImages := cache.NewImageScanCache()
	workspaces, errWorkspaces := cache.NewWorkspaceScanCache(context)
	if errImages != nil || errWorkspaces != nil {
		log.Printf("ERROR [dashboard] reading the scan caches: %v / %v", errImages, errWorkspaces)
		return posture{}
	}

	p := posture{Read: true}
	for name, entry := range imageCache.GetAll() {
		if cache.ImageGone(name, images, imagesKnown) {
			continue
		}
		p.Images.add(entry.Critical, entry.Sensitive, entry.ScannedAt)
	}
	for path, entry := range workspaces.GetAll() {
		if cache.RepositoryGone(path) {
			continue
		}
		p.Repositories.add(entry.Critical, entry.Sensitive, entry.ScannedAt)
	}
	return p
}

// add folds one scanned target into a family's tally.
//
// `sensitive` is the verdict as the cache carries it: nil when no step
// looked for a secret on that target, which leaves it out of the count on
// both sides — neither carrying, nor cleared.
func (p *postureSide) add(critical int, sensitive *bool, scannedAt time.Time) {
	p.Targets++
	p.Critical += critical

	if sensitive != nil {
		p.SecretsKnown++
		if *sensitive {
			p.Secrets++
		}
	}

	// An entry with no timestamp does not make the posture younger: the zero
	// value of a time.Time would be the oldest of all and would make an
	// inventory read "never scanned" when it isn't.
	if scannedAt.IsZero() {
		return
	}
	if p.Oldest.IsZero() || scannedAt.Before(p.Oldest) {
		p.Oldest = scannedAt
	}
}

// ── Coverage ─────────────────────────────────────────────────────────────────
//
// What the caches know is what has been scanned. How much is left requires
// the other half — the inventory — and that one is already in the model:
// `docker system df` counts the images, `fetchWorkspaceStats` the
// repositories. The subtraction is therefore done here, not in readPosture,
// which only reads cache files and has no reason to go query Docker.
//
// All three return `(n, measured)` rather than a plain integer: without the
// second value, an inventory not yet loaded would yield a zero, and "nothing
// to scan" is exactly the opposite of "we don't know yet".

// unscannedImages counts the local images that carry no cache entry.
func (m Model) unscannedImages() (int, bool) {
	if !m.posture.Read || m.loadingOCI || m.ociStats == nil || !m.ociStats.Available {
		return 0, false
	}
	return uncovered(m.ociStats.ImagesCount, m.posture.Images.Targets), true
}

// unscannedRepositories counts the workspaces that carry no cache entry.
func (m Model) unscannedRepositories() (int, bool) {
	if !m.posture.Read || m.loadingWorkspaces {
		return 0, false
	}
	return uncovered(m.workspaceCount, m.posture.Repositories.Targets), true
}

// unscannedTotal folds both, for the tiers too narrow to show them apart. A
// single missing half is enough to make the total unmeasured: the sum of a
// number and an unknown is an unknown.
func (m Model) unscannedTotal() (int, bool) {
	images, okImages := m.unscannedImages()
	repositories, okRepositories := m.unscannedRepositories()
	if !okImages || !okRepositories {
		return 0, false
	}
	return images + repositories, true
}

// uncovered is the inventory minus what has been scanned, floored at zero:
// the cache keeps the entry of an image deleted since, so the difference can
// go below zero — and "there are fewer than zero left to scan" isn't a
// sentence. The floor says what should be taken away from it: nothing left
// to scan.
//
// Both families now discard what has vanished, so `Targets` no longer
// exceeds the inventory in the ordinary case — and this wasn't just a
// negative subtraction caught after the fact: a deleted target exactly
// offset a never-scanned target, and the box announced `0 unscanned` for a
// set that still had some left.
//
// The floor stays, defensively: `docker system df` and `docker image ls`
// don't count exactly the same set (intermediates, dangling), so the
// inventory and the reconciled entries can cross by one unit.
func uncovered(inventory, scanned int) int {
	return max(inventory-scanned, 0)
}
