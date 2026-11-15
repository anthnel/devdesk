package nettrace

import (
	"errors"
	"fmt"
	"syscall"
)

// IsNoRoute reports whether err means "this machine has no route to the
// target": ErrNoRoute, or a raw network/host unreachable errno anywhere in the
// chain. The error text alone never counts (it is localized). It is the single
// place that classifies a no-route failure.
func IsNoRoute(err error) bool {
	if errors.Is(err, ErrNoRoute) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}
	for _, errno := range platformNoRouteErrnos {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}

// connectError maps a connect failure. "Network" or "host unreachable" is
// ErrNoRoute; a refused connect is never ErrUnsupported.
func connectError(err error) error {
	if IsNoRoute(err) {
		if errors.Is(err, ErrNoRoute) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrNoRoute, err)
	}
	return err
}
