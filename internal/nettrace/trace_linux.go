//go:build linux

package nettrace

import (
	"context"
	"net/netip"
)

func supported() error { return nil }

// unsupportedProber is the provisional system prober: it opens nothing.
// TODO(step 4): replace it with the UDP/IP_RECVERR prober.
type unsupportedProber struct{}

func (unsupportedProber) open(context.Context, netip.Addr, int) (probeConn, error) {
	return nil, ErrUnsupported
}

// systemProber returns the prober that uses the system's sockets.
func systemProber() prober { return unsupportedProber{} }
