package netproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// Step names the stage of the proxy path that failed.
type Step string

const (
	// StepSelect: the proxy setting is invalid.
	StepSelect Step = "select"
	// StepDial: the proxy cannot be reached.
	StepDial Step = "dial"
	// StepProxyTLS: TLS to an https:// proxy failed.
	StepProxyTLS Step = "proxy-tls"
	// StepConnect: CONNECT was refused, unreadable, or unanswered.
	StepConnect Step = "connect"
	// StepScheme: the proxy scheme is not supported (socks5).
	StepScheme Step = "scheme"
)

// Error is a failure at the proxy step of a connection, as opposed to a failure
// of the target itself. Proxy is host:port, never a userinfo.
type Error struct {
	// Proxy is the proxy's host:port.
	Proxy string
	// Step is where the proxy path failed.
	Step Step
	// StatusCode is the proxy's answer to CONNECT, 0 when it never gave one.
	StatusCode int
	// Err is the underlying cause, if any.
	Err error
}

// Error implements error. The text is built from the step, the status code and
// the cause's kind, never from the proxy's own words (a proxy is not trusted to
// phrase a message shown to the user): a refused CONNECT shows the standard
// reason phrase of its code, or the bare code when it has none.
func (e *Error) Error() string {
	switch e.Step {
	case StepSelect:
		return "proxy: invalid proxy setting: " + causeText(e.Err)
	case StepScheme:
		return "proxy: scheme " + causeText(e.Err) + " is not supported by the SSL check"
	case StepDial:
		return "proxy: cannot reach " + e.Proxy + ": " + causeText(e.Err)
	case StepProxyTLS:
		return "proxy: TLS to " + e.Proxy + " failed: " + causeText(e.Err)
	}
	if e.StatusCode != 0 {
		if text := http.StatusText(e.StatusCode); text != "" {
			return fmt.Sprintf("proxy: CONNECT refused (%d %s)", e.StatusCode, text)
		}
		return fmt.Sprintf("proxy: CONNECT refused (%d)", e.StatusCode)
	}
	if errors.Is(e.Err, context.Canceled) {
		return "proxy: CONNECT to " + e.Proxy + " canceled"
	}
	if isTimeout(e.Err) {
		return "proxy: no answer from " + e.Proxy + " (timeout)"
	}
	return "proxy: unreadable answer to CONNECT from " + e.Proxy
}

// causeText is err's text, empty for a nil cause.
func causeText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// isTimeout reports whether err is a deadline or a network timeout.
func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }
