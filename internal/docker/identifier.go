package docker

// minShortIDLen is the length of the short container ID printed by `docker ps`.
// maxIDLen is the length of a full SHA-256 container ID.
const (
	minShortIDLen = 12
	maxIDLen      = 64
)

// IsContainerID reports whether s is a well-formed Docker container ID:
// a lowercase hexadecimal string of 12 to 64 characters.
//
// Container IDs are parsed from `docker ps` output and are later embedded in
// shell command lines by the log and inspect pagers. Validating them keeps shell
// metacharacters out of those command lines even if the parsing ever drifts.
func IsContainerID(s string) bool {
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
