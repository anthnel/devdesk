package nettrace

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"time"
)

// Values from the Linux kernel headers (linux/errqueue.h, linux/icmp.h,
// bits/socket.h). They are spelled out here so the decoder builds and is
// tested on every platform.
const (
	// sock_extended_err.ee_origin.
	originLocal = 1 // SO_EE_ORIGIN_LOCAL: the local stack raised the error.
	originICMP  = 2 // SO_EE_ORIGIN_ICMP: an ICMPv4 message arrived.

	// ICMPv4 types and the codes this package cares about.
	icmpDestUnreachable = 3
	icmpTimeExceeded    = 11
	icmpCodePortUnreach = 3

	afInet = 2 // AF_INET

	extendedErrLen = 16 // sizeof(struct sock_extended_err)
	sockaddrInLen  = 16 // sizeof(struct sockaddr_in)
	// Offset of sin_addr inside sockaddr_in: family (2) + port (2).
	sockaddrInAddrOff = 4
)

// reply is one decoded IP_RECVERR error: what answered a probe.
type reply struct {
	Addr     netip.Addr
	ICMPType uint8
	ICMPCode uint8
	Origin   uint8
	Errno    uint32
	// RTT is left zero by decodeExtendedErr; the caller measures it.
	RTT time.Duration
}

var errShortExtendedErr = errors.New("extended error message too short")
var errNotAFInet = errors.New("extended error offender is not an IPv4 address")

// decodeExtendedErr reads a sock_extended_err followed by the sockaddr_in of
// the offender, in native byte order. Extra trailing bytes are ignored.
func decodeExtendedErr(data []byte) (reply, error) {
	if len(data) < extendedErrLen+sockaddrInLen {
		return reply{}, errShortExtendedErr
	}
	sa := data[extendedErrLen : extendedErrLen+sockaddrInLen]
	if binary.NativeEndian.Uint16(sa[0:2]) != afInet {
		return reply{}, errNotAFInet
	}
	return reply{
		Addr:     netip.AddrFrom4([4]byte(sa[sockaddrInAddrOff : sockaddrInAddrOff+4])),
		ICMPType: data[5],
		ICMPCode: data[6],
		Origin:   data[4],
		Errno:    binary.NativeEndian.Uint32(data[0:4]),
	}, nil
}

// classify maps a reply to the kind of hop it reveals.
//
// Known limit: a Port Unreachable from an address other than target is
// reported as HopUnreachable, so a multi-homed target that answers from
// another of its addresses is not recognized as reached.
func classify(r reply, target netip.Addr) HopKind {
	if r.Origin == originLocal {
		return HopError // the local stack refused the probe (e.g. EMSGSIZE)
	}
	if r.Origin != originICMP {
		return HopError
	}
	switch r.ICMPType {
	case icmpTimeExceeded:
		return HopRouter
	case icmpDestUnreachable:
		if r.ICMPCode == icmpCodePortUnreach && r.Addr == target {
			return HopTarget
		}
		return HopUnreachable
	}
	return HopError
}
