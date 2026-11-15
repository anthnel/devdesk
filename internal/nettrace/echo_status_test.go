package nettrace

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"testing"
	"time"
)

// The ICMP echo status mapping is untagged: it runs on every platform, with
// statuses forged by hand (ipexport.h values), never a Windows call.

var (
	echoRouter = netip.MustParseAddr("10.0.0.1")
	echoNoAddr = netip.Addr{}
)

func echoOf(t *testing.T, status uint32, from netip.Addr) reply {
	t.Helper()
	r, err := echoReply(status, from)
	if err != nil {
		t.Fatalf("echoReply(%d, %v): unexpected error %v", status, from, err)
	}
	return r
}

func TestClassifyEchoMapsEveryStatusToAHopKind(t *testing.T) {
	tests := []struct {
		name   string
		status uint32
		from   netip.Addr
		want   HopKind
	}{
		{"success from the target reaches it", ipSuccess, engineTarget, HopTarget},
		{"ttl expired in transit is a router", ipTTLExpiredTransit, echoRouter, HopRouter},
		{"destination net unreachable is refused", ipDestNetUnreachable, echoRouter, HopUnreachable},
		{"destination host unreachable is refused", ipDestHostUnreachable, echoRouter, HopUnreachable},
		{"destination host unreachable from the target is still refused", ipDestHostUnreachable, engineTarget, HopUnreachable},
		{"destination protocol unreachable is refused", ipDestProtUnreachable, echoRouter, HopUnreachable},
		{"destination port unreachable is refused", ipDestPortUnreachable, echoRouter, HopUnreachable},
		{"destination host unreachable without an address is refused", ipDestHostUnreachable, echoNoAddr, HopUnreachable},
		{"ttl expired during reassembly is an error", ipTTLExpiredReassem, echoRouter, HopError},
		{"general failure is an error", ipGeneralFailure, echoNoAddr, HopError},
		{"unknown status is an error", 11999, echoNoAddr, HopError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := reply{Addr: tt.from, Origin: originEcho, Status: tt.status}
			if got := classifyEcho(r, engineTarget); got != tt.want {
				t.Errorf("classifyEcho(status %d, %v) = %v, want %v", tt.status, tt.from, got, tt.want)
			}
			if got := classify(r, engineTarget); got != tt.want {
				t.Errorf("classify must route originEcho to classifyEcho: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOriginEchoIsOutsideKernelOrigins(t *testing.T) {
	if originEcho != 0xF0 {
		t.Errorf("originEcho = %#x, want 0xF0", uint8(originEcho))
	}
	if originEcho == originLocal || originEcho == originICMP {
		t.Error("originEcho collides with a kernel origin")
	}
}

func TestEchoReplyBuildsTheReply(t *testing.T) {
	tests := []struct {
		name     string
		status   uint32
		from     netip.Addr
		wantType uint8
	}{
		{"success is an echo reply, type 0", ipSuccess, engineTarget, 0},
		{"ttl expired is time exceeded, type 11", ipTTLExpiredTransit, echoRouter, icmpTimeExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := echoOf(t, tt.status, tt.from)
			if r.Origin != originEcho {
				t.Errorf("Origin = %#x, want originEcho", r.Origin)
			}
			if r.Status != tt.status {
				t.Errorf("Status = %d, want %d", r.Status, tt.status)
			}
			if r.Addr != tt.from {
				t.Errorf("Addr = %v, want %v", r.Addr, tt.from)
			}
			if r.ICMPType != tt.wantType {
				t.Errorf("ICMPType = %d, want %d", r.ICMPType, tt.wantType)
			}
		})
	}
}

func TestEchoReplyKeepsAnUnreachableWithoutAddress(t *testing.T) {
	r := echoOf(t, ipDestHostUnreachable, echoNoAddr)
	if r.Addr.IsValid() {
		t.Errorf("Addr = %v, want the zero address", r.Addr)
	}
	if got := classifyEcho(r, engineTarget); got != HopUnreachable {
		t.Errorf("kind = %v, want HopUnreachable", got)
	}
}

func TestEchoReplyRequestTimedOutIsSilent(t *testing.T) {
	_, err := echoReply(ipReqTimedOut, echoNoAddr)
	if !errors.Is(err, errSilent) {
		t.Fatalf("echoReply(IP_REQ_TIMED_OUT) error = %v, want errSilent", err)
	}
}

func TestEchoReplyReportsUnusableStatusesAsEchoStatusError(t *testing.T) {
	tests := []struct {
		name   string
		status uint32
		text   string
	}{
		{"general failure", ipGeneralFailure, "ICMP status 11050"},
		{"unknown status", 11999, "ICMP status 11999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := echoReply(tt.status, echoNoAddr)
			var se *EchoStatusError
			if !errors.As(err, &se) {
				t.Fatalf("error = %v (%T), want *EchoStatusError", err, err)
			}
			if se.Status != tt.status {
				t.Errorf("Status = %d, want %d", se.Status, tt.status)
			}
			if err.Error() != tt.text {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.text)
			}
			for _, c := range err.Error() {
				if c > 127 {
					t.Errorf("Error() %q holds a non-ASCII character %q (no localized system text)", err.Error(), c)
				}
			}
		})
	}
}

func TestEchoStatusErrorText(t *testing.T) {
	if got := (&EchoStatusError{Status: 11050}).Error(); got != "ICMP status 11050" {
		t.Errorf("Error() = %q, want %q", got, "ICMP status 11050")
	}
}

// forgedEchoReply4 builds an ICMP_ECHO_REPLY (64-bit layout): Address at 0 in
// network order, Status at 4 little-endian, padded to the full structure.
func forgedEchoReply4(addr [4]byte, status uint32, size int) []byte {
	b := make([]byte, size)
	copy(b[0:4], addr[:])
	binary.LittleEndian.PutUint32(b[4:8], status)
	return b
}

func TestDecodeEchoReply4(t *testing.T) {
	tests := []struct {
		name       string
		buf        []byte
		wantFrom   string
		wantStatus uint32
		wantErr    bool
	}{
		{"ttl expired from a router", forgedEchoReply4([4]byte{10, 0, 0, 1}, ipTTLExpiredTransit, 32), "10.0.0.1", ipTTLExpiredTransit, false},
		{"success from the target", forgedEchoReply4([4]byte{192, 0, 2, 99}, ipSuccess, 40), "192.0.2.99", ipSuccess, false},
		{"exactly the 8 bytes needed", forgedEchoReply4([4]byte{1, 2, 3, 4}, ipDestHostUnreachable, 8), "1.2.3.4", ipDestHostUnreachable, false},
		{"seven bytes is too short", make([]byte, 7), "", 0, true},
		{"empty buffer", nil, "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, status, err := decodeEchoReply4(tt.buf)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("decodeEchoReply4 = (%v, %d, nil), want an error", from, status)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if from != netip.MustParseAddr(tt.wantFrom) {
				t.Errorf("from = %v, want %s", from, tt.wantFrom)
			}
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
		})
	}
}

