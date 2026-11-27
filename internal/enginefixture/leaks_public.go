package enginefixture

import (
	"net/netip"
	"regexp"
	"strings"

	"github.com/anthnel/devdesk/internal/fixtureleak"
)

// rulePublicAddress reports a public IPv4 address or a global IPv6 address: the
// address of a machine that is not the engine's own network, which a fixture has no
// reason to hold. The private, link-local and CGNAT ones are rulePrivateAddress.
const rulePublicAddress = "public-address"

var (
	// documentationNets are the ranges reserved for examples (RFC 5737, RFC 3849),
	// which a fixture may hold.
	documentationNets = []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32"),
	}
	// notPublicV4 are the IPv4 ranges that are neither public nor another rule's: the
	// unspecified address, multicast, and the reserved block that holds the broadcast
	// address and the netmasks (255.255.255.0).
	notPublicV4 = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	}
	globalV6 = netip.MustParsePrefix("2000::/3")

	// ipv6Candidate is a run of hexadecimal groups and colons that may be an address.
	ipv6Candidate = regexp.MustCompile(`(?i)(?:^|[^0-9a-f:.])([0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}(?:\.\d{1,3}){0,3})(?:$|[^0-9a-f:])`)
	// afterVersionWord ends a text that names a version: "version 1.2.3.4", `"Version":"1.2.3.4"`.
	afterVersionWord = regexp.MustCompile(`(?i)\b(?:version|ver|v)[\s"':=]*$`)
	// followedByNumber says the candidate is the start of a longer dotted number
	// (an OID, a five-field version): ".5" after the fourth field.
	followedByNumber = regexp.MustCompile(`^\.\d`)
)

// publicAddress reports a public IPv4 or global IPv6 address in the text. An IPv4
// dotted quad is not counted when it looks like something else: part of a longer
// dotted number (an OID, a version of five fields), just after a dot, a dash or a
// "v", or right after the word "version". That is a heuristic, and it errs on the
// side of reporting: "1.2.3.4" alone is an address. Loopback, the documentation
// ranges, the engines' own networks (EngineBridgeNets) and what the other rules
// own (private, link-local, CGNAT) are not public.
func publicAddress(text string) bool {
	if strings.Contains(text, ".") {
		for _, span := range fixtureleak.AddressSpans(text) {
			if publicV4(text, span[0], span[1]) {
				return true
			}
		}
	}
	if strings.Count(text, ":") > 1 {
		for _, m := range ipv6Candidate.FindAllStringSubmatch(text, -1) {
			if globalAddress(m[1]) {
				return true
			}
		}
	}
	return false
}

// publicV4 reports whether text[start:end], a dotted-quad candidate, is a public address.
func publicV4(text string, start, end int) bool {
	addr, err := netip.ParseAddr(text[start:end])
	if err != nil || !addr.Is4() || notPublic(addr) {
		return false
	}
	before, after := text[:start], text[end:]
	if before != "" {
		switch before[len(before)-1] {
		case '.', '-':
			return false
		}
	}
	return !followedByNumber.MatchString(after) && !afterVersionWord.MatchString(before)
}

// notPublic reports an address another rule owns or that is not routable: private,
// link-local, CGNAT, loopback, the engines' networks, the documentation ranges, the
// unspecified, multicast and reserved ranges.
func notPublic(addr netip.Addr) bool {
	if fixtureleak.IsLocalAddress(addr.String()) || addr.IsLoopback() || inPrefixes(EngineBridgeNets, addr) {
		return true
	}
	return inPrefixes(documentationNets, addr) || inPrefixes(notPublicV4, addr)
}

func inPrefixes(nets []netip.Prefix, addr netip.Addr) bool {
	for _, n := range nets {
		if n.Contains(addr) {
			return true
		}
	}
	return false
}

// globalAddress reports a global unicast IPv6 address (2000::/3) outside the
// documentation range.
func globalAddress(s string) bool {
	addr, err := netip.ParseAddr(s)
	return err == nil && addr.Is6() && !addr.Is4In6() && globalV6.Contains(addr) && !inPrefixes(documentationNets, addr)
}
