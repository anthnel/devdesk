package containers

import (
	"strings"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The Ports column used to be a pass-through: `Cell` returned the string
// `docker ps` printed, with no Less and no Search. A single `-p 80:80` came out
// as thirty-three characters —
//
//	0.0.0.0:80->80/tcp, :::80->80/tcp
//
// — in a column asking for sixteen, for one published port. Three things were
// paid for and none of them read:
//
//   - docker prints a dual-stack publication twice. Merging them back is the one
//     place nothing is given up, and it halves the width on its own
//     (docker.ParseContainerPorts).
//   - the container port is repeated although it is almost never the question;
//     the one being looked for is the host port, because that is what a browser
//     or a curl is pointed at.
//   - `0.0.0.0` and `127.0.0.1` are spelled out although each carries one bit —
//     reachable from the network, or from this machine only. That bit is exactly
//     the security-relevant one, and it was drowning in the address.
//
// What is left is the host port and one icon saying who can reach it.
//
// Hiding the container port is a real loss and it is taken deliberately:
// `8080->80` and `8080->8080` now read alike. The case it costs is diagnosing a
// reverse proxy pointed at the wrong internal port, and the answer is still one
// keystroke away — `enter` opens `docker inspect` in the viewer (§3.25), which
// was not true when this column was written.

// portScopeIcon says who can reach a publication. Four scopes, four glyphs, and
// the glyph carries it rather than a colour.
//
// The plan had colour carrying "all interfaces" and a glyph carrying the rest.
// It cannot: `Style` colours a *cell*, and one cell holds several publications
// that do not share a scope — Rule 122 forbids the alternative, styling inside
// `Cell`. Colour would also be the wrong tool even if it worked, because
// publishing on every interface is what `-p 80:80` does by default, so it is the
// majority state and colouring it would put a colour on nearly every non-empty
// cell and a signal on none of them.
func portScopeIcon(p docker.PortBinding) string {
	switch p.Scope {
	case docker.ScopeLoopback:
		return theme.IconHome
	case docker.ScopeAddress:
		return theme.IconServer
	case docker.ScopeExposed:
		// Declared by the image, published by nobody. It belongs in the column —
		// leaving it out would make a container with exposed ports read as
		// having none — but it must not look connectable, which is what showing
		// it beside the published ones did.
		return theme.IconLock
	default:
		return theme.IconNetwork
	}
}

// portsCell renders one container's publications. Plain text, no styling: it is
// measured before it is drawn (Rule 122).
func portsCell(c docker.Container) string {
	if len(c.Ports) == 0 {
		return ""
	}
	labels := make([]string, 0, len(c.Ports))
	for _, p := range c.Ports {
		labels = append(labels, portLabel(p))
	}
	return strings.Join(labels, "  ")
}

// portLabel is one publication: who can reach it, on which port.
//
// The protocol is named only when it is not tcp. tcp is the massively dominant
// case, so a marker on every entry would inform no one — the colour discipline
// of Rule 122 applies to glyphs and suffixes just as much.
//
// The address family is named never, which departs from the sketch this column
// was planned from. The reasoning that rules out a tcp marker rules out a
// dual-stack one for the same reason and just as hard: after the merge, nearly
// every publication is both families or the only one the host has, so the
// marker would be carried by almost every row. And nothing is done differently
// on the strength of it — a host port is connected to by name, and the resolver
// picks the family.
func portLabel(p docker.PortBinding) string {
	port := p.HostPort
	if p.Scope == docker.ScopeExposed {
		port = p.ContainerPort
	}
	label := portScopeIcon(p) + " " + port
	if p.Protocol != "" && p.Protocol != "tcp" {
		label += "/" + p.Protocol
	}
	return label
}
