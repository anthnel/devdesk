package ociresources

import (
	"errors"

	"github.com/anthnel/devdesk/internal/oci"
)

// reasonTokenRealmOffOrigin is the footer reason of a token realm refused for being on
// another host than the registry (the host itself stays in the journal).
const reasonTokenRealmOffOrigin = "token realm on another host"

// reasonUpstreamBlocked is the footer reason of a proxy whose upstream is temporarily blocked.
const reasonUpstreamBlocked = "the proxy's upstream is temporarily blocked"

// registryAccess is how the failed request identified itself, which changes what a rate
// limit means: a Docker Hub anonymous quota is not a signed-in one.
//
// RED-STAGE STUB (DEVDESK-674): the type exists so the tests compile; registryFailureReason
// ignores it until the implementation lands.
type registryAccess int

const (
	// accessOther is any registry but Docker Hub, and the zero value.
	accessOther registryAccess = iota
	// accessHubAnonymous is Docker Hub reached without credentials.
	accessHubAnonymous
	// accessHubLogin is Docker Hub reached with the entry's credentials.
	accessHubLogin
)

// registryFailureReason is the short, secret-free reason a registry failure can show in
// the footer, or "" when it has none to add.
func registryFailureReason(err error, _ registryAccess) string {
	if errors.Is(err, oci.ErrTokenRealmOffOrigin) {
		return reasonTokenRealmOffOrigin
	}
	var se *oci.StatusError
	if errors.As(err, &se) && se.UpstreamBlocked() {
		return reasonUpstreamBlocked
	}
	var na *oci.NoAnswerError
	if errors.As(err, &na) {
		return na.Error()
	}
	return ""
}
