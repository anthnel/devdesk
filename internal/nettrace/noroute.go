package nettrace

import (
	"errors"
	"syscall"
)

// IsNoRoute reports whether err means "this machine has no route to the
// target": ErrNoRoute, or a raw network/host unreachable errno anywhere in the
// chain. The error text alone never counts. It is the single place that
// classifies a no-route failure.
func IsNoRoute(err error) bool {
	return errors.Is(err, ErrNoRoute) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH)
}
