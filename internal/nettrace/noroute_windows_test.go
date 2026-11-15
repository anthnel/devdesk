//go:build windows

package nettrace

import (
	"errors"
	"fmt"
	"net"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// go-netroute returns a raw windows.Errno, so IsNoRoute has to recognize the
// Windows codes by value: the message is localized and never matched.
func TestIsNoRouteWindowsErrnos(t *testing.T) {
	proofs := []struct {
		name  string
		errno windows.Errno
	}{
		{"WSAENETUNREACH", windows.WSAENETUNREACH},
		{"WSAEHOSTUNREACH", windows.WSAEHOSTUNREACH},
		{"ERROR_NETWORK_UNREACHABLE", windows.ERROR_NETWORK_UNREACHABLE},
		{"ERROR_HOST_UNREACHABLE", windows.ERROR_HOST_UNREACHABLE},
	}
	wraps := []struct {
		name string
		wrap func(error) error
	}{
		{"raw", func(e error) error { return e }},
		{"SyscallError", func(e error) error { return os.NewSyscallError("route", e) }},
		{"OpError", func(e error) error { return &net.OpError{Op: "dial", Net: "tcp", Err: e} }},
		{"percent-w", func(e error) error { return fmt.Errorf("lookup: %w", e) }},
	}
	for _, p := range proofs {
		for _, w := range wraps {
			t.Run(p.name+"/"+w.name, func(t *testing.T) {
				if err := w.wrap(p.errno); !IsNoRoute(err) {
					t.Errorf("IsNoRoute(%#v) = false, want true", err)
				}
			})
		}
	}

	notProofs := []struct {
		name string
		err  error
	}{
		{"WSAECONNREFUSED", windows.WSAECONNREFUSED},
		{"ERROR_ACCESS_DENIED", windows.ERROR_ACCESS_DENIED},
		{"text only", errors.New("no route found for x")},
	}
	for _, n := range notProofs {
		t.Run("not "+n.name, func(t *testing.T) {
			if IsNoRoute(n.err) {
				t.Errorf("IsNoRoute(%v) = true, want false", n.err)
			}
		})
	}
}
