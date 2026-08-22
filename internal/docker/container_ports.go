package docker

import "strings"

// PortScope is what a publication's host address amounts to, which is the one
// thing about it worth knowing at a glance: can this be reached, and by whom.
//
// The address itself is not that. `0.0.0.0` and `127.0.0.1` are eleven and nine
// characters carrying one bit each — exposed to the network, or to this machine
// — and that bit is precisely the security-relevant one, drowned in the noise of
// spelling it out.
type PortScope int

const (
	// ScopeExposed is a port the image declares and nothing published. It is
	// not an association at all: there is no host port to connect to.
	ScopeExposed PortScope = iota
	// ScopeLoopback is published to this machine only (127.0.0.1, ::1).
	ScopeLoopback
	// ScopeAll is published on every interface (0.0.0.0, ::).
	ScopeAll
	// ScopeAddress is published on one named address, which is the only scope
	// whose address is worth keeping — the other three are constants.
	ScopeAddress
)

// PortBinding is one publication, after the two entries docker prints for a
// dual-stack one have been merged back into the single publication they are.
type PortBinding struct {
	// HostIP is empty for ScopeAll, ScopeLoopback and ScopeExposed: the first
	// two are constants the scope already names, the third has no host side.
	HostIP string
	// HostPort is empty for ScopeExposed, and may be a range ("8000-8002")
	// exactly as docker printed it.
	HostPort string
	// ContainerPort is what the port is inside the container. It is parsed and
	// kept even though the list does not show it — see the note on Cell in the
	// containers view — so that a future reader has it without re-parsing.
	ContainerPort string
	// Protocol is tcp, udp or sctp, lowercased as docker prints it.
	Protocol string
	Scope    PortScope
	// V4 and V6 say which families docker printed for this publication. Both
	// are true for the common dual-stack case, which is exactly why they do not
	// reach the screen: a marker carried by nearly every row informs no one.
	V4, V6 bool
}

// ParseContainerPorts turns the `{{.Ports}}` column of `docker ps` into the
// publications it describes.
//
// It lives here rather than in the view for the reason parseSSOutput does: the
// view must not learn to read Docker's output. It is also what makes the column
// searchable — filtering by port number is the obvious need, and it is
// impossible while the cell is one opaque string.
//
// What docker prints for a single `-p 80:80`:
//
//	0.0.0.0:80->80/tcp, :::80->80/tcp
//
// Thirty-three characters for one published port, in a column asking for
// sixteen. Merging the two halves back together is the one place nothing is
// given up: they are the same publication, and printing it twice doubles the
// width without adding a fact.
func ParseContainerPorts(raw string) []PortBinding {
	var out []PortBinding
	// index of the merged binding, keyed by everything that makes two entries
	// the same publication.
	seen := map[string]int{}

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		b, ok := parsePortBinding(entry)
		if !ok {
			continue
		}
		key := b.mergeKey()
		if i, dup := seen[key]; dup {
			out[i].V4 = out[i].V4 || b.V4
			out[i].V6 = out[i].V6 || b.V6
			continue
		}
		seen[key] = len(out)
		out = append(out, b)
	}
	return out
}

// mergeKey is what makes two printed entries one publication. The host address
// is deliberately absent for every scope but ScopeAddress: that is what merges
// `0.0.0.0:80` with `:::80` and `127.0.0.1:80` with `[::1]:80`, while keeping
// two different named addresses apart — they really are two publications.
func (p PortBinding) mergeKey() string {
	addr := ""
	if p.Scope == ScopeAddress {
		addr = p.HostIP
	}
	return string(rune('0'+p.Scope)) + "|" + addr + "|" + p.HostPort + "|" + p.ContainerPort + "|" + p.Protocol
}

// parsePortBinding reads one comma-separated entry.
//
// The forms docker emits, all of which appear in the wild:
//
//	0.0.0.0:80->80/tcp        published on every v4 interface
//	:::80->80/tcp             the same, v6 — note the address is a bare "::"
//	[::]:80->80/tcp           the same again, from a newer docker
//	[::1]:5432->5432/tcp      loopback, v6, bracketed
//	127.0.0.1:5432->5432/tcp  loopback, v4
//	0.0.0.0:8000-8002->8000-8002/tcp   a range
//	80/tcp                    EXPOSE, never published
func parsePortBinding(entry string) (PortBinding, bool) {
	host, target, published := strings.Cut(entry, "->")
	if !published {
		// EXPOSE only. There is no host side, so `host` holds the whole thing.
		port, proto := splitPortProto(host)
		if !isPort(port) {
			return PortBinding{}, false
		}
		return PortBinding{ContainerPort: port, Protocol: proto, Scope: ScopeExposed}, true
	}

	ip, hostPort, ok := splitHostAddr(host)
	if !ok || !isPort(hostPort) {
		return PortBinding{}, false
	}
	containerPort, proto := splitPortProto(target)
	if !isPort(containerPort) {
		return PortBinding{}, false
	}

	b := PortBinding{
		HostPort:      hostPort,
		ContainerPort: containerPort,
		Protocol:      proto,
		Scope:         scopeOf(ip),
	}
	if strings.Contains(ip, ":") {
		b.V6 = true
	} else {
		b.V4 = true
	}
	if b.Scope == ScopeAddress {
		b.HostIP = ip
	}
	return b, true
}

// splitHostAddr separates a host address from its port.
//
// The bracketed form is read first because it is unambiguous, and the bare v6
// form has to be read by the *last* colon rather than by splitting on one:
// `:::80` is the address `::` followed by port 80, and a naive split on ":"
// yields three empty fields and a port that is not there.
func splitHostAddr(s string) (ip, port string, ok bool) {
	if rest, found := strings.CutPrefix(s, "["); found {
		ip, port, ok = strings.Cut(rest, "]")
		if !ok {
			return "", "", false
		}
		port = strings.TrimPrefix(port, ":")
		return ip, port, port != ""
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", false
	}
	ip, port = s[:i], s[i+1:]
	return ip, port, port != ""
}

// isPort accepts a port number or the range form docker prints for `-p
// 8000-8002:8000-8002`, and nothing else.
//
// It is what stops an unrecognised entry from being read as an exposed port:
// without it, any word with no "->" in it parses as a publication named after
// itself, and lands in the column looking like something to connect to.
func isPort(s string) bool {
	if s == "" {
		return false
	}
	lo, hi, isRange := strings.Cut(s, "-")
	if isRange && !allDigits(hi) {
		return false
	}
	return allDigits(lo)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// splitPortProto reads "80/tcp", or "80" when docker omitted the protocol.
func splitPortProto(s string) (port, proto string) {
	port, proto, found := strings.Cut(strings.TrimSpace(s), "/")
	if !found {
		return port, "tcp"
	}
	return port, proto
}

// scopeOf classifies a host address. The wildcard and loopback constants are
// matched exactly; everything else is an address the user chose, and is worth
// showing as such.
func scopeOf(ip string) PortScope {
	switch ip {
	case "0.0.0.0", "::", "*", "":
		return ScopeAll
	case "127.0.0.1", "::1":
		return ScopeLoopback
	}
	if strings.HasPrefix(ip, "127.") {
		return ScopeLoopback
	}
	return ScopeAddress
}
