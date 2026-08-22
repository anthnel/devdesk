package forge

import (
	"net/url"
	"strings"

	"github.com/anthnel/devdesk/internal/config"
)

// shapes is what each platform can express. It lives here rather than on each
// backend because a shape is a property of the *platform*, not of a session —
// and the configuration view needs it before any session exists, to know which
// visibilities to offer.
//
// A backend's Shape() answers with its own entry, so there is still one table:
// the interface method exists for a consumer that holds a session and does not
// know the type.
var shapes = map[string]Shape{
	config.ForgeGitLab: {
		Name: config.ForgeGitLab,
		// Unbounded. GitLab documents a limit of 20 on self-managed instances,
		// but it is configurable and the API does not publish it: declaring 20
		// would be asserting what this cannot check, and refusing a legitimate
		// create. The server refuses with its own message instead.
		MaxNamespaceDepth: 0,
		Visibilities:      []string{"private", "internal", "public"},
		PermanentDelete:   true,
	},
	config.ForgeGitHub: {
		Name: config.ForgeGitHub,
		// An organisation never holds an organisation.
		MaxNamespaceDepth: 1,
		// No `internal` on GitHub.com. A form offering it there offers a value
		// the server will refuse.
		Visibilities: []string{"private", "public"},
		// GitHub deletes at once and has no grace period, so there is nothing
		// for a "permanent" checkbox to mean.
		PermanentDelete: false,
	},
}

// ShapeFor resolves a configured forge type to what it can express.
//
// An unknown type falls back to GitLab's, for VocabularyFor's reason: a screen
// with no visibilities to offer is a worse failure than one offering the wrong
// forge's, and config.applyDefaults already guarantees a known value.
func ShapeFor(forgeType string) Shape {
	if shape, ok := shapes[forgeType]; ok {
		return shape
	}
	return shapes[config.ForgeGitLab]
}

// DetectType guesses a forge from a URL's host, and answers "" when it cannot.
//
// **It only recognises the two public instances.** Self-hosted is the case that
// matters and `git.acme.com` could be either, so anything else is left for the
// user to declare — an unknown host returns "" rather than a default, because a
// wrong guess that looks confident is worse than no guess at all.
//
// Probing was considered and rejected: `/api/v4/version` against `/api/v3/`
// costs a round trip on every keystroke and fails on instances that
// authenticate those endpoints.
func DetectType(rawURL string) string {
	host := hostOf(rawURL)
	if host == "" {
		return ""
	}

	switch {
	case host == "gitlab.com" || strings.HasSuffix(host, ".gitlab.com"):
		return config.ForgeGitLab
	case host == "github.com" || strings.HasSuffix(host, ".github.com"):
		return config.ForgeGitHub
	}
	return ""
}

// hostOf extracts a hostname from what a user may have typed, which is not
// always a URL: `gitlab.com` with no scheme is the common case, and
// url.Parse reads it as a path rather than as a host.
func hostOf(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
