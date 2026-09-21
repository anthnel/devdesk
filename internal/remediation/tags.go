package remediation

import (
	"regexp"
	"sort"
	"strings"

	"github.com/anthnel/devdesk/internal/scan"
)

// Track says how far a base image may move.
type Track string

const (
	// TrackSameLine keeps the major version: 3.18 may become 3.21, never 4.0.
	// A major bump is the case a scan cannot vouch for — it says the CVEs are
	// gone, not that the application still runs — so it is not the default.
	TrackSameLine Track = "same-line"
	// TrackNextMajor also allows the next major version that exists.
	TrackNextMajor Track = "next-major"
)

// ParseTrack reads a configured track, falling back to TrackSameLine for
// anything it does not know.
func ParseTrack(s string) Track {
	if Track(s) == TrackNextMajor {
		return TrackNextMajor
	}
	return TrackSameLine
}

// Ref is an image reference, split.
type Ref struct {
	// Name is the reference without its tag and digest, as it was written:
	// "node", "gcr.io/distroless/static". A candidate is Name plus a new tag, so
	// it reads the way the file already does.
	Name string
	// Registry is the host, empty for Docker Hub. Repository is the path a
	// registry API expects, with Docker Hub's implicit "library/" added.
	Registry   string
	Repository string
	Tag        string
	Digest     string
}

// ParseRef splits an image reference into its parts. It does not validate: a
// string that is not a reference yields a Ref that names nothing useful, and
// the registry says so.
func ParseRef(s string) Ref {
	var r Ref
	s = strings.TrimSpace(s)
	if before, digest, ok := strings.Cut(s, "@"); ok {
		s, r.Digest = before, digest
	}
	// A colon after the last slash is a tag; one before it is a registry port.
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		s, r.Tag = s[:i], s[i+1:]
	}
	r.Name = s

	path := s
	if first, rest, ok := strings.Cut(s, "/"); ok &&
		(strings.ContainsAny(first, ".:") || first == "localhost") {
		r.Registry, path = first, rest
	}
	if r.Registry == "" || r.Registry == "docker.io" || r.Registry == "index.docker.io" {
		r.Registry = ""
		path = strings.TrimPrefix(path, "docker.io/")
		if !strings.Contains(path, "/") {
			path = "library/" + path
		}
	}
	r.Repository = path
	return r
}

// WithTag is the reference for another tag of the same repository. The digest
// is dropped: it pinned the old tag's content and would name it still.
func (r Ref) WithTag(tag string) string { return r.Name + ":" + tag }

var tagVersion = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)(.*)$`)

// SplitTag cuts a tag into its version and its variant: "3.20.1-alpine3.19" is
// version "3.20.1" and variant "alpine3.19", "20-slim" is "20" and "slim". A tag
// with no leading number — "latest", "bookworm" — has no version.
func SplitTag(tag string) (version, variant string) {
	m := tagVersion.FindStringSubmatch(tag)
	if m == nil {
		return "", tag
	}
	return m[1], strings.TrimLeft(m[2], "-_")
}

func versionParts(v string) []string { return strings.Split(v, ".") }

// Candidates are the tags a base image could move to, best first, at most max.
//
// A candidate has the same variant as the current tag ("alpine3.19" stays on
// alpine, "slim" stays slim), the same number of version components (a tag
// written "3.18" is not offered "3.21.1", which would pin what it left
// floating), and a strictly higher version. Under TrackSameLine it also keeps
// the major version; under TrackNextMajor it may take the smallest higher major
// that exists too.
//
// The reason is empty when there are candidates and says why not otherwise: the
// tag carries no version, or nothing newer exists on the line. Nothing here
// says a candidate is *safe* — that is what the re-scan measures.
func Candidates(current string, tags []string, track Track, max int) (out []string, reason string) {
	if current == "" {
		return nil, "the reference has no tag — it is pinned by digest or floats on latest"
	}
	curVersion, curVariant := SplitTag(current)
	if curVersion == "" {
		return nil, "the tag carries no version to move from"
	}
	curParts := versionParts(curVersion)

	var newer []candidate
	seen := map[string]bool{}
	for _, t := range tags {
		v, variant := SplitTag(t)
		if v == "" || variant != curVariant || len(versionParts(v)) != len(curParts) || seen[t] {
			continue
		}
		if c, ok := scan.CompareVersions(v, curVersion); !ok || c <= 0 {
			continue
		}
		seen[t] = true
		newer = append(newer, candidate{t, v})
	}

	// The major versions on offer: the current one, and for next-major the
	// smallest higher one.
	allowed := map[string]bool{curParts[0]: true}
	if track == TrackNextMajor {
		if next := nextMajor(newer, curParts[0]); next != "" {
			allowed[next] = true
		}
	}

	var kept []candidate
	for _, c := range newer {
		if allowed[versionParts(c.version)[0]] {
			kept = append(kept, c)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		c, _ := scan.CompareVersions(kept[i].version, kept[j].version)
		return c > 0
	})
	for _, c := range kept {
		if len(out) == max {
			break
		}
		out = append(out, c.tag)
	}
	if len(out) == 0 {
		return nil, "no newer tag of the same variant exists on this line"
	}
	return out, ""
}

// candidate is a tag and the version it carries.
type candidate struct{ tag, version string }

// nextMajor is the smallest major version above cur among cands, or "".
func nextMajor(cands []candidate, cur string) string {
	best := ""
	for _, c := range cands {
		major := versionParts(c.version)[0]
		if cmp, ok := scan.CompareVersions(major, cur); !ok || cmp <= 0 {
			continue
		}
		if cmp, _ := scan.CompareVersions(major, best); best == "" || cmp < 0 {
			best = major
		}
	}
	return best
}
