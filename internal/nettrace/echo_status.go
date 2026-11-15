package nettrace

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
)

// IP_STATUS values from ipexport.h (Windows SDK), spelled out so the mapping
// builds and is tested on every platform.
const (
	ipSuccess             = 0     // IP_SUCCESS
	ipDestNetUnreachable  = 11002 // IP_DEST_NET_UNREACHABLE
	ipDestHostUnreachable = 11003 // IP_DEST_HOST_UNREACHABLE
	ipDestProtUnreachable = 11004 // IP_DEST_PROT_UNREACHABLE
	ipDestPortUnreachable = 11005 // IP_DEST_PORT_UNREACHABLE
	ipReqTimedOut         = 11010 // IP_REQ_TIMED_OUT
	ipTTLExpiredTransit   = 11013 // IP_TTL_EXPIRED_TRANSIT
	ipTTLExpiredReassem   = 11014 // IP_TTL_EXPIRED_REASSEM
	ipDestUnreachable     = 11040 // IP_DEST_UNREACHABLE
	ipTimeExceeded        = 11041 // IP_TIME_EXCEEDED
	ipDestScopeMismatch   = 11045 // IP_DEST_SCOPE_MISMATCH
	ipGeneralFailure      = 11050 // IP_GENERAL_FAILURE
)

// originEcho marks a reply built from an ICMP echo status (Windows
// IcmpSendEcho). It is a DevDesk value outside the kernel's
// SO_EE_ORIGIN_* range, so it never collides with originLocal or originICMP.
const originEcho = 0xF0

// echoReplyMinLen is the part of ICMP_ECHO_REPLY this package reads: Address
// (4 bytes, network order) and Status (4 bytes, little-endian).
const echoReplyMinLen = 8

// EchoStatusError reports an ICMP echo status that carries no usable hop.
// Its text is ASCII only, never a localized system message.
type EchoStatusError struct {
	Status uint32
}

func (e *EchoStatusError) Error() string {
	return fmt.Sprintf("ICMP status %d", e.Status)
}

// echoReply builds the reply for an echo status and the address that
// answered (which may be the zero address). A timeout is errSilent; a general
// failure or an unknown status is an *EchoStatusError.
func echoReply(status uint32, from netip.Addr) (reply, error) {
	var icmpType uint8
	switch status {
	case ipSuccess:
		icmpType = 0
	case ipTTLExpiredTransit:
		icmpType = icmpTimeExceeded
	case ipReqTimedOut:
		return reply{}, errSilent
	case ipDestNetUnreachable, ipDestHostUnreachable, ipDestProtUnreachable,
		ipDestPortUnreachable, ipTTLExpiredReassem, ipDestUnreachable,
		ipTimeExceeded, ipDestScopeMismatch:
		// Kept as is: classifyEcho decides what they mean.
	default: // ipGeneralFailure and anything unknown
		return reply{}, &EchoStatusError{Status: status}
	}
	return reply{Addr: from, ICMPType: icmpType, Origin: originEcho, Status: status}, nil
}

// classifyEcho maps an echo-status reply to the kind of hop it reveals.
func classifyEcho(r reply, _ netip.Addr) HopKind {
	switch r.Status {
	case ipSuccess:
		return HopTarget
	case ipTTLExpiredTransit:
		return HopRouter
	case ipDestNetUnreachable, ipDestHostUnreachable,
		ipDestProtUnreachable, ipDestPortUnreachable:
		return HopUnreachable
	}
	return HopError
}

// decodeEchoReply4 reads the address and status from an ICMP_ECHO_REPLY.
func decodeEchoReply4(buf []byte) (netip.Addr, uint32, error) {
	if len(buf) < echoReplyMinLen {
		return netip.Addr{}, 0, errors.New("echo reply too short")
	}
	return netip.AddrFrom4([4]byte(buf[0:4])), binary.LittleEndian.Uint32(buf[4:8]), nil
}

// echoOutcome turns the result of one IcmpSendEcho2 call into a reply: count
// is its return value (replies stored in the buffer), code the GetLastError
// value. With no reply, code is the IP_STATUS; a zero code carries no status,
// so it is a failure and never a reached target.
func echoOutcome(count uintptr, code uint32, buf []byte) (reply, error) {
	if count >= 1 {
		from, status, err := decodeEchoReply4(buf)
		if err != nil {
			return reply{}, err
		}
		return echoReply(status, from)
	}
	if code == 0 {
		return reply{}, &EchoStatusError{Status: ipGeneralFailure}
	}
	return echoReply(code, netip.Addr{})
}
