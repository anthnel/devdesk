package netproxy

import (
	"context"
	"errors"
	"testing"
)

func TestStepValues(t *testing.T) {
	tests := []struct {
		step Step
		want string
	}{
		{StepSelect, "select"},
		{StepDial, "dial"},
		{StepProxyTLS, "proxy-tls"},
		{StepConnect, "connect"},
		{StepScheme, "scheme"},
	}
	for _, tt := range tests {
		if string(tt.step) != tt.want {
			t.Errorf("step %q, want %q", tt.step, tt.want)
		}
	}
}

// TestErrorText pins the exact sentences the UI will show (Rule 129). In
// StepConnect, the status code decides "refused"; without one, a timeout cause
// is "no answer" and any other cause is "unreadable".
func TestErrorText(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{"refused 403", &Error{Proxy: "proxy.corp:3128", Step: StepConnect, StatusCode: 403},
			"proxy: CONNECT refused (403 Forbidden)"},
		{"refused 407", &Error{Proxy: "proxy.corp:3128", Step: StepConnect, StatusCode: 407},
			"proxy: CONNECT refused (407 Proxy Authentication Required)"},
		{"refused 502 with a cause", &Error{Proxy: "proxy.corp:3128", Step: StepConnect, StatusCode: 502, Err: boom},
			"proxy: CONNECT refused (502 Bad Gateway)"},
		{"refused with an unknown status", &Error{Proxy: "proxy.corp:3128", Step: StepConnect, StatusCode: 299},
			"proxy: CONNECT refused (299)"},
		{"canceled", &Error{Proxy: "proxy.corp:3128", Step: StepConnect, Err: context.Canceled},
			"proxy: CONNECT to proxy.corp:3128 canceled"},
		{"no answer", &Error{Proxy: "proxy.corp:3128", Step: StepConnect, Err: context.DeadlineExceeded},
			"proxy: no answer from proxy.corp:3128 (timeout)"},
		{"unreadable", &Error{Proxy: "proxy.corp:3128", Step: StepConnect, Err: boom},
			"proxy: unreadable answer to CONNECT from proxy.corp:3128"},
		{"unreachable", &Error{Proxy: "proxy.corp:3128", Step: StepDial, Err: boom},
			"proxy: cannot reach proxy.corp:3128: boom"},
		{"proxy TLS", &Error{Proxy: "proxy.corp:3128", Step: StepProxyTLS, Err: boom},
			"proxy: TLS to proxy.corp:3128 failed: boom"},
		{"invalid setting", &Error{Step: StepSelect, Err: boom},
			"proxy: invalid proxy setting: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorUnwrap(t *testing.T) {
	boom := errors.New("boom")
	var err error = &Error{Proxy: "proxy.corp:3128", Step: StepDial, Err: boom}
	if !errors.Is(err, boom) {
		t.Error("errors.Is does not reach the cause")
	}
	if got := (&Error{Step: StepConnect, StatusCode: 403}).Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil without a cause", got)
	}
}
