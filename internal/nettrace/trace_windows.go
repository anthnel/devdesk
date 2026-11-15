//go:build windows

package nettrace

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultProbePort is the port the preflight connects to; no packet is sent.
const defaultProbePort = 33435

// echoReplyPad is the room the echo API asks for beyond the reply struct and
// the echoed data: an 8-byte ICMP error block (IcmpSendEcho2 documentation).
// echoReplyExtra adds slack for the IO_STATUS_BLOCK the API may append.
const (
	echoReplyPad   = 8
	echoReplyExtra = 16
)

var (
	iphlpapi        = windows.NewLazySystemDLL("iphlpapi.dll")
	procCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procSendEcho2   = iphlpapi.NewProc("IcmpSendEcho2")
	procCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	invalidIcmpFile = ^uintptr(0)
)

// ipOptionInformation mirrors IP_OPTION_INFORMATION (ipexport.h).
type ipOptionInformation struct {
	Ttl         uint8
	Tos         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData unsafe.Pointer
}

// icmpEchoReply mirrors ICMP_ECHO_REPLY (ipexport.h).
type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          unsafe.Pointer
	Options       ipOptionInformation
}

// supported reports whether the echo entry points of iphlpapi.dll resolve.
func supported() error {
	for _, err := range []error{procCreateFile.Find(), procSendEcho2.Find(), procCloseHandle.Find()} {
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnsupported, err)
		}
	}
	return nil
}

// systemProber returns the prober that uses IcmpSendEcho2.
func systemProber() prober {
	return &echoProber{api: winEcho{}, preflight: udpPreflight}
}

// udpPreflight connects (without sending) a UDP socket to dst so a missing
// route fails here with the raw Winsock error instead of looking like silence.
func udpPreflight(ctx context.Context, dst netip.Addr) error {
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp4", netip.AddrPortFrom(dst, defaultProbePort).String())
	if err != nil {
		return err
	}
	return c.Close()
}

// winEcho is the echoAPI backed by IcmpSendEcho2.
type winEcho struct{}

func (winEcho) create() (uintptr, error) {
	h, _, err := procCreateFile.Call()
	if h != invalidIcmpFile {
		return h, nil
	}
	if errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return 0, fmt.Errorf("%w: %w", ErrUnsupported, err)
	}
	return 0, fmt.Errorf("IcmpCreateFile: %w", err)
}

func (winEcho) closeHandle(h uintptr) error {
	if r, _, err := procCloseHandle.Call(h); r == 0 {
		return fmt.Errorf("IcmpCloseHandle: %w", err)
	}
	return nil
}

// echo runs one synchronous IcmpSendEcho2. The Go buffers are safe because the
// call returns before they can move.
func (winEcho) echo(h uintptr, dst netip.Addr, ttl int, timeout time.Duration) (reply, error) {
	if !dst.Is4() {
		return reply{}, ErrNotIPv4
	}
	buf := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(probePayload)+echoReplyPad+echoReplyExtra)
	opts := ipOptionInformation{Ttl: uint8(ttl)}
	addr := binary.NativeEndian.Uint32(dst.AsSlice())
	r1, _, errno := procSendEcho2.Call(h, 0, 0, 0, uintptr(addr),
		uintptr(unsafe.Pointer(&probePayload[0])), uintptr(len(probePayload)),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(max(timeout.Milliseconds(), 1)))
	runtime.KeepAlive(buf)
	runtime.KeepAlive(&opts)
	if r1 >= 1 {
		return echoOutcome(r1, 0, buf)
	}
	// No reply: GetLastError carries the IP_STATUS, never a localized text.
	var code windows.Errno
	if !errors.As(errno, &code) {
		return reply{}, errors.New("IcmpSendEcho2 failed: " + strconv.Itoa(int(r1)))
	}
	return echoOutcome(r1, uint32(code), buf)
}
