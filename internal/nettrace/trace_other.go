//go:build !linux

package nettrace

import (
	"context"
	"net/netip"
)

func supported() error { return ErrUnsupported }

// systemProber is never reached: Trace stops at supported() first.
func systemProber(_ context.Context, _ netip.Addr, _ Options) (Result, error) {
	return Result{}, ErrUnsupported
}
