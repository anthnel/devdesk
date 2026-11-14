package nettrace

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

// Constants mirrored from the kernel headers. The tests spell them out so a
// wrong value in the production code cannot hide behind a shared constant.
const (
	testAFInet        = 2
	testOriginLocal   = 1
	testOriginICMP    = 2
	testICMPDestUnrch = 3
	testICMPTimeExc   = 11
	testCodePortUnrch = 3
)

// extErrBytes builds an IP_RECVERR control message payload: a 16-byte
// sock_extended_err followed by the 16-byte sockaddr_in of the ICMP sender.
func extErrBytes(errno uint32, origin, icmpType, icmpCode uint8, family uint16, port uint16, sender [4]byte) []byte {
	b := make([]byte, 32)
	binary.NativeEndian.PutUint32(b[0:4], errno)
	b[4] = origin
	b[5] = icmpType
	b[6] = icmpCode
	// b[7] is ee_pad, b[8:16] are ee_info and ee_data: left at zero.
	binary.NativeEndian.PutUint16(b[16:18], family)
	binary.BigEndian.PutUint16(b[18:20], port)
	copy(b[20:24], sender[:])
	// b[24:32] is the sockaddr_in padding.
	return b
}

func TestDecodeExtendedErr(t *testing.T) {
	router := netip.MustParseAddr("192.0.2.1")
	tests := []struct {
		name string
		data []byte
		want reply
	}{
		{
			name: "time exceeded from a router",
			data: extErrBytes(113, testOriginICMP, testICMPTimeExc, 0, testAFInet, 0, [4]byte{192, 0, 2, 1}),
			want: reply{Addr: router, ICMPType: testICMPTimeExc, ICMPCode: 0, Origin: testOriginICMP, Errno: 113},
		},
		{
			name: "port unreachable with a nonzero port",
			data: extErrBytes(111, testOriginICMP, testICMPDestUnrch, testCodePortUnrch, testAFInet, 33434, [4]byte{192, 0, 2, 1}),
			want: reply{Addr: router, ICMPType: testICMPDestUnrch, ICMPCode: testCodePortUnrch, Origin: testOriginICMP, Errno: 111},
		},
		{
			name: "local origin keeps its errno",
			data: extErrBytes(90, testOriginLocal, 0, 0, testAFInet, 0, [4]byte{10, 0, 0, 5}),
			want: reply{Addr: netip.MustParseAddr("10.0.0.5"), Origin: testOriginLocal, Errno: 90},
		},
		{
			name: "unspecified sender address is kept as is",
			data: extErrBytes(113, testOriginICMP, testICMPTimeExc, 0, testAFInet, 0, [4]byte{}),
			want: reply{Addr: netip.IPv4Unspecified(), ICMPType: testICMPTimeExc, Origin: testOriginICMP, Errno: 113},
		},
		{
			name: "trailing bytes after the sockaddr are ignored",
			data: append(extErrBytes(113, testOriginICMP, testICMPTimeExc, 0, testAFInet, 0, [4]byte{192, 0, 2, 1}), 0xAA, 0xBB, 0xCC),
			want: reply{Addr: router, ICMPType: testICMPTimeExc, Origin: testOriginICMP, Errno: 113},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeExtendedErr(tc.data)
			if err != nil {
				t.Fatalf("decodeExtendedErr() error = %v, want nil", err)
			}
			if got != tc.want {
				t.Errorf("decodeExtendedErr() = %+v, want %+v", got, tc.want)
			}
			if got.RTT != 0 {
				t.Errorf("RTT = %v, want 0: the caller sets it", got.RTT)
			}
			if !got.Addr.Is4() {
				t.Errorf("Addr = %v, want a plain IPv4 address", got.Addr)
			}
		})
	}
}

