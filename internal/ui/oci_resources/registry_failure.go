package ociresources

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/oci"
)

// reasonTokenRealmOffOrigin is the footer reason of a token realm refused for being on
// another host than the registry (the host itself stays in the journal).
const reasonTokenRealmOffOrigin = "token realm on another host"

// reasonUpstreamBlocked is the footer reason of a proxy whose upstream is temporarily blocked.
const reasonUpstreamBlocked = "the proxy's upstream is temporarily blocked"

// The network causes: the proxy, the name lookup, and any other failed connection.
const (
	reasonProxyUnreachable    = "could not reach the proxy"
	reasonHostNotResolved     = "could not resolve the registry host"
	reasonRegistryUnreachable = "could not reach the registry"
)

// registryAccess is how the failed request identified itself, which changes what a rate
// limit means: a Docker Hub anonymous quota is not a signed-in one.
type registryAccess int

const (
	// accessOther is any registry but Docker Hub, and the zero value.
	accessOther registryAccess = iota
	// accessHubAnonymous is Docker Hub reached without credentials.
	accessHubAnonymous
	// accessHubLogin is Docker Hub reached with the entry's credentials.
	accessHubLogin
)

// accessFor is how a request to entry identifies itself. anonymousOnly is for a request that
// never carries credentials whatever the entry says (the Docker Hub search).
//
// It reads the entry's resolved auth mode, not the credential store: Update must not run a
// credential helper, and a mode that uses credentials is what sends them.
func accessFor(entry *browserRegistryEntry, anonymousOnly bool) registryAccess {
	if entry == nil || !oci.IsDockerHub(entry.URL) {
		return accessOther
	}
	if anonymousOnly || !config.UsesCredentials(entry.authMode) {
		return accessHubAnonymous
	}
	return accessHubLogin
}

// registryFailureReason is the short, secret-free reason a registry failure can show in
// the footer, or "" when it has none to add (a cancellation is the user leaving). Every
// reason is a constant: the error's own text, which can hold the URL and the query, never
// reaches the footer.
func registryFailureReason(err error, access registryAccess) string {
	if errors.Is(err, context.Canceled) {
		return ""
	}
	if errors.Is(err, oci.ErrTokenRealmOffOrigin) {
		return reasonTokenRealmOffOrigin
	}
	var se *oci.StatusError
	if errors.As(err, &se) {
		switch {
		case se.UpstreamBlocked():
			return reasonUpstreamBlocked
		case se.Code == http.StatusTooManyRequests:
			return rateLimitFooterReason(access)
		}
	}
	// No answer is tested before the net errors: a connection that timed out is both.
	var na *oci.NoAnswerError
	if errors.As(err, &na) {
		return na.Error()
	}
	var oe *net.OpError
	var de *net.DNSError
	switch {
	case errors.As(err, &oe) && oe.Op == "proxyconnect":
		return reasonProxyUnreachable
	case errors.As(err, &de):
		return reasonHostNotResolved
	case oe != nil:
		return reasonRegistryUnreachable
	}
	return ""
}

// rateLimitFooterReason is the footer reason of a 429: a Docker Hub anonymous quota is not a
// signed-in one, and any other registry gets the status in the text.
func rateLimitFooterReason(access registryAccess) string {
	switch access {
	case accessHubAnonymous:
		return "Docker Hub anonymous rate limit reached, try again later"
	case accessHubLogin:
		return "Docker Hub rate limit reached, try again later"
	}
	return "rate limit reached (HTTP 429), try again later"
}
