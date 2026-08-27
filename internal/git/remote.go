package git

import (
	"net/url"
	"strings"
)

// What a git remote points at, in the one form the rest of the application can
// compare and display.
//
// These lived in internal/ui/workspaces, where the sync needed them to decide
// whether DevDesk's token may go to a repository's host (§3.17). §3.42 asks the
// same question for another reason — whether plumber may grade a repository at
// all — and a domain package cannot import a view, so the rule moved here
// rather than being written a second time. Two copies of a host comparison is
// how a repository comes to be trusted by one feature and not by the other.

// NormalizeRemoteURL converts any git remote URL to a plain HTTPS URL, without
// credentials and without the .git suffix — suitable for a browser, and for
// comparing hosts.
func NormalizeRemoteURL(rawURL string) string {
	// SCP-like syntax: git@host:path
	if idx := strings.Index(rawURL, ":"); idx != -1 && !strings.Contains(rawURL[:idx], "/") {
		after := rawURL[idx+1:]
		if !strings.HasPrefix(after, "//") {
			host := rawURL[:idx]
			if at := strings.Index(host, "@"); at != -1 {
				host = host[at+1:]
			}
			return "https://" + host + "/" + strings.TrimSuffix(after, ".git")
		}
	}
	// URL syntax: strip credentials and .git suffix
	if idx := strings.Index(rawURL, "://"); idx != -1 {
		scheme := rawURL[:idx]
		rest := rawURL[idx+3:]
		if at := strings.Index(rest, "@"); at != -1 {
			if slash := strings.Index(rest, "/"); slash == -1 || at < slash {
				rest = rest[at+1:]
			}
		}
		return scheme + "://" + strings.TrimSuffix(rest, ".git")
	}
	return rawURL
}

// HostOf returns a URL's host, lowercased and without a port.
func HostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// SameHost reports whether a repository's remote is on the configured forge.
//
// An empty host on either side is never a match: "I could not tell" must not
// read as "yes". That is what decides whether a credential is offered and, for
// plumber, whether a repository is graded at all — a repository of another
// forge is not scanned anonymously, it is not scannable.
func SameHost(remoteURL, forgeURL string) bool {
	remote := HostOf(NormalizeRemoteURL(remoteURL))
	forge := HostOf(forgeURL)
	if remote == "" || forge == "" {
		return false
	}
	return strings.EqualFold(remote, forge)
}
