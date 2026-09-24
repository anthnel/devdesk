// Package imageupdate says whether a newer image exists for a reference
// (§3.88): a newer patch tag on the same line, or — for any tag — new content
// behind the same tag, which is what a floating tag (§3.79) only ever gets.
//
// The registry's side is fetched and cached; the comparison with the local image
// is made when the answer is shown, so a pull clears it at once.
package imageupdate

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
)

const (
	// freshFor is how long a registry's answer stands. Base images are rebuilt
	// daily at most; asking more often than a few times a day buys nothing.
	freshFor = 6 * time.Hour
	// retryAfter is how long a failed check stands: long enough that a registry
	// that does not know the image — a locally built one, a private registry
	// with no credentials — is not asked on every refresh.
	retryAfter = 30 * time.Minute
	// maxParallel bounds the requests in flight: a list of forty images must not
	// open forty connections to one registry at once.
	maxParallel = 4
)

// Facts is what a registry said about one reference.
type Facts = cache.ImageUpdateEntry

// Kind is what kind of update is available.
type Kind int

const (
	// None: no update is known — up to date, not checked yet, or not checkable.
	None Kind = iota
	// NewPatch: a newer patch tag exists on the same line.
	NewPatch
	// NewBuild: the tag now points to other content than the local image.
	NewBuild
)

// Status is the verdict a view shows.
type Status struct {
	Kind Kind
	// Tag is the newer patch tag, for NewPatch.
	Tag string
}

// Available reports whether there is an update to show.
func (s Status) Available() bool { return s.Kind != None }

// Label is the text shown beside the arrow: the tag to move to, or that the
// same tag was rebuilt.
func (s Status) Label() string {
	switch s.Kind {
	case NewPatch:
		return s.Tag
	case NewBuild:
		return "new build"
	}
	return ""
}

// Evaluate compares what the registry said with the digests of the local image
// (RepoDigests, "repo@sha256:…" or a bare "sha256:…").
//
// A newer patch wins over a new build: it is the more specific answer. With no
// local digest — an image built or loaded rather than pulled, or a Dockerfile
// base not present locally — a new build cannot be told from the same one, and
// nothing is said.
func Evaluate(f Facts, local []string) Status {
	switch {
	case f.Failed || f.CheckedAt.IsZero():
		return Status{}
	case f.NewerPatch != "":
		return Status{Kind: NewPatch, Tag: f.NewerPatch}
	case f.Digest == "" || len(local) == 0:
		return Status{}
	}
	for _, d := range local {
		if digestOf(d) == f.Digest {
			return Status{}
		}
	}
	return Status{Kind: NewBuild}
}

func digestOf(repoDigest string) string {
	if _, d, ok := strings.Cut(repoDigest, "@"); ok {
		return d
	}
	return repoDigest
}

// NewerPatch is the highest tag that differs from current only by its last
// version component, with the same variant — 20.11.1-alpine is offered
// 20.11.3-alpine, never 20.12.0-alpine — or "" when there is none.
//
// Only a tag with at least three components has patches of its own. A shorter
// one (3.20, 20) already floats over its patches: a new one moves the tag
// itself, which the digest says.
func NewerPatch(current string, tags []string) string {
	curVersion, curVariant := remediation.SplitTag(current)
	curParts := strings.Split(curVersion, ".")
	if curVersion == "" || len(curParts) < 3 {
		return ""
	}
	prefix := strings.Join(curParts[:len(curParts)-1], ".") + "."
	best, bestVersion := "", curVersion
	for _, t := range tags {
		v, variant := remediation.SplitTag(t)
		if variant != curVariant || !strings.HasPrefix(v, prefix) || len(strings.Split(v, ".")) != len(curParts) {
			continue
		}
		if c, ok := scan.CompareVersions(v, bestVersion); ok && c > 0 {
			best, bestVersion = t, v
		}
	}
	return best
}

