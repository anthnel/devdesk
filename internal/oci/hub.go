package oci

import "strings"

// RegistryHost strips the scheme and the trailing slash from a configured
// registry URL, leaving what Docker itself is keyed on: `host[:port]`, plus
// whatever path a repository manager serves the registry under.
//
// A registry URL is written either way in practice, so every Docker-facing
// use has to strip it (D39): a `docker pull` reference carrying `https://` is
// not a reference at all.
func RegistryHost(registryURL string) string {
	base := strings.TrimSuffix(strings.TrimSpace(registryURL), "/")
	for _, scheme := range []string{"https://", "http://"} {
		if len(base) >= len(scheme) && strings.EqualFold(base[:len(scheme)], scheme) {
			return base[len(scheme):]
		}
	}
	return base
}

// IsDockerHub reports whether a configured registry URL names Docker Hub,
// under any spelling: `docker.io` or `registry-1.docker.io`, with or without a
// scheme. Hub is the one registry whose references carry no host, so every
// caller has to recognise it.
func IsDockerHub(registryURL string) bool {
	host := RegistryHost(registryURL)
	return strings.EqualFold(host, "docker.io") || strings.EqualFold(host, "registry-1.docker.io")
}
