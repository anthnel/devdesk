package netdiag

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// isIPAddress reports whether s is a valid IPv4 or IPv6 address.
func isIPAddress(s string) bool {
	return net.ParseIP(strings.TrimSpace(s)) != nil
}

// maxHostnameLen is the maximum total length of a DNS name (RFC 1035).
const maxHostnameLen = 253

// defaultPort is used when the port field is left empty.
const defaultPort = "443"

// capitalize upper-cases the first letter, so lowercase error strings can be
// reused as footer messages (Rule 137).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// isHostname reports whether s is a syntactically valid DNS hostname:
// dot-separated labels of 1-63 chars, each made of letters, digits and hyphens,
// and not starting or ending with a hyphen.
func isHostname(s string) bool {
	if s == "" || len(s) > maxHostnameLen {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(s, "."), ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !isAlphaNum && r != '-' {
				return false
			}
		}
	}
	return true
}

// validateTarget checks that the target is a usable IP address or hostname.
//
// Nothing here shells out any more — the openssl pipeline this guard was
// written for went with §3.33, and the route trace, the last command that took
// the target as an argument, went with §3.47. It is kept because a resolver
// asked for "example.com; rm -rf /" should be told the target is malformed
// rather than handed it, and because a guard is cheaper to keep than to
// reinstate the day something shells out again.
func validateTarget(target string) error {
	if target == "" {
		return fmt.Errorf("target is required")
	}
	if isIPAddress(target) || isHostname(target) {
		return nil
	}
	return fmt.Errorf("target must be a hostname or IP address")
}

// validatePort checks that the port is a decimal number in the 1-65535 range.
func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("port must be a number between 1 and 65535")
	}
	return nil
}
