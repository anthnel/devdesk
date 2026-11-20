package oci

import (
	"errors"
	"fmt"
	"net/url"
)

// ErrTokenRealmOffOrigin is returned when a Bearer challenge names a token realm on
// another origin (scheme, host and port) than the request that received it, and
// credentials are configured. The credentials would otherwise go to that host.
// Only the realm's host is named, never its userinfo, path or query.
var ErrTokenRealmOffOrigin = errors.New("token realm on another host")

// realmException is a registry whose token realm legitimately lives on another host.
type realmException struct{ registryHost, realmHost string }

// realmExceptions are the only registries allowed to send credentials to a realm on
// another host: Docker Hub and GitLab.com, both https, host compared exactly (no
// port). The list is one way (registry to realm) and frozen by a test: adding an
// entry is a deliberate decision (D168).
var realmExceptions = []realmException{
	{registryHost: "registry-1.docker.io", realmHost: "auth.docker.io"},
	{registryHost: "registry.gitlab.com", realmHost: "gitlab.com"},
}

// checkRealmOrigin refuses, before any request to it, a Bearer realm that is not on
// the origin of the registry request that received the challenge, when credentials
// are configured (withCredentials: both username and password, the condition under
// which Basic is sent). Without credentials the anonymous token request is
// harmless and goes out. A missing or unparsable realm passes: tokenEndpoint
// reports those. An origin is scheme and u.Host exactly, so "host" and "host:443"
// differ: when in doubt, refuse.
func checkRealmOrigin(registryURL, challenge string, withCredentials bool) error {
	if !withCredentials {
		return nil
	}
	realm, ok := parseBearerChallenge(challenge)["realm"]
	if !ok {
		return nil
	}
	u, err := url.Parse(realm)
	if err != nil {
		return nil
	}
	if sameOrigin(registryURL, realm) {
		return nil
	}
	if reg, err := url.Parse(registryURL); err == nil && reg.Host != "" && realmExcepted(reg, u) {
		return nil
	}
	return fmt.Errorf("%w (%s)", ErrTokenRealmOffOrigin, u.Host)
}

// realmExcepted reports whether realmExceptions allows reg to use the realm u.
func realmExcepted(reg, u *url.URL) bool {
	if reg.Scheme != "https" || u.Scheme != "https" {
		return false
	}
	for _, e := range realmExceptions {
		if reg.Host == e.registryHost && u.Host == e.realmHost {
			return true
		}
	}
	return false
}
