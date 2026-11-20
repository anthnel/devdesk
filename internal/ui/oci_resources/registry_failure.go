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

// registryFailureReason is the short, secret-free reason a registry failure can show in
// the footer, or "" when it has none to add.
func registryFailureReason(err error) string {
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
