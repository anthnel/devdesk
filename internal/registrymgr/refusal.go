package registrymgr

import (
	"errors"
	"fmt"
)

// ErrMembersRefused marks a group whose repository manager said it is a group
// and then refused to list its members to the account that asked.
var ErrMembersRefused = errors.New("group members refused")

// MembersRefusedError is that refusal, with what a screen needs to explain it.
// Its text holds the group and the status only: never a URL, header or body.
type MembersRefusedError struct {
	Group     string // repository name in the manager (path segment, no URL)
	Status    int    // 401, 403, or 404 (Nexus: Repository not found)
	Anonymous bool   // the refused request carried no credential
	Cause     error  // the *oci.StatusError (Source: SourceManagementAPI)
}

// Error names the group and the status the manager answered.
func (e *MembersRefusedError) Error() string {
	return fmt.Sprintf("reading the members of group %q: management API returned %d", e.Group, e.Status)
}

// Unwrap gives the status error, so errors.As reaches *oci.StatusError.
func (e *MembersRefusedError) Unwrap() error { return e.Cause }

// Is makes errors.Is(err, ErrMembersRefused) true.
func (e *MembersRefusedError) Is(target error) bool { return target == ErrMembersRefused }
