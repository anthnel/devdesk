package docker

import (
	"errors"
	"fmt"
	"strings"
)

// minShortIDLen is the length of the short container ID printed by `docker ps`.
// maxIDLen is the length of a full SHA-256 container ID.
const (
	minShortIDLen = 12
	maxIDLen      = 64
)

// ErrInvalidImageID, ErrInvalidVolumeName and ErrInvalidNetworkID are wrapped by
// the destructive functions that refuse an identifier before building a command,
// as ErrInvalidContainerID is for containers. A network is named by its ID or by
// its name, hence ErrInvalidNetworkID.
var (
	ErrInvalidImageID    = errors.New("invalid image ID")
	ErrInvalidVolumeName = errors.New("invalid volume name")
	ErrInvalidNetworkID  = errors.New("invalid network ID")
)

// IsContainerID reports whether s is a well-formed Docker container ID:
// a lowercase hexadecimal string of 12 to 64 characters.
//
// Container IDs are parsed from `docker ps` output and are later passed as an
// argv element to the engine. The paths that validate them first, which keeps an
// option such as `-f`, or any other stray text, out of that argv even if the
// parsing ever drifts, are: LogsCmd, GetContainerLogs, InspectContainer, the
// container network reads, the shell in a container (DetectShell and ShellCmd,
// OC-18) and the lifecycle verbs StopContainer, RestartContainer,
// PauseContainer, UnpauseContainer and RemoveContainer (DEVDESK-803, guard G3). RemoveImage, RemoveVolume and RemoveNetwork validate
// their own identifier the same way (IsImageID, IsVolumeName, IsNetworkRef).
func IsContainerID(s string) bool {
	return isLowerHexID(s)
}

// IsImageID reports whether s is an image ID as `image ls` prints it: lowercase
// hexadecimal, 12 to 64 characters, with or without a `sha256:` prefix (the
// package reads both forms elsewhere, see parseImageInspect). Removal is by ID,
// never by reference, so a repository:tag is refused.
func IsImageID(s string) bool {
	return isLowerHexID(strings.TrimPrefix(s, "sha256:"))
}

// IsVolumeName reports whether s is a volume name the engine accepts:
// [A-Za-z0-9][A-Za-z0-9_.-]*. An anonymous volume is a 64-digit hex name and
// matches too. The first character is alphanumeric, so a name is never an option.
func IsVolumeName(s string) bool {
	if s == "" || !isAlnum(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !isAlnum(c) && c != '_' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// IsNetworkRef reports whether s names a network the way `network rm` takes it:
// by name (same alphabet as a volume name) or by ID (lowercase hex, which that
// alphabet already covers, so the two are one rule).
func IsNetworkRef(s string) bool {
	return IsVolumeName(s) || isLowerHexID(s)
}

func isLowerHexID(s string) bool {
	if len(s) < minShortIDLen || len(s) > maxIDLen {
		return false
	}
	for _, r := range s {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			return false
		}
	}
	return true
}

func isAlnum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// requireContainerID refuses id before a command is built for the verb. The
// error quotes the refused value with %q, so a control sequence in it is escaped;
// it reaches the log, the handlers show only a generic failure in the footer.
func requireContainerID(verb, id string) error {
	if IsContainerID(id) {
		return nil
	}
	return fmt.Errorf("%s: %q is not a container ID: %w", cmdLabel(verb), id, ErrInvalidContainerID)
}
