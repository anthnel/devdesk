//go:build linux

package nettrace

import (
	"context"
	"net/netip"
)

func supported() error { return nil }

// systemProber runs the trace with the system's sockets.
// TODO(step 4): the engine does not exist yet.
func systemProber(_ context.Context, _ netip.Addr, _ Options) (Result, error) {
	return Result{}, ErrUnsupported
}
