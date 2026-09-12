package netcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// runResolve answers what the target's name maps to, or what its address maps
// back to. Exactly one of the two questions has meaning for a given target,
// and the other says so rather than being absent.
func runResolve(ctx context.Context, t Target, env Env, _ Settings, _ *Results) []Check {
	if ip := net.ParseIP(strings.TrimSpace(t.Host)); ip != nil {
		return []Check{
			literalAddress(ip),
			reverseLookup(ctx, t, env, ip.String()),
		}
	}
	return []Check{
		forwardLookup(ctx, t, env),
		newCheck(CheckReverseDNS, StageResolve, NotApplicable,
			"Target is a name, not an address"),
	}
}

// forwardLookup resolves a name.
func forwardLookup(ctx context.Context, t Target, env Env) Check {
	ips, err := env.Resolve(ctx, t.Host, t.Resolver)
	c := newCheck(CheckResolve, StageResolve, Fail, "")
	c.fact("Resolver", resolverLabel(t.Resolver))

	if err != nil {
		c.Summary = fmt.Sprintf("%s does not resolve", t.Host)
		c.fact("Error", err.Error())
		if serverMisbehaved(err) {
			c.Reason = ReasonServerMisbehaving
		}
		return c
	}
	if len(ips) == 0 {
		c.Summary = fmt.Sprintf("%s resolves to no address", t.Host)
		return c
	}

	c.Verdict = OK
	if len(ips) == 1 {
		c.Summary = fmt.Sprintf("%s resolves to %s (%s)", t.Host, ips[0], addressClass(ips[0]))
	} else {
		c.Summary = fmt.Sprintf("%s resolves to %d addresses", t.Host, len(ips))
	}
	for _, ip := range ips {
		c.fact("Address", fmt.Sprintf("%s (%s)", ip, addressClass(ip)))
	}
	return c
}

// literalAddress records that no resolution was needed. It is NotApplicable
// with no Because: the question has no meaning here, which is a different
// thing from having been blocked.
func literalAddress(ip net.IP) Check {
	c := newCheck(CheckResolve, StageResolve, NotApplicable,
		"Target is a literal address, no resolution needed")
	c.fact("Address", fmt.Sprintf("%s (%s)", ip, addressClass(ip)))
	return c
}

// reverseLookup asks what an address maps back to.
//
// A missing PTR record is a Warn and never a Fail: plenty of reachable hosts
// have none, and the objective this package serves does not depend on it.
func reverseLookup(ctx context.Context, t Target, env Env, ip string) Check {
	names, err := env.ReverseLookup(ctx, ip, t.Resolver)
	c := newCheck(CheckReverseDNS, StageResolve, Warn, "")
	c.fact("Resolver", resolverLabel(t.Resolver))

	switch {
	case err != nil:
		c.Summary = fmt.Sprintf("%s has no reverse record", ip)
		c.fact("Error", err.Error())
	case len(names) == 0:
		c.Summary = fmt.Sprintf("%s has no reverse record", ip)
	default:
		c.Verdict = OK
		c.Summary = fmt.Sprintf("%s resolves back to %s", ip, strings.TrimSuffix(names[0], "."))
		for _, n := range names {
			c.fact("Name", strings.TrimSuffix(n, "."))
		}
	}
	return c
}

// resolverLabel names the resolver a lookup used. "System resolver" is not a
// placeholder: it is the answer the user cares about, because it is the one
// their own traffic will use.
func resolverLabel(resolver string) string {
	if strings.TrimSpace(resolver) == "" {
		return "System resolver"
	}
	return resolver
}

// serverMisbehaved reports whether err is Go's net.DNSError for a resolver
// that answered with something the client could not use — the exact wording
// ("server misbehaving") is unexported in net, so this matches the DNSError
// field it sets rather than the formatted string, the same way stage_tls.go
// matches tls.RecordHeaderError instead of grepping an error message.
func serverMisbehaved(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.Err == "server misbehaving"
}

// addressClass names the family an address belongs to.
//
// The class is what the diagnosis rests on — private or public, routable or
// not — never the literal octets. It is also what survives pseudonymisation if
// a payload is ever built from these checks.
func addressClass(ip net.IP) string {
	switch {
	case ip.IsLoopback():
		return "loopback"
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return "link-local"
	case ip.IsPrivate():
		return "private"
	case ip.IsUnspecified():
		return "unspecified"
	case ip.To4() == nil:
		return "public IPv6"
	default:
		return "public"
	}
}
