// Package fixtureleak holds the text detectors that the recorded-fixture
// packages (internal/nexusfixture and internal/enginefixture) share: secret
// formats, JWT-like values, URL authorities, private addresses, internal host
// names, the exact-term variants and the external deny list.
//
// Each detector answers about one line of text and returns a yes or a name of
// the input class it found, never a report: the callers report file, line and
// rule, and never the matched text, so that a log never repeats a secret. The
// package knows neither fixture format and does not import "testing".
package fixtureleak

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// DenylistEnv names the variable that points at the external deny list: a file
// of terms, one per line, that no fixture may hold. The file is never in the
// repository, since its terms are the names of an instance.
const DenylistEnv = "DEVDESK_FIXTURE_DENYLIST"

var (
	jwtLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}`)
	// secretFormat matches well-known secret formats: GitHub and GitLab tokens,
	// AWS access key ids, and the first line of a PEM private key.
	secretFormat = regexp.MustCompile(
		`gh[pousr]_[A-Za-z0-9]{36,}|glpat-[A-Za-z0-9_-]{20,}|(?:AKIA|ASIA)[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	// urlAuth reads the authority of a URL, also when its slashes are JSON-escaped.
	urlAuth = regexp.MustCompile(`(?i)https?:\\?/\\?/([^\s/"'<>?#\\,;)]+)`)
	// ipv4Like needs a non-numeric character before the address, not a word boundary:
	// "ip10.1.2.3" holds one.
	ipv4Like = regexp.MustCompile(`(?:^|[^0-9])(\d{1,3}(?:\.\d{1,3}){3})`)
	// ipv6Local starts a unique-local (fc00::/7) or link-local (fe80::/10) address.
	ipv6Local = regexp.MustCompile(`(?i)(?:^|[^0-9a-f])(?:f[cd][0-9a-f]{2}|fe[89ab][0-9a-f]):`)
	// bareHost is a host name in an internal zone; the group is the name.
	bareHost = regexp.MustCompile(`(?i)([a-z0-9_-]+(?:\.[a-z0-9_-]+)*\.(?:home\.arpa|lan|internal|local))(?:$|[^a-z0-9_-])`)
	// percentEscape is a URL escape, a separator for bareHost.
	percentEscape = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)

	cgnat = netip.MustParsePrefix("100.64.0.0/10")

	// jsonEscaper writes a term as it sits inside a JSON string.
	jsonEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `/`, `\/`)
)

// SecretFormat reports whether the line holds a well-known secret format: a
// GitHub or GitLab token, an AWS access key id or the first line of a PEM
// private key.
func SecretFormat(line string) bool { return secretFormat.MatchString(line) }

// JWTLike reports whether the line holds "eyJ" followed by at least ten base64url
// characters, the start of a JWT.
func JWTLike(line string) bool { return jwtLike.MatchString(line) }

// URLAuthorities lists the authority (user info, host and port) of each http or
// https URL on the line, also when the slashes are JSON-escaped.
func URLAuthorities(line string) []string {
	// The cheap test keeps the regular expression off the lines that cannot match.
	if !strings.Contains(strings.ToLower(line), "http") {
		return nil
	}
	var out []string
	for _, m := range urlAuth.FindAllStringSubmatch(line, -1) {
		out = append(out, m[1])
	}
	return out
}

// AllowedAuthority checks the authority of a URL: no user info, and a host among
// hosts (lower case).
func AllowedAuthority(authority string, hosts []string) bool {
	if strings.Contains(authority, "@") {
		return false
	}
	return AllowedHost(authority, hosts)
}

// AllowedHost checks a host, with or without a port, against hosts (lower case).
func AllowedHost(hostPort string, hosts []string) bool {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	for _, h := range hosts {
		if h == host {
			return true
		}
	}
	return false
}

// IsLocalAddress reports whether s is a private, link-local or CGNAT IPv4 or IPv6
// address.
func IsLocalAddress(s string) bool {
	addr, err := netip.ParseAddr(s)
	return err == nil && (addr.IsPrivate() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr))
}