// Registry is what a check asks: a tag's digest, and a repository's tags. It is
// a pair of functions so the policy can be tested without a network.
type Registry struct {
	Digest func(ref remediation.Ref, tag string) (string, error)
	Tags   func(ref remediation.Ref) ([]string, error)
}

// Default reaches each image's own registry, with the credentials the engine
// holds for it or anonymously.
func Default() Registry {
	creds := func(ref remediation.Ref) (string, string) {
		host := ref.Registry
		if host == "" {
			host = "docker.io"
		}
		user, pass, _ := docker.GetStoredCreds(host)
		return user, pass
	}
	return Registry{
		Digest: func(ref remediation.Ref, tag string) (string, error) {
			user, pass := creds(ref)
			return oci.ManifestDigest(oci.RegistryAPIBase(ref.Registry), ref.Repository, tag, user, pass)
		},
		Tags: func(ref remediation.Ref) ([]string, error) {
			user, pass := creds(ref)
			return oci.ListRegistryTags(oci.RegistryAPIBase(ref.Registry), ref.Repository, user, pass)
		},
	}
}

// Check returns the facts for each reference: from the cache while they are
// fresh, from the registry otherwise, and stores what it fetched. It never
// fails: a registry that does not answer is that reference's Failed.
//
// It does I/O and belongs in a Cmd.
func Check(reg Registry, refs []string, now time.Time) map[string]Facts {
	cached, err := cache.ReadImageUpdates()
	if err != nil {
		log.Printf("ERROR [imageupdate] read cache: %v", err)
		cached = map[string]Facts{}
	}
	out := map[string]Facts{}
	var stale []string
	for _, r := range refs {
		if _, seen := out[r]; seen || r == "" {
			continue
		}
		if f, ok := cached[r]; ok && fresh(f, now) {
			out[r] = f
			continue
		}
		out[r] = Facts{} // reserves the key, so a duplicate is not queued twice
		stale = append(stale, r)
	}
	fetched := fetch(reg, stale, now)
	for r, f := range fetched {
		out[r] = f
	}
	if len(fetched) > 0 {
		if err := cache.SetImageUpdates(fetched); err != nil {
			log.Printf("ERROR [imageupdate] store checks: %v", err)
		}
	}
	return out
}

func fresh(f Facts, now time.Time) bool {
	if f.Failed {
		return now.Sub(f.CheckedAt) < retryAfter
	}
	return now.Sub(f.CheckedAt) < freshFor
}

func fetch(reg Registry, refs []string, now time.Time) map[string]Facts {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, maxParallel)
		out = make(map[string]Facts, len(refs))
	)
	for _, r := range refs {
		wg.Add(1)
		sem <- struct{}{}
		go func(r string) {
			defer func() { <-sem; wg.Done() }()
			f := lookup(reg, r, now)
			mu.Lock()
			out[r] = f
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	return out
}

// lookup asks the registry about one reference. An untagged reference is its
// implicit latest; one pinned by digest with no tag has nothing to move and is
// recorded as checked, with nothing found.
func lookup(reg Registry, raw string, now time.Time) Facts {
	f := Facts{CheckedAt: now}
	ref := remediation.ParseRef(raw)
	tag := ref.Tag
	if tag == "" {
		if ref.Digest != "" {
			return f
		}
		tag = "latest"
	}
	digest, err := reg.Digest(ref, tag)
	if err != nil {
		log.Printf("ERROR [imageupdate] digest of %s: %v", raw, err)
		f.Failed = true
		return f
	}
	f.Digest = digest
	if version, _ := remediation.SplitTag(tag); len(strings.Split(version, ".")) >= 3 {
		tags, err := reg.Tags(ref)
		if err != nil {
			// The digest still stands; only the patch side is unknown.
			log.Printf("ERROR [imageupdate] tags of %s: %v", raw, err)
			return f
		}
		f.NewerPatch = NewerPatch(tag, tags)
	}
	return f
}
