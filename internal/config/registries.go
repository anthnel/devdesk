package config

import (
	"fmt"
	"net/url"
	"strings"
)

// A repository-manager group is a pullable registry as well as a container for
// other registries, which is why both kinds live in one list with a
// discriminator rather than in two lists (§3.8, decision A).
const (
	KindRegistry = "registry"
	KindGroup    = "group"
)

// The repository manager serving a group. It is declared, not sniffed from the
// URL: that is what lets a second implementation exist without guessing
// (§3.8, decision F).
const (
	ProviderGeneric     = "generic"
	ProviderNexus       = "nexus"
	ProviderHarbor      = "harbor"
	ProviderArtifactory = "artifactory"
	ProviderGitLab      = "gitlab"
)

// fallbackSlug names an entry whose alias and URL both reduce to nothing.
const fallbackSlug = "registry"

// Providers returns the declared providers in the order a form cycles them,
// generic first because it is the default for a group that says nothing.
func Providers() []string {
	return []string{ProviderGeneric, ProviderNexus, ProviderHarbor, ProviderArtifactory, ProviderGitLab}
}

// Kinds returns the two kinds in the order a form cycles them.
func Kinds() []string {
	return []string{KindRegistry, KindGroup}
}

// Slugify reduces s to the [a-z0-9-] alphabet a slug is restricted to, so that
// it can be a file name, a cache key and a YAML scalar without quoting.
// Returns "" when nothing survives.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// DeriveSlug proposes a slug for an entry that declares none: the alias when
// there is one, the URL's host otherwise.
func DeriveSlug(item RegistryItem) string {
	if s := Slugify(item.Alias); s != "" {
		return s
	}
	return Slugify(registryHost(item.URL))
}

// registryHost extracts host[:port] from a registry URL, which may be written
// with or without a scheme — "registry.example.com:5000/path" is a legal thing
// to type into the form.
func registryHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return raw
}

// normalizeRegistries fills in what a config written by an older build does not
// carry, and refuses one that cannot round-trip.
//
// Slugs DevDesk derives are made unique here. Slugs the file declares are never
// rewritten: a member points at its group by slug, so renaming one silently
// would move that group's members under a different group. A file that declares
// the same slug twice, or points at a group that is not there, is an error the
// user has to resolve — the form is what stops either from being written.
func normalizeRegistries(items []RegistryItem) error {
	for i := range items {
		applyRegistryKind(&items[i])
	}
	taken, err := declaredSlugs(items)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].Slug != "" {
			continue
		}
		items[i].Slug = uniqueSlug(ProposeSlug(items[i]), taken)
		taken[items[i].Slug] = true
	}
	return checkParents(items, taken)
}

// applyRegistryKind fills in kind and provider for an entry that predates them.
func applyRegistryKind(item *RegistryItem) {
	// A management URL used to be the only way to mark a group: NexusDetector
	// keyed on exactly that, so an entry carrying one was a Nexus group in all
	// but name, and migrating it to anything else would change its behaviour.
	legacyGroup := item.Kind == "" && strings.TrimSpace(item.ManagementURL) != ""

	if item.Kind == "" {
		item.Kind = KindRegistry
		if legacyGroup {
			item.Kind = KindGroup
		}
	}
	if item.Kind == KindGroup && item.Provider == "" {
		item.Provider = ProviderGeneric
		if legacyGroup {
			item.Provider = ProviderNexus
		}
	}
}

// declaredSlugs collects the slugs the file states, rejecting a duplicate.
func declaredSlugs(items []RegistryItem) (map[string]bool, error) {
	taken := make(map[string]bool, len(items))
	for i := range items {
		slug := strings.TrimSpace(items[i].Slug)
		items[i].Slug = slug
		if slug == "" {
			continue
		}
		if taken[slug] {
			return nil, fmt.Errorf(
				"two registries declare the slug %q: a slug identifies a group's members and must be unique", slug)
		}
		taken[slug] = true
	}
	return taken, nil
}

// ProposeSlug returns the slug to offer for an entry that declares none: the
// derived one, or a fallback when there is nothing to derive from. It is what
// the form suggests and what normalization writes.
func ProposeSlug(item RegistryItem) string {
	if base := DeriveSlug(item); base != "" {
		return base
	}
	return fallbackSlug
}

// uniqueSlug returns base, or base-2, base-3… until one is free.
func uniqueSlug(base string, taken map[string]bool) string {
	slug := base
	for n := 2; taken[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	return slug
}

// checkParents refuses a member whose group is not in the list, which would
// otherwise be an entry nothing can ever reach.
func checkParents(items []RegistryItem, taken map[string]bool) error {
	for i := range items {
		parent := strings.TrimSpace(items[i].Parent)
		items[i].Parent = parent
		if parent == "" {
			continue
		}
		if !taken[parent] {
			return fmt.Errorf("registry %q names the parent group %q, which is not configured", items[i].Slug, parent)
		}
	}
	return nil
}
