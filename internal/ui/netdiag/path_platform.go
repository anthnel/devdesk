package netdiag

import (
	"errors"
	"runtime"
	"sync"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/wslenv"
)

const (
	// reasonPathUnsupported is the generic refusal: the platform cannot trace a
	// path and no more precise reason is known. The header greys enter out and
	// the handler refuses with the same text.
	reasonPathUnsupported = "The path trace is not supported on this platform"

	// The platform-specific reasons name what is missing and, where one exists,
	// the way out.
	reasonPathMacOS = "The path trace needs Linux — not available on macOS"
	reasonPathWSL1  = "The path trace needs a Linux kernel — WSL1 has none (use WSL2)"
)

// pathPlatformError is a refusal that names the platform. It unwraps to
// nettrace.ErrUnsupported so callers that test for that keep working.
type pathPlatformError struct{ reason string }

func (e *pathPlatformError) Error() string { return e.reason }

func (e *pathPlatformError) Unwrap() error { return nettrace.ErrUnsupported }

// pathSupport answers whether this platform can trace a path; nil means it
// can. A WSL2 whose networking mode is unknown never refuses.
func pathSupport(goos string, kind wslenv.Kind, traceErr error) error {
	switch {
	case goos == "darwin":
		return &pathPlatformError{reason: reasonPathMacOS}
	case goos != "linux" && goos != "windows":
		return &pathPlatformError{reason: "The path trace is not available on " + goos}
	case kind == wslenv.KindWSL1:
		return &pathPlatformError{reason: reasonPathWSL1}
	}
	return traceErr
}

// systemPathSupport detects the platform once: launchable runs on every
// render, so it must not do any I/O.
var systemPathSupport = sync.OnceValue(func() error {
	return pathSupport(runtime.GOOS, wslenv.DetectKind(), nettrace.Supported())
})

// pathUnsupportedReason is the sentence shown for a refusal from supported.
func pathUnsupportedReason(err error) string {
	var pe *pathPlatformError
	if errors.As(err, &pe) {
		return pe.reason
	}
	return reasonPathUnsupported
}