// AddressSpans lists the [start, end) offsets of each dotted-quad candidate on the
// line, before any check of what it is.
func AddressSpans(line string) [][2]int {
	if !strings.Contains(line, ".") {
		return nil
	}
	var out [][2]int
	for _, m := range ipv4Like.FindAllStringSubmatchIndex(line, -1) {
		out = append(out, [2]int{m[2], m[3]})
	}
	return out
}

// LocalAddresses reports whether the line holds a private, link-local, CGNAT or
// unique-local address, except an IPv4 address inside one of the allowed networks.
func LocalAddresses(line string, allowed []netip.Prefix) bool {
	for _, span := range AddressSpans(line) {
		text := line[span[0]:span[1]]
		if !IsLocalAddress(text) {
			continue
		}
		if addr, err := netip.ParseAddr(text); err == nil && inAny(allowed, addr) {
			continue
		}
		return true
	}
	return strings.Count(line, ":") > 1 && ipv6Local.MatchString(line)
}

func inAny(nets []netip.Prefix, addr netip.Addr) bool {
	for _, n := range nets {
		if n.Contains(addr) {
			return true
		}
	}
	return false
}

// InternalHostNames lists the names in an internal zone (.home.arpa, .lan,
// .internal, .local) the line holds. A URL escape is a separator and the line is
// also read decoded: a name found by either reading is returned. The names keep
// the case they have on the line.
func InternalHostNames(line string) []string {
	if !strings.Contains(line, ".") {
		return nil
	}
	// A URL-encoded separator ("%2F") would otherwise join the name before it
	// ("2Fbench-upstream..."), and decoding can make a match disappear
	// ("nexus.home.arpa%2Dmirror" becomes one longer name, "x.lan%41" too). So the
	// line is scanned decoded and with each escape turned into a separator, and
	// any match of either is a name.
	texts := []string{percentEscape.ReplaceAllString(line, " ")}
	if decoded, err := url.PathUnescape(line); err == nil && decoded != line {
		texts = append(texts, decoded)
	}
	var out []string
	for _, text := range texts {
		for _, m := range bareHost.FindAllStringSubmatch(text, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// ExactVariants lists, lower-cased, the forms of each term that can sit in a
// fixture: as given, JSON-escaped, URL-escaped as a query value and as a path. An
// empty term matches nothing and has no variant.
func ExactVariants(terms []string) []string {
	var out []string
	for _, term := range terms {
		if term == "" {
			continue
		}
		for _, v := range []string{term, jsonEscaper.Replace(term), url.QueryEscape(term), url.PathEscape(term)} {
			out = append(out, strings.ToLower(v))
		}
	}
	return out
}

// ParseDenylist keeps the active terms: one per line, blank lines and lines
// starting with # are skipped.
func ParseDenylist(data []byte) []string {
	var terms []string
	for line := range bytes.Lines(data) {
		term := strings.TrimSpace(string(line))
		if term != "" && !strings.HasPrefix(term, "#") {
			terms = append(terms, term)
		}
	}
	return terms
}

// DenylistSummary is the log line that says what the deny list amounted to: an
// empty one never passes for an applied list.
func DenylistSummary(n int) string {
	if n == 0 {
		return "0 terms: the file has no active term, so the external deny list was NOT applied"
	}
	return strconv.Itoa(n) + " terms applied"
}

// ReadDenylist reads the optional external deny list the variable DenylistEnv
// names (through getenv). It always returns a note that says what was done, so that
// an unset variable or a file without an active term is not mistaken for a list
// that was applied. An unreadable file is an error that names the variable.
func ReadDenylist(getenv func(string) string) (terms []string, note string, err error) {
	path := getenv(DenylistEnv)
	if path == "" {
		return nil, DenylistEnv + " is not set: the external deny list was not used", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("%s names a file that cannot be read: %w", DenylistEnv, err)
	}
	terms = ParseDenylist(data)
	return terms, DenylistEnv + ": " + DenylistSummary(len(terms)), nil
}
