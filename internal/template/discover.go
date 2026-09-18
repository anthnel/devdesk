package template

import (
	"regexp"
	"strings"

	"github.com/anthnel/devdesk/internal/oci"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// FromOCI turns what a registry catalog listed into entries, marked
// Discovered. They are read-only until adopted (Put with Discovered cleared),
// at which point they can be given tags and a description.
func FromOCI(registryURL string, listed []oci.TemplateEntry) []Entry {
	out := make([]Entry, 0, len(listed))
	for _, l := range listed {
		out = append(out, Entry{
			Slug:       slugFor("oci", l.Repository, l.Tag),
			Name:       l.Name,
			Source:     Source{Kind: KindOCI, URL: registryURL, Path: l.Repository, Ref: l.Tag},
			Discovered: true,
		})
	}
	return out
}

// Merge lists declared entries first, then the discovered ones that no declared
// entry already covers. Two entries cover the same thing when their sources
// are equal: adopting a discovered template must not leave it listed twice.
func Merge(declared, discovered []Entry) []Entry {
	covered := make(map[Source]bool, len(declared))
	for _, d := range declared {
		covered[d.Source] = true
	}
	out := append([]Entry(nil), declared...)
	for _, d := range discovered {
		if !covered[d.Source] {
			out = append(out, d)
		}
	}
	return out
}

func slugFor(parts ...string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.Join(parts, "-")), "-"), "-")
}
