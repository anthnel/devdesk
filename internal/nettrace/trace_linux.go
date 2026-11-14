//go:build linux

package nettrace

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// defaultProbePort is the destination port of every probe, the traditional
// traceroute base port plus one: nothing normally listens there, so the target
// answers with Port Unreachable.
const defaultProbePort = 33435

// probePayload is the neutral content of a probe.
var probePayload = []byte("abcdefghijklmnopqrstuvwabcdefghi")

// errQueueBufLen sizes the control buffer for one IP_RECVERR message
// (cmsghdr, sock_extended_err and the offender's sockaddr).
const errQueueBufLen = 256

var errNoExtendedErr = errors.New("no IP_RECVERR control message in the error queue")

func supported() error { return nil }

// systemProber returns the prober that uses the system's sockets.
func systemProber() prober { return linuxProber{} }

// linuxProber probes with one connected, unprivileged UDP socket per TTL: the
// TTL is set with IP_TTL, IP_RECVERR makes the kernel queue the ICMP errors
// the probe provokes, and await reads them from the socket's error queue.
type linuxProber struct {
	port int // destination port; 0 means defaultProbePort
}

func (p linuxProber) open(ctx context.Context, dst netip.Addr, ttl int) (probeConn, error) {
	port := p.port
	if port == 0 {
		port = defaultProbePort
	}
	d := net.Dialer{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		if err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TTL, ttl)
			if serr == nil {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVERR, 1)
			}
		}); err != nil {
			return err
		}
		if serr != nil {
			return os.NewSyscallError("setsockopt", serr)
		}
		return nil
	}}
	conn, err := d.DialContext(ctx, "udp4", net.JoinHostPort(dst.String(), strconv.Itoa(port)))
	if err != nil {
		var se *os.SyscallError
		if errors.As(err, &se) && se.Syscall == "connect" {
			return nil, connectError(err)
		}
		return nil, unsupportedSocketError(err)
	}
	udp := conn.(*net.UDPConn)
	raw, err := udp.SyscallConn()
	if err != nil {
		_ = udp.Close()
		return nil, err
	}
	return &linuxConn{conn: udp, raw: raw}, nil
}

// unsupportedSocketError maps a socket or setsockopt failure that means "this
// system will not do unprivileged probing" to ErrUnsupported, keeping the raw
// error. Other errors (EMFILE...) are returned as they are.
func unsupportedSocketError(err error) error {
	for _, errno := range []error{syscall.ENOPROTOOPT, syscall.EOPNOTSUPP, syscall.EPERM, syscall.EACCES} {
		if errors.Is(err, errno) {
			return fmt.Errorf("%w: %w", ErrUnsupported, err)
		}
	}
	return err
}

// connectError maps a connect failure. "Network unreachable" is ErrNoRoute; a
// refused connect is never ErrUnsupported.
func connectError(err error) error {
	if errors.Is(err, syscall.ENETUNREACH) {
		return fmt.Errorf("%w: %w", ErrNoRoute, err)
	}
	return err
}

type linuxConn struct {
	conn *net.UDPConn
	raw  syscall.RawConn
}

func (c *linuxConn) send() (time.Time, error) {
	sentAt := time.Now()
	if _, err := c.conn.Write(probePayload); err != nil {
		return time.Time{}, err
	}
	return sentAt, nil
}

// await reads the socket's error queue until an error arrives, until passes
// (errSilent) or ctx ends (ctx.Err()).
func (c *linuxConn) await(ctx context.Context, until time.Time) (reply, error) {
	if err := ctx.Err(); err != nil {
		return reply{}, err
	}
	if err := c.conn.SetReadDeadline(until); err != nil {
		return reply{}, err
	}
	// Registered after the deadline is set, so the callback's past deadline
	// always wins. ctx.Err() tells a cancellation from a timeout afterwards.
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetReadDeadline(time.Unix(1, 0)) })
	defer stop()

	oob := make([]byte, errQueueBufLen)
	var oobn int
	var recvErr error
	err := c.raw.Read(func(fd uintptr) bool {
		buf := make([]byte, 1)
		for {
			_, oobn, _, _, recvErr = unix.Recvmsg(int(fd), buf, oob, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
			if recvErr != unix.EINTR {
				break
			}
		}
		return recvErr != unix.EAGAIN
	})
	switch {
	case ctx.Err() != nil && err != nil:
		return reply{}, ctx.Err()
	case errors.Is(err, os.ErrDeadlineExceeded):
		// When ctx's own deadline is not after until, the trace's deadline
		// (not the probe timeout) is what ran out: let ctx report it.
		if dl, ok := ctx.Deadline(); ok && !dl.After(until) {
			<-ctx.Done()
			return reply{}, ctx.Err()
		}
		return reply{}, errSilent
	case err != nil:
		return reply{}, err
	case recvErr != nil:
		return reply{}, os.NewSyscallError("recvmsg", recvErr)
	}
	return parseErrQueue(oob[:oobn])
}

func (c *linuxConn) close() error { return c.conn.Close() }

// parseErrQueue decodes the IP_RECVERR control message of a MSG_ERRQUEUE read.
func parseErrQueue(oob []byte) (reply, error) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return reply{}, err
	}
	for _, m := range msgs {
		if m.Header.Level == unix.SOL_IP && m.Header.Type == unix.IP_RECVERR {
			return decodeExtendedErr(m.Data)
		}
	}
	return reply{}, errNoExtendedErr
}