// Fixtures in the style of engine_test.go, with originEcho replies.

func echoRouterReply(addr string) *reply {
	r, _ := echoReply(ipTTLExpiredTransit, netip.MustParseAddr(addr))
	return &r
}

func echoTargetReply() *reply {
	r, _ := echoReply(ipSuccess, engineTarget)
	return &r
}

func echoUnreachReply(addr netip.Addr) *reply {
	r, _ := echoReply(ipDestHostUnreachable, addr)
	return &r
}

func TestRunReachesTheTargetWithEchoReplies(t *testing.T) {
	fp := newFake(6, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
		1: {rep: echoRouterReply("10.0.0.1"), delay: 5 * time.Millisecond},
		2: {rep: echoRouterReply("10.0.0.2"), delay: 10 * time.Millisecond},
		3: {rep: echoTargetReply(), delay: 15 * time.Millisecond},
	}))
	res := mustRun(t, context.Background(), Options{MaxHops: 6, ProbeTimeout: 5 * time.Second}, fp)
	assertBalanced(t, fp)

	if res.Outcome != OutcomeReached {
		t.Fatalf("Outcome = %v, want reached", res.Outcome)
	}
	if len(res.Hops) != 3 {
		t.Fatalf("len(Hops) = %d, want 3: %+v", len(res.Hops), res.Hops)
	}
	wantKinds := []HopKind{HopRouter, HopRouter, HopTarget}
	for i, k := range wantKinds {
		if res.Hops[i].Kind != k {
			t.Errorf("Hops[%d].Kind = %v, want %v", i, res.Hops[i].Kind, k)
		}
	}
	if res.Hops[0].ICMPType != icmpTimeExceeded {
		t.Errorf("Hops[0].ICMPType = %d, want %d", res.Hops[0].ICMPType, icmpTimeExceeded)
	}
	if res.Hops[2].Addr != engineTarget {
		t.Errorf("Hops[2].Addr = %v, want the target", res.Hops[2].Addr)
	}
}

func TestRunEndsUnreachableOnAnEchoDestinationUnreachable(t *testing.T) {
	tests := []struct {
		name string
		from netip.Addr
	}{
		{"from a router", echoRouter},
		{"without an address", echoNoAddr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := newFake(5, table(probeSpec{ignoreUntil: true}, map[int]probeSpec{
				1: {rep: echoRouterReply("10.0.0.9"), delay: 5 * time.Millisecond},
				2: {rep: echoUnreachReply(tt.from), delay: 10 * time.Millisecond},
			}))
			res := mustRun(t, context.Background(), Options{MaxHops: 5, ProbeTimeout: 5 * time.Second}, fp)
			assertBalanced(t, fp)

			if res.Outcome != OutcomeUnreachable {
				t.Fatalf("Outcome = %v, want unreachable", res.Outcome)
			}
			if len(res.Hops) != 2 {
				t.Fatalf("len(Hops) = %d, want 2: %+v", len(res.Hops), res.Hops)
			}
			if res.Hops[1].Kind != HopUnreachable {
				t.Errorf("Hops[1].Kind = %v, want HopUnreachable", res.Hops[1].Kind)
			}
		})
	}
}

func TestEchoOutcomeNoReplyWithoutAStatusIsAFailure(t *testing.T) {
	_, err := echoOutcome(0, 0, nil)
	var se *EchoStatusError
	if !errors.As(err, &se) || se.Status != ipGeneralFailure {
		t.Fatalf("err = %v, want *EchoStatusError %d", err, ipGeneralFailure)
	}
}

func TestEchoOutcomeNoReplyUsesTheLastErrorAsStatus(t *testing.T) {
	if _, err := echoOutcome(0, ipReqTimedOut, nil); !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want errSilent", err)
	}
}

func TestEchoOutcomeDecodesAReply(t *testing.T) {
	buf := []byte{10, 0, 0, 1, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(buf[4:8], ipTTLExpiredTransit)
	rep, err := echoOutcome(1, 0, buf)
	if err != nil || rep.Addr != netip.MustParseAddr("10.0.0.1") || rep.Status != ipTTLExpiredTransit {
		t.Fatalf("reply = %+v, err = %v", rep, err)
	}
}
