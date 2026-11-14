//go:build !linux

package nettrace

import (
	"context"
	"net/netip"
)

func supported() error { return ErrUnsupported }

// unsupportedProber opens nothing: Trace stops at supported() first.
type unsupportedProber struct{}

func (unsupportedProber) open(context.Context, netip.Addr, int) (probeConn, error) {
	return nil, ErrUnsupported
}

// systemProber returns a prober that never opens a connection.
func systemProber() prober { return unsupportedProber{} }
