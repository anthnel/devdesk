package netcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
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

// concludeResolveBehindProxy rewrites a failed direct lookup once the Proxy row
// says a proxy applies to the target: behind a mandatory proxy the name is often
// resolved by the proxy, so "does not resolve" alone would mislead. The verdict
// stays Fail, the lookup did fail.
//
// A server-misbehaving reason is more precise than the generic conclusion and is
// kept as it is; only the Proxy fact is added. The check is replaced with a new
// fact slice: clone is shallow, so the slice is shared with the results this
// call started from and is never written into (Rule 110).
func concludeResolveBehindProxy(t Target, res *Results) {
	c, found := res.Get(CheckResolve)
	if !found || c.Verdict != Fail {
		return
	}
	display, ok := proxyConfigured(res)
	if !ok {
		return
	}

	switch c.Reason {
	case "":
		c.Summary = fmt.Sprintf("%s does not resolve directly; a proxy is configured for this target", t.Host)
		c.Reason = ReasonDirectResolveFailedProxyConfigured
	case ReasonServerMisbehaving:
		if slices.ContainsFunc(c.Facts, func(f Fact) bool { return f.Key == "Proxy" }) {
			return
		}
	default:
		return
	}
	c.Facts = withFactAfter(c.Facts, "Resolver", Fact{Key: "Proxy", Value: display})
	res.add(c)
}

// withFactAfter returns a new slice with f inserted right after the first fact
// named key, or at the end when there is none. It never writes into facts.
func withFactAfter(facts []Fact, key string, f Fact) []Fact {
	at := len(facts)
	for i, existing := range facts {
		if existing.Key == key {
			at = i + 1
			break
		}
	}
	out := make([]Fact, 0, len(facts)+1)
	out = append(out, facts[:at]...)
	out = append(out, f)
	return append(out, facts[at:]...)
}

// forwardLookup resolves a name.
func forwardLookup(ctx context.Context, t Target, env Env) Check {
	ips, err := env.Resolve(ctx, t.Host, t.Resolver)
	c := newCheck(CheckResolve, StageResolve, Fail, "")
	c.fact("Resolver", resolverLabel(t.Resolver))

	if err != nil {
		c.Summary = fmt.Sprintf("%s does not resolve", t.Host)
		c.fact("Error", err.Error())
		if dnsErr, ok := serverMisbehaved(err); ok {
			c.Reason = ReasonServerMisbehaving
			c.fact("Temporary", temporaryLabel(dnsErr.IsTemporary))
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
func serverMisbehaved(err error) (*net.DNSError, bool) {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.Err == "server misbehaving" {
		return dnsErr, true
	}
	return nil, false
}

// temporaryLabel names the one bit "server misbehaving" leaves out of its own
// text: whether the response's code was SERVFAIL (IsTemporary — a transient
// problem, worth a retry) or anything else Go's client will not accept
// (FORMERR, REFUSED, an unsupported EDNS version...), which does not go away
// on its own.
func temporaryLabel(temporary bool) string {
	if temporary {
		return "Yes — a SERVFAIL response, usually transient"
	}
	return "No — the server actively rejected or could not parse the query"
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
