package nettrace

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestIsNoRoute(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ErrNoRoute", ErrNoRoute, true},
		{"wrapped ErrNoRoute", fmt.Errorf("trace: %w", ErrNoRoute), true},
		{"wrapped ENETUNREACH", fmt.Errorf("netlink error: %w", syscall.ENETUNREACH), true},
		{"wrapped EHOSTUNREACH", fmt.Errorf("connect: %w", syscall.EHOSTUNREACH), true},
		{"raw ENETUNREACH", syscall.ENETUNREACH, true},
		{"syscall error EHOSTUNREACH", os.NewSyscallError("connect", syscall.EHOSTUNREACH), true},
		{"ErrNoRoute joined with another errno", fmt.Errorf("%w: %w", ErrNoRoute, syscall.ECONNREFUSED), true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, false},
		{"EACCES", syscall.EACCES, false},
		// Text alone is unknown, not "no route".
		{"text only", errors.New("no route found for x"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNoRoute(tt.err); got != tt.want {
				t.Errorf("IsNoRoute(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
