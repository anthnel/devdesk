package template

import (
	"strings"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/git"
)

// CredentialsFor resolves what fetching src may use — and only for the host it
// belongs to.
//
// A personal access token authenticates one host. Handing the forge's token to
// a template that lives elsewhere would send it to whoever that URL names, so a
// source on any other host is fetched anonymously (git.SameHost's rule, which
// treats "could not tell" as "no"). The catalog file can be shared, which is
// exactly why this cannot be decided by the entry.
//
// Nor is one sent over http://: the forge itself is reached over https, so an
// http:// URL to the same host is either a mistake or a downgrade.
//
// A private repository on a host that is not the forge's is therefore not
// reachable over HTTPS: use an SSH URL, or a public one. secrets may be nil.
func CredentialsFor(cfg *config.Config, secrets credentials.Storage, src Source) Credentials {
	if secrets == nil {
		return Credentials{}
	}

	// A secret does not cross a plaintext connection, even to its own host.
	if isPlainHTTP(src.URL) {
		return Credentials{}
	}

	switch src.Kind {
	case KindGit:
		if git.SameHost(src.URL, cfg.Forge.URL) {
			token, _ := secrets.Load(cfg.Forge.URL)
			return Credentials{Token: token}
		}
	case KindOCI:
		if sameRegistryHost(src.URL, cfg.Registry.URL) {
			password, _ := secrets.Load(cfg.Registry.URL)
			return Credentials{Username: cfg.Registry.Username, Password: password}
		}
	}
	return Credentials{}
}

// sameRegistryHost compares two registry URLs by host. An empty side never
// matches.
func sameRegistryHost(a, b string) bool {
	ha, hb := git.HostOf(a), git.HostOf(b)
	return ha != "" && strings.EqualFold(ha, hb)
}

// isPlainHTTP reports whether raw is an http:// (not https://) URL.
func isPlainHTTP(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "http://")
}