func TestDecodeExtendedErrRejects(t *testing.T) {
	valid := extErrBytes(113, testOriginICMP, testICMPTimeExc, 0, testAFInet, 0, [4]byte{192, 0, 2, 1})
	tests := []struct {
		name string
		data []byte
	}{
		{"nil data", nil},
		{"empty data", []byte{}},
		{"15 bytes: one short of sock_extended_err", valid[:15]},
		{"16 bytes: no sockaddr at all", valid[:16]},
		{"24 bytes: truncated sockaddr_in", valid[:24]},
		{"31 bytes: one byte short of the sockaddr_in", valid[:31]},
		{"AF_INET6 family", extErrBytes(113, testOriginICMP, testICMPTimeExc, 0, 10, 0, [4]byte{192, 0, 2, 1})},
		{"AF_UNSPEC family", extErrBytes(113, testOriginICMP, testICMPTimeExc, 0, 0, 0, [4]byte{192, 0, 2, 1})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := decodeExtendedErr(tc.data); err == nil {
				t.Errorf("decodeExtendedErr() = %+v, nil error; want an error", got)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	target := netip.MustParseAddr("203.0.113.9")
	other := netip.MustParseAddr("192.0.2.1")
	tests := []struct {
		name string
		r    reply
		want HopKind
	}{
		{"time exceeded", reply{Addr: other, Origin: testOriginICMP, ICMPType: testICMPTimeExc}, HopRouter},
		{"time exceeded in reassembly (code 1)", reply{Addr: other, Origin: testOriginICMP, ICMPType: testICMPTimeExc, ICMPCode: 1}, HopRouter},
		{"port unreachable from the target", reply{Addr: target, Origin: testOriginICMP, ICMPType: testICMPDestUnrch, ICMPCode: testCodePortUnrch}, HopTarget},
		{"port unreachable from another address", reply{Addr: other, Origin: testOriginICMP, ICMPType: testICMPDestUnrch, ICMPCode: testCodePortUnrch}, HopUnreachable},
		{"net unreachable", reply{Addr: other, Origin: testOriginICMP, ICMPType: testICMPDestUnrch, ICMPCode: 0}, HopUnreachable},
		{"host unreachable", reply{Addr: other, Origin: testOriginICMP, ICMPType: testICMPDestUnrch, ICMPCode: 1}, HopUnreachable},
		{"host unreachable from the target itself", reply{Addr: target, Origin: testOriginICMP, ICMPType: testICMPDestUnrch, ICMPCode: 1}, HopUnreachable},
		{"local origin (EMSGSIZE)", reply{Addr: other, Origin: testOriginLocal, Errno: 90}, HopError},
		{"local origin (EHOSTUNREACH)", reply{Addr: other, Origin: testOriginLocal, Errno: 113}, HopError},
		{"local origin wins over an ICMP-looking type", reply{Addr: target, Origin: testOriginLocal, ICMPType: testICMPDestUnrch, ICMPCode: testCodePortUnrch}, HopError},
		{"unknown ICMP type (parameter problem)", reply{Addr: other, Origin: testOriginICMP, ICMPType: 12}, HopError},
		{"unknown ICMP type (source quench)", reply{Addr: other, Origin: testOriginICMP, ICMPType: 4}, HopError},
		{"unknown origin (none)", reply{Addr: other, Origin: 0, ICMPType: testICMPTimeExc}, HopError},
		{"unknown origin (ICMPv6)", reply{Addr: other, Origin: 3, ICMPType: testICMPTimeExc}, HopError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.r, target); got != tc.want {
				t.Errorf("classify(%+v) = %v, want %v", tc.r, got, tc.want)
			}
		})
	}
}

// TestClassifyKeepsTypeAndCode pins the project decision that a Port
// Unreachable from a non-target address is reported as unreachable with its
// ICMP type and code intact, so the caller can copy them into the Hop.
func TestClassifyKeepsTypeAndCode(t *testing.T) {
	target := netip.MustParseAddr("203.0.113.9")
	r := reply{Addr: netip.MustParseAddr("192.0.2.1"), Origin: testOriginICMP, ICMPType: testICMPDestUnrch, ICMPCode: testCodePortUnrch}
	if got := classify(r, target); got != HopUnreachable {
		t.Fatalf("classify() = %v, want %v", got, HopUnreachable)
	}
	if r.ICMPType != testICMPDestUnrch || r.ICMPCode != testCodePortUnrch {
		t.Errorf("reply was altered: %+v", r)
	}
}
