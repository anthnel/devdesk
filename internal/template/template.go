// Package template is the catalog of repository templates: what a template is,
// where it lives, and how its files are fetched.
//
// A catalog entry is a reference, not a copy. The content stays where it is —
// a git repository, a local checkout, an OCI artifact — and is fetched when a
// repository is created from it, so it cannot drift from its original.
package template

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Kind says where a template's content lives.
type Kind string

const (
	KindGit   Kind = "git"   // a remote git repository
	KindLocal Kind = "local" // a git repository on this machine
	KindOCI   Kind = "oci"   // an artifact in an OCI registry
)

// Kinds is every kind, in the order a form cycles through them.
var Kinds = []Kind{KindGit, KindLocal, KindOCI}

// Source is the address of a template's content.
//
// The fields mean something different per kind, which is why they are named for
// what they hold rather than for each kind:
//
//	git    URL = clone URL, Path = optional subdirectory, Ref = branch, tag or SHA
//	local  Path = repository directory,                    Ref = branch, tag or SHA
//	oci    URL = registry URL, Path = repository,         Ref = tag
type Source struct {
	Kind Kind   `yaml:"kind"`
	URL  string `yaml:"url,omitempty"`
	Path string `yaml:"path,omitempty"`
	Ref  string `yaml:"ref,omitempty"`
}

// Entry is one template in the catalog.
type Entry struct {
	// Slug is the identifier: what a repository-creation form keeps to remember
	// the choice, unique within the catalog.
	Slug        string   `yaml:"slug"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Source      Source   `yaml:"source"`

	// Discovered marks an entry read from a registry catalog rather than
	// declared. It is never written to the catalog file.
	Discovered bool `yaml:"-"`
}

// File is one file of a template.
type File struct {
	Path       string // relative to the template root, forward slashes
	Content    []byte
	Executable bool
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Validate reports why an entry cannot be stored or fetched, or nil.
func (e Entry) Validate() error {
	if !slugPattern.MatchString(e.Slug) {
		return fmt.Errorf("slug %q must be lowercase letters, digits and dashes", e.Slug)
	}
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("template %q has no name", e.Slug)
	}
	return e.Source.Validate()
}

// Validate reports why a source cannot be fetched, or nil.
func (s Source) Validate() error {
	switch s.Kind {
	case KindGit:
		if err := validateGitURL(s.URL); err != nil {
			return err
		}
	case KindLocal:
		if strings.TrimSpace(s.Path) == "" {
			return fmt.Errorf("a local template needs a directory")
		}
	case KindOCI:
		if strings.TrimSpace(s.URL) == "" || strings.TrimSpace(s.Path) == "" {
			return fmt.Errorf("an OCI template needs a registry URL and a repository")
		}
		if s.Ref == "" {
			return fmt.Errorf("an OCI template needs a tag")
		}
	default:
		return fmt.Errorf("unknown source kind %q", s.Kind)
	}
	for _, p := range []string{s.Path, s.Ref} {
		if strings.HasPrefix(p, "-") {
			return fmt.Errorf("%q would be read as a command-line option", p)
		}
	}
	return nil
}

// validateGitURL accepts https, http, ssh and git URLs and the scp form
// (user@host:path), and nothing else.
//
// The catalog file can be shared, so a URL in it is not trusted: git's `ext::`
// transport runs a command, and `file://` reads this machine. A local checkout
// is a different kind (KindLocal) precisely so neither is needed here.
func validateGitURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("a git template needs a URL")
	}
	if strings.HasPrefix(raw, "-") {
		return fmt.Errorf("%q would be read as a command-line option", raw)
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		switch u.Scheme {
		case "https", "http", "ssh", "git":
			if u.Host != "" {
				return nil
			}
			return fmt.Errorf("%q has no host", raw)
		}
		return fmt.Errorf("scheme %q is not allowed for a git template", u.Scheme)
	}
	if scpForm.MatchString(raw) {
		return nil
	}
	return fmt.Errorf("%q is not a git URL", raw)
}

var scpForm = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:.+$`)

// NormalizeTags trims, lowercases and de-duplicates tags, keeping their order
// and dropping empty ones. Tags are matched by the filter as typed, so they
// are stored the way they will be searched.
func NormalizeTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	var out []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
